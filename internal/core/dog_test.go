package core

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/testutil"
)

// dogSetup starts Irie with a repo, website, whose work lands in its branch
// live (DESIGN §4.8).
func dogSetup(t *testing.T, dog string) (*coreEnv, string) {
	t.Helper()
	testutil.IsolateGit(t)
	repo := website(t, filepath.Join(t.TempDir(), "website"))
	testutil.Git(t, repo, "branch", "live")
	c := setupWith(t, agentConfig{extra: "[repos.aliases]\nweb = " + strconv.Quote(repo) + "\n\n[dog]\nland = { website = \"live\" }\n" + dog})
	return c, repo
}

// sentOn returns what the dog said to the fake agent each time it sent a
// turn on.
func (c *coreEnv) sentOn(id string) []string {
	var out []string
	for _, l := range c.agentLog(id) {
		if l["t"] == "continued" {
			s, _ := l["context"].(string)
			out = append(out, s)
		}
	}
	return out
}

// dog asks the core for its report.
func (c *coreEnv) dog() []ipc.Jot {
	c.t.Helper()
	var r ipc.DogResult
	if err := c.call(ipc.OpDog, nil, &r); err != nil {
		c.t.Fatalf("dog: %v", err)
	}
	return r.Jots
}

func TestTheDogSendsAJotBackWhileItsWorkIsNotCommitted(t *testing.T) {
	c, repo := dogSetup(t, "")
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,stop", Repo: repo})
	row := c.waitState(s.ID, ipc.StateDone)

	// The agent was sent back once, told what is left, and then let stop.
	said := c.sentOn(s.ID)
	if len(said) != 1 || !strings.HasPrefix(said[0], "[Irie] The jot dog: this task is not through yet.") ||
		!strings.Contains(said[0], "Not committed in "+s.Worktree+" (branch "+s.Branch+"): notes.md.") ||
		strings.Contains(said[0], ".claude") {
		t.Errorf("the dog said %q", said)
	}
	// The nudge stands in for the first stop; the second ends the turn.
	var kinds []string
	for _, e := range c.events(s.ID) {
		if e.Kind == event.DogNudged || e.Kind == event.TurnEnded {
			kinds = append(kinds, e.Kind)
			if e.Kind == event.DogNudged && (e.Source != event.SourceIrie || e.Data["dirty"] != float64(1) || e.Data["land"] != "live") {
				t.Errorf("nudge = %+v", e)
			}
		}
	}
	if !slices.Equal(kinds, []string{event.DogNudged, event.TurnEnded}) {
		t.Errorf("events = %v", kinds)
	}
	// Its row says what is left.
	if l := row.Loose; l == nil || l.Dirty != 1 || l.Ahead != 0 || l.Nudged != 1 || len(l.Where) != 1 || l.Where[0].Path != s.Worktree {
		t.Errorf("loose = %+v", row.Loose)
	}
	if row.Detail != "Not through: 1 file not committed" {
		t.Errorf("detail = %q", row.Detail)
	}
}

func TestTheDogWatchesTheJotUntilItsWorkLands(t *testing.T) {
	c, repo := dogSetup(t, "")
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,commit,stop", Repo: repo})
	row := c.waitState(s.ID, ipc.StateDone)
	said := c.sentOn(s.ID)
	if len(said) != 1 || !strings.Contains(said[0], "Not in live yet, from "+s.Worktree) || !strings.Contains(said[0], " Fake work.") {
		t.Errorf("the dog said %q", said)
	}
	if row.Detail != "Not through: 1 commit not in live" || row.Loose == nil || row.Loose.Ahead != 1 || row.Loose.Land != "live" {
		t.Errorf("row = %q, %+v", row.Detail, row.Loose)
	}
	if jots := c.dog(); len(jots) != 1 || jots[0].ID != s.ID || jots[0].State != ipc.StateDone || jots[0].Loose.Ahead != 1 {
		t.Errorf("dog = %+v", jots)
	}

	// It lands, however it lands: the row clears at the dog's next look.
	testutil.Git(t, repo, "branch", "-f", "live", s.Branch)
	if jots := c.dog(); len(jots) != 0 {
		t.Errorf("dog after it landed = %+v", jots)
	}
	if row, _ := c.row(s.ID); row.Loose != nil || row.Detail != "" {
		t.Errorf("row after it landed = %q, %+v", row.Detail, row.Loose)
	}
}

func TestTheDogLeavesAJotThatWaits(t *testing.T) {
	c, repo := dogSetup(t, "")
	// Its own work in the background will wake it.
	bg := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,stop:bg", Repo: repo, Name: "bg"})
	// It asks the user something.
	ask := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,stop:ask", Repo: repo, Name: "ask"})
	// Its work is done and through.
	clean := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Repo: repo, Name: "clean"})
	for _, s := range []ipc.Session{bg, ask, clean} {
		c.waitState(s.ID, ipc.StateDone)
		if said := c.sentOn(s.ID); len(said) != 0 {
			t.Errorf("%s: the dog said %q", s.Name, said)
		}
	}
	if row, _ := c.row(bg.ID); row.Detail != "Not through: 1 file not committed · Waiting on its work in the background · Install the build" {
		t.Errorf("bg: %q", row.Detail)
	}
	if row, _ := c.row(ask.ID); row.Loose == nil || row.Loose.Dirty != 1 {
		t.Errorf("ask: %+v", row.Loose)
	}
	if row, _ := c.row(clean.ID); row.Loose != nil || row.Detail != "" {
		t.Errorf("clean: %q, %+v", row.Detail, row.Loose)
	}
}

