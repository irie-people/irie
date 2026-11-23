package core

import (
	"encoding/json"
	"fmt"
	"github.com/irie-people/irie/internal/brand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/council"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
)

// councilCore is a core with no tmux behind it whose latest council sits
// in session s1, a folder of the test's own, and may bring two plans;
// session s2 is another agent's.
func councilCore(t *testing.T) (*Core, string) {
	t.Helper()
	c, _ := dogCore(t)
	t.Setenv("HOME", t.TempDir())
	folder := t.TempDir()
	cfg := *c.cfg()
	cfg.Arch.Raguel.Folder = folder
	c.useConfig(cfg)
	c.sessions["s1"] = &session{id: "s1", name: "council-2026-11-21", pane: "%1"}
	c.sessions["s2"] = &session{id: "s2", name: "other", pane: "%2"}
	tally := council.Count(council.Input{Now: time.Now()})
	c.ragu.councils = []council.Council{{Date: "2026-11-21", Started: time.Now(), Days: 7, Session: "s1", Name: "council-2026-11-21", Folder: folder, Most: 2, Plans: []council.Plan{}, Tally: &tally}}
	return c, folder
}

// inAgent has the test's own process run in the agent of pane, as a call
// from that agent's tree does; "" for none, as from the user's terminal.
// The pane names its session, as Irie's panes do.
func inAgent(c *Core, pane string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pane == "" {
		c.snap = tmux.Snapshot{}
		return
	}
	var id string
	for _, x := range c.sessions {
		if x.pane == pane {
			id = x.id
		}
	}
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: pane, Role: tmux.RoleAgent, PID: os.Getppid(), IrieSession: id}}}
}

func testPlan(where, title string) *council.Plan {
	return &council.Plan{Seat: "gabriel", Title: title, Evidence: "964 calls held a hand-written sleep, 811 minutes.", Change: "A wait helper in tools/.",
		Saves: "800 minutes a week, by the waits' minutes", Effort: "small", Watch: "Hand-written waits", Where: where, Brief: "Write tools/wait.sh.\nCheck it with tools/check.sh."}
}

