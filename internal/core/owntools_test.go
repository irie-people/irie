package core

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
)

// Skills and tools of your own (DESIGN §4.17): what a plugin of yours hands
// agents' sessions while it is on.

// ownPlugin writes a manifest named name.toml into dir, a plugins folder.
func ownPlugin(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestYourToolsGoToSessions: tools of yours go to a session's servers while
// their plugin is on, after Irie's own, which keep their names.
func TestYourToolsGoToSessions(t *testing.T) {
	cfg := config.Default()
	c := bareCore(t, cfg)
	server := filepath.Join(t.TempDir(), "grants-app")
	if err := os.WriteFile(server, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ownPlugin(t, c.paths.Plugins(), "grants-app", `
[tools.grants]
run = ["`+server+`", "mcp", "--db", "/home/alice/grants.db"]

[tools.variorum]
run = ["`+server+`"]

[tools.gone]
run = ["/home/alice/no-such-server"]
`)
	x := &session{id: "s1"}
	if mcp := c.ownToolsMCP(x, nil); mcp != nil {
		t.Errorf("its plugin off: %+v", mcp)
	}

	cfg.Plugins.Manifests["grants-app"] = true
	c.useConfig(cfg)
	irie := harness.MCPServer{Command: "/home/alice/.local/share/irie/bin/irie", Args: []string{"variorum", "door"}}
	mcp := c.ownToolsMCP(x, map[string]harness.MCPServer{"variorum": irie})
	if g := mcp["grants"]; len(mcp) != 2 || g.Command != server || !slices.Equal(g.Args, []string{"mcp", "--db", "/home/alice/grants.db"}) {
		t.Errorf("on: %+v", mcp)
	}
	if v := mcp["variorum"]; v.Command != irie.Command {
		t.Errorf("Irie's own server lost its name to yours: %+v", v)
	}
	// With no server of Irie's by that name, the name is yours; a server
	// whose program is gone stays out. Args is a list, even an empty one.
	mcp = c.ownToolsMCP(x, nil)
	if v, ok := mcp["variorum"]; len(mcp) != 2 || !ok || v.Command != server || v.Args == nil || len(v.Args) != 0 {
		t.Errorf("no server of Irie's: %+v", mcp)
	}
}

// TestYourSkillGoesToSessions: a skill of yours goes into the sealed folder
// of skills a session gets while its plugin is on, whole, and a change to
// it reaches only the sessions that start after.
func TestYourSkillGoesToSessions(t *testing.T) {
	cfg := config.Default()
	c := bareCore(t, cfg)
	t.Cleanup(func() {
		filepath.WalkDir(c.paths.Skills(), func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				os.Chmod(p, 0o700)
			}
			return nil
		})
	})
	skill := filepath.Join(t.TempDir(), "grants")
	if err := os.MkdirAll(filepath.Join(skill, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(skill, p), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("SKILL.md", "---\nname: grants\ndescription: Find and track grants.\n---\n\n# Grants\n")
	write("references/stages.md", "# Stages\n")
	ownPlugin(t, c.paths.Plugins(), "grants-app", "[skills.grants]\npath = \""+skill+"\"\n")

	x := &session{id: "s1"}
	off := c.skillsFolder(config.Profile{}, x)
	if off == "" {
		t.Fatalf("no folder of Irie's own skills: %v", x.warnings)
	}
	if _, err := os.Stat(filepath.Join(off, "skills", "grants")); !os.IsNotExist(err) {
		t.Errorf("its plugin off, the skill is in the session's folder: %v", err)
	}

	cfg.Plugins.Manifests["grants-app"] = true
	c.useConfig(cfg)
	on := c.skillsFolder(config.Profile{}, x)
	for _, f := range []string{"SKILL.md", "references/stages.md"} {
		if _, err := os.Stat(filepath.Join(on, "skills", "grants", f)); err != nil {
			t.Errorf("its %s: %v", f, err)
		}
	}
	src, err := os.ReadFile(filepath.Join(on, "SOURCES.md"))
	if err != nil || !strings.Contains(string(src), "| grants | your plugin grants-app, from "+skill+" |\n") {
		t.Errorf("SOURCES.md = %s, %v", src, err)
	}
	if _, err := os.Stat(filepath.Join(on, "skills", "present", "SKILL.md")); err != nil {
		t.Errorf("Irie's own skills went: %v", err)
	}

	write("SKILL.md", "---\nname: grants\ndescription: Find, track and report on grants.\n---\n\n# Grants\n")
	later := c.skillsFolder(config.Profile{}, x)
	if later == on {
		t.Fatal("a changed skill came in the same folder")
	}
	if b, err := os.ReadFile(filepath.Join(on, "skills", "grants", "SKILL.md")); err != nil || strings.Contains(string(b), "report") {
		t.Errorf("a session's folder changed under it: %s, %v", b, err)
	}
	// A profile that takes no skills gets no folder.
	no := false
	if dir := c.skillsFolder(config.Profile{Skills: &no}, x); dir != "" {
		t.Errorf("a profile with skills = false: %s", dir)
	}
	if len(x.warnings) > 0 {
		t.Errorf("warnings: %v", x.warnings)
	}
}

// TestASessionStartsWithYourSkillAndTools: a session started while a
// plugin of yours is on gets its skill in its folder of skills and its
// tools among its servers, as its agent's command line names them.
func TestASessionStartsWithYourSkillAndTools(t *testing.T) {
	c := prepare(t, agentConfig{})
	server := filepath.Join(t.TempDir(), "grants-app")
	if err := os.WriteFile(server, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(t.TempDir(), "grants")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: grants\ndescription: Find and track grants.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.WriteConfig(t, agentConfig{extra: fmt.Sprintf("[profiles.claude]\ncommand = %q\nenv = { IRIE_FAKE_PLUGINS = \"1\", IRIE_FAKE_MCP = \"1\" }\n\n[plugins.manifests]\ngrants-app = true\n", c.fake)}.toml(c.fake))
	ownPlugin(t, c.paths.Plugins(), "grants-app", "[skills.grants]\npath = \""+skill+"\"\n\n[tools.grants]\nrun = [\""+server+"\", \"mcp\"]\n\n[words.grants]\ntitle = \"Grant work\"\nskill = \"grants\"\n")
	t.Cleanup(func() {
		filepath.WalkDir(c.paths.Skills(), func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				os.Chmod(p, 0o700)
			}
			return nil
		})
	})
	c.up()

	var sr ipc.SpawnResult
	if err := c.call(ipc.OpSpawn, ipc.SpawnArgs{Task: "find grants for the youth arts program", Profile: "claude"}, &sr); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(c.paths.Session(sr.Session.ID), harness.MCPFile))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Servers map[string]harness.MCPServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if g := f.Servers["grants"]; g.Command != server || !slices.Equal(g.Args, []string{"mcp"}) {
		t.Errorf("the session's servers: %s", b)
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(sr.Session.ID))
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(spec.Argv, "--plugin-dir")
	if i < 0 {
		t.Fatalf("no --plugin-dir: %q", spec.Argv)
	}
	if _, err := os.Stat(filepath.Join(spec.Argv[i+1], "skills", "grants", "SKILL.md")); err != nil {
		t.Errorf("the skill isn't in the session's folder: %v", err)
	}
	c.waitState(sr.Session.ID, ipc.StateWorking)

	// The plugin's word starts a session on its skill: with a task, which
	// names it, and alone, named as the word is.
	for _, w := range []struct{ text, name, task string }{
		{":grants find arts grants", "Find Arts Grants", "/" + brand.Slug + ":grants find arts grants"},
		{":grants", "Grant work", "/" + brand.Slug + ":grants"},
	} {
		var r ipc.InputResult
		// The menus' agent, one whose Claude Code loads plugins.
		if err := c.call(ipc.OpInput, ipc.InputArgs{Text: w.text, Profile: "claude"}, &r); err != nil {
			t.Fatalf("%s: %v", w.text, err)
		}
		if r.Did != ipc.InputSpawned || r.Session == nil || r.Session.Name != w.name {
			t.Fatalf("%s: %+v", w.text, r)
		}
		spec, err := host.ReadSpec(c.paths.SpecFile(r.Session.ID))
		if err != nil {
			t.Fatal(err)
		}
		if got := spec.Argv[len(spec.Argv)-1]; got != w.task {
			t.Errorf("%s: the agent's task is %q, want %q", w.text, got, w.task)
		}
		// The fake agent, like Claude Code, refuses a skill it can't find.
		c.waitState(r.Session.ID, ipc.StateWorking)
	}
}
