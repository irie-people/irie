package core

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/testutil"
	"github.com/irie-hq/irie/internal/testutil/ollamafake"
	"github.com/irie-hq/irie/internal/ui"
)

// models are a model a coding agent can run, and one that cannot call
// tools. Both are small, so they fit any machine that runs the tests.
var models = []ollamafake.Model{
	{Name: "qwen3.5:9b", Size: 100 << 20, Tools: true},
	{Name: "chat:1b", Size: 50 << 20},
}

// ownServer makes the fake Ollama Irie's own server, at a free port, and
// the fake agent the local profile's command. It returns the file the fake
// server writes its environment to.
func ownServer(t *testing.T, a *agentConfig) (log string, port int) {
	t.Helper()
	dir := t.TempDir()
	list := filepath.Join(dir, "models.json")
	b, _ := json.Marshal(models)
	if err := os.WriteFile(list, b, 0o600); err != nil {
		t.Fatal(err)
	}
	log = filepath.Join(dir, "ollama-env.json")
	// tmux passes the environment it starts with to the server's window.
	t.Setenv("IRIE_FAKE_OLLAMA_MODELS", list)
	t.Setenv("IRIE_FAKE_OLLAMA_LOG", log)
	port = testutil.FreePort(t)
	a.local = fmt.Sprintf("ollama = %q\nport = %d\n", testutil.BuildFakeOllama(t), port)
	return log, port
}

// localProfile runs the local profile on the fake agent.
func localProfile(t *testing.T) string {
	return fmt.Sprintf("[profiles.local]\ncommand = %q\n", testutil.BuildFakeAgent(t))
}