func TestACouncilTakesPlansAndItsEndOnlyFromItsOwnSession(t *testing.T) {
	c, folder := councilCore(t)
	where := t.TempDir()
	propose := func(p *council.Plan) (ipc.CouncilResult, error) {
		return c.councilOp(os.Getpid(), ipc.CouncilArgs{Do: ipc.CouncilPropose, Plan: p})
	}
	// From the user's terminal, or another agent, a plan is refused.
	for _, pane := range []string{"", "%2"} {
		inAgent(c, pane)
		if _, err := propose(testPlan(where, "Fewer sleeps")); err == nil || !strings.Contains(err.Error(), "Only the council's own session puts a plan in") {
			t.Errorf("a plan from pane %q: %v", pane, err)
		}
		if _, err := c.councilOp(os.Getpid(), ipc.CouncilArgs{Do: ipc.CouncilDone, Summary: "Done."}); err == nil || !strings.Contains(err.Error(), "Only the council's own session says the council is done") {
			t.Errorf("done from pane %q: %v", pane, err)
		}
	}
	inAgent(c, "%1")
	// A plan with a field wrong is refused, naming it; so is a folder
	// that is not there.
	bad := testPlan(where, "Fewer sleeps")
	bad.Effort = "huge"
	if _, err := propose(bad); err == nil || !strings.Contains(err.Error(), `effort "huge"`) {
		t.Errorf("a bad effort: %v", err)
	}
	if _, err := propose(testPlan(filepath.Join(where, "nope"), "Fewer sleeps")); err == nil || !strings.Contains(err.Error(), "no such folder") {
		t.Errorf("a folder not there: %v", err)
	}
	if _, err := propose(nil); err == nil {
		t.Error("propose with no plan went in")
	}
	// Plans are numbered in the order put in, up to the most it may bring.
	r, err := propose(testPlan(where, "Fewer sleeps"))
	if err != nil || r.Plan != 1 {
		t.Fatalf("the first plan: %+v, %v", r, err)
	}
	if _, err := propose(testPlan(where, "fewer sleeps")); err == nil || !strings.Contains(err.Error(), "Plan 1 has that title") {
		t.Errorf("the same plan again: %v", err)
	}
	if r, err := propose(testPlan(where, "One call for a grep and its read")); err != nil || r.Plan != 2 {
		t.Fatalf("the second plan: %+v, %v", r, err)
	}
	if _, err := propose(testPlan(where, "A third")); err == nil || !strings.Contains(err.Error(), "has brought 2 plans") {
		t.Errorf("a plan past arch.raguel.plans: %v", err)
	}
	// Done writes the minutes into the folder, and takes no plan after.
	if _, err := c.councilOp(os.Getpid(), ipc.CouncilArgs{Do: ipc.CouncilDone}); err == nil || !strings.Contains(err.Error(), "summary") {
		t.Errorf("done with no summary: %v", err)
	}
	r, err = c.councilOp(os.Getpid(), ipc.CouncilArgs{Do: ipc.CouncilDone, Summary: "A quiet week."})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(folder, "2026-11-21 Council.html"), filepath.Join(folder, "2026-11-21 Council.md")}
	if strings.Join(r.Minutes, "|") != strings.Join(want, "|") {
		t.Fatalf("minutes %v, want %v", r.Minutes, want)
	}
	for _, f := range want {
		b, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(b), "Fewer sleeps") || !strings.Contains(string(b), "One call for a grep and its read") || !strings.Contains(string(b), "A quiet week.") {
			t.Errorf("%s: %v\n%s", f, err, b)
		}
	}
	if l, _ := c.latestCouncil(false); l.Done.IsZero() || l.Summary != "A quiet week." || len(l.Plans) != 2 || l.Plans[0].Where != where {
		t.Errorf("the council = %+v", l)
	}
	if _, err := propose(testPlan(where, "Late")); err == nil || !strings.Contains(err.Error(), "is done") {
		t.Errorf("a plan after done: %v", err)
	}
	// raguel.json keeps it for the next core.
	c2, _ := dogCore(t)
	c2.paths = c.paths
	c2.loadRaguel()
	if l, ok := c2.latestCouncil(false); !ok || len(l.Plans) != 2 || l.Plans[1].N != 2 {
		t.Errorf("after a restart: %+v", l)
	}
}

func TestOnlyTheUsersWordAnswersAPlan(t *testing.T) {
	c, _ := councilCore(t)
	where := t.TempDir()
	c.ragu.councils[0].Plans = []council.Plan{*testPlan(where, "Fewer sleeps"), *testPlan(where, "A cheaper model")}
	c.ragu.councils[0].Plans[0].N, c.ragu.councils[0].Plans[1].N = 1, 2
	yes := ipc.CouncilArgs{Do: ipc.CouncilYes, Plans: []int{1}, OK: true}
	no := ipc.CouncilArgs{Do: ipc.CouncilNo, Plans: []int{2}, Why: "not now"}
	// Another agent can't answer for the user.
	inAgent(c, "%2")
	for _, a := range []ipc.CouncilArgs{yes, no} {
		if _, err := c.councilOp(os.Getpid(), a); err == nil || !strings.Contains(err.Error(), "An agent can't answer") {
			t.Errorf("%s from another agent: %v", a.Do, err)
		}
	}
	// Nor can the council's own session, until the user messages it.
	inAgent(c, "%1")
	if _, err := c.councilOp(os.Getpid(), no); err == nil || !strings.Contains(err.Error(), "have not messaged you here") {
		t.Errorf("no from the council, unasked: %v", err)
	}
	c.mu.Lock()
	c.sessions["s1"].userWord = time.Now().Add(-c.cfg().Arch.Gabriel.Word - time.Minute)
	c.mu.Unlock()
	if _, err := c.councilOp(os.Getpid(), no); err == nil {
		t.Error("no from the council, the user's message long past, went through")
	}
	c.markWord("s1")
	if _, err := c.councilOp(os.Getpid(), no); err != nil {
		t.Errorf("no from the council, just after the user's message: %v", err)
	}
	if l, _ := c.latestCouncil(false); l.Plans[1].Said != council.No || l.Plans[1].Why != "not now" || l.Plans[0].Said != council.Waiting {
		t.Errorf("plans = %+v", l.Plans)
	}
	// A yes asks first, saying where the jot runs and what it is told.
	inAgent(c, "")
	if _, err := c.councilOp(0, ipc.CouncilArgs{Do: ipc.CouncilYes, Plans: []int{1}}); err == nil || !strings.Contains(err.Error(), "A jot starts in "+where) || !strings.Contains(err.Error(), "Write tools/wait.sh.") {
		t.Errorf("a yes without OK: %v", err)
	}
	if _, err := c.councilOp(0, ipc.CouncilArgs{Do: ipc.CouncilYes, Plans: []int{3}, OK: true}); err == nil || !strings.Contains(err.Error(), "no plan 3") {
		t.Errorf("a yes to a plan it lacks: %v", err)
	}
	// Not from a socket, no one's word is taken.
	raw, _ := json.Marshal(no)
	if _, err := c.Call(ipc.OpCouncil, raw); err == nil {
		t.Error("an answer not over the socket went through")
	}
}

