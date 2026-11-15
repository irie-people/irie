// Package bridge is `irie bridge`, the Windows app's way into Irie inside
// WSL 2 (docs/WINDOWS.md §5). The app runs it through one wsl.exe and
// speaks package mux over its standard input and output. Each stream it
// opens is a connection to the core's socket, which the bridge relays as
// it is, or a tmux client attached to Irie's server in a terminal of its
// own, a pty here in Linux, whose bytes go to the app's terminal as tmux
// wrote them.
//
// The bridge keeps no state and no rules, and it never touches an agent:
// it only connects to the core, as any client does, and attaches tmux
// clients, which detach when it ends.
package bridge

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/mux"
)

// Options are what the bridge needs from irie.
type Options struct {
	Socket  string       // where the core listens
	Version string       // irie's version, for the hello
	Up      func() error // starts the core, as `irie up` does; nil never starts one
	Env     []string     // the environment tmux clients start in
	Log     io.Writer    // what goes wrong, for the app's log; nil = nowhere
}

// Serve speaks the mux on in and out until the app's side ends: first a
// hello that says how the core stands, starting it when it does not
// answer, then the streams the app opens.
func Serve(in io.Reader, out io.Writer, o Options) error {
	if o.Log == nil {
		o.Log = io.Discard
	}
	h := mux.Hello{Version: o.Version}
	switch {
	case answers(o.Socket):
		h.Core = "running"
	case o.Up == nil:
		h.Problem = brand.Name + " is not running inside Linux."
	default:
		if err := o.Up(); err != nil {
			h.Problem = brand.Name + " did not start inside Linux: " + err.Error()
		} else if answers(o.Socket) {
			h.Core = "started"
		} else {
			h.Problem = brand.Name + " started inside Linux, but its core does not answer."
		}
	}
	if h.Problem != "" {
		fmt.Fprintln(o.Log, "bridge:", h.Problem)
	}
	return mux.Serve(in, out, h, func(op mux.Open, s *mux.Stream) {
		switch op.Kind {
		case mux.StreamCore:
			relay(o.Socket, s)
		case mux.StreamTmux:
			if err := attach(op, o.Env, s); err != nil {
				fmt.Fprintln(o.Log, "bridge: tmux:", err)
				s.CloseWith(mux.End{Error: err.Error()})
			}
		default:
			s.CloseWith(mux.End{Error: fmt.Sprintf("the bridge has no stream of the kind %q", op.Kind)})
		}
	})
}

// answers says whether the core at socket says hello.
func answers(socket string) bool {
	var r ipc.HelloResult
	return ipc.Call(socket, time.Second, ipc.OpHello, ipc.HelloArgs{Client: "bridge", API: ipc.API}, &r) == nil
}

// relay connects s to the core's socket, both ways, until either side
// ends.
func relay(socket string, s *mux.Stream) {
	c, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		s.CloseWith(mux.End{Error: "The core is not answering (" + err.Error() + "). Is " + brand.Name + " running inside Linux?"})
		return
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			c.Close()
			s.Close()
		})
	}
	go func() {
		io.Copy(c, s)
		stop()
	}()
	io.Copy(s, c)
	stop()
}

// attach runs a tmux client of the session op names in a terminal of its
// own, relaying the terminal both ways and giving it each new size, until
// the client exits or the app ends the stream.
func attach(op mux.Open, env []string, s *mux.Stream) error {
	argv, err := tmuxArgs(op)
	if err != nil {
		return err
	}
	cols, rows := clamp(op.Cols, 80), clamp(op.Rows, 24)
	master, slave, err := openPTY()
	if err != nil {
		return fmt.Errorf("no terminal for tmux: %w", err)
	}
	if err := setSize(master, cols, rows); err != nil {
		master.Close()
		slave.Close()
		return err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = termEnv(env)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = ttyAttr()
	if err := cmd.Start(); err != nil {
		master.Close()
		slave.Close()
		return fmt.Errorf("tmux did not start: %w", err)
	}
	slave.Close()
	s.OnSize(func(z mux.Size) { setSize(master, clamp(z.Cols, cols), clamp(z.Rows, rows)) })

	// What tmux draws, to the app, until the terminal closes.
	drawn := make(chan struct{})
	go func() {
		io.Copy(s, master)
		close(drawn)
	}()
	// The app's keys, to the terminal. Once the app ends the stream, tmux
	// hears that its terminal went, and detaches: every agent stays as it
	// was.
	go func() {
		io.Copy(master, s)
		hangUp(cmd.Process)
	}()
	err = cmd.Wait()
	// What tmux wrote last may still be on its way out of the terminal.
	select {
	case <-drawn:
	case <-time.After(500 * time.Millisecond):
	}
	master.Close()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	}
	s.CloseWith(mux.End{Exit: &code})
	return nil
}

// tmuxArgs is the command line of the tmux client op asks for: tmux's
// UTF-8 on, as the app's terminal draws it, attached to Target on the
// server at Socket.
func tmuxArgs(op mux.Open) ([]string, error) {
	bin := op.Tmux
	if bin == "" {
		bin = "tmux"
	} else if !filepath.IsAbs(bin) {
		return nil, fmt.Errorf("tmux must be a full path, not %q", bin)
	}
	if !filepath.IsAbs(op.Socket) {
		return nil, fmt.Errorf("the tmux socket must be a full path, not %q", op.Socket)
	}
	if op.Target == "" || strings.HasPrefix(op.Target, "-") {
		return nil, fmt.Errorf("no tmux session to attach to")
	}
	return []string{bin, "-u", "-S", op.Socket, "attach-session", "-t", op.Target}, nil
}

// termEnv is env for a tmux client in the app's terminal: xterm's own
// TERM, with its colours, and no TMUX, so tmux does not take itself to be
// nested.
func termEnv(env []string) []string {
	out := make([]string, 0, len(env)+2)
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		switch k {
		case "TMUX", "TMUX_PANE", "TERM", "COLORTERM":
			continue
		}
		out = append(out, e)
	}
	return append(out, "TERM=xterm-256color", "COLORTERM=truecolor")
}

// clamp keeps a terminal's side to what a terminal can be, or def when
// none was given.
func clamp(n, def int) int {
	switch {
	case n <= 0:
		return def
	case n < 2:
		return 2
	case n > 1000:
		return 1000
	}
	return n
}

// errNoPTY is openPTY where the bridge has no terminals to give.
var errNoPTY = errors.New("this system has no terminals the bridge can open")
