// Package hook is `irie hook`, the shim an agent runs for each hook event
// (DESIGN §2.2, §3.3). It reads the event from stdin, caps its strings at
// 2 KiB, forwards it to the core and prints the core's reply in the harness's
// own dialect. It always exits 0: when Irie is down, agents keep working.
// An event the core cannot take then, as for the seconds an install takes to
// restart it, goes in the session's missed.ndjson, which the core takes when
// it is back (Missed).
package hook

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
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
	session := getenv(brand.Env("SESSION"))
	if session == "" {
		// An agent team's teammate (DESIGN §2.3) speaks for no session,
		// but waits at its tool calls while the session it works for is
		// frozen.
		if lead := getenv(brand.Env("TEAM_OF")); teamOf.MatchString(lead) {
			if raw, err := io.ReadAll(io.LimitReader(stdin, maxPayload)); err == nil {
				var p struct {
					Event string `json:"hook_event_name"`
				}
				if json.Unmarshal(raw, &p) == nil {
					hold(getenv, lead, p.Event)
				}
			}
		}
		return 0
	}
	socket := getenv(brand.Env("SOCKET"))
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

	// An anonymous research session's shell commands leave through its
	// compartment's exit: Claude Code runs CLAUDE_ENV_FILE before each
	// Bash command, and a SessionStart hook may add to it (DESIGN §4.16).
	// The shim does it itself, so it holds while the core is down.
	if hookEvent == "SessionStart" {
		anonEnv(getenv, session)
	}

	h := claude.Harness{}
	wait := replyTimeout
	if hookEvent == "Stop" {
		wait = stopReplyTimeout
	}
	args := ipc.HookArgs{Harness: h.ID(), Session: session, Payload: payload}
	r, err := ask(socket, args, wait)
	if errors.Is(err, errNoCore) {
		keep(getenv, args, hookEvent, time.Now())
	}
	hold(getenv, session, hookEvent)
	if err != nil {
		return 0
	}
	// Respond needs only the event's name to pick the dialect.
	e := event.Event{Vendor: &event.Vendor{HookEvent: hookEvent}}
	stdout.Write(h.Respond(e, harness.HookReply{Context: r.Context, Deny: r.Deny, Unfinished: r.Unfinished, Allow: r.Allow, Ask: r.Ask}))
	return 0
}

// anonEnv adds the session's compartment's proxy variables, when it has a
// compartment, to Claude Code's CLAUDE_ENV_FILE.
func anonEnv(getenv func(string) string, session string) {
	dst := getenv("CLAUDE_ENV_FILE")
	p, err := paths.New(getenv, os.Getuid())
	if dst == "" || err != nil {
		return
	}
	b, err := os.ReadFile(filepath.Join(p.Session(session), harness.AnonEnvFile))
	if err != nil || len(b) == 0 {
		return
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(b)
}

// teamOf is a session id, as IRIE_TEAM_OF names one.
var teamOf = regexp.MustCompile(`^s[0-9a-z]{12}$`)

// hold makes a frozen session's agent wait at its tool calls, while the
// core is down too (DESIGN §4.3). It stops waiting if the agent, its
// parent, is gone.
func hold(getenv func(string) string, session, hookEvent string) {
	p, err := paths.New(getenv, os.Getuid())
	if err != nil || !holds[hookEvent] {
		return
	}
	dir := p.Session(session)
	for parent := os.Getppid(); host.Frozen(dir) && os.Getppid() == parent; {
		time.Sleep(holdPoll)
	}
}

// errNoCore is ask's error when no core took the event: none listens on
// the socket.
var errNoCore = errors.New("no core listens")

// ask forwards a hook event to the core and returns its reply, waiting at
// most wait for it.
func ask(socket string, a ipc.HookArgs, wait time.Duration) (ipc.HookResult, error) {
	var r ipc.HookResult
	c, err := ipc.Dial(socket, connectTimeout)
	if err != nil {
		return r, errNoCore
	}
	defer c.Close()
	c.Timeout = wait
	return r, c.Call(ipc.OpHook, a, &r)
}

// Missed is one line of a session's missed.ndjson: a hook event the core
// could not take, and when it came.
type Missed struct {
	At      time.Time       `json:"at"`
	Harness string          `json:"harness"`
	Payload json.RawMessage `json:"payload"`
}

// maxMissed is how big a missed.ndjson grows with tool calls: past it,
// while Irie stays down, only the events that change a session's state go
// in.
const maxMissed = 1 << 20

// toolHooks are the hook events of tool calls.
var toolHooks = map[string]bool{"PreToolUse": true, "PostToolUse": true, "PostToolUseFailure": true}

// keep appends a hook event the core could not take to the session's
// missed.ndjson, with when it came. One write appends the whole line, so
// the shims of hooks that run at once never mix their lines.
func keep(getenv func(string) string, a ipc.HookArgs, hookEvent string, at time.Time) {
	p, err := paths.New(getenv, os.Getuid())
	if err != nil {
		return
	}
	file := p.MissedFile(a.Session)
	if fi, err := os.Stat(file); err == nil && fi.Size() > maxMissed && toolHooks[hookEvent] {
		return
	}
	line, err := json.Marshal(Missed{At: at.UTC(), Harness: a.Harness, Payload: a.Payload})
	if err != nil {
		return
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}

// ReadMissed reads the events of a missed.ndjson in the order they came,
// leaving out a line it cannot read.
func ReadMissed(r io.Reader) []Missed {
	var out []Missed
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxPayload)
	for sc.Scan() {
		var m Missed
		if json.Unmarshal(sc.Bytes(), &m) == nil && !m.At.IsZero() && len(m.Payload) > 0 {
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b Missed) int { return a.At.Compare(b.At) })
	return out
}

// whole are the payload fields the shim does not cap: a turn's last
// message, which a delegated session reports back whole (DESIGN §6.4). The
// journal still cuts it.
var whole = map[string]bool{"last_assistant_message": true}

// ends are the payload fields the shim cuts from the middle (event.CapEnds):
// what a command printed, whose end says how it went, such as a test run's
// FAIL lines, and a failed call's error, which starts with its exit code
// (DESIGN §4.8).
var ends = map[string]bool{"tool_response": true, "tool_response.stdout": true, "tool_response.stderr": true, "error": true}

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
			if ends[path] {
				return event.CapEnds(x)
			}
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
