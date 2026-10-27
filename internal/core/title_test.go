package core

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/testutil"
)

func TestTitle(t *testing.T) {
	for _, c := range []struct{ task, want string }{
		{"say hello\nand then wait", "Say Hello"},
		{"  fix the\tflaky   test\x07in CI  \n", "Fix the Flaky Test in CI"},
		{"ñandú facts", "Ñandú Facts"},
		// The @ and + tokens that may lead a task are not its title.
		{"@web! +fast fix the login bug", "Fix the Login Bug"},
		{"@web +fast\nfix the login bug", "Web-fast"},
		{"\n\n", "Task"},
		// 40 characters stay; more are cut at a word's end, with "…".
		{"abcd " + strings.Repeat("abcd ", 6) + "abcde", "Abcd " + strings.Repeat("Abcd ", 6) + "Abcde"},
		{"why are titles that are slugs so hard to read in the sidebar", "Why Are Titles That Are Slugs So Hard…"},
		{strings.Repeat("a", 39) + " b", "A" + strings.Repeat("a", 38) + "…"},
		{"a" + strings.Repeat("b", 50), "A" + strings.Repeat("b", 38) + "…"},
		{strings.Repeat("é", 45), "É" + strings.Repeat("é", 38) + "…"},
	} {
		got := title(c.task)
		if got != c.want {
			t.Errorf("title(%q) = %q, want %q", c.task, got, c.want)
		}
		if n := len([]rune(got)); n > maxTitle {
			t.Errorf("title(%q) has %d characters", c.task, n)
		}
	}
}

