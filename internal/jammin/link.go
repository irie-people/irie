//go:build !nojammin

package jammin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// The sign-in is Spotify's Authorization Code flow with PKCE, as its Web
// API documentation gives it: no secret, a code verifier and its S256
// challenge, a state, and a redirect to the loopback address, where the
// core listens at a free port.

// redirectPath is where Spotify sends the browser back to.
const redirectPath = "/jammin/spotify"

// redirectBase is the redirect URI the person's Spotify app lists: the
// loopback address as an IP literal, as Spotify requires, without a port,
// since Spotify lets a loopback address's port change at each sign-in.
const redirectBase = "http://127.0.0.1" + redirectPath

// linkWait is how long a sign-in may take before the core stops listening.
const linkWait = 10 * time.Minute

// lateSignIn is what a sign-in that took too long says.
var lateSignIn = "The sign-in to Spotify took longer than 10 minutes, so " + brand.Name + " stopped waiting: Link Spotify again."

// linking is a sign-in under way.
type linking struct {
	url      string // the sign-in page
	redirect string // the redirect URI with this sign-in's port
	clientID string
	state    string
	verifier string
	host     string // 127.0.0.1:PORT, the only Host the listener answers
	until    time.Time

	// done says it signed in; problem why it ended without. Both under
	// the source's lock.
	done    bool
	problem string

	srv  *http.Server
	once sync.Once
}

func (l *linking) view() *ipc.JamLink {
	return &ipc.JamLink{Source: ipc.JamSpotify, URL: l.url, Redirect: redirectBase, Until: l.until.Unix()}
}

// stop ends the listening, letting an answer that is being written finish.
func (l *linking) stop() {
	l.once.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = l.srv.Shutdown(ctx)
		}()
	})
}

// signIn starts a sign-in with the Client ID given, else the one in
// config.toml, keeping a new one there first, and listens for Spotify's
// answer for linkWait. A sign-in already under way gives way to it.
func (p *spotify) signIn(ctx context.Context, clientID string) (ipc.JamLink, error) {
	id := p.clientID()
	if strings.TrimSpace(clientID) != "" {
		got, ok := config.SpotifyClientID(clientID)
		if !ok {
			return ipc.JamLink{}, errors.New("that isn't a Client ID: a Spotify app's is 32 hex digits, in its settings at " + spotifyDashboard)
		}
		if got != id {
			if p.sv.Configure == nil {
				return ipc.JamLink{}, errors.New("can't keep the Client ID: there is no config file to write it to")
			}
			if err := p.sv.Configure([]ipc.ConfigSet{{Key: "jammin.spotify.client_id", Value: jsonString(got)}}); err != nil {
				return ipc.JamLink{}, err
			}
		}
		id = got
	}
	if id == "" {
		return ipc.JamLink{}, errors.New(noApp)
	}
	verifier, err := randomWord(64)
	if err != nil {
		return ipc.JamLink{}, err
	}
	state, err := randomWord(32)
	if err != nil {
		return ipc.JamLink{}, err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return ipc.JamLink{}, fmt.Errorf("can't listen for Spotify's answer: %v", err)
	}
	host := ln.Addr().String()
	l := &linking{
		redirect: "http://" + host + redirectPath,
		clientID: id,
		state:    state,
		verifier: verifier,
		host:     host,
		until:    p.sv.now().Add(linkWait),
	}
	q := url.Values{
		"client_id":             {id},
		"response_type":         {"code"},
		"redirect_uri":          {l.redirect},
		"code_challenge_method": {"S256"},
		"code_challenge":        {challenge(verifier)},
		"state":                 {state},
		"scope":                 {strings.Join(spotifyScopes, " ")},
	}
	l.url = p.accounts() + "/authorize?" + q.Encode()
	mux := http.NewServeMux()
	mux.HandleFunc(redirectPath, func(w http.ResponseWriter, r *http.Request) { p.answer(l, w, r) })
	l.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	p.mu.Lock()
	if old := p.link; old != nil {
		old.stop()
	}
	p.link = l
	p.mu.Unlock()
	go func() { _ = l.srv.Serve(ln) }()
	go func() {
		t := time.NewTimer(linkWait)
		defer t.Stop()
		<-t.C
		p.mu.Lock()
		if !l.done && l.problem == "" {
			l.problem = lateSignIn
		}
		p.mu.Unlock()
		l.stop()
	}()
	return *l.view(), nil
}

