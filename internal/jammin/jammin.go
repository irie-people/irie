//go:build !nojammin

// Package jammin is the Jammin plugin's music (docs/proposals/jammin.md):
// a source per service, read and controlled by the core for every client
// to draw. Spotify comes through an app of the person's own, over its Web
// API, and Apple Music through the Music app on this Mac, over its
// scripting dictionary. One source plays at a time: starting one pauses
// another that would play over it, and nothing mixes two sources' music.
//
// What a source says stays in memory, but for the artwork of the track the
// Music app plays, which it gives as data rather than an address. The
// Spotify token lives in the login Keychain.
package jammin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// Source is one place music comes from: a service's account, or an app on
// this computer.
type Source interface {
	Name() string
	Title() string
	// Look says how it stands and what plays. It sends nothing to an app
	// that isn't running, and asks no service it isn't signed in to.
	Look(ctx context.Context) ipc.JamSource
	// Do is one control: play, pause, toggle, next, previous, seek,
	// volume, shuffle, repeat or move.
	Do(ctx context.Context, a ipc.JamminArgs) error
	Lists(ctx context.Context) ([]ipc.JamItem, error)
	Search(ctx context.Context, words string) ([]ipc.JamItem, error)
	Devices(ctx context.Context) ([]ipc.JamDevice, error)
}

// Service is the plugin's music: every source, made at the first call.
type Service struct {
	// Dir is the plugin's folder in Irie's state: the Music app's
	// artwork lives in Dir/art.
	Dir string
	// Store keeps the Spotify token.
	Store Store
	// Config is the plugin's settings now; Configure writes settings to
	// config.toml, keeping its comments.
	Config    func() config.Jammin
	Configure func([]ipc.ConfigSet) error
	// Script runs the scripts that speak to the Music app; nil = osascript.
	Script Runner
	// Running says whether an app runs on this computer, by the path of
	// its program; nil = from the process table, which asks the app
	// nothing.
	Running func(program string) bool
	// Launch starts an app on this computer, by its bundle ID, hidden and
	// in the background, so no window of it comes forward; nil = open -g
	// -j. Tests stand in for it.
	Launch func(bundle string) error
	// Player plays the files source's tracks; nil = afplay.
	Player Player
	// Command runs mdls and afinfo for what a track is; nil = the
	// programs themselves.
	Command func(bin string, args ...string) (string, error)
	// Home is the home folder, for ~ in jammin.folder; "" = the user's.
	Home string
	// HTTP is the client for Spotify; nil = one with a timeout.
	HTTP *http.Client
	// API and Accounts are Spotify's addresses; "" = Spotify's own. Tests
	// point them at stand-ins.
	API, Accounts string
	// Here is this computer's name, as a Spotify app on it names its
	// device; nil = the Mac's computer name.
	Here func() string
	// Now is the clock; nil = time.Now.
	Now func() time.Time
	// Wait waits d, or until ctx ends, whichever is first; nil = a timer.
	// Tests make it instant.
	Wait func(ctx context.Context, d time.Duration) error

	once    sync.Once
	sources []Source
	spotify *spotify
	music   *music
	files   *files
	here    string
	quit    chan struct{}
	closed  sync.Once
}

// init makes the sources, once.
func (s *Service) init() {
	s.once.Do(func() {
		s.spotify = &spotify{sv: s}
		s.music = &music{sv: s}
		s.files = &files{sv: s}
		s.sources = []Source{s.spotify, s.music, s.files}
		s.quit = make(chan struct{})
	})
}

// quitting is closed when the Service closes.
func (s *Service) quitting() chan struct{} {
	s.init()
	return s.quit
}

// Close stops the Service's own goroutines; what plays plays on.
func (s *Service) Close() {
	s.init()
	s.closed.Do(func() { close(s.quit) })
}

// Resume watches the files source's track that played on while the core
// restarted, so the next starts when it ends. It asks nothing of any app
// or service.
func (s *Service) Resume() {
	s.init()
	s.files.Resume()
}

