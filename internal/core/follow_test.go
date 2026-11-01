package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// jot sends a as the task bars send a jot, and fails the test when that
// fails.
func (c *coreEnv) jot(a ipc.SpawnArgs) ipc.SpawnResult {
	c.t.Helper()
	a.Jot = true
	var r ipc.SpawnResult
	if err := c.call("spawn", a, &r); err != nil {
		c.t.Fatalf("jot %q: %v", a.Task, err)
	}
	return r
}

// prompted waits until session id's agent has had text typed to it, and
// returns that prompt's event.
func (c *coreEnv) prompted(id, text string) event.Event {
	c.t.Helper()
	var got event.Event
	testutil.Eventually(c.t, 15*time.Second, "the agent never got “"+text+"”", func() bool {
		for _, e := range c.events(id) {
			if n, _ := e.Int("chars"); e.Kind == event.PromptSubmitted && e.Source == event.SourceHook && n == utf8.RuneCountInString(text) {
				got = e
				return true
			}
		}
		return false
	})
	return got
}

// A jot adds to the one before only when it reads as more for it, soon
// after, in the same place and for the same agent, while that one is still
// starting or its agent is still there.
func TestAJotAddsOnlyToARecentLiveJotInTheSamePlace(t *testing.T) {
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	over := make(chan struct{})
	close(over)
	before := ipc.SpawnArgs{Task: "fix the footer", Repo: "/home/alice/code/web", Jot: true}
	for _, tc := range []struct {
		name   string
		args   ipc.SpawnArgs
		after  time.Duration
		change func(c *Core, last *lastJot, x *session)
		want   bool
	}{
		{"more for it", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo}, 30 * time.Second, nil, true},
		{"a task of its own", ipc.SpawnArgs{Task: "deep research lovable", Repo: before.Repo}, 30 * time.Second, nil, false},
		{"another repo", ipc.SpawnArgs{Task: "also the header", Repo: "/home/alice/code/api"}, 30 * time.Second, nil, false},
		{"its main checkout", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo, InPlace: true}, 30 * time.Second, nil, false},
		{"another agent", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo, Profile: "haiku"}, 30 * time.Second, nil, false},
		{"another model", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo, Model: "sonnet"}, 30 * time.Second, nil, false},
		{"a name of its own", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo, Name: "Header"}, 30 * time.Second, nil, false},
		{"after the window", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo}, 2*time.Minute + time.Second, nil, false},
		{"with the window off", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo}, 30 * time.Second,
			func(c *Core, _ *lastJot, _ *session) {
				cfg := *c.cfg()
				cfg.Spawn.FollowUpWindow = 0
				c.useConfig(cfg)
			}, false},
		{"its session closed", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo}, 30 * time.Second,
			func(c *Core, _ *lastJot, _ *session) { delete(c.sessions, "s1") }, false},
		{"its agent ended", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo}, 30 * time.Second,
			func(_ *Core, _ *lastJot, x *session) { x.dead = true }, false},
		{"its spawn failed", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo}, 30 * time.Second,
			func(c *Core, last *lastJot, _ *session) {
				last.session, last.err = "", errors.New("no such repo")
				delete(c.sessions, "s1")
			}, false},
		{"its spawn not over yet", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo}, 2 * time.Second,
			func(c *Core, last *lastJot, _ *session) {
				last.session, last.done = "", make(chan struct{})
				delete(c.sessions, "s1")
			}, true},
		{"no jot before", ipc.SpawnArgs{Task: "also the header", Repo: before.Repo}, 30 * time.Second,
			func(c *Core, _ *lastJot, _ *session) { c.last = nil }, false},
	} {
		x := &session{id: "s1", name: "Fix the Footer"}
		last := &lastJot{args: before, at: now, session: x.id, done: over}
		c := &Core{sessions: map[string]*session{x.id: x}, last: last}
		c.useConfig(config.Default())
		if tc.change != nil {
			tc.change(c, last, x)
		}
		got, why, ok := c.addable(tc.args, now.Add(tc.after))
		if ok != tc.want || ok && (got != last || why == "") {
			t.Errorf("%s: added %v (%q), want %v", tc.name, ok, why, tc.want)
		}
	}
}