func TestConveningIsRefusedWhileACouncilSits(t *testing.T) {
	c, _ := councilCore(t)
	if _, err := c.councilOp(0, ipc.CouncilArgs{Do: ipc.CouncilConvene}); err == nil || !strings.Contains(err.Error(), "Opus 5.5") || !strings.Contains(err.Error(), "Say OK") {
		t.Errorf("convene without OK: %v", err)
	}
	if _, err := c.councilOp(0, ipc.CouncilArgs{Do: ipc.CouncilConvene, OK: true}); err == nil || !strings.Contains(err.Error(), "A council sits now, in council-2026-11-21") {
		t.Errorf("convene while one sits: %v", err)
	}
	c.ragu.convening = true
	c.ragu.councils[0].Done = time.Now()
	if _, err := c.convene(time.Now()); err == nil || !strings.Contains(err.Error(), "sits now") {
		t.Errorf("convene while another is convened: %v", err)
	}
	c.ragu.convening = false
	if c.councilRunning() {
		t.Error("a council that said it is done still sits")
	}
	c.ragu.councils[0].Done = time.Time{}
	c.mu.Lock()
	c.sessions["s1"].dead = true
	c.mu.Unlock()
	if c.councilRunning() {
		t.Error("a council whose session ended still sits")
	}
	// Off, he does nothing but report.
	cfg := *c.cfg()
	cfg.Arch.Raguel.On = false
	c.useConfig(cfg)
	if _, err := c.councilOp(0, ipc.CouncilArgs{Do: ipc.CouncilTally}); err == nil || !strings.Contains(err.Error(), "off") {
		t.Errorf("a tally while off: %v", err)
	}
	if r, err := c.councilOp(0, ipc.CouncilArgs{}); err != nil || r.On {
		t.Errorf("the report while off: %+v, %v", r, err)
	}
}

