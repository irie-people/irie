package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/focus"
	"github.com/irie-people/irie/internal/proc"
)

// While the user uses Irie.app, tools/live.sh leaves its next build beside
// it (Irie.app.next) rather than stop the app under them, which would take
// their focus twice: once as macOS brings the next app forward, and again
// as the new one opens. The core swaps it in once another app is in front,
// or the user has stepped away, and opens it in the background, so an
// install never takes the focus (Gabriel, DESIGN §4.11).

// appInUse is how recent the user's input must be for the app in front to
// count as in use.
const appInUse = time.Minute

// appSwap is how the core swaps Irie.app; tests replace its parts.
type appSwap struct {
	app, stamp string                               // the installed app, and the build it came from
	front      func() (bundleID string, ok bool)    // the app in front
	idle       func() (time.Duration, bool)         // since the user's last input
	stop       func() (stopped bool)                // stops the running app; whether one ran
	open       func(app string, inFront bool) error // opens app, in front or in the background
	register   func(app string)                     // tells Launch Services of the new app
}

// swapApp swaps in Irie.app's next build when it waits and the app is not
// in use. A test core never does: the app is the user's.
func (c *Core) swapApp() {
	if runtime.GOOS != "darwin" || gabrielOff() {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	s := appSwap{
		app:   filepath.Join(home, "Applications", brand.Name+".app"),
		stamp: filepath.Join(c.paths.Data, "bin", "app.commit"),
		front: func() (string, bool) {
			a, ok, err := focus.Front()
			return a.BundleID, ok && err == nil
		},
		idle:     machineIdle,
		stop:     stopIrieApp,
		open:     openIrieApp,
		register: registerApp,
	}
	if err := s.run(); err != nil {
		c.log.Warn("app swap", "err", err)
	}
}

// run swaps in the next build if one waits and the app is not in use: in
// front with input in the last minute. It returns what failed.
func (s appSwap) run() error {
	next := s.app + ".next"
	if _, err := os.Stat(next); err != nil {
		return nil
	}
	bundle, ok := s.front()
	inFront := ok && bundle == irieBundleID
	if inFront {
		if idle, ok := s.idle(); !ok || idle < appInUse {
			return nil
		}
	}
	ran := s.stop()
	old := s.app + ".old"
	os.RemoveAll(old)
	if err := os.Rename(s.app, old); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(next, s.app); err != nil {
		os.Rename(old, s.app)
		return err
	}
	os.RemoveAll(old)
	s.register(s.app)
	if err := os.Rename(s.stamp+".next", s.stamp); err != nil && !os.IsNotExist(err) {
		return err
	}
	if ran {
		// In front while the user was away, it comes back in front; else
		// behind what they use.
		return s.open(s.app, inFront)
	}
	return nil
}

// stopIrieApp stops every Irie.app, as tools/live.sh does: the app keeps no
// state of its own (docs/MAC.md).
func stopIrieApp() bool {
	pids := appPIDs()
	for _, pid := range pids {
		syscall.Kill(pid, syscall.SIGTERM)
	}
	for i := 0; i < 30 && len(pids) > 0 && len(appPIDs()) > 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	for _, pid := range appPIDs() {
		syscall.Kill(pid, syscall.SIGKILL)
	}
	return len(pids) > 0
}

// appPIDs are the running Irie.app's processes. irie away runs the same
// executable with --away-helper for an agent's command, which is no app to
// stop: stopping it would stop the command (DESIGN §2.1, principle 6).
func appPIDs() []int {
	out, _ := exec.Command("/usr/bin/pgrep", "-x", brand.Exe).Output()
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(f)
		if err != nil {
			continue
		}
		if argv, _, err := proc.Args(pid); err == nil && slices.Contains(argv, "--away-helper") {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// openIrieApp opens the app: in the background with --background, which
// keeps it from taking the focus (IrieApp.swift), unless inFront.
func openIrieApp(app string, inFront bool) error {
	args := []string{app}
	if !inFront {
		args = []string{"-g", app, "--args", "--background"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/open", args...).Run()
}

// registerApp tells Launch Services of a new app, so Finder and the Dock
// show its icon, as tools/live.sh does.
func registerApp(app string) {
	exec.Command("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister", "-f", app).Run()
}
