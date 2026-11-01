// Command mkicon draws Irie's app icon, a rounded square with a deep-green
// ground and a warm sun rising over two calm waves (DESIGN §7.2), and writes
// it as the ten PNGs of a macOS iconset:
//
//	go run ./tools/mkicon mac/.build/Irie.iconset
//	iconutil -c icns -o Irie.icns mac/.build/Irie.iconset
//
// With -phone it writes the phone app's icons instead: the same picture as
// full squares, which iOS and the web app's manifest round themselves,
// into the gateway's files (docs/PHONE.md); with -ios, the native iPhone
// app's icon, one 1024 px square in an asset catalog's icon set:
//
//	go run ./tools/mkicon -phone internal/remote/web
//	go run ./tools/mkicon -ios ios/Irie.swiftpm/Sources/Assets.xcassets/AppIcon.appiconset
//
// It needs the standard library only and draws no text. Each size is drawn
// from the shapes, not shrunk from the largest, so the small ones stay sharp,
// and the same shapes give the same bytes on every run.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// files are the iconset's PNGs and their sizes in pixels, named as iconutil
// wants them.
var files = []struct {
	name string
	px   int
}{
	{"icon_16x16.png", 16},
	{"icon_16x16@2x.png", 32},
	{"icon_32x32.png", 32},
	{"icon_32x32@2x.png", 64},
	{"icon_128x128.png", 128},
	{"icon_128x128@2x.png", 256},
	{"icon_256x256.png", 256},
	{"icon_256x256@2x.png", 512},
	{"icon_512x512.png", 512},
	{"icon_512x512@2x.png", 1024},
}

// phoneFiles are the phone app's icons: apple-touch-icon's 180 px and the
// manifest's two.
var phoneFiles = []struct {
	name string
	px   int
}{
	{"icon-180.png", 180},
	{"icon-192.png", 192},
	{"icon-512.png", 512},
}

func main() {
	args := os.Args[1:]
	flagged := len(args) == 2 && (args[0] == "-phone" || args[0] == "-ios")
	if len(args) != 1 && !flagged {
		fmt.Fprintln(os.Stderr, "usage: mkicon DIR.iconset | mkicon -phone DIR | mkicon -ios DIR.appiconset")
		os.Exit(2)
	}
	var err error
	switch {
	case flagged && args[0] == "-phone":
		err = writePhone(args[1])
	case flagged:
		err = writeIOS(args[1])
	default:
		err = write(args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkicon:", err)
		os.Exit(1)
	}
}

// writePhone writes the phone app's icons into dir.
func writePhone(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range phoneFiles {
		var buf bytes.Buffer
		if err := png.Encode(&buf, drawFull(f.px)); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// iosContents is the icon set's Contents.json: one universal 1024 px
// image, which Xcode 14 and later scale to every size iOS needs.
const iosContents = `{
  "images" : [
    {
      "filename" : "icon-1024.png",
      "idiom" : "universal",
      "platform" : "ios",
      "size" : "1024x1024"
    }
  ],
  "info" : {
    "author" : "mkicon",
    "version" : 1
  }
}
`

// writeIOS writes the native iPhone app's icon set into dir.
func writeIOS(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, drawFull(1024)); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "icon-1024.png"), buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "Contents.json"), []byte(iosContents), 0o644)
}

// drawFull renders the inside of the icon's square, px pixels across, with
// no corners or shadow: the square fills the picture, and whoever shows it
// rounds it. It has no transparency, which iOS would show as black.
func drawFull(px int) *image.NRGBA {
	m := newMark(px)
	m.s = float64(px) / (2 * half)
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	var wg sync.WaitGroup
	for j := 0; j < px; j++ {
		wg.Go(func() {
			for i := 0; i < px; i++ {
				x, y := mid-half+(float64(i)+0.5)/m.s, mid-half+(float64(j)+0.5)/m.s
				img.SetNRGBA(i, j, m.inside(x, y))
			}
		})
	}
	wg.Wait()
	return img
}

