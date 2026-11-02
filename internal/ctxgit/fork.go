package ctxgit

import (
	"bytes"
	"encoding/json"
	"strings"
)

// ForkLines makes the transcript of a fork from the lines of a context:
// each line's sessionId becomes the fork's own, as the Agent SDK's
// forkSession() makes a copy that a resume carries on, and, when the fork
// works in another folder, every mention of the folder the context was in
// becomes the fork's, so that its agent reads and edits its own copy and
// never the one it was forked from. A path is replaced only where it stands
// whole: not where it is the start of a longer name, such as /src/app in
// /src/app-2. A line that is not JSON is left out.
func ForkLines(lines []byte, session, from, to string) []byte {
	from, to = strings.TrimRight(from, "/"), strings.TrimRight(to, "/")
	rewrite := from != "" && to != "" && from != to && plainPath(from) && plainPath(to)
	var out bytes.Buffer
	for _, ln := range bytes.Split(lines, []byte{'\n'}) {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		if rewrite {
			ln = replacePath(ln, []byte(from), []byte(to))
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(ln, &m) != nil {
			continue
		}
		if _, ok := m["sessionId"]; ok {
			id, _ := json.Marshal(session)
			m["sessionId"] = id
			b, err := json.Marshal(m)
			if err != nil {
				continue
			}
			ln = b
		}
		out.Write(ln)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// plainPath reports whether p reads the same inside a JSON string as out of
// one, so that its bytes can be replaced in a line as they are.
func plainPath(p string) bool {
	if !strings.HasPrefix(p, "/") {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == '"' || r == '\\' || r == '<' || r == '>' || r == '&' || r == 0x2028 || r == 0x2029 {
			return false
		}
	}
	return true
}

// replacePath replaces from with to in b wherever from is a whole path:
// what follows it is not part of a longer name.
func replacePath(b, from, to []byte) []byte {
	var out []byte
	rest := b
	for {
		i := bytes.Index(rest, from)
		if i < 0 {
			break
		}
		end := i + len(from)
		if end < len(rest) && nameByte(rest[end]) {
			out = append(out, rest[:end]...)
			rest = rest[end:]
			continue
		}
		out = append(out, rest[:i]...)
		out = append(out, to...)
		rest = rest[end:]
	}
	if out == nil {
		return b
	}
	return append(out, rest...)
}

// nameByte reports whether c can go on a file's name.
func nameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c >= 0x80
}
