package schools

import (
	"fmt"
	"math"

	"github.com/irie-people/roots"
)

// The slices' colours: Irie's six agent hues, worked out from the design
// library's numbers as the cards' are (Roots, agents: hues evenly round
// the OKLCH wheel but needs_gap degrees either side of the palette's needs
// red, at the marks' chroma), in an order that sets each slice's
// neighbours at least 100° round the wheel from it, at two lightnesses
// that alternate, so slices that touch differ in hue and in lightness.
// The lightnesses are the page's own: a pie's slices are large fills,
// lighter than a card's mark, and they hold the dataviz method's floors
// for neighbours (docs/proposals/common-schools.md, "Colours").

// Colours are the slices' fills in the order a pie gives them out, in
// light and in dark mode, as "#rrggbb".
type Colours struct {
	Light []string `json:"light"`
	Dark  []string `json:"dark"`
}

// hueOrder walks the six hues so that each is 100° or 150° from the one
// before it, and the last 110° from the first.
var hueOrder = []int{0, 3, 1, 4, 2, 5}

// sliceLightness is the slices' two OKLCH lightnesses, the first for the
// slots at even places: light mode's, then dark mode's.
var sliceLightness = [2][2]float64{{0.77, 0.68}, {0.69, 0.60}}

// Palette is the design library's palette the page draws in, as the
// Charters Graph's does (pies/pies.css).
const Palette = "evergreen"

// SliceColours are the slices' colours in the page's palette.
func SliceColours() Colours {
	p, ok := roots.PaletteByID(Palette)
	if !ok {
		panic("the design library has no " + Palette + " palette")
	}
	return Colours{Light: sliceColours(p.Light["needs"], false), Dark: sliceColours(p.Dark["needs"], true)}
}

func sliceColours(needs string, dark bool) []string {
	a := roots.TheAgents()
	_, _, red := toOKLCH(needs)
	step := (360 - 2*a.NeedsGap) / float64(a.Slots)
	mode := 0
	if dark {
		mode = 1
	}
	out := make([]string, len(hueOrder))
	for k, i := range hueOrder {
		hue := math.Mod(red+a.NeedsGap+step/2+float64(i)*step, 360)
		out[k] = fromOKLCH(sliceLightness[mode][k%2], a.Mark.Chroma, hue)
	}
	return out
}

// toOKLCH reads "#rrggbb" as OKLCH: lightness, chroma, hue in degrees.
func toOKLCH(hex string) (l, c, h float64) {
	r, g, b := rgb(hex)
	l, a, bb := oklab(linear(r), linear(g), linear(b))
	return l, math.Hypot(a, bb), math.Mod(math.Atan2(bb, a)*180/math.Pi+360, 360)
}

// rgb reads "#rrggbb" into its channels, 0 to 1.
func rgb(hex string) (r, g, b float64) {
	var x, y, z uint8
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &x, &y, &z); err != nil {
		panic("not a colour: " + hex)
	}
	return float64(x) / 255, float64(y) / 255, float64(z) / 255
}

func linear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func gamma(v float64) float64 {
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// oklab is Björn Ottosson's Oklab, from linear sRGB.
func oklab(r, g, b float64) (l, a, bb float64) {
	l1 := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m1 := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s1 := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return 0.2104542553*l1 + 0.7936177850*m1 - 0.0040720468*s1,
		1.9779984951*l1 - 2.4285922050*m1 + 0.4505937099*s1,
		0.0259040371*l1 + 0.7827717662*m1 - 0.8086757660*s1
}

// fromOKLCH writes an OKLCH colour as "#rrggbb", each channel clipped to
// sRGB.
func fromOKLCH(l, c, h float64) string {
	a, b := c*math.Cos(h*math.Pi/180), c*math.Sin(h*math.Pi/180)
	l1 := math.Pow(l+0.3963377774*a+0.2158037573*b, 3)
	m1 := math.Pow(l-0.1055613458*a-0.0638541728*b, 3)
	s1 := math.Pow(l-0.0894841775*a-1.2914855480*b, 3)
	ch := func(v float64) int { return int(math.Round(math.Min(1, math.Max(0, gamma(v))) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x",
		ch(4.0767416621*l1-3.3077115913*m1+0.2309699292*s1),
		ch(-1.2684380046*l1+2.6097574011*m1-0.3413193965*s1),
		ch(-0.0041960863*l1-0.7034186147*m1+1.7076147010*s1))
}
