package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

func (c *coreEnv) input(a ipc.InputArgs) (ipc.InputResult, error) {
	var r ipc.InputResult
	err := c.call(ipc.OpInput, a, &r)
	return r, err
}

// What a person types runs through the same ops a client sends, so a typed
// start, rename or park is a step of the history like any other, and the
// reply says what happened in words to show as they are.
func TestInputRunsWhatWasTyped(t *testing.T) {
	c := setup(t)
	r, err := c.input(ipc.InputArgs{Text: "task a"})
	if err != nil || r.Did != ipc.InputSpawned || r.Op != ipc.OpSpawn || r.Session == nil || !strings.HasPrefix(r.Text, "Started ") {
		t.Fatalf("a task: %+v, %v", r, err)
	}
	a := c.waitState(r.Session.ID, ipc.StateWorking)

	if r, err := c.input(ipc.InputArgs{Text: "hows task a?"}); err != nil || r.Did != ipc.InputAnswered || !strings.HasPrefix(r.Text, "Task A: working") {
		t.Errorf("hows: %+v, %v", r, err)
	}
	if r, err := c.input(ipc.InputArgs{Text: ":rename Login fix", Session: a.ID}); err != nil || r.Session == nil || r.Session.Name != "Login fix" || r.Text != "Renamed Task A to Login fix." {
		t.Errorf(":rename: %+v, %v", r, err)
	}
	if r, err := c.input(ipc.InputArgs{Text: ":park", Session: a.ID}); err != nil || r.Text != "Parked Login fix." {
		t.Errorf(":park: %+v, %v", r, err)
	}
	if row, _ := c.row(a.ID); !row.Parked {
		t.Errorf("the row is not parked: %+v", row)
	}
	// A second :park reads the parked row and refuses, and runs nothing.
	if _, err := c.input(ipc.InputArgs{Text: ":park", Session: a.ID}); code(err) != ipc.CodeBadArgs {
		t.Errorf("a second :park: %v", err)
	}
	// The typed park is the history's last step, as a click's would be.
	if r, err := c.input(ipc.InputArgs{Text: ":undo"}); err != nil || r.Op != ipc.OpUndo || !strings.HasPrefix(r.Text, "Undid ") {
		t.Errorf(":undo: %+v, %v", r, err)
	}
	testutil.Eventually(t, 5*time.Second, "undo did not unpark it", func() bool {
		row, _ := c.row(a.ID)
		return !row.Parked
	})
	if _, err := c.input(ipc.InputArgs{Text: ":bogus"}); code(err) != ipc.CodeBadArgs || !strings.Contains(err.Error(), ":undo, :redo") {
		t.Errorf(":bogus: %v", err)
	}

	// hello names the op, so a client knows this core reads its typing.
	var h ipc.HelloResult
	if err := c.call(ipc.OpHello, ipc.HelloArgs{Client: "test", API: ipc.API}, &h); err != nil || !strings.Contains(strings.Join(h.Features, " "), ipc.OpInput) {
		t.Errorf("hello features %v, %v", h.Features, err)
	}
}

// A + in what was typed picks a profile only where it names one of the
// core's and stands apart from the task's words; any other + is the
// task's, as typed (DESIGN §4.2, "Plus words"). A jot such as "bind fn+x +
// fn+l" once came back refused, "+ needs a profile name", and never
// started.
func TestInputReadsPlusWordsAgainstTheProfiles(t *testing.T) {
	fake := testutil.BuildFakeAgent(t)
	c := setupWith(t, agentConfig{extra: fmt.Sprintf("[profiles.other]\ncommand = %q\n", fake)})
	for _, k := range []struct{ text, task, profile string }{
		{"bind fn+x + fn+l + fn+r,  as emacs does", "bind fn+x + fn+l + fn+r,  as emacs does", "fake"},
		{"fix the login page +Other.", "fix the login page", "other"},
		{"why does +other stall on long tasks", "why does +other stall on long tasks", "fake"},
	} {
		r, err := c.input(ipc.InputArgs{Text: k.text})
		if err != nil || r.Did != ipc.InputSpawned || r.Session == nil {
			t.Fatalf("%q: %+v, %v", k.text, r, err)
		}
		if r.Session.Profile != k.profile {
			t.Errorf("%q runs on +%s, want +%s", k.text, r.Session.Profile, k.profile)
		}
		if b, err := os.ReadFile(filepath.Join(c.paths.Session(r.Session.ID), "task.md")); err != nil || string(b) != k.task {
			t.Errorf("%q: task.md = %q, %v; want %q", k.text, b, err, k.task)
		}
	}
	if _, err := c.input(ipc.InputArgs{Text: "+othre fix it"}); code(err) != ipc.CodeBadArgs || !strings.Contains(err.Error(), "Did you mean +other?") {
		t.Errorf("a slip from +other: %v", err)
	}
}
