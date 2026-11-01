package core

// The trod bot (DESIGN §4.10) keeps your sessions going while you are
// away at night, until their tasks are through. It works from trod.start
// to trod.end, once the Mac has had no keyboard or mouse input for
// trod.away, and it sends one session on trod.nudges times a night at
// most:
//
//   - at the end of a turn whose last message asks you something, it sends
//     the turn on, as the jot dog does (dog.go), telling the agent you are
//     away and to decide itself;
//   - at an AskUserQuestion, it turns the question away with the same note;
//   - every trodEvery seconds it looks at the stopped sessions, and types
//     a message to each that has something left: a question it asked before
//     the shift began, a turn an API error stopped, a jot that is not
//     through, or a usage limit that reset while the session stayed
//     stopped. Each waits trod.retry between two of those, and while
//     trod.max_working sessions work, the others wait their turn.
//
// Every note carries your rules and guidelines (trod.rules,
// trod.guidelines). It never answers for you, never approves a permission
// or a plan, and never acts on a session you closed, parked or froze, one
// that another session delegated, or one that stopped longer than
// trod.within ago. While any session has work left it keeps the Mac
// awake, so that one waiting for a usage limit to reset carries on when it
// does, as Claude Code itself does then.

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
)

// trodEvery is how often, in the core's ticks of a second, the trod bot
// looks at the stopped sessions.
const trodEvery = 10

// trodTypedFor is how soon a message the shift typed must come in as a
// prompt for that prompt to be the shift's rather than the user's.
const trodTypedFor = time.Minute

// maxTrodSends is how many of the shift's sends a session's state keeps.
const maxTrodSends = 64

// Why the trod bot sent a session on (trod.sent's reason).
const (
	trodQuestion = "question" // its agent asked you something
	trodError    = "error"    // an API error stopped its turn
	trodLimit    = "limit"    // a usage limit stopped it, and has reset
	trodLoose    = "loose"    // its jot is not through
)

// How the trod bot sent a session on (trod.sent's how).
const (
	trodAtStop = "stop"  // it sent the turn on at its end
	trodAtAsk  = "ask"   // it turned a question away
	trodTyped  = "typed" // it typed a message to the agent
)

// trodSend is one time the trod bot sent a session on.
type trodSend struct {
	at          time.Time
	reason, how string
}

// trodBy is how the trod bot's freezes and thaws name it (freeze.on's by).
const trodBy = "trod"

// planUsed is how full one of the plan's windows is now, in percent, by
// the newest status line: a window whose reset has passed is empty. It is
// false when the status line said nothing of the window.
func planUsed(w *ipc.Window, now time.Time) (float64, bool) {
	switch {
	case w == nil:
		return 0, false
	case !w.ResetsAt.IsZero() && !w.ResetsAt.After(now):
		return 0, true
	}
	return w.Used, true
}

// overPlan reports whether the plan is past trod.max_plan_use: in either
// of its windows, past which the bot sends nothing on, and in the week's,
// past which it lets the Mac sleep and freezes what would still spend the
// week's allotment, with how full the week is. The caller holds c.mu.
func (c *Core) overPlan(now time.Time, most float64) (either, week bool, weekUsed float64) {
	five, _ := planUsed(c.plan.FiveHour, now)
	weekUsed, _ = planUsed(c.plan.SevenDay, now)
	week = weekUsed >= most
	return week || five >= most, week, weekUsed
}

// passing are the API errors that may pass, after which the shift tries
// the turn again (StopFailure's error types). What only you can mend, such
// as a login or billing, waits for you.
var passing = map[string]bool{"overloaded": true, "server_error": true, "unknown": true, "max_output_tokens": true}

// sentSince is how often the shift sent a session on since from.
func sentSince(sends []trodSend, from time.Time) int {
	n := 0
	for _, s := range sends {
		if !s.at.Before(from) {
			n++
		}
	}
	return n
}

// lastSent is when the shift last sent a session on; zero if never.
func lastSent(sends []trodSend) time.Time {
	if len(sends) == 0 {
		return time.Time{}
	}
	return sends[len(sends)-1].at
}

func later(ts ...time.Time) time.Time {
	var out time.Time
	for _, t := range ts {
		if t.After(out) {
			out = t
		}
	}
	return out
}