// What is typed adds to the task typed just before when it reads as more
// for it, and goes in once spawn.follow_up_delay is over; the same text
// typed with Apart takes it back and starts it on its own, before then or
// after, as the Mac app's New task does.
func TestATypedJotThatReadsAsMoreGoesToTheAgentOfTheOneBefore(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "follow_up_delay = \"2s\"\n"})
	in := func(text, apart string) ipc.InputResult {
		t.Helper()
		r, err := c.input(ipc.InputArgs{Text: text, Apart: apart})
		if err != nil || r.Session == nil {
			t.Fatalf("%q: %+v, %v", text, r, err)
		}
		return r
	}
	a := in("prompt,stop", "")
	b := in("also check the docs", "")
	if a.Did != ipc.InputSpawned || b.Did != ipc.InputAdded || b.Session.ID != a.Session.ID || b.Until.IsZero() || !strings.HasPrefix(b.Text, "Added to ") {
		t.Fatalf("typed %+v, then %+v; want the second added to the first", a, b)
	}
	if _, err := os.Stat(c.paths.FollowUpFile(a.Session.ID)); err != nil {
		t.Errorf("it does not wait in the session's folder: %v", err)
	}
	// Taken back before it is typed, it starts on its own, and nothing is
	// left waiting for the first.
	n := in("also check the docs", a.Session.ID)
	if n.Did != ipc.InputSpawned || n.Session.ID == a.Session.ID || strings.Contains(n.Text, "has it too") {
		t.Fatalf("taken back: %+v", n)
	}
	if _, err := os.Stat(c.paths.FollowUpFile(a.Session.ID)); !os.IsNotExist(err) {
		t.Errorf("the jot taken back still waits: %v", err)
	}
	// The one started apart is the jot the next adds to, and its agent
	// gets that once the delay is over.
	m := in("and a big one", "")
	if m.Did != ipc.InputAdded || m.Session.ID != n.Session.ID {
		t.Fatalf("after the one started apart: %+v, want it added to %s", m, n.Session.ID)
	}
	if e := c.prompted(n.Session.ID, "and a big one"); e.Time().Before(m.Until.Add(-100 * time.Millisecond)) {
		t.Errorf("typed at %v, before %v", e.Time(), m.Until)
	}
	// Taken back once its agent has it, it starts apart all the same, and
	// says so.
	if late := in("and a big one", n.Session.ID); late.Did != ipc.InputSpawned || late.Session.ID == n.Session.ID || !strings.Contains(late.Text, "has it too") {
		t.Errorf("taken back too late: %+v", late)
	}
	// A spawn that is not a jot, as irie ctl spawn's, never adds.
	var r ipc.SpawnResult
	if err := c.call("spawn", ipc.SpawnArgs{Task: "also from a script"}, &r); err != nil || r.Added {
		t.Errorf("not a jot: %+v, %v", r, err)
	}
	if got := c.agentPanes(); got != 4 {
		t.Errorf("%d agents, want 4", got)
	}
}

func TestAJotAddedWhileTheOneBeforeStartsGoesInOnceItsAgentRuns(t *testing.T) {
	// The agent's --version takes a second, which the spawn waits for, so
	// the first jot's spawn is not over when the second comes.
	fake := testutil.BuildFakeAgent(t)
	slow := filepath.Join(testutil.ShortDir(t), "slowagent")
	script := "#!/bin/sh\nfor a; do [ \"$a\" = --version ] && sleep 1; done\nexec '" + fake + "' \"$@\"\n"
	if err := os.WriteFile(slow, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := setupWith(t, agentConfig{command: slow, spawn: "follow_up_delay = \"0s\"\n"})
	type spawned struct {
		r   ipc.SpawnResult
		err error
	}
	first := make(chan spawned, 1)
	go func() {
		var r ipc.SpawnResult
		err := c.call("spawn", ipc.SpawnArgs{Task: "sleep:1s,prompt,stop", Jot: true}, &r)
		first <- spawned{r, err}
	}()
	time.Sleep(300 * time.Millisecond)
	b := c.jot(ipc.SpawnArgs{Task: "also the second part, please"})
	a := <-first
	if a.err != nil || a.r.Added {
		t.Fatalf("the first jot: %+v, %v", a.r, a.err)
	}
	if !b.Added || b.Session.ID != a.r.Session.ID {
		t.Fatalf("the second jot: %+v, want it added to %s", b, a.r.Session.ID)
	}
	// It is typed once the agent has its task, not while it starts.
	task := c.prompted(a.r.Session.ID, "sleep:1s,prompt,stop")
	c.prompted(a.r.Session.ID, "also the second part, please")
	var cv ipc.Conversation
	if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: a.r.Session.ID}, &cv); err != nil {
		t.Fatal(err)
	}
	if len(cv.Pending) != 1 || cv.Pending[0].Text != "also the second part, please" || cv.Pending[0].Time.Before(task.Time()) {
		t.Errorf("pending = %+v, want the jot, typed after the task at %v", cv.Pending, task.Time())
	}
	if n := c.agentPanes(); n != 1 {
		t.Errorf("%d agents, want 1", n)
	}
}

func TestAJotWaitingForItsAgentOutlivesACoreRestart(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "follow_up_delay = \"0s\"\n"})
	a := c.jot(ipc.SpawnArgs{Task: "sleep:4s,prompt,stop"})
	if b := c.jot(ipc.SpawnArgs{Task: "also keep the old flag"}); !b.Added {
		t.Fatalf("not added: %+v", b)
	}
	if b, err := os.ReadFile(c.paths.FollowUpFile(a.Session.ID)); err != nil || !strings.Contains(string(b), "also keep the old flag") {
		t.Fatalf("waiting jots = %q, %v", b, err)
	}
	c.restartCore()
	c.prompted(a.Session.ID, "also keep the old flag")
}
