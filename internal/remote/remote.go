// Package remote is the phone app's gateway (docs/PHONE.md). `irie remote
// serve` serves the app over HTTPS on the machine's local network, and
// relays the core's ops between the phones paired with this machine and
// the core's socket. It is a client of the socket like any other
// (docs/CORE.md): it keeps no rules of its own, and lets a phone use only
// the ops in Ops. A setup page on the port below, over plain HTTP, hands
// out the profile that makes a phone trust Irie's certificate authority.
package remote

import (
	"bufio"
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

//go:embed web
var webFiles embed.FS

// Web is the phone app's files.
func Web() fs.FS {
	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	return sub
}

// Ops are the ops a phone may send: what a person does from the phone app.
// Left out are those that act on the machine's own screen (show, flip), on
// Irie's setup (config, theme, tool, local, preflight, move, trod) or that
// only agents send (hook, status).
var Ops = []string{
	ipc.OpHello, ipc.OpList, ipc.OpInput, ipc.OpComplete, ipc.OpConversation, ipc.OpSend,
	ipc.OpPark, ipc.OpUnpark, ipc.OpClose, ipc.OpReopen, ipc.OpUndo, ipc.OpRedo, ipc.OpRename,
	ipc.OpPriority, ipc.OpFreeze, ipc.OpThaw, ipc.OpRate, ipc.OpResult, ipc.OpRoute, ipc.OpDog,
	ipc.OpReorder, ipc.OpScreen, ipc.OpKeys,
}

// Options configure the gateway.
type Options struct {
	Socket  string // the core's socket
	Dir     string // where the authority, the devices and the pairing code live
	Listen  string // the address to listen on; "" = every address
	Port    int    // HTTPS; the setup page is on Port-1
	Name    string // what the phone calls this machine
	Version string
	Log     *slog.Logger
}

// Server is the gateway.
type Server struct {
	opt    Options
	id     string
	store  *Store
	ca     *Authority
	leaves *Leaves
	web    fs.FS
	log    *slog.Logger
	pusher *pusher
}

// New prepares a gateway: its authority, made on first use, and its
// machine id.
func New(opt Options) (*Server, error) {
	ca, err := LoadAuthority(opt.Dir)
	if err != nil {
		return nil, fmt.Errorf("the certificate authority: %w", err)
	}
	id, err := MachineID(opt.Dir)
	if err != nil {
		return nil, err
	}
	if opt.Name == "" {
		opt.Name = MachineName()
	}
	log := opt.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		opt: opt, id: id, store: &Store{Dir: opt.Dir}, ca: ca,
		leaves: &Leaves{CA: ca, Names: LocalNames, Addrs: Addrs},
		web:    Web(), log: log, pusher: newPusher(),
	}, nil
}

