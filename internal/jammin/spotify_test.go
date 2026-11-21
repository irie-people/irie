//go:build !nojammin

package jammin

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// browse is the person's browser coming back to the redirect URI from
// Spotify's sign-in page, with the words Spotify adds.
func browse(t *testing.T, redirect string, q url.Values, host string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, redirect+"?"+q.Encode(), nil)
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("the redirect didn't answer: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestSigningInTakesTheCodeAtTheLoopbackAndKeepsTheRefreshToken: the
// sign-in page asks for the PKCE code with an S256 challenge, a state and
// only the plugin's scopes, at a redirect URI on 127.0.0.1 with a port; the
// code comes back there and is traded with the verifier; the refresh token
// lands in the Store; and the source is ready.
func TestSigningInTakesTheCodeAtTheLoopbackAndKeepsTheRefreshToken(t *testing.T) {
	k := newKit(t)
	k.store.Delete(tokenKey)
	k.cfg.Spotify.ClientID = ""
	st := k.do(t, ipc.JamminArgs{})
	if s := stateOf(st.Sources, ipc.JamSpotify); s.State != ipc.JamNeeds || !strings.Contains(s.Problem, "developer.spotify.com/dashboard") || !strings.Contains(s.Problem, "http://127.0.0.1/jammin/spotify") {
		t.Fatalf("with no app: %+v", s)
	}
	if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamSignIn, ClientID: "not-an-id"}); err == nil {
		t.Fatal("a Client ID that isn't one was taken")
	}
	r := k.do(t, ipc.JamminArgs{Do: ipc.JamSignIn, ClientID: " " + strings.ToUpper(clientID) + "\n"})
	if r.Link == nil || r.Link.Redirect != "http://127.0.0.1/jammin/spotify" {
		t.Fatalf("link = %+v", r.Link)
	}
	if k.cfg.Spotify.ClientID != clientID || len(k.sets) != 1 {
		t.Fatalf("the Client ID wasn't kept as written: %q, %+v", k.cfg.Spotify.ClientID, k.sets)
	}
	u, err := url.Parse(r.Link.URL)
	if err != nil || u.Path != "/authorize" {
		t.Fatalf("sign-in page %q", r.Link.URL)
	}
	q := u.Query()
	redirect := q.Get("redirect_uri")
	if q.Get("client_id") != clientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
		len(q.Get("code_challenge")) != 43 || len(q.Get("state")) != 32 || !strings.HasPrefix(redirect, "http://127.0.0.1:") || !strings.HasSuffix(redirect, "/jammin/spotify") {
		t.Fatalf("sign-in page's words: %v", q)
	}
	if got := strings.Fields(q.Get("scope")); !slices.Equal(got, spotifyScopes) {
		t.Errorf("scopes = %v, want only %v", got, spotifyScopes)
	}
	if s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify); s.State != ipc.JamLinking || s.Link == nil {
		t.Fatalf("while signing in: %+v", s)
	}

	// Another page can't slip in a code, nor end the sign-in, by another
	// state or another name for the address.
	host := strings.TrimPrefix(strings.TrimSuffix(redirect, "/jammin/spotify"), "http://")
	if code, _ := browse(t, redirect, url.Values{"code": {"the-code"}, "state": {"wrong"}}, ""); code != http.StatusBadRequest {
		t.Errorf("a wrong state answered %d", code)
	}
	if code, _ := browse(t, redirect, url.Values{"code": {"the-code"}, "state": {q.Get("state")}}, "localhost:"+strings.Split(host, ":")[1]); code != http.StatusBadRequest {
		t.Errorf("another Host answered %d", code)
	}
	if s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify); s.State != ipc.JamLinking {
		t.Fatalf("a wrong answer ended the sign-in: %+v", s)
	}

	k.spotify.mu.Lock()
	k.spotify.challenge = q.Get("code_challenge")
	k.spotify.mu.Unlock()
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	code, body := browse(t, redirect, url.Values{"code": {"the-code"}, "state": {q.Get("state")}}, "")
	if code != http.StatusOK || !strings.Contains(body, "Spotify is linked") {
		t.Fatalf("the answer's page: %d %s", code, body)
	}
	if tok, _ := k.store.Get(tokenKey); tok != "refresh-1" {
		t.Fatalf("refresh token kept = %q", tok)
	}
	s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify)
	if s.State != ipc.JamReady || s.Account != "Alice" || s.Now != nil {
		t.Fatalf("after signing in: %+v", s)
	}
	// The sign-in is over: its address answers nothing more.
	time.Sleep(50 * time.Millisecond)
	if resp, err := http.Get(redirect + "?code=the-code&state=" + q.Get("state")); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Error("the redirect still answers after the sign-in")
		}
	}
}

