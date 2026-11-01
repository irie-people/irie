package core

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

func TestYourTurnReadsTheRequest(t *testing.T) {
	for _, c := range []struct {
		msg, want string
		ok        bool
	}{
		{"Learn by doing\n\n**Your Task**: In upload.js, write the `document` case at TODO(human).\n\nGuidance: …", "In upload.js, write the document case at TODO(human).", true},
		{"Your task: fill in TODO(human) in main.go", "fill in TODO(human) in main.go", true},
		{"I left a TODO(human) for you in main.go.", "write the part it left for you at TODO(human)", true},
		{"Done: the parser handles quotes.", "", false},
		{"Your Task: something, with no mark to write at.", "", false},
	} {
		got, ok := yourTurn(c.msg)
		if got != c.want || ok != c.ok {
			t.Errorf("yourTurn(%q) = %q, %v; want %q, %v", c.msg, got, ok, c.want, c.ok)
		}
	}
}

// TestTeachModeTurnIsYours spawns a jot in teach mode whose turn ends on a
// request to write a part (docs/proposals/learning.md §4.4): the session
// needs you, the jot dog lets it be, a message still goes in, and the mode
// lasts through a close and a reopen.
func TestTeachModeTurnIsYours(t *testing.T) {
	c, repo := dogSetup(t, "")
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,stop:turn", Repo: repo, Tutor: "teach"})
	row := c.waitState(s.ID, ipc.StateNeedsYou)
	if row.Reason != event.ReasonTurn || row.Detail != "Your turn: In notes.md, write the summary line under TODO(human)." || row.Tutor != "teach" {
		t.Errorf("row: reason %q, detail %q, tutor %q", row.Reason, row.Detail, row.Tutor)
	}
	if said := c.sentOn(s.ID); len(said) != 0 {
		t.Errorf("the dog sent it back: %q", said)
	}

	// The session runs in Claude Code's Learning style, with Irie's tips
	// under its spinner and the ask for diagrams appended.
	settings, err := os.ReadFile(filepath.Join(c.paths.Session(s.ID), "claude-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"outputStyle": "Learning"`, `"label": "Irie tip"`, `"id": "irie.park"`} {
		if !strings.Contains(string(settings), want) {
			t.Errorf("settings lack %s:\n%s", want, settings)
		}
	}
	argv := c.startArgv(s.ID)
	if i := slices.Index(argv, "--append-system-prompt"); i < 0 || i+1 >= len(argv) || argv[i+1] != harness.VisualNote {
		t.Errorf("argv = %q", argv)
	}
	if spec, err := host.ReadSpec(c.paths.SpecFile(s.ID)); err != nil || spec.Tutor != "teach" {
		t.Errorf("spec: %+v, %v", spec, err)
	}

	// It waits at its prompt, so your word that the part is written goes
	// in, and the turn is the agent's again.
	if err := c.call(ipc.OpSend, ipc.SendArgs{Session: s.ID, Text: "done"}, nil); err != nil {
		t.Fatalf("send in your turn: %v", err)
	}
	testutil.Eventually(t, 5*time.Second, "the agent never got the message", func() bool {
		for _, e := range c.events(s.ID) {
			if n, _ := e.Int("chars"); e.Kind == event.PromptSubmitted && n == len("done") {
				return true
			}
		}
		return false
	})

	c.closeSession(s.ID)
	back, err := c.reopen(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Tutor != "teach" {
		t.Errorf("reopened tutor = %q", back.Tutor)
	}
}

func TestTutorModeComesFromTheConfigUnlessTheTaskSays(t *testing.T) {
	c := setupWith(t, agentConfig{extra: "[learn]\ntutor = \"explain\"\nvisual = false\nagent_tips = false\n"})
	plain := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Name: "plain"})
	off := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Name: "off", Tutor: "off"})
	sub := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Name: "sub", Delegate: true, Parent: plain.ID})
	for _, c := range []struct {
		s    ipc.Session
		want string
	}{{plain, "explain"}, {off, ""}, {sub, ""}} {
		if c.s.Tutor != c.want {
			t.Errorf("%s: tutor %q, want %q", c.s.Name, c.s.Tutor, c.want)
		}
	}
	settings, err := os.ReadFile(filepath.Join(c.paths.Session(plain.ID), "claude-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), `"outputStyle": "Explanatory"`) || strings.Contains(string(settings), "spinnerTipsOverride") {
		t.Errorf("settings:\n%s", settings)
	}
	if slices.Contains(c.startArgv(plain.ID), "--append-system-prompt") {
		t.Error("visual is off, yet it asks for diagrams")
	}

	err = c.call(ipc.OpSpawn, ipc.SpawnArgs{Task: "prompt,stop", Tutor: "socratic"}, nil)
	var e *ipc.Error
	if !errors.As(err, &e) || e.Code != ipc.CodeBadArgs {
		t.Errorf("a tutor mode that is not one: %v", err)
	}
}

func TestThePlusListOffersTheTutorWords(t *testing.T) {
	c := setup(t)
	var r ipc.CompleteResult
	if err := c.call(ipc.OpComplete, ipc.CompleteArgs{Text: "+te"}, &r); err != nil {
		t.Fatal(err)
	}
	if i := slices.Index(r.Items, "+teach"); i < 0 || !strings.Contains(r.Notes[i], "write") {
		t.Errorf("items %q, notes %q", r.Items, r.Notes)
	}
	if err := c.call(ipc.OpComplete, ipc.CompleteArgs{Text: "+"}, &r); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.Items, "+explain") || !slices.Contains(r.Items, "+teach") {
		t.Errorf("items %q", r.Items)
	}
}

func TestLearnServesTheHelpMenu(t *testing.T) {
	c := setupWith(t, agentConfig{extra: "[learn]\nlinks = { \"Team handbook\" = \"https://example.com/handbook\" }\n"})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Cwd: dir})
	var r ipc.LearnResult
	if err := c.call(ipc.OpLearn, ipc.LearnArgs{Session: s.ID}, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Resources) < 3 || r.Resources[0].Title != "Team handbook" || !r.Resources[0].Yours || r.Resources[1].Lang != "go" || !r.Resources[1].Here {
		t.Errorf("resources start %+v", r.Resources[:min(3, len(r.Resources))])
	}
	if len(r.Glossary) == 0 {
		t.Error("no glossary")
	}
	if err := c.call(ipc.OpLearn, ipc.LearnArgs{Session: "s000000000000"}, &r); err == nil {
		t.Error("learn for no session")
	}
}

// startArgv is the command line the fake agent of session id started with.
func (c *coreEnv) startArgv(id string) []string {
	c.t.Helper()
	for _, l := range c.agentLog(id) {
		if l["t"] == "start" {
			raw, _ := l["argv"].([]any)
			out := make([]string, len(raw))
			for i, a := range raw {
				out[i], _ = a.(string)
			}
			return out
		}
	}
	c.t.Fatalf("%s never started", id)
	return nil
}