// Run serves until ctx ends: the app over HTTPS on the port, and the setup
// page over HTTP on the port below.
func (s *Server) Run(ctx context.Context) error {
	tlsLn, err := net.Listen("tcp", net.JoinHostPort(s.opt.Listen, strconv.Itoa(s.opt.Port)))
	if err != nil {
		return err
	}
	setupLn, err := net.Listen("tcp", net.JoinHostPort(s.opt.Listen, strconv.Itoa(s.opt.Port-1)))
	if err != nil {
		tlsLn.Close()
		return err
	}
	app := &http.Server{
		Handler:           s.App(),
		TLSConfig:         &tls.Config{GetCertificate: s.leaves.GetCertificate, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelDebug),
	}
	setup := &http.Server{Handler: s.Setup(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	errs := make(chan error, 2)
	go func() { errs <- app.ServeTLS(tlsLn, "", "") }()
	go func() { errs <- setup.Serve(setupLn) }()
	go s.notify(ctx)
	s.log.Info("serving", "app", tlsLn.Addr().String(), "setup", setupLn.Addr().String(), "name", s.opt.Name)
	select {
	case <-ctx.Done():
		err = nil
	case err = <-errs:
	}
	shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	app.Shutdown(shut)
	setup.Shutdown(shut)
	return err
}

// csp keeps the app to its own files. It talks to other machines' gateways
// over https, and to nothing else.
const csp = "default-src 'self'; connect-src 'self' https:; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; " +
	"manifest-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// App is the HTTPS side: the app's files and its API.
func (s *Server) App() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("POST /api/pair", s.pair)
	mux.HandleFunc("POST /api/call", s.authed(s.call))
	mux.HandleFunc("POST /api/subscribe", s.authed(s.subscribe))
	mux.HandleFunc("DELETE /api/device", s.authed(s.forgetMe))
	mux.HandleFunc("POST /api/push", s.authed(s.push))
	mux.HandleFunc("DELETE /api/push", s.authed(s.push))
	mux.HandleFunc("OPTIONS /api/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": ipc.Errorf(ipc.CodeNotFound, "no such call")})
	})
	mux.HandleFunc("GET /irie.mobileconfig", s.profile)
	mux.Handle("GET /", s.files("index.html"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// The app on one machine talks to the others' gateways. A
			// token, never a cookie, carries who it is, so any origin
			// may ask.
			h.Set("Access-Control-Allow-Origin", "*")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			h.Set("Access-Control-Max-Age", "600")
			h.Set("Cache-Control", "no-store")
		} else {
			h.Set("Content-Security-Policy", csp)
			if r.URL.Path == "/" {
				// The app opening: once per launch of the app.
				s.log.Info("app", "from", r.RemoteAddr, "agent", r.UserAgent())
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// Setup is the HTTP side: only the setup page and the profile, which a
// phone needs before it can trust the HTTPS side. No API is served here.
func (s *Server) Setup() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /setup.json", func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			host = h
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"name":        s.opt.Name,
			"https":       "https://" + net.JoinHostPort(host, strconv.Itoa(s.opt.Port)) + "/",
			"profile":     "/irie.mobileconfig",
			"fingerprint": s.ca.Fingerprint(),
		})
	})
	mux.HandleFunc("GET /irie.mobileconfig", s.profile)
	mux.HandleFunc("GET /irie-ca.cer", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Write(s.ca.Cert.Raw)
	})
	mux.Handle("GET /", s.files("setup.html"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Few requests come here, and each is a step of a phone's setup,
		// which the log then shows.
		s.log.Info("setup", "path", r.URL.Path, "from", r.RemoteAddr, "agent", r.UserAgent())
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", csp)
		mux.ServeHTTP(w, r)
	})
}

// files serves the app's files, with index for "/". Every file is checked
// for a newer version on each load; the service worker keeps them for when
// the machine can't be reached.
func (s *Server) files(index string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		switch name {
		case "":
			name = index
		case "favicon.ico":
			name = "icon-192.png"
		}
		data, err := fs.ReadFile(s.web, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		switch path.Ext(name) {
		case ".html":
			h.Set("Content-Type", "text/html; charset=utf-8")
		case ".js":
			h.Set("Content-Type", "text/javascript; charset=utf-8")
		case ".css":
			h.Set("Content-Type", "text/css; charset=utf-8")
		case ".webmanifest":
			h.Set("Content-Type", "application/manifest+json")
		case ".json":
			h.Set("Content-Type", "application/json")
		case ".png":
			h.Set("Content-Type", "image/png")
		case ".svg":
			h.Set("Content-Type", "image/svg+xml")
		}
		h.Set("Cache-Control", "no-cache")
		w.Write(data)
	})
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-apple-aspen-config")
	w.Header().Set("Content-Disposition", `attachment; filename="Irie.mobileconfig"`)
	w.Write(s.ca.Profile())
}