// TestACanceledSignInStopsListening: cancel ends the sign-in at once, the
// redirect answers no more, the source stands as before, and its Client ID
// shows so a client can sign in again without asking for it.
func TestACanceledSignInStopsListening(t *testing.T) {
	k := newKit(t)
	k.store.Delete(tokenKey)
	r := k.do(t, ipc.JamminArgs{Do: ipc.JamSignIn})
	u, _ := url.Parse(r.Link.URL)
	redirect := u.Query().Get("redirect_uri")
	if s := stateOf(r.Sources, ipc.JamSpotify); s.State != ipc.JamLinking || s.ClientID != clientID {
		t.Fatalf("signing in: %+v", s)
	}
	r = k.do(t, ipc.JamminArgs{Do: ipc.JamCancel})
	if s := stateOf(r.Sources, ipc.JamSpotify); s.State != ipc.JamNeeds || !strings.Contains(s.Problem, "Not signed in") || s.ClientID != clientID || r.Text != "Canceled the sign-in to Spotify." {
		t.Fatalf("canceled: %+v, %q", s, r.Text)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(redirect)
		if err != nil {
			break
		}
		resp.Body.Close()
		if time.Now().After(deadline) {
			t.Fatal("the redirect still answers after a cancel")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if r := k.do(t, ipc.JamminArgs{Do: ipc.JamCancel}); r.Text != "No sign-in was under way." {
		t.Errorf("a second cancel: %q", r.Text)
	}
}

// TestASignInRefusedOnSpotifysPageSaysSo: access_denied ends the sign-in,
// and the source says why once, then what it needs.
func TestASignInRefusedOnSpotifysPageSaysSo(t *testing.T) {
	k := newKit(t)
	k.store.Delete(tokenKey)
	r := k.do(t, ipc.JamminArgs{Do: ipc.JamSignIn})
	u, _ := url.Parse(r.Link.URL)
	q := u.Query()
	if code, body := browse(t, q.Get("redirect_uri"), url.Values{"error": {"access_denied"}, "state": {q.Get("state")}}, ""); code != http.StatusOK || !strings.Contains(body, "didn") {
		t.Fatalf("refused page: %d %s", code, body)
	}
	s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify)
	if s.State != ipc.JamNeeds || !strings.Contains(s.Problem, "didn't agree") {
		t.Fatalf("after a refusal: %+v", s)
	}
	if s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify); s.State != ipc.JamNeeds || !strings.Contains(s.Problem, "Not signed in") {
		t.Fatalf("then: %+v", s)
	}
	if _, err := k.store.Get(tokenKey); err != ErrNoToken {
		t.Errorf("a token was kept: %v", err)
	}
}

