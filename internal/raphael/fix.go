package raphael

import (
	"context"
	"errors"
	"fmt"
	"github.com/irie-people/irie/internal/brand"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/proc"
)

// The kinds of fix.
const (
	fixTool      = "tool"      // a tool clears its own cache: go clean -cache, brew cleanup
	fixTrash     = "trash"     // a folder of caches goes to the Trash
	fixStop      = "stop"      // a process is asked to stop
	fixStopTree  = "stoptree"  // a process an agent left running is asked to stop, then what it runs (left.go)
	fixLaunchOff = "launchoff" // a launch agent of the person's own is turned off
	fixNCStyle   = "ncstyle"   // one of macOS's own senders shows its notices in another style (alerts.go)
)

// DoInput is what a fix needs to check, right before it runs, that it is
// still safe.
type DoInput struct {
	Home string
	UID  int
	// State is the core's state folder, where a fix keeps a copy of what
	// it changes; "" for the system's temporary folder.
	State string
	// Wait is how long a tool's fix waits for the tool to be free, such as
	// every Go build and test to end; 0 for 30 minutes.
	Wait time.Duration
	// Poll is how often it looks while it waits; 0 for 10 s.
	Poll time.Duration
}

// What Do uses of the system, which tests replace.
var (
	readTable = proc.Read
	signal    = func(pid int, s syscall.Signal) error { return syscall.Kill(pid, s) }
	rename    = os.Rename
)

// Do runs the fix f, after it checks again that it is still safe, and
// says what it did in words. It never deletes the person's own files: a
// tool clears its own cache, a folder of caches goes to the Trash, a
// process is asked to stop as quitting it would, and a launch agent is
// turned off with its file left where it is.
func Do(ctx context.Context, r Runner, f Fix, in DoInput) (string, error) {
	switch f.kind {
	case fixTool:
		return doTool(ctx, r, f, in)
	case fixTrash:
		return doTrash(ctx, r, f, in)
	case fixStop:
		return doStop(ctx, f)
	case fixStopTree:
		return doStopTree(ctx, f)
	case fixLaunchOff:
		return doLaunchOff(ctx, r, f, in)
	case fixNCStyle:
		return doNCStyle(ctx, r, f, in)
	}
	return "", errors.New(brand.Raffi + " has no such fix")
}

// doNCStyle has one of macOS's own senders show its notices in the style
// the fix names, keeping a copy of the settings as they were.
func doNCStyle(ctx context.Context, r Runner, f Fix, in DoInput) (string, error) {
	dir := NCBackupDir(in.State)
	changed, err := SetStyle(ctx, r, f.ident, f.path, dir)
	if err != nil {
		return "", err
	}
	if !changed {
		return fmt.Sprintf("macOS already shows %s’s notices as %s.", notifierName(f.ident), f.path), nil
	}
	return fmt.Sprintf("macOS shows %s’s notices as %s now. Your notification settings as they were are in %s.", notifierName(f.ident), f.path, tildePath(dir, in.Home)), nil
}

// Style is what a fix that sets the style of one of macOS's senders of
// notices sets: the sender's id and the style; ok is false for any other
// fix.
func (f Fix) Style() (id, style string, ok bool) {
	if f.kind != fixNCStyle {
		return "", "", false
	}
	return f.ident, f.path, true
}

// NCBackupDir is where the copies of macOS's notification settings go,
// under the state folder.
func NCBackupDir(state string) string {
	if state == "" {
		state = os.TempDir()
	}
	return filepath.Join(state, "ncprefs")
}

