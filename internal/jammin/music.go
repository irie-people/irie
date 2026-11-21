//go:build !nojammin

package jammin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
)

// The Music app, through its scripting dictionary (com.apple.Music.sdef in
// its bundle): scripts of the core's own in JavaScript for Automation,
// which take their words as the run handler's arguments, so no search or
// name becomes part of a script, and answer in JSON.

// musicProgram is the Music app's program, by which the process table
// tells it runs.
const musicProgram = "/System/Applications/Music.app/Contents/MacOS/Music"

// musicApp is the Music app's bundle, whose being there makes the source,
// and musicBundle its ID, by which it is started.
const (
	musicApp    = "/System/Applications/Music.app"
	musicBundle = "com.apple.Music"
)

// musicStart is how long a Play waits for a Music app it started to run.
const musicStart = 10 * time.Second

// musicEvery is how often the core asks the Music app what plays, whoever
// asks it.
const musicEvery = time.Second

// artKept is how many tracks' artwork the core keeps.
const artKept = 20

// musicLook asks the Music app how it stands and what plays. It asks
// nothing of an app that isn't running, which a property's read would
// start.
const musicLook = `function run(argv) {
	var m = Application("Music");
	if (!m.running()) return JSON.stringify({running: false});
	var out = {running: true, state: m.playerState(), volume: m.soundVolume(), shuffle: m.shuffleEnabled(), repeat: m.songRepeat()};
	if (out.state !== "stopped") {
		try {
			var t = m.currentTrack;
			out.track = {name: t.name(), artist: t.artist(), album: t.album(), duration: t.duration(), id: t.persistentID()};
			out.position = m.playerPosition();
		} catch (e) {}
		try { out.list = {name: m.currentPlaylist.name(), id: m.currentPlaylist.persistentID()}; } catch (e) {}
	}
	return JSON.stringify(out);
}`

// musicDo is one control: argv[0] says which, argv[1] what with.
const musicDo = `function run(argv) {
	var m = Application("Music");
	var what = argv[0], arg = argv[1];
	switch (what) {
	case "play":
		if (arg) {
			var lists = m.playlists.whose({persistentID: arg});
			if (lists.length > 0) { m.play(lists[0]); return "ok"; }
			var tracks = m.libraryPlaylists[0].tracks.whose({persistentID: arg});
			if (tracks.length > 0) { m.play(tracks[0]); return "ok"; }
			return "missing";
		}
		m.play(); return "ok";
	case "pause": m.pause(); return "ok";
	case "toggle": m.playpause(); return "ok";
	case "next": m.nextTrack(); return "ok";
	case "previous": m.backTrack(); return "ok";
	case "seek": m.playerPosition = Number(arg); return "ok";
	case "volume": m.soundVolume = Number(arg); return "ok";
	case "shuffle": m.shuffleEnabled = (arg === "true"); return "ok";
	case "repeat": m.songRepeat = arg; return "ok";
	}
	return "unknown";
}`

// musicLists are the user playlists, without folders and the special
// ones.
const musicLists = `function run(argv) {
	var m = Application("Music");
	if (!m.running()) return "[]";
	var ps = m.userPlaylists;
	var names = ps.name(), ids = ps.persistentID(), kinds = ps.specialKind(), smart = ps.smart();
	var out = [];
	for (var i = 0; i < names.length; i++) {
		if (kinds[i] === "none") out.push({name: names[i], id: ids[i], smart: smart[i]});
	}
	return JSON.stringify(out);
}`

// musicSearch searches the library for argv[0], the first argv[1] tracks.
const musicSearch = `function run(argv) {
	var m = Application("Music");
	if (!m.running()) return "[]";
	var found = m.search(m.libraryPlaylists[0], {for: argv[0]}) || [];
	var most = Number(argv[1]), out = [];
	for (var i = 0; i < found.length && i < most; i++) {
		var t = found[i];
		try { out.push({name: t.name(), artist: t.artist(), album: t.album(), duration: t.duration(), id: t.persistentID()}); } catch (e) {}
	}
	return JSON.stringify(out);
}`