// TestWhatPlaysReadsTheTrackItsDeviceAndControls: GET /me/player's track,
// artists, album art at the large size, progress at its time, shuffle and
// repeat, the device and whether it is this computer, and what the player
// disallows.
func TestWhatPlaysReadsTheTrackItsDeviceAndControls(t *testing.T) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's MacBook", "Computer", true)})
	s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify)
	n := s.Now
	if s.State != ipc.JamReady || n == nil || !n.Playing || n.Title != "Harbour Lights" || !slices.Equal(n.Artists, []string{"Night Owls", "The Tide"}) ||
		n.Album != "Low Tide" || n.Art != "https://i.example.com/640" || n.Duration != 245000 || n.Position != 61000 || n.At != k.clock.now().UnixMilli() ||
		!n.Shuffle || n.Repeat != ipc.JamRepeatAll || n.Link != "https://open.spotify.com/track/t1" || n.URI != "spotify:track:t1" || n.Kind != "track" {
		t.Fatalf("now = %+v", n)
	}
	if n.Device == nil || !n.Device.Here || n.Device.Name != "Alice's MacBook" || n.Volume == nil || *n.Volume != 40 {
		t.Errorf("device = %+v, volume %v", n.Device, n.Volume)
	}
	if n.Context == nil || n.Context.URI != "spotify:playlist:pl1" {
		t.Errorf("context = %+v", n.Context)
	}
	for _, c := range []string{ipc.JamCanPlay, ipc.JamCanSkip, ipc.JamCanSeek, ipc.JamCanVolume, ipc.JamCanShuffle, ipc.JamCanRepeat, ipc.JamCanLists, ipc.JamCanSearch, ipc.JamCanDevices} {
		if !slices.Contains(s.Can, c) {
			t.Errorf("can lacks %s: %v", c, s.Can)
		}
	}
	if want := "Spotify: playing “Harbour Lights” by Night Owls, The Tide."; Line(s) != want {
		t.Errorf("line = %q, want %q", Line(s), want)
	}

	// A phone's device isn't this computer's, and says where it plays.
	k.clock.add(time.Minute)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's Phone", "Smartphone", false)})
	s = stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify)
	if s.Now.Device.Here || s.Now.Playing || Line(s) != "Spotify: paused on “Harbour Lights” by Night Owls, The Tide on Alice's Phone." {
		t.Errorf("on a phone: %+v, %q", s.Now.Device, Line(s))
	}
}

// TestNothingPlayingAnEpisodeAndAnAd: a 204 is nothing playing, an
// episode takes its show and its own artwork, and an ad says so.
func TestNothingPlayingAnEpisodeAndAnAd(t *testing.T) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	if s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify); s.State != ipc.JamReady || s.Now != nil || Line(s) != "Spotify: nothing plays." {
		t.Fatalf("nothing: %+v", s)
	}
	k.clock.add(time.Minute)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: `{"device":{"id":"d","name":"Alice's MacBook","type":"Computer","is_active":true,"volume_percent":50},"is_playing":true,"progress_ms":5000,
"item":{"type":"episode","name":"Episode 12","uri":"spotify:episode:e1","duration_ms":3600000,"external_urls":{"spotify":"https://open.spotify.com/episode/e1"},"images":[{"url":"https://i.example.com/ep","width":640}],"show":{"name":"Harbour Talk","images":[{"url":"https://i.example.com/show","width":640}]}},
"currently_playing_type":"episode","actions":{"disallows":{}}}`})
	n := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify).Now
	if n == nil || n.Kind != "episode" || n.Title != "Episode 12" || n.Album != "Harbour Talk" || n.Art != "https://i.example.com/ep" || len(n.Artists) != 0 {
		t.Fatalf("episode: %+v", n)
	}
	k.clock.add(time.Minute)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: `{"device":{"id":"d","name":"x","type":"Computer"},"is_playing":true,"item":null,"currently_playing_type":"ad"}`})
	if n := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify).Now; n == nil || n.Kind != "ad" || n.Title != "An ad" {
		t.Fatalf("ad: %+v", n)
	}
}

