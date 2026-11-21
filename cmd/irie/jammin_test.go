//go:build !nojammin

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// fakeJam stands in for the core's jammin op: it records each call and
// answers with answer.
type fakeJam struct {
	calls  []ipc.JamminArgs
	answer func(ipc.JamminArgs) (ipc.JamminResult, error)
}

func (f *fakeJam) call(a ipc.JamminArgs, _ time.Duration) (ipc.JamminResult, error) {
	f.calls = append(f.calls, a)
	if f.answer != nil {
		return f.answer(a)
	}
	return ipc.JamminResult{Text: "ok"}, nil
}

func jamRun(t *testing.T, f *fakeJam, in string, tty bool, args ...string) (string, string, []string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	var opened []string
	err := jamminCmd(args, jamminIO{in: strings.NewReader(in), out: &out, err: &errb, call: f.call, tty: tty, poll: time.Millisecond,
		open: func(u string) error { opened = append(opened, u); return nil }})
	return out.String(), errb.String(), opened, err
}

// TestIrieJamminSendsEachControl: each word becomes its call, with its
// value read and checked first.
func TestIrieJamminSendsEachControl(t *testing.T) {
	for _, c := range []struct {
		args []string
		want ipc.JamminArgs
	}{
		{nil, ipc.JamminArgs{}},
		{[]string{"pause"}, ipc.JamminArgs{Do: ipc.JamPause}},
		{[]string{"--source", "music", "next"}, ipc.JamminArgs{Do: ipc.JamNext, Source: ipc.JamMusic}},
		{[]string{"play", "spotify:playlist:p1", "--source=spotify"}, ipc.JamminArgs{Do: ipc.JamPlay, URI: "spotify:playlist:p1", Source: ipc.JamSpotify}},
		{[]string{"seek", "1:30"}, ipc.JamminArgs{Do: ipc.JamSeek, Position: 90000}},
		{[]string{"seek", "45.5"}, ipc.JamminArgs{Do: ipc.JamSeek, Position: 45500}},
		{[]string{"repeat", "all"}, ipc.JamminArgs{Do: ipc.JamRepeat, Repeat: "all"}},
		{[]string{"search", "harbour", "lights"}, ipc.JamminArgs{Do: ipc.JamSearch, Query: "harbour lights"}},
		{[]string{"unlink", "spotify", "--all"}, ipc.JamminArgs{Do: ipc.JamSignOut, Source: ipc.JamSpotify, All: true}},
		{[]string{"--source", "files", "play"}, ipc.JamminArgs{Do: ipc.JamPlay, Source: ipc.JamFiles}},
	} {
		f := &fakeJam{}
		if _, _, _, err := jamRun(t, f, "", false, c.args...); err != nil {
			t.Errorf("%v: %v", c.args, err)
			continue
		}
		if len(f.calls) != 1 || f.calls[0].Do != c.want.Do || f.calls[0].Source != c.want.Source || f.calls[0].URI != c.want.URI ||
			f.calls[0].Position != c.want.Position || f.calls[0].Repeat != c.want.Repeat || f.calls[0].Query != c.want.Query || f.calls[0].All != c.want.All {
			t.Errorf("%v: called %+v, want %+v", c.args, f.calls, c.want)
		}
	}
	f := &fakeJam{}
	if _, _, _, err := jamRun(t, f, "", false, "volume", "11"); err != nil || f.calls[0].Volume == nil || *f.calls[0].Volume != 11 {
		t.Errorf("volume: %+v, %v", f.calls, err)
	}
	if _, _, _, err := jamRun(t, f, "", false, "shuffle", "on"); err != nil || f.calls[1].On == nil || !*f.calls[1].On {
		t.Errorf("shuffle: %+v, %v", f.calls, err)
	}
	if _, _, _, err := jamRun(t, f, "", false, "folder", "~/Tapes"); err != nil || f.calls[2].Do != ipc.JamFolder || f.calls[2].Folder != "~/Tapes" {
		t.Errorf("folder: %+v, %v", f.calls, err)
	}
	if _, _, _, err := jamRun(t, f, "", false, "folder", "Tapes"); err != nil || !filepath.IsAbs(f.calls[3].Folder) {
		t.Errorf("a relative folder: %+v, %v", f.calls, err)
	}
	for _, bad := range [][]string{{"seek", "1:75"}, {"volume", "loud"}, {"shuffle", "maybe"}, {"dance"}, {"link", "tidal"}, {"pause", "now"}} {
		if _, _, _, err := jamRun(t, &fakeJam{}, "", false, bad...); err == nil {
			t.Errorf("%v was taken", bad)
		}
	}
}

