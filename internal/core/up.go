package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/paths"
	"github.com/irie-hq/irie/internal/tmux"
)

// upWait is how long `irie up` waits for a core to answer.
const upWait = 5 * time.Second

func runner(cfg config.Config) (*tmux.Runner, error) {
	bin, err := tmux.Find(cfg.Tmux.Path)
	if err != nil {
		return nil, err
	}
	return &tmux.Runner{Bin: bin, Socket: cfg.Tmux.Socket}, nil
}

func hello(p paths.Paths, timeout time.Duration) (string, error) {
	var r ipc.HelloResult
	err := ipc.Call(p.Socket(), timeout, ipc.OpHello, ipc.HelloArgs{Client: "up", API: ipc.API}, &r)
	return r.Version, err
}

// Up is `irie up`. It is idempotent: it starts the server and the core when
// the socket does not answer, and replaces a core, sidebar or placeholder
// that runs another version. Agents are untouched.
func Up(cfg config.Config, p paths.Paths, bin, version string) error {
	if err := p.EnsureRuntime(); err != nil {
		return err
	}
	if err := p.EnsureState(); err != nil {
		return err
	}
	lock, err := lockFile(p.UpLock(), 2*upWait)
	if err != nil {
		return fmt.Errorf("up.lock: %w", err)
	}
	defer lock.Close()
	r, err := runner(cfg)
	if err != nil {
		return err
	}

	conf := []byte(tmux.Conf(tmux.ConfOptions{FocusKey: cfg.UI.FocusKey, SidebarWidth: cfg.UI.SidebarWidth}))
	old, _ := os.ReadFile(p.TmuxConf())
	changed := !bytes.Equal(old, conf)
	if changed {
		if err := os.WriteFile(p.TmuxConf(), conf, 0o600); err != nil {
			return err
		}
	}

	// A changed tmux.conf means a changed config, which the core reads only
	// when it starts, so the core is replaced as well.
	if v, err := hello(p, 500*time.Millisecond); err != nil || v != version || changed {
		if err := startCore(r, p, []string{bin, "core", "--supervise"}, version); err != nil {
			if changed {
				os.Remove(p.TmuxConf()) // so the next up tries again
			}
			return err
		}
	}

	// A new core builds the sidebar before it answers; a running one repairs
	// a missing sidebar within a tick.
	var snap tmux.Snapshot
	for deadline := time.Now().Add(upWait); ; {
		if snap, err = r.Snapshot(); err != nil {
			return err
		}
		if ui := snap.Sidebar(); (ui != nil && ui.Role == tmux.RoleUI) || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Irie's own panes are restarted when they run another version; agent
	// panes never are.
	var restart [][]string
	if ui := snap.Sidebar(); ui != nil && ui.Role == tmux.RoleUI && ui.Version != version {
		restart = append(restart,
			[]string{"respawn-pane", "-k", "-t", ui.ID, "--", bin, "ui", "--supervise"},
			[]string{"set-option", "-p", "-t", ui.ID, tmux.OptVersion, version})
	}
	for _, p := range snap.ByRole(tmux.RolePlaceholder) {
		if p.Version != version {
			restart = append(restart,
				[]string{"respawn-pane", "-k", "-t", p.ID, "--", bin, "placeholder"},
				[]string{"set-option", "-p", "-t", p.ID, tmux.OptVersion, version})
		}
	}
	if len(restart) > 0 {
		if _, err := r.RunList(restart); err != nil {
			return err
		}
	}
	return nil
}

// startCore starts the server with a core, or replaces the server's core,
// and waits for the new core to answer.
func startCore(r *tmux.Runner, p paths.Paths, coreCmd []string, version string) error {
	snap, err := r.Snapshot()
	switch {
	case errors.Is(err, tmux.ErrNoServer):
		err = r.StartServer(p.TmuxConf(), coreCmd)
	case err == nil:
		err = respawnCore(r, snap, p.TmuxConf(), coreCmd)
	}
	if err != nil {
		return err
	}
	core := tmux.AgentsSession + ":=" + tmux.CoreWindow
	if _, err := r.RunList([][]string{
		{"set-option", "-p", "-t", core, tmux.OptRole, tmux.RoleCore},
		{"set-option", "-p", "-t", core, tmux.OptVersion, version},
	}); err != nil {
		return err
	}
	return waitForCore(p, version)
}

// respawnCore reloads tmux.conf and starts a new core in the core window,
// creating the window or the whole irie-agents session when they are gone.
func respawnCore(r *tmux.Runner, snap tmux.Snapshot, conf string, coreCmd []string) error {
	if _, err := r.Run("source-file", conf); err != nil {
		return err
	}
	var cmd []string
	switch cores := snap.ByRole(tmux.RoleCore); {
	case len(cores) > 0:
		cmd = []string{"respawn-pane", "-k", "-t", cores[0].ID, "--"}
	case snap.HasSession(tmux.AgentsSession):
		cmd = []string{"new-window", "-d", "-t", tmux.AgentsSession + ":", "-n", tmux.CoreWindow, "--"}
	default:
		cmd = []string{"new-session", "-d", "-s", tmux.AgentsSession, "-n", tmux.CoreWindow, "-x", "80", "-y", "24", "--"}
	}
	_, err := r.Run(append(cmd, coreCmd...)...)
	return err
}

func waitForCore(p paths.Paths, version string) error {
	deadline := time.Now().Add(upWait)
	for {
		v, err := hello(p, 500*time.Millisecond)
		if err == nil && v == version {
			return nil
		}
		if time.Now().After(deadline) {
			if err == nil {
				err = fmt.Errorf("it runs version %s, not %s", v, version)
			}
			return fmt.Errorf("the core did not answer within %v (%v); see %s", upWait, err, p.CoreLog())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Attach replaces this process with a tmux client of session irie. Inside
// another tmux it refuses unless nested is set; inside Irie's own server it
// switches the current client instead.
func Attach(cfg config.Config, nested bool) error {
	r, err := runner(cfg)
	if err != nil {
		return err
	}
	env := os.Environ()
	argv := []string{"tmux", "-L", cfg.Tmux.Socket, "attach-session", "-t", tmux.MainSession}
	if inside := os.Getenv("TMUX"); inside != "" {
		sock, _, _ := strings.Cut(inside, ",")
		if own, err := r.SocketPath(); err == nil && sameFile(sock, own) {
			return syscall.Exec(r.Bin, []string{"tmux", "-L", cfg.Tmux.Socket, "switch-client", "-t", tmux.MainSession}, env)
		}
		if !nested {
			return errors.New("already inside tmux; run `irie attach --nested` to open Irie inside it")
		}
		env = tmux.WithoutEnv(env, "TMUX", "TMUX_PANE")
	}
	return syscall.Exec(r.Bin, argv, env)
}

func sameFile(a, b string) bool {
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(fa, fb)
}
