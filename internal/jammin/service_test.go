//go:build !nojammin

package jammin

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// TestStartingOneSourcePausesAnotherThatPlaysHere: Spotify starting
// pauses the Music app playing on this Mac first; Music starting pauses
// Spotify when it plays on this computer, and leaves it playing on a
// phone in another room.
func TestStartingOneSourcePausesAnotherThatPlaysHere(t *testing.T) {
	k := musicKit(t, musicPlaying)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's MacBook", "Computer", false)})
	k.do(t, ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamSpotify})
	if got := k.script.did(); !slices.Equal(got, []string{"pause"}) {
		t.Fatalf("Spotify starting sent Music %v, want a pause", got)
	}

	// Spotify plays on this Mac: Music starting pauses it first.
	k = musicKit(t, `{"running":true,"state":"paused","volume":70,"shuffle":false,"repeat":"off","track":{"name":"x","artist":"y","album":"z","duration":100,"id":"0123456789ABCDEF"}}`)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's MacBook", "Computer", true)})
	k.do(t, ipc.JamminArgs{Do: ipc.JamToggle, Source: ipc.JamMusic})
	sent := k.spotify.seen()
	if !slices.Contains(sent, "PUT /v1/me/player/pause") || !slices.Equal(k.script.did(), []string{"toggle"}) {
		t.Fatalf("Music starting: Spotify got %v, Music %v", sent, k.script.did())
	}

	// Spotify plays on a phone: it keeps playing.
	k = musicKit(t, `{"running":true,"state":"paused","volume":70,"shuffle":false,"repeat":"off"}`)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's Phone", "Smartphone", true)})
	k.do(t, ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamMusic})
	if slices.Contains(k.spotify.seen(), "PUT /v1/me/player/pause") {
		t.Fatal("Music starting paused Spotify on a phone")
	}

	// A pause of one playing pauses nothing else.
	k = musicKit(t, musicPlaying)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's MacBook", "Computer", true)})
	k.do(t, ipc.JamminArgs{Do: ipc.JamPause, Source: ipc.JamSpotify})
	if got := k.script.did(); len(got) != 0 {
		t.Fatalf("a pause of Spotify sent Music %v", got)
	}
}

// TestTheSourceACallIsAbout: the one asked for, else the one playing, else
// jammin.source when ready, else the first ready.
func TestTheSourceACallIsAbout(t *testing.T) {
	k := musicKit(t, musicPlaying)
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	if r := k.do(t, ipc.JamminArgs{}); r.Source != ipc.JamMusic {
		t.Errorf("Music playing: %q", r.Source)
	}
	k = musicKit(t, `{"running":true,"state":"paused","volume":70,"shuffle":false,"repeat":"off"}`)
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	if r := k.do(t, ipc.JamminArgs{}); r.Source != ipc.JamSpotify {
		t.Errorf("nothing playing: %q", r.Source)
	}
	k.cfg.Source = ipc.JamMusic
	if r := k.do(t, ipc.JamminArgs{}); r.Source != ipc.JamMusic {
		t.Errorf("jammin.source: %q", r.Source)
	}
	if r := k.do(t, ipc.JamminArgs{Source: ipc.JamSpotify}); r.Source != ipc.JamSpotify {
		t.Errorf("asked: %q", r.Source)
	}
}

