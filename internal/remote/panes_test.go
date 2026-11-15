package remote

import (
	"bufio"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// shop is the plugins op's answer with one plugin, shop, whose part till
// has the pane x.
func shop(on bool, state string, x ipc.PluginPane) ipc.PluginsResult {
	if x.ID == "" {
		x.ID = "shop.till"
	}
	if x.Title == "" {
		x.Title = "Till"
	}
	return ipc.PluginsResult{Plugins: []ipc.Plugin{{
		Name: "shop", Title: "Shop", On: on, Manifest: "/home/alice/.config/irie/plugins/shop.toml",
		Parts: []ipc.PluginPart{{Name: "till", Title: "Till", State: state, Pane: &x}},
	}}}
}

// seenRequest is what the app behind a pane saw of one request.
type seenRequest struct {
	Host, Origin, Referer, Proto, ForHost, For string
}

// till is the app behind the pane: it notes what each request carried,
// redirects to its own address and elsewhere, sets a cookie, and streams
// events until told to stop.
func till(t *testing.T) (*httptest.Server, func() []seenRequest, chan struct{}) {
	t.Helper()
	var mu sync.Mutex
	var seen []seenRequest
	stop := make(chan struct{})
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, seenRequest{r.Host, r.Header.Get("Origin"), r.Header.Get("Referer"), r.Header.Get("X-Forwarded-Proto"), r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-For")})
		mu.Unlock()
		switch r.URL.Path {
		case "/go":
			http.Redirect(w, r, "http://"+r.Host+"/next?x=1", http.StatusSeeOther)
		case "/away":
			http.Redirect(w, r, "https://example.com/", http.StatusFound)
		case "/events":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: first\n\n")
			w.(http.Flusher).Flush()
			<-stop
		default:
			http.SetCookie(w, &http.Cookie{Name: "till_session", Value: "s1", Path: "/", HttpOnly: true})
			fmt.Fprint(w, "the till's page")
		}
	}))
	t.Cleanup(app.Close)
	return app, func() []seenRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]seenRequest(nil), seen...)
	}, stop
}

