package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/paths"
	"github.com/irie-hq/irie/internal/testutil"
	"github.com/irie-hq/irie/internal/tmux"
)

func TestMain(m *testing.M) { testutil.Main(m) }

// testProfile stands in for Claude Code: Irie's flags after the args become
// the shell's positional parameters and are ignored.
const testProfile = `
[spawn]
default_profile = "test"

[profiles.test]
command = "/bin/sh"
args = ["-c", "echo ready; sleep 600"]
`

type coreEnv struct {
	*testutil.Env
	t     *testing.T
	bin   string
	paths paths.Paths
}

func setup(t *testing.T) *coreEnv {
	t.Helper()
	testutil.NeedTmux(t)
	bin := testutil.BuildIrie(t)
	e := testutil.NewEnv(t)
	t.Setenv("IRIE_BIN", bin)
	e.WriteConfig(t, testProfile)
	p, err := paths.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	c := &coreEnv{Env: e, t: t, bin: bin, paths: p}
	c.up()
	return c
}

func (c *coreEnv) up() {
	c.t.Helper()
	if out, err := exec.Command(c.bin, "up").CombinedOutput(); err != nil {
		c.t.Fatalf("irie up: %v\n%s", err, out)
	}
}

func (c *coreEnv) call(op string, args, result any) error {
	cl, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		return err
	}
	defer cl.Close()
	return cl.Call(op, args, result)
}

func (c *coreEnv) spawn(task string) ipc.Session {
	c.t.Helper()
	var r ipc.SpawnResult
	if err := c.call("spawn", ipc.SpawnArgs{Task: task}, &r); err != nil {
		c.t.Fatalf("spawn %q: %v", task, err)
	}
	return r.Session
}

func (c *coreEnv) show(id string, focus bool) {
	c.t.Helper()
	if err := c.call("show", ipc.ShowArgs{Session: id, Focus: focus}, nil); err != nil {
		c.t.Fatalf("show %s: %v", id, err)
	}
}

func (c *coreEnv) list() []ipc.Session {
	c.t.Helper()
	var r ipc.ListResult
	if err := c.call("list", nil, &r); err != nil {
		c.t.Fatalf("list: %v", err)
	}
	return r.Sessions
}

// panes lists every pane of the private server as maps of format fields.
func (c *coreEnv) panes() []map[string]string {
	c.t.Helper()
	keys := []string{"session_name", "window_index", "window_panes", "pane_index", "pane_id", "pane_width", "pane_height", "pane_current_command", "@irie_role", "@irie_session", "@irie_agent", "@irie_name", "@irie_created", "@irie_vendor_id"}
	var f []string
	for _, k := range keys {
		f = append(f, "#{"+k+"}")
	}
	// The trailing "|" keeps empty last fields from being trimmed away.
	out := c.Tmux(c.t, "list-panes", "-a", "-F", strings.Join(f, "\t")+"\t|")
	var rows []map[string]string
	for _, line := range strings.Split(out, "\n") {
		vals := strings.Split(line, "\t")
		if len(vals) != len(keys)+1 {
			continue
		}
		row := map[string]string{}
		for i, k := range keys {
			row[k] = vals[i]
		}
		rows = append(rows, row)
	}
	return rows
}

func (c *coreEnv) pane(id string) map[string]string {
	for _, p := range c.panes() {
		if p["pane_id"] == id {
			return p
		}
	}
	return nil
}

func (c *coreEnv) slot() map[string]string {
	for _, p := range c.panes() {
		if p["session_name"] == tmux.MainSession && p["window_index"] == "0" && p["pane_index"] == "1" {
			return p
		}
	}
	return nil
}

