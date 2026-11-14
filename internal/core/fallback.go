package core

// A model's own usage limit (DESIGN §6.4). Beside the plan's session and
// weekly limits, which every model shares, a model family can have a limit
// of its own, such as Fable's. When one stops a session's turn, Claude
// Code's answer is /model: a model outside that family keeps working. A
// profile with a fallback_model does that on its own. Irie types /model
// into the agent's terminal, lets the switch skip the confirmation Claude
// Code asks while the prompt cache is warm, puts back the default for new
// sessions that /model saves in the user's settings, and once the model
// has changed tells the agent to carry on. The agent's process goes on,
// and with it its work in the background and its teammates. While the
// limit holds, new sessions of the profile start on the fallback.

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
)

// limitHold is how long after a model's own limit stopped a turn new
// sessions start on their profile's fallback. Claude Code does not always
// say when such a limit resets; a session that starts on the model after
// the hold and finds it still out switches in place.
const limitHold = time.Hour

// switchWait is how long a switch waits for Claude Code to report the new
// model before it gives up, leaving the session stopped at its limit.
const switchWait = 30 * time.Second

// switchSettle is how long after the switch Irie waits before it tells the
// agent to carry on: when the switch ends Claude Code's own wait for the
// limit to reset, Claude Code carries on by itself, and the note is left
// out.
const switchSettle = 2 * time.Second

// savedWait is how long Irie looks for the default that /model saves in
// the user's settings, to put the user's own back.
const savedWait = 5 * time.Second

// modelSwitch is a switch Irie is making, from the failed turn that called
// for it until it is done or given up.
type modelSwitch struct {
	from, to string
	limit    string        // what the failed turn said
	done     chan struct{} // closed when model.switched names to
	got      []string      // the names model.switched gave the new model
}

// families are the model families that can have a usage limit of their own,
// as Claude Code's messages name them.
var families = []string{"fable", "opus", "sonnet", "haiku"}

// family is the family of a Claude model, such as fable for
// claude-fable-5-1, fable[1m] or the alias fable; "" for any other model.
func family(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, f := range families {
		if m == f || strings.HasPrefix(m, "claude-"+f) || strings.HasPrefix(m, f+"[") {
			return f
		}
	}
	return ""
}

// ownLimit reports whether a failed turn's message says model's own usage
// limit stopped it, as in "You've reached your Fable limit" or "You've hit
// your Opus limit · resets 3:45pm", rather than the plan's session or
// weekly limit, which switching models does not lift.
func ownLimit(message, model string) bool {
	f := family(model)
	return f != "" && strings.Contains(strings.ToLower(message), f+" limit")
}

// fallbackFor is the switch a failed turn e of session x calls for, if
// any: from the model it runs, whose own usage limit stopped the turn, to
// its profile's fallback model. It notes the limit for new sessions, and
// marks the switch under way, so one session makes one switch at a time.
// The caller holds c.mu, and runs fallBack with the switch.
func (c *Core) fallbackFor(x *session, e event.Event) *modelSwitch {
	from := x.runs()
	said := e.Str("message") + " " + e.Str("details")
	if e.Kind != event.TurnFailed || e.Agent != event.Main || x.harness != "claude-code" || x.local || !ownLimit(said, from) {
		return nil
	}
	// A hook's event has its time once it is recorded, in the record.
	at := e.Time()
	if at.IsZero() {
		at = time.Now()
	}
	c.noteLimit(family(from), at)
	to := c.cfg().Profiles[x.profile].FallbackModel
	if to == "" || family(to) == family(from) || x.switching != nil || x.dead {
		return nil
	}
	x.switching = &modelSwitch{from: from, to: to, limit: e.Str("message"), done: make(chan struct{})}
	return x.switching
}

// noteLimit notes that family f's own usage limit stopped a turn at at.
// The caller holds c.mu.
func (c *Core) noteLimit(f string, at time.Time) {
	if f != "" && at.After(c.limits[f]) {
		c.limits[f] = at
	}
}

