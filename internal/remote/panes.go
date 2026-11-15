package remote

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// Panes on the phone (docs/PHONE.md §2.2). remote.panes names the web
// panes of the plugins folder that phones reach, each at a port of its
// own. The gateway serves that port over HTTPS, with the app's own
// certificates, and passes each request on to the app behind the pane on
// this computer. So an app that listens only on 127.0.0.1 goes on a
// phone's Home Screen with no change to the app, no firewall question
// and nothing sent over the network unencrypted. The core says which panes
// are on, as it does for the Mac app's tabs: the gateway asks it with the
// plugins op, and passes nothing on for a pane that is off, gone or off
// this computer.

// paneLook is how long the gateway goes by what the plugins op last said
// about the panes, and paneStale how long it goes on by that while the
// core doesn't answer, as during an install. Tests shorten them.
var (
	paneLook  = 15 * time.Second
	paneStale = time.Minute
)

// paneState is why a pane's requests pass, or don't.
type paneState int

const (
	paneOn        paneState = iota // passed on to target
	paneGone                       // no manifest has it
	paneOff                        // its plugin or its part is off
	paneNotWeb                     // a view of the app's own, such as variorum
	paneElsewhere                  // its page is off this computer (remote = true)
)

// phonePane is one pane, as the gateway passes requests on to it.
type phonePane struct {
	title  string
	state  paneState
	target *url.URL // the app's origin on this computer, while on
}

// paneBook keeps what the plugins op last said about the panes.
type paneBook struct {
	mu   sync.Mutex
	at   time.Time
	byID map[string]phonePane
	ask  func() (ipc.PluginsResult, error)
}

// pane is how the pane id stands, asking the core when what it said is
// older than paneLook. One request asks at a time; the rest wait for it.
func (b *paneBook) pane(id string) (phonePane, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byID == nil || time.Since(b.at) >= paneLook {
		res, err := b.ask()
		switch {
		case err == nil:
			b.byID, b.at = readPanes(res), time.Now()
		case b.byID == nil || time.Since(b.at) >= paneStale:
			return phonePane{}, err
		}
	}
	if p, ok := b.byID[id]; ok {
		return p, nil
	}
	return phonePane{title: id, state: paneGone}, nil
}

// readPanes reads the panes out of the plugins op's answer. A pane is on
// while its part is, ready or needing something, as the Mac app draws its
// tab then.
func readPanes(res ipc.PluginsResult) map[string]phonePane {
	out := map[string]phonePane{}
	for _, pl := range res.Plugins {
		for _, part := range pl.Parts {
			x := part.Pane
			if x == nil || x.ID == "" {
				continue
			}
			p := phonePane{title: x.Title, state: paneOn}
			if p.title == "" {
				p.title = x.ID
			}
			switch {
			case x.Kind != ipc.PaneWeb:
				p.state = paneNotWeb
			case x.Remote:
				p.state = paneElsewhere
			case !pl.On || part.State != ipc.PartReady && part.State != ipc.PartNeeds:
				p.state = paneOff
			default:
				t, ok := loopbackOrigin(x.Origin)
				if !ok {
					p.state = paneElsewhere
				}
				p.target = t
			}
			out[x.ID] = p
		}
	}
	return out
}

// loopbackOrigin reads origin, a pane's, as an http or https origin on
// this computer: 127.0.0.1, ::1 or localhost, with no path. Nothing else
// is passed on, so remote.panes can't make the gateway reach another
// machine.
func loopbackOrigin(origin string) (*url.URL, bool) {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" {
		return nil, false
	}
	h := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(h); h != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, false
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host}, true
}

// paneKey keeps a request's paneTo in its context, for the proxy.
type paneKey struct{}

// paneTo is where one request goes: the app's origin, the origin the
// phone sees, and the pane's title for a page about it.
type paneTo struct {
	target *url.URL
	public string // such as https://studio.local:7390
	title  string
}