// Info is what any phone may know of this machine before it pairs.
type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	API     int    `json:"api"`
	OS      string `json:"os"`
	Push    bool   `json:"push"` // it sends notifications
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Info{ID: s.id, Name: s.opt.Name, Version: s.opt.Version, API: ipc.API, OS: runtime.GOOS, Push: true})
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code   string `json:"code"`
		Device string `json:"device"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": ipc.Errorf(ipc.CodeBadRequest, "send the code as JSON")})
		return
	}
	token, d, err := s.store.Pair(req.Code, req.Device)
	var pe *PairError
	switch {
	case errors.As(err, &pe):
		status := http.StatusForbidden
		if pe == ErrTooMany {
			status = http.StatusTooManyRequests
		}
		s.log.Warn("pair refused", "from", r.RemoteAddr, "why", pe.Code)
		writeJSON(w, status, map[string]any{"error": pe})
		return
	case err != nil:
		s.log.Error("pair", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": ipc.Errorf(ipc.CodeInternal, "%v", err)})
		return
	}
	s.log.Info("paired", "device", d.ID, "name", d.Name, "from", r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "device": d.ID, "machine": map[string]string{"id": s.id, "name": s.opt.Name}})
}

// authed lets through only a request from a paired phone.
func (s *Server) authed(h func(http.ResponseWriter, *http.Request, Device)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		d, err := s.store.Device(strings.TrimSpace(token))
		var pe *PairError
		if errors.As(err, &pe) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": pe})
			return
		}
		if err != nil {
			s.log.Error("devices", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": ipc.Errorf(ipc.CodeInternal, "%v", err)})
			return
		}
		s.store.Saw(d)
		h(w, r, d)
	}
}

func (s *Server) forgetMe(w http.ResponseWriter, r *http.Request, d Device) {
	if _, err := s.store.Forget(d.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": ipc.Errorf(ipc.CodeInternal, "%v", err)})
		return
	}
	s.log.Info("forgot", "device", d.ID, "name", d.Name)
	w.WriteHeader(http.StatusNoContent)
}

// coreDown is what a phone hears when the core does not answer, as for the
// few seconds an install restarts it.
func (s *Server) coreDown(err error) *ipc.Error {
	s.log.Warn("core", "err", err)
	return &ipc.Error{Code: "core_down", Msg: fmt.Sprintf("Irie isn't answering on %s just now; it restarts on its own within a few seconds.", s.opt.Name)}
}

// call relays one op to the core and its reply back, as the core wrote it.
func (s *Server) call(w http.ResponseWriter, r *http.Request, d Device) {
	var req struct {
		Op   string          `json:"op"`
		Args json.RawMessage `json:"args"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": ipc.Errorf(ipc.CodeBadRequest, "send {op, args} as JSON")})
		return
	}
	if !slices.Contains(Ops, req.Op) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": ipc.Errorf(ipc.CodeUnknownOp, "the phone app can't %s", req.Op)})
		return
	}
	if len(req.Args) == 0 || string(req.Args) == "null" {
		req.Args = json.RawMessage(`{}`)
	}
	c, err := ipc.Dial(s.opt.Socket, 2*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": s.coreDown(err)})
		return
	}
	defer c.Close()
	c.Timeout = 2 * time.Minute
	var res json.RawMessage
	err = c.Call(req.Op, req.Args, &res)
	var e *ipc.Error
	switch {
	case errors.As(err, &e):
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": e})
		return
	case err != nil:
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": s.coreDown(err)})
		return
	}
	if req.Op == ipc.OpHello {
		res = s.hello(res)
	}
	if len(res) == 0 {
		res = json.RawMessage(`{}`)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}

// hello names only the features a phone may use, so it offers no more.
func (s *Server) hello(raw json.RawMessage) json.RawMessage {
	var h ipc.HelloResult
	if json.Unmarshal(raw, &h) != nil {
		return raw
	}
	h.Features = slices.DeleteFunc(h.Features, func(f string) bool { return !slices.Contains(Ops, f) })
	b, err := json.Marshal(h)
	if err != nil {
		return raw
	}
	return b
}

// pingEvery keeps a phone's stream from looking dead while nothing changes.
const pingEvery = 15 * time.Second

