// Command fakeagent stands in for Claude Code in Irie's tests (DESIGN §8.1,
// §3.3). It accepts the flags Irie passes, reads the hooks of --settings and
// runs a step script by calling those hooks with Claude-shaped JSON on
// stdin, as Claude Code's hooks reference describes. It logs every hook's
// reply to a file.
//
// The task is the script when it is one:
//
//	prompt            UserPromptSubmit with the task
//	tool:<name>       PreToolUse, then PostToolUse; AskUserQuestion and
//	                  ExitPlanMode wait for a line of input in between
//	ask               tool:AskUserQuestion
//	permission        PreToolUse, PermissionRequest and a permission_prompt
//	                  Notification for Bash, a line of input, then PostToolUse
//	stop              Stop
//	stopfail:<error>  StopFailure
//	sub:<id>:<steps>  SubagentStart, the steps (joined by +) inside the
//	                  subagent, then SubagentStop
//	sleep:<duration>  a pause
//	exit:<n>          exit with code n at once, without SessionEnd
//	status:<cost>     run the status line command of --settings with
//	                  Claude-shaped JSON, as the status line docs describe:
//	                  that cost in dollars, 12% of the context, and 33% of
//	                  the plan's five hours
//
// Any other task runs the script "prompt". After the script the agent idles:
// each line typed is a prompt and a turn, and EOF or a hangup ends the
// session with SessionEnd. With --resume and no task it starts from a
// resume and idles at once.
//
// `fakeagent agents --json` lists no sessions, as `claude agents --json`
// does on a machine with none outside Irie. The log's first line records
// the agent's argv, pid and the names, not the values, of its environment.
//
// Environment:
//
//	IRIE_FAKE_VERSION  what --version reports (default 2.1.300); a version
//	                   before 2.1.295 refuses settings that use onFailure
//	IRIE_FAKE_NO_NAME  when set, --help lists no --name and --name is refused
//	IRIE_FAKE_PACE     a pause before each step, such as 300ms
//	IRIE_FAKE_LOG      the log file; by default fakeagent.ndjson next to the
//	                   --settings file
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultVersion = "2.1.300"

func main() { os.Exit(run(os.Args[1:])) }

type options struct {
	model, effort, mode, settings, sessionID, name, resume string
	version, help                                          bool
	task                                                   string
}

var (
	uuid    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	modes   = []string{"acceptEdits", "auto", "bypassPermissions", "default", "dontAsk", "manual", "plan"}
	efforts = []string{"high", "low", "max", "medium", "xhigh"}
)

func parseArgs(args []string) (options, error) {
	var o options
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flag, value, inline := strings.Cut(a, "=")
		switch flag {
		case "-v", "--version":
			o.version = true
			continue
		case "-h", "--help":
			o.help = true
			continue
		}
		var dst *string
		switch flag {
		case "--model":
			dst = &o.model
		case "--effort":
			dst = &o.effort
		case "--permission-mode":
			dst = &o.mode
		case "--settings":
			dst = &o.settings
		case "--session-id":
			dst = &o.sessionID
		case "-r", "--resume":
			dst = &o.resume
		case "-n", "--name":
			if os.Getenv("IRIE_FAKE_NO_NAME") == "" {
				dst = &o.name
			}
		}
		if dst == nil {
			return o, fmt.Errorf("error: unknown option '%s'", flag)
		}
		if !inline {
			if i+1 >= len(args) {
				return o, fmt.Errorf("error: option '%s' argument missing", flag)
			}
			i++
			value = args[i]
		}
		*dst = value
	}
	if len(positional) > 1 {
		return o, fmt.Errorf("error: too many arguments: %q", positional)
	}
	if len(positional) == 1 {
		o.task = positional[0]
	}
	if o.mode != "" && !slices.Contains(modes, o.mode) {
		return o, fmt.Errorf("error: option '--permission-mode <mode>' argument '%s' is invalid. Allowed choices are %s.", o.mode, strings.Join(modes, ", "))
	}
	if o.effort != "" && !slices.Contains(efforts, o.effort) {
		return o, fmt.Errorf("error: option '--effort <level>' argument '%s' is invalid. Allowed choices are %s.", o.effort, strings.Join(efforts, ", "))
	}
	if o.sessionID != "" && !uuid.MatchString(o.sessionID) {
		return o, fmt.Errorf("error: --session-id must be a valid UUID")
	}
	return o, nil
}

