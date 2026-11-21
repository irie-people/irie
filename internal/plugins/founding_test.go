//go:build !nofounding

package plugins

import (
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/founding"
	"github.com/irie-people/irie/internal/ipc"
)

func foundingStatus(t *testing.T, e Env) ipc.Plugin {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == "founding" {
			return p
		}
	}
	t.Fatal("no founding plugin")
	return ipc.Plugin{}
}

// TestFoundingIsOffByDefault: a new Irie shows no Charters tab and hands
// sessions no founding-documents skill until asked, and Why says which
// switch turns it on.
func TestFoundingIsOffByDefault(t *testing.T) {
	e := env(t)
	p := foundingStatus(t, e)
	if p.On || p.Title != "Charters" || len(p.Parts) != 1 {
		t.Fatalf("founding = %+v", p)
	}
	x := part(t, p, Founding)
	if x.State != ipc.PartPluginOff || x.Switch != "" {
		t.Errorf("part = %+v", x)
	}
	if x.Pane == nil || x.Pane.ID != "founding.founding" || x.Pane.Kind != PaneFounding || x.Pane.Title != "Charters" {
		t.Errorf("pane = %+v", x.Pane)
	}
	if why := Why(e.Config, Founding); !strings.Contains(why, "irie plugins on founding") {
		t.Errorf("Why = %q", why)
	}
	if left := SkillsLeft(e.Config)[founding.Skill]; !strings.Contains(left, "plugins.founding") {
		t.Errorf("skill left out because %q", left)
	}
	if ed, err := Edits("founding", "", true); err != nil || len(ed) != 1 || ed[0].Key != "plugins.founding" || ed[0].Value != true {
		t.Errorf("Edits = %+v, %v", ed, err)
	}
}

// TestFoundingOnIsReady: on, the texts read, and the skill goes to
// sessions.
func TestFoundingOnIsReady(t *testing.T) {
	e := env(t)
	e.Config.Plugins.Founding = true
	x := part(t, foundingStatus(t, e), Founding)
	if x.State != ipc.PartReady || !strings.Contains(x.Detail, "23 texts") {
		t.Errorf("part = %+v", x)
	}
	if why := Why(e.Config, Founding); why != "" {
		t.Errorf("Why = %q", why)
	}
	if left, out := SkillsLeft(e.Config)[founding.Skill]; out {
		t.Errorf("skill left out: %s", left)
	}
}
