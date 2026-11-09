package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The MCP server itself is internal/mcp's, and its tests are there.

func TestToolsAreWellFormed(t *testing.T) {
	b := New(Options{})
	seen := map[string]bool{}
	for _, tl := range b.tools() {
		if seen[tl.Name] || tl.Description == "" || tl.Call == nil || tl.InputSchema["type"] != "object" {
			t.Errorf("tool %q is not well formed", tl.Name)
		}
		seen[tl.Name] = true
		required, _ := tl.InputSchema["required"].([]string)
		for _, req := range required {
			if _, ok := tl.InputSchema["properties"].(map[string]any)[req]; !ok {
				t.Errorf("%s requires %s, which it does not define", tl.Name, req)
			}
		}
	}
	// Claude Code cuts server instructions at 4,096 characters.
	if n := len(b.instructions()); n > 2000 {
		t.Errorf("the instructions take %d characters", n)
	}
}

func TestCallsOnAPageNeedAnOpenBrowser(t *testing.T) {
	b := New(Options{})
	for _, name := range []string{"read", "links", "click", "screenshot", "back", "eval", "tabs"} {
		args := `{"n":1,"text":"x","script":"1"}`
		i := slicesIndex(b.tools(), name)
		r := b.tools()[i].Call(context.Background(), json.RawMessage(args))
		if !r.IsError || !strings.Contains(r.Content[0].Text, "nothing is open yet") {
			t.Errorf("%s with no browser: %+v", name, r)
		}
	}
}

func slicesIndex(ts []tool, name string) int {
	for i, t := range ts {
		if t.Name == name {
			return i
		}
	}
	return -1
}
