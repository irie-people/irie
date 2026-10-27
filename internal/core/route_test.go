package core

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/route"
	"github.com/irie-hq/irie/internal/testutil"
)

// routing makes the fake agent two route profiles: cheap, accurate enough
// for small work only, and fake, the strongest.
func routing(t *testing.T) agentConfig {
	return agentConfig{extra: fmt.Sprintf(`[profiles.cheap]
command = %q
model = "claude-haiku-5-5"
permission_mode = "auto"

[route]
profiles = ["cheap", "fake"]
prior_weight = 2

[route.accuracy]
cheap = { small = 0.9, medium = 0.5, hard = 0.1 }
fake = { small = 0.95, medium = 0.9, hard = 0.9 }

[route.cost]
cheap = 0.05
fake = 1.0
`, testutil.BuildFakeAgent(t))}
}

// The fake agent runs the first line of a task as its script; the rest
// gives the task its class.
const (
	smallTask = "prompt,stop\nfix the typo in README"
	hardTask  = "prompt,stop\nrefactor the core so state lives in one place"
)

func (c *coreEnv) routeDry(task string) route.Choice {
	c.t.Helper()
	var ch route.Choice
	if err := c.call("route", ipc.RouteArgs{Task: task}, &ch); err != nil {
		c.t.Fatalf("route %q: %v", task, err)
	}
	return ch
}

func (c *coreEnv) kinds(id string) []string {
	var out []string
	for _, e := range c.events(id) {
		out = append(out, e.Kind)
	}
	return out
}

func TestAutoRoutesByCostPerSuccess(t *testing.T) {
	c := setupWith(t, routing(t))
	small := c.spawnArgs(ipc.SpawnArgs{Task: smallTask, Profile: "auto"})
	hard := c.spawnArgs(ipc.SpawnArgs{Task: hardTask, Profile: "auto"})
	if small.Profile != "cheap" || !small.Routed || small.Class != route.Small {
		t.Errorf("small task = %+v", small)
	}
	if hard.Profile != "fake" || !hard.Routed || hard.Class != route.Hard {
		t.Errorf("hard task = %+v", hard)
	}
	// Every spawn has a class, routed or not.
	if s := c.spawnArgs(ipc.SpawnArgs{Task: hardTask, Profile: "cheap"}); s.Routed || s.Class != route.Hard {
		t.Errorf("a chosen profile = %+v", s)
	}

	// The dry run says why: cheap is accurate enough and cheaper per
	// success; for hard work it is not accurate enough.
	ch := c.routeDry(smallTask)
	if ch.Profile != "cheap" || !ch.Met || len(ch.Candidates) != 2 || ch.Candidates[0].Profile != "cheap" || !strings.Contains(ch.Why, "typo") {
		t.Errorf("small choice = %+v", ch)
	}
	if ch := c.routeDry(hardTask); ch.Profile != "fake" || ch.Candidates[0].Profile != "cheap" || ch.Candidates[0].Accuracy >= 0.85 {
		t.Errorf("hard choice = %+v", ch)
	}

	// The + list says where auto sends each class.
	var r ipc.CompleteResult
	if err := c.call("complete", ipc.CompleteArgs{Text: "+au"}, &r); err != nil || !slices.Equal(r.Items, []string{"+auto"}) {
		t.Fatalf("complete +au = %+v, %v", r, err)
	}
	if r.Notes[0] != "small→cheap · medium→fake · hard→fake · by cost per success" {
		t.Errorf("auto's note = %q", r.Notes[0])
	}
}

