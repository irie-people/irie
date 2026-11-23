package lessons

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/habits"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/journal"
)

// Transcript reads the tool calls in one Claude Code transcript that
// started at since or after, in its order: each call's tool, its shell
// command, when it started and, for one that failed, what its error said
// on one line, as the journal words it (claude.ErrorLine), so that a call
// a hook refused, which never reaches the journal, counts as one that
// failed there would. session and agent say whose calls they are. Lines a
// subagent wrote into its session's transcript are left out: it has a
// transcript of its own.
func Transcript(r io.Reader, session, agent string, since time.Time) ([]habits.Call, error) {
	var out []habits.Call
	at := map[string]int{}
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && (bytes.Contains(line, []byte(`"tool_use"`)) || bytes.Contains(line, []byte(`"tool_result"`))) {
			out = transcriptLine(line, session, agent, since, out, at)
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

// transcriptLine adds the calls one transcript line starts, or ends.
func transcriptLine(line []byte, session, agent string, since time.Time, out []habits.Call, at map[string]int) []habits.Call {
	var rec struct {
		Type        string `json:"type"`
		Timestamp   string `json:"timestamp"`
		IsSidechain bool   `json:"isSidechain"`
		Message     struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &rec) != nil || rec.IsSidechain && agent == event.Main {
		return out
	}
	var blocks []struct {
		Type      string                   `json:"type"`
		ID        string                   `json:"id"`
		Name      string                   `json:"name"`
		Input     struct{ Command string } `json:"input"`
		ToolUseID string                   `json:"tool_use_id"`
		IsError   bool                     `json:"is_error"`
		Content   json.RawMessage          `json:"content"`
	}
	if json.Unmarshal(rec.Message.Content, &blocks) != nil {
		return out
	}
	t, _ := time.Parse(time.RFC3339Nano, rec.Timestamp)
	for _, b := range blocks {
		switch {
		case rec.Type == "assistant" && b.Type == "tool_use":
			if t.Before(since) {
				continue
			}
			c := habits.Call{Session: session, Agent: agent, Tool: b.Name, Family: b.Name, At: t}
			if b.Name == "Bash" {
				// Cut as the journal cuts it, which also bounds what a
				// month of long heredocs holds in memory.
				c.Cmd = event.Cap(b.Input.Command)
				c.Family = habits.Family(c.Cmd)
			}
			at[b.ID] = len(out)
			out = append(out, c)
		case rec.Type == "user" && b.Type == "tool_result":
			i, ok := at[b.ToolUseID]
			if !ok {
				continue
			}
			delete(at, b.ToolUseID)
			if !b.IsError {
				continue
			}
			text := resultText(b.Content)
			out[i].Failed = true
			out[i].Error = claude.ErrorLine(out[i].Tool, text, strings.HasPrefix(text, "[Request interrupted by user"))
		}
	}
	return out
}

// resultText is a tool result's text: a string, or the text of its blocks.
func resultText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(content, &blocks)
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// sessionsBack is how far before a stretch Gather looks for the start of a
// session whose calls fall in it, to find its transcript.
const sessionsBack = 30 * 24 * time.Hour

// Gather reads the evidence of the stretch from since to now from the
// journal in dir and, with transcripts, from the transcripts its sessions'
// starts name, and their subagents' beside them: each session's calls
// from its transcripts when one can be read, which hold the calls hooks
// refused and the time before the journal kept errors (22 Nov 2026), and
// else from the journal.
func Gather(dir string, since, now time.Time, transcripts bool) (Evidence, error) {
	evs, err := journal.ReadKinds(dir, since, event.ToolStarted, event.ToolFinished, event.ToolFailed)
	if err != nil {
		return Evidence{}, err
	}
	fromJournal := habits.Calls(evs)
	var calls []habits.Call
	read := map[string]bool{}
	n := 0
	if transcripts {
		starts, err := journal.ReadKinds(dir, since.Add(-sessionsBack), event.SessionStarted)
		if err != nil {
			return Evidence{}, err
		}
		paths := map[string][]string{}
		var order []string
		for _, e := range starts {
			p := e.Str("transcript")
			if p == "" || !filepath.IsAbs(p) || filepath.Ext(p) != ".jsonl" || containsKind(paths[e.Session], p) {
				continue
			}
			if len(paths[e.Session]) == 0 {
				order = append(order, e.Session)
			}
			paths[e.Session] = append(paths[e.Session], p)
		}
		for _, s := range order {
			for _, p := range paths[s] {
				got, ok := readTranscript(p, s, event.Main, since)
				if !ok {
					continue
				}
				read[s] = true
				n++
				calls = append(calls, got...)
				subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(p, ".jsonl"), "subagents", "agent-*.jsonl"))
				sort.Strings(subs)
				for _, sp := range subs {
					id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(sp), "agent-"), ".jsonl")
					if got, ok := readTranscript(sp, s, "sub:"+id, since); ok {
						n++
						calls = append(calls, got...)
					}
				}
			}
		}
	}
	for _, c := range fromJournal {
		if !read[c.Session] {
			calls = append(calls, c)
		}
	}
	ev := Build(calls)
	ev.From, ev.To, ev.Transcripts = since, now, n
	return ev, nil
}

// readTranscript reads one transcript's calls; ok is false when it can't
// be read.
func readTranscript(path, session, agent string, since time.Time) ([]habits.Call, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || fi.ModTime().Before(since) {
		// Nothing in it is from the stretch.
		return nil, err == nil
	}
	calls, err := Transcript(f, session, agent, since)
	return calls, err == nil
}