// write draws each size once and writes the iconset's files into dir.
func write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	encoded := map[int][]byte{}
	for _, f := range files {
		b, ok := encoded[f.px]
		if !ok {
			var buf bytes.Buffer
			if err := png.Encode(&buf, draw(f.px)); err != nil {
				return err
			}
			b = buf.Bytes()
			encoded[f.px] = b
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Irie's colours, as the site's favicon draws the mark. The ground runs from
// a lighter deep green at the top to a deeper one at the bottom, and the
// waves are the sea's colour and foam.
var (
	groundTop    = hex(0x246A4E)
	groundBottom = hex(0x184C37)
	glow         = hex(0x2F7A59)
	sun          = hex(0xF2B544)
	waveColors   = []rgb{hex(0x2E9E77), hex(0xEAF6EF)}
)

// The shapes, in units of the 1024 px canvas. macOS draws an icns as it is,
// so the icon carries its own shape: a square of 824 with continuous
// corners, centred, on a soft shadow, as Apple's template has it.
const (
	canvas = 1024.0
	mid    = canvas / 2
	half   = 412.0 // half the square's side

	shadowDrop  = 10.0 // how far down the shadow falls
	shadowBlur  = 9.0  // its standard deviation
	shadowAlpha = 0.3

	// The light round the sun: how strong it is at the sun, and how far it
	// spreads (its standard deviation).
	glowAlpha, glowBlur = 0.55, 230.0
)

// mark is where the sun and the waves go at one size.
type mark struct {
	s             float64 // pixels per unit
	horizon, sunR float64 // the sun's flat side, and its radius
	rays          [][2]point
	rayW, waveW   float64 // half the lines' widths
	waves         []line
}

// newMark lays the mark out for a picture px pixels square. From 64 px up it
// is the whole mark: five rays round the sun and two swelling waves. Smaller,
// the rays would blur into the sun and the swells into a checkerboard, so it
// draws a larger sun over two level lines, with their edges on whole pixels.
func newMark(px int) mark {
	s := float64(px) / canvas
	if px >= 64 {
		m := mark{s: s, horizon: 540, sunR: 150, rayW: 18, waveW: 19}
		for _, deg := range []float64{10, 50, 90, 130, 170} {
			a := deg * math.Pi / 180
			dx, dy := math.Cos(a), -math.Sin(a)
			// A ray's round ends are centred 213 and 277 from the sun's centre.
			m.rays = append(m.rays, [2]point{{mid + 213*dx, m.horizon + 213*dy}, {mid + 277*dx, m.horizon + 277*dy}})
		}
		m.waves = []line{
			wave{mid - 262, mid + 262, m.horizon + 72, 25}.line(),
			wave{mid - 200, mid + 200, m.horizon + 172, 20}.line(),
		}
		return m
	}
	// snap moves y to the nearest pixel edge, or with off 0.5 to the nearest
	// pixel's middle, where a line an odd number of pixels wide is sharp.
	snap := func(y, off float64) float64 { return (math.Round(y*s-off) + off) / s }
	width := math.Max(1, math.Round(52*s))
	off := math.Mod(width, 2) / 2
	m := mark{s: s, horizon: snap(482, 0), sunR: 205, waveW: width / s / 2}
	m.waves = []line{
		wave{mid - 270, mid + 270, snap(m.horizon+100, off), 0}.line(),
		wave{mid - 190, mid + 190, snap(m.horizon+220, off), 0}.line(),
	}
	return m
}

// far stands for a distance from a shape too great to touch a pixel.
const far = math.MaxFloat64

// draw renders the icon px pixels square. Each pixel depends on nothing but
// its place, so the rows are drawn at once.
func draw(px int) *image.NRGBA {
	m := newMark(px)
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	var wg sync.WaitGroup
	for j := 0; j < px; j++ {
		wg.Go(func() {
			for i := 0; i < px; i++ {
				img.SetNRGBA(i, j, m.at((float64(i)+0.5)/m.s, (float64(j)+0.5)/m.s))
			}
		})
	}
	wg.Wait()
	return img
}

// at is the colour at (x, y), a point on the canvas.
func (m *mark) at(x, y float64) color.NRGBA {
	body := m.cover(square(x, y))
	alpha := body
	if body < 1 {
		// A blurred edge shades as the complementary error function; the
		// pixel's own width softens it where the blur is narrower than a pixel.
		blur := math.Hypot(shadowBlur*m.s, 0.5)
		shadow := shadowAlpha * 0.5 * math.Erfc(square(x, y-shadowDrop)*m.s/(blur*math.Sqrt2))
		alpha = body + shadow*(1-body)
	}
	if alpha <= 0 {
		return color.NRGBA{}
	}
	if body == 0 {
		return color.NRGBA{A: channel(alpha)}
	}

	c := m.ground(x, y)

	// The shadow is black, so under the body's soft edge it only darkens.
	k := body / alpha
	return color.NRGBA{channel(c.r * k), channel(c.g * k), channel(c.b * k), channel(alpha)}
}

// inside is the colour at (x, y) inside the square, opaque.
func (m *mark) inside(x, y float64) color.NRGBA {
	c := m.ground(x, y)
	return color.NRGBA{channel(c.r), channel(c.g), channel(c.b), 255}
}

// ground is the square's colour at (x, y) under nothing but the light: the
// green ground, the glow, the sun and its rays, and the waves.
func (m *mark) ground(x, y float64) rgb {
	c := mix(groundTop, groundBottom, (y-(mid-half))/(2*half))
	r2 := (x-mid)*(x-mid) + (y-m.horizon)*(y-m.horizon)
	c = mix(c, glow, glowAlpha*math.Exp(-r2/(2*glowBlur*glowBlur)))

	d := math.Max(math.Hypot(x-mid, y-m.horizon)-m.sunR, y-m.horizon)
	for _, r := range m.rays {
		d = math.Min(d, segment(x, y, r[0], r[1])-m.rayW)
	}
	c = mix(c, sun, m.cover(d))
	for k, l := range m.waves {
		c = mix(c, waveColors[k], m.cover(l.dist(x, y, m.waveW+2/m.s)-m.waveW))
	}
	return c
}

// cover is how much of a pixel a shape covers, from the signed distance d
// to its edge, in units and negative inside.
func (m *mark) cover(d float64) float64 {
	return math.Max(0, math.Min(1, 0.5-d*m.s))
}

// square is the signed distance from (x, y) to the icon's edge, negative
// inside. Its straight sides meet in superelliptic corners whose curvature
// falls to nothing where they join the sides, like Apple's continuous
// corners; at 45° a corner is where a plain one of radius 185 would be.
func square(x, y float64) float64 {
	const n, c = 3.2, 284.0 // the corners' degree, and how far along each side they reach
	ax, ay := math.Abs(x-mid), math.Abs(y-mid)
	u, v := (ax-(half-c))/c, (ay-(half-c))/c
	if u <= 0 || v <= 0 {
		return math.Max(ax, ay) - half
	}
	// f grows like a distance along the sides. Dividing it by the length of
	// its gradient makes it one round the corner too.
	g := math.Pow(u, n) + math.Pow(v, n)
	f := c * (math.Pow(g, 1/n) - 1)
	k := math.Pow(g, 1/n-1)
	return f / math.Hypot(k*math.Pow(u, n-1), k*math.Pow(v, n-1))
}

type point struct{ x, y float64 }

// segment is the distance from (x, y) to the segment from a to b.
func segment(x, y float64, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := math.Max(0, math.Min(1, ((x-a.x)*dx+(y-a.y)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(x-a.x-t*dx, y-a.y-t*dy)
}

// wave is a wave line from x0 to x1 about the level y: two swells and two
// troughs, each high from the level, rising first as the favicon's do.
type wave struct{ x0, x1, y, high float64 }

// steps is how many straight pieces a wave line is drawn with.
const steps = 512

// line is a wave's line as points evenly spaced along x.
type line struct {
	pts        []point
	box        [4]float64 // its bounds: left, top, right, bottom
	x0, stride float64
}

func (w wave) line() line {
	l := line{
		box:    [4]float64{w.x0, w.y - w.high, w.x1, w.y + w.high},
		x0:     w.x0,
		stride: (w.x1 - w.x0) / steps,
	}
	for k := 0; k <= steps; k++ {
		x := w.x0 + l.stride*float64(k)
		l.pts = append(l.pts, point{x, w.y - w.high*math.Sin(4*math.Pi*(x-w.x0)/(w.x1-w.x0))})
	}
	return l
}

// dist is the distance from (x, y) to the line, or far beyond reach of it.
// Only pieces within reach along x can be the nearest.
func (l line) dist(x, y, reach float64) float64 {
	if x < l.box[0]-reach || x > l.box[2]+reach || y < l.box[1]-reach || y > l.box[3]+reach {
		return far
	}
	lo := max(0, int((x-reach-l.x0)/l.stride))
	hi := min(steps, int((x+reach-l.x0)/l.stride)+1)
	d := far
	for k := lo; k < hi; k++ {
		d = math.Min(d, segment(x, y, l.pts[k], l.pts[k+1]))
	}
	return d
}

// rgb is a colour in sRGB, each channel from 0 to 1.
type rgb struct{ r, g, b float64 }

func hex(v uint32) rgb {
	return rgb{float64(v>>16&0xFF) / 255, float64(v>>8&0xFF) / 255, float64(v&0xFF) / 255}
}

// mix is a moved toward b by t.
func mix(a, b rgb, t float64) rgb {
	return rgb{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}

func channel(v float64) uint8 {
	return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
}
