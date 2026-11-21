//go:build !nojammin

package plugins

import (
	"runtime"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// TestJamminIsOnByDefaultWithItsTab: the plugin is on, its tab comes with
// its Spotify part, and Spotify needs an app until a Client ID is kept,
// then stands as the core's last look says.
func TestJamminIsOnByDefaultWithItsTab(t *testing.T) {
	cfg := config.Default()
	p, ok := Find("jammin")
	if !ok || !p.IsOn(cfg) || p.Key() != "plugins.jammin" || len(p.Parts) != 3 {
		t.Fatalf("plugin = %+v, on %v", p, ok && p.IsOn(cfg))
	}
	if pane := p.Parts[0].Pane; pane == nil || pane.ID != "jammin.tab" || pane.Kind != PaneJammin {
		t.Fatalf("pane = %+v", p.Parts[0].Pane)
	}
	e := Env{Config: cfg}
	if ok, text := checkSpotify(e, true); ok || !strings.Contains(text, "developer.spotify.com/dashboard") || !strings.Contains(text, spotifyRedirect) {
		t.Errorf("no app: %v %q", ok, text)
	}
	e.Config.Jammin.Spotify.ClientID = "0123456789abcdef0123456789abcdef"
	if ok, text := checkSpotify(e, true); !ok || !strings.Contains(text, "Link Spotify") {
		t.Errorf("an app, no look yet: %v %q", ok, text)
	}
	e.Live.Jammin = []ipc.JamSource{{Name: JamminSpotify, State: ipc.JamReady, Account: "Alice"}}
	if ok, text := checkSpotify(e, true); !ok || !strings.Contains(text, "signed in as Alice") {
		t.Errorf("signed in: %v %q", ok, text)
	}
	e.Live.Jammin = []ipc.JamSource{{Name: JamminSpotify, State: ipc.JamNeeds, Problem: "Not signed in to Spotify yet."}}
	if ok, text := checkSpotify(e, true); ok || text != "Not signed in to Spotify yet." {
		t.Errorf("signed out: %v %q", ok, text)
	}
	if why := Why(cfg, JamminSpotify); why != "" {
		t.Errorf("why, on: %q", why)
	}
	cfg.Plugins.Jammin = false
	if why := Why(cfg, JamminMusic); !strings.Contains(why, "plugins on jammin") {
		t.Errorf("why, off: %q", why)
	}
}

// TestJamminMusicIsReadyWhereTheMusicAppIs: the Music app in its place
// readies the part, whether or not it is open; macOS's refusal to let Irie
// control it, at the core's last look, is a lack.
func TestJamminMusicIsReadyWhereTheMusicAppIs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		if ok, _ := checkMusic(Env{}, true); ok {
			t.Fatal("ready off a Mac")
		}
		return
	}
	old := musicApp
	t.Cleanup(func() { musicApp = old })
	musicApp = t.TempDir()
	e := Env{Config: config.Default()}
	e.Live.Jammin = []ipc.JamSource{{Name: JamminMusic, State: ipc.JamNeeds, Problem: "The Music app isn't open: Open Music, or Play, starts it."}}
	if ok, text := checkMusic(e, true); !ok || !strings.Contains(text, "Ready") {
		t.Errorf("not open: %v %q", ok, text)
	}
	e.Live.Jammin[0].Problem = "macOS hasn't let it control Music: allow it in System Settings › Privacy & Security › Automation."
	if ok, text := checkMusic(e, true); ok || !strings.Contains(text, "Automation") {
		t.Errorf("refused: %v %q", ok, text)
	}
	musicApp = "/nowhere/Music.app"
	if ok, _ := checkMusic(Env{Config: config.Default()}, true); ok {
		t.Error("ready with no Music app")
	}
}

// TestJamminFilesAreReadyWhereTheirFolderIs: the folder of music files
// readies the part, and one that isn't there says how to pick another.
func TestJamminFilesAreReadyWhereTheirFolderIs(t *testing.T) {
	e := Env{Config: config.Default()}
	e.Config.Jammin.Folder = t.TempDir()
	if ok, text := checkFiles(e, true); !ok || !strings.Contains(text, "Ready: the music files in") {
		t.Errorf("a folder: %v %q", ok, text)
	}
	e.Config.Jammin.Folder = "/nowhere/Music"
	if ok, text := checkFiles(e, true); ok || !strings.Contains(text, "jammin folder PATH") {
		t.Errorf("no folder: %v %q", ok, text)
	}
}
