package host

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/sysload"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/tmux"
)

func TestMain(m *testing.M) { testutil.Main(m) }

// bound is how long a wait may take: d, or twelve times d while the
// scheduler rather than the host sets the time, when the test runs in
// macOS's background state or the machine has more work waiting than CPUs
// to run it, as during a busy install (tools/live.sh). It is internal/core's
// slow (freeze_test.go) as a wait: each wait still tells a host that acts
// from one that never does.
func bound(t *testing.T, d time.Duration) time.Duration {
	t.Helper()
	if runtime.GOOS == "darwin" {
		out, _ := exec.Command("ps", "-o", "pri=", "-p", strconv.Itoa(os.Getpid())).Output()
		if strings.TrimSpace(string(out)) == "4" {
			t.Logf("waiting up to %s: the test runs in macOS's background state", 12*d)
			return 12 * d
		}
	}
	if l, err := sysload.Read(); err == nil && l.RunQueue > float64(l.CPUs) {
		t.Logf("waiting up to %s: %.1f CPUs' worth of work waiting on %d CPUs", 12*d, l.RunQueue, l.CPUs)
		return 12 * d
	}
	return d
}

type hostEnv struct {
	*testutil.Env
	t   *testing.T
	bin string
	dir string // session dir holding spec.json and exit.json
}

// startHost runs `irie host --spec` as the first process of a pane in a private
// tmux server that uses Irie's generated tmux.conf.
func startHost(t *testing.T, argv ...string) *hostEnv {
	t.Helper()
	return startHostSpec(t, Spec{Argv: argv})
}

// startHostSpec is startHost for a spec of its own. The session, cwd and a
// marker variable are filled in.
func startHostSpec(t *testing.T, spec Spec) *hostEnv {
	t.Helper()
	testutil.NeedTmux(t)
	bin := testutil.BuildIrie(t)
	e := testutil.NewEnv(t)
	h := &hostEnv{Env: e, t: t, bin: bin, dir: filepath.Join(e.StateHome, "irie", "sessions", "s0k3j9x2m4a7q")}
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	spec.Session, spec.Cwd, spec.Env = "s0k3j9x2m4a7q", t.TempDir(), map[string]string{"IRIE_TEST_MARK": "1"}
	if err := WriteSpec(filepath.Join(h.dir, "spec.json"), spec); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(t.TempDir(), "tmux.conf")
	if err := os.WriteFile(conf, []byte(tmux.Conf(tmux.ConfOptions{FocusKey: "C-]", SidebarWidth: 34})), 0o600); err != nil {
		t.Fatal(err)
	}
	e.Tmux(t, "-f", conf, "new-session", "-d", "-s", "h", "-x", "80", "-y", "24", "--", bin, "host", "--spec", filepath.Join(h.dir, "spec.json"))
	return h
}

func (h *hostEnv) format(f string) string {
	h.t.Helper()
	return h.Tmux(h.t, "display-message", "-p", "-t", "h:0.0", f)
}

type proc struct {
	pid, ppid, pgid int
	stat, command   string
}

func processes(t *testing.T) []proc {
	t.Helper()
	out, err := exec.Command("ps", "-A", "-o", "pid=", "-o", "ppid=", "-o", "pgid=", "-o", "stat=", "-o", "command=").Output()
	if err != nil {
		t.Fatal(err)
	}
	var ps []proc
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		pgid, _ := strconv.Atoi(f[2])
		ps = append(ps, proc{pid, ppid, pgid, f[3], strings.Join(f[4:], " ")})
	}
	return ps
}

// child waits for the host's agent process, a child running command, and
// returns it. Matching the command matters: on macOS a short-lived "(tmux)"
// process can show up as a child of the pane's first process before the
// agent starts.
func (h *hostEnv) child(command string) (host int, c proc) {
	h.t.Helper()
	host, _ = strconv.Atoi(h.format("#{pane_pid}"))
	testutil.Eventually(h.t, bound(h.t, 5*time.Second), "agent process did not start", func() bool {
		for _, p := range processes(h.t) {
			if p.ppid == host && p.command == command {
				c = p
				return true
			}
		}
		return false
	})
	return host, c
}

func readExit(t *testing.T, dir string) Exit {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "exit.json"))
	if err != nil {
		t.Fatal(err)
	}
	var x Exit
	if err := json.Unmarshal(b, &x); err != nil {
		t.Fatal(err)
	}
	return x
}

