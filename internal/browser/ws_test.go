package browser

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// wsServer is the server end of a WebSocket for tests: it answers the
// handshake, then hands each connection to serve.
type wsServer struct {
	ln     net.Listener
	accept func(key string) string // the Sec-WebSocket-Accept to send
	status int
}

func newWSServer(t *testing.T, serve func(c *testConn)) *wsServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &wsServer{ln: ln, accept: acceptKey, status: http.StatusSwitchingProtocols}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				req, err := http.ReadRequest(r)
				if err != nil {
					return
				}
				if req.Header.Get("Origin") != "" {
					t.Errorf("the client sent an Origin header: %q", req.Header.Get("Origin"))
				}
				key := req.Header.Get("Sec-WebSocket-Key")
				if s.status != http.StatusSwitchingProtocols {
					fmt.Fprintf(c, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\n\r\n", s.status, http.StatusText(s.status))
					return
				}
				io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+s.accept(key)+"\r\n\r\n")
				serve(&testConn{t: t, c: c, r: r})
			}()
		}
	}()
	return s
}

func (s *wsServer) addr() string { return s.ln.Addr().String() }

// testConn reads and writes frames as a server does: it reads masked
// frames and writes unmasked ones.
type testConn struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
}

// frame writes one frame with fin and op.
func (c *testConn) frame(fin bool, op byte, p []byte) {
	b0 := op
	if fin {
		b0 |= 0x80
	}
	hdr := []byte{b0}
	switch n := len(p); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	c.c.Write(append(hdr, p...))
}

// read returns the next frame from the client, which must be masked.
func (c *testConn) read() (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.r, h[:]); err != nil {
		return 0, nil, err
	}
	if h[1]&0x80 == 0 {
		return 0, nil, errors.New("a client frame without a mask")
	}
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		io.ReadFull(c.r, b[:])
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		io.ReadFull(c.r, b[:])
		n = binary.BigEndian.Uint64(b[:])
	}
	var mask [4]byte
	io.ReadFull(c.r, mask[:])
	p := make([]byte, n)
	if _, err := io.ReadFull(c.r, p); err != nil {
		return 0, nil, err
	}
	for i := range p {
		p[i] ^= mask[i&3]
	}
	return h[0] & 0x0f, p, nil
}

func TestAcceptKeyMatchesRFCExample(t *testing.T) {
	// RFC 6455 §1.3's worked example.
	if got := acceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("acceptKey = %q", got)
	}
}

func TestWebSocketMessagesFragmentsPingsAndClose(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 70000) // a 64-bit length
	pong := make(chan []byte, 1)
	s := newWSServer(t, func(c *testConn) {
		op, p, err := c.read()
		if err != nil || op != opText || string(p) != "hello" {
			t.Errorf("first frame: %v %d %q", err, op, p)
			return
		}
		c.frame(true, opPing, []byte("are you there"))
		if op, p, err := c.read(); err == nil && op == opPong {
			pong <- p
		}
		c.frame(false, opText, []byte("frag"))
		c.frame(true, opPong, nil) // a control frame between fragments
		c.frame(true, opContinue, []byte("ment"))
		c.frame(true, opText, big)
		c.frame(true, opClose, []byte{0x03, 0xe8})
		c.read() // the client's answering close
	})
	w, err := dialWS(s.addr(), "/session", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.WriteText([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	m, err := w.ReadMessage()
	if err != nil || string(m) != "fragment" {
		t.Fatalf("fragmented message = %q, %v", m, err)
	}
	select {
	case p := <-pong:
		if string(p) != "are you there" {
			t.Fatalf("pong carried %q", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no pong")
	}
	if m, err := w.ReadMessage(); err != nil || !bytes.Equal(m, big) {
		t.Fatalf("big message: %d bytes, %v", len(m), err)
	}
	if _, err := w.ReadMessage(); err != io.EOF {
		t.Fatalf("after close: %v, want io.EOF", err)
	}
}

func TestWebSocketHandshakeIsChecked(t *testing.T) {
	s := newWSServer(t, func(c *testConn) {})
	s.accept = func(string) string { return "wrong" }
	if _, err := dialWS(s.addr(), "/session", 5*time.Second); err == nil || !strings.Contains(err.Error(), "Sec-WebSocket-Accept") {
		t.Fatalf("a wrong accept key: %v", err)
	}
	s.status = http.StatusNotFound
	if _, err := dialWS(s.addr(), "/session", 5*time.Second); err == nil {
		t.Fatal("a 404 was taken as a WebSocket")
	}
}
