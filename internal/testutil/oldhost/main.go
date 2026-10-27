// Command oldhost stands in for `irie host` as it was before freeze, which
// the agents of a running Irie may still have: it starts the agent in its
// own process group, in the terminal's foreground, continues every stop of
// the agent it sees, passes SIGHUP, SIGTERM and SIGINT on to the agent's
// group, and writes exit.json. It writes no host.json and knows nothing of
// freeze.json. Run it as `oldhost host --spec <file>`.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type spec struct {
	Argv  []string          `json:"argv"`
	Env   map[string]string `json:"env"`
	Unset []string          `json:"unset"`
	Cwd   string            `json:"cwd"`
}

func main() {
	if len(os.Args) != 4 || os.Args[1] != "host" || os.Args[2] != "--spec" {
		fmt.Fprintln(os.Stderr, "usage: oldhost host --spec FILE")
		os.Exit(2)
	}
	os.Exit(run(os.Args[3]))
}

func run(specPath string) int {
	var s spec
	b, err := os.ReadFile(specPath)
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil || len(s.Argv) == 0 {
		fmt.Fprintln(os.Stderr, "oldhost: bad spec:", err)
		return 126
	}
	finish := func(code, sig int) int {
		x := map[string]any{"code": code, "at": time.Now().UTC()}
		if sig != 0 {
			x["signal"] = sig
		}
		b, _ := json.Marshal(x)
		os.WriteFile(filepath.Join(filepath.Dir(specPath), "exit.json"), b, 0o600)
		return code
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, set := s.Env[k]; !set && !slices.Contains(s.Unset, k) {
			env = append(env, kv)
		}
	}
	for k, v := range s.Env {
		env = append(env, k+"="+v)
	}
	path, err := exec.LookPath(s.Argv[0])
	if err != nil {
		return finish(127, 0)
	}
	attr := &syscall.ProcAttr{Dir: s.Cwd, Env: env, Files: []uintptr{0, 1, 2},
		Sys: &syscall.SysProcAttr{Setpgid: true, Foreground: true, Ctty: 0}}
	foreground := true
	pid, err := syscall.ForkExec(path, s.Argv, attr)
	if errors.Is(err, syscall.ENOTTY) {
		foreground = false
		attr.Sys = &syscall.SysProcAttr{Setpgid: true}
		pid, err = syscall.ForkExec(path, s.Argv, attr)
	}
	if err != nil {
		return finish(126, 0)
	}
	signal.Ignore(syscall.SIGTTOU, syscall.SIGTTIN)
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for sig := range sigs {
			syscall.Kill(-pid, sig.(syscall.Signal))
		}
	}()
	for {
		var ws syscall.WaitStatus
		_, err := syscall.Wait4(pid, &ws, syscall.WUNTRACED, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return finish(126, 0)
		}
		if ws&0x7f == 0x7f { // stopped: continued at once
			syscall.Kill(-pid, syscall.SIGCONT)
			continue
		}
		if foreground {
			p := int32(syscall.Getpgrp())
			syscall.Syscall(syscall.SYS_IOCTL, 0, uintptr(syscall.TIOCSPGRP), uintptr(unsafe.Pointer(&p)))
		}
		if ws.Signaled() {
			return finish(128+int(ws.Signal()), int(ws.Signal()))
		}
		if ws.Exited() {
			return finish(ws.ExitStatus(), 0)
		}
	}
}
