package core

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
)

// toolInputKeys are the data a hook event takes from a tool's input, or
// quotes from its output, which journal.tool_input = "none" leaves out.
var toolInputKeys = []string{"summary", "question", "test_line", "ab_line"}

// hook takes one hook event from the shim (DESIGN §3.3) and records it.
// Nothing holds a call yet. A session that starts may be told it can
// delegate (DESIGN §6.4); a turn's last message is kept whole for
// delegation; a turn stopped at its model's own usage limit may switch
// models, and one at the plan's may fail over, and a model switch Irie
// makes goes ahead without asking (DESIGN §6.4); a jot whose turn ends
// before its work has landed may be sent back to finish it (DESIGN §4.8);
// while you are away at night, an agent that asks you something is told to
// decide itself (DESIGN §4.10); and a session or subagent that starts
// hears the word of the arch agents with it (DESIGN §4.11). Two turns in a
// row that fail on an API error boost the session (boost.go).
func (c *Core) hook(a ipc.HookArgs) (ipc.HookResult, error) {
	return c.takeHook(a, time.Time{})
}

// takeHook is hook for an event that came at at, or now when at is zero.
// One that came earlier is an event the shim kept while no core listened
// (missed.go). It is recorded at the time it came, marked late, and since
// its reply reaches no agent, nothing that only a reply carries is done
// for it: the jot dog's nudge, the trod bot's answer, the arch agents'
// word, Gabriel's guard and a note.
func (c *Core) takeHook(a ipc.HookArgs, at time.Time) (ipc.HookResult, error) {
	late := !at.IsZero()
	h, err := harnessFor(a.Harness)
	if err != nil {
		return ipc.HookResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	evs, err := h.Normalize(a.Payload)
	if err != nil {
		return ipc.HookResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	var call struct {
		Tool  string          `json:"tool_name"`
		Input json.RawMessage `json:"tool_input"`
		Cwd   string          `json:"cwd"`
	}
	json.Unmarshal(a.Payload, &call)
	var (
		stop                    dogStop
		word, focusDeny, gitAsk string
		trod                    trodReply
	)
	if !late {
		// The jot dog reads what a finished turn left, and holds its last
		// message to its tests, before the turn is recorded, outside c.mu
		// (dog.go).
		stop = c.dogAtStop(a.Session, evs)
		// So may the trod bot, which may read the machine's idle time
		// (trod.go).
		if stop.note == "" {
			trod = c.trodAtHook(a.Session, evs)
		}
		// And the arch agents' word to a session or subagent that starts
		// (arch.go), Gabriel's guard of the user's focus (gabriel.go) and
		// Mikael's of what the user's own git runs (gitguard.go).
		word = c.archWord(a.Session, evs)
		if trod.how != trodAtAsk {
			focusDeny = c.focusGuard(a.Session, call.Tool, call.Input, evs)
			gitAsk = c.gitGuard(call.Tool, call.Input, call.Cwd, evs)
		}
	}
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return ipc.HookResult{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	if late && !lateFits(x, at) {
		c.mu.Unlock()
		return ipc.HookResult{}, errStale
	}
	var reply ipc.HookResult
	failover, turnEnded, boosts := false, false, false
	var fall *modelSwitch
	x.quiet++
	// A model switch that Irie is making goes ahead without asking
	// (fallback.go).
	reply.Allow = modelSwitchAsk(x, a.Payload)
	for _, e := range evs {
		if late {
			if e.Data == nil {
				e.Data = map[string]any{}
			}
			e.TS, e.Data["late"] = event.Stamp(at), true
		}
		ends := e.Kind == event.TurnEnded && e.Agent == event.Main
		if ends && asksUser(e.Str("message")) {
			// Whether it asks is read from the whole message, which the
			// journal cuts.
			e.Data["asks"] = true
		}
		switch {
		case e.Kind == event.ToolStarted && x.stopped[e.Agent]:
			// A helper you stopped: its call is turned away, with word to
			// end at once (sub.go).
			reply.Deny = stoppedNote
			continue
		case stop.note != "" && ends:
			// The turn goes on, so it has not ended: the dog sent the
			// agent back to finish what it left, or to put its report
			// right.
			e = nudgeEvent(e, stop)
			reply.Unfinished = stop.note
		case trod.how == trodAtStop && ends:
			// Likewise the trod bot, to decide what it asked.
			e = trodEvent(e, trodAtStop)
			reply.Unfinished = trod.note
		case trod.how == trodAtAsk && e.Kind == event.ToolStarted && e.Str("tool") == "AskUserQuestion":
			// The question is turned away, so its call never runs.
			continue
		case focusDeny != "" && e.Kind == event.ToolStarted:
			// Gabriel turns the call away, so it never runs.
			reply.Deny = focusDeny
			continue
		case gitAsk != "" && e.Kind == event.ToolStarted:
			// Mikael asks the user first, in the harness's permission
			// prompt, about a change to what their own git runs. The call
			// runs if they say yes, so it is recorded as it starts.
			reply.Ask = gitAsk
		case trod.how == trodAtAsk && e.Kind == event.InputNeeded && e.Str("reason") == event.ReasonQuestion:
			e = trodEvent(e, trodAtAsk)
			reply.Deny = trod.note
		}
		if ends && e.Kind == event.TurnEnded && stop.unproven != "" {
			// The turn ends with its word on its tests standing against
			// its last test run: the row says so (dog.go).
			e.Data["unproven"] = stop.unproven
		}
		input := call.Input
		if c.cfg().Journal.ToolInput == "none" {
			for _, k := range toolInputKeys {
				delete(e.Data, k)
			}
			input = nil
		}
		if p := eventPhrase(e, call.Tool, input); p != "" {
			e.Data["phrase"] = p
		}
		// The journal cuts strings, so the whole message is kept first.
		msg := e.Str("message")
		if e.Kind == event.TurnEnded && e.Agent == event.Main && msg != "" {
			c.saveResult(x, msg)
		}
		c.record(x, e)
		boosts = c.boostTurn(x, e) || boosts
		switch e.Kind {
		case event.SubagentFinished:
			delete(x.stopped, e.Agent)
		case event.ModelSwitched:
			modelSwitched(x, e)
		}
		// The note a call to Variorum's door names is the session's, for
		// the Variorum tab (variorum.go).
		if e.Kind == event.ToolStarted {
			c.noteVariorum(x, call.Tool, call.Input)
		}
		// A teach-mode turn that asks the user to write a part is theirs:
		// the agent waits at its prompt (docs/proposals/learning.md §4.4).
		if e.Kind == event.TurnEnded && e.Agent == event.Main && x.tutor == config.TutorTeach {
			if line, ok := yourTurn(msg); ok {
				c.record(x, event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.InputNeeded, Vendor: e.Vendor,
					Data: map[string]any{"reason": event.ReasonTurn, "phrase": "Your turn: " + line}})
			}
		}
		turnEnded = turnEnded || e.Kind == event.TurnEnded && e.Agent == event.Main
		switch {
		case e.Kind == event.SessionStarted:
			reply.Context = joinNotes(word, c.startNote(x))
		case e.Kind == event.SubagentStarted:
			reply.Context = joinNotes(reply.Context, word)
		case e.Kind == event.TurnFailed:
			// A model's own usage limit switches the session to its
			// profile's fallback model, in place (fallback.go); the plan's
			// may carry its task on elsewhere.
			if sw := c.fallbackFor(x, e); sw != nil {
				fall = sw
			} else if e.Str("error") == "rate_limit" && c.cfg().Route.Failover {
				failover = true
			}
		}
	}
	if boosts {
		// A boost takes the tree out of the background at once, and its
		// end puts it back.
		c.background()
	}
	if !late {
		if n := c.takeNote(x, evs); n != "" {
			reply.Context = joinNotes(reply.Context, n)
		}
		// An edit of what a plugin of yours runs holds it (ownhelpers.go).
		if n := c.pinnedNote(call.Tool, call.Input, evs); n != "" {
			reply.Context = joinNotes(reply.Context, n)
		}
	}
	c.followConversation(x)
	if turnEnded && c.cfg().Spawn.CloseWhenDone {
		c.closeWhenDone(x)
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	switch {
	case fall != nil:
		go c.fallBack(a.Session, fall)
	case failover:
		go c.failover(a.Session)
	}
	return reply, nil
}

// joinNotes puts notes for an agent one after another, a blank line
// between each, leaving out the empty ones.
func joinNotes(notes ...string) string {
	var out []string
	for _, n := range notes {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, "\n\n")
}

// eventPhrase is what an event of a tool call says in plain words (Phrase):
// what a tool.started does, or what an input.needed of a permission prompt,
// a question or a plan asks. Other events say nothing more.
func eventPhrase(e event.Event, tool string, input json.RawMessage) string {
	switch {
	case tool == "":
		return ""
	case e.Kind == event.ToolStarted:
		return Phrase(tool, input)
	case e.Kind != event.InputNeeded:
		return ""
	}
	switch e.Str("reason") {
	case event.ReasonPermission:
		return PhraseAsk(tool, input)
	case event.ReasonQuestion, event.ReasonPlan:
		return Phrase(tool, input)
	}
	return ""
}

// failedReason is spawn.failed's reason when the pane dies first.
const failedReason = "exited before it was ready"

// tailLines is how many screen lines a stalled or failed spawn keeps.
const tailLines = 5

// pasteGrace is how long a generic agent that never asks for bracketed
// paste has to start before its task is pasted anyway.
const pasteGrace = 2 * time.Second

// observe records what tmux and the clock say about the sessions: a pane
// that died, which fails a spawn that never became ready, and a spawn that
// did not become ready within spawn.ready_timeout, which is stalled (DESIGN
// §4.2). Both keep the last lines of the pane's screen. A harness without
// hooks never says it is ready, so its task is delivered here instead. The
// caller holds c.mu.
func (c *Core) observe(now time.Time) {
	for _, id := range slices.Sorted(maps.Keys(c.sessions)) {
		x := c.sessions[id]
		t := &x.track
		// An agent that asked for bracketed paste is at its prompt; one that
		// never does gets pasteGrace to get there.
		if !x.observed && !x.dead && t.awaiting() && c.agentRuns(x) && (!x.paste || x.bracket || now.Sub(t.requested) >= pasteGrace) {
			c.deliver(x)
		}
		switch {
		case x.dead && t.exited == nil:
			// A session brought back resting ran no agent (rest.go).
			waiting := t.awaiting() && t.rest == nil
			exit := event.Event{Source: event.SourceTmux, Kind: event.PaneExited, Data: map[string]any{"code": x.exitCode}}
			if ex, err := host.ReadExit(c.paths.ExitFile(x.id)); err == nil {
				exit.Source, exit.Data["code"] = event.SourceHost, ex.Code
				if ex.Signal != 0 {
					exit.Data["signal"] = ex.Signal
				}
			}
			c.record(x, exit)
			if waiting {
				c.record(x, event.Event{Source: event.SourceIrie, Kind: event.SpawnFailed, Data: map[string]any{
					"reason": failedReason, "exit": exit.Data["code"], "tail": strings.Join(c.tail(x.pane), "\n"),
				}})
			}
		case !x.dead && t.awaiting() && t.stalled == nil && now.Sub(t.requested) >= c.cfg().Spawn.ReadyTimeout:
			c.record(x, event.Event{Source: event.SourceIrie, Kind: event.InputNeeded, Data: map[string]any{
				"reason": event.ReasonStalled, "wait": c.cfg().Spawn.ReadyTimeout.String(), "tail": strings.Join(c.tail(x.pane), "\n"),
			}})
		}
	}
}

// agentRuns reports whether the agent has taken its host's place in the
// foreground of its pane. Process names may be cut to 15 characters.
func (c *Core) agentRuns(x *session) bool {
	host := filepath.Base(c.bin)
	return x.command != "" && x.command != host && !(len(x.command) >= 15 && strings.HasPrefix(host, x.command))
}

// deliver gives a generic agent its task and records the prompt. An
// argument task is in already; a paste task goes in from the task file,
// with bracketed paste when the agent asked for it, then Enter. tmux keeps
// tabs and newlines but shows other control characters as ^X, so a task
// cannot steer the terminal. The caller holds c.mu.
func (c *Core) deliver(x *session) {
	file := c.paths.TaskFile(x.id)
	task, err := os.ReadFile(file)
	if err != nil {
		c.log.Warn("deliver", "session", x.id, "err", err)
		return
	}
	if x.paste {
		buf := brand.Slug + "-task-" + x.id
		if _, err := c.tmux.RunList([][]string{
			{"load-buffer", "-b", buf, file},
			{"paste-buffer", "-p", "-d", "-b", buf, "-t", x.pane},
			{"send-keys", "-t", x.pane, "Enter"},
		}); err != nil {
			c.log.Warn("paste the task", "session", x.id, "err", err)
			return
		}
	}
	c.record(x, event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.PromptSubmitted, Data: map[string]any{"chars": utf8.RuneCount(task)}})
}

// tail returns the last lines a pane shows, up to tailLines, leaving out
// blank lines and tmux's note that the pane is dead. A dead pane's note
// scrolls its screen, so some history is read too.
func (c *Core) tail(pane string) []string {
	out, err := c.tmux.Run("capture-pane", "-p", "-J", "-S", "-50", "-t", pane)
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimRight(l, " \t"); l != "" && !strings.HasPrefix(l, "Pane is dead") {
			lines = append(lines, l)
		}
	}
	return lines[max(0, len(lines)-tailLines):]
}