func TestUpBuildsTheLayout(t *testing.T) {
	c := setup(t)
	var main []map[string]string
	var core map[string]string
	for _, p := range c.panes() {
		if p["session_name"] == tmux.MainSession && p["window_index"] == "0" {
			main = append(main, p)
		}
		if p["session_name"] == tmux.AgentsSession && p["@irie_role"] == tmux.RoleCore {
			core = p
		}
	}
	if len(main) != 2 {
		t.Fatalf("irie:0 has %d panes, want 2", len(main))
	}
	if main[0]["@irie_role"] != tmux.RoleUI || main[0]["pane_width"] != "34" {
		t.Errorf("pane 0 = %v, want the 34-column sidebar", main[0])
	}
	if main[1]["@irie_role"] != tmux.RolePlaceholder {
		t.Errorf("pane 1 = %v, want the placeholder", main[1])
	}
	if core == nil {
		t.Fatal("no irie-agents:core pane")
	}
	if got := c.Tmux(t, "display-message", "-p", "-t", tmux.AgentsSession+":core", "#{pane_current_command}"); got != "irie" {
		t.Errorf("irie-agents:core runs %q", got)
	}

	var hello ipc.HelloResult
	if err := c.call("hello", ipc.HelloArgs{Client: "test", API: ipc.API}, &hello); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(c.bin, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), hello.Version) || hello.API != ipc.API {
		t.Errorf("hello = %+v, irie version = %q", hello, out)
	}

	// up is idempotent: the core keeps running.
	pid := c.Tmux(t, "display-message", "-p", "-t", tmux.AgentsSession+":core", "#{pane_pid}")
	c.up()
	if again := c.Tmux(t, "display-message", "-p", "-t", tmux.AgentsSession+":core", "#{pane_pid}"); again != pid {
		t.Errorf("second up replaced the core (pid %s → %s)", pid, again)
	}
}