func TestOutcomesTeachTheRouter(t *testing.T) {
	c := setupWith(t, routing(t))
	// A bad rating of cheap's small work, against starting guesses worth two
	// outcomes: its accuracy falls below the floor, so small work goes to
	// fake.
	s := c.spawnArgs(ipc.SpawnArgs{Task: smallTask, Profile: "auto"})
	if s.Profile != "cheap" {
		t.Fatalf("small task on %s before any rating", s.Profile)
	}
	c.waitState(s.ID, ipc.StateDone)
	if err := c.call("rate", ipc.RateArgs{Session: s.ID, Verdict: ipc.Bad}, nil); err != nil {
		t.Fatal(err)
	}
	if row, _ := c.row(s.ID); row.Rated != ipc.Bad {
		t.Errorf("row rated %q", row.Rated)
	}
	ch := c.routeDry(smallTask)
	i := slices.IndexFunc(ch.Candidates, func(c route.Candidate) bool { return c.Profile == "cheap" })
	if ch.Profile != "fake" || i < 0 || ch.Candidates[i].Outcomes != 1 || ch.Candidates[i].Accuracy != 0.6 {
		t.Errorf("after a bad rating = %+v", ch)
	}
	// A good rating brings it back up.
	if err := c.call("rate", ipc.RateArgs{Session: s.ID, Verdict: ipc.Good}, nil); err != nil {
		t.Fatal(err)
	}
	if ch := c.routeDry(smallTask); ch.Profile != "cheap" {
		t.Errorf("after rating it good = %+v", ch)
	}
	if err := c.call("rate", ipc.RateArgs{Session: s.ID, Verdict: ipc.Bad}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.call("rate", ipc.RateArgs{Session: "s000000000000", Verdict: ipc.Good}, nil); err == nil {
		t.Error("rated a session that is not there")
	}

	// A task run again on a more accurate profile says the first session
	// fell short.
	first := c.spawnArgs(ipc.SpawnArgs{Task: "prompt\nadd a flag", Profile: "cheap"})
	c.spawnArgs(ipc.SpawnArgs{Task: "prompt\n  ADD a flag ", Profile: "fake"})
	if !slices.Contains(c.kinds(first.ID), event.OutcomeEscalated) {
		t.Errorf("the first session's events %v lack its escalation", c.kinds(first.ID))
	}

	// A restarted core still knows the outcomes, from the journal.
	c.restartCore()
	if ch := c.routeDry(smallTask); ch.Profile != "fake" {
		t.Errorf("after a restart = %+v", ch)
	}
}

func TestDelegationRunsASubtaskOnACheaperProfile(t *testing.T) {
	c := setupWith(t, routing(t))
	parent := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Profile: "fake"})
	c.waitState(parent.ID, ipc.StateWorking)
	// The parent, on the strongest profile, is told it can delegate.
	hinted := func(id string) bool {
		for _, r := range c.agentLog(id) {
			if r["event"] == "SessionStart" && strings.Contains(fmt.Sprint(r["stdout"]), "ctl delegate") {
				return true
			}
		}
		return false
	}
	if !hinted(parent.ID) {
		t.Errorf("the parent was not told it can delegate: %v", c.agentLog(parent.ID))
	}

	delegate := func(task string) (string, string, int) {
		cmd := exec.Command(c.bin, "ctl", "delegate", "--timeout", "20s", task)
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

	// Small work goes to cheap, and its result comes back.
	out, errOut, code := delegate(smallTask)
	if code != 0 || out != "Done: prompt,stop\n" || !strings.Contains(errOut, "delegated to +cheap") {
		t.Fatalf("delegate: exit %d, out %q, err %q", code, out, errOut)
	}
	var child ipc.Session
	for _, s := range c.list() {
		if s.Parent == parent.ID {
			child = s
		}
	}
	if child.Profile != "cheap" || !child.Routed {
		t.Fatalf("the delegated session = %+v", child)
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(child.ID))
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.Index(spec.Argv, "--permission-mode"); i < 0 || spec.Argv[i+1] != "acceptEdits" {
		t.Errorf("delegated argv = %q", spec.Argv)
	}
	// A delegated session is not told to delegate again.
	if hinted(child.ID) {
		t.Error("the delegated session was told to delegate")
	}

	// Hard work needs more than the profiles cheaper than the parent's.
	if _, errOut, code := delegate(hardTask); code != exitNeedsStrongerCode || !strings.Contains(errOut, "do it yourself") {
		t.Errorf("hard delegate: exit %d, err %q", code, errOut)
	}
}

// exitNeedsStrongerCode is `irie ctl delegate`'s exit code for a subtask
// that needs a stronger model.
const exitNeedsStrongerCode = 3

func TestAUsageLimitFailsOver(t *testing.T) {
	c := setupWith(t, routing(t))
	// Small work carries on, on cheap, in the same directory.
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stopfail:rate_limit\nfix the typo in README", Profile: "fake"})
	var cont ipc.Session
	testutil.Eventually(t, 15*time.Second, "no session carried the task on", func() bool {
		for _, r := range c.list() {
			if r.ID != s.ID && r.Profile == "cheap" {
				cont = r
				return true
			}
		}
		return false
	})
	if cont.Cwd != s.Cwd || cont.Class != route.Small || !cont.Routed {
		t.Errorf("continuation = %+v, original cwd %s", cont, s.Cwd)
	}
	task, _ := os.ReadFile(c.paths.TaskFile(cont.ID))
	if !strings.HasPrefix(string(task), "[Irie] This task ran on +fake, which stopped at its usage limit.") || !strings.Contains(string(task), "fix the typo") {
		t.Errorf("continuation task = %q", task)
	}
	testutil.Eventually(t, 5*time.Second, "no failover event", func() bool {
		return slices.Contains(c.kinds(s.ID), event.RouteFailover)
	})

	// Hard work does not go to a profile that is not accurate enough: it
	// waits for the limit to reset.
	h := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stopfail:rate_limit\nrefactor the core so state lives in one place", Profile: "fake"})
	var reason string
	testutil.Eventually(t, 15*time.Second, "no failover decision", func() bool {
		for _, e := range c.events(h.ID) {
			if e.Kind == event.RouteFailover {
				reason = e.Str("reason")
				return true
			}
		}
		return false
	})
	if !strings.Contains(reason, "waits for the limit to reset") {
		t.Errorf("failover reason = %q", reason)
	}
	for _, r := range c.list() {
		if strings.HasPrefix(r.Name, h.Name) && r.ID != h.ID {
			t.Errorf("a hard task carried on: %+v", r)
		}
	}
}

func TestToolSessions(t *testing.T) {
	c := setup(t)
	var r ipc.SpawnResult
	if err := c.call("tool", ipc.ToolArgs{Name: ipc.ToolModels}, &r); err != nil {
		t.Fatal(err)
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(r.Session.ID))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Harness != "tool" || !slices.Equal(spec.Argv, []string{c.bin, "ctl", "local"}) || r.Session.Name != "models" {
		t.Errorf("models tool: spec %+v, session %+v", spec, r.Session)
	}
	// It prints and exits; its pane keeps what it printed.
	c.waitState(r.Session.ID, ipc.StateExited)
	if err := c.call("tool", ipc.ToolArgs{Name: "bogus"}, &r); err == nil {
		t.Error("a tool that is not there")
	}
}
