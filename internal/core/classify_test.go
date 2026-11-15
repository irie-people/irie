package core

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/route"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/testutil/ollamafake"
)

func (c *coreEnv) label(ls ...ipc.Label) ipc.LabelResult {
	c.t.Helper()
	var r ipc.LabelResult
	if err := c.call("label", ipc.LabelArgs{Labels: ls}, &r); err != nil {
		c.t.Fatalf("label: %v", err)
	}
	return r
}

// labelled makes n tasks of a class, each of the same three words and one
// of its own, so a task of those three words is near every one.
func labelled(class, words string, n int) []ipc.Label {
	out := make([]ipc.Label, n)
	for i := range out {
		out[i] = ipc.Label{Text: fmt.Sprintf("%s item%d", words, i), Class: class}
	}
	return out
}

func TestTasksTakeTheClassOfTheNearestLabelledTask(t *testing.T) {
	f := ollamafake.New(0, []ollamafake.Model{{Name: "embed-fake", Size: 300 << 20, Embeds: true}})
	srv := httptest.NewServer(f.Handler())
	defer srv.Close()
	a := routing(t)
	a.extra += fmt.Sprintf("\n[route.embed]\nmodel = \"embed-fake\"\nurl = %q\nnear = 0.5\n", srv.URL)
	c := setupWith(t, a)

	// The words call this medium, having none of theirs.
	const task = "polish sidebar badge colours"
	if ch := c.routeDry(task); ch.Class != route.Medium {
		t.Fatalf("before any labels: %s (%s)", ch.Class, ch.Why)
	}
	if err := c.call("label", ipc.LabelArgs{Labels: []ipc.Label{{Text: "x", Class: "huge"}}}, nil); err == nil {
		t.Error("labelled a task with a class that is not one")
	}

	// Too few labelled tasks: the words still decide.
	r := c.label(labelled(route.Hard, "polish sidebar badge", 12)...)
	if r.Added != 12 || r.Labels != 12 || r.Sorting || !strings.Contains(r.Note, "30") {
		t.Errorf("12 labels = %+v", r)
	}
	if ch := c.routeDry(task); ch.Class != route.Medium {
		t.Errorf("with 12 labels: %s (%s)", ch.Class, ch.Why)
	}
	c.label(labelled(route.Small, "rename helper variable", 12)...)
	r = c.label(append(labelled(route.Medium, "write parser test", 12), labelled(route.Hard, "polish sidebar badge", 1)...)...)
	if r.Added != 12 || r.Labels != 36 {
		t.Errorf("a label given again was added: %+v", r)
	}

	// Once they are embedded, the nearest decides, and says so.
	testutil.Eventually(t, 15*time.Second, "the labelled tasks were never embedded", func() bool {
		ch := c.routeDry(task)
		return ch.Class == route.Hard && strings.Contains(ch.Why, "like “polish sidebar badge item") && ch.Profile == "fake"
	})
	// A task unlike every labelled one keeps the words' class.
	if ch := c.routeDry("fix the typo in README"); ch.Class != route.Small || !strings.Contains(ch.Why, "typo") {
		t.Errorf("an unlike task: %s (%s)", ch.Class, ch.Why)
	}
	// Spawns take the class too.
	if s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop\n" + task, Profile: "auto"}); s.Class != route.Hard || s.Profile != "fake" {
		t.Errorf("spawned %+v", s)
	}

	// A restarted core reads the embeddings back rather than make them
	// again: only the task is embedded.
	before := f.Embeds
	c.restartCore()
	if ch := c.routeDry(task); ch.Class != route.Hard {
		t.Errorf("after a restart: %s (%s)", ch.Class, ch.Why)
	}
	if n := f.Embeds - before; n != 1 {
		t.Errorf("after a restart the server embedded %d texts, want just the task", n)
	}

	// With the server gone, the words decide, at once.
	srv.Close()
	start := time.Now()
	if ch := c.routeDry(task); ch.Class != route.Medium {
		t.Errorf("with no server: %s (%s)", ch.Class, ch.Why)
	}
	if d := time.Since(start); d > embedWait+2*time.Second {
		t.Errorf("with no server the route took %v", d)
	}
}

