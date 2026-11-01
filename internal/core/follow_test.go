package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
		got = c.promptOf(id, text)
		return got.Kind != ""
	})
	return got
}

// promptOf is the prompt event of text in session id, if its agent had it.
func (c *coreEnv) promptOf(id, text string) event.Event {
	for _, e := range c.events(id) {
		if n, _ := e.Int("chars"); e.Kind == event.PromptSubmitted && e.Source == event.SourceHook && n == utf8.RuneCountInString(text) {
			return e
		}
	}
	return event.Event{}
}

func TestAJotThatReadsAsMoreGoesToTheAgentOfTheOneBefore(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "follow_up_delay = \"1s\"\n"})
	a := c.jot(ipc.SpawnArgs{Task: "prompt,stop"})
	if a.Added {
		t.Fatalf("the first jot was added to %s", a.Session.Name)
	}
	sent := time.Now()
	b := c.jot(ipc.SpawnArgs{Task: "also check the docs"})
	if !b.Added || b.Session.ID != a.Session.ID {
		t.Fatalf("the second jot: %+v, want it added to %s", b, a.Session.ID)
	}
	if b.Until.Before(sent.Add(900 * time.Millisecond)) {
		t.Errorf("it can be taken back until %v, want a second from %v", b.Until, sent)
	}
	if n := c.agentPanes(); n != 1 {
		t.Errorf("%d agents, want 1", n)
	}
	// It goes to the agent as a prompt of its own, once the delay is over.
	if e := c.prompted(a.Session.ID, "also check the docs"); e.Time().Before(b.Until.Add(-100 * time.Millisecond)) {
		t.Errorf("typed at %v, before %v", e.Time(), b.Until)
	}
	if _, err := os.Stat(c.paths.FollowUpFile(a.Session.ID)); !os.IsNotExist(err) {
		t.Errorf("the waiting jots' file is left: %v", err)
	}

	// A jot that reads as a task of its own starts an agent of its own, and
	// is the one the next jot adds to.
	n := c.jot(ipc.SpawnArgs{Task: "prompt,stop"})
	if n.Added || n.Session.ID == a.Session.ID {
		t.Fatalf("a task of its own: %+v", n)
	}
	if m := c.jot(ipc.SpawnArgs{Task: "it's the second button"}); !m.Added || m.Session.ID != n.Session.ID {
		t.Errorf("after a new jot: %+v, want it added to %s", m, n.Session.ID)
	}
	if n := c.agentPanes(); n != 2 {
		t.Errorf("%d agents, want 2", n)
	}
}

func TestOnlyAJotForTheSamePlaceSoonAfterAddsToTheOneBefore(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "follow_up_window = \"3s\"\nfollow_up_delay = \"0s\"\n"})
	dir := t.TempDir()
	apart := func(r ipc.SpawnResult, before ipc.SpawnResult, why string) {
		t.Helper()
		if r.Added || r.Session.ID == before.Session.ID {
			t.Errorf("%s: %+v, want a session of its own", why, r)
		}
	}
	a := c.jot(ipc.SpawnArgs{Task: "prompt,stop"})
	// Another folder, or another agent, is a task of its own.
	b := c.jot(ipc.SpawnArgs{Task: "also over here", Cwd: dir})
	apart(b, a, "another folder")
	d := c.jot(ipc.SpawnArgs{Task: "also with the fake agent", Cwd: dir, Profile: "fake"})
	apart(d, b, "another agent")
	if r := c.jot(ipc.SpawnArgs{Task: "it is in main.go", Cwd: dir, Profile: "fake"}); !r.Added || r.Session.ID != d.Session.ID {
		t.Fatalf("the same place and agent: %+v, want it added to %s", r, d.Session.ID)
	}
	// A spawn that is not a jot, as irie ctl spawn's, never adds, and is not
	// the jot the next one adds to.
	var r ipc.SpawnResult
	if err := c.call("spawn", ipc.SpawnArgs{Task: "also from a script", Cwd: dir, Profile: "fake"}, &r); err != nil || r.Added || r.Session.ID == d.Session.ID {
		t.Errorf("not a jot: %+v, %v", r, err)
	}
	if r := c.jot(ipc.SpawnArgs{Task: "and the footer", Cwd: dir, Profile: "fake"}); !r.Added || r.Session.ID != d.Session.ID {
		t.Errorf("after a spawn that is not a jot: %+v, want it added to %s", r, d.Session.ID)
	}
	// Once the window has passed since it was last added to, a jot starts
	// its own agent.
	time.Sleep(3500 * time.Millisecond)
	e := c.jot(ipc.SpawnArgs{Task: "also too late", Cwd: dir, Profile: "fake"})
	apart(e, d, "after the window")
	// So does one after the jot before it was closed.
	c.closeSession(e.Session.ID)
	apart(c.jot(ipc.SpawnArgs{Task: "also after the close", Cwd: dir, Profile: "fake"}), e, "after a close")
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
		err := c.call("spawn", ipc.SpawnArgs{Task: "sleep:2s,prompt,stop", Jot: true}, &r)
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
	task := c.prompted(a.r.Session.ID, "sleep:2s,prompt,stop")
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

func TestAJotTakenBackStartsApart(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "follow_up_delay = \"2s\"\n"})
	a := c.jot(ipc.SpawnArgs{Task: "prompt,stop"})
	c.waitState(a.Session.ID, ipc.StateDone)
	if b := c.jot(ipc.SpawnArgs{Task: "also make an icon"}); !b.Added {
		t.Fatalf("not added: %+v", b)
	}
	// Taken back before its agent has it: only the new agent gets it.
	n := c.jot(ipc.SpawnArgs{Task: "also make an icon", Apart: a.Session.ID})
	if n.Added || n.Late || n.Session.ID == a.Session.ID {
		t.Fatalf("taken back: %+v", n)
	}
	time.Sleep(3 * time.Second)
	if e := c.promptOf(a.Session.ID, "also make an icon"); e.Kind != "" {
		t.Errorf("the first agent got the jot taken back: %+v", e)
	}
	// The one started apart is the jot the next adds to. Taken back once
	// its agent has it, it starts apart all the same, and says so.
	if m := c.jot(ipc.SpawnArgs{Task: "and a big one"}); !m.Added || m.Session.ID != n.Session.ID {
		t.Fatalf("after the one started apart: %+v, want it added to %s", m, n.Session.ID)
	}
	c.prompted(n.Session.ID, "and a big one")
	if late := c.jot(ipc.SpawnArgs{Task: "and a big one", Apart: n.Session.ID}); !late.Late || late.Added || late.Session.ID == n.Session.ID {
		t.Errorf("taken back too late: %+v", late)
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