func TestAPaneReachesItsAppAsItsOwnPagesWould(t *testing.T) {
	app, seen, stop := till(t)
	defer close(stop)
	appHost := strings.TrimPrefix(app.URL, "http://")
	_, srv, _ := gateway(t)
	srv.panes.ask = func() (ipc.PluginsResult, error) {
		return shop(true, ipc.PartReady, ipc.PluginPane{Kind: ipc.PaneWeb, Address: app.URL + "/", Origin: app.URL}), nil
	}
	ts := httptest.NewTLSServer(srv.Pane("shop.till"))
	t.Cleanup(ts.Close)
	public := ts.URL
	client := ts.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	do := func(method, path string, header map[string]string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, public+path, strings.NewReader("a=1"))
		for k, v := range header {
			req.Header.Set(k, v)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	last := func() seenRequest {
		t.Helper()
		s := seen()
		if len(s) == 0 {
			t.Fatal("the app saw nothing")
		}
		return s[len(s)-1]
	}

	res := do("GET", "/", map[string]string{"X-Forwarded-For": "203.0.113.9", "X-Forwarded-Host": "example.com"})
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "the till's page" || !strings.Contains(res.Header.Get("Set-Cookie"), "till_session=s1") {
		t.Fatalf("the page: %s %q %v", res.Status, body, res.Header)
	}
	if s := last(); s.Host != appHost || s.Proto != "https" || s.ForHost != strings.TrimPrefix(public, "https://") || s.For != "127.0.0.1" {
		t.Errorf("the app saw %+v: its own Host, and the gateway's X-Forwarded headers, never the phone's", s)
	}

	// A form from the app's own pages comes from the app's own address,
	// as far as the app can tell; a form from another site doesn't.
	do("POST", "/sell", map[string]string{"Origin": public, "Referer": public + "/sell?item=2", "Content-Type": "application/x-www-form-urlencoded"})
	if s := last(); s.Origin != "http://"+appHost || s.Referer != "http://"+appHost+"/sell?item=2" {
		t.Errorf("a form of the app's own: %+v", s)
	}
	do("POST", "/sell", map[string]string{"Origin": "https://example.com", "Referer": "https://example.com/x"})
	if s := last(); s.Origin != "https://example.com" || s.Referer != "https://example.com/x" {
		t.Errorf("a form from another site must reach the app as it came: %+v", s)
	}
	do("POST", "/sell", map[string]string{"Origin": public + ".example.com"})
	if s := last(); s.Origin != public+".example.com" {
		t.Errorf("an origin that only starts like the phone's: %+v", s)
	}

	// A redirect to the app's own address comes back as the phone's; one
	// elsewhere stays.
	if res := do("GET", "/go", nil); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != public+"/next?x=1" {
		t.Errorf("a redirect to the app itself: %s %q", res.Status, res.Header.Get("Location"))
	}
	if res := do("GET", "/away", nil); res.Header.Get("Location") != "https://example.com/" {
		t.Errorf("a redirect elsewhere: %q", res.Header.Get("Location"))
	}

	// Events come as the app sends them, not when the stream ends.
	res = do("GET", "/events", nil)
	got := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(res.Body).ReadString('\n')
		got <- line
	}()
	select {
	case line := <-got:
		if line != "data: first\n" {
			t.Errorf("the stream's first line: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the app's event never came through")
	}
}

func TestAPanePassesNothingOnWhileItIsntOn(t *testing.T) {
	app, seen, stop := till(t)
	defer close(stop)
	_, srv, _ := gateway(t)
	web := ipc.PluginPane{Kind: ipc.PaneWeb, Address: app.URL + "/", Origin: app.URL}
	for _, c := range []struct {
		name   string
		res    ipc.PluginsResult
		err    error
		status int
		want   string
	}{
		{"its plugin off", shop(false, ipc.PartPluginOff, web), nil, http.StatusNotFound, "Till is off on Studio: turn its plugin on there"},
		{"its part off", shop(true, ipc.PartOff, web), nil, http.StatusNotFound, "Till is off on Studio"},
		{"no such pane", shop(true, ipc.PartReady, ipc.PluginPane{ID: "shop.other", Kind: ipc.PaneWeb, Origin: app.URL}), nil, http.StatusNotFound, "No pane shop.till is on Studio"},
		{"a page elsewhere", shop(true, ipc.PartReady, ipc.PluginPane{Kind: ipc.PaneWeb, Origin: "https://till.example.com", Remote: true}), nil, http.StatusNotFound, "Till's page isn't on Studio"},
		{"a page that only says it is here", shop(true, ipc.PartReady, ipc.PluginPane{Kind: ipc.PaneWeb, Origin: "http://10.0.0.9:8080"}), nil, http.StatusNotFound, "Till's page isn't on Studio"},
		{"a view of the app's own", shop(true, ipc.PartReady, ipc.PluginPane{Kind: "variorum"}), nil, http.StatusNotFound, "only the Mac app shows"},
		{"the core down", ipc.PluginsResult{}, errors.New("dial unix: no such file"), http.StatusServiceUnavailable, "isn't answering on Studio just now"},
		{"the app down", shop(true, ipc.PartNeeds, ipc.PluginPane{Kind: ipc.PaneWeb, Origin: "http://127.0.0.1:1"}), nil, http.StatusBadGateway, "Till isn't answering on Studio just now. Start it there"},
	} {
		srv.panes = &paneBook{ask: func() (ipc.PluginsResult, error) { return c.res, c.err }}
		before := len(seen())
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "https://studio.local:7390/", nil)
		req.RemoteAddr = "192.168.1.30:50000"
		srv.Pane("shop.till").ServeHTTP(rec, req)
		if body := html.UnescapeString(rec.Body.String()); rec.Code != c.status || !strings.Contains(body, c.want) || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s: %d %q, want %d and %q", c.name, rec.Code, body, c.status, c.want)
		}
		if len(seen()) != before {
			t.Errorf("%s: the app got a request", c.name)
		}
	}

	// From beyond the local network, nothing: not even whether the pane is on.
	asked := 0
	srv.panes = &paneBook{ask: func() (ipc.PluginsResult, error) { asked++; return shop(true, ipc.PartReady, web), nil }}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "https://studio.local:7390/", nil)
	req.RemoteAddr = "203.0.113.9:50000"
	srv.Pane("shop.till").ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || asked != 0 || len(seen()) != 0 {
		t.Errorf("from the internet: %d, asked the core %d times, the app saw %d", rec.Code, asked, len(seen()))
	}
}

