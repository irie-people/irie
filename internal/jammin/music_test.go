//go:build !nojammin

package jammin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/irie-people/irie/internal/ipc"
)

// musicPlaying is the look script's answer while a track plays.
const musicPlaying = `{"running":true,"state":"playing","volume":70,"shuffle":false,"repeat":"one","position":12.5,
"track":{"name":"Harbour Lights","artist":"Night Owls","album":"Low Tide","duration":245.25,"id":"0123456789ABCDEF"},
"list":{"name":"Morning Focus","id":"FEDCBA9876543210"}}`

// pngBytes is the start of a PNG, enough for the artwork's sniff.
var pngBytes = []byte("\x89PNG\r\n\x1a\nrest")

// musicKit is a kit whose Music app runs and answers look with look.
func musicKit(t *testing.T, look string) *kit {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	k.running = true
	k.script.answer = func(r scriptRun) (string, error) {
		switch r.script {
		case musicLook:
			return look, nil
		case musicArt:
			return "ok", os.WriteFile(r.args[0], pngBytes, 0o600)
		}
		return "ok", nil
	}
	return k
}

// TestMusicNotRunningIsSentNothing: while the Music app isn't running the
// core runs no script at all, since asking would start it, and the source
// says Play or Open Music starts it.
func TestMusicNotRunningIsSentNothing(t *testing.T) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamMusic)
	if s.State != ipc.JamNeeds || !strings.Contains(s.Problem, "isn't open") || !slices.Equal(s.Can, []string{ipc.JamCanPlay}) {
		t.Fatalf("not running: %+v", s)
	}
	if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamPause, Source: ipc.JamMusic}); err == nil {
		t.Error("a pause of a Music app that isn't running was taken")
	}
	if len(k.script.runs) != 0 {
		t.Fatalf("scripts ran: %+v", k.script.runs)
	}
	// Play is the one control that starts it: hidden and in the
	// background first, then the script.
	k.do(t, ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamMusic})
	if got := k.script.did(); !slices.Equal(got, []string{"play"}) || !slices.Equal(k.started, []string{"com.apple.Music"}) {
		t.Fatalf("play sent %v, started %v", got, k.started)
	}
	k.script.answer = func(r scriptRun) (string, error) {
		if r.script == musicLook {
			return musicPlaying, nil
		}
		return "ok", nil
	}
	k.clock.add(2 * time.Second)
	k.do(t, ipc.JamminArgs{Do: ipc.JamPause, Source: ipc.JamMusic})
	if len(k.started) != 1 {
		t.Fatalf("a running Music app was started again: %v", k.started)
	}
}

// TestMusicLookReadsTheTrackInMilliseconds: the look script's JSON becomes
// what plays: the track, its artist and album, duration and position in
// milliseconds, song repeat "one" as track, the playlist it plays within,
// and its artwork written to a file named by the track.
func TestMusicLookReadsTheTrackInMilliseconds(t *testing.T) {
	k := musicKit(t, musicPlaying)
	s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamMusic)
	n := s.Now
	if s.State != ipc.JamReady || n == nil || !n.Playing || n.Title != "Harbour Lights" || !slices.Equal(n.Artists, []string{"Night Owls"}) || n.Album != "Low Tide" ||
		n.Duration != 245250 || n.Position != 12500 || n.Repeat != ipc.JamRepeatTrack || n.Shuffle || n.Volume == nil || *n.Volume != 70 || n.URI != "0123456789ABCDEF" {
		t.Fatalf("now = %+v", n)
	}
	if n.Context == nil || n.Context.Title != "Morning Focus" || n.Context.URI != "FEDCBA9876543210" {
		t.Errorf("context = %+v", n.Context)
	}
	if want := filepath.Join(k.sv.Dir, "art", "music-0123456789ABCDEF.png"); n.Art != want {
		t.Errorf("art = %q, want %q", n.Art, want)
	}
	for _, r := range k.script.runs {
		if r.script == musicLook && r.language != "JavaScript" {
			t.Errorf("the look ran as %s", r.language)
		}
		if r.script == musicArt && r.language != "AppleScript" {
			t.Errorf("the artwork ran as %s", r.language)
		}
	}
	if !slices.Contains(s.Can, ipc.JamCanSeek) || slices.Contains(s.Can, ipc.JamCanDevices) {
		t.Errorf("can = %v", s.Can)
	}
	// The same track's artwork isn't written again.
	k.clock.add(2 * time.Second)
	k.do(t, ipc.JamminArgs{})
	arts := 0
	for _, r := range k.script.runs {
		if r.script == musicArt {
			arts++
		}
	}
	if arts != 1 {
		t.Errorf("the artwork was written %d times", arts)
	}
	// A stopped player plays nothing.
	k.sv.music.next = time.Time{}
	k.script.answer = func(scriptRun) (string, error) {
		return `{"running":true,"state":"stopped","volume":70,"shuffle":false,"repeat":"off"}`, nil
	}
	if s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamMusic); s.State != ipc.JamReady || s.Now != nil {
		t.Errorf("stopped: %+v", s)
	}
}

