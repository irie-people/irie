package host

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/testutil"
)

// inherited is what an agent pane holds when another Claude Code session,
// inside a terminal pane, started the tmux server.
var inherited = []string{
	"PATH=/usr/bin:/bin",
	"HOME=/home/alice",
	"SSH_AUTH_SOCK=/tmp/ssh-alice/agent.1",
	"CLAUDE_CODE_USE_BEDROCK=1",
	"CLAUDE_CODE_FORCE_SESSION_PERSISTENCE=1",
	"CLAUDECODE=1",
	"CLAUDE_CODE_CHILD_SESSION=1",
	"CLAUDE_CODE_SESSION_ID=6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f",
	"CLAUDE_PID=4242",
	"CLAUDE_CODE_MESSAGING_SOCKET=/tmp/cc-socks/4242.sock",
	"CLAUDE_CODE_MESSAGING_TOKEN=secret",
	"CLAUDE_CODE_BRIDGE_SESSION_ID=session_01abc",
	"CLAUDE_CODE_REMOTE_SESSION_ID=session_01def",
	"CLAUDE_EFFORT=max",
	"ACMETERM_PANEL_ID=panel-7",
	"ACMETERM_SURFACE_ID=surface-3",
	"ACMETERM_SOCKET_CAPABILITY=token",
	"ACMETERM_WORKSPACE_ID=workspace-2",
	"ACMETERM_TAB_ID=tab-5",
	"ACMETERM_SOCKET_PATH=/tmp/acmeterm.sock",
	"ACMETERM_THEME=dark",
	"OTHERTERM_WORKSPACE_ID=kept",
	"WINDOWID=88080391",
}

func names(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestWithoutIdentityDropsAnotherSessionsAndTerminalsMarks(t *testing.T) {
	// The terminal's other ids and its socket go with its pane's, while its
	// settings and another terminal's ids stay.
	want := []string{"ACMETERM_THEME", "CLAUDE_CODE_FORCE_SESSION_PERSISTENCE", "CLAUDE_CODE_USE_BEDROCK", "HOME", "OTHERTERM_WORKSPACE_ID", "PATH", "SSH_AUTH_SOCK"}
	if got := names(WithoutIdentity(inherited)); !slices.Equal(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}
}

func TestTheAgentsEnvironmentLeavesTheMarksOut(t *testing.T) {
	env := environ(inherited, map[string]string{"IRIE_SESSION": "s0k3j9x2m4a7q"}, []string{"TMUX"}, "/home/alice/code")
	for _, n := range names(env) {
		if foreign(n) || strings.HasPrefix(n, "ACMETERM_") && n != "ACMETERM_THEME" {
			t.Errorf("the agent inherits %s", n)
		}
	}
	for _, kv := range []string{"SSH_AUTH_SOCK=/tmp/ssh-alice/agent.1", "CLAUDE_CODE_USE_BEDROCK=1", "IRIE_SESSION=s0k3j9x2m4a7q", "PWD=/home/alice/code"} {
		if !slices.Contains(env, kv) {
			t.Errorf("env lacks %s: %v", kv, env)
		}
	}
	// A value the spec sets on purpose, from a profile's env, still reaches
	// the agent.
	env = environ(inherited, map[string]string{"CLAUDECODE": "1"}, nil, "")
	if !slices.Contains(env, "CLAUDECODE=1") {
		t.Errorf("a profile's own CLAUDECODE was dropped: %v", names(env))
	}
}

func TestTheHostDropsInheritedMarks(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("ACMETERM_PANEL_ID", "panel-7")
	t.Setenv("IRIE_TEST_KEEP", "kept")
	// The private server is started from this environment, so its panes,
	// and the host's agent, inherit all three.
	script := `printf '%s|%s|%s\n' "${CLAUDECODE-none}" "${ACMETERM_PANEL_ID-none}" "$IRIE_TEST_KEEP"; sleep 600`
	h := startHost(t, "/bin/sh", "-c", script)
	testutil.Eventually(t, 5*time.Second, "the agent did not print its env", func() bool {
		return strings.Contains(h.Tmux(t, "capture-pane", "-p", "-t", "h:0.0"), "none|none|kept")
	})
}
