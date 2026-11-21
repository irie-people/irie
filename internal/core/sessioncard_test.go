package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/genui"
	"github.com/irie-people/irie/internal/ipc"
)

func TestASessionsCardReadsItsRowAndItsSteps(t *testing.T) {
	row := ipc.Session{Name: "Fix the parser", State: "working", Detail: "Running the tests", Branch: "irie/fix-the-parser"}
	conv := ipc.Conversation{Items: []ipc.Item{
		{Kind: ipc.ItemUser, Text: "fix the parser"},
		{Kind: ipc.ItemTool, Phrase: "Edited parse.go", Detail: "internal/parse.go", Added: 14, Removed: 3},
		{Kind: ipc.ItemTool, Phrase: "Ran the tests", Detail: "go test ./...", State: ipc.ItemError},
		{Kind: ipc.ItemText, Text: "One test fails:\n  TestQuote."},
		{Kind: ipc.ItemNeeds, Text: "a need is told once, below"},
	}, Needs: &ipc.Item{Text: "Allow git push?"}}
	got := sessionContext(row, conv)
	for _, want := range []string{
		"Session: Fix the parser\nState: working, Running the tests\nBranch: irie/fix-the-parser\n",
		"Person: fix the parser\n",
		"Step: Edited parse.go — internal/parse.go (+14 −3)\n",
		"Step: Ran the tests — go test ./... (failed)\n",
		"Agent: One test fails: TestQuote.\n",
		"Waits on the person: Allow git push?",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the context lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "told once") {
		t.Error("a needs item was read twice")
	}
}

func TestASessionsCardKeepsItsNewestSteps(t *testing.T) {
	var many []ipc.Item
	for i := range 2000 {
		many = append(many, ipc.Item{Kind: ipc.ItemText, Text: fmt.Sprintf("step %04d %s", i, strings.Repeat("x", 40))})
	}
	got := sessionContext(ipc.Session{Name: "Long", State: "working"}, ipc.Conversation{Items: many})
	if len(got) > genui.MaxContext || !strings.Contains(got, "step 1999") || strings.Contains(got, "step 0000 ") || !strings.Contains(got, leftOut) {
		t.Errorf("%d bytes, newest kept %v, oldest kept %v, said so %v; want the newest within %d bytes",
			len(got), strings.Contains(got, "step 1999"), strings.Contains(got, "step 0000 "), strings.Contains(got, leftOut), genui.MaxContext)
	}
}

func TestSessionCardsAreOffUntilTurnedOn(t *testing.T) {
	c := cardCore(t, "http://127.0.0.1:1")
	if _, err := c.materializeOp(ipc.MaterializeArgs{Session: "s1"}); err == nil || !strings.Contains(err.Error(), "materialize.sessions") {
		t.Errorf("off: %v; want it refused, naming the key", err)
	}
	cfg := config.Default()
	cfg.Materialize.Sessions = true
	c = bareCore(t, cfg)
	if _, err := c.materializeOp(ipc.MaterializeArgs{Session: "nobody"}); err == nil {
		t.Error("a card was asked for a session that is not there, and given")
	}
}
