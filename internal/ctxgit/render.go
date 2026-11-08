package ctxgit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// render writes a turn as Markdown, the way a person reads it: the prompt,
// each reply, and a line per tool call with what it was given. Thinking,
// tool results and Claude Code's own notes are left out; the turn's JSONL
// beside it keeps everything. It makes `git log -p` and `git diff` of two
// contexts read as conversations.
func render(raw []byte) []byte {
	md, _, _ := readTurn(raw)
	return md
}

// readTurn reads a turn's lines once: its Markdown, the prompt that starts
// it and its last reply.
func readTurn(raw []byte) (md []byte, prompt, said string) {
	var b bytes.Buffer
	for _, ln := range bytes.Split(raw, []byte{'\n'}) {
		var l line
		if len(ln) == 0 || json.Unmarshal(ln, &l) != nil {
			continue
		}
		if p := l.promptText(); p != "" {
			if prompt == "" {
				prompt = p
			}
			fmt.Fprintf(&b, "## You\n\n%s\n\n", p)
			continue
		}
		if l.Type == "system" && strings.Contains(string(ln), `"compact_boundary"`) {
			b.WriteString("---\n\n*The conversation was compacted.*\n\n")
			continue
		}
		if l.Type != "assistant" || l.IsSidechain {
			continue
		}
		for _, bl := range blocks(l.Message.Content) {
			switch bl.Type {
			case "text":
				if t := strings.TrimSpace(bl.Text); t != "" {
					fmt.Fprintf(&b, "%s\n\n", t)
					said = t
				}
			case "tool_use":
				fmt.Fprintf(&b, "- **%s** %s\n\n", bl.Name, toolLine(bl.Input))
			}
		}
	}
	return b.Bytes(), prompt, said
}

// toolLine is a tool call's input on one line: the field that says the
// most, such as a command or a path, or else the input itself, cut.
func toolLine(input json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "path", "pattern", "url", "query", "description", "prompt", "skill"} {
		if v, ok := in[k].(string); ok && strings.TrimSpace(v) != "" {
			return "`" + oneLine(v, 160) + "`"
		}
	}
	b, _ := json.Marshal(in)
	return "`" + oneLine(string(b), 160) + "`"
}

// oneLine is s on one line, cut at n bytes on a rune's edge.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, "`", "'")
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// lastReply is the last text the assistant wrote in a turn's lines.
func lastReply(raw []byte) string {
	last := ""
	for _, ln := range bytes.Split(raw, []byte{'\n'}) {
		var l line
		if len(ln) == 0 || json.Unmarshal(ln, &l) != nil || l.Type != "assistant" || l.IsSidechain {
			continue
		}
		for _, bl := range blocks(l.Message.Content) {
			if t := strings.TrimSpace(bl.Text); bl.Type == "text" && t != "" {
				last = t
			}
		}
	}
	return last
}

// firstPrompt is the prompt that starts a turn's lines, or "".
func firstPrompt(raw []byte) string {
	for _, ln := range bytes.Split(raw, []byte{'\n'}) {
		var l line
		if len(ln) == 0 || json.Unmarshal(ln, &l) != nil {
			continue
		}
		if p := l.promptText(); p != "" {
			return p
		}
	}
	return ""
}
