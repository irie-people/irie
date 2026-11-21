//go:build !nojammin

package jammin

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// fakePlayer stands in for afplay: each track a pid that lives until the
// test ends it, or a signal does.
type fakePlayer struct {
	mu      sync.Mutex
	next    int
	alive   map[int]bool
	played  []string // the files started, in order
	signals []string // "STOP 1", "CONT 1", "TERM 1"
}

func (p *fakePlayer) Start(file string) (int, int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	if p.alive == nil {
		p.alive = map[int]bool{}
	}
	p.alive[p.next] = true
	p.played = append(p.played, file)
	return p.next, int64(p.next * 10), nil
}

func (p *fakePlayer) Alive(pid int, start int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.alive[pid] && start == int64(pid*10)
}

func (p *fakePlayer) Signal(pid int, sig syscall.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	name := map[syscall.Signal]string{syscall.SIGSTOP: "STOP", syscall.SIGCONT: "CONT", syscall.SIGTERM: "TERM"}[sig]
	p.signals = append(p.signals, name+" "+itoa(pid))
	if sig == syscall.SIGTERM {
		p.alive[pid] = false
	}
	return nil
}

// end ends the track pid by itself, as afplay does at the end of a file.
func (p *fakePlayer) end(pid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.alive[pid] = false
}

// last is the base name of the file started last.
func (p *fakePlayer) last() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.played) == 0 {
		return ""
	}
	return filepath.Base(p.played[len(p.played)-1])
}

func itoa(n int) string { return strconv.Itoa(n) }

// filesKit is a kit with a Music folder of music files, a stand-in for
// afplay, and Spotlight knowing one track.
func filesKit(t *testing.T) (*kit, *fakePlayer, string) {
	k := newKit(t)
	k.spotify.on("GET /v1/me/player", answer{status: 204})
	music := filepath.Join(k.sv.Home, "Music")
	for _, f := range []string{"Low Tide/01 Harbour Lights.mp3", "Low Tide/02 Night Drive.mp3", "Low Tide/cover.jpg", "Singles/Garden Radio.m4a",
		".hidden/x.mp3", "notes.txt", "Old/protected.m4p", "Band.band/take.wav"} {
		p := filepath.Join(music, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	player := &fakePlayer{}
	k.sv.Player = player
	k.sv.Command = func(bin string, args ...string) (string, error) {
		file := args[len(args)-1]
		switch {
		case strings.HasSuffix(bin, "mdls") && strings.HasSuffix(file, "01 Harbour Lights.mp3"):
			return "Harbour Lights\x00(\n    \"Night Owls\",\n    \"The Tide\"\n)\x00Low Tide\x00245.5", nil
		case strings.HasSuffix(bin, "mdls"):
			return "\x00\x00\x00", nil
		case strings.HasSuffix(bin, "afinfo"):
			return "File: x\nestimated duration: 180.25 sec\naudio bytes: 1", nil
		}
		return "", os.ErrNotExist
	}
	t.Cleanup(k.sv.Close)
	return k, player, music
}

func filesNow(t *testing.T, k *kit) *ipc.JamNow {
	t.Helper()
	return stateOf(k.do(t, ipc.JamminArgs{Source: ipc.JamFiles}).Sources, ipc.JamFiles).Now
}

// TestFilesPlayAFolderAndGoOnToTheNextTrack: the folder's music files,
// without hidden ones, other files, protected ones or a project's, by
// folder; a folder plays from its first track, with what Spotlight knows of
// it and the folder's cover; the next plays when it ends, named by its
// file, its length from afinfo; and the list stops at its end.
func TestFilesPlayAFolderAndGoOnToTheNextTrack(t *testing.T) {
	k, player, music := filesKit(t)
	s := stateOf(k.do(t, ipc.JamminArgs{}).Sources, ipc.JamFiles)
	if s.State != ipc.JamReady || s.Folder != "~/Music" || s.Now != nil || slices.Contains(s.Can, ipc.JamCanSeek) || slices.Contains(s.Can, ipc.JamCanVolume) {
		t.Fatalf("files: %+v", s)
	}
	r := k.do(t, ipc.JamminArgs{Do: ipc.JamLists, Source: ipc.JamFiles})
	var lists []string
	for _, it := range r.Lists {
		lists = append(lists, it.Title+":"+itoa(it.Tracks)+":"+it.URI)
	}
	if want := []string{"All of ~/Music:3:files:", "Low Tide:2:files:Low Tide/", "Singles:1:files:Singles/"}; !slices.Equal(lists, want) {
		t.Fatalf("lists = %q, want %q", lists, want)
	}
	k.do(t, ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamFiles, URI: "files:Low Tide/"})
	if player.last() != "01 Harbour Lights.mp3" {
		t.Fatalf("played %v", player.played)
	}
	n := filesNow(t, k)
	if n == nil || !n.Playing || n.Title != "Harbour Lights" || !slices.Equal(n.Artists, []string{"Night Owls"}) || n.Album != "Low Tide" || n.Duration != 245500 ||
		n.Art != filepath.Join(music, "Low Tide", "cover.jpg") || n.URI != "files:Low Tide/01 Harbour Lights.mp3" || n.Context == nil || n.Context.Title != "Low Tide" {
		t.Fatalf("now = %+v", n)
	}
	player.end(1)
	k.sv.files.tick()
	if player.last() != "02 Night Drive.mp3" {
		t.Fatalf("after the first: %v", player.played)
	}
	if n := filesNow(t, k); n == nil || n.Title != "02 Night Drive" || n.Duration != 180250 || n.Album != "Low Tide" {
		t.Fatalf("the second: %+v", n)
	}
	player.end(2)
	k.sv.files.tick()
	if n := filesNow(t, k); n != nil || len(player.played) != 2 {
		t.Fatalf("after the list's end: %+v, played %v", n, player.played)
	}
}

// TestFilesPauseAndPlayOnKeepTheirPlace: a pause stops afplay, play
// continues it, and the position counts only the time it played.
func TestFilesPauseAndPlayOnKeepTheirPlace(t *testing.T) {
	k, player, _ := filesKit(t)
	k.do(t, ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamFiles, URI: "files:Singles/Garden Radio.m4a"})
	k.clock.add(10 * time.Second)
	k.do(t, ipc.JamminArgs{Do: ipc.JamPause, Source: ipc.JamFiles})
	k.clock.add(5 * time.Second)
	if n := filesNow(t, k); n == nil || n.Playing || n.Position != 10000 {
		t.Fatalf("paused: %+v", n)
	}
	// While paused, the watcher doesn't take a stopped afplay for one that
	// ended.
	k.sv.files.tick()
	k.do(t, ipc.JamminArgs{Do: ipc.JamToggle, Source: ipc.JamFiles})
	k.clock.add(2 * time.Second)
	if n := filesNow(t, k); n == nil || !n.Playing || n.Position != 12000 {
		t.Fatalf("playing on: %+v", n)
	}
	if want := []string{"STOP 1", "CONT 1"}; !slices.Equal(player.signals, want) || len(player.played) != 1 {
		t.Fatalf("signals %v, played %v", player.signals, player.played)
	}
}

