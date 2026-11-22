package core

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
)

// TestOnSkill: a profile that starts on a skill hands its task to the
// skill's command; a task that already runs it, as one carried on from such
// a session does, stays as it is.
func TestOnSkill(t *testing.T) {
	chaos := config.Profile{Skill: "chaos"}
	for _, tc := range []struct {
		p          config.Profile
		task, want string
	}{
		{config.Profile{}, "break it", "break it"},
		{chaos, "break the refund flow", "/" + brand.Slug + ":chaos break the refund flow"},
		{chaos, "-v first, as a flag would", "/" + brand.Slug + ":chaos -v first, as a flag would"},
		{chaos, "/" + brand.Slug + ":chaos break it", "/" + brand.Slug + ":chaos break it"},
		{chaos, "/" + brand.Slug + ":chaos\nbreak it", "/" + brand.Slug + ":chaos\nbreak it"},
		{chaos, "/" + brand.Slug + ":chaos", "/" + brand.Slug + ":chaos"},
		{chaos, "/" + brand.Slug + ":chaosmonkey x", "/" + brand.Slug + ":chaos /" + brand.Slug + ":chaosmonkey x"},
	} {
		if got := onSkill(tc.p, tc.task); got != tc.want {
			t.Errorf("onSkill(%q, %q) = %q, want %q", tc.p.Skill, tc.task, got, tc.want)
		}
	}
}

// TestAChaosSessionSaysWhyItCannotStart: +chaos starts only where its skill
// can run, and says why not before anything is made.
func TestAChaosSessionSaysWhyItCannotStart(t *testing.T) {
	old := noPlugins(t)
	for name, tc := range map[string]struct {
		cfg  func(*config.Config)
		want string
	}{
		"all skills off": {func(c *config.Config) { c.Skills.Enabled = false }, "+chaos runs " + brand.Name + "'s skill chaos, which is off here: skills are off (skills.enabled)"},
		"this one off":   {func(c *config.Config) { c.Skills.Disabled = []string{"chaos"} }, "turned off (skills.disabled)"},
		"no such skill": {func(c *config.Config) {
			p := c.Profiles["chaos"]
			p.Skill = "nope"
			c.Profiles["chaos"] = p
		}, "has no skill called nope"},
		"an old claude": {func(c *config.Config) {
			p := c.Profiles["chaos"]
			p.Command = old
			c.Profiles["chaos"] = p
		}, "+chaos needs Claude Code to load " + brand.Name + "'s skills"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			tc.cfg(&cfg)
			c := bareCore(t, cfg)
			_, err := c.spawn(ipc.SpawnArgs{Task: "break the refund flow", Profile: "chaos"}, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one that says %q", err, tc.want)
			}
		})
	}
}

// TestAChaosSessionRunsTheSkill: +chaos, typed as any task is, starts a
// Claude Code session whose task runs /irie:chaos on what the person wrote,
// which names the session. Irie's plugin folder holds the skill and its
// references, and the fake agent, like Claude Code, finds the skill there.
func TestAChaosSessionRunsTheSkill(t *testing.T) {
	c := prepare(t, agentConfig{})
	c.WriteConfig(t, agentConfig{extra: fmt.Sprintf("[profiles.chaos]\ncommand = %q\nenv = { IRIE_FAKE_PLUGINS = \"1\" }\n", c.fake)}.toml(c.fake))
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
	if err := c.call(ipc.OpInput, ipc.InputArgs{Text: "+chaos break the refund flow"}, &r); err != nil {
		t.Fatal(err)
	}
	if r.Did != ipc.InputSpawned || r.Session == nil || r.Session.Name != "Break the Refund Flow" {
		t.Fatalf("input = %+v", r)
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(r.Session.ID))
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(spec.Argv, "--plugin-dir")
	if spec.Profile != "chaos" || i < 0 || spec.Argv[len(spec.Argv)-1] != "/"+brand.Slug+":chaos break the refund flow" || !slices.Contains(spec.Argv, "claude-opus-5-5") {
		t.Fatalf("spec = %+v", spec)
	}
	for _, f := range []string{"SKILL.md", "reference/faults.md", "reference/irie.md"} {
		if _, err := os.Stat(filepath.Join(spec.Argv[i+1], "skills", "chaos", f)); err != nil {
			t.Errorf("the skill's %s is not in its folder: %v", f, err)
		}
	}
	// The fake agent, like Claude Code, refuses a skill it cannot find.
	c.waitState(r.Session.ID, ipc.StateWorking)

	// The + list says what +chaos does.
	var cr ipc.CompleteResult
	if err := c.call("complete", ipc.CompleteArgs{Text: "+cha"}, &cr); err != nil || !slices.Equal(cr.Items, []string{"+chaos"}) || !strings.Contains(cr.Notes[0], "runs /"+brand.Slug+":chaos") {
		t.Errorf("complete +cha = %q %q, %v", cr.Items, cr.Notes, err)
	}
}
