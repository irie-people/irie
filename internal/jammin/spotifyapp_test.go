//go:build !nojammin

package jammin

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// appPlaying is the Spotify app's look while a track plays.
const appPlaying = `{"running":true,"state":"playing","volume":55,"shuffle":true,"repeat":false,"position":61.5,
"track":{"name":"Harbour Lights","artist":"Night Owls","album":"Low Tide","duration":245000,"id":"spotify:track:t1","art":"https://i.example.com/a"}}`

// appKit is a kit with no Spotify app of the person's linked and the
// Spotify app running here, answering look with look.
func appKit(t *testing.T, look string) *kit {
	k := newKit(t)
	k.cfg.Spotify.ClientID = ""
	k.spotApp = true
	k.script.answer = func(r scriptRun) (string, error) {
		if r.script == spotifyAppLook {
			return look, nil
		}
		return "ok", nil
	}
	return k
}

// appDid are the controls the Spotify app was asked for, as "what arg ctx".
func appDid(k *kit) []string {
	k.script.mu.Lock()
	defer k.script.mu.Unlock()
	var out []string
	for _, r := range k.script.runs {
		if r.script == spotifyAppDo {
			out = append(out, strings.TrimSpace(strings.Join(r.args, " ")))
		}
	}
	return out
}

// TestTheSpotifyAppPlaysWithNoAccountLinked: with no Spotify app of the
// person's, Spotify is the Spotify app here: what plays, with its link
// back on open.spotify.com, its length in milliseconds and the controls
// the app has, and none of an account's lists, search or devices.
func TestTheSpotifyAppPlaysWithNoAccountLinked(t *testing.T) {
	k := appKit(t, appPlaying)
	s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify)
	n := s.Now
	if s.State != ipc.JamReady || s.Via != ipc.JamViaApp || n == nil || !n.Playing || n.Title != "Harbour Lights" || n.Link != "https://open.spotify.com/track/t1" ||
		n.Duration != 245000 || n.Position != 61500 || n.Art != "https://i.example.com/a" || !n.Shuffle || n.Repeat != ipc.JamRepeatOff || n.Device != nil || *n.Volume != 55 {
		t.Fatalf("via the app: %+v, now %+v", s, n)
	}
	for _, no := range []string{ipc.JamCanLists, ipc.JamCanSearch, ipc.JamCanDevices} {
		if slices.Contains(s.Can, no) {
			t.Errorf("the app can %s: %v", no, s.Can)
		}
	}
	if len(k.spotify.seen()) != 0 {
		t.Fatalf("Spotify's Web API was asked: %v", k.spotify.seen())
	}
	vol, on := 30, false
	for _, a := range []ipc.JamminArgs{{Do: ipc.JamNext}, {Do: ipc.JamSeek, Position: 61500}, {Do: ipc.JamVolume, Volume: &vol}, {Do: ipc.JamShuffle, On: &on},
		{Do: ipc.JamRepeat, Repeat: ipc.JamRepeatAll}, {Do: ipc.JamRepeat, Repeat: ipc.JamRepeatOff}, {Do: ipc.JamPlay, URI: "spotify:track:t9", Context: "spotify:album:a1"}} {
		a.Source = ipc.JamSpotify
		k.do(t, a)
		k.clock.add(2 * time.Second)
	}
	want := []string{"next", "seek 61.500", "volume 30", "shuffle false", "repeat true", "repeat false", "play spotify:track:t9 spotify:album:a1"}
	if got := appDid(k); !slices.Equal(got, want) {
		t.Fatalf("did %q, want %q", got, want)
	}
	if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamLists, Source: ipc.JamSpotify}); err == nil {
		t.Error("the app listed playlists")
	}
	if line := Line(s); line != "Spotify: playing “Harbour Lights” by Night Owls." {
		t.Errorf("line = %q", line)
	}
}

// TestAnAccountLinkedGoesThroughTheWebAPI: with an account linked, the app
// running here changes nothing; and with neither, the source says both
// ways in.
func TestAnAccountLinkedGoesThroughTheWebAPI(t *testing.T) {
	k := newKit(t)
	k.spotApp = true
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	if s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify); s.Via != ipc.JamViaAccount || s.Account != "Alice" {
		t.Fatalf("linked: %+v", s)
	}
	k = newKit(t)
	k.cfg.Spotify.ClientID = ""
	s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamSpotify)
	if s.State != ipc.JamNeeds || !strings.Contains(s.Problem, "No Spotify app yet") || !strings.Contains(s.Problem, "open the Spotify app on this Mac") {
		t.Fatalf("neither: %+v", s)
	}
	if len(k.script.runs) != 0 {
		t.Fatalf("a script ran for a Spotify app that isn't running: %+v", k.script.runs)
	}
}

// TestSpotifyAppScriptsUseTheDictionarysNames: each name the scripts use
// is in JavaScript's form in them, and in the Spotify app's dictionary
// where the app is.
func TestSpotifyAppScriptsUseTheDictionarysNames(t *testing.T) {
	scripts := spotifyAppLook + spotifyAppDo
	for _, name := range spotifyAppNames {
		if js := jsName(name); !strings.Contains(scripts, js) {
			t.Errorf("no script uses %q as %s", name, js)
		}
	}
	if runtime.GOOS != "darwin" {
		return
	}
	var sdef []byte
	for _, app := range []string{"/Applications/Spotify.app", filepath.Join(os.Getenv("HOME"), "Applications", "Spotify.app")} {
		if b, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", "Spotify.sdef")); err == nil {
			sdef = b
			break
		}
	}
	if sdef == nil {
		t.Skip("no Spotify app here to read the dictionary of")
	}
	for _, name := range spotifyAppNames {
		if !regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `"`).Match(sdef) {
			t.Errorf("the Spotify app's dictionary has no %q", name)
		}
	}
}

// TestAfplayRunsInASessionOfItsOwn: the real player starts its program in
// a session of its own, sees it by its pid and start, stops and continues
// it, and ends it; a stand-in for afplay that only sleeps, so no sound.
func TestAfplayRunsInASessionOfItsOwn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals")
	}
	bin := filepath.Join(t.TempDir(), "afplay")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := Afplay{Bin: bin}
	pid, start, err := p.Start(filepath.Join(t.TempDir(), "track.mp3"))
	if err != nil || pid <= 0 {
		t.Fatalf("start: %d, %v", pid, err)
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	if !p.Alive(pid, start) || p.Alive(pid, start+1) {
		t.Fatalf("alive: %v, with another start %v", p.Alive(pid, start), p.Alive(pid, start+1))
	}
	if sid, err := syscall.Getpgid(pid); err != nil || sid != pid {
		t.Errorf("its own group: %d, %v", sid, err)
	}
	for _, sig := range []syscall.Signal{syscall.SIGSTOP, syscall.SIGCONT, syscall.SIGTERM} {
		if err := p.Signal(pid, sig); err != nil {
			t.Fatalf("%v: %v", sig, err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for p.Alive(pid, start) {
		if time.Now().After(deadline) {
			t.Fatal("afplay lived on after SIGTERM")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
