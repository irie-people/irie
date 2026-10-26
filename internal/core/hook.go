package core

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
)

// toolInputKeys are the data a hook event takes from a tool's input, which
// journal.tool_input = "none" leaves out.
var toolInputKeys = []string{"summary", "question"}

// hook takes one hook event from the shim (DESIGN §3.3) and records it.
// Nothing holds a call yet and there are no notes, so the reply is empty.
func (c *Core) hook(a ipc.HookArgs) (ipc.HookResult, error) {
	h, err := harnessFor(a.Harness)
	if err != nil {
		return ipc.HookResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	evs, err := h.Normalize(a.Payload)
	if err != nil {
		return ipc.HookResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return ipc.HookResult{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	for _, e := range evs {
		if c.cfg.Journal.ToolInput == "none" {
			for _, k := range toolInputKeys {
				delete(e.Data, k)
			}
		}
		c.record(x, e)
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return ipc.HookResult{}, nil
}

// failedReason is spawn.failed's reason when the pane dies first.
const failedReason = "exited before it was ready"

// tailLines is how many screen lines a stalled or failed spawn keeps.
const tailLines = 5

// observe records what tmux and the clock say about the sessions: a pane
// that died, which fails a spawn that never became ready, and a spawn that
// did not become ready within spawn.ready_timeout, which is stalled (DESIGN
// §4.2). Both keep the last lines of the pane's screen. The caller holds
// c.mu.
func (c *Core) observe(now time.Time) {
	for _, id := range slices.Sorted(maps.Keys(c.sessions)) {
		x := c.sessions[id]
		t := &x.track
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
		case !x.dead && t.awaiting() && t.stalled == nil && now.Sub(t.requested) >= c.cfg.Spawn.ReadyTimeout:
			c.record(x, event.Event{Source: event.SourceIrie, Kind: event.InputNeeded, Data: map[string]any{
				"reason": event.ReasonStalled, "wait": c.cfg.Spawn.ReadyTimeout.String(), "tail": strings.Join(c.tail(x.pane), "\n"),
			}})
		}
	}
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