// TestEachControlSendsItsRequest: play, an item and a track in its album,
// pause, next, previous, seek, volume, shuffle, repeat and a move, each as
// the Web API's reference gives it.
func TestEachControlSendsItsRequest(t *testing.T) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's MacBook", "Computer", true)})
	vol, on := 30, false
	for _, c := range []struct {
		a    ipc.JamminArgs
		want string
	}{
		{ipc.JamminArgs{Do: ipc.JamPause}, "PUT /v1/me/player/pause"},
		{ipc.JamminArgs{Do: ipc.JamPlay}, "PUT /v1/me/player/play"},
		{ipc.JamminArgs{Do: ipc.JamPlay, URI: "spotify:playlist:pl2"}, `PUT /v1/me/player/play {"context_uri":"spotify:playlist:pl2"}`},
		{ipc.JamminArgs{Do: ipc.JamPlay, URI: "spotify:track:t9"}, `PUT /v1/me/player/play {"uris":["spotify:track:t9"]}`},
		{ipc.JamminArgs{Do: ipc.JamPlay, URI: "spotify:track:t9", Context: "spotify:album:a1"}, `PUT /v1/me/player/play {"context_uri":"spotify:album:a1","offset":{"uri":"spotify:track:t9"}}`},
		{ipc.JamminArgs{Do: ipc.JamNext}, "POST /v1/me/player/next"},
		{ipc.JamminArgs{Do: ipc.JamPrevious}, "POST /v1/me/player/previous"},
		{ipc.JamminArgs{Do: ipc.JamSeek, Position: 90000}, "PUT /v1/me/player/seek?position_ms=90000"},
		{ipc.JamminArgs{Do: ipc.JamVolume, Volume: &vol}, "PUT /v1/me/player/volume?volume_percent=30"},
		{ipc.JamminArgs{Do: ipc.JamShuffle, On: &on}, "PUT /v1/me/player/shuffle?state=false"},
		{ipc.JamminArgs{Do: ipc.JamRepeat, Repeat: ipc.JamRepeatTrack}, "PUT /v1/me/player/repeat?state=track"},
		{ipc.JamminArgs{Do: ipc.JamRepeat, Repeat: ipc.JamRepeatAll}, "PUT /v1/me/player/repeat?state=context"},
		{ipc.JamminArgs{Do: ipc.JamMove, Device: "dev-2"}, `PUT /v1/me/player {"device_ids":["dev-2"],"play":true}`},
	} {
		c.a.Source = ipc.JamSpotify
		before := len(k.spotify.seen())
		k.do(t, c.a)
		var sent []string
		for _, l := range k.spotify.seen()[before:] {
			if !strings.HasPrefix(l, "GET ") && !strings.HasPrefix(l, "POST /api/token") {
				sent = append(sent, l)
			}
		}
		if len(sent) != 1 || sent[0] != c.want {
			t.Errorf("%s: sent %q, want %q", c.a.Do, sent, c.want)
		}
		k.clock.add(time.Minute)
	}
	if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamSpotify, URI: "https://open.spotify.com/track/t1"}); err == nil {
		t.Error("a link that isn't a Spotify URI was played")
	}
}

// TestAControlShowsItsChangeAtOnceAndAsksSpotifySoon: a pause's answer is
// paused before Spotify says so, and the next look half a second on asks
// Spotify again; looks in between don't.
func TestAControlShowsItsChangeAtOnceAndAsksSpotifySoon(t *testing.T) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's MacBook", "Computer", true)})
	k.do(t, ipc.JamminArgs{})
	reads := func() int {
		n := 0
		for _, l := range k.spotify.seen() {
			if strings.HasPrefix(l, "GET /v1/me/player?") {
				n++
			}
		}
		return n
	}
	k.clock.add(time.Second)
	k.do(t, ipc.JamminArgs{})
	if reads() != 1 {
		t.Fatalf("a look a second on asked Spotify again: %d", reads())
	}
	r := k.do(t, ipc.JamminArgs{Do: ipc.JamPause, Source: ipc.JamSpotify})
	if n := stateOf(r.Sources, ipc.JamSpotify).Now; n == nil || n.Playing {
		t.Fatalf("the pause's answer: %+v", n)
	}
	if reads() != 1 {
		t.Fatalf("the pause's answer asked Spotify at once: %d", reads())
	}
	k.clock.add(600 * time.Millisecond)
	k.do(t, ipc.JamminArgs{})
	if reads() != 2 {
		t.Fatalf("the look after a pause didn't ask Spotify: %d", reads())
	}
	// Nothing playing is asked about every 15 seconds.
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	k.clock.add(5 * time.Second)
	k.do(t, ipc.JamminArgs{})
	k.clock.add(5 * time.Second)
	k.do(t, ipc.JamminArgs{})
	if reads() != 3 {
		t.Fatalf("idle looks: %d", reads())
	}
	k.clock.add(16 * time.Second)
	k.do(t, ipc.JamminArgs{})
	if reads() != 4 {
		t.Fatalf("an idle look after 15 s: %d", reads())
	}
}

