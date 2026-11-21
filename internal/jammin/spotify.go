//go:build !nojammin

package jammin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// Spotify's addresses, as its Web API documentation gives them.
const (
	spotifyAPI       = "https://api.spotify.com/v1"
	spotifyAccounts  = "https://accounts.spotify.com"
	spotifyDashboard = "https://developer.spotify.com/dashboard"
)

// spotifyScopes are all the plugin asks of an account, and no more: what
// plays and its controls, and the person's playlists.
var spotifyScopes = []string{
	"user-read-playback-state",
	"user-modify-playback-state",
	"user-read-currently-playing",
	"playlist-read-private",
	"playlist-read-collaborative",
}

// How often the core asks Spotify what plays, whoever asks it: while
// something plays, while nothing does, and just after a control, which
// Spotify applies a moment later.
const (
	spotifyPlaying = 4 * time.Second
	spotifyIdle    = 15 * time.Second
	spotifyAfter   = 500 * time.Millisecond
)

// spotifyLongest is the longest 429 wait a control waits out; a longer one
// fails the call, saying how long.
const spotifyLongest = 5 * time.Second

// tokenKey is the Spotify refresh token's key in the Store.
const tokenKey = "spotify"

// spotify is the Spotify source: the person's account, through their own
// app, over the Web API.
type spotify struct {
	sv *Service

	mu      sync.Mutex
	access  string    // the access token, in memory only
	expires time.Time // when it ends
	user    string    // who the account is, by its display name
	last    ipc.JamSource
	next    time.Time // when to ask Spotify again
	held    time.Time // a 429 asked to wait until then
	link    *linking  // the sign-in under way
}

func (*spotify) Name() string  { return ipc.JamSpotify }
func (*spotify) Title() string { return "Spotify" }

func (p *spotify) api() string {
	if p.sv.API != "" {
		return strings.TrimSuffix(p.sv.API, "/")
	}
	return spotifyAPI
}

func (p *spotify) accounts() string {
	if p.sv.Accounts != "" {
		return strings.TrimSuffix(p.sv.Accounts, "/")
	}
	return spotifyAccounts
}

