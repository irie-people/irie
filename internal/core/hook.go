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

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
)

// toolInputKeys are the data a hook event takes from a tool's input, which
// journal.tool_input = "none" leaves out.
var toolInputKeys = []string{"summary", "question"}

// hook takes one hook event from the shim (DESIGN §3.3) and records it.
// Nothing holds a call yet. A session that starts may be told it can
// delegate (DESIGN §6.4); a turn's last message is kept whole for
// delegation; a turn stopped at a usage limit may fail over; a jot whose
// turn ends before its work has landed may be sent back to finish it
// (DESIGN §4.8).
func (c *Core) hook(a ipc.HookArgs) (ipc.HookResult, error) {
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
	}
	json.Unmarshal(a.Payload, &call)
	// The jot dog reads what a finished turn left before the turn is
	// recorded, outside c.mu (dog.go).
	unfinished, loose := c.dogAtStop(a.Session, evs)
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return ipc.HookResult{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	var reply ipc.HookResult
	failover, turnEnded := false, false
	x.quiet++
	for _, e := range evs {
		if unfinished != "" && e.Kind == event.TurnEnded && e.Agent == event.Main {
			// The turn goes on, so it has not ended: the dog sent the
			// agent back to finish what it left.
			e = nudgeEvent(e, loose)
			reply.Unfinished = unfinished
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
			reply.Context = c.delegateHint(x)
		case e.Kind == event.TurnFailed && e.Str("error") == "rate_limit" && c.cfg().Route.Failover:
			failover = true
		}
	}
	if n := c.takeNote(x, evs); n != "" {
		reply.Context = strings.TrimSpace(reply.Context + "\n\n" + n)
	}
	c.followConversation(x)
	if turnEnded && c.cfg().Spawn.CloseWhenDone {
		c.closeWhenDone(x)
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	if failover {
		go c.failover(a.Session)
	}
	return reply, nil
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
			waiting := t.awaiting()
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
		buf := "irie-task-" + x.id
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