// TestIrieJamminMovesByNameAndListsWithURIs: move finds a device by its
// name, and lists print what play takes for each.
func TestIrieJamminMovesByNameAndListsWithURIs(t *testing.T) {
	f := &fakeJam{answer: func(a ipc.JamminArgs) (ipc.JamminResult, error) {
		switch a.Do {
		case ipc.JamDevices:
			return ipc.JamminResult{Devices: []ipc.JamDevice{{ID: "d1", Name: "Kitchen"}, {ID: "d2", Name: "Alice's Phone"}}}, nil
		case ipc.JamLists:
			return ipc.JamminResult{Lists: []ipc.JamItem{{Kind: "playlist", Title: "Morning Focus", By: "Alice", URI: "spotify:playlist:p1"}}}, nil
		}
		return ipc.JamminResult{Text: "moved"}, nil
	}}
	if _, _, _, err := jamRun(t, f, "", false, "move", "kitchen"); err != nil || f.calls[1].Device != "d1" {
		t.Fatalf("move: %+v, %v", f.calls, err)
	}
	if _, _, _, err := jamRun(t, f, "", false, "move", "Garage"); err == nil || !strings.Contains(err.Error(), "Kitchen, Alice's Phone") {
		t.Errorf("move to none: %v", err)
	}
	out, _, _, err := jamRun(t, f, "", false, "lists")
	if err != nil || out != "playlist  Morning Focus · Alice\n    spotify:playlist:p1\n" {
		t.Errorf("lists: %q, %v", out, err)
	}
}

// TestIrieJamminLinkAsksForTheClientIDOpensTheBrowserAndWaits: with no
// app yet, link asks for the Client ID on a terminal, starts the sign-in,
// opens the page at Return, and waits until the source is ready.
func TestIrieJamminLinkAsksForTheClientIDOpensTheBrowserAndWaits(t *testing.T) {
	looks := 0
	f := &fakeJam{answer: func(a ipc.JamminArgs) (ipc.JamminResult, error) {
		switch a.Do {
		case ipc.JamSignIn:
			return ipc.JamminResult{Link: &ipc.JamLink{Source: ipc.JamSpotify, URL: "https://accounts.example.com/authorize?x", Redirect: "http://127.0.0.1/jammin/spotify", Until: time.Now().Add(time.Minute).Unix()}}, nil
		}
		looks++
		st := ipc.JamSource{Name: ipc.JamSpotify, State: ipc.JamNeeds, Problem: "No Spotify app yet: make one."}
		switch {
		case looks == 2:
			st.State = ipc.JamLinking
		case looks > 2:
			st.State, st.Account = ipc.JamReady, "Alice"
		}
		return ipc.JamminResult{Sources: []ipc.JamSource{st}}, nil
	}}
	out, errs, opened, err := jamRun(t, f, "0123456789abcdef0123456789abcdef\n\n", true, "link", "spotify")
	if err != nil || !strings.Contains(out, "Signed in to Spotify as Alice") {
		t.Fatalf("link: %q %q %v", out, errs, err)
	}
	if !strings.Contains(errs, "Redirect URI: http://127.0.0.1/jammin/spotify") || !strings.Contains(errs, "https://accounts.example.com/authorize?x") {
		t.Errorf("said %q", errs)
	}
	if len(opened) != 1 || opened[0] != "https://accounts.example.com/authorize?x" {
		t.Errorf("opened %v", opened)
	}
	if f.calls[1].ClientID != "0123456789abcdef0123456789abcdef" {
		t.Errorf("sign-in call %+v", f.calls[1])
	}
	// Without a terminal, no Client ID is asked for and nothing opens.
	looks = 0
	if _, _, opened, err := jamRun(t, f, "", false, "link", "spotify"); err == nil || len(opened) != 0 {
		t.Errorf("no terminal: %v, opened %v", err, opened)
	}
}
