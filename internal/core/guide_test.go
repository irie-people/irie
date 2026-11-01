package core

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/tmux"
)

// bareCore is a core with its own folders and no tmux, for what happens
// before a spawn reaches tmux.
func bareCore(t *testing.T, cfg config.Config) *Core {
	t.Helper()
	home := t.TempDir()
	p, err := paths.New(func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	return newCore(Options{Config: cfg, Paths: p, Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{Bin: filepath.Join(home, "no-tmux")})
}

// noPlugins is a claude whose --help lists no --plugin-dir.
func noPlugins(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\ncase \"$1\" in --version) echo '2.1.316 (Claude Code)';; --help) echo '  -n, --name <name>  Set a display name';; esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestTheGuideSaysWhyItIsOff(t *testing.T) {
	no := false
	old := noPlugins(t)
	for name, tc := range map[string]struct {
		cfg  func(*config.Config)
		want string
	}{
		"all skills off": {func(c *config.Config) { c.Skills.Enabled = false }, "skills are off (skills.enabled)"},
		"this one off":   {func(c *config.Config) { c.Skills.Disabled = []string{"dead-mans-switch"} }, "turned off (skills.disabled)"},
		"profile takes none": {func(c *config.Config) {
			p := c.Profiles["claude"]
			p.Skills = &no
			c.Profiles["claude"] = p
		}, "this profile takes no skills"},
		"no claude": {func(c *config.Config) { delete(c.Profiles, "claude") }, "+claude"},
		"an old claude": {func(c *config.Config) {
			p := c.Profiles["claude"]
			p.Command = old
			c.Profiles["claude"] = p
		}, "needs Claude Code to load Irie's skills"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			tc.cfg(&cfg)
			c := bareCore(t, cfg)
			_, err := c.tool(ipc.ToolArgs{Name: ipc.ToolSwitch})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one that says %q", err, tc.want)
			}
		})
	}
}

// TestTheSwitchGuideRunsIriesSkill: :setup switch starts a Claude Code
// session on +claude whose task runs the skill from Irie's plugin folder,
// which the agent finds, as Claude Code would; a reopen passes the folder
// again.
func TestTheSwitchGuideRunsIriesSkill(t *testing.T) {
	c := prepare(t, agentConfig{})
	c.WriteConfig(t, agentConfig{extra: fmt.Sprintf("[profiles.claude]\ncommand = %q\nenv = { IRIE_FAKE_PLUGINS = \"1\" }\n", c.fake)}.toml(c.fake))
	// The plugin folders are read-only; the test's folders go when it ends.
	t.Cleanup(func() {
		filepath.WalkDir(c.paths.Skills(), func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				os.Chmod(p, 0o700)
			}
			return nil
		})
	})
	c.up()

	var r ipc.InputResult
	if err := c.call(ipc.OpInput, ipc.InputArgs{Text: ":setup switch"}, &r); err != nil {
		t.Fatal(err)
	}
	if r.Did != ipc.InputRan || !r.Show || r.Session == nil || r.Text != "Started Dead Man's Switch." {
		t.Fatalf("input = %+v", r)
	}
	id := r.Session.ID
	// The folder a launch passed; first, also that the task is the skill.
	folder := func(first bool) string {
		t.Helper()
		spec, err := host.ReadSpec(c.paths.SpecFile(id))
		if err != nil {
			t.Fatal(err)
		}
		i := slices.Index(spec.Argv, "--plugin-dir")
		if spec.Profile != "claude" || i < 0 || first && spec.Argv[len(spec.Argv)-1] != "/irie:dead-mans-switch" {
			t.Fatalf("spec = %+v", spec)
		}
		return spec.Argv[i+1]
	}
	dir := folder(true)
	if _, err := os.Stat(filepath.Join(dir, "skills", "dead-mans-switch", "SKILL.md")); err != nil {
		t.Errorf("the skill is not in its folder: %v", err)
	}
	// The fake agent, like Claude Code, refuses a skill it cannot find.
	c.waitState(id, ipc.StateWorking)

	if err := c.call(ipc.OpClose, ipc.CloseArgs{Session: id}, nil); err != nil {
		t.Fatal(err)
	}
	var re ipc.ReopenResult
	if err := c.call(ipc.OpReopen, ipc.ReopenArgs{Session: id}, &re); err != nil {
		t.Fatal(err)
	}
	if again := folder(false); again != dir {
		t.Errorf("reopened with %s, want %s", again, dir)
	}
}