// TestFilesSkipRepeatAndShuffle: next and previous, previous within three
// seconds going back and after it starting over, repeat all wrapping and
// repeat track playing it again, and shuffle keeping the track playing
// first.
func TestFilesSkipRepeatAndShuffle(t *testing.T) {
	k, player, _ := filesKit(t)
	k.do(t, ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamFiles})
	if player.last() != "01 Harbour Lights.mp3" {
		t.Fatalf("the folder's first: %v", player.played)
	}
	k.do(t, ipc.JamminArgs{Do: ipc.JamNext, Source: ipc.JamFiles})
	if player.last() != "02 Night Drive.mp3" {
		t.Fatalf("next: %v", player.played)
	}
	k.clock.add(time.Second)
	k.do(t, ipc.JamminArgs{Do: ipc.JamPrevious, Source: ipc.JamFiles})
	if player.last() != "01 Harbour Lights.mp3" {
		t.Fatalf("previous at once: %v", player.played)
	}
	k.clock.add(10 * time.Second)
	k.do(t, ipc.JamminArgs{Do: ipc.JamPrevious, Source: ipc.JamFiles})
	if player.last() != "01 Harbour Lights.mp3" || len(player.played) != 4 {
		t.Fatalf("previous later starts over: %v", player.played)
	}
	k.do(t, ipc.JamminArgs{Do: ipc.JamRepeat, Source: ipc.JamFiles, Repeat: ipc.JamRepeatAll})
	k.do(t, ipc.JamminArgs{Do: ipc.JamNext, Source: ipc.JamFiles}) // Night Drive
	k.do(t, ipc.JamminArgs{Do: ipc.JamNext, Source: ipc.JamFiles}) // Garden Radio
	k.do(t, ipc.JamminArgs{Do: ipc.JamNext, Source: ipc.JamFiles}) // wraps
	if player.last() != "01 Harbour Lights.mp3" {
		t.Fatalf("repeat all: %v", player.played)
	}
	k.do(t, ipc.JamminArgs{Do: ipc.JamRepeat, Source: ipc.JamFiles, Repeat: ipc.JamRepeatTrack})
	before := len(player.played)
	player.end(player.next)
	k.sv.files.tick()
	if len(player.played) != before+1 || player.last() != "01 Harbour Lights.mp3" {
		t.Fatalf("repeat track: %v", player.played)
	}
	on := true
	k.do(t, ipc.JamminArgs{Do: ipc.JamShuffle, Source: ipc.JamFiles, On: &on})
	q := k.sv.files.st.Queue
	if len(q) != 3 || q[k.sv.files.st.At] != filepath.Join("Low Tide", "01 Harbour Lights.mp3") {
		t.Fatalf("shuffled: %v at %d", q, k.sv.files.st.At)
	}
	if n := filesNow(t, k); n == nil || !n.Shuffle || n.Repeat != ipc.JamRepeatTrack {
		t.Fatalf("now: %+v", n)
	}
}

