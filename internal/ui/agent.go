package ui

// The agents' own colours. While a session works, the Mac app draws its
// card's mark, the frame round its terminal and the lines about it in a
// colour of its own (Theme.swift, agentHex), and Irie's status line draws
// the session's state and the rest of its line in the same colour (DESIGN
// §6.3). The steps follow Theme.swift's, so the colours match.

import (
	"fmt"
	"hash/fnv"
	"image/color"
	"math"
)

// agentSlots is how many colours there are.
const agentSlots = 6

// terminalGround is the ground of the Mac app's terminal (Theme.swift,
// terminalBackground).
var terminalGround = rgb8{0x14, 0x11, 0x0D}

// AgentColor is a working session's own colour, as "#rrggbb": its card's
// mark in the Mac app's dark mode, in the palette theme names (rosewater
// for a name it does not know). On the Mac app's terminal, where the
// status line draws it, it reads at 4.5:1 or more (WCAG 2.2, 1.4.3).
func AgentColor(theme, id string) string {
	t, ok := themes[theme]
	if !ok {
		t = themes["rosewater"]
	}
	c := agentMark(agentSlot(id), t.dark)
	return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)
}

// agentSlot is a session's slot, the same one every time: FNV-1a of its ID,
// as Theme.swift's agentSlot.
func agentSlot(id string) int {
	h := fnv.New32a()
	h.Write([]byte(id))
	return int(h.Sum32() % agentSlots)
}

// agentHue is a slot's hue in degrees. The slots fan out over the half of
// the colour wheel that starts 90° past the palette's accent, and leave out
// 30° either side of its needs red, which is only for what needs you
// (DESIGN §5.3).
func agentHue(slot int, p palette) float64 {
	start := hueOf(p.accent) + 90
	// The red's place along the fan, and the stretch of the fan it takes.
	red := math.Mod(hueOf(p.needs)-start, 360)
	if red < 0 {
		red += 360
	}
	if red >= 270 {
		red -= 360
	}
	from := max(0, red-30)
	gap := max(0, min(180, red+30)-from)
	along := float64(slot%agentSlots) * (180 - gap) / float64(agentSlots-1)
	if gap > 0 && along >= from {
		along += gap
	}
	return math.Mod(start+along, 360)
}

// agentMark is a slot's mark on a dark palette's working card: as bright
// as keeps 3.2:1 on the card's wash and on the selection, past full
// brightness paling toward white, for a dark hue such as a blue.
func agentMark(slot int, p palette) rgb8 {
	hue := agentHue(slot, p)
	wash, selection := agentWash(hue, p).luminance(), channels(p.selection).luminance()
	mark := func(x float64) rgb8 {
		if x <= 1 {
			return fromHSB(hue, 0.62, x)
		}
		return fromHSB(hue, 0.62*(2-x), 1)
	}
	lo, hi := 0.0, 2.0
	for range 14 {
		mid := (lo + hi) / 2
		if l := mark(mid).luminance(); ratio(l, wash) >= 3.2 && ratio(l, selection) >= 3.2 {
			hi = mid
		} else {
			lo = mid
		}
	}
	return mark(hi)
}

// agentWash is a dark palette's working card: the card washed with the
// hue, as far as keeps its muted text at 4.6:1 and leaves the brightest
// mark of the hue 3.2:1.
func agentWash(hue float64, p palette) rgb8 {
	card, vivid := channels(p.card), fromHSB(hue, 0.62, 0.6)
	muted, brightest := channels(p.muted).luminance(), fromHSB(hue, 0.62, 1).luminance()
	fits := func(t float64) bool {
		l := mix(card, vivid, t).luminance()
		return ratio(l, muted) >= 4.6 && ratio(l, brightest) >= 3.2
	}
	lo, hi := 0.0, 0.45
	if fits(hi) {
		return mix(card, vivid, hi)
	}
	for range 14 {
		mid := (lo + hi) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return mix(card, vivid, lo)
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

// luminance is WCAG 2.2's relative luminance.
func (c rgb8) luminance() float64 {
	lin := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.r) + 0.7152*lin(c.g) + 0.0722*lin(c.b)
}

// ratio is WCAG 2.2's contrast ratio between two luminances.
func ratio(a, b float64) float64 {
	return (max(a, b) + 0.05) / (min(a, b) + 0.05)
}

// hueOf is a colour's hue in degrees, as AppKit's getHue gives it.
func hueOf(c color.Color) float64 {
	x := channels(c)
	r, g, b := float64(x.r)/255, float64(x.g)/255, float64(x.b)/255
	hi, lo := max(r, g, b), min(r, g, b)
	if hi == lo {
		return 0
	}
	var h float64
	switch hi {
	case r:
		h = (g - b) / (hi - lo)
	case g:
		h = 2 + (b-r)/(hi-lo)
	default:
		h = 4 + (r-g)/(hi-lo)
	}
	if h *= 60; h < 0 {
		h += 360
	}
	return h
}

// fromHSB is the colour of a hue in degrees, a saturation and a brightness,
// each channel rounded, as AppKit's NSColor(hue:saturation:brightness:alpha:)
// gives it in sRGB.
func fromHSB(h, s, v float64) rgb8 {
	h = math.Mod(h, 360) / 60
	i := math.Floor(h)
	f := h - i
	p, q, t := v*(1-s), v*(1-s*f), v*(1-s*(1-f))
	var r, g, b float64
	switch int(i) {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	ch := func(x float64) uint8 { return uint8(math.Round(x * 255)) }
	return rgb8{ch(r), ch(g), ch(b)}
}
