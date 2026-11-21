//go:build !nojammin

package plugins

import (
	"os"
	"runtime"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// The jammin plugin's parts, by name: its sources.
const (
	JamminSpotify = "spotify"
	JamminMusic   = "music"
)

// PaneJammin is the kind of the Jammin tab's pane: a view of the Mac app's
// own (docs/proposals/plugin-panes.md).
const PaneJammin = "jammin"

// spotifyDashboard is where a person makes the Spotify app Jammin reads
// their account through, and the redirect URI it must list.
const (
	spotifyDashboard = "https://developer.spotify.com/dashboard"
	spotifyRedirect  = "http://127.0.0.1/jammin/spotify"
)

// musicApp is the Music app's bundle, whose being there readies its part.
var musicApp = "/System/Applications/Music.app"

func init() { built["jammin"] = jamminPlugin }

// jamminPlugin is the jammin plugin: the music a person plays, beside the
// sessions, a source per service, one source at a time
// (docs/proposals/jammin.md).
func jamminPlugin() Plugin {
	brings := []string{"the Jammin tab in the right sidebar", ":jammin", brand.Slug + " jammin"}
	return Plugin{
		Name:    "jammin",
		Title:   "Jammin",
		Summary: "Your jam box: the music you play, beside your sessions, from Spotify through an app of yours and from the Music app on this Mac, one source at a time. What it reads stays in memory, and agents can't reach it.",
		key:     "plugins.jammin",
		on:      func(c config.Config) bool { return c.Plugins.Jammin },
		Parts: []Part{
			{
				Name:    JamminSpotify,
				Title:   "Spotify",
				Summary: "Your Spotify account, through a Spotify app of yours: what plays on any device of yours, with play, pause, skip, seek, volume and devices, your playlists and a search. Spotify lets apps control playback only for Premium accounts.",
				Setup:   ":jammin",
				Brings:  brings,
				own:     always,
				check:   checkSpotify,
				// The tab is the plugin's, and shows with its first part,
				// which is on while the plugin is.
				Pane: &ipc.PluginPane{ID: "jammin.tab", Title: "Jammin", Icon: "radio", Kind: PaneJammin},
			},
			{
				Name:    JamminMusic,
				Title:   "Apple Music",
				Summary: "The Music app on this Mac: your library and Apple Music, played, paused and skipped from the Jammin tab, with your playlists and a search of your library.",
				Setup:   ":jammin",
				Brings:  brings,
				own:     always,
				check:   checkMusic,
			},
		},
	}
}

// live is the named source by the core's last look, or nil before one.
func live(e Env, name string) *ipc.JamSource {
	for i, st := range e.Live.Jammin {
		if st.Name == name && st.State != "" {
			return &e.Live.Jammin[i]
		}
	}
	return nil
}

// checkSpotify says how the Spotify source stands: from config.toml and
// the core's last look, which it keeps in memory. It reads no token and
// asks Spotify nothing.
func checkSpotify(e Env, _ bool) (bool, string) {
	if _, ok := config.SpotifyClientID(e.Config.Jammin.Spotify.ClientID); !ok {
		return false, "No Spotify app yet: make one at " + spotifyDashboard + " with the redirect URI " + spotifyRedirect + ", then Link Spotify in the Jammin tab, or `" + brand.Slug + " jammin link spotify`, takes its Client ID. Spotify lets apps in development mode work only for Premium accounts."
	}
	st := live(e, JamminSpotify)
	switch {
	case st == nil:
		return true, "Ready once signed in: your Spotify app's Client ID is kept, and Link Spotify in the Jammin tab signs in, if it hasn't yet."
	case st.State == ipc.JamReady && st.Account != "":
		return true, "Ready: signed in as " + st.Account + ". The Jammin tab, :jammin or `" + brand.Slug + " jammin` plays it."
	case st.State == ipc.JamReady:
		return true, "Ready: signed in. The Jammin tab, :jammin or `" + brand.Slug + " jammin` plays it."
	case st.State == ipc.JamLinking:
		return false, "Signing in: finish it on Spotify's page."
	}
	return false, strings.TrimSpace(st.Problem)
}

// checkMusic says how the Apple Music source stands: whether the Music app
// is here, and whether macOS let Irie control it at the core's last look.
// That the app isn't open is no lack: Play starts it.
func checkMusic(e Env, _ bool) (bool, string) {
	if runtime.GOOS != "darwin" {
		return false, "Apple Music is the Music app on a Mac, and this computer isn't one."
	}
	if _, err := os.Stat(musicApp); err != nil {
		return false, "There is no Music app on this Mac at " + musicApp + "."
	}
	if st := live(e, JamminMusic); st != nil && st.State != ipc.JamReady && strings.Contains(st.Problem, "Automation") {
		return false, st.Problem
	}
	return true, "Ready: the Music app on this Mac. macOS asks you once whether " + brand.Name + " may control it. The Jammin tab, :jammin or `" + brand.Slug + " jammin --source music` plays it."
}