func TestTheGatewayAsksTheCoreAboutPanesNowAndThen(t *testing.T) {
	defer func(look, stale time.Duration) { paneLook, paneStale = look, stale }(paneLook, paneStale)
	paneLook, paneStale = time.Hour, time.Hour
	asks := 0
	down := false
	b := &paneBook{ask: func() (ipc.PluginsResult, error) {
		asks++
		if down {
			return ipc.PluginsResult{}, errors.New("core down")
		}
		return shop(true, ipc.PartReady, ipc.PluginPane{Kind: ipc.PaneWeb, Origin: "http://127.0.0.1:8080"}), nil
	}}
	for range 3 {
		if p, err := b.pane("shop.till"); err != nil || p.state != paneOn || p.target.String() != "http://127.0.0.1:8080" {
			t.Fatalf("pane: %+v %v", p, err)
		}
	}
	if asks != 1 {
		t.Errorf("asked %d times within paneLook, want once", asks)
	}
	// Once it is older than paneLook it asks again; while the core is
	// down, it goes by the last answer until that is paneStale old.
	paneLook = 0
	down = true
	if p, err := b.pane("shop.till"); err != nil || p.state != paneOn || asks != 2 {
		t.Errorf("with the core down: %+v %v, %d asks", p, err, asks)
	}
	paneStale = 0
	if _, err := b.pane("shop.till"); err == nil {
		t.Error("an answer older than paneStale still counts")
	}
}

func TestOnlyAnOriginOnThisComputerIsAPanesTarget(t *testing.T) {
	for origin, want := range map[string]string{
		"http://127.0.0.1:8080":     "http://127.0.0.1:8080",
		"http://127.0.0.1:8080/":    "http://127.0.0.1:8080",
		"https://localhost:8443":    "https://localhost:8443",
		"http://[::1]:9000":         "http://[::1]:9000",
		"http://127.0.0.2:80":       "http://127.0.0.2:80",
		"http://10.0.0.9:8080":      "",
		"http://studio.local:8080":  "",
		"https://till.example.com":  "",
		"ftp://127.0.0.1:21":        "",
		"http://127.0.0.1:8080/app": "",
		"http://alice@127.0.0.1:80": "",
		"http://127.0.0.1:80?x=1":   "",
		"":                          "",
	} {
		u, ok := loopbackOrigin(origin)
		if got := ""; ok {
			got = u.String()
			if got != want {
				t.Errorf("loopbackOrigin(%q) = %q, want %q", origin, got, want)
			}
		} else if want != "" {
			t.Errorf("loopbackOrigin(%q) refused, want %q", origin, want)
		}
	}
}

func TestCutOriginTakesOnlyTheWholeOrigin(t *testing.T) {
	for s, want := range map[string]string{
		"http://127.0.0.1:8080":        "",
		"HTTP://127.0.0.1:8080/x":      "/x",
		"http://127.0.0.1:8080?a=b":    "?a=b",
		"http://127.0.0.1:8080#top":    "#top",
		"http://127.0.0.1:80801/x":     "no",
		"http://127.0.0.1:8080.evil/x": "no",
		"/relative":                    "no",
	} {
		rest, ok := cutOrigin(s, "http://127.0.0.1:8080")
		if !ok {
			rest = "no"
		}
		if rest != want {
			t.Errorf("cutOrigin(%q) = %q, want %q", s, rest, want)
		}
	}
}
