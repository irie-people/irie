package core

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/harness"
	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
)

// moveWait is how long a move waits for the outside copy to exit. Claude
// Code gives its SessionEnd hooks 1.5 s by default, so a clean exit fits.
const moveWait = 5 * time.Second

// move carries an idle Claude Code session that runs outside Irie on in an
// Irie pane (DESIGN §4.6). It ends the outside copy, then resumes the
// conversation in place, in the folder it ran in, with Irie's settings and
// hooks. Nothing is spawned unless the outside copy has exited.
func (c *Core) move(a ipc.MoveArgs) (ipc.Session, error) {
	if a.VendorID == "" {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "move needs a vendor_id")
	}
	profileName, profile, err := c.resumeProfile(a.Profile)
	if err != nil {
		return ipc.Session{}, err
	}
	h, err := harnessFor(profile.Harness)
	if err != nil {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "profile %s: %v", profileName, err)
	}
	// `claude stop` gets the profile's environment without a local model's.
	env := c.agentEnv(profile)
	// A local profile's model is checked, and may be loaded, as for a spawn
	// (DESIGN §6.1), and before the outside copy ends.
	var warnings []string
	if profile.Local != nil {
		if warnings, err = c.prepareLocal(profileName, &profile, false); err != nil {
			c.log.Warn("move refused", "profile", profileName, "err", err)
			return ipc.Session{}, err
		}
	}
	var probe harness.Probe
	if p, ok := h.(harness.Prober); ok {
		probe = p.Probe(profile, c.agentEnv(profile))
	}
	probe.Warnings = append(warnings, probe.Warnings...)

	// One spawn or move at a time, and the decision rests on what Claude
	// Code says now, not on the last poll.
	c.spawnMu.Lock()
	defer c.spawnMu.Unlock()
	if err := c.refreshOutside(0, true); err != nil {
		return ipc.Session{}, ipc.Errorf(ipc.CodeInternal, "cannot list the Claude Code sessions: %v", err)
	}
	c.mu.Lock()
	o, found := c.findOutside(a.VendorID)
	ids, _ := c.ownSessions()
	c.mu.Unlock()
	switch {
	case !found && ids[a.VendorID]:
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "session %s already runs in Irie", a.VendorID)
	case !found:
		return ipc.Session{}, ipc.Errorf(ipc.CodeNotFound, "no Claude Code session %s runs outside Irie", a.VendorID)
	case !o.Movable:
		return ipc.Session{}, ipc.Errorf(ipc.CodeBusy, "%s cannot move now: %s", outsideName(o), o.Why)
	}
	if fi, err := os.Stat(o.Cwd); err != nil || !fi.IsDir() {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "%s ran in %s, which is gone", outsideName(o), o.Cwd)
	}

	stop := func() error { return stopBackground(profile.Command, env, o.ID) }
	if err := endOutside(o, stop, moveWait); err != nil {
		c.log.Warn("move", "session", o.VendorID, "err", err)
		return ipc.Session{}, err
	}

	c.mu.Lock()
	// The name Claude Code lists it under, which its titles may change
	// (title.go).
	name := cleanText(o.Name, 200)
	if name == "" {
		name = slug(filepath.Base(o.Cwd))
	}
	x, err := c.launch(job{
		place: place{name: unique(name, c.names(nil)), cwd: o.Cwd}, profile: profileName, run: profile, h: h, probe: probe,
		resume: o.VendorID, data: map[string]any{"resume": o.VendorID, "kind": o.Kind},
	})
	if err == nil {
		// The conversation is Irie's now, before the next poll says so.
		c.outside.list = slices.DeleteFunc(c.outside.list, func(l ipc.OutsideSession) bool { return l.VendorID == o.VendorID })
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	if err != nil {
		c.log.Warn("move", "session", o.VendorID, "err", err)
		return ipc.Session{}, fmt.Errorf("%s stopped where it ran, but Irie could not start it: %v; `claude --resume %s` in %s carries it on", outsideName(o), err, o.VendorID, o.Cwd)
	}
	c.publishOutside()
	c.log.Info("moved", "session", x.id, "name", x.name, "resume", o.VendorID, "kind", o.Kind)
	return x.wire(), nil
}

// resumeProfile is the profile a moved session runs on: the one asked for,
// which must run Claude Code, or else the profile that lists the sessions.
func (c *Core) resumeProfile(name string) (string, config.Profile, error) {
	if name == "" {
		name, p, ok := c.claudeProfile()
		if !ok {
			return "", config.Profile{}, ipc.Errorf(ipc.CodeNotFound, "no profile runs Claude Code")
		}
		return name, p, nil
	}
	p, ok := c.profile(name)
	if !ok {
		return "", config.Profile{}, ipc.Errorf(ipc.CodeNotFound, "no profile +%s; the profiles are %s, and +%s<model> for a local model", name, strings.Join(slices.Sorted(maps.Keys(c.cfg.Profiles)), ", "), localPrefix)
	}
	if p.Harness != "claude-code" {
		return "", config.Profile{}, ipc.Errorf(ipc.CodeBadArgs, "profile +%s runs %s, which cannot carry a Claude Code conversation on", name, p.Harness)
	}
	return name, p, nil
}

// findOutside returns the listed session outside Irie with the vendor id.
// The caller holds c.mu.
func (c *Core) findOutside(vendorID string) (ipc.OutsideSession, bool) {
	for _, o := range c.outside.list {
		if o.VendorID == vendorID {
			return o, true
		}
	}
	return ipc.OutsideSession{}, false
}

// outsideName is how messages name an outside session.
func outsideName(o ipc.OutsideSession) string {
	if o.Name != "" {
		return o.Name
	}
	return "session " + o.Key()
}

// endOutside ends the outside copy of a session: `claude stop <id>`, by
// stop, for a background session, which Claude Code's supervisor would
// otherwise start again, and SIGTERM for an interactive one. It then waits
// up to wait for the process to exit. It never sends SIGKILL, and never
// signals anything but one process that Claude Code named.
func endOutside(o ipc.OutsideSession, stop func() error, wait time.Duration) error {
	name := outsideName(o)
	if o.PID <= 1 {
		return ipc.Errorf(ipc.CodeBadArgs, "%s has no process to stop, so nothing was moved", name)
	}
	switch o.Kind {
	case ipc.KindBackground:
		if err := stop(); err != nil {
			return ipc.Errorf(ipc.CodeInternal, "claude stop %s: %v; nothing was moved", o.ID, err)
		}
	case ipc.KindInteractive:
		if err := syscall.Kill(o.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return ipc.Errorf(ipc.CodeInternal, "cannot stop %s (pid %d): %v; nothing was moved", name, o.PID, err)
		}
	default:
		return ipc.Errorf(ipc.CodeBadArgs, "Irie cannot move a session of kind %q", o.Kind)
	}
	for deadline := time.Now().Add(wait); alive(o.PID); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			return ipc.Errorf(ipc.CodeBusy, "%s (pid %d) did not exit within %v of being asked to stop, so nothing was moved; it may still run where it was", name, o.PID, wait)
		}
	}
	return nil
}

// alive reports whether a process exists, ours to signal or not.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// stopBackground runs `<command> stop <id>` for a background session.
func stopBackground(command string, env []string, id string) error {
	if id == "" || strings.HasPrefix(id, "-") {
		return errors.New("no short id")
	}
	path, err := host.LookPath(command, env)
	if err != nil {
		return fmt.Errorf("%s: command not found", command)
	}
	_, err = runCLI(path, []string{"stop", id}, env)
	return err
}
