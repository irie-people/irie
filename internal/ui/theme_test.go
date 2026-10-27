package ui

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/ipc"
)

// luminance is a colour's relative luminance, as WCAG 2.2 defines it for
// sRGB.
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		x := float64(v>>8) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrast is WCAG 2.2's contrast ratio of two colours.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func TestContrastMatchesWCAG(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want float64
	}{
		{"#000000", "#FFFFFF", 21},
		{"#FFFFFF", "#FFFFFF", 1},
		{"#767676", "#FFFFFF", 4.54}, // the lightest grey that passes 1.4.3 on white
	} {
		if got := contrast(lipgloss.Color(c.a), lipgloss.Color(c.b)); math.Abs(got-c.want) > 0.005 {
			t.Errorf("contrast(%s, %s) = %.3f, want %.2f", c.a, c.b, got, c.want)
		}
	}
}

// TestPalettesMeetWCAG: in every palette, light and dark, each colour drawn
// as text reads at 4.5:1 (WCAG 2.2, 1.4.3) and the accent, drawn as a mark,
// at 3:1 (1.4.11) on every surface they are drawn on.
func TestPalettesMeetWCAG(t *testing.T) {
	type pair struct {
		name   string
		fg, bg color.Color
		min    float64
	}
	var table []pair
	for _, name := range config.Themes {
		for mode, p := range map[string]palette{"light": themes[name].light, "dark": themes[name].dark} {
			surfaces := []struct {
				name string
				c    color.Color
			}{{"sidebar", p.sidebar}, {"card", p.card}, {"panel", p.panel}, {"field", p.field}, {"hover", p.hover}, {"selection", p.selection}, {"needsTint", p.needsTint}}
			texts := []struct {
				name string
				c    color.Color
			}{{"ink", p.ink}, {"muted", p.muted}, {"needs", p.needs}, {"done", p.done}, {"paused", p.paused}, {"frozen", p.frozen}, {"accentInk", p.accentInk}}
			for _, s := range surfaces {
				for _, x := range texts {
					table = append(table, pair{fmt.Sprintf("%s/%s/%s on %s", name, mode, x.name, s.name), x.c, s.c, 4.5})
				}
				table = append(table, pair{fmt.Sprintf("%s/%s/accent on %s", name, mode, s.name), p.accent, s.c, 3})
			}
		}
	}
	if len(table) != len(config.Themes)*2*7*8 {
		t.Fatalf("%d pairs, want %d", len(table), len(config.Themes)*2*7*8)
	}
	for _, c := range table {
		if got := contrast(c.fg, c.bg); got < c.min {
			t.Errorf("%s: %.2f:1, want at least %.1f:1", c.name, got, c.min)
		}
	}
}

// Every colour of every palette is a hex value that parsed.
func TestPalettesAreWhole(t *testing.T) {
	for name, th := range themes {
		for mode, p := range map[string]palette{"light": th.light, "dark": th.dark} {
			v := reflect.ValueOf(p)
			for i := range v.NumField() {
				if f := v.Field(i); f.IsNil() || f.Elem().Type() != reflect.TypeOf(color.RGBA{}) {
					t.Errorf("%s %s: %s is not a hex colour", name, mode, v.Type().Field(i).Name)
				}
			}
		}
	}
}

// swatches names every colour of a palette by its key in the Mac app's
// Theme.swift.
func swatches(p palette) map[string]color.Color {
	return map[string]color.Color{
		"sidebar": p.sidebar, "ground": p.ground, "card": p.card, "panel": p.panel, "field": p.field,
		"edge": p.edge, "hover": p.hover, "selection": p.selection, "needsTint": p.needsTint,
		"ink": p.ink, "muted": p.muted, "accent": p.accent, "accentInk": p.accentInk, "accentFill": p.accentFill, "onAccent": p.onAccent,
		"needs": p.needs, "done": p.done, "paused": p.paused, "frozen": p.frozen, "sun": p.sun, "waves": p.waves,
	}
}

// hexOf writes c as Theme.swift's 0xRRGGBB does, after the 0x.
func hexOf(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%02X%02X%02X", r>>8, g>>8, b>>8)
}

var (
	// swiftPalette is a palette of Theme.swift: `static let <id> =
	// Palette(id: "<id>", name: ..., light: Swatches(...), dark:
	// Swatches(...))`.
	swiftPalette = regexp.MustCompile(`(?s)static let (\w+) = Palette\(\s*id: "([^"]*)".*?light: Swatches\(([^)]*)\),\s*dark: Swatches\(([^)]*)\)\)`)
	// swiftSwatch is one colour of a Swatches: `<key>: 0xRRGGBB`.
	swiftSwatch = regexp.MustCompile(`(\w+): 0x([0-9A-Fa-f]{6})\b`)
)