const help = `Usage: claude [options] [prompt]

Options:
  --effort <level>           Effort level for the current session
  -h, --help                 Display help for command
  --model <model>            Model for the current session
  -n, --name <name>          Set a display name for this session
  --permission-mode <mode>   Permission mode to use for the session
  -r, --resume [value]       Resume a conversation by session ID
  --session-id <uuid>        Use a specific session ID for the conversation
  --settings <file-or-json>  Path to a settings JSON file or a JSON string
  -v, --version              Output the version number
`

func run(args []string) int {
	if len(args) > 0 && args[0] == "agents" {
		if !slices.Contains(args[1:], "--json") {
			fmt.Fprintln(os.Stderr, "fakeagent: agents needs --json")
			return 1
		}
		fmt.Println("[]")
		return 0
	}
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	version := os.Getenv("IRIE_FAKE_VERSION")
	if version == "" {
		version = defaultVersion
	}
	switch {
	case o.version:
		fmt.Printf("%s (Claude Code)\n", version)
		return 0
	case o.help:
		h := help
		if os.Getenv("IRIE_FAKE_NO_NAME") != "" {
			h = strings.Replace(h, "  -n, --name <name>          Set a display name for this session\n", "", 1)
		}
		fmt.Print(h)
		return 0
	}
	a, err := newAgent(o, version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeagent:", err)
		return 1
	}
	return a.run()
}

// handler and group are the hook settings of Claude Code's hooks reference.
type handler struct {
	Type      string   `json:"type"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Timeout   int      `json:"timeout"`
	OnFailure string   `json:"onFailure"`
}

type group struct {
	Matcher string    `json:"matcher"`
	Hooks   []handler `json:"hooks"`
}

// handlerKeys are the command hook fields the hooks reference documents.
var handlerKeys = []string{"type", "command", "args", "timeout", "onFailure", "async", "asyncRewake", "shell", "if", "statusMessage", "once"}

// loadHooks reads the hooks of a --settings value, a path or inline JSON. It
// is strict, as a CLI that validates its settings would be.
func loadHooks(value, version string) (map[string][]group, error) {
	if value == "" {
		return nil, nil
	}
	b := []byte(value)
	if !strings.HasPrefix(strings.TrimSpace(value), "{") {
		var err error
		if b, err = os.ReadFile(value); err != nil {
			return nil, err
		}
	}
	var raw struct {
		Hooks map[string][]struct {
			Matcher string                       `json:"matcher"`
			Hooks   []map[string]json.RawMessage `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	out := map[string][]group{}
	for ev, groups := range raw.Hooks {
		for gi, g := range groups {
			out[ev] = append(out[ev], group{Matcher: g.Matcher})
			for hi, fields := range g.Hooks {
				where := fmt.Sprintf("hooks.%s[%d].hooks[%d]", ev, gi, hi)
				for k := range fields {
					if !slices.Contains(handlerKeys, k) {
						return nil, fmt.Errorf("settings: %s: unknown key %q", where, k)
					}
				}
				buf, _ := json.Marshal(fields)
				var h handler
				if err := json.Unmarshal(buf, &h); err != nil {
					return nil, fmt.Errorf("settings: %s: %w", where, err)
				}
				if h.OnFailure != "" && (h.OnFailure != "block" && h.OnFailure != "continue" || !atLeast(version, "2.1.295")) {
					return nil, fmt.Errorf("settings: %s: onFailure %q is not supported", where, h.OnFailure)
				}
				out[ev][gi].Hooks = append(out[ev][gi].Hooks, h)
			}
		}
	}
	return out, nil
}

func atLeast(v, min string) bool {
	part := func(s string, i int) int {
		f := strings.Split(s, ".")
		if i >= len(f) {
			return 0
		}
		n, _ := strconv.Atoi(f[i])
		return n
	}
	for i := range 3 {
		x, y := part(v, i), part(min, i)
		if x != y {
			return x > y
		}
	}
	return true
}

// matcherSupport lists the events whose matcher filters a payload field.
var matcherSupport = map[string]bool{
	"PreToolUse": true, "PostToolUse": true, "PostToolUseFailure": true, "PermissionRequest": true,
	"Notification": true, "SessionStart": true, "SessionEnd": true, "StopFailure": true,
	"SubagentStart": true, "SubagentStop": true,
}

var exact = regexp.MustCompile(`^[A-Za-z0-9_\- ,|]+$`)

