//go:build !nojammin

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
)

// irie jammin: the jammin plugin's music (docs/proposals/jammin.md), in a
// terminal. The core does the work, so what plays shows in the Mac app's
// Jammin tab too. go build -tags nojammin leaves this file out, and
// jammin_off.go stands in.

// jamminUsage is the jammin plugin's lines in the usage.
const jamminUsage = `  ` + brand.Slug + ` jammin [play|pause|next|previous]
                          your music: what plays on Spotify and in the Music
                          app, and its controls (` + brand.Slug + ` jammin help)
`

const jamminHelp = `usage:
  ` + brand.Slug + ` jammin                     what plays on each source
  ` + brand.Slug + ` jammin play [URI]          play, or play an item: a Spotify URI, or a
                              playlist's or track's ID in the Music app
  ` + brand.Slug + ` jammin pause|toggle|next|previous
  ` + brand.Slug + ` jammin seek TIME           from that point of the track: 90 or 1:30
  ` + brand.Slug + ` jammin volume N            0 to 100
  ` + brand.Slug + ` jammin shuffle on|off
  ` + brand.Slug + ` jammin repeat off|track|all
  ` + brand.Slug + ` jammin lists               your playlists, with what play takes for each
  ` + brand.Slug + ` jammin search WORDS        tracks, albums and playlists
  ` + brand.Slug + ` jammin devices             where Spotify can play
  ` + brand.Slug + ` jammin folder PATH         the folder of your music files, ~/Music unless
                              you pick another
  ` + brand.Slug + ` jammin move DEVICE         move what plays there, by its name or ID
  ` + brand.Slug + ` jammin link spotify [CLIENT_ID]
                              sign in to Spotify through your Spotify app
  ` + brand.Slug + ` jammin unlink spotify [--all]
                              sign out; --all forgets the Client ID too
--source spotify|music|files picks the source; without it, the one playing.
--json prints the core's answer as JSON.
Spotify comes through an app of yours: make one at
https://developer.spotify.com/dashboard with the redirect URI
http://127.0.0.1/jammin/spotify, and Web API. Spotify lets apps in
development mode work only for Premium accounts. Without one, Jammin plays
the Spotify app on this Mac while it is open.
`

func init() {
	pluginCmds["jammin"] = pluginCmd{run: func(args []string) error {
		cfg, _, err := load()
		if err != nil {
			return err
		}
		if why := plugins.Why(cfg, plugins.JamminSpotify); why != "" {
			return errors.New(why)
		}
		return jamminCmd(args, jamminIO{in: os.Stdin, out: os.Stdout, err: os.Stderr, call: jamminCall, open: openURL, tty: isTerminal(os.Stdin)})
	}}
}

// jamminIO is what irie jammin talks to: the person's terminal, the core,
// and their browser. Tests stand in for each.
type jamminIO struct {
	in       io.Reader
	out, err io.Writer
	call     func(ipc.JamminArgs, time.Duration) (ipc.JamminResult, error)
	open     func(url string) error
	tty      bool
	// poll is how often a sign-in looks whether it is done; 0 = a second.
	poll time.Duration
}

// jamminCall asks the core's jammin op.
func jamminCall(a ipc.JamminArgs, wait time.Duration) (ipc.JamminResult, error) {
	_, p, err := load()
	if err != nil {
		return ipc.JamminResult{}, err
	}
	var r ipc.JamminResult
	err = ipc.Call(p.Socket(), wait, ipc.OpJammin, a, &r)
	return r, err
}

// openURL opens a page in the person's own browser.
func openURL(u string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "/usr/bin/open"
	}
	return exec.Command(opener, u).Start()
}

// jamminWait is how long one call may take: the Music app's first answer
// waits while macOS asks the person whether Irie may control it.
const jamminWait = 3 * time.Minute