// runs is the model session x's agent runs now: the one it switched to,
// else the one it started on, else the one its status line names.
func (x *session) runs() string {
	switch {
	case x.track.model != "":
		return x.track.model
	case x.model != "":
		return x.model
	}
	return x.usage.Model
}

// startsOn is the model a launch of profile p runs: its fallback while its
// own model's limit holds, with why. The caller holds c.mu.
func (c *Core) startsOn(p config.Profile, now time.Time) (model, why string) {
	f := family(p.Model)
	at, ok := c.limits[f]
	if p.FallbackModel == "" || f == "" || !ok || now.Sub(at) >= limitHold {
		return p.Model, ""
	}
	return p.FallbackModel, fmt.Sprintf("starts on %s: %s's own usage limit stopped a session at %s", p.FallbackModel, p.Model, at.Local().Format("15:04"))
}

// fallBack makes switch sw of session id, in place, then tells its agent
// to carry on. Its route.fallback says how it went: a reason when the
// session waits for the limit to reset instead. It holds no lock.
func (c *Core) fallBack(id string, sw *modelSwitch) {
	defer func() {
		c.mu.Lock()
		if x := c.sessions[id]; x != nil && x.switching == sw {
			x.switching = nil
		}
		c.mu.Unlock()
	}()
	data := map[string]any{"from": sw.from, "to": sw.to, "limit": sw.limit}
	err := c.switchModel(id, sw)
	if err != nil {
		data["reason"] = "it waits for the limit to reset: " + err.Error()
		c.log.Info("no fallback", "session", id, "to", sw.to, "err", err)
	} else {
		c.log.Info("fell back", "session", id, "from", sw.from, "to", sw.to)
	}
	if !c.noteFallback(id, data) || err != nil {
		return
	}
	time.Sleep(switchSettle)
	if err := c.typeTo(id, fallbackNote(sw.from, sw.to), c.carriesOn, nil); err != nil {
		c.log.Info("fallback: no carry-on", "session", id, "err", err)
	}
}

// switchModel types /model into session id's agent and waits for Claude
// Code to report the new model, then puts back the user's default model
// for new sessions, which /model saves. A session started with its slash
// commands off cannot switch.
func (c *Core) switchModel(id string, sw *modelSwitch) error {
	spec, err := host.ReadSpec(c.paths.SpecFile(id))
	switch {
	case err != nil:
		return err
	case slices.Contains(spec.Argv, "--disable-slash-commands"):
		return fmt.Errorf("it started with --disable-slash-commands, which turns /model off: close and reopen it to start it without")
	}
	// One switch at a time, so each puts back the user's own default and
	// none the one another switch saved.
	c.switchMu.Lock()
	defer c.switchMu.Unlock()
	dir := c.claudeDir(spec.Profile)
	was, savedErr := claude.SavedModel(dir)
	if savedErr != nil {
		c.log.Warn("fallback: the user's default model", "session", id, "dir", dir, "err", savedErr)
	}
	// /model goes only into the agent's own prompt with nothing typed in
	// it, as continue's word does, and Enter only once the prompt shows it.
	if err := c.typeTo(id, "/model "+sw.to, c.switchable, c.promptHolds); err != nil {
		return err
	}
	select {
	case <-sw.done:
	case <-time.After(switchWait):
		return fmt.Errorf("Claude Code did not report %s within %s", sw.to, switchWait)
	}
	if savedErr == nil {
		c.mu.Lock()
		names := append([]string{sw.to}, sw.got...)
		c.mu.Unlock()
		c.keepDefault(id, dir, was, names)
	}
	return nil
}

