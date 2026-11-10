package anon

// The forwarder (DESIGN §4.16). An anonymous session's shell runs in Claude
// Code's sandbox, which lets its programs connect to nothing but two ports
// on 127.0.0.1: a SOCKS5 proxy and an HTTP proxy, which `irie anon mcp`
// holds for as long as the session lives. Each carries a connection on
// through the compartment's exit, a Mullvad relay's SOCKS5 proxy or a
// SocksPort of Irie's Tor, with its name looked up at the exit. So a
// program that ignores proxy settings can't leave at all, and the shell
// sees only those local ports, never the exit.

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// closedPorts are the ports the forwarder refuses: signing in (SSH,
// telnet, remote desktops) and sending mail or messages. Anonymous
// research does neither, and either would tie one of the user's own
// accounts to the compartment's exit.
var closedPorts = map[int]bool{
	22: true, 23: true, 25: true, 110: true, 143: true, 465: true, 587: true, 993: true, 995: true,
	2525: true, 3389: true, 5222: true, 5223: true, 5900: true, 6667: true, 6697: true,
}

// handshake is how long a client or the exit may take over a proxy's
// opening words.
const handshake = 30 * time.Second

// Forwarder carries the shell's connections on through a compartment's
// exit.
type Forwarder struct {
	Exit string // the exit's SOCKS5 proxy, host:port
	// Dial reaches the exit; nil dials it with a net.Dialer.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Upstream is where a compartment's forwarder sends its connections: the
// exit's own SOCKS5 proxy.
func (c Compartment) Upstream() string {
	if c.Route == Tor {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(c.TorPort))
	}
	return SocksAddr(c.Relay)
}

// serveConns answers on ln with handle, each connection in a goroutine of its
// own, until ln is closed.
func serveConns(ln net.Listener, handle func(net.Conn)) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go handle(c)
	}
}

// refused says why the forwarder won't carry a connection to host and
// port, or "" when it will.
func refused(host string, port int) string {
	if closedPorts[port] {
		return fmt.Sprintf("port %d is for signing in or sending messages, which anonymous research doesn't do", port)
	}
	name := strings.ToLower(strings.TrimSuffix(host, "."))
	if name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return name + " is this machine, which the exit can't reach"
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return host + " is a private address, which the exit can't reach"
	}
	return ""
}

// socksReplies are SOCKS5's reply codes (RFC 1928 §6), in words.
var socksReplies = map[byte]string{
	1: "it failed", 2: "its rules don't allow it", 3: "the network is unreachable", 4: "the host is unreachable",
	5: "the host refused the connection", 6: "its time to live ran out", 7: "the command isn't supported", 8: "the address type isn't supported",
}

// connect opens a connection to host and port through the exit, which
// looks host up. code is the SOCKS5 reply to pass on when it fails. Its
// errors name no address: not the exit's, which an agent is never told.
func (f Forwarder) connect(ctx context.Context, host string, port int) (net.Conn, byte, error) {
	dial := f.Dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: handshake}).DialContext
	}
	up, err := dial(ctx, "tcp", f.Exit)
	if err != nil {
		return nil, 1, errors.New("the exit didn't answer (is Mullvad VPN connected, or Irie's Tor running?)")
	}
	req := []byte{5, 1, 0}
	if ip := net.ParseIP(host); ip == nil {
		if len(host) == 0 || len(host) > 255 {
			up.Close()
			return nil, 1, fmt.Errorf("%q is not a host's name", host)
		}
		req = append(append(req, 3, byte(len(host))), host...)
	} else if ip4 := ip.To4(); ip4 != nil {
		req = append(append(req, 1), ip4...)
	} else {
		req = append(append(req, 4), ip.To16()...)
	}
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	up.SetDeadline(time.Now().Add(handshake))
	// The greeting, with no authentication, and its answer; then the
	// request and its answer.
	var r [5]byte
	if _, err := up.Write([]byte{5, 1, 0}); err != nil {
		up.Close()
		return nil, 1, errors.New("the exit closed the connection")
	}
	if _, err := io.ReadFull(up, r[:2]); err != nil || r[0] != 5 || r[1] != 0 {
		up.Close()
		return nil, 1, errors.New("the exit's proxy refused to talk")
	}
	if _, err := up.Write(req); err != nil {
		up.Close()
		return nil, 1, errors.New("the exit closed the connection")
	}
	if _, err := io.ReadFull(up, r[:5]); err != nil || r[0] != 5 {
		up.Close()
		return nil, 1, errors.New("the exit's proxy gave no answer")
	}
	if r[1] != 0 {
		up.Close()
		words := socksReplies[r[1]]
		if words == "" {
			words = fmt.Sprintf("it answered %d", r[1])
		}
		return nil, r[1], fmt.Errorf("the exit couldn't reach %s: %s", net.JoinHostPort(host, strconv.Itoa(port)), words)
	}
	// The address the exit's proxy bound, which is no business of the
	// shell's: r[3] is its type, and r[4] the first byte of it.
	rest := map[byte]int{1: 4 + 2 - 1, 4: 16 + 2 - 1, 3: int(r[4]) + 2}[r[3]]
	if rest == 0 {
		up.Close()
		return nil, 1, errors.New("the exit's proxy gave an answer it can't be read by")
	}
	if _, err := io.ReadFull(up, make([]byte, rest)); err != nil {
		up.Close()
		return nil, 1, errors.New("the exit's proxy gave no answer")
	}
	up.SetDeadline(time.Time{})
	return up, 0, nil
}

