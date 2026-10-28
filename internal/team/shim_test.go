package team

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/testutil"
	"github.com/irie-hq/irie/internal/tmux"
)

const sock = "/tmp/tmux-501/irie-teams"

func shim(t *testing.T, env ...string) Shim {
	t.Helper()
	p := filepath.Join(t.TempDir(), "team.env")
	if err := WriteEnv(p, env); err != nil {
		t.Fatal(err)
	}
	return Shim{Tmux: "/usr/bin/tmux", Socket: sock, Session: "s38bzqd9xtyvv", EnvFile: p, Now: func() time.Time { return time.Unix(0, 46) }}
}

var agentEnv = []string{"HOME=/home/alice", "TMUX=" + sock + ",123,0", "TMUX_PANE=%4"}

func noQuery(...string) (string, error) { panic("no query expected") }

func TestShimPassesTheAgentsOwnTmuxOn(t *testing.T) {
	s := shim(t)
	for _, args := range [][]string{
		{"ls"},
		{"new-session", "-d", "-s", "dev", "npm run dev"},
		{"-L", "other", "split-window"},
		{"-S", "/tmp/tmux-501/default", "split-window"},
		{"display-message", "-p", "-t", "dev:0", "#{pane_id}"},
		{"-c", "echo hi"},
		{},
	} {
		call, err := s.Route(args, agentEnv, noQuery)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(call.Args, args) || !slices.Equal(call.Env, []string{"HOME=/home/alice"}) {
			t.Errorf("%q went on as %q with %q", args, call.Args, call.Env)
		}
	}
}

func TestShimTakesClaudeCodesQuestionsToTheTeamServer(t *testing.T) {
	s := shim(t)
	for _, args := range [][]string{
		{"display-message", "-p", "-t", "%4", "#{client_termtype}\t#{allow-passthrough}"},
		{"show", "-gv", "focus-events"},
		{"show", "-Av", "mouse"},
	} {
		call, err := s.Route(args, agentEnv, noQuery)
		if err != nil {
			t.Fatal(err)
		}
		if want := append([]string{"-S", sock}, args...); !slices.Equal(call.Args, want) || !slices.Equal(call.Env, agentEnv) {
			t.Errorf("%q went on as %q with %q", args, call.Args, call.Env)
		}
	}
}

func TestShimGivesATeammateASessionOfItsOwn(t *testing.T) {
	s := shim(t, "HOME=/home/alice", "PATH=/home/alice/bin:/usr/bin")
	args := []string{"-S", sock, "split-window", "-d", "-t", "%4", "-h", "-l", "70%", "-e", "X=1", "-P", "-F", "#{pane_id}", "--", "cat"}
	call, err := s.Route(args, agentEnv, noQuery)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-S", sock, "new-session", "-d", "-s", "s38bzqd9xtyvv-1a", "-e", "HOME=/home/alice", "-e", "PATH=/home/alice/bin:/usr/bin", "-e", "X=1", "-P", "-F", "#{pane_id}", "--", "cat"}
	if !slices.Equal(call.Args, want) {
		t.Errorf("split went on as\n%q, want\n%q", call.Args, want)
	}
}

func TestShimListsTheTeamAsTheAnchorsWindow(t *testing.T) {
	s := shim(t)
	asked := ""
	query := func(q ...string) (string, error) {
		asked = strings.Join(q, " ")
		return "s38bzqd9xtyvv\n", nil
	}
	call, err := s.Route([]string{"-S", sock, "list-panes", "-t", "@2", "-F", "#{pane_id}"}, agentEnv, query)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-S", sock, "list-panes", "-a", "-f", "#{m:s38bzqd9xtyvv*,#{session_name}}", "-F", "#{pane_id}"}
	if !slices.Equal(call.Args, want) || asked != "display-message -p -t @2 #{session_name}" {
		t.Errorf("list went on as %q after asking %q", call.Args, asked)
	}
	// Another session's window is listed as it is.
	other := func(...string) (string, error) { return "s38bzqd9xtyvv-k1\n", nil }
	args := []string{"-S", sock, "list-panes", "-t", "@7"}
	if call, _ := s.Route(args, agentEnv, other); !slices.Equal(call.Args, args) {
		t.Errorf("a teammate's window listed as %q", call.Args)
	}
	if _, err := s.Route([]string{"-S", sock, "kill-server"}, agentEnv, noQuery); err == nil {
		t.Error("kill-server went on")
	}
}