func TestTitleCase(t *testing.T) {
	for in, want := range map[string]string{
		"fix the build and run the tests":   "Fix the Build and Run the Tests",
		"what is this for":                  "What Is This For",
		"a plan for the week":               "A Plan for the Week",
		"notes on the plan: a draft for it": "Notes on the Plan: A Draft for It",
		"set up the server via ssh":         "Set Up the Server via Ssh",
		"fix (beta) builds, or don't":       "Fix (Beta) Builds, or Don't",
		// What has a capital, a digit or / . _ ` stays: acronyms, paths, code.
		"iOS build fails on launch":         "iOS Build Fails on Launch",
		"macOS: the dock icon is blurry":    "macOS: The Dock Icon Is Blurry",
		"update README for v2":              "Update README for v2",
		"read /etc/hosts and main.go":       "Read /etc/hosts and main.go",
		"rename snake_case names in `go`":   "Rename snake_case Names in `go`",
		"add the --verbose flag":            "Add the --verbose Flag",
		"2fa for the login page":            "2fa for the Login Page",
		"git branch naming with kebab-case": "Git Branch Naming With Kebab-case",
		"":                                  "",
	} {
		if got := titleCase(in); got != want {
			t.Errorf("titleCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNumberedOf(t *testing.T) {
	for name, want := range map[string]bool{
		"say-hello": true, "say-hello-2": true, "say-hello-12": true,
		"say-hello-1": false, "say-hello-02": false, "say-hello-x": false, "say-hello-world": false,
		"say-hello-2-2": false, "Say hello": false, "": false,
	} {
		if got := numberedOf(name, "say-hello"); got != want {
			t.Errorf("numberedOf(%q, say-hello) = %v, want %v", name, got, want)
		}
	}
	if !numberedOf("Phase-2-3", "Phase-2") || numberedOf("Phase", "Phase-2") {
		t.Error("a base that ends in a number")
	}
}

func aiTitle(t string) string {
	return transcriptLine(map[string]any{"type": "ai-title", "aiTitle": t, "sessionId": "6f1c0d2e-0000-4000-8000-000000000001"})
}

func customTitle(t string) string {
	return transcriptLine(map[string]any{"type": "custom-title", "customTitle": t, "sessionId": "6f1c0d2e-0000-4000-8000-000000000001"})
}

func TestTheTallyTakesEachNewTitleOnce(t *testing.T) {
	dir := t.TempDir()
	own := filepath.Join(dir, "session.jsonl")
	sub := filepath.Join(dir, "session", "subagents", "agent-a1.jsonl")
	var tally tokenTally
	read := func(lines ...string) (string, bool) {
		t.Helper()
		if len(lines) > 0 {
			appendFile(t, own, strings.Join(lines, ""))
		}
		tally.update([]string{own, sub})
		return tally.title([]string{own})
	}
	if tt, ok := read(); ok {
		t.Errorf("a title before the transcript exists: %q", tt)
	}
	// The newest of a read's titles; the usage on the same read still counts.
	if tt, ok := read(aiTitle("First"), reply("m1", 1, 1, 0, 0), aiTitle("Second")); !ok || tt != "Second" {
		t.Errorf("title = %q, %v; want Second", tt, ok)
	}
	if got, _ := tally.update([]string{own}); got.Output != 1 {
		t.Errorf("tokens = %+v", got)
	}
	// Taken once: nothing new, nothing to take.
	if tt, ok := read(userSays("u1", 1, "ai-title")); ok {
		t.Errorf("a title again: %q", tt)
	}
	// A name given in Claude Code outranks the titles before it.
	if tt, ok := read(aiTitle("Third"), customTitle("mine")); ok {
		t.Errorf("a title after a custom title: %q", tt)
	}
	if tt, ok := read(aiTitle("Fourth")); !ok || tt != "Fourth" {
		t.Errorf("title = %q, %v; want Fourth", tt, ok)
	}
	// A subagent's titles are not the session's.
	appendFile(t, sub, aiTitle("A subagent's"))
	if tt, ok := read(); ok {
		t.Errorf("a subagent's title: %q", tt)
	}
	// A transcript replaced is read again, to its newest title.
	if err := os.WriteFile(own, []byte(aiTitle("Fifth")), 0o600); err != nil {
		t.Fatal(err)
	}
	if tt, ok := read(); !ok || tt != "Fifth" {
		t.Errorf("after the transcript was replaced: %q, %v", tt, ok)
	}
}

func TestClosedRecordsKeepWhetherTheNameWasTheUsers(t *testing.T) {
	for _, c := range []struct {
		data       map[string]any
		named, old bool
	}{
		{map[string]any{"name": "Fix login", "named": true}, true, false},
		{map[string]any{"name": "Fix login", "named": false}, false, false},
		{map[string]any{"name": "fix-login"}, false, true},
	} {
		r := closedFrom(event.Event{Session: "s000000000001", Kind: event.UIClosed, Data: c.data})
		if r.Named != c.named || r.Old != c.old {
			t.Errorf("%v: named %v, old %v; want %v, %v", c.data, r.Named, r.Old, c.named, c.old)
		}
	}
}

// readUp appends lines to session id's transcript and waits until the core
// has read them: a reply after them adds an output token, so the row's
// output counts the replies the transcript holds.
func (c *coreEnv) readUp(id, lines string) ipc.Session {
	c.t.Helper()
	path := c.transcriptOf(id)
	b, _ := os.ReadFile(path)
	n := bytes.Count(b, []byte(`"id":"mark-`)) + 1
	appendFile(c.t, path, lines+reply(fmt.Sprintf("mark-%d", n), 0, 1, 0, 0))
	var row ipc.Session
	testutil.Eventually(c.t, 5*time.Second, "the core did not read the transcript", func() bool {
		r, ok := c.row(id)
		row = r
		return ok && r.Tokens != nil && r.Tokens.Output == int64(n)
	})
	return row
}

// A session given no name takes the title Claude Code gives it, numbered
// when another row has it; no title of a subagent's, and no title written
// again, changes it.
func TestASessionTakesTheTitleClaudeCodeGivesIt(t *testing.T) {
	c := setup(t)
	a := c.spawn("look into the flaky login test")
	c.waitState(a.ID, ipc.StateWorking)
	if a.Name != "Look Into the Flaky Login Test" {
		t.Errorf("name = %q, want the task's title", a.Name)
	}
	// In title case, and as tidy as a rename's.
	row := c.readUp(a.ID, aiTitle("Flaky  login\ttest"))
	if p := c.pane(row.Pane); row.Name != "Flaky Login Test" || p["@irie_name"] != row.Name || p["@irie_named"] != "0" {
		t.Errorf("row %q, pane %v; want the title, not pinned", row.Name, p)
	}
	// Nobody renamed it.
	if evs := c.renames(a.ID); len(evs) != 0 {
		t.Errorf("a title was journaled as a rename: %+v", evs)
	}

	b := c.spawn("prompt,sub:a1:tool:Read,stop")
	c.waitState(b.ID, ipc.StateDone)
	if row := c.readUp(b.ID, aiTitle("Flaky login test")); row.Name != "Flaky Login Test-2" {
		t.Errorf("a title another row has = %q, want Flaky Login Test-2", row.Name)
	}
	for range 2 {
		c.readUp(a.ID, aiTitle("Flaky login test"))
		c.readUp(b.ID, aiTitle("Flaky login test"))
	}
	ra, _ := c.row(a.ID)
	rb, _ := c.row(b.ID)
	if ra.Name != "Flaky Login Test" || rb.Name != "Flaky Login Test-2" {
		t.Errorf("after the titles came again: %q and %q", ra.Name, rb.Name)
	}
	var sub string
	for _, e := range c.events(b.ID) {
		if e.Kind == event.SubagentFinished {
			sub = e.Str("transcript")
		}
	}
	if sub == "" {
		t.Fatal("no subagent transcript")
	}
	appendFile(t, sub, aiTitle("Reading the file"))
	if row := c.readUp(b.ID, ""); row.Name != "Flaky Login Test-2" {
		t.Errorf("a subagent's title named the session %q", row.Name)
	}

	// A new title renames it; a restarted core reads the transcripts again
	// and comes to the same names.
	if row := c.readUp(a.ID, aiTitle("Fix the flaky login test")); row.Name != "Fix the Flaky Login Test" {
		t.Errorf("a newer title = %q", row.Name)
	}
	c.restartCore()
	ra, _ = c.row(a.ID)
	rb, _ = c.row(b.ID)
	if ra.Name != "Fix the Flaky Login Test" || rb.Name != "Flaky Login Test-2" || c.pane(ra.Pane)["@irie_named"] != "0" {
		t.Errorf("after a restart: %q and %q", ra.Name, rb.Name)
	}
}

// A name the user chose, at spawn or by rename, is shown as given, goes to
// Claude Code, and outlasts its titles, a restart and a close and reopen.
func TestANameTheUserChoseIsPinned(t *testing.T) {
	c := setupWith(t, agentConfig{readyTimeout: "1s"})
	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: " Docs\tguide #1 "})
	c.waitState(a.ID, ipc.StateWorking)
	if a.Name != "Docs guide #1" || c.pane(a.Pane)["@irie_named"] != "1" {
		t.Errorf("explicit name = %q, pane %v", a.Name, c.pane(a.Pane))
	}
	if runs := c.runs(a.ID); len(runs) != 1 || !slices.Contains(runs[0].argv, "--name") || runs[0].argv[slices.Index(runs[0].argv, "--name")+1] != a.Name {
		t.Errorf("runs = %+v, want --name %q", runs, a.Name)
	}
	// Its slug names its folder.
	if !strings.HasPrefix(a.Cwd, c.paths.ScratchDir("docs-guide-1")) {
		t.Errorf("cwd = %q", a.Cwd)
	}
	if row := c.readUp(a.ID, aiTitle("Writing the guide")); row.Name != a.Name {
		t.Errorf("a title renamed an explicit name to %q", row.Name)
	}

	b := c.spawn("prompt")
	c.waitState(b.ID, ipc.StateWorking)
	if _, err := c.rename(b.ID, "Login fix"); err != nil {
		t.Fatal(err)
	}
	if row := c.readUp(b.ID, aiTitle("Something else")); row.Name != "Login fix" || c.pane(row.Pane)["@irie_named"] != "1" {
		t.Errorf("after a title: %q, pane %v", row.Name, c.pane(row.Pane))
	}
	c.restartCore()
	if row := c.readUp(b.ID, aiTitle("Something newer")); row.Name != "Login fix" {
		t.Errorf("after a restart and a title: %q", row.Name)
	}

	c.closeSession(b.ID)
	c.waitGone(b.Pane, 3*time.Second)
	s, err := c.reopen(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Login fix" || c.pane(s.Pane)["@irie_named"] != "1" {
		t.Errorf("reopened as %q, pane %v", s.Name, c.pane(s.Pane))
	}
	c.waitState(b.ID, ipc.StateIdle)
	if row := c.readUp(b.ID, aiTitle("Something newer still")); row.Name != "Login fix" {
		t.Errorf("after a reopen and a title: %q", row.Name)
	}
	// It carried its conversation on, under the name Claude Code has for
	// it: no --name, as at its spawn.
	if runs := c.runs(b.ID); len(runs) != 2 || slices.Contains(runs[0].argv, "--name") || slices.Contains(runs[1].argv, "--name") {
		t.Errorf("runs = %+v, want no --name", runs)
	}
}

// Sessions from before titles have no @irie_named. A restarted core
// settles each once: a name made from its task's first words becomes the
// task's title, and any other is kept as the user's.
func TestSessionsFromBeforeTitlesAreSettledOnce(t *testing.T) {
	c := setup(t)
	a := c.spawn("say hello")
	b := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "docs-guide"})
	d := c.spawn("say hello")
	for _, s := range []ipc.Session{a, b, d} {
		c.waitState(s.ID, ipc.StateWorking)
	}
	for pane, name := range map[string]string{a.Pane: "say-hello", b.Pane: "docs-guide", d.Pane: "say-hello-2"} {
		c.Tmux(t, "set-option", "-p", "-t", pane, "@irie_name", name)
		c.Tmux(t, "set-option", "-p", "-u", "-t", pane, "@irie_named")
	}
	check := func(when string) {
		t.Helper()
		ra, _ := c.row(a.ID)
		rb, _ := c.row(b.ID)
		rd, _ := c.row(d.ID)
		names := []string{ra.Name, rd.Name}
		slices.Sort(names)
		if !slices.Equal(names, []string{"Say Hello", "Say Hello-2"}) || rb.Name != "docs-guide" {
			t.Errorf("%s: names %q and %q", when, names, rb.Name)
		}
		for _, r := range []ipc.Session{ra, rb, rd} {
			want := map[bool]string{true: "1", false: "0"}[r.ID == b.ID]
			if p := c.pane(r.Pane); p["@irie_name"] != r.Name || p["@irie_named"] != want {
				t.Errorf("%s: %s's pane %v, want the name and @irie_named %s", when, r.Name, p, want)
			}
		}
	}
	c.restartCore()
	check("after the first restart")
	c.restartCore()
	check("after the second restart")
}
