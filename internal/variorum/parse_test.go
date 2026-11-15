package variorum

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// shellQuoted quotes a path for a POSIX shell as Variorum's setup does: in
// double quotes, with ", \, $ and ` behind a backslash.
func shellQuoted(path string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range path {
		if strings.ContainsRune("\"\\$`", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// claudeSetup is Claude Code's setup as Variorum copies it.
func claudeSetup(name, key, path string) string {
	return "claude mcp remove -s user " + name + " >/dev/null 2>&1; claude mcp add -s user " + name + " -e VARIORUM_MCP_KEY=" + key + " -- " + shellQuoted(path)
}

// anySetup is Any MCP client's setup as Variorum copies it.
func anySetup(key, path string) string {
	quoted, _ := json.Marshal(path)
	return `{
  "mcpServers": {
    "variorum": {
      "command": ` + string(quoted) + `,
      "args": [],
      "env": {
        "VARIORUM_MCP_KEY": "` + key + `"
      }
    }
  }
}`
}

const door = "/Applications/Variorum.app/Contents/MacOS/variorum-mcp"

// TestParseTellsTheSetupsApartByShape: Claude Code's line keeps its key
// and its program, however the path is quoted; Any MCP client's stanza
// does too, and so does the object inside it, or the server's own.
func TestParseTellsTheSetupsApartByShape(t *testing.T) {
	odd := `/home/alice/Applications/Variorum "β" $HOME ` + "`x`" + ` \n.app/Contents/MacOS/variorum-mcp`
	for _, tc := range []struct {
		name, text, kind, command, key string
	}{
		{"Claude Code's", claudeSetup("variorum", sessionsKey, door), Sessions, door, sessionsKey},
		{"its debug build's", claudeSetup("variorum-dev", sessionsKey, door), Sessions, door, sessionsKey},
		{"a path to quote", claudeSetup("variorum", sessionsKey, odd), Sessions, odd, sessionsKey},
		{"pasted with space around", "\n  " + claudeSetup("variorum", sessionsKey, door) + "\n\n", Sessions, door, sessionsKey},
		{"only the add", "claude mcp add --scope user variorum --env VARIORUM_MCP_KEY=" + sessionsKey + " -- '" + door + "'", Sessions, door, sessionsKey},
		{"Any MCP client's", anySetup(tabKey, door), Tab, door, tabKey},
		{"a path to escape", anySetup(tabKey, odd), Tab, odd, tabKey},
		{"the object inside", `{"variorum": {"command": "` + door + `", "env": {"VARIORUM_MCP_KEY": "` + tabKey + `"}}}`, Tab, door, tabKey},
		{"the server's own", ` {"command": "` + door + `", "args": [], "env": {"VARIORUM_MCP_KEY": "` + tabKey + `"}, "type": "stdio"} `, Tab, door, tabKey},
	} {
		kind, s, err := Parse(tc.text)
		if err != nil || kind != tc.kind || s.Command != tc.command || s.Key != tc.key {
			t.Errorf("%s: kind %q, setup %q key %q, err %v", tc.name, kind, s.Command, s.Key, err)
		}
	}
}

// TestParseRefusesOtherShapes: anything but the two setups is refused with
// a sentence, and no refusal ever quotes the key.
func TestParseRefusesOtherShapes(t *testing.T) {
	secret := "vmk1_" + strings.Repeat("SECRET", 7) + "x"
	ok := claudeSetup("variorum", secret, door)
	for name, text := range map[string]string{
		"nothing":          " \n ",
		"words":            "my key is " + secret,
		"Codex's setup":    "codex mcp add variorum --env VARIORUM_MCP_KEY=" + secret + " -- " + shellQuoted(door),
		"another command":  "rm -rf ~; " + strings.TrimPrefix(ok, "claude mcp remove -s user variorum >/dev/null 2>&1; "),
		"no program":       "claude mcp add -s user variorum -e VARIORUM_MCP_KEY=" + secret + " --",
		"no --":            "claude mcp add -s user variorum -e VARIORUM_MCP_KEY=" + secret + " " + door,
		"arguments":        ok + " --flag " + secret,
		"no key":           "claude mcp add -s user variorum -- " + door,
		"two keys":         "claude mcp add -s user variorum -e VARIORUM_MCP_KEY=" + secret + " -e VARIORUM_MCP_KEY=" + secret + " -- " + door,
		"another variable": "claude mcp add -s user variorum -e VARIORUM_MCP_KEY=" + secret + " OTHER=" + secret + " -- " + door,
		"two names":        "claude mcp add -s user variorum other -e VARIORUM_MCP_KEY=" + secret + " -- " + door,
		"an option":        "claude mcp add -s user variorum --header X=" + secret + " -e VARIORUM_MCP_KEY=" + secret + " -- " + door,
		"an open quote":    "claude mcp add -s user variorum -e VARIORUM_MCP_KEY=" + secret + ` -- "` + door,
		"a spaced key":     "claude mcp add -s user variorum -e 'VARIORUM_MCP_KEY=" + secret + " " + secret + "' -- " + door,
		"a long key":       "claude mcp add -s user variorum -e VARIORUM_MCP_KEY=" + strings.Repeat(secret, 6) + " -- " + door,
		"the key hidden":   claudeSetup("variorum", "••••••••", door),
		"not JSON":         `{"mcpServers": {"variorum": {"command": "` + door + `", "env": {"VARIORUM_MCP_KEY": "` + secret + `"}}}`,
		"no command":       `{"mcpServers": {"variorum": {"env": {"VARIORUM_MCP_KEY": "` + secret + `"}}}}`,
		"JSON arguments":   `{"command": "` + door + `", "args": ["` + secret + `"], "env": {"VARIORUM_MCP_KEY": "` + secret + `"}}`,
		"JSON variables":   `{"command": "` + door + `", "env": {"VARIORUM_MCP_KEY": "` + secret + `", "OTHER": "` + secret + `"}}`,
		"JSON no key":      `{"command": "` + door + `", "env": {}}`,
		"another server":   `{"mcpServers": {"a": {"command": "` + door + `"}, "b": {"command": "` + door + `", "env": {"VARIORUM_MCP_KEY": "` + secret + `"}}}}`,
		"over HTTP":        `{"command": "` + door + `", "type": "http", "env": {"VARIORUM_MCP_KEY": "` + secret + `"}}`,
	} {
		kind, s, err := Parse(text)
		if err == nil {
			t.Errorf("%s: kept as %s: %v", name, kind, s)
			continue
		}
		if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "••") {
			t.Errorf("%s: the refusal quotes the key: %v", name, err)
		}
		if !strings.HasSuffix(err.Error(), ".") || !strings.Contains(err.Error(), " ") {
			t.Errorf("%s: %q isn't a sentence", name, err)
		}
	}
	if _, _, err := Parse(claudeSetup("variorum", "••••••••", door)); err != errHidden {
		t.Errorf("the key as Variorum shows it: %v", err)
	}
}

// TestASetupNeverPrintsItsKey: a setup printed by mistake names only its
// program.
func TestASetupNeverPrintsItsKey(t *testing.T) {
	s := Setup{Command: door, Key: sessionsKey}
	for _, f := range []string{"%v", "%+v", "%s"} {
		if out := fmt.Sprintf(f, s); strings.Contains(out, sessionsKey) || !strings.Contains(out, door) {
			t.Errorf("%s: %s", f, out)
		}
	}
}
