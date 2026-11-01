package testutil

// A test run that is killed, or that times out, never runs its cleanups:
// Go runs no t.Cleanup on SIGKILL, on SIGINT, or after -timeout panics. A
// test's tmux server is a daemon of its own, so it would run on with
// everything in it, the core, the sidebar, agents and their hosts, for
// days. Three things keep a run from leaving any of it behind:
//
//   - A test's cleanup ends every tmux server in its dirs, and then kills
//     what is left of their process trees: a process that ignores the
//     SIGHUP the end of its pane brings, one that is stopped, or one in a
//     session of its own.
//   - A reaper, the test binary started again and detached from it, holds
//     a pipe from the binary and is told each dir the binary makes. When
//     the binary ends, however it ends, the pipe closes, and the reaper
//     cleans every dir whose cleanup did not run.
//   - Each such dir names the process that made it in an owner file, and
//     Main first cleans the dirs of earlier runs whose owner is gone, for a
//     run whose reaper was killed too.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/proc"
)

// reaperEnv starts the test binary as the reaper rather than its tests:
// "fork" starts the reaper and exits, "run" is the reaper.
const reaperEnv = "IRIE_TEST_REAPER"

// ownerFile, in a dir a test binary made, names that binary: "pid start".
const ownerFile = ".irie-test-owner"

// stopGrace is how long an ended tmux server and its panes' first
// processes get to exit before they are killed, and restGrace how much
// longer, at most, the rest of their trees get after them: what has not
// exited with its pane by then waits on something that will not come, such
// as a hook stopped by a freeze.
var stopGrace, restGrace = 5 * time.Second, time.Second

var (
	// mainRan says Main runs this binary's tests, so the binary may be
	// started again as the reaper.
	mainRan bool

	reaper struct {
		once sync.Once
		mu   sync.Mutex
		w    *os.File // the pipe to the reaper; nil without one
	}

	selfOnce  sync.Once
	selfStart int64 // when this process started, by the process table
)

// asReaper runs the reaper's part when this binary was started as one, and
// reports whether it was, with the code to exit with.
func asReaper() (int, bool) {
	switch os.Getenv(reaperEnv) {
	case "fork":
		return forkReaper(), true
	case "run":
		return reap(os.Stdin), true
	}
	return 0, false
}

// own marks dir as this binary's: the owner file names it, and the reaper
// cleans dir if the binary ends before release.
func own(dir string) {
	if start := self(); start != 0 {
		os.WriteFile(filepath.Join(dir, ownerFile), fmt.Appendf(nil, "%d %d\n", os.Getpid(), start), 0o600)
	}
	tell("+" + dir)
}

// release tells the reaper that dir is clean.
func release(dir string) { tell("-" + dir) }

// self returns when this process started, or 0 when the table cannot say.
func self() int64 {
	selfOnce.Do(func() {
		t, err := proc.Read()
		if err != nil {
			return
		}
		if p, ok := t.Get(os.Getpid()); ok {
			selfStart = p.Start
		}
	})
	return selfStart
}

// tell sends the reaper one line, starting it first. Without Main there
// is none: started again, such a binary would run its tests.
func tell(line string) {
	if !mainRan {
		return
	}
	reaper.once.Do(startReaper)
	reaper.mu.Lock()
	defer reaper.mu.Unlock()
	if reaper.w != nil {
		fmt.Fprintln(reaper.w, line)
	}
}

// startReaper starts the reaper, detached: the binary starts itself, in a
// session of its own, which starts the reaper and exits. The reaper so
// belongs to no process group and no process tree of the run's, and
// whatever kills the run does not reach it. It reads the pipe whose write
// end only this process holds, and holds none of the run's output, so go
// test never waits for it.
func startReaper() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	r, w, err := os.Pipe()
	if err != nil {
		return
	}
	defer r.Close()
	cmd := &exec.Cmd{
		Path:        exe,
		Args:        []string{"irie-test-reaper"},
		Env:         reaperEnviron("fork"),
		Dir:         "/",
		Stdin:       r,
		SysProcAttr: &syscall.SysProcAttr{Setsid: true},
	}
	if err := cmd.Run(); err != nil {
		w.Close()
		return
	}
	reaper.w = w
}

// forkReaper is the middle process: it starts the reaper on the same pipe
// and exits without waiting, so the reaper's parent is init.
func forkReaper() int {
	exe, err := os.Executable()
	if err != nil {
		return 1
	}
	cmd := &exec.Cmd{Path: exe, Args: []string{"irie-test-reaper"}, Env: reaperEnviron("run"), Dir: "/", Stdin: os.Stdin}
	if err := cmd.Start(); err != nil {
		return 1
	}
	return 0
}

// reaperEnviron is this process's environment for the reaper: mode in
// reaperEnv, and no tmux of the caller's.
func reaperEnviron(mode string) []string {
	return append(ownEnviron(), reaperEnv+"="+mode)
}

