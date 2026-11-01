// Package loginenv reads the environment a person's login shell sets up.
// An app opened from the Finder, the Dock or Spotlight starts with
// launchd's short PATH, so a server it started would not find claude, git
// or node; `irie up --login-env`, which the Mac app runs, starts the server
// with this environment instead (DESIGN §7.2).
package loginenv

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Marker comes before the environment in the shell's output, so whatever
// its startup files print is skipped.
const Marker = "__IRIE_LOGIN_ENV__"

// Timeout is how long a shell gets to start and print its environment.
const Timeout = 10 * time.Second

// dropped are names a login environment does not pass on: the shell's own,
// a tmux it may run in, and the app's launch.
var dropped = []string{"PWD", "OLDPWD", "SHLVL", "_", "TMUX", "TMUX_PANE", "__CFBundleIdentifier", "XPC_SERVICE_NAME", "XPC_FLAGS"}

// Capture runs shell as a login and interactive shell, as a terminal
// would, and returns the environment it sets up. Its startup files get no
// input, and after timeout the shell and what it started are killed.
func Capture(shell string, env []string, timeout time.Duration) ([]string, error) {
	if shell == "" {
		return nil, errors.New("no shell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", `printf '%s' `+Marker+`; env -0`)
	cmd.Env = env
	cmd.Stdin = nil // /dev/null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	if home := value(env, "HOME"); home != "" {
		cmd.Dir = home
	}
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, errors.New("the login shell took longer than " + timeout.String())
	}
	got, perr := Parse(out)
	if perr != nil {
		if err != nil {
			return nil, err
		}
		return nil, perr
	}
	return got, nil
}

// Parse reads the NUL-separated environment after the last Marker.
func Parse(out []byte) ([]string, error) {
	i := bytes.LastIndex(out, []byte(Marker))
	if i < 0 {
		return nil, errors.New("the login shell printed no environment")
	}
	var env []string
	for _, kv := range strings.Split(string(out[i+len(Marker):]), "\x00") {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" || strings.ContainsAny(name, "\n ") || slices.Contains(dropped, name) {
			continue
		}
		env = append(env, kv)
	}
	if value(env, "PATH") == "" {
		return nil, errors.New("the login shell printed no PATH")
	}
	return env, nil
}

// ForServer returns the environment a server started from an app should
// have: the login shell's, else env as it is. A shell that set no locale
// gets the one Terminal sets on a Mac, so agents write UTF-8.
func ForServer(env []string, goos string) ([]string, error) {
	shell := value(env, "SHELL")
	if shell == "" {
		shell = "/bin/zsh"
		if goos != "darwin" {
			shell = "/bin/sh"
		}
	}
	got, err := Capture(shell, env, Timeout)
	if err != nil {
		return env, err
	}
	if goos == "darwin" && value(got, "LANG") == "" && value(got, "LC_ALL") == "" && value(got, "LC_CTYPE") == "" {
		got = append(got, "LC_CTYPE=UTF-8")
	}
	return got, nil
}

func value(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if s, ok := strings.CutPrefix(kv, key+"="); ok {
			v = s
		}
	}
	return v
}
