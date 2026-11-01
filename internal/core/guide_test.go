package core

import (
	"fmt"
	"io"
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

func TestTheGuideSaysWhyItIsOff(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  func(*config.Config)
		env  string
		want string
	}{
		"all skills off":   {cfg: func(c *config.Config) { c.Skills.Enabled = false }, want: "[skills] enabled = false"},
		"this one off":     {cfg: func(c *config.Config) { c.Skills.Disabled = []string{"dead-mans-switch"} }, want: "[skills] disabled names it"},
		"off where it ran": {cfg: func(*config.Config) {}, env: "off", want: "IRIE_SKILLS=off"},
		"no claude":        {cfg: func(c *config.Config) { delete(c.Profiles, "claude") }, want: "+claude"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("IRIE_SKILLS", tc.env)
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

func TestPruneKeepsTheFoldersSessionsUse(t *testing.T) {
	c := bareCore(t, config.Default())
	used, err := c.pluginDir()
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(c.paths.SkillPlugins(), "0000000000000000")
	if err := os.MkdirAll(filepath.Join(stale, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "s0000000000a1"
	if err := os.MkdirAll(c.paths.Session(id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := host.WriteSpec(c.paths.SpecFile(id), host.Spec{Session: id, Argv: []string{"claude", "--plugin-dir", used, "/irie:dead-mans-switch"}, Cwd: "/", Skills: true}); err != nil {
		t.Fatal(err)
	}
	c.sessions[id] = &session{id: id}
	c.pruneSkills()
	if _, err := os.Stat(used); err != nil {
		t.Errorf("the folder a session uses is gone: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a folder no session uses is still there: %v", err)
	}
}

// TestTheSwitchGuideRunsIriesSkill: :setup switch starts a Claude Code
// session on +claude whose task runs the skill from Irie's plugin folder,
// which the agent finds, and a reopen hands it the folder again.
func TestTheSwitchGuideRunsIriesSkill(t *testing.T) {
	c := prepare(t, agentConfig{})
	c.WriteConfig(t, agentConfig{extra: fmt.Sprintf("[profiles.claude]\ncommand = %q\n", c.fake)}.toml(c.fake))
	c.up()

	var r ipc.InputResult
	if err := c.call(ipc.OpInput, ipc.InputArgs{Text: ":setup switch"}, &r); err != nil {
		t.Fatal(err)
	}
	if r.Did != ipc.InputRan || !r.Show || r.Session == nil || r.Text != "Started Dead Man's Switch." {
		t.Fatalf("input = %+v", r)
	}
	id := r.Session.ID
	spec, err := host.ReadSpec(c.paths.SpecFile(id))
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(spec.Argv, "--plugin-dir")
	if spec.Profile != "claude" || !spec.Skills || i < 0 || spec.Argv[len(spec.Argv)-1] != "/irie:dead-mans-switch" {
		t.Fatalf("spec = %+v", spec)
	}
	dir := spec.Argv[i+1]
	if filepath.Dir(dir) != c.paths.SkillPlugins() {
		t.Errorf("plugin folder %s, want one under %s", dir, c.paths.SkillPlugins())
	}
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
	spec, err = host.ReadSpec(c.paths.SpecFile(id))
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.Index(spec.Argv, "--plugin-dir"); i < 0 || spec.Argv[i+1] != dir || !spec.Skills {
		t.Errorf("reopened spec = %+v, want --plugin-dir %s", spec, dir)
	}
}