func (s *Service) home() string {
	if s.Home != "" {
		return s.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) wait(ctx context.Context, d time.Duration) error {
	if s.Wait != nil {
		return s.Wait(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) config() config.Jammin {
	if s.Config != nil {
		return s.Config()
	}
	return config.Jammin{}
}

// hereName is this computer's name, as the Spotify app names its device
// after it: the Mac's computer name, else the host's.
func (s *Service) hereName() string {
	if s.Here != nil {
		return s.Here()
	}
	if s.here == "" {
		if out, err := exec.Command("/usr/sbin/scutil", "--get", "ComputerName").Output(); err == nil {
			s.here = strings.TrimSpace(string(out))
		}
		if s.here == "" {
			s.here, _ = os.Hostname()
		}
	}
	return s.here
}

// source is the source by name.
func (s *Service) source(name string) (Source, bool) {
	s.init()
	for _, src := range s.sources {
		if src.Name() == name {
			return src, true
		}
	}
	return nil, false
}

// Do answers the jammin op. The core has already refused it to agents.
func (s *Service) Do(ctx context.Context, a ipc.JamminArgs) (ipc.JamminResult, error) {
	s.init()
	if a.Source != "" {
		if _, ok := s.source(a.Source); !ok {
			return ipc.JamminResult{}, fmt.Errorf("there is no source called %q: the sources are %s", a.Source, strings.Join(config.JamminSources, " and "))
		}
	}
	var r ipc.JamminResult
	switch a.Do {
	case ipc.JamLook:
		r.Sources = s.look(ctx)
		r.Source = s.pick(r.Sources, a.Source)
		r.Text = Text(r.Sources)
		return r, nil
	case ipc.JamSignIn:
		if a.Source != "" && a.Source != ipc.JamSpotify {
			return r, errors.New("only Spotify has an account to sign in to: Apple Music is the Music app on this Mac")
		}
		link, err := s.spotify.signIn(ctx, a.ClientID)
		if err != nil {
			return r, err
		}
		r.Link = &link
		r.Source = ipc.JamSpotify
		r.Sources = s.look(ctx)
		r.Text = "Sign in to Spotify at " + link.URL + " within 10 minutes. Your Spotify app must list the redirect URI " + link.Redirect + "."
		return r, nil
	case ipc.JamFolder:
		if err := s.files.setFolder(a.Folder); err != nil {
			return r, err
		}
		r.Source = ipc.JamFiles
		r.Sources = s.look(ctx)
		r.Text = "Your music files are " + stateOf(r.Sources, ipc.JamFiles).Folder + " now."
		return r, nil
	case ipc.JamCancel:
		if a.Source != "" && a.Source != ipc.JamSpotify {
			return r, errors.New("only Spotify has a sign-in to cancel")
		}
		r.Text = "No sign-in was under way."
		if s.spotify.cancelSignIn() {
			r.Text = "Canceled the sign-in to Spotify."
		}
		r.Source = ipc.JamSpotify
		r.Sources = s.look(ctx)
		return r, nil
	case ipc.JamSignOut:
		if a.Source != "" && a.Source != ipc.JamSpotify {
			return r, errors.New("only Spotify has an account to sign out of")
		}
		if err := s.spotify.signOut(a.All); err != nil {
			return r, err
		}
		r.Source = ipc.JamSpotify
		r.Sources = s.look(ctx)
		r.Text = "Signed out of Spotify: its token is gone from the Keychain."
		if a.All {
			r.Text += " The Client ID is gone from config.toml too."
		} else {
			r.Text += " The Client ID stays in config.toml, so Link Spotify signs in again with the same app."
		}
		return r, nil
	}

	all := s.look(ctx)
	name := s.pick(all, a.Source)
	if name == "" {
		return r, errors.New("no source is ready to play: link Spotify, or open the Music app")
	}
	src, _ := s.source(name)
	st := stateOf(all, name)
	r.Source = name
	switch a.Do {
	case ipc.JamLists, ipc.JamSearch, ipc.JamDevices:
		if err := can(st, a.Do); err != nil {
			return r, err
		}
		var err error
		switch a.Do {
		case ipc.JamLists:
			r.Lists, err = src.Lists(ctx)
		case ipc.JamSearch:
			words := strings.TrimSpace(a.Query)
			if words == "" {
				return r, errors.New("search for what? Give it some words")
			}
			r.Found, err = src.Search(ctx, words)
		case ipc.JamDevices:
			r.Devices, err = src.Devices(ctx)
		}
		if err != nil {
			return r, err
		}
		r.Sources = all
		r.Text = itemsText(st.Title, a.Do, r)
		return r, nil
	case ipc.JamPlay, ipc.JamPause, ipc.JamToggle, ipc.JamNext, ipc.JamPrevious, ipc.JamSeek, ipc.JamVolume, ipc.JamShuffle, ipc.JamRepeat, ipc.JamMove:
	default:
		return r, fmt.Errorf("the jammin op takes play, pause, toggle, next, previous, seek, volume, shuffle, repeat, lists, search, devices, move, folder, link, cancel or unlink, or nothing for a look, not %q", a.Do)
	}
	if err := checkControl(a); err != nil {
		return r, err
	}
	if err := can(st, a.Do); err != nil {
		return r, err
	}
	if starts(a, st) {
		s.quiet(ctx, name, all)
	}
	if err := src.Do(ctx, a); err != nil {
		return r, err
	}
	r.Sources = s.look(ctx)
	r.Text = didText(a, stateOf(r.Sources, name))
	return r, nil
}

// look reads every source at once.
func (s *Service) look(ctx context.Context) []ipc.JamSource {
	out := make([]ipc.JamSource, len(s.sources))
	var wg sync.WaitGroup
	for i, src := range s.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = src.Look(ctx)
		}()
	}
	wg.Wait()
	return out
}

// pick is the source a call is about: the one asked for, else the one
// playing, else jammin.source when it is ready, else the first ready.
func (s *Service) pick(all []ipc.JamSource, asked string) string {
	if asked != "" {
		return asked
	}
	for _, st := range all {
		if st.Now != nil && st.Now.Playing {
			return st.Name
		}
	}
	if want := s.config().Source; want != "" && stateOf(all, want).State == ipc.JamReady {
		return want
	}
	for _, st := range all {
		if st.State == ipc.JamReady {
			return st.Name
		}
	}
	return ""
}

// stateOf is the named source in all.
func stateOf(all []ipc.JamSource, name string) ipc.JamSource {
	for _, st := range all {
		if st.Name == name {
			return st
		}
	}
	return ipc.JamSource{Name: name}
}

// needs says what a call needs of a source: what it must be able to do.
var needs = map[string]string{
	ipc.JamPlay:     ipc.JamCanPlay,
	ipc.JamPause:    ipc.JamCanPlay,
	ipc.JamToggle:   ipc.JamCanPlay,
	ipc.JamNext:     ipc.JamCanSkip,
	ipc.JamPrevious: ipc.JamCanSkip,
	ipc.JamSeek:     ipc.JamCanSeek,
	ipc.JamVolume:   ipc.JamCanVolume,
	ipc.JamShuffle:  ipc.JamCanShuffle,
	ipc.JamRepeat:   ipc.JamCanRepeat,
	ipc.JamLists:    ipc.JamCanLists,
	ipc.JamSearch:   ipc.JamCanSearch,
	ipc.JamDevices:  ipc.JamCanDevices,
	ipc.JamMove:     ipc.JamCanDevices,
}

// can says why st can't do what a call asks, or nil when it can.
func can(st ipc.JamSource, do string) error {
	if st.State != ipc.JamReady && !(st.Name == ipc.JamMusic && do == ipc.JamPlay) {
		if st.Problem != "" {
			return errors.New(st.Problem)
		}
		return fmt.Errorf("%s isn't ready", st.Title)
	}
	if need := needs[do]; need != "" && !slices.Contains(st.Can, need) {
		return fmt.Errorf("%s can't %s now", st.Title, words[do])
	}
	return nil
}

// words are the calls in words, for a sentence that says one can't be done.
var words = map[string]string{
	ipc.JamPlay: "play", ipc.JamPause: "pause", ipc.JamToggle: "play or pause",
	ipc.JamNext: "skip to the next track", ipc.JamPrevious: "go back a track",
	ipc.JamSeek: "seek", ipc.JamVolume: "change the volume", ipc.JamShuffle: "shuffle",
	ipc.JamRepeat: "repeat", ipc.JamLists: "list your playlists", ipc.JamSearch: "search",
	ipc.JamDevices: "list your devices", ipc.JamMove: "move what plays",
}

// checkControl says what is wrong with a control's arguments.
func checkControl(a ipc.JamminArgs) error {
	switch a.Do {
	case ipc.JamSeek:
		if a.Position < 0 {
			return errors.New("seek to a point from the track's start: 0 or more milliseconds")
		}
	case ipc.JamVolume:
		if a.Volume == nil || *a.Volume < 0 || *a.Volume > 100 {
			return errors.New("the volume goes from 0 to 100")
		}
	case ipc.JamShuffle:
		if a.On == nil {
			return errors.New("shuffle on or off?")
		}
	case ipc.JamRepeat:
		switch a.Repeat {
		case ipc.JamRepeatOff, ipc.JamRepeatTrack, ipc.JamRepeatAll:
		default:
			return fmt.Errorf("repeat takes off, track or all, not %q", a.Repeat)
		}
	case ipc.JamMove:
		if strings.TrimSpace(a.Device) == "" {
			return errors.New("move to which device? Devices lists them")
		}
	}
	return nil
}

// starts says whether a call starts st playing: a play, or a toggle while
// it is paused.
func starts(a ipc.JamminArgs, st ipc.JamSource) bool {
	switch a.Do {
	case ipc.JamPlay:
		return true
	case ipc.JamToggle:
		return st.Now == nil || !st.Now.Playing
	}
	return false
}

// quiet pauses each other source that would play over the one starting,
// as its last look found them, so two sources never play at once. Spotify's
// Developer Policy forbids playing its content over other audio, and two
// tracks at once are no jam.
func (s *Service) quiet(ctx context.Context, starting string, all []ipc.JamSource) {
	for _, st := range all {
		if st.Name == starting || !overlaps(st) {
			continue
		}
		if src, ok := s.source(st.Name); ok {
			// A pause that fails leaves the other source playing; the one
			// starting starts all the same, as the person asked.
			_ = src.Do(ctx, ipc.JamminArgs{Do: ipc.JamPause, Source: st.Name})
		}
	}
}

// overlaps says whether another source, as its last look found it, would
// play over one starting on this computer: it plays here. A source that
// names no device, as the Music app's never does, plays here; Spotify
// plays here when its device is this computer, so a phone in another room
// plays on.
func overlaps(other ipc.JamSource) bool {
	n := other.Now
	return n != nil && n.Playing && (n.Device == nil || n.Device.Here)
}

// Last is how each source stood at its last look, with no new look: for
// the plugin's parts, which ask no source anything. A source never looked
// at is left out.
func (s *Service) Last() []ipc.JamSource {
	s.init()
	var out []ipc.JamSource
	s.spotify.mu.Lock()
	if st := s.spotify.last; st.State != "" {
		out = append(out, st)
	}
	s.spotify.mu.Unlock()
	s.music.mu.Lock()
	if st := s.music.last; st.State != "" {
		out = append(out, st)
	}
	s.music.mu.Unlock()
	return out
}