// doTool waits until the tool is free, then has it clear its cache, and
// says how much it freed.
func doTool(ctx context.Context, r Runner, f Fix, in DoInput) (string, error) {
	wait, poll := in.Wait, in.Poll
	if wait <= 0 {
		wait = 30 * time.Minute
	}
	if poll <= 0 {
		poll = 10 * time.Second
	}
	end := time.Now().Add(wait)
	for {
		ps, err := Procs(ctx, r)
		if err != nil {
			return "", err
		}
		who := busyWith(ps, f.busy)
		if who == "" {
			break
		}
		if time.Now().After(end) {
			return "", fmt.Errorf("%s is still at work (%s), so "+brand.Raffi+" left its cache alone: try again once it is done", f.busy[0], who)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}
	}
	before := duSizes(ctx, r, []string{f.path})[f.path]
	args := []string{"clean", "-cache"}
	if strings.HasSuffix(f.tool, "/brew") {
		args = []string{"cleanup"}
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	out, errb, err := r.Run(tctx, f.tool, args...)
	if err != nil {
		return "", fmt.Errorf("%s: %v: %s", command(append([]string{tildePath(f.tool, in.Home)}, args...)...), err, cut(strings.TrimSpace(string(errb)), 300))
	}
	freed := before - duSizes(ctx, r, []string{f.path})[f.path]
	if n := brewFreed(string(out)); n > 0 {
		freed = n
	}
	if freed > 0 {
		return fmt.Sprintf("%s: freed %s.", f.Label, Size(freed)), nil
	}
	return f.Label + ": done.", nil
}

// brewFreed reads what `brew cleanup` says it freed; 0 when it does not.
func brewFreed(out string) int64 {
	return brewFrees(strings.Replace(out, "has freed approximately", "would free approximately", 1))
}

// busyWith names a process of a tool that works now; "" for none. Go is
// busy while a go command, a compiler or linker of its toolchain or a test
// binary runs; Homebrew while brew runs.
func busyWith(ps []Proc, tools []string) string {
	for _, p := range ps {
		base := filepath.Base(p.Path)
		for _, t := range tools {
			switch t {
			case "go":
				if base == "go" || strings.Contains(p.Path, "/pkg/tool/") || strings.HasSuffix(base, ".test") {
					return fmt.Sprintf("%s, pid %d", base, p.PID)
				}
			case "brew":
				if base == "brew" || strings.Contains(p.Path, "/Homebrew/") {
					return fmt.Sprintf("%s, pid %d", base, p.PID)
				}
			default:
				if base == t {
					return fmt.Sprintf("%s, pid %d", base, p.PID)
				}
			}
		}
	}
	return ""
}

// doTrash puts a folder of caches in the Trash, once it is sure it still is
// one: a folder, not a link, in the caches folder, whose app is not open.
func doTrash(ctx context.Context, r Runner, f Fix, in DoInput) (string, error) {
	dir := cachesDir(in.Home)
	if filepath.Dir(f.path) != dir {
		return "", fmt.Errorf("%s is not in %s, so "+brand.Raffi+" leaves it be", f.path, dir)
	}
	fi, err := os.Lstat(f.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("%s is gone already", tildePath(f.path, in.Home))
	case err != nil:
		return "", err
	case !fi.IsDir():
		return "", fmt.Errorf("%s is no longer a folder, so "+brand.Raffi+" leaves it be", tildePath(f.path, in.Home))
	}
	ps, _ := Procs(ctx, r)
	if appRunning(f.ident, appPath(ctx, r, f.ident), runningApps(ctx, r), ps) {
		return "", fmt.Errorf("its app is open now: quit it, then try again")
	}
	size := duSizes(ctx, r, []string{f.path})[f.path]
	trash := filepath.Join(in.Home, ".Trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(trash, filepath.Base(f.path))
	for i := 2; exists(dest); i++ {
		dest = filepath.Join(trash, fmt.Sprintf("%s %d", filepath.Base(f.path), i))
	}
	if err := rename(f.path, dest); err != nil {
		return "", fmt.Errorf("couldn't move it to the Trash: %v", err)
	}
	return fmt.Sprintf("Put %s (%s) in the Trash: empty the Trash to free the room, or drag it back to %s.", tildePath(f.path, in.Home), Size(size), tildePath(dir, in.Home)), nil
}

// doStop asks a process to stop (SIGTERM), once it is sure it is the same
// process, and waits a few seconds for it to go.
func doStop(ctx context.Context, f Fix) (string, error) {
	same := func() (bool, error) {
		t, err := readTable()
		if err != nil {
			return false, err
		}
		p, ok := t.Get(f.pid)
		return ok && p.Start == f.start && !p.Zombie, nil
	}
	ok, err := same()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%s (pid %d) is no longer running", filepath.Base(f.path), f.pid)
	}
	if err := signal(f.pid, syscall.SIGTERM); err != nil {
		return "", err
	}
	for i := 0; i < 50; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		if ok, _ := same(); !ok {
			return fmt.Sprintf("Stopped %s (pid %d).", filepath.Base(f.path), f.pid), nil
		}
	}
	return fmt.Sprintf("Asked %s (pid %d) to stop, but it still runs: it may be stuck. `kill -9 %d` makes it stop at once, losing what it was doing.", filepath.Base(f.path), f.pid, f.pid), nil
}

// stopGrace is how long doStopTree waits for what it asked to stop to go,
// the root and then what it ran.
var stopGrace = 5 * time.Second