func (p *spotify) client() *http.Client {
	if p.sv.HTTP != nil {
		return p.sv.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// clientID is the person's app's Client ID, or "" for none.
func (p *spotify) clientID() string {
	id, _ := config.SpotifyClientID(p.sv.config().Spotify.ClientID)
	return id
}

// apiError is a refusal of Spotify's, in its regular error object.
type apiError struct {
	Status  int
	Message string
	Reason  string
	Wait    time.Duration // a 429's Retry-After
}

func (e *apiError) Error() string {
	switch {
	case e.Reason == "PREMIUM_REQUIRED" || strings.Contains(strings.ToLower(e.Message), "premium required"):
		return "Spotify lets an app control playback only for Premium accounts"
	case e.Reason == "NO_ACTIVE_DEVICE" || (e.Status == http.StatusNotFound && strings.Contains(strings.ToLower(e.Message), "no active device")):
		return "Nothing plays on any device of yours: open Spotify on one, or pick one under Devices"
	case e.Reason == "QUOTA_EXCEEDED":
		return "Your Spotify developer account's quota is used up for now, which no wait fixes: Spotify counts every app of the account together"
	case e.Status == http.StatusTooManyRequests:
		return fmt.Sprintf("Spotify asks %s to wait %s before it asks again", brand.Name, e.Wait.Round(time.Second))
	case e.Status == http.StatusForbidden && e.Message != "":
		return "Spotify refused: " + e.Message
	case e.Status == http.StatusNotFound:
		return "Spotify doesn't know that, or it isn't there for your account"
	case e.Message != "":
		return "Spotify said: " + e.Message
	}
	return fmt.Sprintf("Spotify answered %d", e.Status)
}

// errSignedOut says the account must sign in again: its refresh token is
// gone, or Spotify no longer takes it.
var errSignedOut = errors.New("Spotify signed " + brand.Name + " out: Link Spotify in the Jammin tab, or `" + brand.Slug + " jammin link spotify`, signs in again")

// call sends one request to the Web API with the account's token, and
// decodes the answer into out unless it has none. A 401 refreshes the token
// once. A 429 is waited out when wait allows and Spotify asks for no more
// than spotifyLongest; otherwise it fails, and holds every call until then.
func (p *spotify) call(ctx context.Context, method, path string, query url.Values, body, out any, wait bool) (int, error) {
	refused := false // Spotify refused the access token once
	for try := 0; ; try++ {
		p.mu.Lock()
		held := p.held
		p.mu.Unlock()
		if d := held.Sub(p.sv.now()); d > 0 {
			if !wait || d > spotifyLongest {
				return 0, &apiError{Status: http.StatusTooManyRequests, Wait: d}
			}
			if err := p.sv.wait(ctx, d); err != nil {
				return 0, err
			}
		}
		token, err := p.token(ctx, refused)
		if err != nil {
			return 0, err
		}
		u := p.api() + path
		if len(query) > 0 {
			u += "?" + query.Encode()
		}
		var rd io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return 0, err
			}
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		} else if method == http.MethodPut || method == http.MethodPost {
			// Spotify wants a length on every PUT and POST, an empty one
			// included.
			req.ContentLength = 0
			req.Body = http.NoBody
		}
		resp, err := p.client().Do(req)
		if err != nil {
			return 0, fmt.Errorf("Spotify didn't answer: %w", err)
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusUnauthorized && !refused:
			refused = true
			continue
		case resp.StatusCode == http.StatusUnauthorized:
			return resp.StatusCode, errSignedOut
		case resp.StatusCode == http.StatusTooManyRequests:
			e := refusal(resp.StatusCode, data)
			e.Wait = retryAfter(resp.Header.Get("Retry-After"))
			if e.Reason != "QUOTA_EXCEEDED" {
				p.mu.Lock()
				p.held = p.sv.now().Add(e.Wait)
				p.mu.Unlock()
				if wait && e.Wait <= spotifyLongest && try < 2 {
					continue
				}
			}
			return resp.StatusCode, e
		case resp.StatusCode >= 300:
			return resp.StatusCode, refusal(resp.StatusCode, data)
		}
		if out != nil && resp.StatusCode != http.StatusNoContent && len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return resp.StatusCode, fmt.Errorf("Spotify's answer didn't read: %v", err)
			}
		}
		return resp.StatusCode, nil
	}
}

