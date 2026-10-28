package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/tmux"
)

// teamAgent stands in for Claude Code with agent teams: it starts one
// teammate with the calls Claude Code 2.1.310 makes, through the tmux it
// finds on its PATH, and stays.
const teamAgent = `#!/bin/sh
sock=${TMUX%%,*}
p=$(tmux -S "$sock" split-window -d -t "$TMUX_PANE" -h -l 70% -P -F '#{pane_id}' -- cat)
tmux -S "$sock" set-option -p -t "$p" remain-on-exit failed
tmux -S "$sock" respawn-pane -k -t "$p" -- "sh -c 'echo mate=\${IRIE_SESSION:-none} teams=\$CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS of=\$IRIE_TEAM_OF; exec sleep 600' sh --agent-name probe --agent-color blue"
echo "lead started $p"
exec sleep 600
`

func TestTeammatesRunOnTheTeamServer(t *testing.T) {
	agent := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(agent, []byte(teamAgent), 0o755); err != nil {
		t.Fatal(err)
	}
	c := setupWith(t, agentConfig{command: agent, spawn: "teammates = \"panes\"\n"})
	s := c.spawn("start a team")

	var row ipc.Session
	testutil.Eventually(t, 10*time.Second, "the teammate on the session's row", func() bool {
		for _, r := range c.list() {
			if r.ID == s.ID && len(r.Team) == 1 {
				row = r
				return true
			}
		}
		return false
	})
	mate := row.Team[0]
	if mate.Name != "probe" || mate.Color != "blue" || mate.Dead || !strings.HasPrefix(mate.Session, s.ID+"-") {
		t.Errorf("teammate = %+v", mate)
	}

	var info ipc.AttachInfoResult
	if err := c.call("attach-info", nil, &info); err != nil {
		t.Fatal(err)
	}
	if info.View != tmux.ViewSession || info.Teams == "" {
		t.Fatalf("attach-info = %+v", info)
	}
	teams := func(args ...string) string {
		out, _ := exec.Command(testutil.NeedTmux(t), append([]string{"-S", info.Teams}, args...)...).Output()
		return strings.TrimSpace(string(out))
	}
	// Its own session, its own environment: the agent's, without the
	// session's id, so its Irie hooks never speak for the session; they
	// only wait while it is frozen.
	want := "mate=none teams=1 of=" + s.ID
	screen := func() string { return teams("capture-pane", "-p", "-t", "="+mate.Session+":") }
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(screen(), want); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the teammate's screen:\n%s\npanes:\n%s", screen(), teams("list-panes", "-a", "-F", "#{session_name} #{pane_id} #{pane_dead} #{pane_start_command}"))
		}
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(spec.Env["TMUX"], info.Teams+",") || spec.Env["TMUX_PANE"] == "" || len(spec.Path) != 1 {
		t.Errorf("the agent's spec: env %v, path %v", spec.Env, spec.Path)
	}
	settings, _ := os.ReadFile(filepath.Join(c.paths.Session(s.ID), "claude-settings.json"))
	if !strings.Contains(string(settings), `"teammateMode": "tmux"`) {
		t.Errorf("settings file:\n%s", settings)
	}

	// The team ends with its session.
	if err := c.call("close", ipc.CloseArgs{Session: s.ID}, nil); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 10*time.Second, "the team ended with its session", func() bool {
		return !strings.Contains(teams("list-sessions", "-F", "#{session_name}"), s.ID)
	})
}

func TestSessionsStartWithoutTeamsByDefault(t *testing.T) {
	c := setup(t)
	s := c.spawn("no team")
	spec, err := host.ReadSpec(c.paths.SpecFile(s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Env["TMUX"]; ok || len(spec.Path) != 0 {
		t.Errorf("spec: env %v, path %v", spec.Env, spec.Path)
	}
	if _, err := os.Stat(filepath.Join(c.paths.Session(s.ID), "bin", "tmux")); err == nil {
		t.Error("a session without teams got the shim")
	}
}

func TestSplitShowsTheSessionInTheView(t *testing.T) {
	c := setup(t)
	app, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, err := app.Subscribe(ipc.SubscribeArgs{View: ipc.ViewApp}); err != nil {
		t.Fatal(err)
	}
	lead, other := c.spawn("the lead"), c.spawn("another")
	c.show(other.ID, false)
	before := c.pane(other.Pane)

	if err := c.call("show", ipc.ShowArgs{Session: lead.ID, Split: true}, nil); err != nil {
		t.Fatal(err)
	}
	inView := func() bool {
		for _, p := range c.panes() {
			if p["session_name"] == tmux.ViewSession && p["pane_id"] == lead.Pane {
				return true
			}
		}
		return false
	}
	testutil.Eventually(t, 3*time.Second, "the lead in the view session", inView)
	if s := c.slot(); s["@irie_role"] != tmux.RolePlaceholder {
		t.Errorf("during the split the slot holds %v", s)
	}
	if got := c.Tmux(t, "display-message", "-p", "-t", lead.Pane, "#{window-size}"); got != "latest" {
		t.Errorf("the lead's window-size is %q", got)
	}
	shown := map[string]bool{}
	for _, r := range c.list() {
		shown[r.ID] = r.Shown
	}
	if !shown[lead.ID] || shown[other.ID] {
		t.Errorf("shown = %v", shown)
	}

	c.show(other.ID, false)
	testutil.Eventually(t, 3*time.Second, "the view session gone", func() bool { return !inView() })
	if s := c.slot(); s["pane_id"] != other.Pane {
		t.Errorf("after the split the slot holds %v", s)
	}
	if p := c.pane(lead.Pane); p["pane_width"] != before["pane_width"] || p["pane_height"] != before["pane_height"] {
		t.Errorf("after the split the lead is %sx%s, want %sx%s", p["pane_width"], p["pane_height"], before["pane_width"], before["pane_height"])
	}
}
