package core

import (
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/proc"
)

// A session's tree, as the process table has it: the agent its host runs,
// Irie's own helpers the agent runs, with what they start, and its tools.
func TestTheAgentAndIriesHelpersKeepUpWhileItsToolsStepBack(t *testing.T) {
	const host = 100
	ps := []proc.Proc{
		{PID: 1, PPID: 0, Name: "launchd"},
		{PID: 90, PPID: 1, Name: "tmux"},
		{PID: host, PPID: 90, Name: "irie"},
		{PID: 101, PPID: host, Name: "claude"},
		{PID: 102, PPID: 101, Name: "irie"},      // the browser connector
		{PID: 103, PPID: 102, Name: "firefox"},   // the browser it drives
		{PID: 104, PPID: 103, Name: "plugin-co"}, // and a page's process
		{PID: 105, PPID: 101, Name: "irie"},      // a hook call
		{PID: 106, PPID: 101, Name: "zsh"},       // a Bash tool call
		{PID: 107, PPID: 106, Name: "go"},
		{PID: 108, PPID: 107, Name: "compile"},
		{PID: 109, PPID: 106, Name: "irie"}, // irie ctl from the tool: a tool too
		{PID: 110, PPID: 101, Name: "caffeinate"},
		{PID: 111, PPID: 101, Name: "node"}, // another MCP server
		{PID: 112, PPID: 111, Name: "irie"},
	}
	tb := proc.NewTable(ps)
	up := map[int]bool{101: true, 102: true, 103: true, 104: true, 105: true}
	seen := 0
	for _, q := range tb.Tree(host) {
		seen++
		if got := keepsUp(tb, host, "irie", q); got != up[q.PID] {
			t.Errorf("%d %s: keeps up = %v, want %v", q.PID, q.Name, got, up[q.PID])
		}
	}
	if seen != 12 {
		t.Errorf("the tree has %d processes, want 12", seen)
	}
	// A process whose parent left the table, as one may between two reads,
	// steps back like any tool.
	if keepsUp(tb, host, "irie", proc.Proc{PID: 200, PPID: 199, Name: "irie"}) {
		t.Error("a process with no parent in the table keeps up")
	}
}

func TestOwnNameIsWhatTheProcessTableCallsIrie(t *testing.T) {
	for _, tc := range []struct{ bin, want string }{
		{"/home/alice/.local/share/irie/bin/irie", "irie"},
		{"", brand.Slug},
		{"/home/alice/bin/a-very-long-build-of-irie", "a-very-long-buil"},
	} {
		if got := (&Core{bin: tc.bin}).ownName(); got != tc.want {
			t.Errorf("ownName(%q) = %q, want %q", tc.bin, got, tc.want)
		}
	}
}