// cancelSignIn stops a sign-in under way, if any: the core stops listening,
// and the source stands as it did before.
func (p *spotify) cancelSignIn() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	l := p.link
	if l == nil || l.done {
		return false
	}
	l.problem = "Canceled."
	l.stop()
	p.link = nil
	p.next = time.Time{}
	return true
}

// answer is Spotify's answer at the redirect URI: a code to trade for
// tokens, or why there is none. One that isn't for this sign-in, by its
// Host or its state, changes nothing, so another page can neither end the
// sign-in nor slip in a code of its own.
func (p *spotify) answer(l *linking, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	if r.Host != l.host || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(l.state)) != 1 {
		page(w, http.StatusBadRequest, "This isn't the sign-in "+brand.Name+" started", "Nothing changed. Link Spotify again in "+brand.Name+" if you meant to sign in.")
		return
	}
	p.mu.Lock()
	ended := l.done || l.problem != ""
	p.mu.Unlock()
	if ended {
		page(w, http.StatusGone, "This sign-in has ended", "Link Spotify again in "+brand.Name+" to sign in.")
		return
	}
	end := func(problem string) {
		p.mu.Lock()
		l.problem = problem
		p.mu.Unlock()
		l.stop()
	}
	if e := q.Get("error"); e != "" {
		why := "Spotify's sign-in ended without signing in (" + e + ")."
		if e == "access_denied" {
			why = "You didn't agree on Spotify's page, so " + brand.Name + " isn't signed in."
		}
		end(why + " Link Spotify again to try once more.")
		page(w, http.StatusOK, "Not signed in", why)
		return
	}
	code := q.Get("code")
	if code == "" {
		page(w, http.StatusBadRequest, "Spotify sent no code", "Link Spotify again in "+brand.Name+".")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok, err := p.trade(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {l.redirect},
		"client_id":     {l.clientID},
		"code_verifier": {l.verifier},
	})
	if err == nil && tok.RefreshToken == "" {
		err = errors.New("Spotify gave no refresh token, so the sign-in wouldn't last")
	}
	if err == nil {
		if serr := p.sv.Store.Set(tokenKey, tok.RefreshToken); serr != nil {
			err = fmt.Errorf("the Keychain didn't keep the Spotify token: %v", serr)
		}
	}
	if err != nil {
		why := sentence(err.Error())
		end(why + " Link Spotify again to try once more.")
		page(w, http.StatusOK, "Not signed in", why)
		return
	}
	p.mu.Lock()
	p.access, p.expires = tok.AccessToken, p.sv.now().Add(time.Duration(tok.ExpiresIn)*time.Second)
	p.user, p.next, p.held = "", time.Time{}, time.Time{}
	l.done = true
	p.mu.Unlock()
	l.stop()
	page(w, http.StatusOK, "Spotify is linked", "Your music is in "+brand.Name+"'s Jammin tab now. You can close this page.")
}

// page answers the browser with a short page of its own, which loads
// nothing.
func page(w http.ResponseWriter, status int, title, line string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>%s</title>
<style>body{font:16px/1.5 -apple-system,system-ui,sans-serif;max-width:32em;margin:4em auto;padding:0 1em;color:#1f2a24;background:#f6f3ec}@media(prefers-color-scheme:dark){body{color:#e8e4da;background:#16201b}}h1{font-size:1.3em;font-weight:600}</style>
<h1>%s</h1><p>%s</p>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(line))
}

// challenge is a code verifier's S256 challenge: its SHA-256, in base64url
// without padding.
func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// wordLetters are what a verifier and a state are made of: letters and
// digits, which PKCE allows and no address needs to escape.
const wordLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// randomWord is n letters and digits from the system's random source.
func randomWord(n int) (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(wordLetters)))
	for range n {
		i, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(wordLetters[i.Int64()])
	}
	return b.String(), nil
}

// jsonString is s as a JSON value, for a config setting.
func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