// Claude Code 2.1.310's calls for two teammates and the end of the session,
// as a logging tmux saw them, run against a real team server.
func TestClaudeCodesTeammateCallsGiveEachASession(t *testing.T) {
	bin := testutil.NeedTmux(t)
	e := testutil.NewEnv(t)
	conf := filepath.Join(t.TempDir(), "tmux-teams.conf")
	if err := os.WriteFile(conf, []byte(tmux.TeamConf()), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &tmux.Runner{Bin: bin, Socket: e.Socket}
	id := "s38bzqd9xtyvv"
	out, err := r.StartWith(conf, "new-session", "-d", "-s", id, "-x", "80", "-y", "24", "-P", "-F", "#{pane_id} #{socket_path}", "--", "true")
	if err != nil {
		t.Fatal(err)
	}
	anchor, socket, _ := strings.Cut(strings.TrimSpace(out), " ")
	envFile := filepath.Join(t.TempDir(), "team.env")
	if err := WriteEnv(envFile, []string{"IRIE_TEST_TEAMMATE=yes"}); err != nil {
		t.Fatal(err)
	}
	s := Shim{Tmux: bin, Socket: socket, Session: id, EnvFile: envFile}
	env := append(tmuxFree(os.Environ()), "TMUX="+socket+",1,0", "TMUX_PANE="+anchor)
	call := func(args ...string) string {
		t.Helper()
		c, err := s.Route(args, env, func(q ...string) (string, error) {
			out, err := exec.Command(bin, append([]string{"-S", socket}, q...)...).Output()
			return string(out), err
		})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, c.Args...)
		cmd.Env = c.Env
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("tmux %q: %v", c.Args, err)
		}
		return strings.TrimSpace(string(out))
	}
	S := []string{"-S", socket}
	with := func(args ...string) []string { return append(slices.Clone(S), args...) }

	call("display-message", "-p", "-t", anchor, "#{client_termtype}\t#{allow-passthrough}\t#{focus-events}")
	win := call(with("display-message", "-t", anchor, "-p", "#{window_id}")...)
	if got := call(with("list-panes", "-t", win, "-F", "#{pane_id}")...); got != anchor {
		t.Fatalf("before any teammate the window lists %q", got)
	}
	spawn := func(name, color string, args ...string) string {
		t.Helper()
		p := call(with(append(append([]string{"split-window", "-d"}, args...), "-P", "-F", "#{pane_id}", "--", "cat")...)...)
		call(with("set-option", "-p", "-t", p, "pane-border-style", "fg="+color)...)
		call(with("select-pane", "-t", p, "-T", name)...)
		call(with("set-option", "-w", "-t", win, "pane-border-status", "top")...)
		call(with("select-layout", "-t", win, "main-vertical")...)
		call(with("resize-pane", "-t", anchor, "-x", "30%")...)
		call(with("set-option", "-p", "-t", p, "remain-on-exit", "failed")...)
		call(with("respawn-pane", "-k", "-t", p, "--", "env CLAUDECODE=1 sh -c 'echo $IRIE_TEST_TEAMMATE; sleep 60' --agent-name "+name+" --agent-color "+color)...)
		return p
	}
	first := spawn("probe", "blue", "-t", anchor, "-h", "-l", "70%")
	if got := call(with("list-panes", "-t", win, "-F", "#{pane_id}")...); got != anchor+"\n"+first {
		t.Errorf("with one teammate the window lists %q", got)
	}
	second := spawn("second", "green", "-t", first, "-v")

	var teams map[string][]Teammate
	testutil.Eventually(t, 5*time.Second, "both teammates listed", func() bool {
		out, err := r.Run("list-panes", "-a", "-F", Format)
		if err != nil {
			return false
		}
		teams = Parse(out)
		return len(teams[id]) == 2 && teams[id][0].Name == "probe"
	})
	if got := teams[id]; got[0].Pane != first || got[0].Color != "blue" || got[1].Pane != second || got[1].Name != "second" {
		t.Errorf("teammates = %+v", got)
	}
	for _, tm := range teams[id] {
		if n := run(t, r, "list-panes", "-t", "="+tm.Session, "-F", "#{pane_id}"); n != tm.Pane {
			t.Errorf("session %s holds %q, want only %s", tm.Session, n, tm.Pane)
		}
	}
	testutil.Eventually(t, 5*time.Second, "the teammate's environment", func() bool {
		return strings.Contains(run(t, r, "capture-pane", "-p", "-t", first), "yes")
	})
	if got := run(t, r, "list-panes", "-t", "="+id, "-F", "#{pane_id}"); got != anchor {
		t.Errorf("the anchor's window holds %q", got)
	}

	call(with("kill-pane", "-t", second)...)
	call(with("kill-pane", "-t", first)...)
	out, _ = r.Run("list-sessions", "-F", "#{session_name}")
	if strings.TrimSpace(out) != id {
		t.Errorf("after the teammates end the server has %q", out)
	}
}

func tmuxFree(env []string) []string { return without(env, "TMUX", "TMUX_PANE") }

func run(t *testing.T, r *tmux.Runner, args ...string) string {
	t.Helper()
	out, err := r.Run(args...)
	if err != nil {
		t.Fatalf("tmux %q: %v", args, err)
	}
	return strings.TrimSpace(out)
}
