package ctxgit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// A Claude Code transcript is JSONL, one entry a line (DESIGN §4.7). An
// entry of the conversation has a uuid and the uuid of the entry before it,
// parentUuid, so a transcript is a tree: a /rewind or an interrupted turn
// starts a new line of entries from an older one, and the newest entry of
// the file ends the line the conversation is on. A compaction's boundary
// has no parent but a logicalParentUuid, which carries the line on. Other
// lines, such as titles and file checkpoints, have no uuid and are not the
// conversation's. The format is Claude Code's own and changes between
// versions, so anything not understood is skipped, never fatal.

// node is one entry of a transcript, without its text: where its line is,
// and what links it to the others.
type node struct {
	id     string
	parent string
	off    int64 // the line's start in the file
	n      int   // the line's length, without its newline
	prompt bool  // a message from the user that starts a turn
	msg    bool  // a message, the user's or the assistant's, rather than Claude Code's bookkeeping
	at     time.Time
}

// leafTypes are the entries that can end the conversation's line: those a
// resume carries on from. Progress and other bookkeeping lines may have
// uuids too, but the conversation goes on from a message.
var leafTypes = map[string]bool{"user": true, "assistant": true, "system": true, "attachment": true}

// line is the part of a transcript line the store reads.
type line struct {
	Type          string          `json:"type"`
	UUID          string          `json:"uuid"`
	Parent        string          `json:"parentUuid"`
	LogicalParent string          `json:"logicalParentUuid"`
	SessionID     string          `json:"sessionId"`
	Timestamp     string          `json:"timestamp"`
	IsSidechain   bool            `json:"isSidechain"`
	IsMeta        bool            `json:"isMeta"`
	IsCompact     bool            `json:"isCompactSummary"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Message       struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// block is one content block of a message.
type block struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// blocks reads a message's content: a string is one text block.
func blocks(content json.RawMessage) []block {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	json.Unmarshal(content, &bs)
	return bs
}

// promptText is what the user said in a prompt, or "" when the entry is
// not one: a tool's result, a note Claude Code adds (isMeta), a compaction's
// summary, a subagent's line, or the output of a local command, which
// follows the command that started its turn.
func (l line) promptText() string {
	if l.Type != "user" || l.IsMeta || l.IsCompact || l.IsSidechain || len(l.ToolUseResult) > 0 {
		return ""
	}
	var texts []string
	images := 0
	for _, b := range blocks(l.Message.Content) {
		switch b.Type {
		case "tool_result":
			return ""
		case "text":
			texts = append(texts, b.Text)
		case "image":
			images++
		}
	}
	text := strings.TrimSpace(strings.Join(texts, "\n"))
	for _, p := range []string{"<local-command-stdout>", "<local-command-stderr>", "<bash-stdout>", "<bash-stderr>", "<local-command-caveat>"} {
		if strings.HasPrefix(text, p) {
			return ""
		}
	}
	if text == "" && images > 0 {
		return "(an image)"
	}
	return commandText(text)
}

// commandText says a slash command or a shell command the way it was
// typed: "/model opus" for its tags, "! ls" for a shell one; and a notice
// that background work finished by its summary.
func commandText(text string) string {
	tag := func(name string) string {
		_, after, ok := strings.Cut(text, "<"+name+">")
		if !ok {
			return ""
		}
		v, _, _ := strings.Cut(after, "</"+name+">")
		return strings.TrimSpace(v)
	}
	if name := tag("command-name"); name != "" {
		if args := tag("command-args"); args != "" {
			return name + " " + args
		}
		return name
	}
	if in := tag("bash-input"); in != "" {
		return "! " + in
	}
	if strings.HasPrefix(text, "<task-notification>") {
		if sum := tag("summary"); sum != "" {
			return "Background: " + sum
		}
	}
	return text
}

// feed reads one transcript file on from where it last stopped, keeping
// where each entry is but not its text, which is read again when a turn is
// stored. A file that shrank was replaced, and is read again from the top.
type feed struct {
	path  string
	off   int64 // bytes read
	nodes map[string]*node
	leaf  *node     // the newest entry that can end the conversation's line
	used  time.Time // when a sync last read it
}

func newFeed(path string) *feed { return &feed{path: path, nodes: map[string]*node{}} }

// read takes in the lines written since the last read. A last line without
// its newline is still being written, and waits for the next read.
func (f *feed) read() error {
	file, err := os.Open(f.path)
	if err != nil {
		return err
	}
	defer file.Close()
	fi, err := file.Stat()
	if err != nil {
		return err
	}
	if fi.Size() < f.off {
		*f = *newFeed(f.path)
	}
	if _, err := file.Seek(f.off, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(file, 256<<10)
	for {
		b, err := r.ReadBytes('\n')
		if len(b) == 0 || b[len(b)-1] != '\n' {
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			return nil
		}
		start := f.off
		f.off += int64(len(b))
		f.take(bytes.TrimRight(b, "\r\n"), start)
	}
}

// take records one line read at off.
func (f *feed) take(b []byte, off int64) {
	var l line
	if json.Unmarshal(b, &l) != nil || l.UUID == "" {
		return
	}
	parent := l.Parent
	if parent == "" {
		parent = l.LogicalParent
	}
	at, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
	n := &node{id: l.UUID, parent: parent, off: off, n: len(b), prompt: l.promptText() != "", msg: l.Type == "user" || l.Type == "assistant", at: at}
	first := len(f.nodes) == 0
	f.nodes[n.id] = n
	// The conversation's line goes on only from an entry that links to the
	// line before it; only the first entry of a file stands alone. An
	// entry with no parent later on, of a kind this reader does not know,
	// never takes the line over.
	if leafTypes[l.Type] && !l.IsSidechain && (parent == "" && first || parent != "" && f.nodes[parent] != nil) {
		f.leaf = n
	}
}

// chain is the conversation's line, oldest first: the newest entry and
// those before it, up to one with no parent the file holds.
func (f *feed) chain() []*node {
	return f.chainTo(f.leaf)
}

// chainTo is the line that ends at n, oldest first.
func (f *feed) chainTo(n *node) []*node {
	var out []*node
	seen := map[string]bool{}
	for n != nil && !seen[n.id] {
		seen[n.id] = true
		out = append(out, n)
		n = f.nodes[n.parent]
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// turns splits a line into turns: the first holds what came before the
// first prompt, which is often nothing, and each other starts with a
// prompt and runs to the next.
func turns(chain []*node) [][]*node {
	out := [][]*node{nil}
	for _, n := range chain {
		if n.prompt {
			out = append(out, nil)
		}
		out[len(out)-1] = append(out[len(out)-1], n)
	}
	return out
}

// lines reads the raw lines of nodes from the file, a newline after each.
func (f *feed) lines(nodes []*node) ([]byte, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	file, err := os.Open(f.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out bytes.Buffer
	// Lines that follow one another in the file are read at once.
	for i := 0; i < len(nodes); {
		j, end := i+1, nodes[i].off+int64(nodes[i].n)+1
		for j < len(nodes) && nodes[j].off == end {
			end = nodes[j].off + int64(nodes[j].n) + 1
			j++
		}
		b := make([]byte, end-nodes[i].off)
		if _, err := file.ReadAt(b, nodes[i].off); err != nil {
			return nil, err
		}
		// Each line's own newline, or a \r\n, becomes a newline.
		for _, n := range nodes[i:j] {
			at := n.off - nodes[i].off
			out.Write(b[at : at+int64(n.n)])
			out.WriteByte('\n')
		}
		i = j
	}
	return out.Bytes(), nil
}
