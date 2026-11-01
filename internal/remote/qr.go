package remote

import (
	"fmt"
	"net/url"
	"strings"

	"rsc.io/qr"
)

// QR encodes text as a QR code at the medium level of error correction,
// which a phone's camera reads from a screen.
func QR(text string) (*qr.Code, error) {
	return qr.Encode(text, qr.M)
}

// QRSVG is a QR code of text as an SVG picture, black modules on white,
// with a quiet zone of four modules round it.
func QRSVG(text string) (string, error) {
	c, err := QR(text)
	if err != nil {
		return "", err
	}
	const quiet = 4
	n := c.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n, n, n)
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}

// PairLink is the link that pairs the native iPhone app with a machine:
// the gateway's HTTPS address, the code, the full fingerprint of Irie's
// authority and the machine's name. It travels by a channel the person
// trusts, as a QR code on the machine's screen, so the app pins the
// authority it names and needs no profile.
func PairLink(base, code, fingerprint, name string) string {
	q := url.Values{}
	q.Set("u", base)
	q.Set("c", code)
	q.Set("f", fingerprint)
	q.Set("n", name)
	return "irie://pair?" + q.Encode()
}