func TestNudgesZeroOnlyWatches(t *testing.T) {
	c, repo := dogSetup(t, "nudges = 0\n")
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,stop", Repo: repo})
	row := c.waitState(s.ID, ipc.StateDone)
	if said := c.sentOn(s.ID); len(said) != 0 {
		t.Errorf("the dog said %q", said)
	}
	if row.Loose == nil || row.Loose.Dirty != 1 {
		t.Errorf("loose = %+v", row.Loose)
	}
}

func TestTheDogFindsAScratchJotsOwnWorktree(t *testing.T) {
	c, repo := dogSetup(t, "")
	// A task that names no repo runs in a scratch folder, and its agent
	// makes a worktree for itself under a folder named for its
	// conversation, as Claude Code's scratchpad is.
	s := c.spawn("prompt,stop")
	c.waitState(s.ID, ipc.StateDone)
	if jots := c.dog(); len(jots) != 0 {
		t.Fatalf("dog before any work = %+v", jots)
	}
	pad := filepath.Join(t.TempDir(), s.VendorID, "scratchpad")
	if err := os.MkdirAll(pad, 0o700); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(pad, "fix")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "fix", tree)
	testutil.WriteFiles(t, tree, map[string]string{"fix.md": "fixed\n"})
	// A worktree of another conversation is not its.
	other := filepath.Join(t.TempDir(), "11111111-2222-4333-8444-555555555555", "scratchpad", "other")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "other", other)
	testutil.WriteFiles(t, other, map[string]string{"other.md": "not mine\n"})

	jots := c.dog()
	if len(jots) != 1 || jots[0].ID != s.ID || jots[0].Loose.Dirty != 1 || len(jots[0].Loose.Where) != 1 ||
		!sameDir(jots[0].Loose.Where[0].Path, tree) || jots[0].Loose.Where[0].Branch != "fix" {
		t.Fatalf("dog = %+v", jots)
	}
	if row, _ := c.row(s.ID); row.Detail != "Not through: 1 file not committed" {
		t.Errorf("row = %q", row.Detail)
	}

	// Closed, it is still on the dog's list for a day.
	c.closeSession(s.ID)
	jots = c.dog()
	if len(jots) != 1 || jots[0].ID != s.ID || jots[0].State != "closed" || jots[0].Closed == nil || jots[0].Loose.Dirty != 1 {
		t.Errorf("dog after close = %+v", jots)
	}
}

func TestADelegatedTaskIsNotAJot(t *testing.T) {
	c, repo := dogSetup(t, "")
	parent := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: repo, Name: "parent"})
	c.waitState(parent.ID, ipc.StateWorking)
	// What a subtask leaves is for the session that delegated it to take
	// on, even in a worktree of its own.
	sub := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:sub.md,stop", Repo: repo, Delegate: true, Parent: parent.ID, Name: "sub"})
	row := c.waitState(sub.ID, ipc.StateDone)
	if said := c.sentOn(sub.ID); len(said) != 0 || row.Loose != nil {
		t.Errorf("the dog watched a delegated task: %q, %+v", said, row.Loose)
	}
}

func TestAsksUser(t *testing.T) {
	for msg, want := range map[string]bool{
		"Shall I land it?":                 true,
		"Which one do you want?**\n":       true,
		"Want me to run it? (`live.sh`)":   false,
		"(Shall I?)":                       true,
		"Done. Committed and installed.":   false,
		"Is it right? I committed it.":     false,
		"":                                 false,
		"Say so and I'll do it.":           false,
		"Ready when you are — shall I go?": true,
	} {
		if got := asksUser(msg); got != want {
			t.Errorf("asksUser(%q) = %v, want %v", msg, got, want)
		}
	}
}

func TestLandOfFindsTheRepoByPathAliasOrName(t *testing.T) {
	home, _ := os.UserHomeDir()
	c := &Core{}
	c.useConfig(config.Default())
	c.cfg().Repos.Aliases = map[string]string{"site": "~/code/website"}
	c.cfg().Dog.Land = map[string]string{"site": "release", "api": "main", "~/code/irie": "live"}
	for r, want := range map[string]string{
		filepath.Join(home, "code/website"): "release",
		"/home/alice/src/api":               "main",
		filepath.Join(home, "code/irie"):    "live",
		"/home/alice/src/other":             "",
		"":                                  "",
	} {
		if got := c.landOf(r); got != want {
			t.Errorf("landOf(%q) = %q, want %q", r, got, want)
		}
	}
}
