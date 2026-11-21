package config

import (
	"regexp"
	"slices"
	"strings"
)

// The jammin plugin's settings (docs/proposals/jammin.md) live here, so the
// config needs none of the plugin's packages: a build without it (go build
// -tags nojammin) still reads a config.toml that sets them.

// Jammin is the jammin plugin: music beside the sessions, a source per
// service.
type Jammin struct {
	// Source is the source the Jammin tab opens on when none plays:
	// spotify or music; "" = the first ready.
	Source string `toml:"source"`
	// Spotify is the Spotify source: an app of the person's own, made at
	// Spotify's developer dashboard. Its tokens are the Keychain's, never
	// config.toml's.
	Spotify JamminSpotify `toml:"spotify"`
	// Folder is the folder of music files the files source plays, an
	// absolute or ~ path; "" = ~/Music.
	Folder string `toml:"folder"`
}

// JamminSpotify is the Spotify source's settings.
type JamminSpotify struct {
	// ClientID is the Client ID of the person's Spotify app: 32 hex
	// digits; "" = none yet.
	ClientID string `toml:"client_id"`
}

// JamminSources are the sources' names, in the order they show.
var JamminSources = []string{"spotify", "music", "files"}

// spotifyClientID is a Spotify app's Client ID, as its dashboard shows it.
var spotifyClientID = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// SpotifyClientID is id as a Spotify Client ID, in lower case, and ok when
// it is one: 32 hex digits, around which a copy may have caught spaces.
func SpotifyClientID(id string) (string, bool) {
	id = strings.TrimSpace(id)
	if !spotifyClientID.MatchString(id) {
		return "", false
	}
	return strings.ToLower(id), true
}

// defaultJammin is the plugin's settings until config.toml says otherwise.
func defaultJammin() Jammin { return Jammin{Folder: "~/Music"} }

// JamminOn says whether the jammin plugin runs: plugins.jammin, with the
// plugin built in.
func (c Config) JamminOn() bool { return JamminBuilt && c.Plugins.Jammin }

func checkJammin(j Jammin, bad func(string, ...any)) {
	if j.Source != "" && !slices.Contains(JamminSources, j.Source) {
		bad("jammin.source = %q: must be %s, or empty for the first source ready", j.Source, strings.Join(JamminSources, " or "))
	}
	if j.Folder != "" && !pathLike(j.Folder) {
		bad("jammin.folder = %q: must be an absolute or ~ path to the folder of your music files, or empty for ~/Music", j.Folder)
	}
	if j.Spotify.ClientID != "" {
		if _, ok := SpotifyClientID(j.Spotify.ClientID); !ok {
			bad("jammin.spotify.client_id = %q: must be your Spotify app's Client ID, 32 hex digits from its settings at https://developer.spotify.com/dashboard, or empty for none", j.Spotify.ClientID)
		}
	}
}
