package ui

// The agents' own colours. While a session works, the Mac app draws its
// card's mark, the frame round its terminal and the lines about it in a
// colour of its own (Theme.swift, agentHex), and Irie's status line draws
// the session's state and the rest of its line in the same colour (DESIGN
// §6.3). The core picks which colour each session takes (internal/core,
// hue.go). The steps follow Theme.swift's, so the colours match.
//
// The colours are worked out in OKLCH, Oklab's polar form, where hues of
// one lightness and chroma look equally light and equally vivid: six hues
// spread round the whole wheel, at a low chroma, so the agents differ by
// hue and none glares.

import (
	"fmt"
	"image/color"
	"math"
)

// AgentColors is how many colours the agents have.
const AgentColors = 6

// The hues leave out hueGap degrees either side of the palette's needs red,
// which is only for what needs you (DESIGN §5.3), and spread evenly over
// the rest of the wheel, hueStep degrees apart.
const (
	hueGap  = 30.0
	hueStep = (360 - 2*hueGap) / AgentColors
)

// A dark palette's working card: the mark's chroma and the lightness it
// starts from, and the chroma and the lift of the card's wash.
const (
	markChroma     = 0.085
	darkMarkL      = 0.78
	darkWashChroma = 0.04
	darkWashLift   = 0.025
)

// terminalGround is the ground of the Mac app's terminal (Theme.swift,
// terminalBackground).
var terminalGround = rgb8{0x14, 0x11, 0x0D}

// AgentColor is the agents' colour n, from 1 to AgentColors, as "#rrggbb":
// a working card's mark in the Mac app's dark mode, in the palette theme
// names (rosewater for a name it does not know); "" for 0, no colour. On
// the Mac app's terminal, where the status line draws it, it reads at
// 4.5:1 or more (WCAG 2.2, 1.4.3).
func AgentColor(theme string, n int) string {
	if n < 1 {
		return ""
	}
	t, ok := themes[theme]
	if !ok {
		t = themes["rosewater"]
	}
	c := agentMark((n-1)%AgentColors, t.dark)
	return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)
}

// HuesApart is how far round the colour wheel, in degrees, colour a's hue
// sits from colour b's, each from 1 to AgentColors: the same in every
// palette.
func HuesApart(a, b int) float64 {
	d := math.Abs(float64(a-b)) * hueStep
	return min(d, 360-d)
}

// agentHue is a slot's hue, from 0, in OKLCH degrees.
func agentHue(slot int, p palette) float64 {
	_, _, red := toOKLCH(channels(p.needs))
	return math.Mod(red+hueGap+hueStep/2+float64(slot%AgentColors)*hueStep, 360)
}

// agentMark is a slot's mark on a dark palette's working card: at
// darkMarkL, or lighter where that keeps less than 3.2:1 on the card's wash
// or on the selection.
func agentMark(slot int, p palette) rgb8 {
	hue := agentHue(slot, p)
	wash, selection := agentWash(hue, p).luminance(), channels(p.selection).luminance()
	l := reach(darkMarkL, 1, func(l float64) bool {
		y := fromOKLCH(l, markChroma, hue).luminance()
		return ratio(y, wash) >= 3.2 && ratio(y, selection) >= 3.2
	})
	return fromOKLCH(l, markChroma, hue)
}

// agentWash is a dark palette's working card: the card's lightness, lifted
// a little, in the hue, as far toward that as keeps its muted text at
// 4.6:1.
func agentWash(hue float64, p palette) rgb8 {
	card := channels(p.card)
	l, _, _ := toOKLCH(card)
	full := fromOKLCH(l+darkWashLift, darkWashChroma, hue)
	muted := channels(p.muted).luminance()
	fits := func(t float64) bool { return ratio(mix(card, full, t).luminance(), muted) >= 4.6 }
	if fits(1) {
		return full
	}
	lo, hi := 0.0, 1.0
	for range 14 {
		mid := (lo + hi) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return mix(card, full, lo)
}

// reach is from, or the nearest value toward to for which ok holds, found
// in 14 halvings, as Theme.swift's reach. ok must hold at to.
func reach(from, to float64, ok func(float64) bool) float64 {
	if ok(from) {
		return from
	}
	lo, hi := from, to
	for range 14 {
		mid := (lo + hi) / 2
		if ok(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// rgb8 is a colour's sRGB channels.
type rgb8 struct{ r, g, b uint8 }

func channels(c color.Color) rgb8 {
	r, g, b, _ := c.RGBA()
	return rgb8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

// mix is a moved toward b by t, channel by channel.
func mix(a, b rgb8, t float64) rgb8 {
	ch := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return rgb8{ch(a.r, b.r), ch(a.g, b.g), ch(a.b, b.b)}
}

// linear is an sRGB channel's linear light.
func linear(v uint8) float64 {
	x := float64(v) / 255
	if x <= 0.04045 {
		return x / 12.92
	}
	return math.Pow((x+0.055)/1.055, 2.4)
}

// luminance is WCAG 2.2's relative luminance.
func (c rgb8) luminance() float64 {
	return 0.2126*linear(c.r) + 0.7152*linear(c.g) + 0.0722*linear(c.b)
}

// ratio is WCAG 2.2's contrast ratio between two luminances.
func ratio(a, b float64) float64 {
	return (max(a, b) + 0.05) / (min(a, b) + 0.05)
}

// toOKLCH is a colour's OKLCH: its lightness, chroma and hue in degrees
// (Björn Ottosson, "A perceptual color space for image processing", 2020).
func toOKLCH(c rgb8) (l, chroma, hue float64) {
	r, g, b := linear(c.r), linear(c.g), linear(c.b)
	l1 := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m1 := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s1 := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	l = 0.2104542553*l1 + 0.7936177850*m1 - 0.0040720468*s1
	a := 1.9779984951*l1 - 2.4285922050*m1 + 0.4505937099*s1
	bb := 0.0259040371*l1 + 0.7827717662*m1 - 0.8086757660*s1
	hue = math.Atan2(bb, a) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}
	return l, math.Sqrt(a*a + bb*bb), hue
}

// fromOKLCH is the sRGB colour of a lightness, a chroma and a hue in
// degrees, each channel held to sRGB's range and rounded, as Theme.swift's
// hex(l:c:h:).
func fromOKLCH(l, chroma, hue float64) rgb8 {
	a, b := chroma*math.Cos(hue*math.Pi/180), chroma*math.Sin(hue*math.Pi/180)
	l1 := l + 0.3963377774*a + 0.2158037573*b
	m1 := l - 0.1055613458*a - 0.0638541728*b
	s1 := l - 0.0894841775*a - 1.2914855480*b
	l3, m3, s3 := l1*l1*l1, m1*m1*m1, s1*s1*s1
	ch := func(x float64) uint8 {
		x = min(1, max(0, x))
		if x <= 0.0031308 {
			x *= 12.92
		} else {
			x = 1.055*math.Pow(x, 1/2.4) - 0.055
		}
		return uint8(math.Round(x * 255))
	}
	return rgb8{
		ch(4.0767416621*l3 - 3.3077115913*m3 + 0.2309699292*s3),
		ch(-1.2684380046*l3 + 2.6097574011*m3 - 0.3413193965*s3),
		ch(-0.0041960863*l3 - 0.7034186147*m3 + 1.7076147010*s3),
	}
}