// musicArt writes the current track's artwork to the file argv[0]. The
// Music app gives it as data, so this one is AppleScript, which writes data
// to a file as it is.
const musicArt = `on run argv
	set p to item 1 of argv
	if application "Music" is not running then return "none"
	tell application "Music"
		try
			set d to raw data of artwork 1 of current track
		on error
			return "none"
		end try
	end tell
	set f to open for access (POSIX file p) with write permission
	try
		set eof f to 0
		write d to f
		close access f
	on error e
		close access f
		error e
	end try
	return "ok"
end run`

// musicNames are the names of the Music app's dictionary the scripts use,
// in the dictionary's own words, for the test that checks each is there.
var musicNames = []string{
	"player state", "sound volume", "shuffle enabled", "song repeat",
	"current track", "persistent ID", "player position", "current playlist",
	"playpause", "next track", "back track", "user playlist", "library playlist",
	"special kind", "smart", "search", "raw data", "artwork",
}

// music is the Apple Music source: the Music app on this Mac.
type music struct {
	sv *Service

	mu   sync.Mutex
	last ipc.JamSource
	next time.Time
	art  struct{ track, file string } // the current track's artwork, written
}

func (*music) Name() string  { return ipc.JamMusic }
func (*music) Title() string { return "Apple Music" }

// running says whether the Music app runs, from the process table: its
// program by name, then by its path, which asks the app nothing.
func (m *music) running() bool {
	if m.sv.Running != nil {
		return m.sv.Running(musicProgram)
	}
	t, err := proc.Read()
	if err != nil {
		return false
	}
	for _, p := range t.All() {
		if p.Name != "Music" || p.Zombie {
			continue
		}
		if argv, _, err := proc.Args(p.PID); err == nil && len(argv) > 0 && argv[0] == musicProgram {
			return true
		}
	}
	return false
}

// musicState is what musicLook answers.
type musicState struct {
	Running  bool        `json:"running"`
	State    string      `json:"state"`
	Volume   *int        `json:"volume"`
	Shuffle  bool        `json:"shuffle"`
	Repeat   string      `json:"repeat"`
	Position *float64    `json:"position"`
	Track    *musicTrack `json:"track"`
	List     *struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"list"`
}

// musicTrack is a track as the scripts give it; its duration in seconds.
type musicTrack struct {
	Name     string  `json:"name"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Duration float64 `json:"duration"`
	ID       string  `json:"id"`
}

// persistentID is the form of a persistent ID: 16 hex digits.
var persistentID = regexp.MustCompile(`^[0-9A-F]{16}$`)

func (m *music) Look(ctx context.Context) ipc.JamSource {
	m.mu.Lock()
	if m.sv.now().Before(m.next) {
		last := m.last
		m.mu.Unlock()
		return last
	}
	m.mu.Unlock()
	st := m.read(ctx)
	m.mu.Lock()
	m.last, m.next = st, m.sv.now().Add(musicEvery)
	m.mu.Unlock()
	return st
}

func (m *music) read(ctx context.Context) ipc.JamSource {
	st := ipc.JamSource{Name: m.Name(), Title: m.Title()}
	if _, err := os.Stat(musicApp); err != nil && m.sv.Running == nil {
		st.State, st.Problem = ipc.JamNeeds, "There is no Music app on this computer: Apple Music needs a Mac's."
		return st
	}
	if !m.running() {
		st.State, st.Problem = ipc.JamNeeds, "The Music app isn't open: Open Music, or Play, starts it."
		st.Can = []string{ipc.JamCanPlay}
		return st
	}
	out, err := m.sv.script(ctx, "JavaScript", musicLook)
	if err != nil {
		return musicFailed(st, err)
	}
	var ms musicState
	if err := json.Unmarshal([]byte(out), &ms); err != nil {
		st.State, st.Problem = ipc.JamError, "The Music app's answer didn't read."
		return st
	}
	if !ms.Running {
		st.State, st.Problem = ipc.JamNeeds, "The Music app isn't open: Open Music, or Play, starts it."
		st.Can = []string{ipc.JamCanPlay}
		return st
	}
	st.State = ipc.JamReady
	st.Can = []string{ipc.JamCanPlay, ipc.JamCanSkip, ipc.JamCanVolume, ipc.JamCanShuffle, ipc.JamCanRepeat, ipc.JamCanLists, ipc.JamCanSearch}
	if ms.Track == nil || ms.State == "stopped" {
		return st
	}
	t := *ms.Track
	n := &ipc.JamNow{
		Playing:  ms.State == "playing" || ms.State == "fast forwarding" || ms.State == "rewinding",
		Title:    t.Name,
		Album:    t.Album,
		Kind:     "track",
		URI:      t.ID,
		Duration: int(t.Duration * 1000),
		Volume:   ms.Volume,
		Shuffle:  ms.Shuffle,
		Repeat:   map[string]string{"one": ipc.JamRepeatTrack, "all": ipc.JamRepeatAll}[ms.Repeat],
		At:       m.sv.now().UnixMilli(),
	}
	if n.Repeat == "" {
		n.Repeat = ipc.JamRepeatOff
	}
	if t.Artist != "" {
		n.Artists = []string{t.Artist}
	}
	if ms.Position != nil && *ms.Position > 0 {
		n.Position = int(*ms.Position * 1000)
	}
	if ms.List != nil && ms.List.Name != "" {
		n.Context = &ipc.JamItem{Kind: "playlist", Title: ms.List.Name, URI: ms.List.ID}
	}
	if n.Duration > 0 {
		st.Can = append(st.Can, ipc.JamCanSeek)
	}
	n.Art = m.artwork(ctx, t.ID)
	st.Now = n
	return st
}

