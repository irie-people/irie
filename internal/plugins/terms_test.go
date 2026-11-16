//go:build !noterms

package plugins

import (
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/terms"
)

// TestTermsInShortHasATabOfItsOwn: on by default, Terms in short brings a
// web pane, the Terms tab, at the page the core serves; it is ready once
// something answers there, and says what holds the port until then; off,
// its words say how to turn it on.
func TestTermsInShortHasATabOfItsOwn(t *testing.T) {
	e := env(t)
	answers := false
	old := termsServed
	termsServed = func(addr string) bool { return answers && addr == "127.0.0.1:7393" }
	t.Cleanup(func() { termsServed = old })
	var p ipc.Plugin
	for _, x := range Status(e) {
		if x.Name == "terms" {
			p = x
		}
	}
	if !p.On || p.Title != "Terms in short" || len(p.Parts) != 1 {
		t.Fatalf("terms = %+v", p)
	}
	x := part(t, p, Terms)
	if x.Pane == nil || x.Pane.Kind != ipc.PaneWeb || x.Pane.Address != terms.PageAddress || x.Pane.Origin != "http://127.0.0.1:7393" || x.Pane.ID != "terms.terms" || x.Pane.Store != "terms" || x.Pane.Title != "Terms" {
		t.Errorf("pane = %+v", x.Pane)
	}
	if x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "Nothing answers at 127.0.0.1:7393") {
		t.Errorf("not served: %+v", x)
	}
	answers = true
	for _, y := range Status(e) {
		if y.Name == "terms" {
			if x := part(t, y, Terms); x.State != ipc.PartReady || !strings.Contains(x.Detail, "Terms tab") {
				t.Errorf("served: %+v", x)
			}
		}
	}
	e.Config.Plugins.Terms = false
	if why := Why(e.Config, Terms); !strings.Contains(why, "plugins on terms") {
		t.Errorf("Why = %q", why)
	}
	if ed, err := Edits("terms", "", true); err != nil || len(ed) != 1 || ed[0].Key != "plugins.terms" {
		t.Errorf("Edits = %+v, %v", ed, err)
	}
}
