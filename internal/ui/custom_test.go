package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// hexes is a palette's colours by their names in a theme file.
func hexes(p palette) map[string]string {
	out := map[string]string{}
	for k, c := range p.roles() {
		out[k] = "#" + hexOf(*c)
	}
	return out
}

// A palette of the user's own that is one of Irie's colour for colour
// gives the agents the same colours on the status line.
func TestAPaletteOfYourOwnColoursTheAgentsAsIriesDo(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(themes)) {
		p, err := ParseDark(hexes(themes[name].dark))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for n := 1; n <= AgentColors; n++ {
			if got, want := AgentColorIn(p, n), AgentColor(name, n); got != want {
				t.Errorf("%s, colour %d: %s, want %s", name, n, got, want)
			}
		}
	}
	// Its own needs red turns the agents' hues with it, as in the app.
	own := hexes(themes["rosewater"].dark)
	own["needs"] = "#7FB2FF"
	p, err := ParseDark(own)
	if err != nil {
		t.Fatal(err)
	}
	if AgentColorIn(p, 1) == AgentColor("rosewater", 1) {
		t.Errorf("a blue needs colour left colour 1 as rosewater's %s", AgentColor("rosewater", 1))
	}
	if AgentColorIn(p, 0) != "" {
		t.Errorf("no colour: %q", AgentColorIn(p, 0))
	}
	// A role left out keeps rosewater's; "RRGGBB" without # is taken too.
	if p, err := ParseDark(map[string]string{"accent": "336699"}); err != nil || fmt.Sprint(p.p.card) != fmt.Sprint(themes["rosewater"].dark.card) {
		t.Errorf("a palette with one colour: %v, card %v", err, p.p.card)
	}
	for _, bad := range []map[string]string{{"plaid": "#000000"}, {"card": "#12345"}, {"card": "red"}, {"card": "#GG0000"}} {
		if _, err := ParseDark(bad); err == nil {
			t.Errorf("%v: no error", bad)
		}
	}
}

// The core reads a palette of the user's own by the names the Mac app
// writes its theme files with (Looks.swift, ColorRole.all).
func TestPaletteRolesMatchTheMacApp(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "mac", "Sources", "Irie", "Looks.swift"))
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("no mac/Sources/Irie/Looks.swift")
	}
	if err != nil {
		t.Fatal(err)
	}
	var p palette
	want := slices.Sorted(maps.Keys(p.roles()))
	var got []string
	for _, m := range regexp.MustCompile(`\.init\(key: "(\w+)", path: \\\.(\w+),`).FindAllSubmatch(src, -1) {
		got = append(got, string(m[1]))
		// Each name is its swatch's, in snake case.
		if snake := strings.ToLower(regexp.MustCompile(`([a-z])([A-Z])`).ReplaceAllString(string(m[2]), "${1}_${2}")); snake != string(m[1]) {
			t.Errorf("Looks.swift names \\.%s %q", m[2], m[1])
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("Looks.swift's roles are %v, the core's %v", got, want)
	}
}