func TestRaguelsArchRowListsTheWaitingPlans(t *testing.T) {
	c, folder := councilCore(t)
	where := t.TempDir()
	l := &c.ragu.councils[0]
	l.Done = time.Now()
	for i, title := range []string{"Fewer sleeps", "A cheaper model"} {
		p := *testPlan(where, title)
		p.N = i + 1
		l.Plans = append(l.Plans, p)
	}
	l.Plans[1].Said = council.No
	var row ipc.ArchAgent
	for _, a := range c.archList().Agents {
		if a.Name == "raguel" {
			row = a
		}
	}
	if row.Nickname != brand.Ragu || row.Icon != "🤝" || !row.On || !strings.Contains(row.Status, "Last council 2026-11-21: 2 plans, 1 waiting for your word.") || !strings.Contains(row.Status, "He holds one when you ask.") {
		t.Fatalf("row = %+v", row)
	}
	if len(row.Actions) != 1 || row.Actions[0].Label != "Hold a council now" || row.Actions[0].Op != ipc.OpCouncil || !strings.Contains(row.Actions[0].Ask, "Opus 5.5") || !strings.Contains(row.Actions[0].Ask, folder) || strings.Contains(string(row.Actions[0].Args), `"ok"`) {
		t.Errorf("actions = %+v", row.Actions)
	}
	if len(row.Items) != 1 || row.Items[0].Text != "1. Fewer sleeps" || !strings.Contains(row.Items[0].Detail, brand.Gabriel+" 🪽") {
		t.Fatalf("items = %+v", row.Items)
	}
	acts := row.Items[0].Actions
	if len(acts) != 2 || acts[0].Label != "Yes" || !strings.Contains(acts[0].Ask, where) || !strings.Contains(acts[0].Ask, "Write tools/wait.sh.") || acts[1].Label != "No" || acts[1].Ask != "" {
		t.Errorf("the plan's actions = %+v", acts)
	}
	var a ipc.CouncilArgs
	if err := json.Unmarshal(acts[0].Args, &a); err != nil || a.Do != ipc.CouncilYes || fmt.Sprint(a.Plans) != "[1]" || a.OK {
		t.Errorf("yes args %s: %v", acts[0].Args, err)
	}
	keys := map[string]bool{}
	for _, s := range row.Settings {
		keys[s.Key] = true
	}
	for _, k := range []string{"arch.raguel.on", "arch.raguel.every", "arch.raguel.plans"} {
		if !keys[k] {
			t.Errorf("no setting %s", k)
		}
	}
	// While one sits, it says so and offers no other.
	l.Done = time.Time{}
	if n := c.raguelNow(); !strings.Contains(n.status, "Holding a council now, in council-2026-11-21") || len(n.actions) != 0 {
		t.Errorf("while one sits: %+v", n)
	}
}

func TestAYesStartsThePlansBriefAsAJot(t *testing.T) {
	folder, where := t.TempDir(), t.TempDir()
	c := prepare(t, agentConfig{extra: fmt.Sprintf("[arch.raguel]\nfolder = %q\n", folder)})
	plan := *testPlan(where, "Fewer sleeps")
	plan.N, plan.Brief = 1, "Write tools/wait.sh, and check it."
	b, _ := json.Marshal(raguelSaved{Councils: []council.Council{{Date: "2026-11-21", Started: time.Now(), Days: 7, Session: "sgone", Name: "council-2026-11-21", Folder: folder, Most: 5, Plans: []council.Plan{plan}}}})
	if err := os.MkdirAll(c.paths.State, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.paths.State, "raguel.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	c.up()
	var r ipc.CouncilResult
	if err := c.call(ipc.OpCouncil, ipc.CouncilArgs{Do: ipc.CouncilYes, Plans: []int{1}, OK: true}, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Jots) != 1 || r.Jots[0].Plan != 1 || r.Jots[0].Cwd != where || r.Jots[0].Name != "Fewer sleeps" {
		t.Fatalf("jots = %+v", r.Jots)
	}
	task, err := os.ReadFile(c.paths.TaskFile(r.Jots[0].Session))
	if err != nil || !strings.HasPrefix(string(task), "Write tools/wait.sh, and check it.") || !strings.Contains(string(task), `plan 1 of `+brand.Raguel+`'s council of 2026-11-21, "Fewer sleeps"`) {
		t.Errorf("task.md = %q, %v", task, err)
	}
	if r.Latest == nil || r.Latest.Plans[0].Said != council.Yes || r.Latest.Plans[0].Jot != r.Jots[0].Session {
		t.Errorf("the council = %+v", r.Latest)
	}
	// In the group Council.
	row, _ := c.row(r.Jots[0].Session)
	var group string
	for _, g := range c.group(ipc.GroupArgs{Do: ipc.GroupList}).Groups {
		if g.ID == row.Group {
			group = g.Name
		}
	}
	if group != "Council" {
		t.Errorf("the jot is in group %q (%q), want Council", group, row.Group)
	}
	// It started once.
	if err := c.call(ipc.OpCouncil, ipc.CouncilArgs{Do: ipc.CouncilYes, Plans: []int{1}, OK: true}, &r); err == nil || !strings.Contains(err.Error(), "started already") {
		t.Errorf("a second yes: %v", err)
	}
}