// keepDefault puts back the user's default model for new sessions, which
// /model replaced in their settings with one of names, the switch's model:
// as it was, or with none when they had none. A default that someone else
// changed meanwhile stays as they left it.
func (c *Core) keepDefault(id, dir string, was json.RawMessage, names []string) {
	var old string
	if json.Unmarshal(was, &old) == nil && slices.Contains(names, old) {
		return
	}
	for until := time.Now().Add(savedWait); ; time.Sleep(100 * time.Millisecond) {
		put, settled, err := claude.RestoreModel(dir, was, names)
		switch {
		case err != nil:
			c.log.Warn("fallback: the user's default model", "session", id, "dir", dir, "err", err)
			return
		case put:
			c.log.Info("fallback: put the user's default model back", "session", id, "model", string(was))
			return
		case settled, time.Now().After(until):
			return
		}
	}
}

// claudeDir is Claude Code's config folder for a session of profile name,
// whose settings hold the user's default model.
func (c *Core) claudeDir(name string) string {
	p, _ := c.profile(name)
	return claude.ConfigDir(home(), c.agentEnv(p))
}

// noteFallback records session id's route.fallback. It reports whether the
// session is still there to switch.
func (c *Core) noteFallback(id string, data map[string]any) bool {
	c.mu.Lock()
	x := c.sessions[id]
	if x == nil {
		c.mu.Unlock()
		return false
	}
	c.record(x, event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.RouteFallback, Data: data})
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return true
}

// fallbackNote is what the agent is told once its model has switched.
func fallbackNote(from, to string) string {
	return fmt.Sprintf("["+brand.Name+"] %s's own usage limit stopped your last turn, so this session now runs on %s. Carry on with your task from where you stopped.", from, to)
}

// switchable says why session x cannot take /model now, if it cannot: it
// must still be stopped at its failed turn, with nothing else waiting on
// the user, and its prompt must show with nothing typed in it. The caller
// holds c.mu.
func (c *Core) switchable(x *session, text string) error {
	if !failedTurn(x) {
		return ipc.Errorf(ipc.CodeBusy, "%s is no longer stopped at its limit", x.name)
	}
	return c.continuable(x, text)
}

// carriesOn says why session x's agent should not be told to carry on
// after its switch: Claude Code carried on by itself, or something else
// sent it on meanwhile. The caller holds c.mu.
func (c *Core) carriesOn(x *session, text string) error {
	if !failedTurn(x) {
		return ipc.Errorf(ipc.CodeBusy, "%s went on without it", x.name)
	}
	return c.continuable(x, text)
}

// modelSwitchAsk answers a PreModelSwitch: allow when it is the switch
// Irie is making for session x, so it needs no confirmation. Any other
// switch, such as the user's own /model, goes as Claude Code would take
// it. The caller holds c.mu.
func modelSwitchAsk(x *session, payload json.RawMessage) bool {
	if x.switching == nil {
		return false
	}
	var p struct {
		Event     string `json:"hook_event_name"`
		To        string `json:"to_model"`
		Requested string `json:"requested_model"`
	}
	if json.Unmarshal(payload, &p) != nil || p.Event != "PreModelSwitch" {
		return false
	}
	return switchedTo(x.switching.to, p.To, p.Requested)
}

// switchedTo reports whether a switch that asked for want went to model
// to, as requested: Claude Code names the model by its id, which may carry
// a suffix such as [1m], and the request as typed.
func switchedTo(want, to, requested string) bool {
	return requested == want || to == want || strings.HasPrefix(to, want+"[")
}

// modelSwitched takes a model.switched of session x: the switch Irie is
// making is done when it names its model. The caller holds c.mu.
func modelSwitched(x *session, e event.Event) {
	sw := x.switching
	if sw == nil || !switchedTo(sw.to, e.Str("model"), e.Str("requested")) {
		return
	}
	select {
	case <-sw.done:
	default:
		for _, k := range []string{"model", "requested"} {
			if v := e.Str(k); v != "" && !slices.Contains(sw.got, v) {
				sw.got = append(sw.got, v)
			}
		}
		close(sw.done)
	}
}
