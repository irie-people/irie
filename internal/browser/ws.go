package browser

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The client end of a WebSocket (RFC 6455) that carries text messages, which
// is all WebDriver BiDi needs. Written from the RFC: the opening handshake of
// §4.1, the framing of §5.2, masking of §5.3, fragments of §5.4 and the
// control frames of §5.5.

// wsGUID is what §1.3 appends to the key before hashing it into
// Sec-WebSocket-Accept.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// maxMessage bounds one message from the browser. A full-page screenshot is
// the largest a call asks for.
const maxMessage = 64 << 20

const (
	opContinue = 0x0
	opText     = 0x1
	opBinary   = 0x2
	opClose    = 0x8
	opPing     = 0x9
	opPong     = 0xA
)

type wsConn struct {
	c   net.Conn
	r   *bufio.Reader
	wmu sync.Mutex // one frame at a time
}

// acceptKey is the Sec-WebSocket-Accept value for key (§4.2.2).
func acceptKey(key string) string {
	h := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h[:])
}

// dialWS opens ws://addr/path. It sends no Origin header: Firefox's Remote
// Agent refuses a connection whose origin it was not told to allow, and a
// client that is not a web page has none.
func dialWS(addr, path string, timeout time.Duration) (*wsConn, error) {
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	var k [16]byte
	rand.Read(k[:])
	key := base64.StdEncoding.EncodeToString(k[:])
	c.SetDeadline(time.Now().Add(timeout))
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(c, req); err != nil {
		c.Close()
		return nil, err
	}
	r := bufio.NewReader(c)
	resp, err := http.ReadResponse(r, &http.Request{Method: http.MethodGet})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("websocket handshake: %w", err)
	}
	// A 101 has no body, so what the server sends next stays in r.
	resp.Body.Close()
	switch {
	case resp.StatusCode != http.StatusSwitchingProtocols:
		c.Close()
		return nil, fmt.Errorf("websocket handshake: %s", resp.Status)
	case !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket"):
		c.Close()
		return nil, errors.New("websocket handshake: no Upgrade: websocket")
	case resp.Header.Get("Sec-WebSocket-Accept") != acceptKey(key):
		c.Close()
		return nil, errors.New("websocket handshake: wrong Sec-WebSocket-Accept")
	}
	c.SetDeadline(time.Time{})
	return &wsConn{c: c, r: r}, nil
}

// write sends one frame. A client masks every frame it sends (§5.3).
func (w *wsConn) write(op byte, p []byte) error {
	hdr := make([]byte, 0, 14)
	hdr = append(hdr, 0x80|op) // FIN: never fragmented
	switch n := len(p); {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 0x80|126)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(n))
	default:
		hdr = append(hdr, 0x80|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	var mask [4]byte
	rand.Read(mask[:])
	hdr = append(hdr, mask[:]...)
	frame := make([]byte, len(hdr)+len(p))
	copy(frame, hdr)
	for i, b := range p {
		frame[len(hdr)+i] = b ^ mask[i&3]
	}
	w.wmu.Lock()
	defer w.wmu.Unlock()
	_, err := w.c.Write(frame)
	return err
}

// WriteText sends one text message.
func (w *wsConn) WriteText(p []byte) error { return w.write(opText, p) }

// ReadMessage returns the next whole message, joining fragments, answering
// pings and passing over pongs on the way. A close from the server is
// answered and ends the connection with io.EOF.
func (w *wsConn) ReadMessage() ([]byte, error) {
	var msg []byte
	started := false
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0f
		masked := h[1]&0x80 != 0
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		// A server does not mask its frames; one that does is still read.
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.r, mask[:]); err != nil {
				return nil, err
			}
		}
		if n > maxMessage || uint64(len(msg))+n > maxMessage {
			return nil, errors.New("websocket: message too large")
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(w.r, p); err != nil {
			return nil, err
		}
		if masked {
			for i := range p {
				p[i] ^= mask[i&3]
			}
		}
		switch op {
		case opClose:
			// Echo the status code, as §5.5.1 asks.
			w.write(opClose, p[:min(len(p), 2)])
			return nil, io.EOF
		case opPing:
			if err := w.write(opPong, p); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opText, opBinary:
			if started {
				return nil, errors.New("websocket: a new message inside a fragmented one")
			}
			started, msg = true, p
		case opContinue:
			if !started {
				return nil, errors.New("websocket: a continuation with nothing to continue")
			}
			msg = append(msg, p...)
		default:
			return nil, fmt.Errorf("websocket: unknown opcode %#x", op)
		}
		if fin {
			return msg, nil
		}
	}
}

// Close sends a normal close (status 1000) and closes the connection.
func (w *wsConn) Close() error {
	w.write(opClose, []byte{0x03, 0xe8})
	return w.c.Close()
}
