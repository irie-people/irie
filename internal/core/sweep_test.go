package core

import (
	"encoding/json"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
)

// TestSweepsDisk: a search of the whole disk or the home folder has macOS
// ask about each private folder it reads; a narrower one, a shallow one or
// text that only names one runs.
func TestSweepsDisk(t *testing.T) {
	for cmd, want := range map[string]bool{
		"find / -name uv 2>/dev/null":                                true,
		"find ~ -iname 'personal*.md' -not -path '*/node_modules/*'": true,
		"find ~/ -maxdepth 4 -type d -name irie":                     true,
		`find "$HOME" -name '*.pdf'`:                                 true,
		"find /home/alice -name x":                                   true,
		"find -L / -name x":                                          true,
		"find ~/Library -maxdepth 3 -iname '*vpn*'":                  true,
		"find /Users /tmp -name x":                                   true,
		"sudo find / -name x":                                        true,
		"cd /tmp && find /Volumes -name '*.dmg'":                     true,
		"rg TODO ~":                                                  true,
		"rg -l connectOverCDP $HOME":                                 true,
		"rg -e foo /":                                                true,
		"rg --files ~":                                               true,
		"fd uv /":                                                    true,
		"du -sh ~":                                                   true,
		"du -d1 -x ~":                                                true,
		"du -sh ~/*":                                                 true,
		"tree ~":                                                     true,
		"grep -rn password /":                                        true,
		"ls -laR ~":                                                  true,
		`bash -c 'find / -name x'`:                                   true,
		`eval "find ~ -name x"`:                                      true,
		"echo found; find / -name x | head":                          true,
		"find . -name x":                                             false,
		"find ~/Workspaces/irie -name '*.go'":                        false,
		"find /home/alice/proj -name x":                              false,
		"find / -maxdepth 1 -type d":                                 false,
		"find ~ -maxdepth 1 -name '.*rc'":                            false,
		"find /tmp -name x":                                          false,
		"rg / src":                                                   false,
		"rg -d 1 foo ~":                                              false,
		"rg TODO":                                                    false,
		"fd x":                                                       false,
		"fd -d 1 x ~":                                                false,
		"du -sh ~/Library/Caches":                                    false,
		"tree -L 1 ~":                                                false,
		"grep -n '~' notes.md":                                       false,
		"grep foo /etc/hosts":                                        false,
		"ls ~":                                                       false,
		"ls -la /":                                                   false,
		"echo find / -name x":                                        false,
		`git commit -m "find / -name x"`:                             false,
		"cat > notes.md <<'EOF'\nfind / -name x\nEOF":                false,
		"mdfind -name uv":                                            false,
		"command -v find":                                            false,
	} {
		if got := sweepsDisk(cmd, "/home/alice"); got != want {
			t.Errorf("sweepsDisk(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// TestGabrielTurnsAwayASweep: the guard tells the agent where to look
// instead, unless the user watches the session, and lets a narrow search run.
func TestGabrielTurnsAwayASweep(t *testing.T) {
	t.Setenv("IRIE_GABRIEL", "")
	t.Setenv("HOME", "/home/alice")
	cfg := config.Default()
	cfg.Arch.Gabriel.On, cfg.Arch.Gabriel.Guard = true, true
	c := bareCore(t, cfg)
	started := []event.Event{{Kind: event.ToolStarted}}
	guard := func(cmd string) string {
		in, err := json.Marshal(map[string]string{"command": cmd})
		if err != nil {
			t.Fatal(err)
		}
		return c.focusGuard("s1", "Bash", in, started)
	}
	if got := guard("find ~ -name '*.pdf'"); got != sweepNote {
		t.Errorf("a sweep of the home folder: %q", got)
	}
	if got := guard("find /home/alice/proj -name '*.pdf'"); got != "" {
		t.Errorf("a narrow search: %q", got)
	}
	if got := guard("open -g report.pdf"); got != focusNote {
		t.Errorf("a window behind: %q", got)
	}
}
