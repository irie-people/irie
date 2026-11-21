//go:build !nojammin

package jammin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
)

// The Spotify app on this Mac, through its scripting dictionary (Spotify.sdef
// in its bundle), for an account with no app of the person's own linked: it
// needs no Premium and no Client ID, and reaches only the app here, with no
// playlists, search or devices. As with the Music app, the core sends it
// nothing while it isn't running.

// spotifyAppProgram is how the Spotify app's program's path ends, wherever
// the app is.
const spotifyAppProgram = "/Spotify.app/Contents/MacOS/Spotify"

// spotifyAppLook asks the Spotify app how it stands and what plays.
const spotifyAppLook = `function run(argv) {
	var s = Application("Spotify");
	if (!s.running()) return JSON.stringify({running: false});
	var out = {running: true, state: s.playerState(), volume: s.soundVolume(), shuffle: s.shuffling(), repeat: s.repeating()};
	try {
		var t = s.currentTrack;
		out.track = {name: t.name(), artist: t.artist(), album: t.album(), duration: t.duration(), id: t.id(), art: t.artworkUrl()};
		out.position = s.playerPosition();
	} catch (e) {}
	return JSON.stringify(out);
}`

// spotifyAppDo is one control: argv[0] says which, argv[1] and argv[2]
// what with.
const spotifyAppDo = `function run(argv) {
	var s = Application("Spotify");
	if (!s.running()) return "closed";
	var what = argv[0], arg = argv[1], ctx = argv[2];
	switch (what) {
	case "play":
		if (arg) { if (ctx) { s.playTrack(arg, {inContext: ctx}); } else { s.playTrack(arg); } return "ok"; }
		s.play(); return "ok";
	case "pause": s.pause(); return "ok";
	case "toggle": s.playpause(); return "ok";
	case "next": s.nextTrack(); return "ok";
	case "previous": s.previousTrack(); return "ok";
	case "seek": s.playerPosition = Number(arg); return "ok";
	case "volume": s.soundVolume = Number(arg); return "ok";
	case "shuffle": s.shuffling = (arg === "true"); return "ok";
	case "repeat": s.repeating = (arg === "true"); return "ok";
	}
	return "unknown";
}`

// spotifyAppNames are the names of the Spotify app's dictionary the
// scripts use, in its own words, for the test that checks each where the
// app is.
var spotifyAppNames = []string{
	"player state", "sound volume", "shuffling", "repeating", "current track",
	"player position", "artwork url", "play track", "playpause", "next track",
	"previous track",
}

// appRunning says whether the Spotify app runs here, from the process
// table, which asks the app nothing.
func (p *spotify) appRunning() bool {
	if p.sv.Running != nil {
		return p.sv.Running(spotifyAppProgram)
	}
	t, err := proc.Read()
	if err != nil {
		return false
	}
	for _, x := range t.All() {
		if x.Name != "Spotify" || x.Zombie {
			continue
		}
		if argv, _, err := proc.Args(x.PID); err == nil && len(argv) > 0 && strings.HasSuffix(argv[0], spotifyAppProgram) {
			return true
		}
	}
	return false
}

// appState is what spotifyAppLook answers.
type appState struct {
	Running  bool     `json:"running"`
	State    string   `json:"state"`
	Volume   *int     `json:"volume"`
	Shuffle  bool     `json:"shuffle"`
	Repeat   bool     `json:"repeat"`
	Position *float64 `json:"position"`
	Track    *struct {
		Name     string  `json:"name"`
		Artist   string  `json:"artist"`
		Album    string  `json:"album"`
		Duration float64 `json:"duration"`
		ID       string  `json:"id"`
		Art      string  `json:"art"`
	} `json:"track"`
}

