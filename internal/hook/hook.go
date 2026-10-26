// Package hook is `irie hook`, the shim an agent runs for each hook event
// (DESIGN §2.2, §3.3). It reads the event from stdin, caps its strings at
// 2 KiB, forwards it to the core and prints the core's reply in the harness's
// own dialect. It always exits 0: when Irie is down, agents keep working.
package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/harness"
	"github.com/irie-hq/irie/internal/harness/claude"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/paths"
)

// connectTimeout bounds connecting to the core.
const connectTimeout = 200 * time.Millisecond

// replyTimeout bounds waiting for the core's answer, which comes at once:
// nothing holds a call yet.
var replyTimeout = 2 * time.Second

// maxPayload is the most the shim reads from stdin.
const maxPayload = 256 << 20

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
	c, err := ipc.Dial(socket, connectTimeout)
	if err != nil {
		return 0
	}
	defer c.Close()
	c.Timeout = replyTimeout
	var r ipc.HookResult
	if err := c.Call(ipc.OpHook, ipc.HookArgs{Harness: h.ID(), Session: session, Payload: payload}, &r); err != nil {
		return 0
	}
	// Respond needs only the event's name to pick the dialect.
	e := event.Event{Vendor: &event.Vendor{HookEvent: hookEvent}}
	stdout.Write(h.Respond(e, harness.HookReply{Context: r.Context, Deny: r.Deny}))
	return 0
}

// shrink caps every string of a JSON payload at event.MaxString bytes and
// records, under irie_lengths, how many characters each cut string had, by
// its dotted path. It also returns the payload's hook_event_name.
func shrink(raw []byte) (json.RawMessage, string, error) {
	var v map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // numbers pass through as written
	if err := dec.Decode(&v); err != nil {
		return nil, "", err
	}
	lengths := map[string]int{}
	for k, item := range v {
		v[k] = walk(item, k, lengths)
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
