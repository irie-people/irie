//go:build !nojammin

package jammin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// fakeSpotify stands in for Spotify's Web API and its accounts service,
// answering as Spotify's documentation says. Nothing in these tests reaches
// Spotify itself.
type fakeSpotify struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	calls     []string            // "METHOD /path?query body", in order
	access    string              // the access token it takes
	refresh   string              // the refresh token it takes
	rotate    string              // a refresh token to hand out at the next refresh
	revoked   bool                // refresh tokens are refused
	challenge string              // from the last authorize URL
	answers   map[string][]answer // by "METHOD /path": each in turn, the last again and again
	refreshes int
}

// answer is what the fake answers to a call.
type answer struct {
	status int
	body   string
	header map[string]string
}

func newFakeSpotify(t *testing.T) *fakeSpotify {
	f := &fakeSpotify{t: t, access: "access-1", refresh: "refresh-1", answers: map[string][]answer{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// on sets what METHOD /path answers: each answer in turn, then the last
// again and again.
func (f *fakeSpotify) on(route string, a ...answer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[route] = a
}

func (f *fakeSpotify) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeSpotify) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	line := r.Method + " " + r.URL.Path
	if r.URL.RawQuery != "" {
		line += "?" + r.URL.RawQuery
	}
	if len(body) > 0 {
		line += " " + string(body)
	}
	f.calls = append(f.calls, line)
	if r.URL.Path == "/api/token" {
		f.token(w, r, body)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.access {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"status":401,"message":"The access token expired"}}`)
		return
	}
	if (r.Method == http.MethodPut || r.Method == http.MethodPost) && r.ContentLength < 0 {
		f.t.Errorf("%s has no Content-Length", line)
	}
	route := r.Method + " " + r.URL.Path
	var a answer
	if list := f.answers[route]; len(list) > 0 {
		a = list[0]
		if len(list) > 1 {
			f.answers[route] = list[1:]
		}
	} else {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/me":
			a = answer{status: 200, body: `{"id":"alice","display_name":"Alice"}`}
		case r.Method != http.MethodGet:
			a = answer{status: 204}
		default:
			a = answer{status: 404, body: `{"error":{"status":404,"message":"Not found."}}`}
		}
	}
	for k, v := range a.header {
		w.Header().Set(k, v)
	}
	if a.body != "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(a.status)
	io.WriteString(w, a.body)
}

// token is the accounts service's token endpoint: a code for tokens, or a
// refresh token for an access token, as the PKCE flow has them.
func (f *fakeSpotify) token(w http.ResponseWriter, r *http.Request, body []byte) {
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		f.t.Errorf("token: %s with %q", r.Method, r.Header.Get("Content-Type"))
	}
	form, _ := url.ParseQuery(string(body))
	if form.Get("client_id") != clientID {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"invalid_client","error_description":"Invalid client"}`)
		return
	}
	if r.Header.Get("Authorization") != "" {
		f.t.Errorf("token: a PKCE client sends no Authorization header")
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		if form.Get("code") != "the-code" || challenge(form.Get("code_verifier")) != f.challenge || !strings.HasPrefix(form.Get("redirect_uri"), "http://127.0.0.1:") {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"invalid_grant","error_description":"Invalid authorization code"}`)
			return
		}
		fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","scope":"user-read-playback-state","expires_in":3600,"refresh_token":%q}`, f.access, f.refresh)
	case "refresh_token":
		f.refreshes++
		if f.revoked || form.Get("refresh_token") != f.refresh {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"invalid_grant","error_description":"Refresh token revoked"}`)
			return
		}
		f.access = fmt.Sprintf("access-%d", f.refreshes+1)
		if f.rotate != "" {
			f.refresh, f.rotate = f.rotate, ""
			fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600,"refresh_token":%q}`, f.access, f.refresh)
			return
		}
		fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, f.access)
	default:
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"unsupported_grant_type"}`)
	}
}

// clientID is the tests' Spotify app.
const clientID = "0123456789abcdef0123456789abcdef"

// memStore is a Store in memory.
type memStore struct {
	mu   sync.Mutex
	keys map[string]string
}

func (m *memStore) Get(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.keys[key]
	if !ok {
		return "", ErrNoToken
	}
	return t, nil
}

func (m *memStore) Set(key, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys == nil {
		m.keys = map[string]string{}
	}
	m.keys[key] = token
	return nil
}

func (m *memStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keys, key)
	return nil
}

// fakeScript stands in for osascript: it records each script it is given
// and answers by the script.
type fakeScript struct {
	mu     sync.Mutex
	runs   []scriptRun
	answer func(r scriptRun) (string, error)
}

type scriptRun struct {
	language, script string
	args             []string
}

func (f *fakeScript) Run(_ context.Context, language, script string, args ...string) (string, error) {
	r := scriptRun{language, script, args}
	f.mu.Lock()
	f.runs = append(f.runs, r)
	answer := f.answer
	f.mu.Unlock()
	if answer == nil {
		return "ok", nil
	}
	return answer(r)
}

// did are the controls the stand-in was asked for, as "what arg".
func (f *fakeScript) did() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.runs {
		if r.script == musicDo {
			out = append(out, strings.TrimSpace(strings.Join(r.args, " ")))
		}
	}
	return out
}

// clock is a clock the test moves.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

// kit is a Service with every stand-in: a signed-in Spotify, and a Music
// app that isn't running.
type kit struct {
	sv      *Service
	spotify *fakeSpotify
	store   *memStore
	script  *fakeScript
	clock   *clock
	cfg     config.Jammin
	running bool     // the Music app
	spotApp bool     // the Spotify app
	started []string // the apps Launch started
	waited  []time.Duration
	sets    []ipc.ConfigSet
}

func newKit(t *testing.T) *kit {
	k := &kit{spotify: newFakeSpotify(t), store: &memStore{}, script: &fakeScript{}, clock: &clock{at: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)}}
	k.cfg.Spotify.ClientID = clientID
	k.store.Set(tokenKey, "refresh-1")
	k.sv = &Service{
		Dir:    t.TempDir(),
		Home:   t.TempDir(), // no Music folder: the files source needs one
		Store:  k.store,
		Config: func() config.Jammin { return k.cfg },
		Configure: func(sets []ipc.ConfigSet) error {
			k.sets = append(k.sets, sets...)
			for _, s := range sets {
				switch s.Key {
				case "jammin.spotify.client_id":
					json.Unmarshal(s.Value, &k.cfg.Spotify.ClientID)
				case "jammin.folder":
					json.Unmarshal(s.Value, &k.cfg.Folder)
				}
			}
			return nil
		},
		Script: k.script,
		Running: func(program string) bool {
			if program == spotifyAppProgram {
				return k.spotApp
			}
			return k.running
		},
		Launch: func(bundle string) error {
			k.started = append(k.started, bundle)
			k.running = true
			return nil
		},
		API:      k.spotify.srv.URL + "/v1",
		Accounts: k.spotify.srv.URL,
		Here:     func() string { return "Alice's MacBook" },
		Now:      k.clock.now,
		Wait: func(_ context.Context, d time.Duration) error {
			k.waited = append(k.waited, d)
			k.clock.add(d)
			return nil
		},
	}
	k.sv.init()
	return k
}

// do calls the op, failing the test on an error.
func (k *kit) do(t *testing.T, a ipc.JamminArgs) ipc.JamminResult {
	t.Helper()
	r, err := k.sv.Do(context.Background(), a)
	if err != nil {
		t.Fatalf("%+v: %v", a, err)
	}
	return r
}

// playing is GET /me/player's answer for a track that plays on device.
func playing(track, device, kind string, isPlaying bool) string {
	return fmt.Sprintf(`{"device":{"id":"dev-1","is_active":true,"is_private_session":false,"is_restricted":false,"name":%q,"type":%q,"volume_percent":40,"supports_volume":true},
"repeat_state":"context","shuffle_state":true,"context":{"type":"playlist","href":"https://api.spotify.com/v1/playlists/pl1","external_urls":{"spotify":"https://open.spotify.com/playlist/pl1"},"uri":"spotify:playlist:pl1"},
"timestamp":1763726400000,"progress_ms":61000,"is_playing":%t,
"item":{"type":"track","name":%q,"uri":"spotify:track:t1","duration_ms":245000,"is_local":false,"external_urls":{"spotify":"https://open.spotify.com/track/t1"},
"artists":[{"name":"Night Owls"},{"name":"The Tide"}],"album":{"name":"Low Tide","uri":"spotify:album:a1","images":[{"url":"https://i.example.com/640","width":640,"height":640},{"url":"https://i.example.com/300","width":300,"height":300},{"url":"https://i.example.com/64","width":64,"height":64}]}},
"currently_playing_type":"track","actions":{"disallows":{"resuming":true}}}`, device, kind, isPlaying, track)
}
