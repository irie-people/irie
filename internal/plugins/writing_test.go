//go:build !nowriting

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
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
	old := variorumApps
	t.Cleanup(func() { variorumApps = old })
	app := filepath.Join(t.TempDir(), "Variorum.app")
	variorumApps = []string{app}
	e := env(t)

	p := writingStatus(t, e)
	if !p.On || p.Title != "Writing" || len(p.Parts) != 1 {
		t.Fatalf("writing = %+v", p)
	}
	if x := part(t, p, Variorum); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, variorumSite) || x.Setup != ":write" {
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
	if why := Why(e.Config, Variorum); !strings.Contains(why, "irie plugins on writing") {
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