// TestFilesPlayOnThroughARestart: a core after this one takes up the
// afplay that plays on, going on to the next when it ends, and one that
// ended while no core watched goes on at once.
func TestFilesPlayOnThroughARestart(t *testing.T) {
	k, player, _ := filesKit(t)
	k.do(t, ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamFiles, URI: "files:Low Tide/"})
	k.sv.Close()
	// The next core: the same state folder, the same afplay still playing.
	next := &Service{Dir: k.sv.Dir, Home: k.sv.Home, Store: k.store, Config: k.sv.Config, Player: player, Command: k.sv.Command, Script: k.script, Running: k.sv.Running, Now: k.clock.now}
	t.Cleanup(next.Close)
	next.Resume()
	if len(player.played) != 1 {
		t.Fatalf("the next core started a track: %v", player.played)
	}
	player.end(1)
	next.files.tick()
	if player.last() != "02 Night Drive.mp3" {
		t.Fatalf("after the restart: %v", player.played)
	}
	next.Close()
	player.end(2) // ends while no core watches: the list is over
	third := &Service{Dir: k.sv.Dir, Home: k.sv.Home, Store: k.store, Config: k.sv.Config, Player: player, Command: k.sv.Command, Script: k.script, Running: k.sv.Running, Now: k.clock.now}
	t.Cleanup(third.Close)
	third.Resume()
	if len(player.played) != 2 || third.files.st.PID != 0 {
		t.Fatalf("a list that ended played on: %v, pid %d", player.played, third.files.st.PID)
	}
}

// TestFilesStayInTheirFolder: an item outside the folder, or not one of
// its music files, is refused; a search finds tracks with their folder to
// play within; and folder keeps the folder in config.toml with the home
// folder as ~, refusing what isn't a folder.
func TestFilesStayInTheirFolder(t *testing.T) {
	k, player, _ := filesKit(t)
	for _, uri := range []string{"files:../secret.mp3", "files:/etc/hosts", "files:notes.txt", "files:Old/protected.m4p", "spotify:track:t1"} {
		if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamFiles, URI: uri}); err == nil {
			t.Errorf("%s was played", uri)
		}
	}
	if len(player.played) != 0 {
		t.Fatalf("played %v", player.played)
	}
	r := k.do(t, ipc.JamminArgs{Do: ipc.JamSearch, Source: ipc.JamFiles, Query: "harbour LIGHTS"})
	if len(r.Found) != 1 || r.Found[0].URI != "files:Low Tide/01 Harbour Lights.mp3" || r.Found[0].Context != "files:Low Tide/" {
		t.Fatalf("found %+v", r.Found)
	}
	k.do(t, ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamFiles, URI: r.Found[0].URI, Context: r.Found[0].Context})
	other := filepath.Join(k.sv.Home, "Tapes")
	os.MkdirAll(other, 0o755)
	r = k.do(t, ipc.JamminArgs{Do: ipc.JamFolder, Folder: other})
	if k.cfg.Folder != "~/Tapes" || stateOf(r.Sources, ipc.JamFiles).Folder != "~/Tapes" {
		t.Fatalf("folder kept as %q, %+v", k.cfg.Folder, k.sets)
	}
	if !slices.Contains(player.signals, "TERM 1") {
		t.Errorf("the old folder's track played on: %v", player.signals)
	}
	if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamFolder, Folder: filepath.Join(other, "nope")}); err == nil {
		t.Error("a folder that isn't there was kept")
	}
	if _, err := k.sv.Do(context.Background(), ipc.JamminArgs{Do: ipc.JamFolder, Folder: "relative/path"}); err == nil {
		t.Error("a relative folder was kept")
	}
}