func jamminCmd(args []string, j jamminIO) error {
	var a ipc.JamminArgs
	asJSON := false
	var rest []string
	for i := 0; i < len(args); i++ {
		switch w := args[i]; {
		case w == "--json":
			asJSON = true
		case w == "--all":
			a.All = true
		case w == "--source" && i+1 < len(args):
			i++
			a.Source = args[i]
		case strings.HasPrefix(w, "--source="):
			a.Source = strings.TrimPrefix(w, "--source=")
		case w == "-h" || w == "--help":
			rest = []string{"help"}
			i = len(args)
		default:
			rest = append(rest, w)
		}
	}
	sub := ""
	if len(rest) > 0 {
		sub, rest = rest[0], rest[1:]
	}
	one := func(what string) (string, error) {
		if len(rest) != 1 {
			return "", fmt.Errorf("usage: %s jammin %s", brand.Slug, what)
		}
		return rest[0], nil
	}
	switch sub {
	case "help":
		fmt.Fprint(j.out, jamminHelp)
		return nil
	case "", "status":
	case "play":
		a.Do = ipc.JamPlay
		if len(rest) > 1 {
			return fmt.Errorf("usage: %s jammin play [URI]", brand.Slug)
		}
		if len(rest) == 1 {
			a.URI = rest[0]
		}
	case "pause", "toggle", "next", "previous":
		a.Do = map[string]string{"pause": ipc.JamPause, "toggle": ipc.JamToggle, "next": ipc.JamNext, "previous": ipc.JamPrevious}[sub]
		if len(rest) > 0 {
			return fmt.Errorf("usage: %s jammin %s", brand.Slug, sub)
		}
	case "seek":
		w, err := one("seek TIME")
		if err != nil {
			return err
		}
		ms, ok := clockTime(w)
		if !ok {
			return fmt.Errorf("seek takes a time from the track's start, such as 90 or 1:30, not %q", w)
		}
		a.Do, a.Position = ipc.JamSeek, ms
	case "volume":
		w, err := one("volume N")
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(w)
		if err != nil {
			return fmt.Errorf("the volume is a number from 0 to 100, not %q", w)
		}
		a.Do, a.Volume = ipc.JamVolume, &n
	case "shuffle":
		w, err := one("shuffle on|off")
		if err != nil {
			return err
		}
		on := w == "on"
		if !on && w != "off" {
			return fmt.Errorf("shuffle on or off, not %q", w)
		}
		a.Do, a.On = ipc.JamShuffle, &on
	case "repeat":
		w, err := one("repeat off|track|all")
		if err != nil {
			return err
		}
		a.Do, a.Repeat = ipc.JamRepeat, w
	case "lists":
		a.Do = ipc.JamLists
	case "search":
		if len(rest) == 0 {
			return fmt.Errorf("usage: %s jammin search WORDS", brand.Slug)
		}
		a.Do, a.Query = ipc.JamSearch, strings.Join(rest, " ")
	case "devices":
		a.Do = ipc.JamDevices
	case "folder":
		w, err := one("folder PATH")
		if err != nil {
			return err
		}
		if !filepath.IsAbs(w) && w != "~" && !strings.HasPrefix(w, "~/") {
			if w, err = filepath.Abs(w); err != nil {
				return err
			}
		}
		a.Do, a.Folder = ipc.JamFolder, w
	case "move":
		if len(rest) == 0 {
			return fmt.Errorf("usage: %s jammin move DEVICE", brand.Slug)
		}
		id, err := jamminDevice(j, a.Source, strings.Join(rest, " "))
		if err != nil {
			return err
		}
		a.Do, a.Device = ipc.JamMove, id
	case "link":
		if len(rest) == 0 || rest[0] != "spotify" || len(rest) > 2 {
			return fmt.Errorf("usage: %s jammin link spotify [CLIENT_ID]", brand.Slug)
		}
		if len(rest) == 2 {
			a.ClientID = rest[1]
		}
		return jamminLink(j, a.ClientID, asJSON)
	case "unlink":
		if len(rest) != 1 || rest[0] != "spotify" {
			return fmt.Errorf("usage: %s jammin unlink spotify [--all]", brand.Slug)
		}
		a.Do, a.Source = ipc.JamSignOut, ipc.JamSpotify
	default:
		return fmt.Errorf("%s jammin has no %q: %s jammin help lists what it takes", brand.Slug, sub, brand.Slug)
	}
	r, err := j.call(a, jamminWait)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(j.out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	switch a.Do {
	case ipc.JamLists, ipc.JamSearch:
		items := r.Lists
		if a.Do == ipc.JamSearch {
			items = r.Found
		}
		if len(items) == 0 {
			fmt.Fprintln(j.out, r.Text)
		}
		for _, it := range items {
			line := it.Kind + "  " + it.Title
			if it.By != "" {
				line += " · " + it.By
			}
			fmt.Fprintf(j.out, "%s\n    %s\n", line, it.URI)
		}
	case ipc.JamDevices:
		if len(r.Devices) == 0 {
			fmt.Fprintln(j.out, r.Text)
		}
		for _, d := range r.Devices {
			mark := " "
			if d.Active {
				mark = "▶"
			}
			fmt.Fprintf(j.out, "%s %s (%s)  %s\n", mark, d.Name, d.Kind, d.ID)
		}
	default:
		fmt.Fprintln(j.out, r.Text)
	}
	return nil
}

// clockTime reads a time from a track's start, seconds or M:SS, as
// milliseconds.
func clockTime(w string) (int, bool) {
	m, s, ok := strings.Cut(w, ":")
	if !ok {
		m, s = "0", w
	}
	mins, err1 := strconv.Atoi(m)
	secs, err2 := strconv.ParseFloat(s, 64)
	if err1 != nil || err2 != nil || mins < 0 || secs < 0 || (ok && secs >= 60) {
		return 0, false
	}
	return mins*60000 + int(secs*1000), true
}

// jamminDevice is the ID of the device that name names, by its ID or its
// name, as Spotify lists them now.
func jamminDevice(j jamminIO, source, name string) (string, error) {
	r, err := j.call(ipc.JamminArgs{Do: ipc.JamDevices, Source: source}, jamminWait)
	if err != nil {
		return "", err
	}
	for _, d := range r.Devices {
		if d.ID == name {
			return d.ID, nil
		}
	}
	var names []string
	for _, d := range r.Devices {
		if strings.EqualFold(d.Name, name) {
			return d.ID, nil
		}
		names = append(names, d.Name)
	}
	if len(names) == 0 {
		return "", errors.New("no device of yours is open: open Spotify on one")
	}
	return "", fmt.Errorf("no device is called %q: there are %s", name, strings.Join(names, ", "))
}

// jamminLink signs in to Spotify: it takes the Client ID, asks the core
// to listen for Spotify's answer, offers to open the sign-in page in the
// person's browser, and waits until it is done.
func jamminLink(j jamminIO, id string, asJSON bool) error {
	// One reader for both answers, so the first doesn't take the second's.
	in := bufio.NewReader(j.in)
	if id == "" {
		r, err := j.call(ipc.JamminArgs{}, jamminWait)
		if err != nil {
			return err
		}
		if st := stateOfJam(r.Sources, ipc.JamSpotify); st.State == ipc.JamNeeds && strings.Contains(st.Problem, "No Spotify app yet") {
			if !j.tty {
				return errors.New(st.Problem)
			}
			fmt.Fprint(j.err, `Make your Spotify app first, which takes a minute:
  1. Open https://developer.spotify.com/dashboard and choose Create app.
  2. Any name and description, such as Jammin and "my jam box".
  3. Redirect URI: http://127.0.0.1/jammin/spotify, exactly.
  4. Which API: Web API. Agree and save.
  5. Copy the Client ID from its settings.
Spotify lets apps in development mode work only for Premium accounts.
Client ID: `)
			line, err := in.ReadString('\n')
			if err != nil && strings.TrimSpace(line) == "" {
				return errors.New("no Client ID given")
			}
			id = strings.TrimSpace(line)
		}
	}
	r, err := j.call(ipc.JamminArgs{Do: ipc.JamSignIn, Source: ipc.JamSpotify, ClientID: id}, jamminWait)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(j.out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	if r.Link == nil {
		return errors.New("the core started no sign-in")
	}
	fmt.Fprintf(j.err, "Sign in to Spotify, and agree, at:\n  %s\n", r.Link.URL)
	if j.tty && j.open != nil {
		fmt.Fprint(j.err, "Press Return to open it in your browser, or open it yourself. ")
		if _, err := in.ReadString('\n'); err == nil {
			if err := j.open(r.Link.URL); err != nil {
				fmt.Fprintln(j.err, "Couldn't open your browser: open the address yourself.")
			}
		}
	}
	fmt.Fprintln(j.err, "Waiting for Spotify… (Ctrl-C cancels)")
	poll := j.poll
	if poll == 0 {
		poll = time.Second
	}
	// Ctrl-C cancels the sign-in, so the core stops listening at once.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	until := time.Unix(r.Link.Until, 0).Add(5 * time.Second)
	for time.Now().Before(until) {
		select {
		case <-stop:
			_, _ = j.call(ipc.JamminArgs{Do: ipc.JamCancel, Source: ipc.JamSpotify}, 10*time.Second)
			return errors.New("canceled: not signed in to Spotify")
		case <-time.After(poll):
		}
		r, err := j.call(ipc.JamminArgs{}, jamminWait)
		if err != nil {
			return err
		}
		switch st := stateOfJam(r.Sources, ipc.JamSpotify); st.State {
		case ipc.JamLinking:
			continue
		case ipc.JamReady:
			who := ""
			if st.Account != "" {
				who = " as " + st.Account
			}
			fmt.Fprintf(j.out, "Signed in to Spotify%s. The Jammin tab, :jammin or %s jammin plays it.\n", who, brand.Slug)
			return nil
		default:
			return errors.New(st.Problem)
		}
	}
	return errors.New("the sign-in took too long: " + brand.Slug + " jammin link spotify again")
}

// stateOfJam is the named source in all.
func stateOfJam(all []ipc.JamSource, name string) ipc.JamSource {
	for _, st := range all {
		if st.Name == name {
			return st
		}
	}
	return ipc.JamSource{Name: name}
}