// A subtask its caller waits on never goes to a profile whose answers come
// after the wait.
func TestADelegationPassesOverAProfileTooSlowForItsWait(t *testing.T) {
	cfg := config.Default()
	cfg.Route.Profiles = []string{"haiku", "sonnet", "claude"}
	c := bareCore(t, cfg)
	start := time.Now().Add(-3 * time.Hour)
	for i := range 3 {
		o := c.outcome(fmt.Sprintf("s%012d", i))
		o.profile, o.class, o.parent = "haiku", route.Small, "s00000000000p"
		o.spawned = start.Add(time.Duration(i) * time.Minute)
		o.ended = o.spawned.Add(20 * time.Minute)
	}
	find := func(ch route.Choice, p string) route.Candidate {
		i := slices.IndexFunc(ch.Candidates, func(c route.Candidate) bool { return c.Profile == p })
		if i < 0 {
			t.Fatalf("no %s in %+v", p, ch)
		}
		return ch.Candidates[i]
	}
	only := cfg.Route.Profiles
	if ch := c.choose(route.Small, "", only, 0, false); ch.Profile != "haiku" || find(ch, "haiku").Time != 20*time.Minute || find(ch, "haiku").Timed != 3 {
		t.Errorf("with no wait = %+v", ch)
	}
	ch := c.choose(route.Small, "", only, 10*time.Minute, false)
	if h := find(ch, "haiku"); ch.Profile != "sonnet" || h.Usable || h.Note != "answers in about 20m, past the 10m wait" {
		t.Errorf("with a 10m wait = %+v", ch)
	}
	if ch := c.choose(route.Small, "", only, time.Hour, false); ch.Profile != "haiku" {
		t.Errorf("with an hour's wait = %+v", ch)
	}
	// A session still at work when its caller gave up counts as taking at
	// least the wait.
	o := c.outcome("s000000000009")
	o.profile, o.class, o.parent, o.spawned, o.waited = "sonnet", route.Small, "s00000000000p", time.Now().Add(-time.Hour), 10*time.Minute
	// A session the user started, slow as it may be, says nothing of how
	// soon a subtask comes back.
	u := c.outcome("s00000000000u")
	u.profile, u.class, u.spawned, u.ended = "claude", route.Small, start, start.Add(5*time.Hour)
	if ch := c.choose(route.Small, "", only, 10*time.Minute, false); ch.Profile != "claude" || find(ch, "sonnet").Usable || find(ch, "claude").Timed != 0 {
		t.Errorf("after sonnet ran past the wait = %+v", ch)
	}
}

func TestADelegationStillAtWorkIsHandedOn(t *testing.T) {
	agent := testutil.BuildFakeAgent(t)
	c := setupWith(t, agentConfig{extra: fmt.Sprintf(`[profiles.slow]
command = %[1]q
model = "claude-haiku-5-5"
permission_mode = "auto"

[profiles.quick]
command = %[1]q
model = "claude-haiku-5-5"
permission_mode = "auto"
env = { IRIE_FAKE_SCRIPT = "prompt,stop" }

[route]
profiles = ["slow", "quick", "fake"]
prior_weight = 2

[route.accuracy]
slow = { small = 0.9, medium = 0.5, hard = 0.1 }
quick = { small = 0.95, medium = 0.5, hard = 0.1 }
fake = { small = 0.95, medium = 0.9, hard = 0.9 }

[route.cost]
slow = 0.05
quick = 0.1
fake = 1.0
`, agent)})
	parent := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Profile: "fake"})
	c.waitState(parent.ID, ipc.StateWorking)
	wait := "8s"
	delegate := func(task string) (string, string, int) {
		cmd := exec.Command(c.bin, "ctl", "delegate", "--timeout", wait, task)
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "IRIE_SESSION="+parent.ID)
		var out, errOut strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return out.String(), errOut.String(), code
	}

	// slow is the cheapest per success, but is still at work after the
	// wait: it is stopped, and quick finishes the subtask.
	out, errOut, code := delegate("prompt,sleep:2m,stop\nfix the typo in README")
	if code != 0 || !strings.Contains(out, "Done") || !strings.Contains(errOut, "delegated to +slow") || !strings.Contains(errOut, "+slow was still at work after 8s, so it handed the subtask on to +quick") {
		t.Fatalf("delegate: exit %d, out %q, err %q", code, out, errOut)
	}
	var first, second ipc.Session
	for _, s := range c.list() {
		switch {
		case s.Parent == parent.ID && s.Profile == "slow":
			first = s
		case s.Parent == parent.ID && s.Profile == "quick":
			second = s
		}
	}
	if first.ID != "" {
		t.Errorf("the slow session is still there: %+v", first)
	}
	if second.ID == "" || !second.Routed || second.Class != route.Small {
		t.Fatalf("the session that took it over = %+v", second)
	}
	task, _ := os.ReadFile(c.paths.TaskFile(second.ID))
	if !strings.HasPrefix(string(task), "[Irie] This subtask ran on +slow, which was still at work after 8s, so Irie stopped it.") || !strings.Contains(string(task), "fix the typo") {
		t.Errorf("the task it took over = %q", task)
	}
	// The stopped session's record says why, and the router now knows
	// slow takes at least that long.
	var timedOut []event.Event
	for _, e := range c.journal() {
		if e.Kind == event.DelegateTimedOut {
			timedOut = append(timedOut, e)
		}
	}
	if len(timedOut) != 1 || timedOut[0].Str("to_profile") != "quick" {
		t.Errorf("timed-out events = %+v", timedOut)
	}
	var ch route.Choice
	if err := c.call("route", ipc.RouteArgs{Task: "fix the typo in README", Parent: parent.ID, Wait: 8 * time.Second}, &ch); err != nil {
		t.Fatal(err)
	}
	if ch.Profile != "quick" || !strings.Contains(ch.Candidates[slices.IndexFunc(ch.Candidates, func(c route.Candidate) bool { return c.Profile == "slow" })].Note, "past the 8s wait") {
		t.Errorf("a subtask waited 8s for = %+v", ch)
	}
	// With no one waiting, slow is still the cheapest.
	if ch := c.routeDry("fix the typo in README"); ch.Profile != "slow" {
		t.Errorf("with no wait = %+v", ch)
	}

	// The next subtask waited as long for goes to quick at once.
	if _, errOut, code := delegate("prompt,stop\nfix the typo in README"); code != 0 || !strings.Contains(errOut, "delegated to +quick") {
		t.Errorf("the next delegate: exit %d, err %q", code, errOut)
	}
}