func TestAgentRunsInItsOwnForegroundGroup(t *testing.T) {
	h := startHost(t, "sleep", "600")
	host, c := h.child("sleep 600")
	if c.pgid == host {
		t.Errorf("agent pgid %d equals the host pid", c.pgid)
	}
	if c.pgid != c.pid {
		t.Errorf("agent pgid %d, want its own pid %d", c.pgid, c.pid)
	}
	testutil.Eventually(t, bound(t, 5*time.Second), "pane_current_command is not the agent", func() bool {
		return h.format("#{pane_current_command}") == "sleep"
	})
}

func TestExitCodeIsRecorded(t *testing.T) {
	h := startHost(t, "/bin/sh", "-c", "exit 3")
	testutil.Eventually(t, bound(t, 5*time.Second), "pane did not die", func() bool {
		return h.format("#{pane_dead}") == "1"
	})
	if got := h.format("#{pane_dead_status}"); got != "3" {
		t.Errorf("pane_dead_status = %s, want 3", got)
	}
	// It names its host, which a pane that lives on would still have.
	pid, _ := strconv.Atoi(h.format("#{pane_pid}"))
	if x := readExit(t, h.dir); x.Code != 3 || x.Signal != 0 || x.Host != pid {
		t.Errorf("exit.json = %+v, want code 3 from host %d", x, pid)
	}
}

func TestMissingCommandExits127(t *testing.T) {
	h := startHost(t, "irie-test-no-such-command")
	testutil.Eventually(t, bound(t, 5*time.Second), "pane did not die", func() bool {
		return h.format("#{pane_dead}") == "1"
	})
	if got := h.format("#{pane_dead_status}"); got != "127" {
		t.Errorf("pane_dead_status = %s, want 127", got)
	}
	if x := readExit(t, h.dir); x.Code != 127 {
		t.Errorf("exit.json = %+v, want code 127", x)
	}
	// tmux scrolls the screen up a line to say the pane is dead, so the
	// message is read from the history too.
	if screen := h.Tmux(t, "capture-pane", "-p", "-S", "-", "-t", "h:0.0"); !strings.Contains(screen, "irie-test-no-such-command") {
		t.Errorf("screen does not name the command:\n%s", screen)
	}
}