// TestSpotifysRefusalsInWords: Premium required, no active device, the
// quota used up, a 429 waited out and one too long.
func TestSpotifysRefusalsInWords(t *testing.T) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's MacBook", "Computer", false)})
	k.do(t, ipc.JamminArgs{})
	try := func(route string, a ...answer) error {
		k.spotify.on(route, a...)
		k.clock.add(time.Minute)
		_, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamNext, Source: ipc.JamSpotify})
		return err
	}
	if err := try("POST /v1/me/player/next", answer{status: 403, body: `{"error":{"status":403,"message":"Player command failed: Premium required","reason":"PREMIUM_REQUIRED"}}`}); err == nil || !strings.Contains(err.Error(), "Premium") {
		t.Errorf("premium: %v", err)
	}
	if err := try("POST /v1/me/player/next", answer{status: 404, body: `{"error":{"status":404,"message":"Player command failed: No active device found","reason":"NO_ACTIVE_DEVICE"}}`}); err == nil || !strings.Contains(err.Error(), "open Spotify on one") {
		t.Errorf("no device: %v", err)
	}
	if err := try("POST /v1/me/player/next", answer{status: 429, body: `{"error":{"status":429,"message":"Quota exceeded"},"reason":"QUOTA_EXCEEDED"}`, header: map[string]string{"Retry-After": "2"}}); err == nil || !strings.Contains(err.Error(), "quota") || len(k.waited) != 0 {
		t.Errorf("quota: %v, waited %v", err, k.waited)
	}
	if err := try("POST /v1/me/player/next", answer{status: 429, header: map[string]string{"Retry-After": "3"}}, answer{status: 204}); err != nil || len(k.waited) != 1 || k.waited[0] != 3*time.Second {
		t.Errorf("a short 429: %v, waited %v", err, k.waited)
	}
	if err := try("POST /v1/me/player/next", answer{status: 429, header: map[string]string{"Retry-After": "120"}}); err == nil || !strings.Contains(err.Error(), "wait 2m0s") {
		t.Errorf("a long 429: %v", err)
	}
	// While held, a look shows the last look rather than an error.
	k.spotify.on("POST /v1/me/player/next", answer{status: 204})
	if s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify); s.State != ipc.JamReady {
		t.Errorf("a look while held: %+v", s)
	}
}

// TestTheTokenRefreshesAndANewRefreshTokenIsKept: an access token that is
// about to end is renewed with the refresh token and the Client ID, a 401
// renews it once, a new refresh token replaces the old in the Store, and
// one Spotify no longer takes signs out.
func TestTheTokenRefreshesAndANewRefreshTokenIsKept(t *testing.T) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	k.spotify.rotate = "refresh-2"
	k.do(t, ipc.JamminArgs{})
	if tok, _ := k.store.Get(tokenKey); tok != "refresh-2" {
		t.Fatalf("the new refresh token wasn't kept: %q", tok)
	}
	// Spotify ends the access token early: a 401 renews it once.
	k.spotify.mu.Lock()
	k.spotify.access = "access-new"
	k.spotify.mu.Unlock()
	k.clock.add(time.Minute)
	k.do(t, ipc.JamminArgs{})
	if k.spotify.refreshes != 2 {
		t.Fatalf("refreshes = %d", k.spotify.refreshes)
	}
	// An hour on, the token is renewed before it is used.
	k.clock.add(time.Hour)
	k.do(t, ipc.JamminArgs{})
	if k.spotify.refreshes != 3 {
		t.Fatalf("refreshes after an hour = %d", k.spotify.refreshes)
	}
	// A refresh token Spotify no longer takes signs out.
	k.spotify.mu.Lock()
	k.spotify.revoked = true
	k.spotify.mu.Unlock()
	k.clock.add(2 * time.Hour)
	s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify)
	if s.State != ipc.JamNeeds || !strings.Contains(s.Problem, "signs in again") {
		t.Fatalf("revoked: %+v", s)
	}
	if _, err := k.store.Get(tokenKey); err != ErrNoToken {
		t.Errorf("the revoked token stayed: %v", err)
	}
}

