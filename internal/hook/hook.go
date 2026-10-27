// Package hook is `irie hook`, the shim an agent runs for each hook event
// (DESIGN §2.2, §3.3). It reads the event from stdin, caps its strings at
// 2 KiB, forwards it to the core and prints the core's reply in the harness's
// own dialect. It always exits 0: when Irie is down, agents keep working.
package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/harness"
	"github.com/irie-hq/irie/internal/harness/claude"
	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/paths"
)

// connectTimeout bounds connecting to the core.
const connectTimeout = 200 * time.Millisecond

// replyTimeout bounds waiting for the core's answer, which comes at once:
// nothing holds a call yet.
var replyTimeout = 2 * time.Second

// stopReplyTimeout bounds waiting for the answer to a Stop, for which the
// jot dog reads the jot's worktree with git first (DESIGN §4.8). It stays
// under the Stop hook's own timeout of 10 s, so a slow answer only lets the
// turn end.
var stopReplyTimeout = 8 * time.Second

// maxPayload is the most the shim reads from stdin.
const maxPayload = 256 << 20

// holdPoll is how often a tool call that a freeze holds looks again.
var holdPoll = 50 * time.Millisecond

// holds are the hook events at which an agent waits while its session is
// frozen: those before and after a tool call, which the agent waits for.
var holds = map[string]bool{"PreToolUse": true, "PostToolUse": true, "PostToolUseFailure": true}

// Run is `irie hook`. It reads IRIE_SESSION and IRIE_SOCKET from getenv and
// returns the exit code, which is always 0.
func Run(stdin io.Reader, stdout io.Writer, getenv func(string) string) (code int) {
	defer func() {
		recover()
		code = 0
	}()
	session := getenv("IRIE_SESSION")
	if session == "" {
		return 0
	}
	socket := getenv("IRIE_SOCKET")
	if socket == "" {
		p, err := paths.FromEnv()
		if err != nil {
			return 0
		}
		socket = p.Socket()
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, maxPayload))
	if err != nil {
		return 0
	}
	payload, hookEvent, err := shrink(raw)
	if err != nil {
		return 0
	}

	h := claude.Harness{}
	wait := replyTimeout
	if hookEvent == "Stop" {
		wait = stopReplyTimeout
	}
	r, ok := ask(socket, ipc.HookArgs{Harness: h.ID(), Session: session, Payload: payload}, wait)
	// A frozen session's agent waits at its tool calls, while the core is
	// down too (DESIGN §4.3). It stops waiting if the agent, its parent, is
	// gone.
	if p, err := paths.New(getenv, os.Getuid()); err == nil && holds[hookEvent] {
		dir := p.Session(session)
		for parent := os.Getppid(); host.Frozen(dir) && os.Getppid() == parent; {
			time.Sleep(holdPoll)
		}
	}
	if !ok {
		return 0
	}
	// Respond needs only the event's name to pick the dialect.
	e := event.Event{Vendor: &event.Vendor{HookEvent: hookEvent}}
	stdout.Write(h.Respond(e, harness.HookReply{Context: r.Context, Deny: r.Deny, Unfinished: r.Unfinished}))
	return 0
}

// ask forwards a hook event to the core and returns its reply, waiting at
// most wait for it.
func ask(socket string, a ipc.HookArgs, wait time.Duration) (ipc.HookResult, bool) {
	var r ipc.HookResult
	c, err := ipc.Dial(socket, connectTimeout)
	if err != nil {
		return r, false
	}
	defer c.Close()
	c.Timeout = wait
	if err := c.Call(ipc.OpHook, a, &r); err != nil {
		return r, false
	}
	return r, true
}

// whole are the payload fields the shim does not cap: a turn's last
// message, which a delegated session reports back whole (DESIGN §6.4). The
// journal still cuts it.
var whole = map[string]bool{"last_assistant_message": true}

// shrink caps every string of a JSON payload at event.MaxString bytes, but
// those in whole, and records, under irie_lengths, how many characters each
// cut string had, by its dotted path. It also returns the payload's
// hook_event_name.
func shrink(raw []byte) (json.RawMessage, string, error) {
	var v map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // numbers pass through as written
	if err := dec.Decode(&v); err != nil {
		return nil, "", err
	}
	lengths := map[string]int{}
	for k, item := range v {
		if !whole[k] {
			v[k] = walk(item, k, lengths)
		}
	}
	if len(lengths) > 0 {
		v["irie_lengths"] = lengths
	}
	name, _ := v["hook_event_name"].(string)
	b, err := json.Marshal(v)
	return b, name, err
}

func walk(v any, path string, lengths map[string]int) any {
	switch x := v.(type) {
	case string:
		if len(x) > event.MaxString {
			lengths[path] = utf8.RuneCountInString(x)
			return event.Cap(x)
		}
	case map[string]any:
		for k, item := range x {
			x[k] = walk(item, path+"."+k, lengths)
		}
	case []any:
		for i, item := range x {
			x[i] = walk(item, path+"."+strconv.Itoa(i), lengths)
		}
	}
	return v
}