// refusal reads Spotify's error object, {"error": {"status", "message",
// "reason"}}.
func refusal(status int, data []byte) *apiError {
	var body struct {
		Error struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
			Reason  string `json:"reason"`
		} `json:"error"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(data, &body)
	e := &apiError{Status: status, Message: body.Error.Message, Reason: body.Error.Reason}
	if e.Reason == "" {
		e.Reason = body.Reason
	}
	return e
}

// retryAfter reads a 429's Retry-After, in seconds; none is a second.
func retryAfter(s string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return time.Second
	}
	return time.Duration(n) * time.Second
}

// token is an access token that works: the one in memory, else a new one
// from the refresh token. fresh asks for a new one whatever the one in
// memory says, after Spotify refused it.
func (p *spotify) token(ctx context.Context, fresh bool) (string, error) {
	p.mu.Lock()
	if !fresh && p.access != "" && p.sv.now().Before(p.expires.Add(-time.Minute)) {
		t := p.access
		p.mu.Unlock()
		return t, nil
	}
	p.mu.Unlock()
	id := p.clientID()
	if id == "" {
		return "", errors.New(noApp)
	}
	refresh, err := p.sv.Store.Get(tokenKey)
	if errors.Is(err, ErrNoToken) {
		return "", errSignedOut
	}
	if err != nil {
		return "", fmt.Errorf("the Keychain didn't give the Spotify token: %v", err)
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {id}}
	tok, err := p.trade(ctx, form)
	if err != nil {
		if isGrantRefused(err) {
			_ = p.sv.Store.Delete(tokenKey)
			return "", errSignedOut
		}
		return "", err
	}
	if tok.RefreshToken != "" && tok.RefreshToken != refresh {
		if err := p.sv.Store.Set(tokenKey, tok.RefreshToken); err != nil {
			return "", fmt.Errorf("Spotify gave a new token, which the Keychain didn't keep: %v", err)
		}
	}
	p.mu.Lock()
	p.access, p.expires = tok.AccessToken, p.sv.now().Add(time.Duration(tok.ExpiresIn)*time.Second)
	p.mu.Unlock()
	return tok.AccessToken, nil
}

// grant is what Spotify's token endpoint answers.
type grant struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// grantError is the token endpoint's refusal, as RFC 6749 shapes it.
type grantError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *grantError) Error() string {
	if e.Description != "" {
		return "Spotify refused the sign-in: " + e.Description
	}
	return "Spotify refused the sign-in: " + e.Code
}

// isGrantRefused says Spotify no longer takes the grant: the refresh token
// was revoked, or the code was used.
func isGrantRefused(err error) bool {
	var g *grantError
	return errors.As(err, &g) && g.Code == "invalid_grant"
}

// trade posts a form to Spotify's token endpoint: a code for tokens, or a
// refresh token for an access token.
func (p *spotify) trade(ctx context.Context, form url.Values) (grant, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.accounts()+"/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return grant{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client().Do(req)
	if err != nil {
		return grant{}, fmt.Errorf("Spotify didn't answer: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var g grantError
		if json.Unmarshal(data, &g) == nil && g.Code != "" {
			return grant{}, &g
		}
		return grant{}, fmt.Errorf("Spotify's sign-in answered %d", resp.StatusCode)
	}
	var tok grant
	if err := json.Unmarshal(data, &tok); err != nil || tok.AccessToken == "" {
		return grant{}, errors.New("Spotify's sign-in answered without a token")
	}
	if tok.ExpiresIn <= 0 {
		tok.ExpiresIn = 3600
	}
	return tok, nil
}

// noApp is what the source says with no Client ID.
var noApp = "No Spotify app yet: make one at " + spotifyDashboard + " with the redirect URI " + redirectBase + ", then Link Spotify in the Jammin tab, or `" + brand.Slug + " jammin link spotify`, takes its Client ID. Spotify lets apps in development mode work only for Premium accounts."

// Look says how the account stands and what plays, asking Spotify at most
// as often as the cadence allows, with the Client ID kept.
func (p *spotify) Look(ctx context.Context) ipc.JamSource {
	st := p.look(ctx)
	st.ClientID = p.clientID()
	return st
}

func (p *spotify) look(ctx context.Context) ipc.JamSource {
	st := ipc.JamSource{Name: p.Name(), Title: p.Title()}
	p.mu.Lock()
	if l := p.link; l != nil {
		if !l.done && l.problem == "" && !p.sv.now().Before(l.until) {
			l.problem = lateSignIn
			l.stop()
		}
		switch {
		case l.done:
			p.link = nil
		case l.problem != "":
			// Said once; the next look says what the source needs.
			p.link = nil
			p.next = time.Time{}
			p.mu.Unlock()
			st.State, st.Problem = ipc.JamNeeds, l.problem
			return st
		default:
			p.mu.Unlock()
			st.State, st.Link = ipc.JamLinking, l.view()
			st.Problem = "Signing in: finish it on Spotify's page."
			return st
		}
	}
	if p.sv.now().Before(p.next) {
		last := p.last
		p.mu.Unlock()
		return last
	}
	p.mu.Unlock()

	st = p.read(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = st
	switch {
	case st.State == ipc.JamReady && st.Now != nil && st.Now.Playing:
		p.next = p.sv.now().Add(spotifyPlaying)
	case st.State == ipc.JamReady:
		p.next = p.sv.now().Add(spotifyIdle)
	default:
		// A problem is looked at again at the next ask, as a sign-in or a
		// new Client ID may have fixed it; a 429 holds every call itself.
		p.next = time.Time{}
	}
	return st
}

// read asks Spotify what plays.
func (p *spotify) read(ctx context.Context) ipc.JamSource {
	st := ipc.JamSource{Name: p.Name(), Title: p.Title()}
	if p.clientID() == "" {
		st.State, st.Problem = ipc.JamNeeds, noApp
		return st
	}
	// An access token in memory says it is signed in; only without one
	// is the Keychain asked, so a look every few seconds runs no security.
	p.mu.Lock()
	user, signedIn := p.user, p.access != ""
	p.mu.Unlock()
	if !signedIn {
		if _, err := p.sv.Store.Get(tokenKey); errors.Is(err, ErrNoToken) {
			st.State, st.Problem = ipc.JamNeeds, "Not signed in to Spotify yet: Link Spotify in the Jammin tab, or `"+brand.Slug+" jammin link spotify`, signs in with your Spotify app."
			return st
		}
	}
	if user == "" {
		var me struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		}
		if _, err := p.call(ctx, http.MethodGet, "/me", nil, nil, &me, false); err != nil {
			return p.failed(st, err)
		}
		user = me.DisplayName
		if user == "" {
			user = me.ID
		}
		p.mu.Lock()
		p.user = user
		p.mu.Unlock()
	}
	st.Account = user
	var pb playback
	code, err := p.call(ctx, http.MethodGet, "/me/player", url.Values{"additional_types": {"episode"}}, nil, &pb, false)
	if err != nil {
		return p.failed(st, err)
	}
	st.State = ipc.JamReady
	st.Can = []string{ipc.JamCanPlay, ipc.JamCanLists, ipc.JamCanSearch, ipc.JamCanDevices}
	if code == http.StatusNoContent || (pb.Item == nil && pb.Device == nil) {
		// Nothing plays on any device, nor is paused on one.
		return st
	}
	st.Now = pb.now(p.sv.now(), p.sv.hereName())
	st.Can = append(st.Can, pb.can()...)
	return st
}

// failed is st after a look that failed: signed out needs a sign-in, a
// 429 keeps the last look, and the rest is an error to show.
func (p *spotify) failed(st ipc.JamSource, err error) ipc.JamSource {
	if errors.Is(err, errSignedOut) {
		p.mu.Lock()
		p.user, p.access = "", ""
		p.mu.Unlock()
		st.State, st.Problem = ipc.JamNeeds, sentence(err.Error())
		return st
	}
	var e *apiError
	if errors.As(err, &e) && e.Status == http.StatusTooManyRequests && e.Reason != "QUOTA_EXCEEDED" {
		p.mu.Lock()
		last := p.last
		p.mu.Unlock()
		if last.State == ipc.JamReady {
			return last
		}
	}
	st.State, st.Problem = ipc.JamError, sentence(err.Error())
	return st
}

// playback is GET /me/player's answer, as much of it as the tab shows.
type playback struct {
	Device       *spotifyDevice `json:"device"`
	RepeatState  string         `json:"repeat_state"`
	ShuffleState bool           `json:"shuffle_state"`
	Context      *struct {
		Type         string            `json:"type"`
		URI          string            `json:"uri"`
		ExternalURLs map[string]string `json:"external_urls"`
	} `json:"context"`
	Timestamp   int64        `json:"timestamp"`
	ProgressMS  *int         `json:"progress_ms"`
	IsPlaying   bool         `json:"is_playing"`
	Item        *spotifyItem `json:"item"`
	CurrentType string       `json:"currently_playing_type"`
	Actions     struct {
		Disallows map[string]bool `json:"disallows"`
	} `json:"actions"`
}

// spotifyDevice is a device of the account's, as Spotify names it.
type spotifyDevice struct {
	ID             string `json:"id"`
	IsActive       bool   `json:"is_active"`
	IsRestricted   bool   `json:"is_restricted"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	VolumePercent  *int   `json:"volume_percent"`
	SupportsVolume *bool  `json:"supports_volume"`
}