// Pane serves one pane of remote.panes on its port: each request from the
// local network, while the core says the pane is on, goes to the app
// behind it as the app would take it from a page of its own on this
// computer. The Host header is the app's own address. An Origin or a
// Referer of the address the phone sees becomes the app's, so the app's
// own check that a form came from its own pages holds, and a form from
// any other site still fails it. A redirect to the app's own address
// comes back as the phone's. The app's answers are otherwise its own: the
// gateway adds no header and keeps no cookie.
func (s *Server) Pane(id string) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite:        rewritePane,
		ModifyResponse: paneResponse,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			to, _ := r.Context().Value(paneKey{}).(paneTo)
			s.log.Warn("pane", "pane", id, "err", err)
			s.panePage(w, http.StatusBadGateway, to.title+" isn't answering on "+s.opt.Name+" just now. Start it there, then try again.")
		},
		ErrorLog: slog.NewLogLogger(s.log.Handler(), slog.LevelDebug),
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !localPeer(r.RemoteAddr) {
			s.refuse(w, r)
			return
		}
		p, err := s.panes.pane(id)
		if err != nil {
			s.panePage(w, http.StatusServiceUnavailable, s.coreDown(err).Msg)
			return
		}
		switch p.state {
		case paneGone:
			s.panePage(w, http.StatusNotFound, "No pane "+id+" is on "+s.opt.Name+": no manifest in "+brand.Name+"'s plugins folder there has it.")
			return
		case paneOff:
			s.panePage(w, http.StatusNotFound, p.title+" is off on "+s.opt.Name+": turn its plugin on there, in Settings › Plugins, and it opens here.")
			return
		case paneNotWeb:
			s.panePage(w, http.StatusNotFound, p.title+" is a view of "+brand.Name+"'s own, which only the Mac app shows.")
			return
		case paneElsewhere:
			s.panePage(w, http.StatusNotFound, p.title+"'s page isn't on "+s.opt.Name+", so the phone opens it at its own address.")
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/" {
			s.log.Info("pane", "pane", id, "from", r.RemoteAddr, "agent", r.UserAgent())
		}
		ctx := context.WithValue(r.Context(), paneKey{}, paneTo{target: p.target, public: "https://" + r.Host, title: p.title})
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rewritePane sends a request on to the app. ReverseProxy has already
// dropped the X-Forwarded headers the phone sent; SetXForwarded sets the
// gateway's own.
func rewritePane(pr *httputil.ProxyRequest) {
	to := pr.In.Context().Value(paneKey{}).(paneTo)
	pr.SetURL(to.target)
	pr.SetXForwarded()
	own := to.target.Scheme + "://" + to.target.Host
	if o := pr.In.Header.Get("Origin"); o != "" && strings.EqualFold(o, to.public) {
		pr.Out.Header.Set("Origin", own)
	}
	if ref := pr.In.Header.Get("Referer"); ref != "" {
		if rest, ok := cutOrigin(ref, to.public); ok {
			pr.Out.Header.Set("Referer", own+rest)
		}
	}
}

// paneResponse turns a redirect to the app's own address into one to the
// address the phone sees.
func paneResponse(res *http.Response) error {
	to, ok := res.Request.Context().Value(paneKey{}).(paneTo)
	if !ok {
		return nil
	}
	if loc := res.Header.Get("Location"); loc != "" {
		if rest, ok := cutOrigin(loc, to.target.Scheme+"://"+to.target.Host); ok {
			res.Header.Set("Location", to.public+rest)
		}
	}
	return nil
}

// cutOrigin is what follows origin at the start of s, when s is on that
// origin: "", or a path, query or fragment.
func cutOrigin(s, origin string) (string, bool) {
	if len(s) < len(origin) || !strings.EqualFold(s[:len(origin)], origin) {
		return "", false
	}
	rest := s[len(origin):]
	if rest != "" && !strings.ContainsAny(rest[:1], "/?#") {
		return "", false
	}
	return rest, true
}

// panePage is a page of the gateway's own about a pane, for the phone.
func (s *Server) panePage(w http.ResponseWriter, status int, msg string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light dark"><title>%s</title><style>body{font:17px/1.45 -apple-system,system-ui,sans-serif;margin:2em 1.5em;max-width:34em}</style><p>%s</p>`+"\n",
		html.EscapeString(brand.Name), html.EscapeString(msg))
}