// subscribe relays a subscription: the core's reply as the first line,
// then its frames as it writes them, and a ping when it is quiet.
func (s *Server) subscribe(w http.ResponseWriter, r *http.Request, d Device) {
	var args struct {
		Session      string `json:"session,omitempty"`
		Agent        string `json:"agent,omitempty"`
		Conversation bool   `json:"conversation,omitempty"`
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &args); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": ipc.Errorf(ipc.CodeBadRequest, "send the subscription's args as JSON")})
			return
		}
	}
	h := w.Header()
	h.Set("Content-Type", "application/x-ndjson")
	h.Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	line := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return s.writeLine(w, rc, b)
	}

	conn, err := net.DialTimeout("unix", s.opt.Socket, 2*time.Second)
	if err != nil {
		line(map[string]any{"ok": false, "error": s.coreDown(err)})
		return
	}
	defer conn.Close()
	req, _ := json.Marshal(map[string]any{"id": 1, "op": ipc.OpSubscribe, "args": args})
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write(append(req, '\n')); err != nil {
		line(map[string]any{"ok": false, "error": s.coreDown(err)})
		return
	}
	rd := bufio.NewReaderSize(conn, 64<<10)
	first, err := readLine(rd)
	if err != nil {
		line(map[string]any{"ok": false, "error": s.coreDown(err)})
		return
	}
	var rep ipc.Reply
	if err := json.Unmarshal(first, &rep); err != nil || !rep.OK {
		if rep.Error == nil {
			rep.Error = s.coreDown(fmt.Errorf("subscribe: %s", first))
		}
		line(map[string]any{"ok": false, "error": rep.Error})
		return
	}
	conn.SetDeadline(time.Time{})
	if line(map[string]any{"ok": true}) != nil {
		return
	}

	frames := make(chan []byte, 16)
	go func() {
		defer close(frames)
		for {
			f, err := readLine(rd)
			if err != nil {
				return
			}
			select {
			case frames <- f:
			case <-r.Context().Done():
				return
			}
		}
	}()
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case f, ok := <-frames:
			if !ok || s.writeLine(w, rc, f) != nil {
				return
			}
		case <-ping.C:
			if s.writeLine(w, rc, []byte(`{"t":"ping"}`)) != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// writeLine writes one line of a stream and flushes it, giving up on a
// phone that has not taken it in 30 s.
func (s *Server) writeLine(w http.ResponseWriter, rc *http.ResponseController, b []byte) error {
	rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := w.Write(append(b[:len(b):len(b)], '\n')); err != nil {
		return err
	}
	return rc.Flush()
}

// readLine reads one line of the core's socket, up to ipc.MaxMessage.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		switch {
		case err == nil:
			return line[:len(line)-1], nil
		case err != bufio.ErrBufferFull:
			return nil, err
		case len(line) > ipc.MaxMessage:
			return nil, errors.New("the core sent a line too long")
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// MachineName is what a phone calls this machine when remote.name is
// empty: on macOS its computer name, as Sharing settings show it, else its
// host name without a .local.
func MachineName() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("scutil", "--get", "ComputerName").Output(); err == nil {
			if n := strings.TrimSpace(string(out)); n != "" {
				return n
			}
		}
	}
	h, _ := os.Hostname()
	h = strings.TrimSuffix(h, ".local")
	if h == "" {
		h = "this machine"
	}
	return h
}

var (
	namesOnce sync.Once
	names     []string
)

// LocalNames are the names a phone may reach this machine by: its
// Bonjour name, name.local, as macOS's local host name or Avahi's host name
// give it.
func LocalNames() []string {
	namesOnce.Do(func() {
		var hs []string
		if runtime.GOOS == "darwin" {
			if out, err := exec.Command("scutil", "--get", "LocalHostName").Output(); err == nil {
				hs = append(hs, strings.TrimSpace(string(out)))
			}
		}
		if h, err := os.Hostname(); err == nil {
			hs = append(hs, h)
		}
		for _, h := range hs {
			h = strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(h, "."), ".local"))
			if h != "" && !strings.Contains(h, ".") && !slices.Contains(names, h+".local") {
				names = append(names, h+".local")
			}
		}
	})
	return names
}

// Addrs are this machine's addresses on the networks it is up on.
func Addrs() []net.IP {
	var out []net.IP
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 {
			continue
		}
		as, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok {
				out = append(out, n.IP)
			}
		}
	}
	return out
}

// LANAddr is this machine's address on its local network: an IPv4
// address of a network interface that can broadcast, as Wi-Fi and Ethernet
// can, and is not a VPN's tunnel or a container's bridge. A VPN such as
// Mullvad also gives a private address, which no phone can reach.
func LANAddr() net.IP {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	virtual := []string{"docker", "br-", "veth", "virbr", "bridge", "utun", "wg", "tun", "tap", "awdl", "llw", "anpi", "vmnet", "vboxnet", "lxc", "cni", "flannel", "tailscale", "zt"}
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagBroadcast == 0 || i.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 ||
			slices.ContainsFunc(virtual, func(p string) bool { return strings.HasPrefix(i.Name, p) }) {
			continue
		}
		as, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok {
				if ip4 := n.IP.To4(); ip4 != nil && localIP(ip4) && !ip4.IsLinkLocalUnicast() {
					return ip4
				}
			}
		}
	}
	return nil
}