// TestPlaylistsAndASearchInSpotifys2026Shapes: playlists count their items
// as Spotify names them since February 2026, a search gives tracks, then
// albums, then playlists, 10 of each at most, skipping the nulls Spotify
// leaves, and the devices say which is this computer.
func TestPlaylistsAndASearchInSpotifys2026Shapes(t *testing.T) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	k.spotify.on("GET /v1/me/playlists", answer{status: 200, body: `{"items":[{"type":"playlist","name":"Morning Focus","uri":"spotify:playlist:p1","external_urls":{"spotify":"https://open.spotify.com/playlist/p1"},"owner":{"display_name":"Alice"},"images":[{"url":"https://i.example.com/p1","width":300}],"items":{"href":"x","total":42}},null]}`})
	r := k.do(t, ipc.JamminArgs{Do: ipc.JamLists, Source: ipc.JamSpotify})
	if len(r.Lists) != 1 || r.Lists[0].Title != "Morning Focus" || r.Lists[0].Tracks != 42 || r.Lists[0].By != "Alice" || r.Lists[0].Art != "https://i.example.com/p1" {
		t.Fatalf("lists = %+v", r.Lists)
	}
	if got := k.spotify.seen(); !slices.Contains(got, "GET /v1/me/playlists?limit=50") {
		t.Errorf("asked %v", got)
	}
	k.spotify.on("GET /v1/search", answer{status: 200, body: `{"tracks":{"items":[{"type":"track","name":"Harbour Lights","uri":"spotify:track:t1","duration_ms":245000,"artists":[{"name":"Night Owls"}],"album":{"name":"Low Tide","uri":"spotify:album:a1","images":[{"url":"https://i.example.com/64","width":64}]}}]},
"albums":{"items":[{"type":"album","name":"Low Tide","uri":"spotify:album:a1","total_tracks":9,"artists":[{"name":"Night Owls"}],"images":[]}]},
"playlists":{"items":[null,{"type":"playlist","name":"Night Drive","uri":"spotify:playlist:p9","owner":{"display_name":"Bob"},"items":{"total":7}}]}}`})
	r = k.do(t, ipc.JamminArgs{Do: ipc.JamSearch, Source: ipc.JamSpotify, Query: "harbour lights"})
	kinds := []string{}
	for _, it := range r.Found {
		kinds = append(kinds, it.Kind+":"+it.Title)
	}
	if !slices.Equal(kinds, []string{"track:Harbour Lights", "album:Low Tide", "playlist:Night Drive"}) || r.Found[0].Album != "Low Tide" || r.Found[0].Context != "spotify:album:a1" || r.Found[1].Context != "" || r.Found[1].Tracks != 9 || r.Found[2].Tracks != 7 {
		t.Fatalf("found = %+v", r.Found)
	}
	if got := k.spotify.seen(); !slices.Contains(got, "GET /v1/search?limit=10&q=harbour+lights&type=track%2Calbum%2Cplaylist") {
		t.Errorf("asked %v", got)
	}
	if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamSearch, Source: ipc.JamSpotify, Query: "  "}); err == nil {
		t.Error("an empty search was sent")
	}
	k.spotify.on("GET /v1/me/player/devices", answer{status: 200, body: `{"devices":[{"id":"a","is_active":true,"name":"alice's macbook","type":"Computer","volume_percent":50,"supports_volume":true},{"id":"b","name":"Kitchen","type":"Speaker","volume_percent":null,"supports_volume":false,"is_restricted":true},{"id":null,"name":"Unknown","type":"Computer"}]}`})
	r = k.do(t, ipc.JamminArgs{Do: ipc.JamDevices, Source: ipc.JamSpotify})
	if len(r.Devices) != 2 || !r.Devices[0].Here || r.Devices[1].Here || !r.Devices[1].Restricted || r.Devices[1].Volume != nil {
		t.Fatalf("devices = %+v", r.Devices)
	}
}

// TestSigningOutForgetsTheTokenAndKeepsTheClientID: unlink removes the
// token and what the core read; --all removes the Client ID too.
func TestSigningOutForgetsTheTokenAndKeepsTheClientID(t *testing.T) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's MacBook", "Computer", true)})
	k.do(t, ipc.JamminArgs{})
	r := k.do(t, ipc.JamminArgs{Do: ipc.JamSignOut})
	if _, err := k.store.Get(tokenKey); err != ErrNoToken {
		t.Fatalf("the token stayed: %v", err)
	}
	if s := stateOf(r.Sources, ipc.JamSpotify); s.State != ipc.JamNeeds || s.Now != nil || s.Account != "" || k.cfg.Spotify.ClientID != clientID {
		t.Fatalf("signed out: %+v, client id %q", s, k.cfg.Spotify.ClientID)
	}
	k.do(t, ipc.JamminArgs{Do: ipc.JamSignOut, All: true})
	if k.cfg.Spotify.ClientID != "" {
		t.Fatalf("--all kept the Client ID %q", k.cfg.Spotify.ClientID)
	}
}