func (d spotifyDevice) jam(here string) ipc.JamDevice {
	out := ipc.JamDevice{ID: d.ID, Name: d.Name, Kind: d.Type, Active: d.IsActive, Restricted: d.IsRestricted}
	out.Here = strings.EqualFold(d.Type, "Computer") && here != "" && strings.EqualFold(strings.TrimSpace(d.Name), strings.TrimSpace(here))
	if d.SupportsVolume == nil || *d.SupportsVolume {
		out.Volume = d.VolumePercent
	}
	return out
}

// spotifyItem is a track or an episode, or a simplified album, playlist or
// track in a list.
type spotifyItem struct {
	Type         string            `json:"type"`
	Name         string            `json:"name"`
	URI          string            `json:"uri"`
	DurationMS   int               `json:"duration_ms"`
	ExternalURLs map[string]string `json:"external_urls"`
	Artists      []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album *struct {
		Name   string         `json:"name"`
		URI    string         `json:"uri"`
		Images []spotifyImage `json:"images"`
	} `json:"album"`
	Images []spotifyImage `json:"images"`
	Show   *struct {
		Name   string         `json:"name"`
		Images []spotifyImage `json:"images"`
	} `json:"show"`
	Owner *struct {
		DisplayName string `json:"display_name"`
		ID          string `json:"id"`
	} `json:"owner"`
	// Items is a playlist's count, as Spotify names it since February
	// 2026; Tracks, as it named it before, and an album's.
	Items  *struct{ Total int } `json:"items"`
	Tracks *struct{ Total int } `json:"tracks"`
	// TotalTracks is an album's count.
	TotalTracks int `json:"total_tracks"`
}

