package core

// Routing (DESIGN §6.4). A task spawned on auto runs on the profile with
// the lowest cost per successful task among those accurate enough for its
// class. Accuracy starts from the config's guesses and moves with the
// user's own outcomes, which are folded from the journal like every row's
// state: a rating, and a task run again on a more accurate profile, which
// says the first session fell short. The same choice hands a Claude Code
// session's subtasks to cheaper profiles, and carries a task on when its
// session stops at a usage limit.

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/route"
	"github.com/irie-people/irie/internal/statusline"
)

// settle is how long after its first turn ended a session counts as a
// success, unless the user rated it or ran its task again.
const settle = 30 * time.Minute

// escalateWithin is how soon a task spawned again on a more accurate
// profile says its first session fell short.
const escalateWithin = 2 * time.Hour

// memoryWeight is what a local model's memory costs per task, relative to
// a task on the strongest profile, when it takes the whole budget: a
// small cost, so memory weighs without a local model ever costing tokens.
const memoryWeight = 0.01

// resultFile holds what a session's agent said at the end of its last
// turn, whole; the journal keeps it cut.
const resultFile = "result.md"

// outcome is what a session's events say about its route and how it went.
// Nothing in it is stored.
type outcome struct {
	profile    string
	class      string
	hash       string // the task's fingerprint
	routed     bool
	reason     string // why auto chose the profile
	parent     string // the session that delegated it
	continues  string // the session it carries on from after a usage limit
	spawned    time.Time
	ended      time.Time // when a turn first ended
	limited    bool      // a turn stopped at a usage limit
	rated      string    // good or bad
	escalated  bool
	failedOver bool
	cost       *float64 // from status.json, for a session that is gone
}

func (o *outcome) apply(e event.Event) {
	switch e.Kind {
	case event.SpawnRequested:
		o.profile, o.class, o.hash, o.reason = e.Str("profile"), e.Str("class"), e.Str("task_hash"), e.Str("route")
		o.parent, o.continues, o.spawned = e.Str("parent"), e.Str("continues"), e.Time()
		o.routed, _ = e.Data["routed"].(bool)
	case event.TurnEnded:
		if o.ended.IsZero() && e.Agent == event.Main {
			o.ended = e.Time()
		}
	case event.TurnFailed:
		if e.Str("error") == "rate_limit" {
			o.limited = true
		}
	case event.OutcomeRated:
		o.rated = e.Str("verdict")
	case event.OutcomeEscalated:
		o.escalated = true
	case event.RouteFailover:
		o.failedOver = true
	}
}

// verdict is how a session went, as far as Irie can tell: the user's
// rating; else a miss when its task ran again on a more accurate profile;
// else a success once a turn has ended and settle has passed. A session
// stopped by a usage limit, or with no turn ended, says nothing about
// accuracy.
func (o *outcome) verdict(now time.Time) (good, known bool) {
	switch {
	case o.class == "":
		return false, false
	case o.rated == ipc.Good:
		return true, true
	case o.rated == ipc.Bad, o.escalated:
		return false, true
	case o.limited || o.ended.IsZero():
		return false, false
	}
	return true, now.Sub(o.ended) >= settle
}

// outcome returns a session's outcome, making it. The caller holds c.mu.
func (c *Core) outcome(id string) *outcome {
	o := c.outcomes[id]
	if o == nil {
		o = &outcome{}
		c.outcomes[id] = o
	}
	return o
}

// loadOutcomes folds the journal into every session's outcome, those gone
// included, and reads what a gone session cost from its status.json.
func (c *Core) loadOutcomes() {
	evs, err := journal.Read(c.paths.Journal(), time.Time{})
	if err != nil {
		c.log.Warn("journal", "err", err)
	}
	for _, e := range evs {
		switch e.Kind {
		case event.SpawnRequested, event.TurnEnded, event.TurnFailed, event.OutcomeRated, event.OutcomeEscalated, event.RouteFailover:
			c.outcome(e.Session).apply(e)
		}
	}
	for id, o := range c.outcomes {
		if o.class == "" {
			continue
		}
		var u statusline.Usage
		if b, err := os.ReadFile(c.paths.StatusFile(id)); err == nil && json.Unmarshal(b, &u) == nil {
			o.cost = u.CostUSD
		}
	}
}

