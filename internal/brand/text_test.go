package brand

import "testing"

// TestTextAndArchUseThisBuildsNames: Text and Arch give Irie's own texts
// the names this build carries, Irie's or a copy's: the product, its
// command and variables, the arch agents and how the texts speak of one.
func TestTextAndArchUseThisBuildsNames(t *testing.T) {
	if got, want := Text("Irie's sidebar: run `irie up`, then read IRIE_SESSION."), Name+"'s sidebar: run `"+Slug+" up`, then read "+EnvPrefix+"_SESSION."; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
	if got, want := Arch("Mikael is with you. He stands with your user; ask him."), Mikael+" is with you. "+upper(He)+" stands with your user; ask "+Him+"."; got != want {
		t.Errorf("Arch = %q, want %q", got, want)
	}
}