// TestMusicKeepsTheNewest20Artworks: each new track's artwork is a file,
// and only the newest 20 stay.
func TestMusicKeepsTheNewest20Artworks(t *testing.T) {
	k := musicKit(t, "")
	for i := range 23 {
		id := fmt.Sprintf("%016X", i+1)
		file := k.sv.music.artwork(context.Background(), id)
		if file == "" {
			t.Fatalf("no artwork for %s", id)
		}
		old := time.Now().Add(time.Duration(i-30) * time.Minute)
		os.Chtimes(file, old, old)
	}
	ents, _ := os.ReadDir(filepath.Join(k.sv.Dir, "art"))
	if len(ents) != artKept {
		t.Fatalf("%d artworks kept, want %d", len(ents), artKept)
	}
	if _, err := os.Stat(filepath.Join(k.sv.Dir, "art", "music-0000000000000001.png")); err == nil {
		t.Error("the oldest artwork stayed")
	}
	// Data that is no picture is no artwork, and a track ID that isn't
	// one names no file.
	k.script.answer = func(r scriptRun) (string, error) { return "ok", os.WriteFile(r.args[0], []byte("<html>"), 0o600) }
	if file := k.sv.music.artwork(context.Background(), "00000000000000FF"); file != "" {
		t.Errorf("not a picture: %q", file)
	}
	if file := k.sv.music.artwork(context.Background(), "../../etc/x"); file != "" {
		t.Errorf("a path for an ID: %q", file)
	}
}

// TestMusicPermissionRefusedSaysWhereToAllowIt: macOS's -1743, which it
// answers when the person hasn't let Irie control Music, says where to
// allow it.
func TestMusicPermissionRefusedSaysWhereToAllowIt(t *testing.T) {
	k := musicKit(t, "")
	k.script.answer = func(scriptRun) (string, error) {
		return "", errors.New("execution error: Error: Error: Not authorized to send Apple events to Music. (-1743)")
	}
	s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamMusic)
	if s.State != ipc.JamNeeds || !strings.Contains(s.Problem, "Privacy & Security › Automation") {
		t.Fatalf("refused: %+v", s)
	}
}