// stats are the user's outcomes, by profile and class. The caller holds
// c.mu.
func (c *Core) stats(now time.Time) route.Stats {
	var s route.Stats
	for id, o := range c.outcomes {
		good, known := o.verdict(now)
		if !known {
			continue
		}
		cost := o.cost
		if x := c.sessions[id]; x != nil {
			if x.local {
				cost = nil
			} else if x.usage.CostUSD != nil {
				cost = x.usage.CostUSD
			}
		}
		s.Add(o.profile, o.class, good, cost)
	}
	return s
}

// strongest is the route profile with the highest starting cost, which
// costs are measured against.
func strongest(r config.Route) string {
	best, cost := "", -1.0
	for _, p := range r.Profiles {
		if c, ok := r.Cost[p]; ok && c > cost {
			best, cost = p, c
		}
	}
	return best
}

// planPressure is how much more a token of the user's Claude plan costs as
// its five hours fill: from route.pressure percent used, up to five times
// at the limit. At the limit of either window, the plan cannot take a task.
func (c *Core) planPressure(plan ipc.Plan) (factor float64, blocked bool, note string) {
	r := c.cfg().Route
	for _, w := range []*ipc.Window{plan.FiveHour, plan.SevenDay} {
		if w != nil && w.Used >= 100 {
			note = "the plan's limit is reached"
			if !w.ResetsAt.IsZero() {
				note += "; it resets at " + w.ResetsAt.Local().Format("15:04")
			}
			return 1, true, note
		}
	}
	if plan.FiveHour == nil || r.Pressure >= 100 || plan.FiveHour.Used <= r.Pressure {
		return 1, false, ""
	}
	return 1 + 4*(plan.FiveHour.Used-r.Pressure)/(100-r.Pressure), false, fmt.Sprintf("plan %.0f%% used", plan.FiveHour.Used)
}

// onPlan reports whether a profile runs on the user's Claude plan: Claude
// Code with no gateway of its own.
func onPlan(p config.Profile) bool {
	return p.Harness == "claude-code" && p.Local == nil && p.Env["ANTHROPIC_BASE_URL"] == ""
}