// appLook reads the Spotify app on this Mac.
func (p *spotify) appLook(ctx context.Context) ipc.JamSource {
	st := ipc.JamSource{Name: p.Name(), Title: p.Title(), Via: ipc.JamViaApp}
	out, err := p.sv.script(ctx, "JavaScript", spotifyAppLook)
	if err != nil {
		f := musicFailed(st, err)
		f.Problem = strings.ReplaceAll(f.Problem, "Music", "Spotify")
		return f
	}
	var a appState
	if err := json.Unmarshal([]byte(out), &a); err != nil || !a.Running {
		st.State, st.Problem = ipc.JamNeeds, "The Spotify app isn't open."
		return st
	}
	st.State = ipc.JamReady
	st.Can = []string{ipc.JamCanPlay, ipc.JamCanSkip, ipc.JamCanVolume, ipc.JamCanShuffle, ipc.JamCanRepeat}
	if a.Track == nil || a.Track.Name == "" || a.State == "stopped" {
		return st
	}
	t := a.Track
	n := &ipc.JamNow{
		Playing: a.State == "playing",
		Title:   t.Name,
		Album:   t.Album,
		Kind:    "track",
		URI:     t.ID,
		Link:    openLink(t.ID),
		Volume:  a.Volume,
		Shuffle: a.Shuffle,
		Repeat:  ipc.JamRepeatOff,
		At:      p.sv.now().UnixMilli(),
	}
	if strings.HasPrefix(t.Art, "https://") {
		n.Art = t.Art
	}
	if a.Repeat {
		n.Repeat = ipc.JamRepeatAll
	}
	if t.Artist != "" {
		n.Artists = []string{t.Artist}
	}
	// The app gives a track's length in milliseconds; older ones gave
	// seconds.
	if d := t.Duration; d >= 20000 {
		n.Duration = int(d)
	} else if d > 0 {
		n.Duration = int(d * 1000)
	}
	if a.Position != nil && *a.Position > 0 {
		n.Position = int(*a.Position * 1000)
	}
	if strings.HasPrefix(t.ID, "spotify:episode:") {
		n.Kind = "episode"
	} else if strings.HasPrefix(t.ID, "spotify:ad:") {
		n.Kind = "ad"
	}
	if n.Duration > 0 {
		st.Can = append(st.Can, ipc.JamCanSeek)
	}
	st.Now = n
	return st
}

// openLink is the web address of a Spotify URI, such as spotify:track:ID,
// for the link back Spotify's guidelines ask for; "" for none.
func openLink(uri string) string {
	parts := strings.Split(uri, ":")
	if len(parts) != 3 || parts[0] != "spotify" || parts[2] == "" {
		return ""
	}
	switch parts[1] {
	case "track", "album", "playlist", "artist", "episode", "show":
		return "https://open.spotify.com/" + parts[1] + "/" + parts[2]
	}
	return ""
}

// appDo sends one control to the Spotify app.
func (p *spotify) appDo(ctx context.Context, a ipc.JamminArgs) error {
	var what, arg, within string
	switch a.Do {
	case ipc.JamPlay:
		what, arg = "play", strings.TrimSpace(a.URI)
		if arg != "" && !strings.HasPrefix(arg, "spotify:") {
			return fmt.Errorf("%q isn't a Spotify URI, such as spotify:playlist:ID", arg)
		}
		if c := strings.TrimSpace(a.Context); strings.HasPrefix(c, "spotify:") {
			within = c
		}
	case ipc.JamPause:
		what = "pause"
	case ipc.JamToggle:
		what = "toggle"
	case ipc.JamNext:
		what = "next"
	case ipc.JamPrevious:
		what = "previous"
	case ipc.JamSeek:
		what, arg = "seek", strconv.FormatFloat(float64(a.Position)/1000, 'f', 3, 64)
	case ipc.JamVolume:
		what, arg = "volume", strconv.Itoa(*a.Volume)
	case ipc.JamShuffle:
		what, arg = "shuffle", strconv.FormatBool(*a.On)
	case ipc.JamRepeat:
		// The app repeats or doesn't; a track's repeat is the whole list's.
		what, arg = "repeat", strconv.FormatBool(a.Repeat != ipc.JamRepeatOff)
	default:
		return fmt.Errorf("the Spotify app on this Mac takes no %q: link your account for that", a.Do)
	}
	out, err := p.sv.script(ctx, "JavaScript", spotifyAppDo, what, arg, within)
	if err != nil {
		return errors.New(strings.ReplaceAll(musicFailed(ipc.JamSource{}, err).Problem, "Music", "Spotify"))
	}
	switch strings.TrimSpace(out) {
	case "ok":
	case "closed":
		return errors.New("the Spotify app isn't open")
	default:
		return fmt.Errorf("the Spotify app didn't take %s", what)
	}
	p.mu.Lock()
	p.next = p.sv.now() // ask again at once
	p.mu.Unlock()
	return nil
}