// TestPalettesMatchTheMacApp: the Mac app's Theme.swift and the sidebar
// hold the same palettes, colour by colour, so the two never drift.
func TestPalettesMatchTheMacApp(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "mac", "Sources", "Irie", "Theme.swift"))
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("no mac/Sources/Irie/Theme.swift")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(src, []byte("struct Palette")) {
		t.Skip("this Theme.swift has no palettes yet")
	}
	found := swiftPalette.FindAllSubmatch(src, -1)
	if len(found) == 0 {
		t.Fatal("Theme.swift has a Palette struct, but no palette this test can read")
	}
	seen := map[string]bool{}
	for _, f := range found {
		id := string(f[2])
		seen[id] = true
		th, ok := themes[id]
		if !ok {
			t.Errorf("Theme.swift has the palette %s, the sidebar does not", id)
			continue
		}
		for _, mode := range []struct {
			name string
			body []byte
			p    palette
		}{{"light", f[3], th.light}, {"dark", f[4], th.dark}} {
			want := swatches(mode.p)
			if len(want) != reflect.TypeOf(mode.p).NumField() {
				t.Fatalf("swatches names %d colours of %d", len(want), reflect.TypeOf(mode.p).NumField())
			}
			got := map[string]string{}
			for _, kv := range swiftSwatch.FindAllSubmatch(mode.body, -1) {
				got[string(kv[1])] = strings.ToUpper(string(kv[2]))
			}
			for _, k := range slices.Sorted(maps.Keys(want)) {
				if got[k] != hexOf(want[k]) {
					t.Errorf("%s %s: %s is 0x%s in Theme.swift and 0x%s here", id, mode.name, k, got[k], hexOf(want[k]))
				}
			}
			for k := range got {
				if _, ok := want[k]; !ok {
					t.Errorf("%s %s: Theme.swift has %s, the sidebar does not", id, mode.name, k)
				}
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(themes)) {
		if !seen[name] {
			t.Errorf("the sidebar has the palette %s, Theme.swift does not", name)
		}
	}
}

// asksForBackground reports whether cmd asks the terminal for its
// background, alone or in a batch. Of the commands it is given it runs only
// a batch, which does not run what it holds; a tick would wait a second.
func asksForBackground(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	code := func(c tea.Cmd) uintptr { return reflect.ValueOf(c).Pointer() }
	nop := func() tea.Msg { return nil }
	ask, batch := code(tea.RequestBackgroundColor), code(tea.Batch(nop, nop))
	switch code(cmd) {
	case ask:
		return true
	case batch:
		msgs, _ := cmd().(tea.BatchMsg)
		return slices.ContainsFunc(msgs, func(c tea.Cmd) bool { return c != nil && code(c) == ask })
	}
	return false
}

// groundOf is the background of the first character on screen.
func groundOf(m Model) string { return cells(strings.Split(m.View().Content, "\n")[0])[0].bg }

func TestTheThemeComesFromTheConfig(t *testing.T) {
	if got, want := slices.Sorted(maps.Keys(themes)), slices.Sorted(slices.Values(config.Themes)); !reflect.DeepEqual(got, want) {
		t.Fatalf("palettes %v, config themes %v", got, want)
	}
	if config.Default().UI.Theme != defaultTheme {
		t.Errorf("ui.theme defaults to %q, the sidebar to %q", config.Default().UI.Theme, defaultTheme)
	}
	for _, name := range config.Themes {
		m := New(&fakeBackend{}, Options{Theme: name, Now: func() time.Time { return now }})
		m = send(m, tea.WindowSizeMsg{Width: 34, Height: 24})
		if m.pal != themes[name].dark || groundOf(m) != rgb(themes[name].dark.sidebar) {
			t.Errorf("theme %s: ground %s, want %s", name, groundOf(m), rgb(themes[name].dark.sidebar))
		}
	}
	if m := New(&fakeBackend{}, Options{}); m.pal != themes["rosewater"].dark {
		t.Error("no theme is not rosewater")
	}
}

// The sidebar asks the terminal for its background and draws the dark set
// until it answers, then the set that suits the answer.
func TestLightOrDarkFollowsTheTerminal(t *testing.T) {
	m, _ := newModel(t, 34, 24, 3)
	if !asksForBackground(m.Init()) {
		t.Error("Init does not ask for the background")
	}
	th := themes[defaultTheme]
	if !m.dark || groundOf(m) != rgb(th.dark.sidebar) {
		t.Errorf("before an answer: dark %v, ground %s", m.dark, groundOf(m))
	}
	// While there is no answer, a tick asks again, a few times.
	asks := 0
	for range askBackground + 3 {
		next, cmd := m.Update(tickMsg{})
		m = next.(Model)
		if asksForBackground(cmd) {
			asks++
		}
	}
	if asks != askBackground {
		t.Errorf("ticks asked %d times, want %d", asks, askBackground)
	}
	// A new size, such as a client attaching, asks again.
	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 34, Height: 30}); !asksForBackground(cmd) {
		t.Error("a new size does not ask for the background")
	}

	m = send(m, tea.BackgroundColorMsg{Color: lipgloss.Color("#FAFAF8")})
	if m.dark || groundOf(m) != rgb(th.light.sidebar) {
		t.Errorf("a light terminal: dark %v, ground %s, want %s", m.dark, groundOf(m), rgb(th.light.sidebar))
	}
	if _, cmd := m.Update(tickMsg{}); asksForBackground(cmd) {
		t.Error("a tick asks again after an answer")
	}
	// The command bar follows too.
	bar := strings.Split(m.View().Content, "\n")
	if c := cells(bar[len(bar)-1]); c[0].bg != rgb(th.light.field) {
		t.Errorf("bar on %s, want the light field %s", c[0].bg, rgb(th.light.field))
	}
	m = send(m, tea.BackgroundColorMsg{Color: lipgloss.Color("#1E1E2E")})
	if !m.dark || groundOf(m) != rgb(th.dark.sidebar) {
		t.Errorf("a dark terminal: dark %v, ground %s", m.dark, groundOf(m))
	}
}

