package main

import (
	"bytes"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestIconset checks that mkicon writes all ten files at their sizes, and
// the same bytes on a second run (DESIGN M9).
func TestIconset(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := write(a); err != nil {
		t.Fatal(err)
	}
	if err := write(b); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		first, err := os.ReadFile(filepath.Join(a, f.name))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(first))
		if err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		if r := img.Bounds(); r.Dx() != f.px || r.Dy() != f.px {
			t.Errorf("%s is %dx%d, want %dx%d", f.name, r.Dx(), r.Dy(), f.px, f.px)
		}
		second, err := os.ReadFile(filepath.Join(b, f.name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Errorf("%s differs between two runs", f.name)
		}
	}
}

// TestShape checks what macOS needs, as it draws an icns as it is: clear
// corners round a solid square, with the sun in it in both layouts.
func TestShape(t *testing.T) {
	want := color.NRGBA{0xF2, 0xB5, 0x44, 0xFF}
	for _, px := range []int{16, 32, 64, 256} {
		img := draw(px)
		for _, p := range [][2]int{{0, 0}, {px - 1, 0}, {0, px - 1}, {px - 1, px - 1}} {
			if a := img.NRGBAAt(p[0], p[1]).A; a != 0 {
				t.Errorf("%d px: the corner %v has alpha %d, want 0", px, p, a)
			}
		}
		if a := img.NRGBAAt(px/2, px/2).A; a != 0xFF {
			t.Errorf("%d px: the middle has alpha %d, want 255", px, a)
		}
		m := newMark(px)
		x, y := int(mid*m.s), int((m.horizon-m.sunR/2)*m.s)
		if got := img.NRGBAAt(x, y); got != want {
			t.Errorf("%d px: the sun at (%d, %d) is %v, want %v", px, x, y, got, want)
		}
	}
}

// TestPhoneIcons checks that the phone app's icons in the gateway's files
// are what -phone draws now: full squares with no transparency, at their
// sizes (docs/PHONE.md).
func TestPhoneIcons(t *testing.T) {
	dir := t.TempDir()
	if err := writePhone(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range phoneFiles {
		drawn, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(drawn))
		if err != nil {
			t.Fatal(err)
		}
		if r := img.Bounds(); r.Dx() != f.px || r.Dy() != f.px {
			t.Errorf("%s is %dx%d", f.name, r.Dx(), r.Dy())
		}
		for _, p := range [][2]int{{0, 0}, {f.px - 1, 0}, {0, f.px - 1}, {f.px - 1, f.px - 1}} {
			if _, _, _, a := img.At(p[0], p[1]).RGBA(); a != 0xFFFF {
				t.Errorf("%s is clear at %v", f.name, p)
			}
		}
		kept, err := os.ReadFile(filepath.Join("..", "..", "internal", "remote", "web", f.name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(kept, drawn) {
			t.Errorf("internal/remote/web/%s is not what mkicon draws: run go run ./tools/mkicon -phone internal/remote/web", f.name)
		}
	}
}
