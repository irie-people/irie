package core

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/raphael"
	"github.com/irie-people/irie/internal/tmux"
)

func TestLeftSessionsListTheOpenThenTheClosedWithTheirFolders(t *testing.T) {
	c := bareCore(t, config.Default())
	now := time.Now()
	x := &session{id: "s1", name: "fix-login", vendorID: "conv-now", worktree: "/w/fix-login", cwd: "/w/fix-login", created: now.Add(-5 * time.Hour)}
	x.track.heard, x.track.activity = now.Add(-3*time.Hour), done
	x.track.own = []string{"/p/conv-before.jsonl", "/p/conv-now.jsonl"}
	busy := &session{id: "s2", name: "busy", created: now}
	busy.track.activity = working
	c.sessions = map[string]*session{"s1": x, "s2": busy}
	if err := host.WriteSpec(c.paths.SpecFile("s3"), host.Spec{Session: "s3", Argv: []string{"claude"}, Cwd: "/w/clean-up", Worktree: "/w/clean-up"}); err != nil {
		t.Fatal(err)
	}
	c.closes.list = []closedRecord{{ID: "s3", Name: "can-we-clean-up-the", VendorID: "conv-old", Closed: now.Add(-time.Hour)}, {ID: "s4", Name: "no-spec"}}
	ss := c.leftSessions()
	if len(ss) != 4 || ss[0].ID != "s1" || ss[1].ID != "s2" || ss[2].ID != "s3" || ss[3].ID != "s4" {
		t.Fatalf("sessions = %+v; want the open, then the closed, the last closed first", ss)
	}
	if s := ss[0]; !s.Open || s.Working || !slices.Equal(s.Conversations, []string{"conv-now", "conv-before"}) || !s.Quiet.Equal(x.track.heard) || s.Worktree != "/w/fix-login" {
		t.Errorf("the open one: %+v", s)
	}
	if !ss[1].Working {
		t.Errorf("one in a turn: %+v", ss[1])
	}
	if s := ss[2]; s.Open || s.Worktree != "/w/clean-up" || !slices.Equal(s.Conversations, []string{"conv-old"}) || !s.Closed.Equal(now.Add(-time.Hour)) {
		t.Errorf("the closed one: %+v", s)
	}
	if s := ss[3]; s.Worktree != "" || len(s.Conversations) != 0 {
		t.Errorf("one with no spec: %+v", s)
	}
}

// countRunner runs the tools for real, and keeps which pids each lsof
// read.
type countRunner struct{ lsofs [][]string }

func (r *countRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	if i := slices.Index(args, "-p"); filepath.Base(name) == "lsof" && i >= 0 && i+1 < len(args) {
		r.lsofs = append(r.lsofs, strings.Split(args[i+1], ","))
	}
	return raphael.Exec{}.Run(ctx, name, args...)
}

// reads is how many lsofs read pid.
func (r *countRunner) reads(pid int) int {
	n := 0
	for _, pids := range r.lsofs {
		if slices.Contains(pids, strconv.Itoa(pid)) {
			n++
		}
	}
	return n
}

// TestRaphaelFindsWhatAClosedSessionLeftRunning leaves a real process
// running in a closed session's worktree, as an agent's shell does when it
// ends: launchd (or init) takes it, and its folder names the session.
func TestRaphaelFindsWhatAClosedSessionLeftRunning(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		if _, err := os.Stat("/usr/bin/lsof"); err != nil {
			t.Skip("no lsof to read a process's folder")
		}
	}
	// lsof names a folder by its real path: on macOS the temporary
	// folder is reached through a link (/var is /private/var).
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := paths.New(func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	c := newCore(Options{Config: config.Default(), Paths: p, Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{Bin: filepath.Join(home, "no-tmux")})
	runner := &countRunner{}
	c.raph.runner = runner
	wt := filepath.Join(c.paths.Worktrees(), "irie", "listener")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := host.WriteSpec(c.paths.SpecFile("s38gone"), host.Spec{Session: "s38gone", Argv: []string{"claude"}, Cwd: wt, Worktree: wt}); err != nil {
		t.Fatal(err)
	}
	c.closes.list = []closedRecord{{ID: "s38gone", Name: "can-we-clean-up-the", Closed: time.Now().Add(-time.Hour)}}
	out, err := exec.Command("/bin/sh", "-c", `cd "$1" && { sleep 300 >/dev/null 2>&1 & echo $!; }`, "sh", wt).Output()
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	for deadline := time.Now().Add(5 * time.Second); ; {
		q, err := proc.One(pid)
		if err != nil {
			t.Fatal(err)
		}
		if q.PPID == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Skipf("an orphan's parent here is pid %d, not launchd or init", q.PPID)
		}
		time.Sleep(20 * time.Millisecond)
	}
	look := func() []raphael.Left {
		ps, err := raphael.Procs(context.Background(), raphael.Exec{})
		if err != nil {
			t.Fatal(err)
		}
		tb, err := proc.Read()
		if err != nil {
			t.Fatal(err)
		}
		// An hour on: what was left is past its grace.
		return c.leftRunning(context.Background(), ps, tb, time.Now().Add(time.Hour))
	}
	found := func(ls []raphael.Left) (raphael.Left, bool) {
		i := slices.IndexFunc(ls, func(l raphael.Left) bool { return l.Root.PID == pid })
		if i < 0 {
			return raphael.Left{}, false
		}
		return ls[i], true
	}
	l, ok := found(look())
	if !ok || l.Session == nil || l.Session.ID != "s38gone" || l.Root.Cwd != wt || !strings.Contains(l.How, "worktree") {
		t.Fatalf("left = %+v, %v; want the sleep, by its folder in the closed session's worktree", l, ok)
	}
	if len(runner.lsofs) != 1 || runner.reads(pid) != 1 {
		t.Errorf("lsof ran %d times, %d of them for the sleep; want once", len(runner.lsofs), runner.reads(pid))
	}
	// The next sample reads it no more: only a process new since, if any.
	if _, ok := found(look()); !ok || runner.reads(pid) != 1 {
		t.Errorf("the next sample: found %v, the sleep read by %d lsofs; want 1", ok, runner.reads(pid))
	}
}