// trodWatches reports whether the trod bot may send session x on: a
// Claude Code session that runs, which no other session delegated or
// carries on, and which you have not parked or frozen. A session you closed
// is gone. The caller holds c.mu.
func (c *Core) trodWatches(x *session) bool {
	return x.harness == "claude-code" && !x.dead && !x.track.parked && x.track.frozen == nil &&
		(x.out == nil || x.out.parent == "" && !x.out.failedOver)
}

// shift is the trod bot as it stands at now: whether it is on and in
// its hours, the hours of this shift or the last, and when it is out of
// them, nothing more. until is zero for a shift around the clock.
type shift struct {
	n         config.Trod
	from, to  time.Time
	in        bool
	until     time.Time // what the notes say the user is away until
	idle      time.Duration
	idleKnown bool
	away      bool // trod.away has passed with no input, or Irie cannot tell
}

// shiftAt is the shift at now. It reads the machine's idle time only when
// the shift is in its hours and needs it, so the caller does not hold c.mu.
func (c *Core) shiftAt(now time.Time) shift {
	n := c.cfg().Trod
	s := shift{n: n}
	s.from, s.to, s.in = n.Shift(now)
	s.in = s.in && n.On
	if n.Start != n.End {
		s.until = s.to
	}
	if !s.in {
		return s
	}
	s.away = true
	if n.Away > 0 {
		s.idle, s.idleKnown = machineIdle()
		s.away = !s.idleKnown || s.idle >= n.Away
	}
	return s
}

// works reports whether the shift steps in now.
func (s shift) works() bool { return s.in && s.away }

// lookTool finds a system tool on PATH, else where macOS keeps it; "" when
// it is in neither.
func lookTool(name, fallback string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if _, err := os.Stat(fallback); err == nil {
		return fallback
	}
	return ""
}

var hidIdle = regexp.MustCompile(`"HIDIdleTime" = (\d+)`)

