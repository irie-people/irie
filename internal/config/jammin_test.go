package config

import (
	"strings"
	"testing"
)

// TestJamminSettingsAreChecked: the source the tab opens on is one of the
// sources, and a Spotify Client ID is 32 hex digits, taken in either case
// and around spaces a copy caught.
func TestJamminSettingsAreChecked(t *testing.T) {
	c := Default()
	if !c.Plugins.Jammin || c.Jammin.Source != "" || c.Jammin.Spotify.ClientID != "" {
		t.Fatalf("defaults: %+v, on %v", c.Jammin, c.Plugins.Jammin)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("defaults don't validate: %v", err)
	}
	c.Jammin.Source = "tidal"
	c.Jammin.Spotify.ClientID = "not-one"
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "jammin.source") || !strings.Contains(err.Error(), "jammin.spotify.client_id") {
		t.Fatalf("bad settings: %v", err)
	}
	if id, ok := SpotifyClientID(" 0123456789ABCDEF0123456789abcdef\n"); !ok || id != "0123456789abcdef0123456789abcdef" {
		t.Errorf("client id = %q, %v", id, ok)
	}
	for _, bad := range []string{"", "0123456789abcdef", "0123456789abcdef0123456789abcdeg", "0123456789abcdef 0123456789abcdef"} {
		if _, ok := SpotifyClientID(bad); ok {
			t.Errorf("%q taken as a Client ID", bad)
		}
	}
	got, err := Parse([]byte("[jammin]\nsource = \"music\"\n[jammin.spotify]\nclient_id = \"0123456789abcdef0123456789abcdef\"\n"), "config.toml")
	if err != nil || got.Jammin.Source != "music" || got.Jammin.Spotify.ClientID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("parse: %+v, %v", got.Jammin, err)
	}
}