func TestSpecEnvAndCwdReachTheAgent(t *testing.T) {
	h := startHost(t, "/bin/sh", "-c", `printf '%s|%s\n' "$IRIE_TEST_MARK" "$PWD"; sleep 600`)
	b, err := os.ReadFile(filepath.Join(h.dir, "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec Spec
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(spec.Cwd)
	testutil.Eventually(t, bound(t, 5*time.Second), "agent output did not appear", func() bool {
		screen := h.Tmux(t, "capture-pane", "-p", "-J", "-t", "h:0.0")
		got, ok := strings.CutPrefix(strings.TrimSpace(screen), "1|")
		if !ok {
			return false
		}
		got, _ = filepath.EvalSymlinks(strings.TrimSpace(got))
		return got == want
	})
}

func TestStopsTheHostDidNotCauseAreContinued(t *testing.T) {
	h := startHost(t, "sleep", "600")
	_, c := h.child("sleep 600")
	if out, err := exec.Command("kill", "-STOP", strconv.Itoa(c.pid)).CombinedOutput(); err != nil {
		t.Fatalf("kill -STOP %d: %v: %s", c.pid, err, out)
	}
	// Without the host the agent would still be stopped after this pause.
	time.Sleep(300 * time.Millisecond)
	testutil.Eventually(t, bound(t, time.Second), "stopped agent was not continued", func() bool {
		for _, p := range processes(t) {
			if p.pid == c.pid {
				return !strings.HasPrefix(p.stat, "T")
			}
		}
		return false
	})
}

func TestTheHostSaysWhoItIsInHostJSON(t *testing.T) {
	h := startHost(t, "sleep", "600")
	host, c := h.child("sleep 600")
	var info Info
	testutil.Eventually(t, bound(t, 5*time.Second), "no host.json", func() bool {
		var err error
		info, err = ReadInfo(filepath.Join(h.dir, InfoFile))
		return err == nil
	})
	if info != (Info{PID: host, Agent: c.pid, Freeze: 1}) {
		t.Errorf("host.json = %+v, want pid %d, agent %d, freeze 1", info, host, c.pid)
	}
}

func TestAFrozenAgentStaysStopped(t *testing.T) {
	// The core stops a frozen session's agent itself; the host must not
	// continue it, as it continues stops it did not cause.
	h := startHost(t, "sleep", "600")
	_, c := h.child("sleep 600")
	freeze := filepath.Join(h.dir, FreezeFile)
	if err := os.WriteFile(freeze, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !Frozen(h.dir) {
		t.Fatal("Frozen does not see freeze.json")
	}
	if err := syscall.Kill(-c.pid, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	for _, p := range processes(t) {
		if p.pid == c.pid && !strings.HasPrefix(p.stat, "T") {
			t.Fatalf("the frozen agent runs again: %s", p.stat)
		}
	}
	// Thawed, the agent runs, and a later stop is someone else's again.
	syscall.Kill(-c.pid, syscall.SIGCONT)
	os.Remove(freeze)
	if err := syscall.Kill(c.pid, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, bound(t, 2*time.Second), "a stop after the thaw was not continued", func() bool {
		for _, p := range processes(t) {
			if p.pid == c.pid {
				return !strings.HasPrefix(p.stat, "T")
			}
		}
		return false
	})
}

func TestUnsetVariablesNeverReachTheAgent(t *testing.T) {
	script := `printf '%s|%s|%s\n' "${TMUX-unset}" "${TMUX_PANE-unset}" "$IRIE_TEST_MARK"; sleep 600`
	h := startHostSpec(t, Spec{Argv: []string{"/bin/sh", "-c", script}, Unset: []string{"TMUX", "TMUX_PANE"}})
	testutil.Eventually(t, bound(t, 5*time.Second), "the agent did not print its env", func() bool {
		return strings.Contains(h.Tmux(t, "capture-pane", "-p", "-t", "h:0.0"), "unset|unset|1")
	})
	// Without Unset the pane's own tmux variables reach the agent.
	h = startHostSpec(t, Spec{Argv: []string{"/bin/sh", "-c", script}})
	testutil.Eventually(t, bound(t, 5*time.Second), "the agent did not print its env", func() bool {
		screen := h.Tmux(t, "capture-pane", "-p", "-t", "h:0.0")
		return strings.Contains(screen, "|1") && !strings.Contains(screen, "unset")
	})
}

func TestSIGTERMEndsAStoppedAgent(t *testing.T) {
	// The agent's own child catches SIGTERM, as Claude Code does, and is
	// stopped, as in a frozen session, where the host's wait does not see it.
	// A caught signal waits until the process runs again, so the host
	// continues the group after passing SIGTERM on.
	inner := filepath.Join(t.TempDir(), "inner.sh")
	if err := os.WriteFile(inner, []byte("trap 'exit 0' TERM\nwhile :; do sleep 1; done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := startHost(t, "/bin/sh", "-c", `/bin/sh "$0" & trap "" TERM; read line`, inner)
	host, sh := h.child(`/bin/sh -c /bin/sh "$0" & trap "" TERM; read line ` + inner)
	var child proc
	testutil.Eventually(t, bound(t, 5*time.Second), "the agent's child did not start", func() bool {
		for _, p := range processes(t) {
			if p.ppid == sh.pid && p.command == "/bin/sh "+inner {
				child = p
				return true
			}
		}
		return false
	})
	state := func() string {
		for _, p := range processes(t) {
			if p.pid == child.pid {
				return p.stat
			}
		}
		return "gone"
	}
	if out, err := exec.Command("kill", "-STOP", strconv.Itoa(child.pid)).CombinedOutput(); err != nil {
		t.Fatalf("kill -STOP %d: %v: %s", child.pid, err, out)
	}
	testutil.Eventually(t, bound(t, time.Second), "the child did not stop", func() bool { return strings.HasPrefix(state(), "T") })
	if out, err := exec.Command("kill", "-TERM", strconv.Itoa(host)).CombinedOutput(); err != nil {
		t.Fatalf("kill -TERM %d: %v: %s", host, err, out)
	}
	testutil.Eventually(t, bound(t, 5*time.Second), "the stopped child never ran its SIGTERM trap", func() bool {
		s := state()
		return s == "gone" || strings.HasPrefix(s, "Z")
	})
}

// startShellHost runs script as the agent of a spec that names a shell: a
// plain /bin/sh standing in for the user's login shell, which would read
// their own profile. The spec's Env is the agent's alone, and ShellEnv the
// shell's.
func startShellHost(t *testing.T, script string) *hostEnv {
	t.Helper()
	return startHostSpec(t, Spec{
		Argv: []string{"/bin/sh", "-c", script}, Unset: []string{"TMUX", "TMUX_PANE"},
		Shell: []string{"/bin/sh"}, ShellEnv: map[string]string{"IRIE_TEST_SHELL": "yes"},
	})
}

// screen is what the pane shows, without colours.
func (h *hostEnv) screen() string {
	h.t.Helper()
	return h.Tmux(h.t, "capture-pane", "-p", "-J", "-t", "h:0.0")
}

// shellUp waits for the agent's exit and for the shell's prompt under the
// line the host writes, and returns the exit.
func (h *hostEnv) shellUp() Exit {
	h.t.Helper()
	testutil.Eventually(h.t, bound(h.t, 5*time.Second), "the agent's exit was not recorded", func() bool {
		_, err := os.Stat(filepath.Join(h.dir, ExitFile))
		return err == nil
	})
	testutil.Eventually(h.t, bound(h.t, 5*time.Second), "no prompt under the host's line", func() bool {
		_, after, ok := strings.Cut(h.screen(), ShellNoteEnd)
		return ok && strings.TrimSpace(after) != ""
	})
	return readExit(h.t, h.dir)
}

// typed runs line in the pane's shell and returns what it wrote to file,
// in the shell's folder.
func (h *hostEnv) typed(line, file string) string {
	h.t.Helper()
	h.Tmux(h.t, "send-keys", "-t", "h:0.0", line, "Enter")
	var b []byte
	testutil.Eventually(h.t, bound(h.t, 5*time.Second), "the shell did not run "+line, func() bool {
		var err error
		b, err = os.ReadFile(file)
		return err == nil && strings.HasSuffix(string(b), "\n")
	})
	return strings.TrimSpace(string(b))
}

func TestAnAgentThatExitsByItselfLeavesTheShellInItsPane(t *testing.T) {
	h := startShellHost(t, "printf 'the agent says goodbye\\n'; exit 3")
	x := h.shellUp()
	pid, _ := strconv.Atoi(h.format("#{pane_pid}"))
	if x.Code != 3 || x.Host != pid {
		t.Fatalf("exit.json = %+v, want code 3 from the host, pid %d", x, pid)
	}
	if h.format("#{pane_dead}") != "0" {
		t.Fatal("the pane died with its agent")
	}
	// host.json names the shell, a child of the host leading its own group.
	var info Info
	testutil.Eventually(t, bound(t, 5*time.Second), "host.json names no shell", func() bool {
		var err error
		info, err = ReadInfo(filepath.Join(h.dir, InfoFile))
		return err == nil && info.Shell > 0
	})
	if !slices.ContainsFunc(processes(t), func(p proc) bool { return p.pid == info.Shell && p.ppid == pid && p.pgid == p.pid }) {
		t.Errorf("host.json's shell %d is not the host's child leading its own group", info.Shell)
	}
	// The agent's last screen stays, and the host's line comes under it.
	screen := h.screen()
	bye, note := strings.Index(screen, "the agent says goodbye"), strings.Index(screen, "── sh exited with code 3"+ShellNoteEnd)
	if bye < 0 || note < bye {
		t.Fatalf("want the agent's words, then the host's line, on the screen:\n%s", screen)
	}

	// It is a shell in the agent's folder, with the pane's environment and
	// ShellEnv, but not the agent's own variables or the unset ones.
	var spec Spec
	b, _ := os.ReadFile(filepath.Join(h.dir, "spec.json"))
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	got := h.typed(`printf '%s|%s|%s|%s\n' "$PWD" "${IRIE_TEST_SHELL-unset}" "${IRIE_TEST_MARK-unset}" "${TMUX-unset}" > seen.txt`, filepath.Join(spec.Cwd, "seen.txt"))
	where, rest, _ := strings.Cut(got, "|")
	want, _ := filepath.EvalSymlinks(spec.Cwd)
	if w, _ := filepath.EvalSymlinks(where); w != want || rest != "yes|unset|unset" {
		t.Fatalf("the shell saw %q, want %s|yes|unset|unset", got, want)
	}

	// Once the shell exits, the pane dies with the agent's code.
	h.Tmux(t, "send-keys", "-t", "h:0.0", "exit 7", "Enter")
	testutil.Eventually(t, bound(t, 5*time.Second), "the pane did not die with its shell", func() bool {
		return h.format("#{pane_dead}") == "1"
	})
	if got := h.format("#{pane_dead_status}"); got != "3" {
		t.Errorf("pane_dead_status = %s, want the agent's 3", got)
	}
}

func TestASignalThatEndsTheAgentStartsNoShell(t *testing.T) {
	// A close, a rest and a respawn end the agent through its host.
	h := startHostSpec(t, Spec{Argv: []string{"sleep", "600"}, Shell: []string{"/bin/sh"}})
	host, _ := h.child("sleep 600")
	if err := syscall.Kill(host, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, bound(t, 5*time.Second), "the pane did not die", func() bool {
		return h.format("#{pane_dead}") == "1"
	})
	if x := readExit(t, h.dir); x.Code != 128+int(syscall.SIGTERM) || x.Host != host {
		t.Errorf("exit.json = %+v, want SIGTERM's code from host %d", x, host)
	}
	if strings.Contains(h.screen(), ShellNoteEnd) {
		t.Errorf("a shell was started:\n%s", h.screen())
	}
}

func TestSIGTERMToTheHostEndsItsShell(t *testing.T) {
	// An interactive shell ignores SIGTERM, which a close sends the host:
	// the host hangs it up instead, as a closing terminal does.
	h := startShellHost(t, "exit 0")
	h.shellUp()
	host, _ := strconv.Atoi(h.format("#{pane_pid}"))
	if err := syscall.Kill(host, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, bound(t, 3*time.Second), "the shell outlived SIGTERM to its host", func() bool {
		return h.format("#{pane_dead}") == "1"
	})
	if got := h.format("#{pane_dead_status}"); got != "0" {
		t.Errorf("pane_dead_status = %s, want the agent's 0", got)
	}
}

func TestTheShellGetsTheTerminalAsItWasBeforeTheAgent(t *testing.T) {
	// An agent killed mid-screen leaves its terminal raw, the mouse on and
	// the alternate screen up, with the cursor hidden.
	h := startShellHost(t, `stty -echo -icanon; printf '\033[?1000h\033[?1049h\033[?25l'; exit 0`)
	h.shellUp()
	if got := h.format("#{mouse_any_flag} #{alternate_on} #{cursor_flag}"); got != "0 0 1" {
		t.Errorf("mouse, alternate screen and cursor = %q, want 0 0 1", got)
	}
	var spec Spec
	b, _ := os.ReadFile(filepath.Join(h.dir, "spec.json"))
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	modes := strings.Fields(h.typed("stty -a > modes.txt", filepath.Join(spec.Cwd, "modes.txt")))
	for _, m := range []string{"echo", "icanon"} {
		if !slices.Contains(modes, m) || slices.Contains(modes, "-"+m) {
			t.Errorf("the shell runs its programs without %s: %v", m, modes)
		}
	}
}

func TestAShellWhoseFolderHasGoneStartsAtHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := startShellHost(t, `gone=$PWD; cd / && rmdir "$gone" && exit 0`)
	h.shellUp()
	got := h.typed("pwd > where.txt", filepath.Join(home, "where.txt"))
	want, _ := filepath.EvalSymlinks(home)
	if g, _ := filepath.EvalSymlinks(got); g != want {
		t.Fatalf("the shell started in %q, want home, %q", got, want)
	}
}

func TestTheHostsLineSaysWhatEnded(t *testing.T) {
	for _, c := range []struct {
		x    Exit
		want string
	}{
		{Exit{Code: 0}, "claude exited"},
		{Exit{Code: 3}, "claude exited with code 3"},
		{Exit{Code: 137, Signal: 9}, "claude was ended by SIGKILL"},
	} {
		if got := shellNote("/home/alice/.local/bin/claude", c.x); got != "\x1b[2m── "+c.want+ShellNoteEnd+"\x1b[0m" {
			t.Errorf("shellNote(%+v) = %q, want %q", c.x, got, c.want)
		}
	}
}