// musicFailed is st after a script that failed: macOS's refusal to let
// Irie control Music in words, and the rest as an error.
func musicFailed(st ipc.JamSource, err error) ipc.JamSource {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "-1743") || strings.Contains(strings.ToLower(msg), "not authorized"):
		st.State, st.Problem = ipc.JamNeeds, "macOS hasn't let "+brand.Name+" control Music: allow it in System Settings › Privacy & Security › Automation, under "+brand.Name+"."
	case strings.Contains(msg, "-600"):
		st.State, st.Problem = ipc.JamNeeds, "The Music app isn't open: Open Music, or Play, starts it."
		st.Can = []string{ipc.JamCanPlay}
	default:
		st.State, st.Problem = ipc.JamError, "The Music app didn't answer: "+sentence(msg)
	}
	return st
}

// artwork is the file holding a track's artwork, written when the track
// changes, or "" when it has none. The newest artKept are kept.
func (m *music) artwork(ctx context.Context, track string) string {
	if !persistentID.MatchString(track) || m.sv.Dir == "" {
		return ""
	}
	m.mu.Lock()
	if m.art.track == track {
		file := m.art.file
		m.mu.Unlock()
		return file
	}
	m.mu.Unlock()
	dir := filepath.Join(m.sv.Dir, "art")
	file := ""
	if err := os.MkdirAll(dir, 0o700); err == nil {
		raw := filepath.Join(dir, "music-"+track+".part")
		if out, err := m.sv.script(ctx, "AppleScript", musicArt, raw); err == nil && strings.TrimSpace(out) == "ok" {
			file = keepArt(raw)
		} else {
			os.Remove(raw)
		}
		trimArt(dir)
	}
	m.mu.Lock()
	m.art.track, m.art.file = track, file
	m.mu.Unlock()
	return file
}

// keepArt names a written artwork by what it is, PNG or JPEG, or removes
// it when it is neither.
func keepArt(raw string) string {
	b, err := os.ReadFile(raw)
	ext := ""
	switch {
	case err != nil:
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		ext = ".png"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		ext = ".jpg"
	}
	if ext == "" {
		os.Remove(raw)
		return ""
	}
	file := strings.TrimSuffix(raw, ".part") + ext
	if err := os.Rename(raw, file); err != nil {
		os.Remove(raw)
		return ""
	}
	return file
}

// trimArt keeps the newest artKept artworks in dir.
func trimArt(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type art struct {
		path string
		at   time.Time
	}
	var all []art
	for _, e := range ents {
		if info, err := e.Info(); err == nil && e.Type().IsRegular() {
			all = append(all, art{filepath.Join(dir, e.Name()), info.ModTime()})
		}
	}
	slices.SortFunc(all, func(a, b art) int { return b.at.Compare(a.at) })
	for _, a := range all[min(len(all), artKept):] {
		os.Remove(a.path)
	}
}

