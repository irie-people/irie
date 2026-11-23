package harness

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskArg: a task goes to the agent as its argument when it can, after
// a space when it starts like a flag, and through the task file when it is
// too long for an argument or holds a NUL byte.
func TestTaskArg(t *testing.T) {
	dir := "/home/alice/state/sessions/s1"
	file := "Your full task is in " + filepath.Join(dir, TaskFile) + ". Read all of it before doing anything else."
	for task, want := range map[string]string{
		"fix the parser":                 "fix the parser",
		"-v is broken":                   " -v is broken",
		strings.Repeat("a", InlineMax):   strings.Repeat("a", InlineMax),
		strings.Repeat("a", InlineMax+1): file,
		"a\x00b":                         file,
	} {
		if got := TaskArg(task, dir); got != want {
			t.Errorf("TaskArg(%.20q) = %.40q, want %.40q", task, got, want)
		}
	}
}

// TestMCPServerJSON: a stdio server is written as its command and
// arguments alone, as Claude Code's mcpServers config has always taken
// them, and a server of another type, such as the shared browser server
// over HTTP (DESIGN §4.15), with its type, address, headers and timeout.
func TestMCPServerJSON(t *testing.T) {
	for _, c := range []struct {
		s    MCPServer
		want string
	}{
		{MCPServer{Command: "/home/alice/bin/tool", Args: []string{"serve"}}, `{"command":"/home/alice/bin/tool","args":["serve"]}`},
		{
			MCPServer{Type: MCPHTTP, URL: "http://127.0.0.1:7440/mcp", Headers: map[string]string{"Authorization": "Bearer t"}, Timeout: 600000},
			`{"type":"http","url":"http://127.0.0.1:7440/mcp","headers":{"Authorization":"Bearer t"},"timeout":600000}`,
		},
	} {
		if b, err := json.Marshal(c.s); err != nil || string(b) != c.want {
			t.Errorf("%+v is written %s, %v; want %s", c.s, b, err, c.want)
		}
	}
}