// matches reports whether a matcher selects value: "*" or "" selects
// everything, plain names are an exact list split by | or ",", and anything
// else is an unanchored regular expression.
func matches(matcher, value string) bool {
	if matcher == "" || matcher == "*" {
		return true
	}
	if exact.MatchString(matcher) {
		for _, m := range strings.FieldsFunc(matcher, func(r rune) bool { return r == '|' || r == ',' }) {
			if strings.TrimSpace(m) == value {
				return true
			}
		}
		return false
	}
	re, err := regexp.Compile(matcher)
	return err == nil && re.MatchString(value)
}

type agent struct {
	o          options
	version    string
	hooks      map[string][]group
	logPath    string
	sessionID  string
	transcript string
	cwd        string
	mode       string
	model      string
	in         *bufio.Reader
	pace       time.Duration

	mu    sync.Mutex // one hook at a time
	tools int
}

func newAgent(o options, version string) (*agent, error) {
	hooks, err := loadHooks(o.settings, version)
	if err != nil {
		return nil, err
	}
	a := &agent{o: o, version: version, hooks: hooks, in: bufio.NewReader(os.Stdin), model: o.model, mode: o.mode}
	a.cwd, _ = os.Getwd()
	switch {
	case o.resume != "":
		a.sessionID = o.resume
	case o.sessionID != "":
		a.sessionID = o.sessionID
	default:
		a.sessionID = newUUID()
	}
	if a.model == "" {
		a.model = "claude-opus-5-5"
	}
	if a.mode == "" || a.mode == "manual" {
		a.mode = "default"
	}
	base := os.TempDir()
	if o.settings != "" && !strings.HasPrefix(strings.TrimSpace(o.settings), "{") {
		base = filepath.Dir(o.settings)
		a.logPath = filepath.Join(base, "fakeagent.ndjson")
	}
	if p := os.Getenv("IRIE_FAKE_LOG"); p != "" {
		a.logPath = p
	}
	a.transcript = filepath.Join(base, "transcripts", a.sessionID+".jsonl")
	if d, err := time.ParseDuration(os.Getenv("IRIE_FAKE_PACE")); err == nil {
		a.pace = d
	}
	return a, nil
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// record appends one line to the log.
func (a *agent) record(v map[string]any) {
	if a.logPath == "" {
		return
	}
	f, err := os.OpenFile(a.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(v)
	f.Write(append(b, '\n'))
}

func (a *agent) run() int {
	env := map[string]string{}
	for _, k := range []string{"IRIE_SESSION", "IRIE_SOCKET", "IRIE_BIN", "TMUX", "TMUX_PANE"} {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	var names []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		names = append(names, k)
	}
	slices.Sort(names)
	a.record(map[string]any{"t": "start", "argv": os.Args[1:], "cwd": a.cwd, "env": env, "env_names": names, "pid": os.Getpid(), "session_id": a.sessionID})
	fmt.Printf("fakeagent %s · session %s\n", a.version, a.sessionID)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		a.end("other")
		os.Exit(0)
	}()

	source := "startup"
	if a.o.resume != "" {
		source = "resume"
	}
	a.fire("SessionStart", source, map[string]any{"source": source, "model": a.model})

	first, _, _ := strings.Cut(strings.TrimSpace(a.o.task), "\n")
	steps, err := parseScript(first)
	if err != nil {
		steps = []step{{kind: "prompt"}}
	}
	if strings.TrimSpace(a.o.task) == "" {
		steps = nil
	}
	for i, s := range steps {
		time.Sleep(a.pace)
		a.record(map[string]any{"t": "step", "n": i + 1, "step": s.kind, "arg": s.arg})
		if code, exit := a.do(s, nil); exit {
			return code
		}
	}
	fmt.Println("idle")
	for {
		line, err := a.in.ReadString('\n')
		if err != nil {
			a.end("other")
			return 0
		}
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		fmt.Println("› " + line)
		a.fire("UserPromptSubmit", "", map[string]any{"prompt": line})
		a.stop()
	}
}

func (a *agent) end(reason string) {
	a.fire("SessionEnd", reason, map[string]any{"reason": reason})
	a.record(map[string]any{"t": "exit", "code": 0})
}

// subagent is the context of steps run inside a subagent.
type subagent struct{ id, kind string }

func (s *subagent) fields(m map[string]any) map[string]any {
	if s != nil {
		m["agent_id"], m["agent_type"] = s.id, s.kind
	}
	return m
}

func (a *agent) do(s step, sub *subagent) (int, bool) {
	switch s.kind {
	case "prompt":
		first, _, _ := strings.Cut(a.o.task, "\n")
		fmt.Println("› " + strings.TrimSpace(first))
		a.fire("UserPromptSubmit", "", map[string]any{"prompt": a.o.task})
	case "tool":
		a.tool(s.arg, sub)
	case "permission":
		a.permission(sub)
	case "stop":
		a.stop()
	case "stopfail":
		fmt.Println("✕ API Error: " + s.arg)
		a.fire("StopFailure", s.arg, map[string]any{"error": s.arg, "error_details": "fake " + s.arg, "last_assistant_message": "API Error: " + s.arg})
	case "sub":
		in := &subagent{id: s.arg, kind: "Explore"}
		fmt.Println("◇ subagent " + s.arg)
		a.fire("SubagentStart", in.kind, in.fields(map[string]any{}))
		for _, inner := range s.sub {
			a.do(inner, in)
		}
		a.fire("SubagentStop", in.kind, in.fields(map[string]any{
			"agent_transcript_path":  filepath.Join(strings.TrimSuffix(a.transcript, ".jsonl"), "subagents", "agent-"+s.arg+".jsonl"),
			"last_assistant_message": "Subagent done.",
			"stop_hook_active":       false,
			"background_tasks":       []any{},
			"session_crons":          []any{},
		}))
	case "sleep":
		time.Sleep(s.d)
	case "exit":
		a.record(map[string]any{"t": "exit", "code": s.n})
		fmt.Printf("fakeagent: exit %d\n", s.n)
		return s.n, true
	case "status":
		a.status(s.arg)
	}
	return 0, false
}

// status runs the settings' status line command once, as Claude Code does
// after a change: JSON on stdin, through sh, in the session's directory.
func (a *agent) status(cost string) {
	command := loadStatusLine(a.o.settings)
	if command == "" {
		a.record(map[string]any{"t": "status", "error": "no status line"})
		return
	}
	usd, _ := strconv.ParseFloat(cost, 64)
	in, _ := json.Marshal(map[string]any{
		"session_id": a.sessionID, "transcript_path": a.transcript, "cwd": a.cwd,
		"model":          map[string]any{"id": a.model, "display_name": "Fake"},
		"workspace":      map[string]any{"current_dir": a.cwd, "project_dir": a.cwd},
		"version":        a.version,
		"cost":           map[string]any{"total_cost_usd": usd, "total_duration_ms": 1000, "total_api_duration_ms": 500},
		"context_window": map[string]any{"total_input_tokens": 24000, "total_output_tokens": 800, "context_window_size": 200000, "used_percentage": 12, "remaining_percentage": 88},
		"rate_limits":    map[string]any{"five_hour": map[string]any{"used_percentage": 33, "resets_at": time.Now().Add(2 * time.Hour).Unix()}},
	})
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin, cmd.Dir = bytes.NewReader(in), a.cwd
	out, err := cmd.Output()
	exit := 0
	if err != nil {
		exit = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		}
	}
	a.record(map[string]any{"t": "status", "stdout": string(out), "exit": exit})
}