// spotifyImage is one size of an artwork; Spotify lists the widest first.
type spotifyImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// art is the artwork's address at about size points wide: the smallest of
// them that is at least that wide, else the widest.
func art(images []spotifyImage, size int) string {
	best := ""
	bestW := 0
	for _, im := range images {
		if !strings.HasPrefix(im.URL, "https://") {
			continue
		}
		switch {
		case best == "":
			best, bestW = im.URL, im.Width
		case im.Width >= size && (bestW < size || im.Width < bestW):
			best, bestW = im.URL, im.Width
		case bestW < size && im.Width > bestW:
			best, bestW = im.URL, im.Width
		}
	}
	return best
}

func (it *spotifyItem) artists() []string {
	var out []string
	for _, a := range it.Artists {
		if a.Name != "" {
			out = append(out, a.Name)
		}
	}
	return out
}

// now is what plays, as the tab draws it.
func (pb playback) now(at time.Time, here string) *ipc.JamNow {
	n := &ipc.JamNow{Playing: pb.IsPlaying, Shuffle: pb.ShuffleState, At: at.UnixMilli()}
	switch pb.RepeatState {
	case "track":
		n.Repeat = ipc.JamRepeatTrack
	case "context":
		n.Repeat = ipc.JamRepeatAll
	default:
		n.Repeat = ipc.JamRepeatOff
	}
	if pb.ProgressMS != nil {
		n.Position = *pb.ProgressMS
	}
	if pb.Device != nil {
		d := pb.Device.jam(here)
		n.Device = &d
		n.Volume = d.Volume
	}
	n.Kind = pb.CurrentType
	if it := pb.Item; it != nil {
		n.Title, n.URI, n.Duration = it.Name, it.URI, it.DurationMS
		n.Link = it.ExternalURLs["spotify"]
		n.Artists = it.artists()
		if it.Album != nil {
			n.Album = it.Album.Name
			n.Art = art(it.Album.Images, 640)
		}
		if it.Show != nil {
			// An episode: its show is where the album goes.
			n.Album = it.Show.Name
			if n.Art = art(it.Images, 640); n.Art == "" {
				n.Art = art(it.Show.Images, 640)
			}
		}
	} else if pb.CurrentType == "ad" {
		n.Title = "An ad"
	}
	if c := pb.Context; c != nil && c.URI != "" {
		n.Context = &ipc.JamItem{Kind: c.Type, URI: c.URI, Link: c.ExternalURLs["spotify"]}
	}
	return n
}

// can is what the player allows now, by what Spotify says it disallows.
func (pb playback) can() []string {
	no := pb.Actions.Disallows
	var out []string
	if !no["skipping_next"] || !no["skipping_prev"] {
		out = append(out, ipc.JamCanSkip)
	}
	if !no["seeking"] && pb.Item != nil {
		out = append(out, ipc.JamCanSeek)
	}
	if !no["toggling_shuffle"] {
		out = append(out, ipc.JamCanShuffle)
	}
	if !no["toggling_repeat_context"] || !no["toggling_repeat_track"] {
		out = append(out, ipc.JamCanRepeat)
	}
	if pb.Device != nil && pb.Device.VolumePercent != nil && (pb.Device.SupportsVolume == nil || *pb.Device.SupportsVolume) {
		out = append(out, ipc.JamCanVolume)
	}
	return out
}