func (m *music) Do(ctx context.Context, a ipc.JamminArgs) error {
	var what, arg string
	switch a.Do {
	case ipc.JamPlay:
		what, arg = "play", strings.TrimSpace(a.URI)
		if arg != "" && !persistentID.MatchString(arg) {
			return fmt.Errorf("%q isn't a playlist or track of the Music app's: they go by a persistent ID of 16 hex digits", arg)
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
		what, arg = "repeat", map[string]string{ipc.JamRepeatOff: "off", ipc.JamRepeatTrack: "one", ipc.JamRepeatAll: "all"}[a.Repeat]
	default:
		return fmt.Errorf("Apple Music takes no %q", a.Do)
	}
	// A pause of a Music app that isn't running is no pause at all, and
	// asking would start it. A play starts it first, hidden and in the
	// background, since a script would start it with its window in sight.
	if !m.running() {
		if what != "play" {
			return errors.New("the Music app isn't open")
		}
		if err := m.start(ctx); err != nil {
			return err
		}
	}
	out, err := m.sv.script(ctx, "JavaScript", musicDo, what, arg)
	if err != nil {
		return errors.New(musicFailed(ipc.JamSource{}, err).Problem)
	}
	switch strings.TrimSpace(out) {
	case "ok":
	case "missing":
		return errors.New("the Music app has no playlist or track by that ID: Lists shows yours")
	default:
		return fmt.Errorf("the Music app didn't take %s", what)
	}
	m.mu.Lock()
	m.next = time.Time{} // ask again at once
	m.mu.Unlock()
	return nil
}

// start starts the Music app hidden and in the background, and waits until
// it runs.
func (m *music) start(ctx context.Context) error {
	launch := m.sv.Launch
	if launch == nil {
		launch = func(bundle string) error {
			return exec.CommandContext(ctx, "/usr/bin/open", "-g", "-j", "-b", bundle).Run()
		}
	}
	if err := launch(musicBundle); err != nil {
		return fmt.Errorf("the Music app didn't start: %v", err)
	}
	for waited := time.Duration(0); waited < musicStart; waited += 250 * time.Millisecond {
		if m.running() {
			return nil
		}
		if err := m.sv.wait(ctx, 250*time.Millisecond); err != nil {
			return err
		}
	}
	return errors.New("the Music app didn't start within 10 seconds")
}

func (m *music) Lists(ctx context.Context) ([]ipc.JamItem, error) {
	out, err := m.sv.script(ctx, "JavaScript", musicLists)
	if err != nil {
		return nil, errors.New(musicFailed(ipc.JamSource{}, err).Problem)
	}
	var lists []struct {
		Name  string `json:"name"`
		ID    string `json:"id"`
		Smart bool   `json:"smart"`
	}
	if err := json.Unmarshal([]byte(out), &lists); err != nil {
		return nil, errors.New("the Music app's playlists didn't read")
	}
	var items []ipc.JamItem
	for _, l := range lists {
		if persistentID.MatchString(l.ID) && l.Name != "" {
			items = append(items, ipc.JamItem{Kind: "playlist", Title: l.Name, URI: l.ID})
		}
	}
	return items, nil
}

// searchMost is the most tracks a search of the library gives.
const searchMost = 25

func (m *music) Search(ctx context.Context, words string) ([]ipc.JamItem, error) {
	out, err := m.sv.script(ctx, "JavaScript", musicSearch, words, strconv.Itoa(searchMost))
	if err != nil {
		return nil, errors.New(musicFailed(ipc.JamSource{}, err).Problem)
	}
	var tracks []musicTrack
	if err := json.Unmarshal([]byte(out), &tracks); err != nil {
		return nil, errors.New("the Music app's search didn't read")
	}
	var items []ipc.JamItem
	for _, t := range tracks {
		if persistentID.MatchString(t.ID) {
			items = append(items, ipc.JamItem{Kind: "track", Title: t.Name, By: t.Artist, Album: t.Album, URI: t.ID, Duration: int(t.Duration * 1000)})
		}
	}
	return items, nil
}

func (m *music) Devices(context.Context) ([]ipc.JamDevice, error) {
	return nil, errors.New("Apple Music plays here, on this Mac: AirPlay devices come later")
}
