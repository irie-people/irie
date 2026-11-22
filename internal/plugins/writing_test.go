//go:build !nowriting

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/variorum"
)

func writingStatus(t *testing.T, e Env) ipc.Plugin {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == "writing" {
			return p
		}
	}
	t.Fatal("no writing plugin")
	return ipc.Plugin{}
}

// TestWritingNeedsVariorumUntilItIsInstalled: the writing plugin is on by
// default, and its part says where Variorum comes from until the app is in
// an Applications folder. Off with its plugin, its skill leaves every
// session; its own switch is its skill's.
func TestWritingNeedsVariorumUntilItIsInstalled(t *testing.T) {
	folder := applications(t)
	app := filepath.Join(folder, "Variorum.app")
	e := env(t)

	p := writingStatus(t, e)
	if !p.On || p.Title != "Writing" || len(p.Parts) != 2 {
		t.Fatalf("writing = %+v", p)
	}
	if x := part(t, p, Variorum); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, variorum.Site) || x.Setup != ":write" {
		t.Errorf("without Variorum: %+v", x)
	}
	if err := os.Mkdir(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if x := part(t, writingStatus(t, e), Variorum); x.State != ipc.PartReady || !strings.Contains(x.Detail, ":write") {
		t.Errorf("with Variorum: %+v", x)
	}

	e.Config.Plugins.Writing = false
	if x := part(t, writingStatus(t, e), Variorum); x.State != ipc.PartPluginOff {
		t.Errorf("plugin off: %+v", x)
	}
	if why := SkillsLeft(e.Config)[variorumSkill]; !strings.Contains(why, "plugins.writing") {
		t.Errorf("skill left out because %q", why)
	}
	if why := Why(e.Config, Variorum); !strings.Contains(why, brand.Slug+" plugins on writing") {
		t.Errorf("Why = %q", why)
	}

	e.Config.Plugins.Writing = true
	e.Config.Skills.Disabled = []string{variorumSkill}
	if x := part(t, writingStatus(t, e), Variorum); x.State != ipc.PartOff {
		t.Errorf("skill disabled: %+v", x)
	}
	if why := Why(e.Config, Variorum); !strings.Contains(why, "skills.disabled") {
		t.Errorf("Why = %q", why)
	}
	if _, err := Edits("writing", Variorum, true); err == nil || !strings.Contains(err.Error(), "no switch of its own") {
		t.Errorf("Edits = %v", err)
	}
}

// TestABundlesPartWithNoSwitchStaysWithItsPlugin: a part with no switch of
// its own goes on with its plugin, and a bundle can't take it off alone.
func TestABundlesPartWithNoSwitchStaysWithItsPlugin(t *testing.T) {
	e := env(t)
	e.Config.Plugins.Writing = false
	manifest(t, e, "words.toml", "plugins = [\"writing.variorum\"]\n")
	dir := e.Paths.Plugins()
	on, err := BundleEdits(dir, "words", true)
	if err != nil || len(on) != 1 || on[0] != (config.Edit{Key: "plugins.writing", Value: true}) {
		t.Fatalf("on = %v, %v", on, err)
	}
	if _, err := BundleEdits(dir, "words", false); err == nil || !strings.Contains(err.Error(), "nothing here to turn off") {
		t.Errorf("off = %v", err)
	}
	e.Config.Plugins.Writing = true
	b := bundle(t, e, "words")
	if b.State != ipc.BundleOn || !strings.Contains(b.Plugins[0].Detail, "no switch of its own: it is on while the Writing plugin is") {
		t.Errorf("bundle = %+v", b)
	}
}

// applications points variorum.Folders, and ~/.claude.json, at folders of
// the test's own, with nothing in them.
func applications(t *testing.T) string {
	t.Helper()
	folder := t.TempDir()
	oldFolders, oldClaude := variorum.Folders, claudeJSON
	t.Cleanup(func() { variorum.Folders, claudeJSON = oldFolders, oldClaude })
	variorum.Folders = []string{folder}
	claudeJSON = filepath.Join(t.TempDir(), ".claude.json")
	return folder
}

