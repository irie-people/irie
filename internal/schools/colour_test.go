package schools

import (
	"math"
	"testing"
)

// machado are Machado, Oliveira and Fernandes's (2009) simulations of
// protanopia and deuteranopia at full severity, in linear sRGB, the model
// the dataviz method's floors are set for.
var machado = map[string][3][3]float64{
	"protan": {{0.152286, 1.052583, -0.204868}, {0.114503, 0.786281, 0.099216}, {-0.003882, -0.048116, 1.051998}},
	"deutan": {{0.367322, 0.860646, -0.227968}, {0.280085, 0.672501, 0.047413}, {-0.011820, 0.042940, 0.968881}},
}

// lab is a colour in Oklab, seen as the vision named sees it ("" for
// normal vision).
func lab(hex, vision string) [3]float64 {
	r, g, b := rgb(hex)
	c := [3]float64{linear(r), linear(g), linear(b)}
	if m, ok := machado[vision]; ok {
		var d [3]float64
		for i := range 3 {
			d[i] = math.Min(1, math.Max(0, m[i][0]*c[0]+m[i][1]*c[1]+m[i][2]*c[2]))
		}
		c = d
	}
	l, a, bb := oklab(c[0], c[1], c[2])
	return [3]float64{l, a, bb}
}

func deltaE(a, b [3]float64) float64 {
	return 100 * math.Sqrt((a[0]-b[0])*(a[0]-b[0])+(a[1]-b[1])*(a[1]-b[1])+(a[2]-b[2])*(a[2]-b[2]))
}

// TestTheSlicesStayApart: slices that touch, the last beside the first
// too, stay apart by the dataviz method's floors, in light and in dark: 15
// in normal vision (ΔE, Oklab ×100), and 8 for protanopes and deuteranopes;
// their hues sit 100° or more apart, and they are the library's chroma.
func TestTheSlicesStayApart(t *testing.T) {
	c := SliceColours()
	for mode, list := range map[string][]string{"light": c.Light, "dark": c.Dark} {
		if len(list) != len(hueOrder) {
			t.Fatalf("%s: %d colours", mode, len(list))
		}
		for i := range list {
			a, b := list[i], list[(i+1)%len(list)]
			if d := deltaE(lab(a, ""), lab(b, "")); d < 15 {
				t.Errorf("%s: %s and %s are %.1f apart in normal vision", mode, a, b, d)
			}
			for _, v := range []string{"protan", "deutan"} {
				if d := deltaE(lab(a, v), lab(b, v)); d < 8 {
					t.Errorf("%s: %s and %s are %.1f apart for a %s", mode, a, b, d, v)
				}
			}
			_, _, ha := toOKLCH(a)
			_, _, hb := toOKLCH(b)
			if apart := math.Min(math.Abs(ha-hb), 360-math.Abs(ha-hb)); apart < 99 {
				t.Errorf("%s: %s and %s are %.0f° apart", mode, a, b, apart)
			}
		}
		for _, x := range list {
			if _, ch, _ := toOKLCH(x); math.Abs(ch-0.085) > 0.006 {
				t.Errorf("%s: %s has chroma %.3f, not the library's", mode, x, ch)
			}
		}
	}
}