// TestMusicControlsAndTheirWordsAsArguments: each control sends its word
// and value as arguments to the one script, never inside it; a search's
// words go the same way, whatever they hold; an ID that isn't one is
// refused.
func TestMusicControlsAndTheirWordsAsArguments(t *testing.T) {
	k := musicKit(t, musicPlaying)
	vol, on := 25, true
	for _, a := range []ipc.JamminArgs{
		{Do: ipc.JamPause}, {Do: ipc.JamToggle}, {Do: ipc.JamNext}, {Do: ipc.JamPrevious},
		{Do: ipc.JamSeek, Position: 61500}, {Do: ipc.JamVolume, Volume: &vol}, {Do: ipc.JamShuffle, On: &on},
		{Do: ipc.JamRepeat, Repeat: ipc.JamRepeatAll}, {Do: ipc.JamPlay, URI: "FEDCBA9876543210"},
	} {
		a.Source = ipc.JamMusic
		k.do(t, a)
	}
	want := []string{"pause", "toggle", "next", "previous", "seek 61.500", "volume 25", "shuffle true", "repeat all", "play FEDCBA9876543210"}
	if got := k.script.did(); !slices.Equal(got, want) {
		t.Fatalf("did %q, want %q", got, want)
	}
	if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamMusic, URI: `x"); Application("Finder").delete(`}); err == nil {
		t.Error("an ID that isn't one was played")
	}
	words := `"); doShellScript("rm") //`
	k.script.answer = func(r scriptRun) (string, error) {
		if r.script == musicSearch {
			if len(r.args) != 2 || r.args[0] != words || r.args[1] != "25" {
				t.Errorf("search args = %q", r.args)
			}
			return `[{"name":"Harbour Lights","artist":"Night Owls","album":"Low Tide","duration":245,"id":"0123456789ABCDEF"},{"name":"bad","id":"nope"}]`, nil
		}
		return musicPlaying, nil
	}
	r := k.do(t, ipc.JamminArgs{Do: ipc.JamSearch, Source: ipc.JamMusic, Query: words})
	if len(r.Found) != 1 || r.Found[0].Title != "Harbour Lights" || r.Found[0].By != "Night Owls" || r.Found[0].Duration != 245000 {
		t.Fatalf("found = %+v", r.Found)
	}
	for _, run := range k.script.runs {
		if strings.Contains(run.script, words) {
			t.Errorf("words got into a script: %q", run.script)
		}
	}
	k.script.answer = func(r scriptRun) (string, error) {
		if r.script == musicLists {
			return `[{"name":"Morning Focus","id":"FEDCBA9876543210","smart":false},{"name":"","id":"0000000000000001"}]`, nil
		}
		return musicPlaying, nil
	}
	if r := k.do(t, ipc.JamminArgs{Do: ipc.JamLists, Source: ipc.JamMusic}); len(r.Lists) != 1 || r.Lists[0].URI != "FEDCBA9876543210" {
		t.Fatalf("lists = %+v", r.Lists)
	}
	if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamDevices, Source: ipc.JamMusic}); err == nil {
		t.Error("Apple Music listed devices")
	}
}

// TestMusicScriptsUseTheDictionarysNames: each name the scripts use is in
// the Music app's dictionary, read from its bundle where it is, and in the
// scripts in JavaScript's form of it.
func TestMusicScriptsUseTheDictionarysNames(t *testing.T) {
	scripts := musicLook + musicDo + musicLists + musicSearch
	for _, name := range musicNames {
		if name == "raw data" || name == "artwork" {
			if !strings.Contains(musicArt, name) {
				t.Errorf("the artwork script lacks %q", name)
			}
			continue
		}
		if js := jsName(name); !strings.Contains(scripts, js) {
			t.Errorf("no script uses %q as %s", name, js)
		}
	}
	if runtime.GOOS != "darwin" {
		return
	}
	sdef, err := os.ReadFile(filepath.Join(musicApp, "Contents", "Resources", "com.apple.Music.sdef"))
	if err != nil {
		t.Skipf("no Music app's dictionary here: %v", err)
	}
	for _, name := range musicNames {
		if !regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `"`).Match(sdef) {
			t.Errorf("the Music app's dictionary has no %q", name)
		}
	}
}

// jsName is a dictionary's name in JavaScript for Automation's form:
// words run together, each after the first with a capital, a plural for
// an element class.
func jsName(name string) string {
	parts := strings.Fields(name)
	for i := 1; i < len(parts); i++ {
		r := []rune(parts[i])
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, "")
}

// TestOsascriptTakesTheScriptOnItsInputAndWordsAsArguments: the real
// runner gives osascript the script on standard input and the words after
// "-", run here against a stand-in program that prints what it got.
func TestOsascriptTakesTheScriptOnItsInputAndWordsAsArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	bin := filepath.Join(t.TempDir(), "osascript")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'args:%s|' \"$@\"\nprintf 'in:'\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	out, err := Osascript{Bin: bin}.Run(context.Background(), "JavaScript", "the script", "a b", `"c"`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `args:-l|args:JavaScript|args:-|args:a b|args:"c"|in:the script`; out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}