// painted checks that every line of m's screen is the pane's width and that
// every character on it is drawn on one of the palette's surfaces, never on
// the terminal's own background.
func painted(t *testing.T, name string, m Model) {
	t.Helper()
	p := m.pal
	surfaces := map[string]bool{}
	for _, c := range []color.Color{p.sidebar, p.card, p.panel, p.field, p.hover, p.selection, p.needsTint} {
		surfaces[rgb(c)] = true
	}
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) != m.height {
		t.Errorf("%s: %d lines, want %d", name, len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != m.width {
			t.Errorf("%s: line %d is %d columns, want %d: %q", name, i, w, m.width, ansiRE.ReplaceAllString(l, ""))
		}
		for _, c := range cells(l) {
			if !surfaces[c.bg] {
				t.Errorf("%s: line %d: %q is on %q, not on the palette: %q", name, i, c.r, c.bg, ansiRE.ReplaceAllString(l, ""))
				break
			}
		}
	}
}

func TestEveryCellIsOnThePalette(t *testing.T) {
	plan := ipc.Header{Plan: &ipc.Plan{FiveHour: &ipc.Window{Used: 92}}, Memory: &ipc.Memory{Low: true}}
	for _, bg := range []string{"#101010", "#FAFAFA"} {
		for _, h := range []int{3, 5, 8, 12, 24, 30} {
			name := fmt.Sprintf("%s 34×%d", bg, h)
			m := statesModel(t, Options{})
			m = send(m, tea.BackgroundColorMsg{Color: lipgloss.Color(bg)})
			m = send(m, tea.WindowSizeMsg{Width: 34, Height: h})
			m = send(m, outsideFrame(outsideSessions(3), ""))
			m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameHeader, Header: &plan}})
			painted(t, name+", list", m)
			for range 4 {
				m = send(m, keyMsg("down"))
			}
			painted(t, name+", a red row", m)
			m = send(m, keyMsg("end"))
			painted(t, name+", outside", m)
			m = send(m, keyMsg("up"))
			m = send(m, keyMsg("up"))
			m = send(m, keyMsg("up"))
			painted(t, name+", outside header", m)
			m = send(m, keyMsg("tab"))
			painted(t, name+", empty bar", m)
			m = typeText(m, "one")
			m = send(m, keyMsg("ctrl+j"))
			m = typeText(m, "two")
			painted(t, name+", two lines", m)
			m = send(m, keyMsg("esc"))
			m.setStatus("no such directory: /home/alice/missing", true)
			painted(t, name+", an error", m)
			m = send(m, OfflineMsg{})
			painted(t, name+", offline", m)
		}
		m, _ := newModel(t, 34, 24, 0)
		m = send(m, tea.BackgroundColorMsg{Color: lipgloss.Color(bg)})
		painted(t, bg+" the placeholder", m)
	}
}
