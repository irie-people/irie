package ui

import (
	"image/color"
	"math"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/irie-people/irie/internal/config"
)

// dichromat is how colours look to someone with one of the three kinds of
// cone missing: the simulation of Brettel, Viénot and Mollon (1997), in
// linear sRGB, with one matrix on each side of a separation plane. The
// numbers are libDaltonLens's (public domain).
type dichromat struct {
	name   string
	m1, m2 [9]float64
	normal [3]float64
}

var dichromats = []dichromat{
	{"protan",
		[9]float64{0.14980, 1.19548, -0.34528, 0.10764, 0.84864, 0.04372, 0.00384, -0.00540, 1.00156},
		[9]float64{0.14570, 1.16172, -0.30742, 0.10816, 0.85291, 0.03892, 0.00386, -0.00524, 1.00139},
		[3]float64{0.00048, 0.00393, -0.00441}},
	{"deutan",
		[9]float64{0.36477, 0.86381, -0.22858, 0.26294, 0.64245, 0.09462, -0.02006, 0.02728, 0.99278},
		[9]float64{0.37298, 0.88166, -0.25464, 0.25954, 0.63506, 0.10540, -0.01980, 0.02784, 0.99196},
		[3]float64{-0.00281, -0.00611, 0.00892}},
	{"tritan",
		[9]float64{1.01277, 0.13548, -0.14826, -0.01243, 0.86812, 0.14431, 0.07589, 0.80500, 0.11911},
		[9]float64{0.93678, 0.18979, -0.12657, 0.06154, 0.81526, 0.12320, -0.37562, 1.12767, 0.24796},
		[3]float64{0.03901, -0.02788, -0.01113}},
}

// sees is c in Oklab as d sees it; a nil d has full colour vision.
func (d *dichromat) sees(c color.Color) [3]float64 {
	x := channels(c)
	rgb := [3]float64{linear(x.r), linear(x.g), linear(x.b)}
	if d != nil {
		m := d.m2
		if rgb[0]*d.normal[0]+rgb[1]*d.normal[1]+rgb[2]*d.normal[2] >= 0 {
			m = d.m1
		}
		var out [3]float64
		for i := range out {
			out[i] = min(1, max(0, m[3*i]*rgb[0]+m[3*i+1]*rgb[1]+m[3*i+2]*rgb[2]))
		}
		rgb = out
	}
	l, a, b := oklab(rgb[0], rgb[1], rgb[2])
	return [3]float64{l, a, b}
}

// apart is how far apart a and b look to d: CSS Color 4's deltaE-OK, the
// distance between them in Oklab. One just noticeable difference is 0.02;
// under 0.06 two colours are hard to tell apart.
func (d *dichromat) apart(a, b color.Color) float64 {
	p, q := d.sees(a), d.sees(b)
	return math.Sqrt((p[0]-q[0])*(p[0]-q[0]) + (p[1]-q[1])*(p[1]-q[1]) + (p[2]-q[2])*(p[2]-q[2]))
}

// viewer is one kind of vision: full colour vision, or a dichromat's.
type viewer struct {
	name string
	d    *dichromat
}

func viewers() []viewer {
	v := []viewer{{"normal", nil}}
	for i := range dichromats {
		v = append(v, viewer{dichromats[i].name, &dichromats[i]})
	}
	return v
}

// The simulation gives what the color-theory research measured: before
// these palettes, Evergreen's light needs you and paused looked the same
// to deutans, and its done and frozen were close to everyone.
func TestDichromatsMatchTheResearch(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want map[string]float64
	}{
		{"#B42A1E", "#855700", map[string]float64{"normal": 0.1251, "protan": 0.0725, "deutan": 0.0170, "tritan": 0.1040}},
		{"#3F6A5F", "#286873", map[string]float64{"normal": 0.0389, "protan": 0.0373, "deutan": 0.0386, "tritan": 0.0258}},
	} {
		for _, v := range viewers() {
			if got := v.d.apart(lipgloss.Color(c.a), lipgloss.Color(c.b)); math.Abs(got-c.want[v.name]) > 0.0006 {
				t.Errorf("%s and %s for %s vision: %.4f apart, want %.4f", c.a, c.b, v.name, got, c.want[v.name])
			}
		}
	}
}

// TestStatesStayApartForColourBlindViewers: in every palette and mode,
// needs you and paused look apart in full colour vision and to protans,
// deutans and tritans alike, and done and frozen to all but tritans. In
// light mode both of the first are dark enough for 4.5:1 text, so a red and
// an amber of one lightness turn one olive for a deutan: needs you is
// deeper than paused instead. Tritans, about 1 in 10,000, see done and
// frozen close in three palettes; each state's glyph tells them apart
// (DESIGN §5.3).
func TestStatesStayApartForColourBlindViewers(t *testing.T) {
	for _, name := range config.Themes {
		for _, mode := range []struct {
			name string
			p    palette
		}{{"light", themes[name].light}, {"dark", themes[name].dark}} {
			p := mode.p
			for _, c := range []struct {
				a, b    string
				x, y    color.Color
				tritans bool
				min     float64
			}{
				{"needs", "paused", p.needs, p.paused, true, 0.07},
				{"done", "frozen", p.done, p.frozen, false, 0.06},
			} {
				for _, v := range viewers() {
					if v.name == "tritan" && !c.tritans {
						continue
					}
					if got := v.d.apart(c.x, c.y); got < c.min {
						t.Errorf("%s %s: %s and %s are %.3f apart for %s vision, want at least %.2f", name, mode.name, c.a, c.b, got, v.name, c.min)
					}
				}
			}
		}
	}
}
