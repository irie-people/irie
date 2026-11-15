package main

import (
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/winapp"
	"github.com/irie-people/irie/internal/wsl"
)

const (
	createNoWindow   = 0x08000000
	createNewConsole = 0x00000010
)

// machine is this Windows for the backend: how far it is set up (package
// wsl, as `irie setup` looks), the fixes, each in a console window of its
// own where it asks something, and the bridge, one wsl.exe running
// `irie bridge` with no window.
type machine struct {
	env wsl.Env
}

// newMachine is this Windows, or with IRIE_APP_BRIDGE a stand-in for Irie
// in Linux: a command that speaks the bridge's side, such as
// tools/winpreview -bridge, for CI on a Windows without WSL 2.
func newMachine(dir string) winapp.Machine {
	if cmd := strings.TrimSpace(os.Getenv(brand.Env("APP_BRIDGE"))); cmd != "" {
		return standIn{cmd}
	}
	exe, _ := os.Executable()
	e := wsl.EnvFromOS()
	// The app is the Start menu's; the wrapper beside it runs Irie in a
	// terminal, at sign-in and in Windows Terminal's profile.
	e.App, e.Exe = exe, wsl.WrapperBeside(exe)
	return &machine{env: e}
}

func (m *machine) Look() wsl.State { return wsl.Look(m, m.env) }

func (m *machine) Fix(st wsl.State, step wsl.Step) error {
	if step.ID == "install" {
		// In a window of its own that waits for Enter, so its last words
		// can be read before it closes.
		code, err := m.Run("wsl.exe", wsl.InstallWindow(st.Distro.Name)...)
		if err == nil && code != 0 {
			err = errors.New("the installer did not finish")
		}
		return err
	}
	return wsl.Fix(m, m.env, st, step)
}

// Output runs a program with no window and gives what it wrote.
func (m *machine) Output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	b, err := cmd.Output()
	return wsl.Decode(b), err
}

// Run runs a program for the person: wsl.exe in a console window of its
// own, since it may ask them something, and PowerShell, whose scripts
// ask nothing (an administrator's OK comes in a window of Windows' own),
// with none.
func (m *machine) Run(name string, args ...string) (int, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	flags := uint32(createNewConsole)
	if strings.EqualFold(name, "powershell.exe") {
		flags = createNoWindow
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	case err != nil:
		return -1, err
	}
	return 0, nil
}

// Distros is wsl.Lister's: WSL's own list, from the registry.
func (m *machine) Distros() []wsl.Distro { return wsl.RegistryDistros() }

func (m *machine) Bridge(distro string) (winapp.Process, error) {
	return startBridge("wsl.exe", wsl.Command(distro, []string{"bridge"}))
}

// standIn is a computer whose Irie is a command of the tests'.
type standIn struct{ cmd string }

func (s standIn) Look() wsl.State {
	return wsl.State{Build: 26100, WSL: true, Distro: wsl.Distro{Name: "stand-in", Version: 2}, HasDistro: true, Installed: true,
		KeepRunning: true, Shortcut: true, Startup: true}
}

func (s standIn) Fix(wsl.State, wsl.Step) error { return nil }

func (s standIn) Bridge(string) (winapp.Process, error) {
	argv, err := windows.DecomposeCommandLine(s.cmd)
	if err != nil || len(argv) == 0 {
		return nil, errors.New("IRIE_APP_BRIDGE is not a command line")
	}
	return startBridge(argv[0], argv[1:])
}

// bridge is a running bridge: its pipes, and the end of what it wrote to
// standard error, which also goes to the app's log.
type bridge struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	tail   *tail
	once   sync.Once
	code   int
	done   chan struct{}
}

func startBridge(name string, args []string) (*bridge, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	t := &tail{}
	cmd.Stderr = t
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	log.Printf("bridge: %s %s (pid %d)", name, strings.Join(args, " "), cmd.Process.Pid)
	b := &bridge{cmd: cmd, stdin: in, stdout: out, tail: t, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			b.code = exit.ExitCode()
		}
		log.Printf("bridge ended: %v; it said: %s", err, t.String())
		close(b.done)
	}()
	return b, nil
}

func (b *bridge) Read(p []byte) (int, error)  { return b.stdout.Read(p) }
func (b *bridge) Write(p []byte) (int, error) { return b.stdin.Write(p) }

func (b *bridge) Close() error {
	b.once.Do(func() {
		b.stdin.Close()
		b.cmd.Process.Kill()
	})
	return nil
}

func (b *bridge) Wait() (int, string) {
	<-b.done
	return b.code, b.tail.String()
}

// tail keeps the last 4 KiB written to it, and logs each line.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	log.Printf("bridge: %s", strings.TrimRight(string(p), "\r\n"))
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