// localReady says whether a local profile can take a task now: Irie's
// Ollama server runs, the profile has a model, and the machine can hold it.
// With probe, it starts the server when it must; else it says so.
func (c *Core) localReady(name string, p config.Profile, probe bool) (need, budget int64, note string, ok bool) {
	if isLlama(p) {
		return c.llamaReady(name, p)
	}
	o, own, err := c.ollama(probe)
	if err != nil {
		if err == errLocalDown {
			return 0, 0, "Irie's Ollama server is not running", false
		}
		return 0, 0, err.Error(), false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	model, err := c.localModel(ctx, o, own, name, p)
	if err != nil {
		return 0, 0, err.Error(), false
	}
	ms, err := c.localModels(ctx, o, own)
	if err != nil {
		return 0, 0, err.Error(), false
	}
	i := slices.IndexFunc(ms, func(m ipc.LocalModel) bool { return m.Name == model })
	if i < 0 {
		return 0, 0, "no model " + model + "; irie setup local", false
	}
	m := ms[i]
	machine := c.readMachine()
	budget = localmodel.Budget(c.cfg().Local.MemoryGiB, machine)
	switch {
	case m.Why != "":
		return 0, 0, model + " " + m.Why, false
	case !m.Fits:
		return 0, 0, model + " is too big for this machine", false
	case m.Loaded:
		return m.Need, budget, model + " is in memory", true
	}
	c.mu.Lock()
	freed := c.localMem
	c.mu.Unlock()
	if free := machine.Available + freed - localmodel.Reserve(machine); machine.Total > 0 && m.Need > free {
		return 0, 0, fmt.Sprintf("%s needs %s and %s is free", model, localmodel.Bytes(m.Need), localmodel.Bytes(max(free, 0))), false
	}
	return m.Need, budget, model, true
}

// choose weighs the profiles in only for a task of class (DESIGN §6.4):
// accuracy from the starting guesses and the user's outcomes, cost per
// attempt from the guesses and what sessions cost, the plan's pressure,
// and whether a local model fits now. With probe, a local profile that may
// win is checked, starting Irie's Ollama server; without, it is judged as
// it stands. The caller holds no lock.
func (c *Core) choose(class, why string, only []string, probe bool) route.Choice {
	r := c.cfg().Route
	c.mu.Lock()
	stats := c.stats(time.Now())
	plan := c.plan
	c.mu.Unlock()
	ref := strongest(r)
	factor, blocked, planNote := c.planPressure(plan)
	var cands []route.Candidate
	for _, name := range only {
		prior, ok := r.Prior(name)
		p, isProfile := c.profile(name)
		if !ok || !isProfile {
			continue
		}
		counts := stats.Counts(name, class)
		cand := route.Candidate{
			Profile: name, Accuracy: route.Accuracy(prior.Accuracy[class], r.PriorWeight, counts),
			Outcomes: counts.Good + counts.Bad, Cost: prior.Cost, Usable: true,
		}
		if mean, ok := stats.MeanCost(name, class); ok {
			if refMean, ok := stats.MeanCost(ref, class); ok && refMean > 0 {
				cand.Cost = mean / refMean
			}
		}
		switch {
		case p.Local != nil && cand.Accuracy < r.MinAccuracy:
			// Below the floor it cannot win, so it is not probed.
			cand.Usable, cand.Note = false, "not accurate enough for "+class+" work"
		case p.Local != nil:
			need, budget, note, ok := c.localReady(name, p, probe)
			cand.Usable, cand.Note = ok, note
			if ok && budget > 0 {
				cand.Cost += memoryWeight * float64(need) / float64(budget)
			}
		case onPlan(p):
			cand.Cost *= factor
			cand.Note = planNote
			if blocked {
				cand.Usable = false
			}
		}
		cands = append(cands, cand)
	}
	return route.Choose(class, why, cands, r.MinAccuracy)
}

// parentProfile is the profile a delegated or carried-on task must be
// cheaper than: its parent session's, else the strongest.
func (c *Core) parentProfile(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if x := c.sessions[id]; x != nil {
		return x.profile
	}
	if o := c.outcomes[id]; o != nil && o.profile != "" {
		return o.profile
	}
	return strongest(c.cfg().Route)
}

// autoRoute picks a spawn's profile. A delegated task, or one carried on
// after a usage limit, runs only on a profile cheaper than the session it
// comes from, and only on one accurate enough.
func (c *Core) autoRoute(a ipc.SpawnArgs, class, why string) (route.Choice, error) {
	r := c.cfg().Route
	only, from := r.Profiles, ""
	switch {
	case a.Delegate && a.Parent != "":
		from = a.Parent
	case a.Continues != "":
		from = a.Continues
	}
	parent := ""
	if from != "" || a.Delegate {
		parent = strongest(r)
		if from != "" {
			parent = c.parentProfile(from)
		}
		only = route.Cheaper(parent, only, r.Priors())
	}
	ch := c.choose(class, why, only, true)
	switch {
	case parent != "" && !ch.Met:
		return ch, ipc.Errorf(ipc.CodeNeedsStronger, "this looks like %s work, and no profile cheaper than +%s is accurate enough for it (%s)", class, parent, ch.Reason)
	case ch.Profile == "":
		return ch, ipc.Errorf(ipc.CodeBadArgs, "auto: %s", ch.Reason)
	}
	return ch, nil
}

// escalate notes that a task ran before, not long ago, on a less accurate
// profile: the user running it again says that session fell short. A
// session rated good keeps its rating. The caller holds c.mu.
func (c *Core) escalate(hash, class, profile string, now time.Time) {
	pr, ok := c.cfg().Route.Prior(profile)
	if !ok {
		return
	}
	for _, id := range slices.Sorted(maps.Keys(c.outcomes)) {
		o := c.outcomes[id]
		x := c.sessions[id]
		if x == nil || o.hash != hash || o.profile == profile || o.rated == ipc.Good || o.escalated || now.Sub(o.spawned) > escalateWithin {
			continue
		}
		if op, ok := c.cfg().Route.Prior(o.profile); ok && op.Accuracy[class] < pr.Accuracy[class] {
			c.record(x, event.Event{Source: event.SourceIrie, Kind: event.OutcomeEscalated, Data: map[string]any{"profile": profile}})
		}
	}
}

// delegateNote is what a Claude Code session is told about delegating, with
// the profiles it can hand work to.
const delegateNote = `[Irie] To save tokens, you can hand a small, self-contained subtask to a cheaper model: run ` + "`" + `"$IRIE_BIN" ctl delegate "<subtask>"` + "`" + ` with Bash. It runs the subtask in this directory on the cheapest profile accurate enough for it (%s), waits until it is done, and prints the result; it refuses a subtask that needs your accuracy. Delegate mechanical work, such as renames, docstrings, simple tests and summaries, on files you are not editing yourself, and keep planning, design and review. Run a long subtask in the background.`

// delegateHint is the note for a session that may delegate: a Claude Code
// session on a cloud profile with cheaper ones to hand work to, which was
// not delegated itself. The caller holds c.mu.
func (c *Core) delegateHint(x *session) string {
	r := c.cfg().Route
	if !r.Delegate || x.harness != "claude-code" || x.local || x.out == nil || x.out.parent != "" {
		return ""
	}
	cheaper := route.Cheaper(x.profile, r.Profiles, r.Priors())
	if len(cheaper) == 0 {
		return ""
	}
	names := make([]string, len(cheaper))
	for i, p := range cheaper {
		names[i] = "+" + p
	}
	return fmt.Sprintf(delegateNote, strings.Join(names, ", "))
}

// failoverNote opens the task a failover carries on.
func failoverNote(profile string) string {
	return fmt.Sprintf("[Irie] This task ran on +%s, which stopped at its usage limit. What it did so far is in this directory: check it, then finish the task.\n\n", profile)
}

// failover carries a task on after its session stopped at a usage limit
// (DESIGN §6.4): in the same directory, on the cheapest profile accurate
// enough for its class that is not that one. When none is, the task waits
// for the limit to reset, as Claude Code does. A session fails over once.
func (c *Core) failover(id string) {
	c.mu.Lock()
	x := c.sessions[id]
	if x == nil || x.local || x.out == nil || x.out.failedOver {
		c.mu.Unlock()
		return
	}
	name, named, cwd, profile, tutor := x.name, x.named, x.cwd, x.profile, x.tutor
	c.mu.Unlock()
	task, err := os.ReadFile(c.paths.TaskFile(id))
	if err != nil {
		c.log.Warn("failover", "session", id, "err", err)
		return
	}
	note := func(data map[string]any) {
		c.mu.Lock()
		if x := c.sessions[id]; x != nil {
			c.record(x, event.Event{Source: event.SourceIrie, Kind: event.RouteFailover, Data: data})
		}
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
	}
	// A session you closed meanwhile is done: nothing carries it on.
	c.mu.Lock()
	closed := c.sessions[id] == nil
	c.mu.Unlock()
	if closed {
		return
	}
	// The task carries on under its name, which is the user's only when it
	// was theirs; else Claude Code may title the new session (title.go).
	if tutor == "" {
		tutor = config.TutorOff
	}
	s, err := c.spawn(ipc.SpawnArgs{Task: failoverNote(profile) + string(task), Cwd: cwd, Profile: config.Auto, Name: name, Continues: id, Tutor: tutor}, named)
	if err != nil {
		c.log.Info("no failover", "session", id, "err", err)
		note(map[string]any{"reason": "it waits for the limit to reset: " + err.Error()})
		return
	}
	note(map[string]any{"to": s.ID, "profile": s.Profile})
}

// rate takes the user's rating of a session's work.
func (c *Core) rate(a ipc.RateArgs) error {
	if a.Verdict != ipc.Good && a.Verdict != ipc.Bad {
		return ipc.Errorf(ipc.CodeBadArgs, "a rating is good or bad, not %q", a.Verdict)
	}
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	c.record(x, event.Event{Source: event.SourceUser, Kind: event.OutcomeRated, Data: map[string]any{"verdict": a.Verdict}})
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return nil
}

// routeDry is `irie ctl route`: where a task would run, and why, without
// running it.
func (c *Core) routeDry(a ipc.RouteArgs) (route.Choice, error) {
	if strings.TrimSpace(a.Task) == "" {
		return route.Choice{}, ipc.Errorf(ipc.CodeBadArgs, "empty task")
	}
	class, why := route.Classify(a.Task, c.cfg().Route.Words)
	ch, err := c.autoRoute(ipc.SpawnArgs{Task: a.Task, Delegate: a.Parent != "", Parent: a.Parent}, class, why)
	if err != nil && ch.Class == "" {
		return ch, err
	}
	return ch, nil
}

// result is what a session's agent said at the end of its last turn.
func (c *Core) result(a ipc.ResultArgs) (ipc.ResultResult, error) {
	c.mu.Lock()
	_, ok := c.sessions[a.Session]
	c.mu.Unlock()
	if !ok {
		return ipc.ResultResult{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	b, err := os.ReadFile(filepath.Join(c.paths.Session(a.Session), resultFile))
	if err != nil && !os.IsNotExist(err) {
		return ipc.ResultResult{}, err
	}
	return ipc.ResultResult{Text: string(b)}, nil
}

// autoNote is what the + list says auto does: where each class goes now.
func (c *Core) autoNote() string {
	var parts []string
	for _, class := range route.Classes {
		ch := c.choose(class, "", c.cfg().Route.Profiles, false)
		to := ch.Profile
		if to == "" {
			to = "none"
		}
		parts = append(parts, class+"→"+to)
	}
	return strings.Join(parts, " · ") + " · by cost per success"
}

// toolHarness runs one of Irie's own commands in a session, such as `irie
// setup local`, so the user answers it in the slot. It takes no task.
type toolHarness struct{}

func (toolHarness) ID() string { return "tool" }

func (toolHarness) Levels() harness.Levels { return harness.Generic }

func (toolHarness) Launch(p config.Profile, s harness.Spawn) ([]string, map[string]string, map[string][]byte, error) {
	return append([]string{p.Command}, p.Args...), maps.Clone(p.Env), map[string][]byte{harness.TaskFile: []byte(s.Task)}, nil
}

func (toolHarness) Normalize([]byte) ([]event.Event, error) { return nil, fmt.Errorf("tool: no hooks") }

func (toolHarness) Respond(event.Event, harness.HookReply) []byte { return nil }

// tool spawns a tool session.
func (c *Core) tool(a ipc.ToolArgs) (ipc.Session, error) {
	var args []string
	var name, title string
	switch a.Name {
	case ipc.ToolSetup:
		args, name, title = []string{"setup", "local"}, "setup-local", "set up local models"
	case ipc.ToolModels:
		args, name, title = []string{"ctl", "local"}, "models", "local models and their memory"
	default:
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "no tool %q; the tools are %s and %s", a.Name, ipc.ToolSetup, ipc.ToolModels)
	}
	p := config.Profile{Harness: "tool", Command: c.bin, Args: args, Env: map[string]string{}}
	return c.spawnProfile(ipc.SpawnArgs{Task: title, Name: name}, false, "tool:"+a.Name, p, nil, map[string]any{})
}

// saveResult keeps a turn's last message whole, for `irie ctl delegate`.
// The caller holds c.mu.
func (c *Core) saveResult(x *session, msg string) {
	if err := writeFileAtomic(filepath.Join(c.paths.Session(x.id), resultFile), []byte(msg)); err != nil {
		c.log.Warn("result", "session", x.id, "err", err)
	}
}

func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
