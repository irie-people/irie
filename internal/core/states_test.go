package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/testutil"
	"github.com/irie-hq/irie/internal/tmux"
)

// row returns the session's row, if it is listed.
func (c *coreEnv) row(id string) (ipc.Session, bool) {
	for _, s := range c.list() {
		if s.ID == id {
			return s, true
		}
	}
	return ipc.Session{}, false
}

// waitState waits until the session's row is in state and returns the row.
func (c *coreEnv) waitState(id, state string) ipc.Session {
	c.t.Helper()
	var last ipc.Session
	deadline := time.Now().Add(15 * time.Second)
	for {
		if s, ok := c.row(id); ok {
			if last = s; s.State == state {
				return s
			}
		}
		if time.Now().After(deadline) {
			screen, _ := c.TryTmux("capture-pane", "-p", "-t", last.Pane)
			log, _ := os.ReadFile(filepath.Join(c.paths.Session(id), "fakeagent.ndjson"))
			c.t.Fatalf("session %s is %q (%s), want %q\nscreen:\n%s\nagent log:\n%s", id, last.State, last.Detail, state, screen, log)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// states polls a session until it reaches final and returns the states it
// was seen in, in order and without repeats.
func (c *coreEnv) states(id, final string) []string {
	c.t.Helper()
	var seen []string
	deadline := time.Now().Add(15 * time.Second)
	for {
		if s, ok := c.row(id); ok && (len(seen) == 0 || seen[len(seen)-1] != s.State) {
			seen = append(seen, s.State)
		}
		if len(seen) > 0 && seen[len(seen)-1] == final {
			return seen
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("session %s went through %v, never %q", id, seen, final)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// keys types into a session's pane.
func (c *coreEnv) keys(s ipc.Session, keys ...string) {
	c.t.Helper()
	c.Tmux(c.t, append([]string{"send-keys", "-t", s.Pane}, keys...)...)
}

// agentLog reads what the fake agent of a session logged.
func (c *coreEnv) agentLog(id string) []map[string]any {
	c.t.Helper()
	b, err := os.ReadFile(filepath.Join(c.paths.Session(id), "fakeagent.ndjson"))
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

// journal reads every line of the journal, strictly, and fails the test on
// a line that is not a valid envelope (DESIGN §5.1).
func (c *coreEnv) journal() []event.Event {
	c.t.Helper()
	files, err := filepath.Glob(filepath.Join(c.paths.Journal(), "*.ndjson"))
	if err != nil || len(files) == 0 {
		c.t.Fatalf("no journal files in %s: %v", c.paths.Journal(), err)
	}
	var out []event.Event
	for _, f := range files {
		if fi, err := os.Stat(f); err != nil || fi.Mode().Perm() != 0o600 {
			c.t.Errorf("%s: mode %v, %v", f, fi.Mode().Perm(), err)
		}
		b, err := os.ReadFile(f)
		if err != nil {
			c.t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			dec := json.NewDecoder(strings.NewReader(line))
			dec.DisallowUnknownFields()
			var e event.Event
			if err := dec.Decode(&e); err != nil {
				c.t.Errorf("%s: %q: %v", filepath.Base(f), line, err)
				continue
			}
			if err := event.Validate(e); err != nil {
				c.t.Errorf("%s: %q: %v", filepath.Base(f), line, err)
			}
			out = append(out, e)
		}
	}
	return out
}

func (c *coreEnv) events(id string) []event.Event {
	var out []event.Event
	for _, e := range c.journal() {
		if e.Session == id {
			out = append(out, e)
		}
	}
	return out
}

// within reads the NDJSON events that `irie ctl watch` prints, and reports
// whether one that ok accepts arrives within d. Every line must be a valid
// envelope.
func within(t *testing.T, lines <-chan string, d time.Duration, ok func(event.Event) bool) bool {
	t.Helper()
	wait := time.After(d)
	for {
		select {
		case l, open := <-lines:
			if !open {
				return false
			}
			dec := json.NewDecoder(strings.NewReader(l))
			dec.DisallowUnknownFields()
			var e event.Event
			if err := dec.Decode(&e); err != nil {
				t.Fatalf("watch printed %q: %v", l, err)
			}
			if err := event.Validate(e); err != nil {
				t.Errorf("watch printed %q: %v", l, err)
			}
			if ok(e) {
				return true
			}
		case <-wait:
			return false
		}
	}
}

func kinds(evs []event.Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

func TestPromptToolStopIsWorkingThenDone(t *testing.T) {
	c := setupWith(t, agentConfig{env: map[string]string{"IRIE_FAKE_PACE": "700ms"}})
	s := c.spawn("prompt,tool:Bash,stop")
	seen := c.states(s.ID, ipc.StateDone)
	if w := slices.Index(seen, ipc.StateWorking); w < 0 || w > slices.Index(seen, ipc.StateDone) {
		t.Errorf("states %v: want working, then done", seen)
	}
	for _, st := range seen {
		if st != ipc.StateStarting && st != ipc.StateWorking && st != ipc.StateDone {
			t.Errorf("states %v: %s does not belong", seen, st)
		}
	}
	if r, _ := c.row(s.ID); r.Detail == "" && r.State != ipc.StateDone {
		t.Errorf("row = %+v", r)
	}
	// Viewing a finished session makes it idle.
	c.show(s.ID, false)
	c.waitState(s.ID, ipc.StateIdle)
}

func TestASubagentIsListedUnderItsSessionUntilItStops(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,sub:helper1:tool:Grep+sleep:2s,stop")
	testutil.Eventually(t, 5*time.Second, "the subagent was not listed", func() bool {
		r, _ := c.row(s.ID)
		return len(r.Subagents) == 1 && r.Subagents[0].Agent == "sub:helper1" && r.Subagents[0].Type == "Explore"
	})
	c.waitState(s.ID, ipc.StateDone)
	if r, _ := c.row(s.ID); len(r.Subagents) != 0 {
		t.Errorf("after the stop: subagents %+v", r.Subagents)
	}
}

func TestRedUntilAnswered(t *testing.T) {
	c := setup(t)
	permission := c.spawn("prompt,permission,tool:Bash")
	question := c.spawn("prompt,tool:AskUserQuestion")
	limited := c.spawn("prompt,stopfail:rate_limit")

	r := c.waitState(permission.ID, ipc.StateNeedsYou)
	// The row asks in plain words; the call it asks about is in its command.
	if r.Reason != "permission" || r.Detail != "Asks: may it run rm -rf build?" || r.Command != "Bash · rm -rf build" {
		t.Errorf("permission row = %+v", r)
	}
	c.keys(permission, "Enter")
	c.waitState(permission.ID, ipc.StateWorking)

	r = c.waitState(question.ID, ipc.StateNeedsYou)
	if r.Reason != "question" || !strings.Contains(strings.ToLower(r.Detail), "which colour should the button be?") {
		t.Errorf("question row = %+v", r)
	}

	r = c.waitState(limited.ID, ipc.StateNeedsYou)
	if r.Reason != "rate_limit" || !strings.Contains(r.Detail, "rate_limit") {
		t.Errorf("rate-limited row = %+v", r)
	}

	// Tool calls and what they ask are in plain words too.
	said := map[string]string{}
	for _, e := range c.journal() {
		if p := e.Str("phrase"); p != "" {
			said[p] = e.Kind
		}
	}
	for p, kind := range map[string]string{
		"Deleting build":                           event.ToolStarted,
		"Asks: may it run rm -rf build?":           event.InputNeeded,
		"Asks: which colour should the button be?": event.InputNeeded,
	} {
		if said[p] != kind {
			t.Errorf("no %s says %q: %v", kind, p, said)
		}
	}
}

// TestTheManualCheck follows M2's manual check with the fake agent: an agent
// asks a question before it starts, its row is red until it is answered, it
// finishes with ✓ and turns to ○ once viewed.
func TestTheManualCheck(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,ask,stop")
	if r := c.waitState(s.ID, ipc.StateNeedsYou); r.Reason != "question" {
		t.Errorf("row = %+v", r)
	}
	c.keys(s, "-l", "Green")
	c.keys(s, "Enter")
	c.waitState(s.ID, ipc.StateDone)
	c.show(s.ID, true)
	c.waitState(s.ID, ipc.StateIdle)
}

func TestNoHooksInTimeIsStalledWithTheScreenTail(t *testing.T) {
	c := setupWith(t, agentConfig{readyTimeout: "1s"})
	s := c.spawn("sleep:3s,prompt")
	r := c.waitState(s.ID, ipc.StateStalled)
	if len(r.Tail) == 0 || len(r.Tail) > 5 || !slices.ContainsFunc(r.Tail, func(l string) bool { return strings.Contains(l, "fakeagent") }) {
		t.Errorf("tail = %q, want up to 5 screen lines with the agent's banner", r.Tail)
	}
	if r.Reason != "stalled" {
		t.Errorf("row = %+v", r)
	}
	// Once the agent gets going, the row recovers.
	c.waitState(s.ID, ipc.StateWorking)
}

func TestSpawnsThatCannotStartFail(t *testing.T) {
	for _, c := range []struct {
		name string
		a    agentConfig
		task string
		code int
		tail string
	}{
		{"exit at start", agentConfig{}, "exit:1", 1, "fakeagent: exit 1"},
		{"command not found", agentConfig{command: "/home/alice/no/such/claude"}, "prompt", 127, "command not found"},
		{"flag value rejected", agentConfig{mode: "sometimes"}, "prompt", 1, "argument 'sometimes' is invalid"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := setupWith(t, c.a)
			s := e.spawn(c.task)
			r := e.waitState(s.ID, ipc.StateFailed)
			if r.ExitCode == nil || *r.ExitCode != c.code {
				t.Errorf("exit code %v, want %d", r.ExitCode, c.code)
			}
			if !slices.ContainsFunc(r.Tail, func(l string) bool { return strings.Contains(l, c.tail) }) {
				t.Errorf("tail = %q, want a line with %q", r.Tail, c.tail)
			}
			if !strings.Contains(r.Detail, strconv.Itoa(c.code)) {
				t.Errorf("detail = %q", r.Detail)
			}
			got := kinds(e.events(s.ID))
			if !slices.Contains(got, event.SpawnRequested) || !slices.Contains(got, event.SpawnFailed) || slices.Contains(got, event.PromptSubmitted) {
				t.Errorf("journal kinds = %v", got)
			}
		})
	}
}

// stopCore stops the core for good: its supervisor passes the signal on and
// exits.
func (c *coreEnv) stopCore() {
	c.t.Helper()
	pid := c.Tmux(c.t, "display-message", "-p", "-t", tmux.AgentsSession+":core", "#{pane_pid}")
	if out, err := exec.Command("kill", "-TERM", pid).CombinedOutput(); err != nil {
		c.t.Fatalf("kill %s: %v %s", pid, err, out)
	}
	testutil.Eventually(c.t, 5*time.Second, "the core still answers", func() bool {
		return c.call("hello", ipc.HelloArgs{Client: "test", API: ipc.API}, nil) != nil
	})
}

func TestHooksFailOpenWhileTheCoreIsStopped(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,ask,tool:Bash,stop")
	c.waitState(s.ID, ipc.StateNeedsYou)
	before := len(c.agentLog(s.ID))
	c.stopCore()
	c.keys(s, "Enter")
	var after []map[string]any
	testutil.Eventually(t, 10*time.Second, "the agent did not finish its turn without the core", func() bool {
		after = c.agentLog(s.ID)[before:]
		return slices.ContainsFunc(after, func(r map[string]any) bool { return r["t"] == "hook" && r["event"] == "Stop" })
	})
	hooks := 0
	for _, r := range after {
		if r["t"] != "hook" {
			continue
		}
		hooks++
		if r["exit"] != 0.0 || r["stdout"] != "" || r["blocked"] != false {
			t.Errorf("hook reply while the core was stopped: %v", r)
		}
		if ms, _ := r["ms"].(float64); ms >= 300 && !demoted(t) {
			t.Errorf("%s took %v ms, want under 300", r["event"], ms)
		}
	}
	if hooks < 4 {
		t.Errorf("%d hooks ran while the core was stopped, want PostToolUse, PreToolUse, PostToolUse and Stop", hooks)
	}
}

func TestEveryJournalLineIsAnEnvelope(t *testing.T) {
	c := setupWith(t, agentConfig{readyTimeout: "1s"})
	full := c.spawn("prompt,tool:Read,sub:a1b2c3:tool:Bash+tool:Grep,ask,stop,stopfail:overloaded,exit:3")
	failed := c.spawn("exit:1")
	stalled := c.spawn("sleep:3s,prompt")
	ended := c.spawn("prompt,stop")

	c.waitState(full.ID, ipc.StateNeedsYou)
	c.show(full.ID, false)
	c.keys(full, "Enter")
	if r := c.waitState(full.ID, ipc.StateExited); r.ExitCode == nil || *r.ExitCode != 3 {
		t.Errorf("full row = %+v", r)
	}
	c.waitState(failed.ID, ipc.StateFailed)
	c.waitState(stalled.ID, ipc.StateStalled)
	c.waitState(stalled.ID, ipc.StateWorking)
	c.waitState(ended.ID, ipc.StateDone)
	c.keys(ended, "C-d")
	c.waitState(ended.ID, ipc.StateExited)

	evs := c.journal()
	var last uint64
	seen := map[string]bool{}
	for _, e := range evs {
		if e.Seq <= last {
			t.Errorf("seq %d after %d", e.Seq, last)
		}
		last = e.Seq
		seen[e.Kind] = true
		if e.Session == full.ID && e.Vendor != nil && e.Vendor.AgentID == "a1b2c3" && e.Agent != "sub:a1b2c3" {
			t.Errorf("a subagent's event belongs to %q: %+v", e.Agent, e)
		}
		if e.Source == event.SourceHook && (e.Vendor == nil || e.Vendor.HookEvent == "" || e.Agent == "") {
			t.Errorf("a hook event without its vendor ids or agent: %+v", e)
		}
	}
	for _, k := range []string{
		event.SpawnRequested, event.SpawnFailed, event.SessionStarted, event.PromptSubmitted,
		event.ToolStarted, event.ToolFinished, event.InputNeeded, event.TurnEnded, event.TurnFailed,
		event.SubagentStarted, event.SubagentFinished, event.SessionEnded, event.PaneExited, event.UIShown,
	} {
		if !seen[k] {
			t.Errorf("no %s in the journal", k)
		}
	}
	if !slices.ContainsFunc(c.events(stalled.ID), func(e event.Event) bool {
		return e.Kind == event.InputNeeded && e.Str("reason") == event.ReasonStalled && e.Source == event.SourceIrie
	}) {
		t.Errorf("no stall in %v", kinds(c.events(stalled.ID)))
	}
	// The journal records spawn.requested, then session.started or spawn.failed.
	for _, s := range []ipc.Session{full, failed, stalled, ended} {
		k := kinds(c.events(s.ID))
		if len(k) < 2 || k[0] != event.SpawnRequested {
			t.Errorf("%s: journal kinds %v", s.Name, k)
		}
	}
}

func TestTheAgentGetsIriesEnvAndNoTMUX(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt")
	c.waitState(s.ID, ipc.StateWorking)
	log := c.agentLog(s.ID)
	if len(log) == 0 || log[0]["t"] != "start" {
		t.Fatalf("agent log = %v", log)
	}
	env, _ := log[0]["env"].(map[string]any)
	for k, want := range map[string]string{"IRIE_SESSION": s.ID, "IRIE_SOCKET": c.paths.Socket(), "IRIE_BIN": c.bin} {
		if env[k] != want {
			t.Errorf("%s = %v, want %s", k, env[k], want)
		}
	}
	for _, k := range []string{"TMUX", "TMUX_PANE"} {
		if v, ok := env[k]; ok {
			t.Errorf("the agent sees %s=%v", k, v)
		}
	}
	var argv []string
	for _, a := range log[0]["argv"].([]any) {
		argv = append(argv, a.(string))
	}
	settings := filepath.Join(c.paths.Session(s.ID), "claude-settings.json")
	for flag, want := range map[string]string{"--settings": settings, "--session-id": s.VendorID} {
		if i := slices.Index(argv, flag); i < 0 || argv[i+1] != want {
			t.Errorf("argv %q: want %s %s", argv, flag, want)
		}
	}
	// Given no name, Claude Code titles the session itself (title.go).
	if slices.Contains(argv, "--name") {
		t.Errorf("argv %q: --name for a session given no name", argv)
	}
	if fi, err := os.Stat(settings); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("settings file: %v", err)
	}
	// Irie says nothing back, but for the delegation note when the session
	// starts (DESIGN §6.4).
	for _, r := range log {
		if r["t"] != "hook" {
			continue
		}
		hint := r["event"] == "SessionStart" && strings.Contains(fmt.Sprint(r["stdout"]), "ctl delegate")
		if r["exit"] != 0.0 || r["stdout"] != "" && !hint {
			t.Errorf("hook reply %v", r)
		}
	}
}

func TestAnOldClaudeStillSpawnsAndSaysWhy(t *testing.T) {
	c := setupWith(t, agentConfig{env: map[string]string{"IRIE_FAKE_VERSION": "2.1.200", "IRIE_FAKE_NO_NAME": "1"}})
	s := c.spawn("prompt")
	r := c.waitState(s.ID, ipc.StateWorking)
	warnings := strings.Join(r.Warnings, "\n")
	if !strings.Contains(warnings, "2.1.200") || !strings.Contains(warnings, "2.1.295") || !strings.Contains(warnings, "--name") {
		t.Errorf("warnings = %q", r.Warnings)
	}
	b, err := os.ReadFile(filepath.Join(c.paths.Session(s.ID), "claude-settings.json"))
	if err != nil || bytes.Contains(b, []byte("onFailure")) {
		t.Errorf("settings for an old Claude Code: %v\n%s", err, b)
	}
}

func TestARestartedCoreKeepsTheDerivedStates(t *testing.T) {
	c := setup(t)
	done := c.spawn("prompt,stop")
	asks := c.spawn("prompt,ask")
	viewed := c.spawn("prompt,stop")
	failed := c.spawn("exit:2")
	c.waitState(done.ID, ipc.StateDone)
	c.waitState(asks.ID, ipc.StateNeedsYou)
	c.waitState(viewed.ID, ipc.StateDone)
	c.show(viewed.ID, false)
	c.waitState(viewed.ID, ipc.StateIdle)
	c.waitState(failed.ID, ipc.StateFailed)
	before := c.list()

	kill(t, c.supervised(tmux.AgentsSession+":core"))
	var after []ipc.Session
	testutil.Eventually(t, 10*time.Second, "the core did not come back", func() bool {
		var r ipc.ListResult
		if c.call("list", nil, &r) != nil {
			return false
		}
		after = r.Sessions
		return true
	})
	if len(after) != len(before) {
		t.Fatalf("rows before %+v, after %+v", before, after)
	}
	for i := range before {
		x, y := before[i], after[i]
		if x.ID != y.ID || x.State != y.State || x.Reason != y.Reason || (x.ExitCode == nil) != (y.ExitCode == nil) {
			t.Errorf("row %d: before %+v, after %+v", i, x, y)
		}
	}
	// The new core carries on from the journal.
	c.keys(asks, "Enter")
	c.waitState(asks.ID, ipc.StateWorking)
	var last uint64
	for _, e := range c.journal() {
		if e.Seq <= last {
			t.Errorf("seq %d after %d across the restart", e.Seq, last)
		}
		last = e.Seq
	}
}

func TestCtlSpawnsWithNoWindowOpen(t *testing.T) {
	c := prepare(t, agentConfig{terminal: true})
	ctl := func(args ...string) (string, string, error) {
		t.Helper()
		cmd := exec.Command(c.bin, append([]string{"ctl"}, args...)...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	// Nothing runs yet: ctl spawn starts Irie itself.
	out, errOut, err := ctl("spawn", "--wait", "prompt")
	if err != nil {
		t.Fatalf("ctl spawn: %v\n%s%s", err, out, errOut)
	}
	id := strings.TrimSpace(out)
	if !sessionID.MatchString(id) {
		t.Fatalf("ctl spawn printed %q", out)
	}
	if clients, _ := c.TryTmux("list-clients"); clients != "" {
		t.Errorf("a client is attached: %q", clients)
	}
	r := c.waitState(id, ipc.StateWorking)

	out, _, err = ctl("list", "--json")
	var list ipc.ListResult
	if err != nil || json.Unmarshal([]byte(out), &list) != nil || len(list.Sessions) != 1 || list.Sessions[0].State != ipc.StateWorking {
		t.Errorf("ctl list --json = %q, %v", out, err)
	}
	if out, _, err = ctl("list"); err != nil || !strings.Contains(out, id) || !strings.Contains(out, "working") || !strings.Contains(out, r.Name) {
		t.Errorf("ctl list = %q, %v", out, err)
	}

	// watch prints every event as NDJSON.
	watch := exec.Command(c.bin, "ctl", "watch")
	pipe, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { watch.Process.Kill(); watch.Wait() })
	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(pipe)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	// Showing the first session is an event; once watch prints it, watch
	// is subscribed.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, errOut, err := ctl("show", id); err != nil {
			t.Fatalf("ctl show: %v %s", err, errOut)
		}
		if within(t, lines, 200*time.Millisecond, func(e event.Event) bool { return e.Session == id && e.Kind == event.UIShown }) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watch never printed a show")
		}
	}
	if slot := c.slot(); slot == nil || slot["pane_id"] != r.Pane {
		t.Errorf("slot = %v, want %s", slot, r.Pane)
	}
	out, errOut, err = ctl("spawn", "prompt,tool:Bash,stop")
	if err != nil {
		t.Fatalf("ctl spawn: %v %s", err, errOut)
	}
	second := strings.TrimSpace(out)
	var got []string
	if !within(t, lines, 15*time.Second, func(e event.Event) bool {
		if e.Session == second {
			got = append(got, e.Kind)
		}
		return e.Session == second && e.Kind == event.TurnEnded
	}) {
		t.Fatalf("watched %v for the second session", got)
	}
	want := []string{event.SpawnRequested, event.SessionStarted, event.PromptSubmitted, event.ToolStarted, event.ToolFinished, event.TurnEnded}
	if !slices.Equal(got, want) {
		t.Errorf("watched %v, want %v", got, want)
	}

	// A spawn that cannot start fails loudly.
	out, errOut, err = ctl("spawn", "--wait", "exit:4")
	if err == nil || !strings.Contains(errOut, "failed") || !strings.Contains(errOut, "fakeagent: exit 4") {
		t.Errorf("ctl spawn --wait of a failing agent: %v\nstdout %q\nstderr %q", err, out, errOut)
	}
}
