package habits

import (
	"reflect"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/event"
)

// TestFailuresRankWhatFailedAndWhatWorkedNext: the same mistake counts as one
// kind across files and sessions, with what the agent did next that worked;
// a failure with no error said is counted, not ranked.
func TestFailuresRankWhatFailedAndWhatWorkedNext(t *testing.T) {
	const notRead = "File has not been read yet. Read it first before writing to it."
	cs := []Call{
		{Session: "s1", Agent: "main", Tool: "Edit", Family: "Edit", Failed: true, Error: notRead},
		{Session: "s1", Agent: "main", Tool: "Read", Family: "Read"},
		{Session: "s1", Agent: "main", Tool: "Edit", Family: "Edit"},
		{Session: "s2", Agent: "main", Tool: "Edit", Family: "Edit", Failed: true, Error: notRead},
		{Session: "s2", Agent: "main", Tool: "Edit", Family: "Edit", Failed: true, Error: notRead},
		{Session: "s2", Agent: "main", Tool: "Read", Family: "Read"},
		{Session: "s2", Agent: "main", Tool: "Bash", Family: "python", Cmd: "python x.py", Failed: true, Error: "exit 127: zsh: command not found: python"},
		{Session: "s2", Agent: "main", Tool: "Bash", Family: "python3", Cmd: "python3 x.py"},
		{Session: "s2", Agent: "main", Tool: "Bash", Family: "go test", Cmd: "go test ./...", Failed: true},
	}
	r := Build(cs, 0)
	if r.Failed != 5 {
		t.Errorf("failed = %d, want 5", r.Failed)
	}
	want := []Failure{
		{Kind: "Edit: " + notRead, Calls: 3, Sessions: 2, Example: notRead, Then: "Read"},
		{Kind: "python: exit N: zsh: command not found: python", Calls: 1, Sessions: 1, Example: "exit 127: zsh: command not found: python", Then: "python3"},
	}
	if !reflect.DeepEqual(r.Failures, want) {
		t.Errorf("failures =\n %+v\nwant\n %+v", r.Failures, want)
	}
	var b strings.Builder
	Print(&b, r)
	if !strings.Contains(b.String(), "Calls that failed: 5") || !strings.Contains(b.String(), "then: python3") {
		t.Errorf("the printed report:\n%s", b.String())
	}
}

// TestFailureKindLeavesOutWhatChanges: paths, numbers and quoted words are
// left out, so the same error on other files is one kind.
func TestFailureKindLeavesOutWhatChanges(t *testing.T) {
	a := FailureKind(Call{Family: "cat", Error: "exit 1: cat: /home/alice/a.txt: No such file or directory"})
	b := FailureKind(Call{Family: "cat", Error: "exit 2: cat: ../b/c.go: No such file or directory"})
	if a != b || a != "cat: exit N: cat: PATH: No such file or directory" {
		t.Errorf("kinds %q and %q", a, b)
	}
	if got := FailureKind(Call{Family: "Edit", Error: `String to replace not found in file. String: "x := 1"`}); got != "Edit: String to replace not found in file. String: '…'" {
		t.Errorf("quoted: %q", got)
	}
}

// TestCallsKeepWhatTheErrorSaid: a failed call carries the journal's error.
func TestCallsKeepWhatTheErrorSaid(t *testing.T) {
	v := &event.Vendor{ToolUseID: "toolu_1"}
	cs := Calls([]event.Event{
		{Kind: event.ToolStarted, Session: "s1", Agent: "main", Vendor: v, Data: map[string]any{"tool": "Edit"}},
		{Kind: event.ToolFailed, Session: "s1", Agent: "main", Vendor: v, Data: map[string]any{"tool": "Edit", "error": "File has not been read yet."}},
	})
	if len(cs) != 1 || !cs[0].Failed || cs[0].Error != "File has not been read yet." {
		t.Errorf("calls = %+v", cs)
	}
}
