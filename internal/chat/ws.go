//go:build !nochat

package chat

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/version"
)

// The client end of a WebSocket (RFC 6455) that carries text messages,
// over TLS, which is what Discord's Gateway and Slack's Socket Mode speak.
// Irie's browser has its own, for ws:// to a browser on this computer
// (internal/browser/ws.go), in a plugin a build may leave out; this one adds
// TLS, and says how the server closed. Written from the RFC: the opening
// handshake of §4.1, the framing of §5.2, masking of §5.3, fragments of
// §5.4 and the control frames of §5.5.

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// wsMax bounds one message: Discord's first events about a server carry
// its channels.
const wsMax = 32 << 20

const (
	opContinue = 0x0
	opText     = 0x1
	opBinary   = 0x2
	opClose    = 0x8
	opPing     = 0x9
	opPong     = 0xA
)

// Closed is how the server ended a connection: the status code of its
// close frame, such as Discord's 4004, and its reason.
type Closed struct {
	Code   int
	Reason string
}

func (e *Closed) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("closed with %d: %s", e.Code, e.Reason)
	}
	return fmt.Sprintf("closed with %d", e.Code)
}

type wsConn struct {
	c   net.Conn
	r   *bufio.Reader
	wmu sync.Mutex
}

func acceptKey(key string) string {
	h := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h[:])
}

// dialWS opens a WebSocket to raw, wss:// over TLS 1.2 or later with the
// system's roots, or ws:// as a test's server is.
func dialWS(ctx context.Context, raw string) (*wsConn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	host := u.Host
	switch u.Scheme {
	case "wss":
		if u.Port() == "" {
			host = net.JoinHostPort(u.Hostname(), "443")
		}
	case "ws":
		if u.Port() == "" {
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	default:
		return nil, fmt.Errorf("%q isn't a WebSocket's address", raw)
	}
	d := net.Dialer{Timeout: 20 * time.Second}
	var c net.Conn
	if u.Scheme == "wss" {
		c, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", host)
	} else {
		c, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, err
	}
	var k [16]byte
	rand.Read(k[:])
	key := base64.StdEncoding.EncodeToString(k[:])
	c.SetDeadline(time.Now().Add(20 * time.Second))
	req := "GET " + u.RequestURI() + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"User-Agent: " + brand.Slug + "/" + version.Base + "\r\n" +
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
	// The connection ends with ctx.
	go func() {
		<-ctx.Done()
		c.Close()
	}()
	return &wsConn{c: c, r: r}, nil
}

// write sends one frame, masked, as a client masks every frame (§5.3).
func (w *wsConn) write(op byte, p []byte) error {
	hdr := make([]byte, 0, 14)
	hdr = append(hdr, 0x80|op)
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
	w.c.SetWriteDeadline(time.Now().Add(20 * time.Second))
	_, err := w.c.Write(frame)
	return err
}

// WriteText sends one text message.
func (w *wsConn) WriteText(p []byte) error { return w.write(opText, p) }

// ReadMessage returns the next whole message, joining fragments, answering
// pings on the way. A close from the server is answered and ends the
// connection with a *Closed.
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
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.r, mask[:]); err != nil {
				return nil, err
			}
		}
		if n > wsMax || uint64(len(msg))+n > wsMax {
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
			w.write(opClose, p[:min(len(p), 2)])
			e := &Closed{Code: 1005}
			if len(p) >= 2 {
				e.Code = int(binary.BigEndian.Uint16(p[:2]))
				e.Reason = Line(string(p[2:]), 120)
			}
			w.c.Close()
			return nil, e
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

// CloseWith sends a close with code, such as 4000 to keep a Discord session
// resumable, and closes the connection.
func (w *wsConn) CloseWith(code int) error {
	w.write(opClose, binary.BigEndian.AppendUint16(nil, uint16(code)))
	return w.c.Close()
}
