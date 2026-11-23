package core

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// setupShell starts Irie with spawn.shell_after_exit on, which test cores
// have off (internal/testutil), and a plain shell for the user's: theirs
// may ask questions on a first run, and a login shell would read their
// own profile. The core starts "$SHELL -l", and this one drops the -l.
func setupShell(t *testing.T) *coreEnv {
	t.Helper()
	plain := filepath.Join(t.TempDir(), "plain-sh")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\nexec /bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", plain)
	c := prepare(t, agentConfig{})
	t.Setenv(brand.Env(shellAfterExitEnv), "on")
	c.up()
	return c
}

// format reads a tmux format of a pane; "" once the pane has gone.
func (c *coreEnv) format(pane, f string) string {
	c.t.Helper()
	out, _ := c.TryTmux("list-panes", "-a", "-F", "#{pane_id}\t"+f)
	for _, line := range strings.Split(out, "\n") {
		if id, v, ok := strings.Cut(line, "\t"); ok && id == pane {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// TestAnAgentThatExitsLeavesItsTerminalAsTheUsersShell follows a terminal
// that goes on as the user's shell once its agent exits by itself
// (spawn.shell_after_exit, DESIGN §2.3): its row says the agent exited, with
// its code, and that its terminal is a shell now; keys reach the shell, in
// the session's folder; Close All Finished passes it over while a program
// runs there, and its close ends the shell through its host, as an agent's.
func TestAnAgentThatExitsLeavesItsTerminalAsTheUsersShell(t *testing.T) {
	c := setupShell(t)
	s := c.spawn("prompt,stop,exit:3")
	row := c.waitState(s.ID, ipc.StateExited)
	if row.ExitCode == nil || *row.ExitCode != 3 {
		t.Fatalf("row = %+v, want exit code 3", row)
	}
	testutil.Eventually(t, 10*time.Second, "the row does not say its terminal is a shell", func() bool {
		row, _ = c.row(s.ID)
		return row.Shell && row.Detail == "exited 3 · now a shell"
	})
	if c.format(row.Pane, "#{pane_dead}") != "0" {
		t.Fatal("the pane died with its agent")
	}

	// Keys reach the shell, which runs in the session's folder, with the
	// session's own variable, so that irie ctl there speaks for it.
	if err := c.call(ipc.OpKeys, ipc.KeysArgs{Session: s.ID, Text: `printf '%s|%s\n' "$PWD" "${` + brand.Env("SESSION") + `-unset}" > where.txt`}, nil); err != nil {
		t.Fatalf("keys to the shell: %v", err)
	}
	if err := c.call(ipc.OpKeys, ipc.KeysArgs{Session: s.ID, Keys: []string{"Enter"}}, nil); err != nil {
		t.Fatalf("Enter to the shell: %v", err)
	}
	where := filepath.Join(row.Cwd, "where.txt")
	testutil.Eventually(t, 10*time.Second, "the shell did not run what keys typed", func() bool {
		b, err := os.ReadFile(where)
		return err == nil && strings.HasSuffix(string(b), "\n")
	})
	got, _ := os.ReadFile(where)
	dir, id, _ := strings.Cut(strings.TrimSpace(string(got)), "|")
	want, _ := filepath.EvalSymlinks(row.Cwd)
	if g, _ := filepath.EvalSymlinks(dir); g != want || id != s.ID {
		t.Fatalf("the shell saw %q, want the session's folder %q and its id %s", got, want, s.ID)
	}

	// A program of yours in the shell keeps Close All Finished away.
	c.keys(row, "sleep 600", "Enter")
	testutil.Eventually(t, 10*time.Second, "sleep did not take the terminal", func() bool {
		return c.format(row.Pane, "#{pane_current_command}") == "sleep"
	})
	var r ipc.CloseResult
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Finished: true}, &r); err == nil && slices.Contains(r.Sessions, s.ID) {
		t.Fatal("Close All Finished closed a card whose shell runs a program")
	}
	if _, ok := c.row(s.ID); !ok {
		t.Fatal("the card went")
	}
	c.keys(row, "C-c")
	testutil.Eventually(t, 10*time.Second, "the shell did not take the terminal back", func() bool {
		return c.format(row.Pane, "#{pane_current_command}") != "sleep"
	})

	// At its prompt it is finished, and its close ends it at SIGTERM to
	// its host, which hangs the shell up, without the kill that follows an
	// agent that outlives closeWait.
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Finished: true}, &r); err != nil || !slices.Contains(r.Sessions, s.ID) {
		t.Fatalf("Close All Finished closed %v (%v), want %s", r.Sessions, err, s.ID)
	}
	gone := func() bool { return c.pane(row.Pane) == nil }
	for deadline := time.Now().Add(15 * time.Second); !gone(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(filepath.Join(c.paths.State, "core.log"))
			panes, _ := c.TryTmux("list-panes", "-a", "-F", "#{pane_id} #{pane_pid} #{pane_dead} #{pane_current_command} #{@irie_role}")
			t.Fatalf("the shell's window outlived its close\npanes:\n%s\ncore.log:\n%s", panes, tail(string(log), 40))
		}
	}
	if log, _ := os.ReadFile(filepath.Join(c.paths.State, "core.log")); strings.Contains(string(log), "did not exit after SIGTERM") {
		t.Errorf("the shell needed a kill to close:\n%s", log)
	}
}

// TestASpawnThatFailsKeepsTheAgentsLastWordsAboveTheShell: a failed spawn's
// tail is the agent's screen, not the shell's prompt under it.
func TestASpawnThatFailsKeepsTheAgentsLastWordsAboveTheShell(t *testing.T) {
	c := setupShell(t)
	s := c.spawn("exit:1")
	row := c.waitState(s.ID, ipc.StateFailed)
	if n := len(row.Tail); n == 0 || row.Tail[n-1] != "fakeagent: exit 1" {
		t.Fatalf("tail = %q, want the agent's last line last", row.Tail)
	}
	testutil.Eventually(t, 10*time.Second, "the failed row does not say its terminal is a shell", func() bool {
		row, _ = c.row(s.ID)
		return row.Shell && strings.HasSuffix(row.Detail, " · now a shell")
	})
}

func TestTheTailOfAScreenEndsAtTheHostsLine(t *testing.T) {
	screen := strings.Join([]string{
		"old line", "error: no such model", "fakeagent: exit 1", "",
		"── claude exited with code 1" + host.ShellNoteEnd, "sh-3.2$ ls", "a.txt", "sh-3.2$ ",
	}, "\n")
	if got := screenTail(screen); !slices.Equal(got, []string{"old line", "error: no such model", "fakeagent: exit 1"}) {
		t.Fatalf("screenTail = %q", got)
	}
	// A dead pane's screen, with no shell, is as before.
	if got := screenTail("a\nb\n\nPane is dead (status 1, Mon Nov 23 10:00:00 2026)\n"); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("screenTail of a dead pane = %q", got)
	}
}