// ownEnviron is this process's environment without reaperEnv and without
// the tmux a caller may run in.
func ownEnviron() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k != reaperEnv && k != "TMUX" && k != "TMUX_PANE" {
			env = append(env, kv)
		}
	}
	return env
}

// reap is the reaper: it keeps the dirs it is told of, "+dir" and "-dir",
// until in ends, which is when the test binary ended, and then cleans each
// dir still kept.
func reap(in io.Reader) int {
	dirs := map[string]bool{}
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "+"):
			dirs[line[1:]] = true
		case strings.HasPrefix(line, "-"):
			delete(dirs, line[1:])
		}
	}
	for d := range dirs {
		cleanDir(d)
	}
	return 0
}

// sweep cleans the dirs matching patterns whose owner file names a process
// that is gone: dirs of a run that died with its reaper. A dir without an
// owner file is left alone, as is one whose owner still runs.
func sweep(patterns ...string) {
	t, err := proc.Read()
	if err != nil {
		return
	}
	for _, pat := range patterns {
		dirs, _ := filepath.Glob(pat)
		for _, d := range dirs {
			pid, start, ok := readOwner(d)
			if ok && !t.Alive(proc.Proc{PID: pid, Start: start}) {
				cleanDir(d)
			}
		}
	}
}

func readOwner(dir string) (pid int, start int64, ok bool) {
	b, err := os.ReadFile(filepath.Join(dir, ownerFile))
	if err != nil {
		return 0, 0, false
	}
	f := strings.Fields(string(b))
	if len(f) != 2 {
		return 0, 0, false
	}
	pid, err1 := strconv.Atoi(f[0])
	start, err2 := strconv.ParseInt(f[1], 10, 64)
	return pid, start, err1 == nil && err2 == nil && pid > 0
}

// cleanDir ends the tmux servers in dir and everything they run, then
// removes dir.
func cleanDir(dir string) {
	stopServers(tmuxSockets(dir))
	os.RemoveAll(dir)
}

// tmuxSockets returns the sockets of the tmux servers under dir, a
// TMUX_TMPDIR or a dir that holds one: tmux keeps its sockets in
// tmux-<uid> under TMUX_TMPDIR. The main server and the team server of a
// test's Irie (DESIGN §2.3) both live there.
func tmuxSockets(dir string) []string {
	sub := fmt.Sprintf("tmux-%d", os.Getuid())
	var out []string
	for _, pat := range []string{filepath.Join(dir, sub, "*"), filepath.Join(dir, "*", sub, "*")} {
		m, _ := filepath.Glob(pat)
		for _, p := range m {
			if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSocket != 0 {
				out = append(out, p)
			}
		}
	}
	return out
}

// stopServers ends the tmux servers at socks and everything they run. It
// reads their process trees first, while every process still hangs under
// its server, then kills each server, waits for the servers and their
// panes' first processes (stopGrace), then for the rest (restGrace), and
// kills whatever of the trees still runs.
func stopServers(socks []string) {
	if len(socks) == 0 {
		return
	}
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return
	}
	var pids []int
	for _, s := range socks {
		if pid := serverPID(bin, s); pid > 0 {
			pids = append(pids, pid)
		}
	}
	var first, rest []proc.Proc
	if t, err := proc.Read(); err == nil {
		for _, pid := range pids {
			if p, ok := t.Get(pid); ok {
				first = append(first, p)
			}
			for _, p := range t.Tree(pid) {
				if p.PPID == pid {
					first = append(first, p)
				} else {
					rest = append(rest, p)
				}
			}
		}
	}
	for _, s := range socks {
		tmuxAt(bin, s, "kill-server")
	}
	deadline := time.Now().Add(stopGrace)
	waitGone(first, deadline)
	if d := time.Now().Add(restGrace); d.Before(deadline) {
		deadline = d
	}
	waitGone(rest, deadline)
	for _, p := range alive(append(first, rest...)) {
		syscall.Kill(p.PID, syscall.SIGKILL)
	}
}

// waitGone waits until no process of ps runs, or until deadline.
func waitGone(ps []proc.Proc, deadline time.Time) {
	for len(alive(ps)) > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}

// serverPID asks the tmux server at sock for its pid; 0 when none answers.
func serverPID(bin, sock string) int {
	out, err := tmuxAt(bin, sock, "display-message", "-p", "#{pid}")
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return pid
}

// tmuxAt runs tmux against the server at sock, for at most 3 s: a server
// that hangs never holds up a cleanup.
func tmuxAt(bin, sock string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append([]string{"-S", sock}, args...)...)
	cmd.Env = ownEnviron()
	return cmd.Output()
}

// alive returns the processes of ps that still run.
func alive(ps []proc.Proc) []proc.Proc {
	t, err := proc.Read()
	if err != nil {
		return nil
	}
	var out []proc.Proc
	for _, p := range ps {
		if t.Alive(p) {
			out = append(out, p)
		}
	}
	return out
}
