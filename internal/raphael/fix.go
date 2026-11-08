package raphael

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	fixLaunchOff = "launchoff" // a launch agent of the person's own is turned off
)

// DoInput is what a fix needs to check, right before it runs, that it is
// still safe.
type DoInput struct {
	Home string
	UID  int
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
	case fixLaunchOff:
		return doLaunchOff(ctx, r, f, in)
	}
	return "", errors.New("Raffi has no such fix")
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
			return "", fmt.Errorf("%s is still at work (%s), so Raffi left its cache alone: try again once it is done", f.busy[0], who)
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
		return "", fmt.Errorf("%s is not in %s, so Raffi leaves it be", f.path, dir)
	}
	fi, err := os.Lstat(f.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("%s is gone already", tildePath(f.path, in.Home))
	case err != nil:
		return "", err
	case !fi.IsDir():
		return "", fmt.Errorf("%s is no longer a folder, so Raffi leaves it be", tildePath(f.path, in.Home))
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

// doLaunchOff turns off a launch agent of the person's own: bootout stops
// it now, disable keeps it from starting again. Its file stays.
func doLaunchOff(ctx context.Context, r Runner, f Fix, in DoInput) (string, error) {
	if filepath.Dir(f.path) != filepath.Join(in.Home, "Library", "LaunchAgents") {
		return "", fmt.Errorf("%s is not one of your own launch agents, so Raffi leaves it be", f.path)
	}
	target := fmt.Sprintf("gui/%d/%s", in.UID, f.ident)
	// bootout fails for one that is not loaded, which is as good.
	r.Run(ctx, "/bin/launchctl", "bootout", target)
	if _, errb, err := r.Run(ctx, "/bin/launchctl", "disable", target); err != nil {
		return "", fmt.Errorf("launchctl disable %s: %v: %s", target, err, strings.TrimSpace(string(errb)))
	}
	return fmt.Sprintf("Turned off %s: it no longer starts. Its file stays in ~/Library/LaunchAgents; to turn it back on, run: %s", f.ident, f.Undo), nil
}