func TestALocalSpawnRunsOnIriesOwnServer(t *testing.T) {
	a := agentConfig{extra: localProfile(t)}
	log, port := ownServer(t, &a)
	c := setupWith(t, a)

	// The local profile has no model of its own, and the server has one an
	// agent can run, so it runs that one.
	var r ipc.SpawnResult
	if err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "local"}, &r); err != nil {
		t.Fatal(err)
	}
	row := c.waitState(r.Session.ID, ipc.StateWorking)
	if !row.Local || row.Model != "qwen3.5:9b" {
		t.Errorf("row = %+v", row)
	}

	// Irie's own server runs in its own window, with Irie's settings.
	var server map[string]string
	for _, p := range c.panes() {
		if p["@irie_role"] == "runtime" {
			server = p
		}
	}
	// It runs under `irie runtime`, which stops its model runners with it.
	if server == nil || server["session_name"] != "irie-agents" || server["pane_current_command"] != "irie" {
		t.Fatalf("server pane = %v", server)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]string
	json.Unmarshal(b, &env)
	for k, want := range map[string]string{
		"OLLAMA_HOST": fmt.Sprintf("127.0.0.1:%d", port), "OLLAMA_CONTEXT_LENGTH": "65536", "OLLAMA_KV_CACHE_TYPE": "q8_0",
		"OLLAMA_FLASH_ATTENTION": "1", "OLLAMA_NUM_PARALLEL": "1", "OLLAMA_MAX_LOADED_MODELS": "1",
	} {
		if env[k] != want {
			t.Errorf("server %s = %q, want %q", k, env[k], want)
		}
	}

	// The agent is pointed at that server and model, with the context the
	// check measured.
	spec, err := host.ReadSpec(c.paths.SpecFile(r.Session.ID))
	if err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	for k, want := range map[string]string{
		"ANTHROPIC_BASE_URL": url, "ANTHROPIC_AUTH_TOKEN": "ollama", "ANTHROPIC_API_KEY": "",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "qwen3.5:9b", "CLAUDE_CODE_SUBAGENT_MODEL": "qwen3.5:9b",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "65536",
	} {
		if v, ok := spec.Env[k]; !ok || v != want {
			t.Errorf("agent %s = %q (set %v), want %q", k, v, ok, want)
		}
	}
	if i := slices.Index(spec.Argv, "--model"); i < 0 || spec.Argv[i+1] != "qwen3.5:9b" {
		t.Errorf("argv = %q", spec.Argv)
	}
	if !spec.Local || spec.Model != "qwen3.5:9b" {
		t.Errorf("spec = %+v", spec)
	}

	// A model that cannot call tools is refused, and nothing is spawned.
	n := c.agentPanes()
	err = c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "ollama:chat:1b"}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot call tools") {
		t.Errorf("+ollama:chat:1b: %v", err)
	}
	if c.agentPanes() != n {
		t.Error("a refused spawn made a pane")
	}

	// The + list offers the model with what it costs; the one without tools
	// is left out.
	var cr ipc.CompleteResult
	if err := c.call("complete", ipc.CompleteArgs{Text: "+oll"}, &cr); err != nil || !slices.Equal(cr.Items, []string{"+ollama:qwen3.5:9b"}) {
		t.Fatalf("complete +oll = %+v, %v", cr, err)
	}
	if note := cr.Notes[0]; !strings.Contains(note, "local, $0") || !strings.Contains(note, "in memory") {
		t.Errorf("note = %q", note)
	}

	// The models, weighed: the cheapest in memory first, the loaded one
	// measured, the other estimated.
	var lr ipc.LocalResult
	if err := c.call("local", ipc.LocalArgs{}, &lr); err != nil {
		t.Fatal(err)
	}
	if !lr.Own || !lr.Running || lr.URL != url || len(lr.Models) != 2 {
		t.Fatalf("local = %+v", lr)
	}
	if !slices.IsSortedFunc(lr.Models, func(a, b ipc.LocalModel) int { return cmp.Compare(a.Need, b.Need) }) {
		t.Errorf("models not cheapest first: %+v", lr.Models)
	}
	byName := map[string]ipc.LocalModel{}
	for _, m := range lr.Models {
		byName[m.Name] = m
	}
	if m := byName["qwen3.5:9b"]; !m.Loaded || !m.Measured || m.Need != 100<<20+64<<20 || !m.Fits {
		t.Errorf("qwen3.5:9b = %+v", m)
	}
	if m := byName["chat:1b"]; m.Why != "cannot call tools" || m.Measured || m.Need <= m.Size {
		t.Errorf("chat:1b = %+v", m)
	}

	// A check that passes with remember makes the model +local's.
	var pr ipc.PreflightResult
	if err := c.call("preflight", ipc.PreflightArgs{Profile: "local", Model: "qwen3.5:9b", Remember: true}, &pr); err != nil || !pr.OK || pr.Context != 65536 {
		t.Fatalf("preflight = %+v, %v", pr, err)
	}
	b, _ = os.ReadFile(c.paths.LocalFile())
	if !strings.Contains(string(b), `"model": "qwen3.5:9b"`) || !strings.Contains(string(b), "measured") {
		t.Errorf("local.json = %s", b)
	}

	// A restarted core finds its server again rather than starting another.
	c.restartCore()
	if err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "local"}, &r); err != nil {
		t.Fatal(err)
	}
	c.waitState(r.Session.ID, ipc.StateWorking)
	servers := 0
	for _, p := range c.panes() {
		if p["@irie_role"] == "runtime" {
			servers++
		}
	}
	if servers != 1 {
		t.Errorf("%d servers after a restart, want 1", servers)
	}
}

func TestALocalSpawnOnAShortContextIsRefused(t *testing.T) {
	// The user's own server, at Ollama's default context below 24 GiB of
	// VRAM.
	srv := httptest.NewServer(ollamafake.New(4096, models).Handler())
	defer srv.Close()
	c := setupWith(t, agentConfig{extra: localProfile(t), local: fmt.Sprintf("ollama = \"/nonexistent/ollama\"\nurl = %q\n", srv.URL)})

	err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "local"}, nil)
	if err == nil || !strings.Contains(err.Error(), "runs with a 4k context") || !strings.Contains(err.Error(), "OLLAMA_CONTEXT_LENGTH") {
		t.Fatalf("spawn: %v", err)
	}
	// --force spawns anyway, says why, and tells Claude Code the real
	// context so it compacts in time.
	var r ipc.SpawnResult
	if err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "local", Force: true}, &r); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(r.Session.Warnings, func(w string) bool { return strings.Contains(w, "4k context") }) {
		t.Errorf("warnings = %q", r.Session.Warnings)
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(r.Session.ID))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "4096" || spec.Env["ANTHROPIC_BASE_URL"] != srv.URL {
		t.Errorf("env = %v", spec.Env)
	}
	// The user's own server is used as it is; Irie starts none of its own.
	for _, p := range c.panes() {
		if p["@irie_role"] == "runtime" {
			t.Errorf("Irie started a server beside the user's: %v", p)
		}
	}
}

