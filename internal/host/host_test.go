package host

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/testutil"
	"github.com/irie-hq/irie/internal/tmux"
)

func TestMain(m *testing.M) { testutil.Main(m) }

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
	testutil.Eventually(h.t, 5*time.Second, "agent process did not start", func() bool {
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
	testutil.Eventually(t, 5*time.Second, "pane_current_command is not the agent", func() bool {
		return h.format("#{pane_current_command}") == "sleep"
	})
}

func TestExitCodeIsRecorded(t *testing.T) {
	h := startHost(t, "/bin/sh", "-c", "exit 3")
	testutil.Eventually(t, 5*time.Second, "pane did not die", func() bool {
		return h.format("#{pane_dead}") == "1"
	})
	if got := h.format("#{pane_dead_status}"); got != "3" {
		t.Errorf("pane_dead_status = %s, want 3", got)
	}
	if x := readExit(t, h.dir); x.Code != 3 || x.Signal != 0 {
		t.Errorf("exit.json = %+v, want code 3", x)
	}
}

func TestMissingCommandExits127(t *testing.T) {
	h := startHost(t, "irie-test-no-such-command")
	testutil.Eventually(t, 5*time.Second, "pane did not die", func() bool {
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
	testutil.Eventually(t, 5*time.Second, "agent output did not appear", func() bool {
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
	testutil.Eventually(t, time.Second, "stopped agent was not continued", func() bool {
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
	testutil.Eventually(t, 5*time.Second, "the agent did not print its env", func() bool {
		return strings.Contains(h.Tmux(t, "capture-pane", "-p", "-t", "h:0.0"), "unset|unset|1")
	})
	// Without Unset the pane's own tmux variables reach the agent.
	h = startHostSpec(t, Spec{Argv: []string{"/bin/sh", "-c", script}})
	testutil.Eventually(t, 5*time.Second, "the agent did not print its env", func() bool {
		screen := h.Tmux(t, "capture-pane", "-p", "-t", "h:0.0")
		return strings.Contains(screen, "|1") && !strings.Contains(screen, "unset")
	})
}