// doStopTree stops a process an agent left running, then what it runs: the
// root first, so that a loop can't start its children again. What it runs
// is read right before, while each still hangs under it, so that a child
// it started again since goes too; each is asked to stop only while it is
// the same process, by when it started. A root that still runs after
// stopGrace may be stuck, and what it runs is left be.
func doStopTree(ctx context.Context, f Fix) (string, error) {
	t, err := readTable()
	if err != nil {
		return "", err
	}
	root := proc.Proc{PID: f.pid, Start: f.start}
	if !t.Alive(root) {
		return "", fmt.Errorf("%s (pid %d) is no longer running", f.path, f.pid)
	}
	kids := t.Tree(f.pid)
	// What it ran when he looked and no longer hangs under it, as when a
	// parent of it ended, goes too.
	for _, r := range f.tree {
		if p, ok := t.Get(r.PID); ok && p.Start == r.Start && !slices.ContainsFunc(kids, func(k proc.Proc) bool { return k.PID == r.PID }) {
			kids = append(kids, p)
		}
	}
	stopAll(t, []proc.Proc{root})
	if left, err := waitStopped(ctx, []proc.Proc{root}); err != nil {
		return "", err
	} else if len(left) > 0 {
		return fmt.Sprintf("Asked %s (pid %d) to stop, but it still runs: it may be stuck, so Raffi left what it runs be. `kill -9 %d` makes it stop at once, losing what it was doing.", f.path, f.pid, f.pid), nil
	}
	if len(kids) == 0 {
		return fmt.Sprintf("Stopped %s (pid %d).", f.path, f.pid), nil
	}
	t, err = readTable()
	if err != nil {
		return "", err
	}
	stopAll(t, kids)
	left, err := waitStopped(ctx, kids)
	if err != nil {
		return "", err
	}
	ran := plural(len(kids), "the process it ran", "the %d processes it ran")
	if len(left) == 0 {
		return fmt.Sprintf("Stopped %s (pid %d) and %s.", f.path, f.pid, ran), nil
	}
	pids := make([]string, len(left))
	for i, p := range left {
		pids[i] = strconv.Itoa(p.PID)
	}
	return fmt.Sprintf("Stopped %s (pid %d), but %d of %s still %s: `kill -9 %s` makes %s stop at once.", f.path, f.pid, len(left), ran, plural(len(left), "runs", "run"), strings.Join(pids, " "), plural(len(left), "it", "them")), nil
}

// stopAll asks each of ps that is still the same process in t to stop, and
// lets one that is stopped (ps's T) go on, so that it can.
func stopAll(t proc.Table, ps []proc.Proc) {
	for _, p := range ps {
		q, ok := t.Get(p.PID)
		if !ok || q.Start != p.Start || q.Zombie {
			continue
		}
		signal(p.PID, syscall.SIGTERM)
		if q.Stopped {
			signal(p.PID, syscall.SIGCONT)
		}
	}
}

// waitStopped waits up to stopGrace for each of ps to go, and returns
// those that still run.
func waitStopped(ctx context.Context, ps []proc.Proc) ([]proc.Proc, error) {
	deadline := time.Now().Add(stopGrace)
	for {
		t, err := readTable()
		if err != nil {
			return nil, err
		}
		var left []proc.Proc
		for _, p := range ps {
			if t.Alive(p) {
				left = append(left, p)
			}
		}
		if len(left) == 0 || time.Now().After(deadline) {
			return left, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// doLaunchOff turns off a launch agent of the person's own: bootout stops
// it now, disable keeps it from starting again. Its file stays.
func doLaunchOff(ctx context.Context, r Runner, f Fix, in DoInput) (string, error) {
	if filepath.Dir(f.path) != filepath.Join(in.Home, "Library", "LaunchAgents") {
		return "", fmt.Errorf("%s is not one of your own launch agents, so "+brand.Raffi+" leaves it be", f.path)
	}
	target := fmt.Sprintf("gui/%d/%s", in.UID, f.ident)
	// bootout fails for one that is not loaded, which is as good.
	r.Run(ctx, "/bin/launchctl", "bootout", target)
	if _, errb, err := r.Run(ctx, "/bin/launchctl", "disable", target); err != nil {
		return "", fmt.Errorf("launchctl disable %s: %v: %s", target, err, strings.TrimSpace(string(errb)))
	}
	return fmt.Sprintf("Turned off %s: it no longer starts. Its file stays in ~/Library/LaunchAgents; to turn it back on, run: %s", f.ident, f.Undo), nil
}