func TestTheStatusLineGivesCostAndPlanUse(t *testing.T) {
	// The user's own status line, which Irie's runs after it.
	home := t.TempDir()
	got := filepath.Join(home, "status-input.json")
	testutil.WriteFiles(t, home, map[string]string{
		".claude/settings.json": fmt.Sprintf(`{"statusLine": {"type": "command", "command": "cat > %s; printf mine", "padding": 1}}`, got),
	})
	t.Setenv("HOME", home)
	c := setupWith(t, agentConfig{statusLine: true})
	s := c.spawn("prompt,status:0.42,stop")
	c.waitState(s.ID, ipc.StateDone)

	// The row has the session's cost and context, and the header the plan's
	// use.
	var row ipc.Session
	testutil.Eventually(t, 5*time.Second, "no cost on the row", func() bool {
		row, _ = c.row(s.ID)
		return row.CostUSD != nil
	})
	if *row.CostUSD != 0.42 || row.Context == nil || *row.Context != 12 {
		t.Errorf("row cost %v, context %v", *row.CostUSD, row.Context)
	}
	cl, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	frames, err := cl.Subscribe(ipc.SubscribeArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if f := <-frames; f.Header == nil || f.Header.Plan == nil || f.Header.Plan.FiveHour == nil || f.Header.Plan.FiveHour.Used != 33 {
		t.Errorf("snapshot header = %+v", f.Header)
	}

	// The user's status line got the same input, and Claude Code shows what
	// it printed.
	b, err := os.ReadFile(got)
	if err != nil || !strings.Contains(string(b), `"total_cost_usd":0.42`) {
		t.Errorf("the user's status line read %s (%v)", b, err)
	}
	var printed []string
	for _, r := range c.agentLog(s.ID) {
		if r["t"] == "status" {
			printed = append(printed, fmt.Sprint(r["stdout"]))
		}
	}
	if !slices.Equal(printed, []string{"mine"}) {
		t.Errorf("status line printed %q, want the user's own", printed)
	}
	settings, _ := os.ReadFile(filepath.Join(c.paths.Session(s.ID), "claude-settings.json"))
	if !strings.Contains(string(settings), `"padding": 1`) {
		t.Errorf("the user's padding is lost:\n%s", settings)
	}

	// A restarted core still knows the cost, from status.json.
	c.restartCore()
	if row, _ := c.row(s.ID); row.CostUSD == nil || *row.CostUSD != 0.42 {
		t.Errorf("after a restart, cost %v", row.CostUSD)
	}
}

func TestTheStatusLineHearsTheStateAndTheHelpers(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,sub:helper1:sleep:2s,stop")
	cl, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	status := func() ipc.StatusReply {
		var r ipc.StatusReply
		if err := cl.Call(ipc.OpStatus, ipc.StatusArgs{Session: s.ID, Payload: json.RawMessage(`{"cost": {"total_cost_usd": 0.1}}`)}, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	testutil.Eventually(t, 5*time.Second, "the answer has no helper", func() bool {
		r := status()
		return r.State == ipc.StateWorking && len(r.Subagents) == 1 && r.Subagents[0].Type == "Explore"
	})
	// While it works it has its card's colour: in ui.theme's palette until
	// the Mac app says which one it draws in.
	if r := status(); r.State == ipc.StateWorking && r.Color != ui.AgentColor("rosewater", s.ID) {
		t.Errorf("working colour %q, want rosewater's %q", r.Color, ui.AgentColor("rosewater", s.ID))
	}
	if err := cl.Call(ipc.OpTheme, ipc.ThemeArgs{Theme: "evergreen"}, nil); err != nil {
		t.Fatal(err)
	}
	if r := status(); r.State == ipc.StateWorking && r.Color != ui.AgentColor("evergreen", s.ID) {
		t.Errorf("after the app's palette, colour %q, want evergreen's %q", r.Color, ui.AgentColor("evergreen", s.ID))
	}
	if err := cl.Call(ipc.OpTheme, ipc.ThemeArgs{Theme: "plaid"}, nil); code(err) != ipc.CodeBadArgs {
		t.Errorf("an unknown palette: %v", err)
	}
	c.waitState(s.ID, ipc.StateDone)
	if r := status(); r.State != ipc.StateDone || len(r.Subagents) != 0 || r.Color != "" {
		t.Errorf("after the stop: %+v", r)
	}
}

func TestMemoryIsMeasured(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt")
	c.waitState(s.ID, ipc.StateWorking)
	// Every sampleEvery ticks the core measures each session's processes
	// and the machine's memory.
	var row ipc.Session
	testutil.Eventually(t, 3*time.Duration(sampleEvery)*time.Second, "no memory on the row", func() bool {
		row, _ = c.row(s.ID)
		return row.Memory > 0
	})
	if row.Memory > 1<<30 {
		t.Errorf("the fake agent takes %d bytes", row.Memory)
	}
	cl, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	frames, err := cl.Subscribe(ipc.SubscribeArgs{})
	if err != nil {
		t.Fatal(err)
	}
	f := <-frames
	if f.Header == nil || f.Header.Memory == nil {
		t.Skip("no memory reading on this system")
	}
	if m := f.Header.Memory; m.Total <= 0 || m.Agents < row.Memory || m.Budget != m.Total/3*2 {
		t.Errorf("header memory = %+v", m)
	}
}

// TestAMoveOntoALocalProfileIsChecked: a session moved in onto a local
// profile runs on its model, as a spawn there does, and one the check
// refuses keeps running where it was.
func TestAMoveOntoALocalProfileIsChecked(t *testing.T) {
	srv := httptest.NewServer(ollamafake.New(65536, models).Handler())
	defer srv.Close()
	f := newFakeClaude(t)
	c := setupWith(t, agentConfig{
		command: f.script, readyTimeout: "2s",
		extra: fmt.Sprintf("[profiles.local]\ncommand = %q\n", f.script),
		local: fmt.Sprintf("ollama = \"/nonexistent/ollama\"\nurl = %q\n", srv.URL),
	})
	done := f.add(ipc.KindInteractive, ipc.StatusIdle, idleID, "outside-task", t.TempDir())
	c.waitOutside("the idle session was not listed", func(o []ipc.OutsideSession) bool { return len(o) == 1 && o[0].Movable })

	err := c.call(ipc.OpMove, ipc.MoveArgs{VendorID: idleID, Profile: "ollama:chat:1b"}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot call tools") {
		t.Fatalf("move onto +ollama:chat:1b: %v", err)
	}
	if exits(done) {
		t.Fatal("a refused move ended the outside copy")
	}

	var r ipc.MoveResult
	if err := c.call(ipc.OpMove, ipc.MoveArgs{VendorID: idleID, Profile: "local"}, &r); err != nil {
		t.Fatal(err)
	}
	if !exits(done) {
		t.Fatal("the outside copy still runs")
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(r.Session.ID))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["ANTHROPIC_BASE_URL"] != srv.URL || spec.Env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "65536" || !spec.Local || spec.Model != "qwen3.5:9b" {
		t.Errorf("spec: env %v, local %v, model %q", spec.Env, spec.Local, spec.Model)
	}
	if n := len(spec.Argv); n < 2 || spec.Argv[n-2] != "--resume" || spec.Argv[n-1] != idleID {
		t.Errorf("argv = %q", spec.Argv)
	}
	if row := c.waitState(r.Session.ID, ipc.StateIdle); !row.Local || row.Model != "qwen3.5:9b" || row.Profile != "local" {
		t.Errorf("row = %+v", row)
	}
}