// Do sends one control to Spotify, and keeps what it changes until Spotify
// says so too.
func (p *spotify) Do(ctx context.Context, a ipc.JamminArgs) error {
	q := url.Values{}
	var body any
	method, path := http.MethodPut, ""
	switch a.Do {
	case ipc.JamPlay:
		path = "/me/player/play"
		if uri := strings.TrimSpace(a.URI); uri != "" {
			if !strings.HasPrefix(uri, "spotify:") {
				return fmt.Errorf("%q isn't a Spotify URI, such as spotify:playlist:ID", uri)
			}
			switch {
			case strings.HasPrefix(uri, "spotify:track:") || strings.HasPrefix(uri, "spotify:episode:"):
				if c := strings.TrimSpace(a.Context); strings.HasPrefix(c, "spotify:") {
					body = map[string]any{"context_uri": c, "offset": map[string]string{"uri": uri}}
				} else {
					body = map[string]any{"uris": []string{uri}}
				}
			default:
				body = map[string]any{"context_uri": uri}
			}
		}
	case ipc.JamPause:
		path = "/me/player/pause"
	case ipc.JamToggle:
		p.mu.Lock()
		playing := p.last.Now != nil && p.last.Now.Playing
		p.mu.Unlock()
		if playing {
			return p.Do(ctx, ipc.JamminArgs{Do: ipc.JamPause})
		}
		return p.Do(ctx, ipc.JamminArgs{Do: ipc.JamPlay})
	case ipc.JamNext:
		method, path = http.MethodPost, "/me/player/next"
	case ipc.JamPrevious:
		method, path = http.MethodPost, "/me/player/previous"
	case ipc.JamSeek:
		path = "/me/player/seek"
		q.Set("position_ms", strconv.Itoa(a.Position))
	case ipc.JamVolume:
		path = "/me/player/volume"
		q.Set("volume_percent", strconv.Itoa(*a.Volume))
	case ipc.JamShuffle:
		path = "/me/player/shuffle"
		q.Set("state", strconv.FormatBool(*a.On))
	case ipc.JamRepeat:
		path = "/me/player/repeat"
		q.Set("state", map[string]string{ipc.JamRepeatOff: "off", ipc.JamRepeatTrack: "track", ipc.JamRepeatAll: "context"}[a.Repeat])
	case ipc.JamMove:
		path = "/me/player"
		p.mu.Lock()
		playing := p.last.Now != nil && p.last.Now.Playing
		p.mu.Unlock()
		body = map[string]any{"device_ids": []string{a.Device}, "play": playing}
	default:
		return fmt.Errorf("Spotify takes no %q", a.Do)
	}
	if _, err := p.call(ctx, method, path, q, body, nil, true); err != nil {
		return err
	}
	p.after(a)
	return nil
}

// after keeps what a control changed in the last look, so the answer shows
// it, and asks Spotify again soon, when it has applied it.
func (p *spotify) after(a ipc.JamminArgs) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next = p.sv.now().Add(spotifyAfter)
	n := p.last.Now
	if n == nil {
		return
	}
	c := *n
	switch a.Do {
	case ipc.JamPlay:
		c.Playing = true
	case ipc.JamPause:
		c.Playing = false
	case ipc.JamSeek:
		c.Position, c.At = a.Position, p.sv.now().UnixMilli()
	case ipc.JamVolume:
		v := *a.Volume
		c.Volume = &v
	case ipc.JamShuffle:
		c.Shuffle = *a.On
	case ipc.JamRepeat:
		c.Repeat = a.Repeat
	}
	p.last.Now = &c
}

