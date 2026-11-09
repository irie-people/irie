package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