// fakeVariorum makes Variorum.app in folder, its door a script, and
// returns the door.
func fakeVariorum(t *testing.T, folder string) string {
	t.Helper()
	contents := filepath.Join(folder, "Variorum.app", "Contents")
	if err := os.MkdirAll(filepath.Join(contents, "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>io.lduarte.Variorum</string></dict></plist>
`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	door := filepath.Join(contents, "MacOS", variorum.Program)
	if err := os.WriteFile(door, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return door
}

// TestTheVariorumTabNeedsBothSetups: the tab's part has a switch of its
// own, on by default, has no words to set it up, and is ready once Variorum is
// installed and both setups are kept; its detail says what is missing
// and what fixes it, and warns while Claude Code adds Variorum's door to
// every session. Its check reads files alone.
func TestTheVariorumTabNeedsBothSetups(t *testing.T) {
	folder := applications(t)
	e := env(t)
	tab := func() ipc.PluginPart { return part(t, writingStatus(t, e), VariorumTab) }

	x := tab()
	if x.State != ipc.PartNeeds || !x.On || x.Switch != "variorum.tab" || x.Setup != "" || x.Title != "Variorum tab" || !strings.Contains(x.Detail, variorum.Site) || len(x.Brings) != 1 {
		t.Errorf("without Variorum: %+v", x)
	}
	door := fakeVariorum(t, folder)
	if x := tab(); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "none of Variorum's setups") || !strings.Contains(x.Detail, "variorum connect") {
		t.Errorf("no setups: %+v", x)
	}
	if err := variorum.Save(e.Paths.Data, variorum.Sessions, variorum.Setup{Command: door, Key: "vmk2_claude-code_k"}); err != nil {
		t.Fatal(err)
	}
	if x := tab(); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "not Any MCP client's") {
		t.Errorf("Claude Code's alone: %+v", x)
	}
	if err := variorum.Save(e.Paths.Data, variorum.Tab, variorum.Setup{Command: door, Key: "vmk1_k"}); err != nil {
		t.Fatal(err)
	}
	if x := tab(); x.State != ipc.PartReady || strings.Contains(x.Detail, "claude mcp remove") || strings.Contains(x.Detail, "vmk") {
		t.Errorf("both: %+v", x)
	}

	if err := os.WriteFile(claudeJSON, []byte(`{"numStartups": 3, "mcpServers": {"variorum": {"type": "stdio", "command": "`+door+`"}}, "projects": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if x := tab(); x.State != ipc.PartReady || !strings.Contains(x.Detail, "Remove it with `claude mcp remove -s user variorum`.") || !strings.Contains(x.Detail, "16 places") || !strings.Contains(x.Detail, "shut each other out") {
		t.Errorf("set up for every folder: %+v", x)
	}
	if err := os.WriteFile(claudeJSON, []byte(`{"projects": {"/home/alice/book": {"mcpServers": {"variorum": {}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if x := tab(); strings.Contains(x.Detail, "claude mcp remove") {
		t.Errorf("set up for one folder: %+v", x)
	}

	e.Config.Plugins.Writing = false
	if x := tab(); x.State != ipc.PartPluginOff {
		t.Errorf("plugin off: %+v", x)
	}
	if why := Why(e.Config, VariorumTab); !strings.Contains(why, brand.Slug+" plugins on writing") {
		t.Errorf("Why = %q", why)
	}
	e.Config.Plugins.Writing = true
	if why := Why(e.Config, VariorumTab); why != "" {
		t.Errorf("Why with the plugin on = %q", why)
	}
	if es, err := Edits("writing", VariorumTab, false); err != nil || len(es) != 1 || es[0] != (config.Edit{Key: "variorum.tab", Value: false}) {
		t.Errorf("Edits = %v, %v", es, err)
	}

	// Its switch is the tab's alone: :write sessions keep the door.
	e.Config.Variorum.Tab = false
	if x := tab(); x.State != ipc.PartOff || x.On || !strings.Contains(x.Detail, ":write sessions still get Variorum's door") {
		t.Errorf("tab off: %+v", x)
	}
	if why := Why(e.Config, VariorumTab); !strings.Contains(why, brand.Slug+" plugins on writing tab") {
		t.Errorf("Why with the tab off = %q", why)
	}
	if why := Why(e.Config, Variorum); why != "" {
		t.Errorf("writing in Variorum with the tab off: %q", why)
	}
}

// TestABundleTurnsTheVariorumTabOff: the tab's own switch lets a bundle
// take it away again, and bring it back with its plugin.
func TestABundleTurnsTheVariorumTabOff(t *testing.T) {
	e := env(t)
	manifest(t, e, "desk.toml", "plugins = [\"writing.tab\"]\n")
	dir := e.Paths.Plugins()
	off, err := BundleEdits(dir, "desk", false)
	if err != nil || len(off) != 1 || off[0] != (config.Edit{Key: "variorum.tab", Value: false}) {
		t.Fatalf("off = %v, %v", off, err)
	}
	on, err := BundleEdits(dir, "desk", true)
	if err != nil || len(on) != 2 || on[0] != (config.Edit{Key: "plugins.writing", Value: true}) || on[1] != (config.Edit{Key: "variorum.tab", Value: true}) {
		t.Fatalf("on = %v, %v", on, err)
	}
	e.Config.Variorum.Tab = false
	if b := bundle(t, e, "desk"); b.State != ipc.BundleOff || b.Plugins[0].On || strings.Contains(b.Plugins[0].Detail, "no switch of its own") {
		t.Errorf("the tab off: %+v", b)
	}
}
