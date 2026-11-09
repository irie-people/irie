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
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// A palette given by its colours that is one of Irie's colour for colour
// is that palette: the same colours back, and the same agents' colours on
// the status line.
func TestAPaletteByItsColoursIsIriesOwn(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(themes)) {
		built, ok := BuiltIn(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		light, dark := built.Colors()
		if len(light) != 21 || len(dark) != 21 {
			t.Fatalf("%s: %d and %d colours, want 21", name, len(light), len(dark))
		}
		p, err := ParseTheme(light, dark)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if l, d := p.Colors(); !maps.Equal(l, light) || !maps.Equal(d, dark) {
			t.Errorf("%s does not come back as it went", name)
		}
		for n := 1; n <= AgentColors; n++ {
			if got, want := AgentColorIn(p, n), AgentColor(name, n); got != want {
				t.Errorf("%s, colour %d: %s, want %s", name, n, got, want)
			}
		}
	}
	// Its own needs red turns the agents' hues with it, as in the app.
	p, err := ParseTheme(nil, map[string]string{"needs": "#7FB2FF"})
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
	if p, err := ParseTheme(map[string]string{"accent": "336699"}, nil); err != nil || fmt.Sprint(p.light.card) != fmt.Sprint(themes["rosewater"].light.card) || hexOf(p.light.accent) != "336699" {
		t.Errorf("a palette with one colour: %v, card %v, accent %v", err, p.light.card, p.light.accent)
	}
	for _, bad := range []map[string]string{{"plaid": "#000000"}, {"card": "#12345"}, {"card": "red"}, {"card": "#GG0000"}} {
		if _, err := ParseTheme(bad, nil); err == nil {
			t.Errorf("light %v: no error", bad)
		}
		if _, err := ParseTheme(nil, bad); err == nil {
			t.Errorf("dark %v: no error", bad)
		}
	}
}

// The sidebar and the slot draw in the palette the header says the Mac app
// draws in, light or dark as the terminal is, and keep ui.theme until it
// says one.
func TestTheSidebarAndTheSlotTakeTheAppsPalette(t *testing.T) {
	own := map[string]string{"sidebar": "#203040", "ink": "#F0F4F8"}
	light, _ := BuiltIn("lilac")
	l, _ := light.Colors()
	header := &ipc.Header{Theme: &ipc.Theme{Name: "harbour", Light: l, Dark: own}}

	m := New(&fakeBackend{}, Options{Theme: "terracotta", Now: time.Now})
	m.setDark(true)
	if hexOf(m.pal.sidebar) != hexOf(themes["terracotta"].dark.sidebar) {
		t.Fatalf("before the header: sidebar %s", hexOf(m.pal.sidebar))
	}
	m.apply(ipc.Frame{T: ipc.FrameHeader, Header: header})
	if hexOf(m.pal.sidebar) != "203040" || hexOf(m.pal.ink) != "F0F4F8" || hexOf(m.pal.card) != hexOf(themes["rosewater"].dark.card) {
		t.Errorf("dark, the app's palette: sidebar %s, ink %s, card %s", hexOf(m.pal.sidebar), hexOf(m.pal.ink), hexOf(m.pal.card))
	}
	m.setDark(false)
	if hexOf(m.pal.sidebar) != hexOf(themes["lilac"].light.sidebar) {
		t.Errorf("light, the app's palette: sidebar %s", hexOf(m.pal.sidebar))
	}
	// A header with no palette leaves it be.
	m.apply(ipc.Frame{T: ipc.FrameHeader, Header: &ipc.Header{}})
	if hexOf(m.pal.sidebar) != hexOf(themes["lilac"].light.sidebar) {
		t.Errorf("a header without a palette changed it: %s", hexOf(m.pal.sidebar))
	}

	// The slot's colours are the package's; the next test finds them as
	// they were.
	t.Cleanup(func() { slotColours(themes[defaultTheme].dark) })
	s := newSlot(nil, SlotOptions{Theme: "evergreen"})
	s.applyFrame(ipc.Frame{T: ipc.FrameSnapshot, Header: header})
	if hexOf(s.pal.sidebar) != "203040" || hexOf(slotGround) != hexOf(themes["rosewater"].dark.ground) {
		t.Errorf("the slot in the app's palette: sidebar %s, ground %s", hexOf(s.pal.sidebar), hexOf(slotGround))
	}
	// A core that sends only a name: Irie's palette of that name.
	s.applyFrame(ipc.Frame{T: ipc.FrameHeader, Header: &ipc.Header{Theme: &ipc.Theme{Name: "evergreen"}}})
	if hexOf(s.pal.sidebar) != hexOf(themes["evergreen"].dark.sidebar) {
		t.Errorf("by name: sidebar %s", hexOf(s.pal.sidebar))
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