// Lists are the account's playlists, the newest 50 it follows or made.
func (p *spotify) Lists(ctx context.Context) ([]ipc.JamItem, error) {
	var page struct {
		Items []*spotifyItem `json:"items"`
	}
	if _, err := p.call(ctx, http.MethodGet, "/me/playlists", url.Values{"limit": {"50"}}, nil, &page, true); err != nil {
		return nil, err
	}
	var out []ipc.JamItem
	for _, it := range page.Items {
		if it != nil && it.URI != "" {
			out = append(out, it.jam("playlist"))
		}
	}
	return out, nil
}

// Search finds tracks, albums and playlists, the 10 of each that Spotify's
// development mode allows.
func (p *spotify) Search(ctx context.Context, words string) ([]ipc.JamItem, error) {
	var found struct {
		Tracks, Albums, Playlists struct {
			Items []*spotifyItem `json:"items"`
		}
	}
	q := url.Values{"q": {words}, "type": {"track,album,playlist"}, "limit": {"10"}}
	if _, err := p.call(ctx, http.MethodGet, "/search", q, nil, &found, true); err != nil {
		return nil, err
	}
	var out []ipc.JamItem
	// Tracks first, then albums, then playlists, each in Spotify's order.
	for _, group := range []struct {
		kind  string
		items []*spotifyItem
	}{{"track", found.Tracks.Items}, {"album", found.Albums.Items}, {"playlist", found.Playlists.Items}} {
		for _, it := range group.items {
			// Spotify's search leaves a null in place of a playlist it
			// won't show.
			if it != nil && it.URI != "" {
				out = append(out, it.jam(group.kind))
			}
		}
	}
	return out, nil
}

// jam is a list's item as the tab shows it.
func (it *spotifyItem) jam(kind string) ipc.JamItem {
	if it.Type != "" {
		kind = it.Type
	}
	out := ipc.JamItem{Kind: kind, Title: it.Name, URI: it.URI, Link: it.ExternalURLs["spotify"], Duration: it.DurationMS}
	switch {
	case len(it.Artists) > 0:
		out.By = strings.Join(it.artists(), ", ")
	case it.Owner != nil:
		out.By = it.Owner.DisplayName
	}
	if it.Album != nil {
		out.Album = it.Album.Name
		out.Art = art(it.Album.Images, 64)
		if strings.HasPrefix(it.Album.URI, "spotify:album:") {
			out.Context = it.Album.URI
		}
	}
	if out.Art == "" {
		out.Art = art(it.Images, 64)
	}
	switch {
	case it.Items != nil:
		out.Tracks = it.Items.Total
	case it.Tracks != nil:
		out.Tracks = it.Tracks.Total
	case it.TotalTracks > 0:
		out.Tracks = it.TotalTracks
	}
	return out
}

// Devices are where the account can play now.
func (p *spotify) Devices(ctx context.Context) ([]ipc.JamDevice, error) {
	var list struct {
		Devices []spotifyDevice `json:"devices"`
	}
	if _, err := p.call(ctx, http.MethodGet, "/me/player/devices", nil, nil, &list, true); err != nil {
		return nil, err
	}
	here := p.sv.hereName()
	out := make([]ipc.JamDevice, 0, len(list.Devices))
	for _, d := range list.Devices {
		if d.ID != "" {
			out = append(out, d.jam(here))
		}
	}
	return out, nil
}

// signOut signs out: the token goes from the Store, what the core read
// goes from memory, and with all the Client ID goes from config.toml.
func (p *spotify) signOut(all bool) error {
	p.mu.Lock()
	if p.link != nil {
		p.link.stop()
		p.link = nil
	}
	p.access, p.expires, p.user = "", time.Time{}, ""
	p.last, p.next, p.held = ipc.JamSource{}, time.Time{}, time.Time{}
	p.mu.Unlock()
	if err := p.sv.Store.Delete(tokenKey); err != nil {
		return fmt.Errorf("the Keychain didn't let go of the Spotify token: %v", err)
	}
	if all && p.sv.Configure != nil {
		if err := p.sv.Configure([]ipc.ConfigSet{{Key: "jammin.spotify.client_id", Value: json.RawMessage(`""`)}}); err != nil {
			return err
		}
	}
	return nil
}