func TestUpReplacesACoreAndSidebarOfAnotherVersion(t *testing.T) {
	c := setup(t)
	a := c.spawn("keep running")
	format := func(target, f string) string {
		return c.Tmux(t, "display-message", "-p", "-t", target, f)
	}
	corePID := format(tmux.AgentsSession+":core", "#{pane_pid}")
	uiPID := format(tmux.Sidebar, "#{pane_pid}")

	// The same source linked without symbols: another binary, so another version.
	other := filepath.Join(t.TempDir(), "irie")
	if out, err := exec.Command("go", "build", "-ldflags=-s", "-o", other, "github.com/irie-hq/irie/cmd/irie").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	v, err := exec.Command(other, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimPrefix(strings.TrimSpace(string(v)), "irie ")
	t.Setenv("IRIE_BIN", other)
	if out, err := exec.Command(other, "up").CombinedOutput(); err != nil {
		t.Fatalf("irie up: %v\n%s", err, out)
	}

	var hello ipc.HelloResult
	if err := c.call("hello", ipc.HelloArgs{Client: "test", API: ipc.API}, &hello); err != nil {
		t.Fatal(err)
	}
	if hello.Version != version {
		t.Errorf("core version %q, want %q", hello.Version, version)
	}
	if got := format(tmux.AgentsSession+":core", "#{pane_pid}"); got == corePID {
		t.Error("the core pane was not respawned")
	}
	if pid, v := format(tmux.Sidebar, "#{pane_pid}"), format(tmux.Sidebar, "#{@irie_version}"); pid == uiPID || v != version {
		t.Errorf("sidebar pid %s (was %s) at version %q, want a new pane at %q", pid, uiPID, v, version)
	}
	if v := format(tmux.Slot, "#{@irie_role} #{@irie_version}"); v != tmux.RolePlaceholder+" "+version {
		t.Errorf("placeholder = %q, want one at version %s", v, version)
	}
	if p := c.pane(a.Pane); p == nil || p["@irie_session"] != a.ID {
		t.Errorf("the agent did not survive the upgrade: %v", p)
	}
	if ls := c.list(); len(ls) != 1 || ls[0].ID != a.ID || ls[0].State != ipc.StateWorking {
		t.Errorf("list after the upgrade = %+v", ls)
	}
}

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestSpawnCreatesAnAgentWindow(t *testing.T) {
	c := setup(t)
	const task = "say hello\nand then wait"
	s := c.spawn(task)
	if !regexp.MustCompile(`^s[0-9a-hjkmnp-tv-z]{12}$`).MatchString(s.ID) {
		t.Errorf("session id %q", s.ID)
	}
	if s.Name != "say-hello" {
		t.Errorf("name = %q, want say-hello", s.Name)
	}

	var p map[string]string
	for _, row := range c.panes() {
		if row["@irie_session"] == s.ID {
			p = row
		}
	}
	if p == nil {
		t.Fatal("no pane carries @irie_session")
	}
	if p["session_name"] != tmux.AgentsSession || p["window_panes"] != "1" || p["@irie_role"] != tmux.RoleAgent || p["@irie_agent"] != "main" || p["@irie_name"] != "say-hello" {
		t.Errorf("agent pane = %v", p)
	}
	if !uuidV4.MatchString(p["@irie_vendor_id"]) {
		t.Errorf("@irie_vendor_id = %q", p["@irie_vendor_id"])
	}
	if _, err := time.Parse(time.RFC3339Nano, p["@irie_created"]); err != nil {
		t.Errorf("@irie_created = %q: %v", p["@irie_created"], err)
	}

	dir := c.paths.Session(s.ID)
	if b, err := os.ReadFile(filepath.Join(dir, "task.md")); err != nil || string(b) != task {
		t.Errorf("task.md = %q, %v", b, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec host.Spec
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(spec.Argv[:3], []string{"/bin/sh", "-c", "echo ready; sleep 600"}) || spec.Argv[len(spec.Argv)-1] != task {
		t.Errorf("spec argv = %q", spec.Argv)
	}
	// No @ token: a new scratch dir under the data dir.
	if !strings.HasPrefix(spec.Cwd, filepath.Join(c.paths.Scratch(), "say-hello")) {
		t.Errorf("cwd = %q, want a scratch dir", spec.Cwd)
	}

	testutil.Eventually(t, 5*time.Second, "agent did not print ready", func() bool {
		out, _ := c.TryTmux("capture-pane", "-p", "-t", p["pane_id"])
		return strings.Contains(out, "ready")
	})
	if ls := c.list(); len(ls) != 1 || ls[0].ID != s.ID || ls[0].State != ipc.StateWorking {
		t.Errorf("list = %+v", ls)
	}

	// Same first words again: a numbered slug.
	if again := c.spawn("say hello"); again.Name != "say-hello-2" {
		t.Errorf("second name = %q, want say-hello-2", again.Name)
	}
}

func TestSpawnInPlace(t *testing.T) {
	c := setup(t)
	dir := t.TempDir()
	var r ipc.SpawnResult
	if err := c.call("spawn", ipc.SpawnArgs{Task: "look around", Cwd: dir}, &r); err != nil {
		t.Fatal(err)
	}
	if r.Session.Cwd != dir {
		t.Errorf("cwd = %q, want %q", r.Session.Cwd, dir)
	}
	err := c.call("spawn", ipc.SpawnArgs{Task: "x", Cwd: filepath.Join(dir, "missing")}, nil)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("spawn in a missing dir: %v", err)
	}
	if err := c.call("spawn", ipc.SpawnArgs{Task: "  \n "}, nil); err == nil {
		t.Error("empty task spawned")
	}
}

func TestShowSwapsTheSessionIntoTheSlot(t *testing.T) {
	c := setup(t)
	a, b := c.spawn("task a"), c.spawn("task b")
	paneA, paneB := c.pane(a.Pane), c.pane(b.Pane)
	if paneA == nil || paneB == nil {
		t.Fatalf("panes %q %q not found", a.Pane, b.Pane)
	}
	slot := c.slot()
	for _, p := range []map[string]string{paneA, paneB} {
		if p["pane_width"] != slot["pane_width"] || p["pane_height"] != slot["pane_height"] {
			t.Errorf("agent pane %v is not the slot's size %sx%s", p, slot["pane_width"], slot["pane_height"])
		}
	}

	c.show(a.ID, false)
	if got := c.slot(); got["pane_id"] != a.Pane {
		t.Fatalf("slot holds %v, want %s", got, a.Pane)
	}
	if got := c.Tmux(t, "display-message", "-p", "-t", tmux.MainSession+":0", "#{pane_index}"); got != "0" {
		t.Errorf("show without focus moved focus to pane %s", got)
	}

	c.show(b.ID, true)
	if got := c.slot(); got["pane_id"] != b.Pane {
		t.Fatalf("slot holds %v, want %s", got, b.Pane)
	}
	homeA := c.pane(a.Pane)
	if homeA["session_name"] != tmux.AgentsSession || homeA["window_panes"] != "1" {
		t.Errorf("A did not go home: %v", homeA)
	}
	if homeA["pane_width"] != paneA["pane_width"] || homeA["pane_height"] != paneA["pane_height"] {
		t.Errorf("A resized: %v → %v", paneA, homeA)
	}
	if nowB := c.pane(b.Pane); nowB["pane_width"] != paneB["pane_width"] || nowB["pane_height"] != paneB["pane_height"] {
		t.Errorf("B resized: %v → %v", paneB, nowB)
	}
	if got := c.Tmux(t, "display-message", "-p", "-t", tmux.MainSession+":0", "#{pane_index}"); got != "1" {
		t.Errorf("focused show left focus on pane %s", got)
	}

	var shown []string
	for _, s := range c.list() {
		if s.Shown {
			shown = append(shown, s.ID)
		}
	}
	if !slices.Equal(shown, []string{b.ID}) {
		t.Errorf("shown sessions = %v, want [%s]", shown, b.ID)
	}
	if err := c.call("show", ipc.ShowArgs{Session: "s-nope"}, nil); err == nil {
		t.Error("show of an unknown session succeeded")
	}
}

func TestKilledSlotIsRepairedWithin2s(t *testing.T) {
	c := setup(t)
	placeholderBack := func() bool {
		s := c.slot()
		return s != nil && s["@irie_role"] == tmux.RolePlaceholder
	}
	c.Tmux(t, "kill-pane", "-t", tmux.Slot)
	testutil.Eventually(t, 2*time.Second, "placeholder did not come back", placeholderBack)

	a := c.spawn("task a")
	c.show(a.ID, false)
	c.Tmux(t, "kill-pane", "-t", tmux.Slot)
	testutil.Eventually(t, 2*time.Second, "placeholder did not come back after killing a shown agent", placeholderBack)
}

// supervised returns the pid of the process that the supervisor running in
// target's pane started.
func (c *coreEnv) supervised(target string) int {
	c.t.Helper()
	sup := c.Tmux(c.t, "display-message", "-p", "-t", target, "#{pane_pid}")
	out, err := exec.Command("ps", "-A", "-o", "pid=", "-o", "ppid=").Output()
	if err != nil {
		c.t.Fatal(err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == sup {
			pid, _ := strconv.Atoi(f[0])
			return pid
		}
	}
	return 0
}

func kill(t *testing.T, pid int) {
	t.Helper()
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
	testutil.Eventually(t, 2*time.Second, "killed process still running", func() bool {
		return exec.Command("kill", "-0", strconv.Itoa(pid)).Run() != nil
	})
}

func TestSidebarComesBackWithin2s(t *testing.T) {
	c := setup(t)
	old := c.supervised(tmux.Sidebar)
	if old == 0 {
		t.Fatal("no sidebar process under its supervisor")
	}
	kill(t, old)
	testutil.Eventually(t, 2*time.Second, "the supervisor did not restart the sidebar", func() bool {
		pid := c.supervised(tmux.Sidebar)
		return pid != 0 && pid != old
	})
	testutil.Eventually(t, 2*time.Second, "the new sidebar did not draw", func() bool {
		out, _ := c.TryTmux("capture-pane", "-p", "-t", tmux.Sidebar)
		return strings.Contains(out, "☀ Irie")
	})
}

func TestCoreRestartGivesTheSameRows(t *testing.T) {
	c := setup(t)
	c.spawn("task a")
	b := c.spawn("task b")
	c.show(b.ID, false)
	before := c.list()

	// Kill the core under its supervisor; the supervisor starts a new one.
	corePID := c.supervised(tmux.AgentsSession + ":core")
	if corePID == 0 {
		t.Fatal("core process not found under its supervisor")
	}
	kill(t, corePID)
	var after []ipc.Session
	testutil.Eventually(t, 10*time.Second, "core did not come back", func() bool {
		var r ipc.ListResult
		if err := c.call("list", nil, &r); err != nil {
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
		if x.ID != y.ID || x.Name != y.Name || !x.Created.Equal(y.Created) || x.Cwd != y.Cwd || x.Pane != y.Pane || x.Shown != y.Shown || x.State != y.State {
			t.Errorf("row %d: before %+v, after %+v", i, x, y)
		}
	}
}

// TestTheSidebarSpawnsAndShows drives the real sidebar the way the manual
// check does. A client attached from inside a second private server stands
// in for the terminal.
func TestTheSidebarSpawnsAndShows(t *testing.T) {
	c := setup(t)
	term := c.Socket + "-term"
	termTmux := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("tmux", append([]string{"-L", term}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux -L %s %q: %v\n%s", term, args, err, out)
		}
		return string(out)
	}
	termTmux("-f", "/dev/null", "new-session", "-d", "-s", "t", "-x", "100", "-y", "20", "--", c.bin, "attach", "--nested")
	t.Cleanup(func() { exec.Command("tmux", "-L", term, "kill-server").Run() })
	screen := func() string { return termTmux("capture-pane", "-p", "-t", "t") }
	active := func() string {
		return c.Tmux(t, "display-message", "-p", "-t", tmux.MainSession+":0", "#{pane_index}")
	}

	testutil.Eventually(t, 5*time.Second, "the sidebar and placeholder did not appear", func() bool {
		s := screen()
		return strings.Contains(s, "☀ Irie") && strings.Contains(s, "type a new task") && strings.Contains(s, "›")
	})

	dir := t.TempDir()
	termTmux("send-keys", "-t", "t", "-l", "@"+dir+" say hello")
	termTmux("send-keys", "-t", "t", "Enter")
	var s ipc.Session
	testutil.Eventually(t, 5*time.Second, "typing a task did not spawn it", func() bool {
		if ls := c.list(); len(ls) == 1 {
			s = ls[0]
		}
		return s.ID != ""
	})
	if s.Cwd != dir || s.Name != "say-hello" {
		t.Errorf("spawned %+v, want say-hello in %s", s, dir)
	}
	testutil.Eventually(t, 5*time.Second, "the row did not appear", func() bool {
		return strings.Contains(screen(), "say-hello")
	})

	// Enter on the new row shows it and focuses it.
	termTmux("send-keys", "-t", "t", "Enter")
	testutil.Eventually(t, 5*time.Second, "Enter did not show the session", func() bool {
		slot := c.slot()
		return slot != nil && slot["pane_id"] == s.Pane && active() == "1" && strings.Contains(screen(), "ready")
	})

	// The focus key toggles back to the sidebar.
	termTmux("send-keys", "-t", "t", "C-]")
	testutil.Eventually(t, 2*time.Second, "C-] did not move focus to the sidebar", func() bool {
		return active() == "0"
	})
}

func TestSubscribeSendsSnapshotThenUpdates(t *testing.T) {
	c := setup(t)
	cl, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	frames, err := cl.Subscribe(ipc.SubscribeArgs{})
	if err != nil {
		t.Fatal(err)
	}
	next := func() ipc.Frame {
		t.Helper()
		select {
		case f := <-frames:
			return f
		case <-time.After(5 * time.Second):
			t.Fatal("no frame")
		}
		return ipc.Frame{}
	}
	if f := next(); f.T != ipc.FrameSnapshot || len(f.Sessions) != 0 {
		t.Fatalf("first frame = %+v", f)
	}
	s := c.spawn("watch me")
	for {
		f := next()
		if f.T == ipc.FrameUpsert && f.Session != nil && f.Session.ID == s.ID {
			break
		}
		if f.T == ipc.FrameSnapshot && slices.ContainsFunc(f.Sessions, func(x ipc.Session) bool { return x.ID == s.ID }) {
			break
		}
	}
}