// machineIdle is how long the machine has had no keyboard or mouse input:
// IOHIDSystem's HIDIdleTime, in nanoseconds, by ioreg (macOS). It is false
// where there is no ioreg or it says nothing.
func machineIdle() (time.Duration, bool) {
	bin := lookTool("ioreg", "/usr/sbin/ioreg")
	if bin == "" {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-c", "IOHIDSystem", "-d", "4", "-k", "HIDIdleTime").Output()
	if err != nil {
		return 0, false
	}
	m := hidIdle.FindSubmatch(out)
	if m == nil {
		return 0, false
	}
	ns, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(ns), true
}

// trodReply is the trod bot's answer to a hook event: a turn's end it
// sends on, or a question it turns away, and what it tells the agent.
type trodReply struct {
	how, note string
}

// trodAtHook is the trod bot at a hook event (DESIGN §4.10): at the end
// of a turn whose last message asks the user something, or at an
// AskUserQuestion, it tells the agent the user is away and to decide
// itself, unless a stop hook sent the turn on already, work of its own in
// the background will wake it, or the shift told it so before and it has
// not worked since, so its question waits for the user. The caller does
// not hold c.mu.
func (c *Core) trodAtHook(id string, evs []event.Event) trodReply {
	if n := c.cfg().Trod; !n.On || !n.Questions {
		return trodReply{}
	}
	how := ""
	for _, e := range evs {
		switch {
		case e.Kind == event.TurnEnded && e.Agent == event.Main && asksUser(e.Str("message")) && e.Data["stop_hook_active"] != true:
			if w, _ := e.Int("waiting"); w == 0 {
				how = trodAtStop
			}
		case e.Kind == event.InputNeeded && e.Str("reason") == event.ReasonQuestion && e.Vendor != nil && e.Vendor.HookEvent == "PreToolUse":
			how = trodAtAsk
		}
	}
	if how == "" {
		return trodReply{}
	}
	s := c.shiftAt(time.Now())
	if !s.works() {
		return trodReply{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	x := c.sessions[id]
	if x == nil || !c.trodWatches(x) || x.track.trodAnswered || sentSince(x.track.trod, s.from) >= s.n.Nudges {
		return trodReply{}
	}
	if over, _, _ := c.overPlan(time.Now(), s.n.MaxPlanUse); over {
		// Past the plan's use it may spend, its question waits for you.
		return trodReply{}
	}
	return trodReply{how: how, note: trodNote(s, trodQuestion, how, "", false)}
}

// trodEvent stands in for the event the trod bot answered: a turn's
// end it sent on, with what the agent had said, or a question it turned
// away, with the question.
func trodEvent(e event.Event, how string) event.Event {
	data := map[string]any{"reason": trodQuestion, "how": how}
	for _, k := range []string{"message", "question"} {
		if v := e.Str(k); v != "" {
			data[k] = v
		}
	}
	return event.Event{Source: event.SourceIrie, Agent: e.Agent, Kind: event.TrodSent, Vendor: e.Vendor, Data: data}
}

// trodDue is a message the trod bot types to a session.
type trodDue struct {
	id, reason, text string
	data             map[string]any
}

// trodNext is what the trod bot would type session x now and why,
// when something is due, and whether x still has work, for which the Mac
// stays awake: it works, it waits for work of its own or for a usage limit
// to reset, or the shift will send it on once trod.retry has passed. A
// session the shift sent on trod.nudges times this night is sent on no
// more, but the Mac stays awake while it works. reset is when the plan's
// usage limit last reset, as far as Irie knows. The caller holds c.mu.
func (c *Core) trodNext(x *session, now time.Time, s shift, reset time.Time) (d *trodDue, work bool) {
	t := &x.track
	if !c.trodWatches(x) {
		return nil, false
	}
	state := t.state(x.dead)
	switch {
	case state == ipc.StateWorking || state == ipc.StateStarting:
		return nil, true
	case t.waiting > 0:
		// Work of its own in the background, or a wakeup, will wake it.
		return nil, true
	case s.n.Within > 0 && now.Sub(t.stopped) > s.n.Within:
		return nil, false
	}
	capped := sentSince(t.trod, s.from) >= s.n.Nudges
	waits := now.Sub(later(t.stopped, lastSent(t.trod))) < s.n.Retry
	due := func(reason string, data map[string]any) (*trodDue, bool) {
		switch {
		case capped:
			return nil, false
		case waits:
			return nil, true
		}
		return &trodDue{id: x.id, reason: reason, data: data}, true
	}
	switch state {
	case ipc.StateNeedsYou:
		// Only a failed turn, with nothing else waiting on the user, such
		// as a permission prompt.
		e, ok := t.needs[event.Main]
		if !ok || len(t.needs) != 1 || e.Kind != event.TurnFailed {
			return nil, false
		}
		switch err := e.Str("error"); {
		case passing[err]:
			return due(trodError, map[string]any{"error": err})
		case err == "rate_limit":
			// While the plan is at its limit the session waits for the
			// reset, at which Claude Code carries the task on itself; the
			// shift steps in only when it stays stopped after that.
			r, limited := planLimit(c.plan, now)
			if limited {
				return nil, true
			}
			if c.autoContinueOff(x) {
				// Claude Code then asks the user what to do in its
				// terminal, which a typed message would answer.
				return nil, false
			}
			if now.Sub(later(reset, r)) < s.n.Retry {
				waits = true
			}
			return due(trodLimit, nil)
		}
		return nil, false
	case ipc.StateDone, ipc.StateIdle:
	default:
		return nil, false
	}
	switch {
	case t.asked:
		if !s.n.Questions || t.trodAnswered || capped {
			// Asked again after the shift's answer: it waits for the user.
			return nil, false
		}
		// A question waits on nobody but the user: it is answered at once.
		waits = false
		return due(trodQuestion, nil)
	case len(x.dog.loose) > 0:
		return due(trodLoose, nil)
	}
	return nil, false
}

// spendsAlone reports whether session x would spend the plan's use with
// no one sending it on: it works or starts, or it will wake on its own, by
// work of its own in the background or, stopped at a usage limit, at the
// reset, where Claude Code carries its task on. The caller holds c.mu.
func (c *Core) spendsAlone(x *session, now time.Time) bool {
	t := &x.track
	if !c.trodWatches(x) {
		return false
	}
	switch t.state(x.dead) {
	case ipc.StateWorking, ipc.StateStarting:
		return true
	case ipc.StateDone, ipc.StateIdle:
		return t.waiting > 0
	case ipc.StateNeedsYou:
		e, ok := t.needs[event.Main]
		return ok && len(t.needs) == 1 && e.Kind == event.TurnFailed && e.Str("error") == "rate_limit" && !c.autoContinueOff(x)
	}
	return false
}

// planLimit is when the plan's usage limit resets, by its windows at
// their limit in the newest status line, and whether that is still to
// come.
func planLimit(plan ipc.Plan, now time.Time) (time.Time, bool) {
	var reset time.Time
	for _, w := range []*ipc.Window{plan.FiveHour, plan.SevenDay} {
		if w != nil && w.Used >= 100 {
			reset = later(reset, w.ResetsAt)
		}
	}
	return reset, reset.After(now)
}

// trodSweep is the trod bot's look at the stopped sessions (DESIGN
// §4.10): it types a message to each that has something left and is due,
// in the list's order, while fewer than trod.max_working sessions work;
// it keeps the Mac awake while any has work left; and the sidebars hear
// when it starts and stops. Past trod.max_plan_use it sends nothing on;
// past it in the week's window it lets the Mac sleep and, while you are
// away, freezes each session it would have kept the Mac awake for, so
// that none goes over the week's allotment. It thaws those once the week
// is back under it in its hours; out of them, they wait for you. One
// sweep runs at a time.
func (c *Core) trodSweep() {
	if !c.trodSweeping.CompareAndSwap(false, true) {
		return
	}
	defer c.trodSweeping.Store(false)
	now := time.Now()
	s := c.shiftAt(now)
	c.mu.Lock()
	if r, _ := planLimit(c.plan, now); r.After(c.trodReset) && !r.After(now) {
		c.trodReset = r
	}
	over, overWeek, weekUsed := c.overPlan(now, s.n.MaxPlanUse)
	var dues []trodDue
	work := false
	if s.in {
		working := 0
		rows := c.list()
		for _, r := range rows {
			if r.State == ipc.StateWorking {
				working++
			}
		}
		for _, r := range rows {
			x := c.sessions[r.ID]
			if f := x.track.frozen; f != nil && f.Str("by") == trodBy && !overWeek {
				if err := c.thawLocked(x, "the week's plan use is back under the most it may use"); err != nil {
					c.log.Warn("trod bot: thaw", "session", x.id, "err", err)
				}
				continue
			}
			d, w := c.trodNext(x, now, s, c.trodReset)
			if overWeek && s.away && c.spendsAlone(x, now) {
				why := fmt.Sprintf("the week's plan use is at %.0f%%, the most it may use is %.0f%%", weekUsed, s.n.MaxPlanUse)
				if err := c.freezeLocked(x, why); err != nil {
					c.log.Warn("trod bot: freeze", "session", x.id, "err", err)
				}
				continue
			}
			work = work || w
			if d == nil || over || !s.away || s.n.MaxWorking > 0 && working >= s.n.MaxWorking {
				continue
			}
			d.text = trodNote(s, d.reason, trodTyped, c.trodDetail(x, d), true)
			dues = append(dues, *d)
			working++
		}
	}
	c.trodNow = trodNow{in: s.in, away: s.away, until: s.until, idle: s.idle, idleKnown: s.idleKnown, capped: over}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	c.keepAwake(s.in && s.n.KeepAwake && work && !overWeek)
	c.publishHeader()
	for _, d := range dues {
		if err := c.typeTo(d.id, d.text, c.trodTypes(d)); err != nil {
			c.log.Info("trod bot", "session", d.id, "reason", d.reason, "err", err)
		}
	}
}

// trodNow is the trod bot as its last sweep found it, for the
// sidebars' header and the report. c.mu guards it.
type trodNow struct {
	in, away  bool
	until     time.Time
	idle      time.Duration
	idleKnown bool
	capped    bool // past trod.max_plan_use
}

// trodTypes is the check typeTo makes for a message the trod bot
// types, just before it types it: the session still stopped as the shift
// found it, which then records that it sent the session on. The caller
// holds c.mu.
func (c *Core) trodTypes(d trodDue) func(*session, string) error {
	return func(x *session, text string) error {
		state := x.track.state(x.dead)
		_, failed := x.track.needs[event.Main]
		switch {
		case !c.trodWatches(x):
			return ipc.Errorf(ipc.CodeBusy, "%s is out of the trod bot's hands now", x.name)
		case state == ipc.StateNeedsYou && (!failed || len(x.track.needs) != 1 || x.track.needs[event.Main].Kind != event.TurnFailed),
			state != ipc.StateNeedsYou && state != ipc.StateDone && state != ipc.StateIdle:
			return ipc.Errorf(ipc.CodeBusy, "%s is %s now", x.name, state)
		case strings.ContainsAny(text, "\r\n") && !x.bracket:
			return ipc.Errorf(ipc.CodeBusy, "%s can’t take a message of several lines", x.name)
		}
		data := map[string]any{"reason": d.reason, "how": trodTyped}
		maps.Copy(data, d.data)
		c.record(x, event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.TrodSent, Data: data})
		return nil
	}
}

// trodDetail is what a typed note says of the session's own case: what
// its jot left, the error that stopped it. The caller holds c.mu.
func (c *Core) trodDetail(x *session, d *trodDue) string {
	switch d.reason {
	case trodLoose:
		return strings.Join(looseLines(x.dog.loose), " ")
	case trodError:
		e, _ := d.data["error"].(string)
		return e
	}
	return ""
}

// trodNote is what the trod bot tells an agent it sends on: why, and
// what to do, then the user's rules and guidelines, and what to leave for
// them. A note typed into a terminal is one line.
func trodNote(s shift, reason, how, detail string, oneLine bool) string {
	until := ""
	if !s.until.IsZero() {
		until = " until " + s.until.Format("15:04")
	}
	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	switch reason {
	case trodQuestion:
		if how == trodTyped {
			add("[Irie] The trod bot: you asked the user something, but they are away" + until + " and can’t answer.")
		} else {
			add("[Irie] The trod bot: the user is away" + until + " and can’t answer you.")
		}
		add("Don’t wait for them: if your question is about how to do your task, take the option you would recommend and carry on to the end of it. If it offers more than your task, leave that for the morning.")
	case trodError:
		add(fmt.Sprintf("[Irie] The trod bot: your last turn stopped on an API error (%s). The user is away%s, so carry on with your task from where you stopped.", detail, until))
	case trodLimit:
		add("[Irie] The trod bot: the usage limit that stopped you has reset. The user is away" + until + ", so carry on with your task from where you stopped.")
	case trodLoose:
		add("[Irie] The trod bot: your task is not through yet. " + detail)
		add("The user is away" + until + ", so finish it now: commit your work, then land it the way this repo’s AGENTS.md or CLAUDE.md says. If an install or another session holds you up, wait for it and try again. If you stopped on purpose, because your task says not to land it or only the user can decide what comes next, say so in one line.")
	}
	list := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		if oneLine {
			var b strings.Builder
			b.WriteString(title)
			for i, it := range items {
				fmt.Fprintf(&b, " %d. %s", i+1, strings.TrimSpace(it))
			}
			add(b.String())
			return
		}
		add(title)
		for _, it := range items {
			add("- " + strings.TrimSpace(it))
		}
	}
	list("The user’s rules for the night, which you must follow:", s.n.Rules)
	list("Their guidelines, for when you decide:", s.n.Guidelines)
	add("Leave for the user only what needs them: a login or password, an approval in their browser or on their phone, or a permission prompt, and say in one line what you left them and why. If nothing is left that you can do, say so in one line and stop.")
	if oneLine {
		return strings.Join(lines, " ")
	}
	return strings.Join(lines, "\n")
}

// autoContinueOff reports whether the user turned off Claude Code's own
// continue at a usage limit (autoContinueAtUsageLimit = false in its user
// settings), after which Claude Code shows its usage-limit menu in the
// terminal instead of waiting. The caller holds c.mu.
func (c *Core) autoContinueOff(x *session) bool {
	dir := c.cfg().Profiles[x.profile].Env["CLAUDE_CONFIG_DIR"]
	if dir == "" {
		dir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return false
	}
	var s struct {
		AutoContinue *bool `json:"autoContinueAtUsageLimit"`
	}
	return json.Unmarshal(b, &s) == nil && s.AutoContinue != nil && !*s.AutoContinue
}

// keepAwake keeps the Mac from sleeping while on: caffeinate -i, which
// lets the display sleep, for as long as the core runs (-w) or until it is
// off again. Where there is no caffeinate it does nothing.
func (c *Core) keepAwake(on bool) {
	c.awakeMu.Lock()
	defer c.awakeMu.Unlock()
	switch {
	case on && c.awake == nil:
		bin := lookTool("caffeinate", "/usr/bin/caffeinate")
		if bin == "" {
			return
		}
		cmd := exec.Command(bin, "-i", "-w", strconv.Itoa(os.Getpid()))
		if err := cmd.Start(); err != nil {
			c.log.Warn("trod bot: keep the Mac awake", "err", err)
			return
		}
		c.awake = cmd
		c.log.Info("trod bot: the Mac stays awake", "pid", cmd.Process.Pid)
		go func() {
			cmd.Wait()
			c.awakeMu.Lock()
			if c.awake == cmd {
				c.awake = nil
			}
			c.awakeMu.Unlock()
		}()
	case !on && c.awake != nil:
		c.awake.Process.Kill()
		c.awake = nil
		c.log.Info("trod bot: the Mac may sleep")
	}
}

// awakeNow reports whether the trod bot keeps the Mac awake.
func (c *Core) awakeNow() bool {
	c.awakeMu.Lock()
	defer c.awakeMu.Unlock()
	return c.awake != nil
}

// trodHeader is the trod bot in the sidebars' header, while it is in
// its hours. The caller holds c.mu.
func (c *Core) trodHeader(awake bool) *ipc.TrodShift {
	if !c.trodNow.in {
		return nil
	}
	h := &ipc.TrodShift{Away: c.trodNow.away, Awake: awake, Capped: c.trodNow.capped}
	if u := c.trodNow.until; !u.IsZero() {
		h.Until = &u
	}
	return h
}

// trodRow is what the trod bot did for session x in this shift or the
// last, for its row; nil when it did nothing. The caller holds c.mu.
func trodRow(t *track, from time.Time) *ipc.TrodRow {
	n := sentSince(t.trod, from)
	if n == 0 {
		return nil
	}
	last := t.trod[len(t.trod)-1]
	return &ipc.TrodRow{Sent: n, Last: last.at, Reason: last.reason}
}

// trodReport is the trod bot's report (irie ctl trod): whether it
// works now and why, the rules and guidelines it gives, and what it did in
// this shift or the last, by session in the list's order, with what it
// leaves for the user.
func (c *Core) trodReport() ipc.TrodResult {
	now := time.Now()
	s := c.shiftAt(now)
	awake := c.awakeNow()
	c.mu.Lock()
	defer c.mu.Unlock()
	n := s.n
	r := ipc.TrodResult{
		On: n.On, In: s.in, Away: s.away, Works: s.works(), Awake: awake, From: s.from, To: s.to,
		Start: n.Start, End: n.End, Rules: slices.Clone(n.Rules), Guidelines: slices.Clone(n.Guidelines),
		MaxPlanUse: n.MaxPlanUse, Sessions: []ipc.TrodSession{},
	}
	if s.idleKnown {
		secs := int64(s.idle / time.Second)
		r.IdleSeconds = &secs
	}
	r.Capped, _, _ = c.overPlan(now, n.MaxPlanUse)
	for _, w := range []struct {
		w   *ipc.Window
		out **float64
	}{{c.plan.FiveHour, &r.FiveHours}, {c.plan.SevenDay, &r.Week}} {
		if used, ok := planUsed(w.w, now); ok {
			*w.out = &used
		}
	}
	for _, row := range c.list() {
		x := c.sessions[row.ID]
		var sent []ipc.TrodSend
		for _, e := range x.track.trod {
			if !e.at.Before(s.from) {
				sent = append(sent, ipc.TrodSend{At: e.at, Reason: e.reason, How: e.how})
			}
		}
		// In its hours the report says what waits for the user; after
		// them, what the bot did, and what it froze.
		left := c.trodLeft(x, row, s)
		froze := x.track.frozen != nil && x.track.frozen.Str("by") == trodBy
		if len(sent) == 0 && !froze && (!s.in || left == "") {
			continue
		}
		r.Sessions = append(r.Sessions, ipc.TrodSession{ID: x.id, Name: x.name, State: row.State, Sent: sent, Left: left})
	}
	return r
}

// trodLeft says why the trod bot leaves a session for the user, when
// it does: it sent it on as often as a night allows, it waits on the user
// in its terminal, a failed turn needs the user to mend something, or its
// agent asked again after the shift told it to decide. The caller holds
// c.mu.
func (c *Core) trodLeft(x *session, row ipc.Session, s shift) string {
	t := &x.track
	if f := t.frozen; f != nil && f.Str("by") == trodBy {
		return "frozen by the trod bot: " + f.Str("reason")
	}
	if !c.trodWatches(x) || row.State == ipc.StateWorking {
		return ""
	}
	if e, ok := t.needs[event.Main]; ok && len(t.needs) == 1 && e.Kind == event.TurnFailed {
		if err := e.Str("error"); !passing[err] && err != "rate_limit" {
			return "its turn failed: " + err
		}
	} else if row.State == ipc.StateNeedsYou {
		return row.Detail
	}
	switch n := sentSince(t.trod, s.from); {
	case n > 0 && n >= s.n.Nudges:
		return fmt.Sprintf("sent on %d times, the most a night", n)
	case t.asked && t.trodAnswered && (row.State == ipc.StateDone || row.State == ipc.StateIdle):
		return "asks you again"
	}
	return ""
}
