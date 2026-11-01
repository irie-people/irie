package ui

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/irie-people/irie/internal/config"
)

// macMarks are the marks of the six colours on each palette's dark working
// cards, as the Mac app's Theme.agentHex(slot:palette:dark:high:) gives
// them with high false.
var macMarks = map[string][AgentColors]string{
	"rosewater":  {"#d1b377", "#a1c48c", "#75c9bd", "#7fc1e6", "#b0b1ed", "#d9a3ce"},
	"terracotta": {"#ddad7d", "#b5be7f", "#81c9aa", "#73c6da", "#9db7ef", "#cca7dd"},
	"lilac":      {"#d0b477", "#9fc48e", "#74c9bf", "#81c0e7", "#b2b0ec", "#daa3cd"},
	"evergreen":  {"#d1b377", "#a1c48d", "#74c9be", "#80c1e6", "#b1b0ec", "#daa3ce"},
}

// TestAgentColorsMatchTheMacApp: a session has the colour its card's mark
// has in the Mac app.
func TestAgentColorsMatchTheMacApp(t *testing.T) {
	for _, name := range config.Themes {
		want, ok := macMarks[name]
		if !ok {
			t.Errorf("no marks to check for the palette %s", name)
			continue
		}
		for n := 1; n <= AgentColors; n++ {
			if got := AgentColor(name, n); got != want[n-1] {
				t.Errorf("%s colour %d: %s, the Mac app's is %s", name, n, got, want[n-1])
			}
		}
	}
	if got, want := AgentColor("no-such-palette", 1), macMarks["rosewater"][0]; got != want {
		t.Errorf("an unknown palette gives %s, want rosewater's %s", got, want)
	}
	if got := AgentColor("rosewater", 0); got != "" {
		t.Errorf("no colour gives %q", got)
	}
}

// TestAgentColorsReadOnTheTerminal: on the Mac app's terminal, every colour
// reads as text at 4.5:1, and a palette's six differ.
func TestAgentColorsReadOnTheTerminal(t *testing.T) {
	ground := terminalGround.luminance()
	for _, name := range config.Themes {
		var seen []string
		for n := 1; n <= AgentColors; n++ {
			c := AgentColor(name, n)
			var x rgb8
			if _, err := fmt.Sscanf(c, "#%02x%02x%02x", &x.r, &x.g, &x.b); err != nil {
				t.Fatalf("%s colour %d: %q: %v", name, n, c, err)
			}
			if r := ratio(x.luminance(), ground); r < 4.5 {
				t.Errorf("%s colour %d: %s is %.2f:1 on the terminal", name, n, c, r)
			}
			if slices.Contains(seen, c) {
				t.Errorf("%s: colour %d is %s, as an earlier one is", name, n, c)
			}
			seen = append(seen, c)
		}
	}
}

// TestAgentColorsAreCalm: every colour is as vivid as the next, and far
// less than the neon of a fully saturated hue.
func TestAgentColorsAreCalm(t *testing.T) {
	for _, name := range config.Themes {
		for n := 1; n <= AgentColors; n++ {
			c := agentMark(n-1, themes[name].dark)
			if _, chroma, _ := toOKLCH(c); math.Abs(chroma-markChroma) > 0.01 {
				t.Errorf("%s colour %d: chroma %.3f, want about %.3f", name, n, chroma, markChroma)
			}
		}
	}
}

// TestHuesApart: neighbours on the wheel sit 50° apart, the two either
// side of the needs red's gap 110°, and the hues sit that far apart in
// every palette.
func TestHuesApart(t *testing.T) {
	for _, c := range []struct {
		a, b int
		want float64
	}{{1, 2, 50}, {1, 3, 100}, {1, 4, 150}, {1, 5, 160}, {1, 6, 110}, {6, 1, 110}, {3, 3, 0}} {
		if got := HuesApart(c.a, c.b); got != c.want {
			t.Errorf("colours %d and %d: %v° apart, want %v°", c.a, c.b, got, c.want)
		}
	}
	for _, name := range config.Themes {
		p := themes[name].dark
		_, _, red := toOKLCH(channels(p.needs))
		for a := 1; a <= AgentColors; a++ {
			h := agentHue(a-1, p)
			if d := math.Abs(math.Mod(h-red+540, 360) - 180); d < hueGap {
				t.Errorf("%s colour %d: hue %.0f° is %.0f° from the needs red", name, a, h, d)
			}
			for b := 1; b <= AgentColors; b++ {
				d := math.Abs(h - agentHue(b-1, p))
				if d = min(d, 360-d); math.Abs(d-HuesApart(a, b)) > 1e-9 {
					t.Errorf("%s colours %d and %d: hues %.1f° apart, HuesApart says %.1f°", name, a, b, d, HuesApart(a, b))
				}
			}
		}
	}
}