// SOCKS answers one SOCKS5 client (RFC 1928): CONNECT alone, no
// authentication, and the address passed on to the exit as it came, a
// name looked up there.
func (f Forwarder) SOCKS(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(handshake))
	br := bufio.NewReader(c)
	var b [4]byte
	if _, err := io.ReadFull(br, b[:2]); err != nil || b[0] != 5 {
		return
	}
	methods := make([]byte, b[1])
	if _, err := io.ReadFull(br, methods); err != nil {
		return
	}
	if !slices.Contains(methods, 0) {
		c.Write([]byte{5, 0xff})
		return
	}
	c.Write([]byte{5, 0})
	if _, err := io.ReadFull(br, b[:4]); err != nil || b[0] != 5 {
		return
	}
	reply := func(code byte) { c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0}) }
	var host string
	switch b[3] {
	case 1, 4:
		ip := make([]byte, map[byte]int{1: 4, 4: 16}[b[3]])
		if _, err := io.ReadFull(br, ip); err != nil {
			return
		}
		host = net.IP(ip).String()
	case 3:
		n, err := br.ReadByte()
		if err != nil {
			return
		}
		name := make([]byte, n)
		if _, err := io.ReadFull(br, name); err != nil {
			return
		}
		host = string(name)
	default:
		reply(8)
		return
	}
	var p [2]byte
	if _, err := io.ReadFull(br, p[:]); err != nil {
		return
	}
	port := int(binary.BigEndian.Uint16(p[:]))
	if b[1] != 1 {
		reply(7)
		return
	}
	if refused(host, port) != "" {
		reply(2)
		return
	}
	up, code, err := f.connect(context.Background(), host, port)
	if err != nil {
		reply(code)
		return
	}
	defer up.Close()
	reply(0)
	c.SetDeadline(time.Time{})
	splice(c, br, up)
}

// HTTP answers one HTTP proxy client: CONNECT, which most programs send
// for https, or a plain http:// request in absolute form, one to a
// connection.
func (f Forwarder) HTTP(c net.Conn) {
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(handshake))
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	c.SetReadDeadline(time.Time{})
	host, port := req.URL.Hostname(), req.URL.Port()
	switch {
	case req.Method == http.MethodConnect:
		host, port, err = net.SplitHostPort(req.Host)
		if err != nil {
			answer(c, http.StatusBadRequest, "CONNECT needs a host and port")
			return
		}
	case req.URL.Scheme != "http" || host == "":
		answer(c, http.StatusBadRequest, "this proxy takes CONNECT, or an http:// address in full")
		return
	case port == "":
		port = "80"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 || n > 65535 {
		answer(c, http.StatusBadRequest, port+" is not a port")
		return
	}
	if why := refused(host, n); why != "" {
		answer(c, http.StatusForbidden, why)
		return
	}
	up, _, err := f.connect(req.Context(), host, n)
	if err != nil {
		answer(c, http.StatusBadGateway, err.Error())
		return
	}
	defer up.Close()
	if req.Method == http.MethodConnect {
		io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
		splice(c, br, up)
		return
	}
	hopByHop(req.Header)
	req.RequestURI, req.Close = "", true
	if err := req.Write(up); err != nil {
		answer(c, http.StatusBadGateway, "the request didn't get through the exit")
		return
	}
	resp, err := http.ReadResponse(bufio.NewReader(up), req)
	if err != nil {
		answer(c, http.StatusBadGateway, "no answer came back through the exit")
		return
	}
	defer resp.Body.Close()
	hopByHop(resp.Header)
	resp.Close = true
	resp.Write(c)
}

// hopByHop takes out the headers that belong to one connection, not to
// the request or response (RFC 9110 §7.6.1).
func hopByHop(h http.Header) {
	for _, f := range h.Values("Connection") {
		for _, k := range strings.Split(f, ",") {
			h.Del(strings.TrimSpace(k))
		}
	}
	for _, k := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(k)
	}
}

// answer is the proxy's own response, with words that name no address.
func answer(c net.Conn, code int, words string) {
	fmt.Fprintf(c, "HTTP/1.1 %d %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s\n",
		code, http.StatusText(code), len(words)+1, words)
}

// splice copies both ways between a client, whose reader may hold bytes
// already read, and up, until both are done.
func splice(c net.Conn, br *bufio.Reader, up net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(up, br)
		closeWrite(up)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(c, up)
		closeWrite(c)
		done <- struct{}{}
	}()
	<-done
	<-done
}

func closeWrite(c net.Conn) {
	if w, ok := c.(interface{ CloseWrite() error }); ok {
		w.CloseWrite()
		return
	}
	c.Close()
}