// loadStatusLine returns the status line command of a --settings value.
func loadStatusLine(value string) string {
	b := []byte(value)
	if !strings.HasPrefix(strings.TrimSpace(value), "{") {
		var err error
		if b, err = os.ReadFile(value); err != nil {
			return ""
		}
	}
	var s struct {
		StatusLine struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	json.Unmarshal(b, &s)
	return s.StatusLine.Command
}

func (a *agent) stop() {
	fmt.Println("✓ done")
	first, _, _ := strings.Cut(strings.TrimSpace(a.o.task), "\n")
	a.fire("Stop", "", map[string]any{"stop_hook_active": false, "last_assistant_message": "Done: " + first, "background_tasks": []any{}, "session_crons": []any{}})
}

func (a *agent) toolID() string {
	a.tools++
	return fmt.Sprintf("toolu_fake%04d", a.tools)
}

const question = "Which colour should the button be?"

func (a *agent) toolInput(name string) map[string]any {
	switch name {
	case "Bash":
		return map[string]any{"command": "echo fake", "description": "Print a word"}
	case "Read", "Write", "Edit":
		return map[string]any{"file_path": filepath.Join(a.cwd, "README.md")}
	case "Grep", "Glob":
		return map[string]any{"pattern": "TODO"}
	case "AskUserQuestion":
		return map[string]any{"questions": []any{map[string]any{
			"question": question, "header": "Colour", "multiSelect": false,
			"options": []any{map[string]any{"label": "Green", "description": "Calm"}, map[string]any{"label": "Gold", "description": "Warm"}},
		}}}
	case "ExitPlanMode":
		return map[string]any{"plan": "## Plan\n1. Make the change\n2. Run the tests"}
	}
	return map[string]any{}
}

// readLine waits for a line typed into the agent's terminal.
func (a *agent) readLine() string {
	line, _ := a.in.ReadString('\n')
	return strings.TrimSpace(line)
}

func (a *agent) tool(name string, sub *subagent) {
	id, input := a.toolID(), a.toolInput(name)
	fmt.Println("● " + name)
	a.fire("PreToolUse", name, sub.fields(map[string]any{"tool_name": name, "tool_input": input, "tool_use_id": id}))
	start := time.Now()
	response := map[string]any{"stdout": "fake", "stderr": "", "interrupted": false, "isImage": false}
	switch name {
	case "AskUserQuestion":
		fmt.Println("? " + question + " (type an answer, then Enter)")
		response = map[string]any{"questions": input["questions"], "answers": map[string]any{question: a.readLine()}}
	case "ExitPlanMode":
		fmt.Println("? approve the plan (Enter)")
		a.readLine()
		response = map[string]any{"plan": input["plan"]}
	}
	a.fire("PostToolUse", name, sub.fields(map[string]any{
		"tool_name": name, "tool_input": input, "tool_response": response, "tool_use_id": id,
		"duration_ms": time.Since(start).Milliseconds(),
	}))
}

func (a *agent) permission(sub *subagent) {
	id := a.toolID()
	input := map[string]any{"command": "rm -rf build", "description": "Remove the build directory"}
	fmt.Println("● Bash rm -rf build")
	a.fire("PreToolUse", "Bash", sub.fields(map[string]any{"tool_name": "Bash", "tool_input": input, "tool_use_id": id}))
	start := time.Now()
	a.fire("PermissionRequest", "Bash", sub.fields(map[string]any{"tool_name": "Bash", "tool_input": input, "permission_suggestions": []any{}}))
	a.fire("Notification", "permission_prompt", sub.fields(map[string]any{
		"message": "Claude needs your permission to use Bash", "title": "Permission needed", "notification_type": "permission_prompt",
	}))
	fmt.Println("? allow Bash: rm -rf build (Enter)")
	a.readLine()
	a.fire("PostToolUse", "Bash", sub.fields(map[string]any{
		"tool_name": "Bash", "tool_input": input, "tool_use_id": id, "duration_ms": time.Since(start).Milliseconds(),
		"tool_response": map[string]any{"stdout": "", "stderr": "", "interrupted": false, "isImage": false},
	}))
}

// fire runs the hooks configured for an event, one after another, and logs
// each reply.
func (a *agent) fire(event, value string, fields map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	payload := map[string]any{
		"session_id":      a.sessionID,
		"transcript_path": a.transcript,
		"cwd":             a.cwd,
		"hook_event_name": event,
		"permission_mode": a.mode,
	}
	for k, v := range fields {
		payload[k] = v
	}
	body, _ := json.Marshal(payload)
	for _, g := range a.hooks[event] {
		if matcherSupport[event] && !matches(g.Matcher, value) {
			continue
		}
		for _, h := range g.Hooks {
			if h.Type == "command" {
				a.runHook(event, value, h, body)
			}
		}
	}
}

func (a *agent) runHook(event, value string, h handler, body []byte) {
	timeout := time.Duration(h.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 600 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var cmd *exec.Cmd
	if h.Args != nil {
		cmd = exec.CommandContext(ctx, h.Command, h.Args...)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", h.Command)
	}
	// Hooks run in their own session, without a controlling terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(body), &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	case err != nil:
		code = -1
	}
	timedOut := ctx.Err() != nil
	failed := timedOut || (code != 0 && code != 2)
	a.record(map[string]any{
		"t": "hook", "event": event, "match": value, "exit": code, "stdout": stdout.String(),
		"stderr": stderr.String(), "ms": time.Since(start).Milliseconds(), "timed_out": timedOut,
		"blocked": code == 2 || failed && h.OnFailure == "block",
	})
}
