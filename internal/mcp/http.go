package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
)

// MaxBody bounds a message a client posts. A tool's arguments are small:
// the most an agent sends is text it types or a script it runs.
const MaxBody = 4 << 20

// HTTP serves one endpoint of MCP's Streamable HTTP transport (MCP
// 2025-11-25, "Transports"), statelessly: it gives no session ID and
// answers each request with one JSON object, never an SSE stream, so a
// client of a server that restarted carries on with the one after it.
// Pick names the server a request reaches, by its credentials, or refuses
// it with an HTTP status. Calls run under Context, the server's own life,
// rather than the request's: the transport says a connection that drops
// is not a cancellation.
type HTTP struct {
	Pick    func(r *http.Request) (Server, int)
	Context context.Context
}

// ServeHTTP answers a POST of one JSON-RPC message. GET, which would open
// a stream of the server's own messages, and DELETE, which would end a
// session, are refused with 405, as the transport allows a server that
// sends no messages of its own and keeps no sessions. Following the
// transport's security warning, only a request addressed to this computer
// by its loopback name, from no other origin, is answered, which keeps a
// web page that renamed itself to 127.0.0.1 (DNS rebinding) out.
func (h HTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !Loopback(r) {
		refuse(w, http.StatusForbidden, "only this computer's own clients may call")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		refuse(w, http.StatusMethodNotAllowed, "POST one JSON-RPC message")
		return
	}
	s, status := h.Pick(r)
	if status != 0 {
		refuse(w, status, http.StatusText(status))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			refuse(w, http.StatusRequestEntityTooLarge, "the message is too large")
			return
		}
		refuse(w, http.StatusBadRequest, err.Error())
		return
	}
	var env struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write(encode(json.RawMessage("null"), nil, &rpcError{CodeParse, "parse error: " + err.Error()}))
		return
	}
	// A notification, or a response to a request of the server's, of
	// which it makes none: accepted, with nothing to answer.
	if env.Method == "" || len(env.ID) == 0 || string(env.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	ctx := h.Context
	if ctx == nil {
		ctx = context.Background()
	}
	reply := s.Reply(ctx, body)
	w.Header().Set("Content-Type", "application/json")
	w.Write(reply)
}

// refuse answers with status and a JSON-RPC error that has no id, as the
// transport has it.
func refuse(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(encode(json.RawMessage("null"), nil, &rpcError{-32600, message}))
}

// Loopback reports whether a request is addressed to this computer by a
// loopback name or address, and comes from no other origin: a page in a
// browser sends its own Origin, which names its site.
func Loopback(r *http.Request) bool {
	if !loopbackHost(r.Host) {
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && loopbackHost(u.Host)
}

func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