// TestCallsTheOpRefuses: an unknown source or call, and controls with no
// value or one out of range, each say why.
func TestCallsTheOpRefuses(t *testing.T) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Alice's MacBook", "Computer", true)})
	big, low := 101, -1
	for _, c := range []struct {
		a    ipc.JamminArgs
		says string
	}{
		{ipc.JamminArgs{Source: "tidal"}, "no source called"},
		{ipc.JamminArgs{Do: "dance"}, "not \"dance\""},
		{ipc.JamminArgs{Do: ipc.JamVolume, Source: ipc.JamSpotify}, "0 to 100"},
		{ipc.JamminArgs{Do: ipc.JamVolume, Source: ipc.JamSpotify, Volume: &big}, "0 to 100"},
		{ipc.JamminArgs{Do: ipc.JamVolume, Source: ipc.JamSpotify, Volume: &low}, "0 to 100"},
		{ipc.JamminArgs{Do: ipc.JamSeek, Source: ipc.JamSpotify, Position: -5}, "0 or more"},
		{ipc.JamminArgs{Do: ipc.JamShuffle, Source: ipc.JamSpotify}, "on or off"},
		{ipc.JamminArgs{Do: ipc.JamRepeat, Source: ipc.JamSpotify, Repeat: "forever"}, "off, track or all"},
		{ipc.JamminArgs{Do: ipc.JamMove, Source: ipc.JamSpotify}, "which device"},
		{ipc.JamminArgs{Do: ipc.JamSignIn, Source: ipc.JamMusic}, "only Spotify"},
		{ipc.JamminArgs{Do: ipc.JamNext, Source: ipc.JamMusic}, "isn't open"},
	} {
		_, err := k.sv.Do(context.Background(), c.a)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%+v: %v, want %q", c.a, err, c.says)
		}
		k.clock.add(time.Minute)
	}
	// With nothing ready, a control says what to do.
	k.cfg.Spotify.ClientID = ""
	if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamNext}); err == nil || !strings.Contains(err.Error(), "no source is ready") {
		t.Errorf("nothing ready: %v", err)
	}
}

// TestTheTextSaysEachSource: :jammin's words, a line a source.
func TestTheTextSaysEachSource(t *testing.T) {
	k := musicKit(t, musicPlaying)
	k.spotify.on("GET /v1/me/player", answer{status: 200, body: playing("Harbour Lights", "Kitchen", "Speaker", false)})
	r := k.do(t, ipc.JamminArgs{})
	want := "Spotify: paused on “Harbour Lights” by Night Owls, The Tide on Kitchen.\nApple Music: playing “Harbour Lights” by Night Owls.\nFiles: There's no folder at ~/Music: Choose Folder in the Jammin tab, or set jammin.folder, to play the music files you keep."
	if r.Text != want {
		t.Errorf("text = %q, want %q", r.Text, want)
	}
}

// TestKeychain keeps a token in a temporary keychain of its own.
func TestKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" || testing.Short() {
		t.Skip("macOS's Keychain")
	}
	path := filepath.Join(t.TempDir(), "test.keychain-db")
	if out, err := exec.Command("/usr/bin/security", "create-keychain", "-p", "test", path).CombinedOutput(); err != nil {
		t.Skipf("security create-keychain: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("/usr/bin/security", "delete-keychain", path).Run() })
	k := Keychain{Path: path}
	if _, err := k.Get(tokenKey); err != ErrNoToken {
		t.Fatalf("empty: %v", err)
	}
	for _, tok := range []string{"AQD-abc_def", `p"a\ss'x`} {
		if err := k.Set(tokenKey, tok); err != nil {
			t.Fatalf("set %q: %v", tok, err)
		}
		if got, err := k.Get(tokenKey); err != nil || got != tok {
			t.Errorf("get = %q, %v; want %q", got, err, tok)
		}
	}
	if err := k.Delete(tokenKey); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get(tokenKey); err != ErrNoToken {
		t.Errorf("after delete: %v", err)
	}
	if err := k.Set("../x", "t"); err == nil {
		t.Error("a key that names no source was taken")
	}
}

// TestFilesStore keeps a token in a file of the user's alone.
func TestFilesStore(t *testing.T) {
	f := Files{Dir: filepath.Join(t.TempDir(), "keys")}
	if _, err := f.Get(tokenKey); err != ErrNoToken {
		t.Fatalf("empty: %v", err)
	}
	if err := f.Set(tokenKey, "AQD-abc"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Get(tokenKey); got != "AQD-abc" {
		t.Fatalf("got %q", got)
	}
	if err := f.Set(tokenKey, "two words"); err == nil {
		t.Error("a token with a space was kept")
	}
	if err := f.Delete(tokenKey); err != nil {
		t.Fatal(err)
	}
}
