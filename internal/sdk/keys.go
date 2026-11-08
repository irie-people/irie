package sdk

import (
	"bytes"
	"unicode/utf8"
)

// keyKind is what a key does in this program.
type keyKind int

const (
	keyRune       keyKind = iota // a character to type
	keyPaste                     // a bracketed paste: text to type as it is, new lines and all
	keyEnter                     // send the message, or pick the choice
	keyBackspace                 // delete the character before the cursor
	keyClearLine                 // Ctrl+U: delete what is typed
	keyDeleteWord                // Ctrl+W: delete the word before the cursor
	keyEscape                    // stop the turn, or say no to a question
	keyInterrupt                 // Ctrl+C: stop the turn; twice, end the session
	keyEnd                       // Ctrl+D: end the session when nothing is typed
	keyUp
	keyDown
	keyRedraw // Ctrl+L
)

type key struct {
	kind keyKind
	text string // keyRune and keyPaste
}

// Bracketed paste's marks (DESIGN §3.5): the core types a message as a
// paste, then Enter.
var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

// keyReader turns what the terminal sends into keys. It keeps what a read
// cut short, such as half of a paste, for the next.
type keyReader struct {
	rest    []byte
	pasting bool
	paste   []byte
}

// feed reads one chunk the terminal sent. A lone Escape at the end of a
// chunk is the Escape key: a terminal sends a key's sequence in one write.
func (r *keyReader) feed(b []byte) []key {
	buf := append(r.rest, b...)
	r.rest = nil
	var keys []key
	for len(buf) > 0 {
		if r.pasting {
			i := bytes.Index(buf, pasteEnd)
			if i < 0 {
				// Keep a tail that may be the start of the end mark.
				keep := min(len(buf), len(pasteEnd)-1)
				r.paste = append(r.paste, buf[:len(buf)-keep]...)
				r.rest = append([]byte(nil), buf[len(buf)-keep:]...)
				return keys
			}
			r.paste = append(r.paste, buf[:i]...)
			keys = append(keys, key{kind: keyPaste, text: string(bytes.ToValidUTF8(r.paste, []byte("?")))})
			r.pasting, r.paste = false, nil
			buf = buf[i+len(pasteEnd):]
			continue
		}
		c := buf[0]
		switch {
		case c == 0x1b:
			if len(buf) == 1 {
				keys = append(keys, key{kind: keyEscape})
				return keys
			}
			if bytes.HasPrefix(buf, pasteStart) {
				r.pasting = true
				buf = buf[len(pasteStart):]
				continue
			}
			if bytes.HasPrefix(pasteStart, buf) {
				// Half of the paste's mark: wait for the rest.
				r.rest = append([]byte(nil), buf...)
				return keys
			}
			n, k, ok := escape(buf)
			if n == 0 {
				r.rest = append([]byte(nil), buf...)
				return keys
			}
			if ok {
				keys = append(keys, k)
			}
			buf = buf[n:]
		case c == '\r' || c == '\n':
			keys = append(keys, key{kind: keyEnter})
			buf = buf[1:]
			// A terminal that sends CR LF for Enter means one Enter.
			if c == '\r' && len(buf) > 0 && buf[0] == '\n' {
				buf = buf[1:]
			}
		case c == 0x7f || c == 0x08:
			keys = append(keys, key{kind: keyBackspace})
			buf = buf[1:]
		case c == 0x03:
			keys = append(keys, key{kind: keyInterrupt})
			buf = buf[1:]
		case c == 0x04:
			keys = append(keys, key{kind: keyEnd})
			buf = buf[1:]
		case c == 0x15:
			keys = append(keys, key{kind: keyClearLine})
			buf = buf[1:]
		case c == 0x17:
			keys = append(keys, key{kind: keyDeleteWord})
			buf = buf[1:]
		case c == 0x0c:
			keys = append(keys, key{kind: keyRedraw})
			buf = buf[1:]
		case c == '\t':
			keys = append(keys, key{kind: keyRune, text: "\t"})
			buf = buf[1:]
		case c < 0x20:
			buf = buf[1:] // other control keys do nothing here
		default:
			if !utf8.FullRune(buf) {
				r.rest = append([]byte(nil), buf...)
				return keys
			}
			ch, n := utf8.DecodeRune(buf)
			if ch != utf8.RuneError {
				keys = append(keys, key{kind: keyRune, text: string(ch)})
			}
			buf = buf[n:]
		}
	}
	return keys
}

// escape reads the escape sequence at the start of b: how many bytes it
// takes, and the key, when it is one this program uses. n is 0 when b holds
// only part of a sequence.
func escape(b []byte) (n int, k key, ok bool) {
	switch b[1] {
	case 0x1b:
		// Escape twice, quickly.
		return 1, key{kind: keyEscape}, true
	case '[', 'O':
		// CSI or SS3: parameters, then a final byte from @ to ~.
		for i := 2; i < len(b); i++ {
			c := b[i]
			if c >= 0x40 && c <= 0x7e {
				switch string(b[i:i+1]) + string(b[1:2]) {
				case "A[", "AO":
					return i + 1, key{kind: keyUp}, true
				case "B[", "BO":
					return i + 1, key{kind: keyDown}, true
				}
				return i + 1, key{}, false
			}
		}
		return 0, key{}, false
	}
	// Alt and a key: neither does anything here.
	_, size := utf8.DecodeRune(b[1:])
	return 1 + size, key{}, false
}
