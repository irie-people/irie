package core

import (
	"io"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/focus"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/sysload"
)

func TestBusyNextHasRoomToCalmDown(t *testing.T) {
	now := time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC)
	l := func(rq float64, pressure int) sysload.Load {
		return sysload.Load{CPUs: 12, RunQueue: rq, Pressure: pressure}
	}
	for _, tc := range []struct {
		name      string
		load      sysload.Load
		was       bool
		calmSince time.Time
		at        time.Time
		busy      bool
	}{
		{"room to spare", l(5, 1), false, time.Time{}, now, false},
		{"less than a CPU free", l(11.5, 1), false, time.Time{}, now, true},
		{"memory under pressure", l(2, sysload.PressureWarn), false, time.Time{}, now, true},
		{"between, while busy", l(10.5, 1), true, time.Time{}, now, true},
		{"between, while calm", l(10.5, 1), false, time.Time{}, now, false},
		{"room again, not for long", l(4, 1), true, now, now.Add(10 * time.Second), true},
		{"room again, for long enough", l(4, 1), true, now, now.Add(gabrielCalm), false},
		{"no reading", sysload.Load{}, true, time.Time{}, now, false},
	} {
		busy, _ := busyNext(tc.load, tc.was, tc.calmSince, tc.at)
		if busy != tc.busy {
			t.Errorf("%s: busy = %v, want %v", tc.name, busy, tc.busy)
		}
	}
	// The calm is timed from when the room came back.
	busy, since := busyNext(l(4, 1), true, time.Time{}, now)
	if !busy || !since.Equal(now) {
		t.Errorf("first calm reading: %v, %v", busy, since)
	}
}

func TestInFocus(t *testing.T) {
	now := time.Now()
	c := &Core{sessions: map[string]*session{
		"s-shown":    {id: "s-shown"},
		"s-told":     {id: "s-told", userWord: now.Add(-5 * time.Minute)},
		"s-told-old": {id: "s-told-old", userWord: now.Add(-30 * time.Minute)},
		"s-raised":   {id: "s-raised"},
		"s-other":    {id: "s-other"},
	}, selected: "s-shown"}
	c.sessions["s-raised"].track.priority = 1
	got := c.inFocus(true, 10*time.Minute, now)
	if want := map[string]bool{"s-shown": true, "s-told": true, "s-raised": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("Irie in front: %v, want %v", got, want)
	}
	// Another app in front: the session on show steps back with the rest.
	got = c.inFocus(false, 10*time.Minute, now)
	if want := map[string]bool{"s-told": true, "s-raised": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("another app in front: %v, want %v", got, want)
	}
}

func TestHeavyOutsidePicksOnlyWhatMaySlow(t *testing.T) {
	me := os.Getuid()
	c := &Core{bin: "/home/alice/.local/share/irie/bin/irie"}
	g := config.Default().Arch.Gabriel
	// The process table: a session's pane (100) with a build under it, the
	// app in front (200) with a helper, and the rest.
	t0 := proc.NewTable([]proc.Proc{
		{PID: 100, PPID: 1, Start: 1}, {PID: 101, PPID: 100, Start: 2},
		{PID: 200, PPID: 1, Start: 3}, {PID: 201, PPID: 200, Start: 4},
		{PID: 300, PPID: 1, Start: 5}, {PID: 301, PPID: 1, Start: 6}, {PID: 302, PPID: 1, Start: 7},
		{PID: 303, PPID: 1, Start: 8}, {PID: 304, PPID: 1, Start: 9}, {PID: 305, PPID: 1, Start: 10},
		{PID: 306, PPID: 1, Start: 11}, {PID: 307, PPID: 1, Start: 12}, {PID: 308, PPID: 1, Start: 13},
	})
	ps := []focus.Proc{
		{PID: 101, UID: me, CPU: 300, Path: "go"},                                                         // a session's own
		{PID: 201, UID: me, CPU: 150, Path: "/Applications/Chrome.app/Contents/Frameworks/Helper"},        // the app in front
		{PID: 300, UID: me, CPU: 180, Path: "/Applications/Docker.app/Contents/MacOS/com.docker.backend"}, // heavy: slowed
		{PID: 301, UID: me, CPU: 20, Path: "/Applications/Notes.app/Contents/MacOS/Notes"},                // light
		{PID: 302, UID: me, CPU: 90, Path: "/Applications/zoom.us.app/Contents/MacOS/zoom.us"},            // spared
		{PID: 303, UID: me, CPU: 95, Path: "/usr/libexec/mdworker"},                                       // macOS's own
		{PID: 304, UID: 0, CPU: 99, Path: "/usr/local/bin/heavy"},                                         // not the user's
		{PID: 305, UID: me, CPU: 80, Path: "/Applications/Music.app/Contents/MacOS/Music"},                // spared
		{PID: 306, UID: me, CPU: 70, Path: "/opt/homebrew/bin/ffmpeg"},                                    // kept awake for
		{PID: 307, UID: me, CPU: 60, Path: c.bin},                                                         // Irie's own
		{PID: 308, UID: me, CPU: 75, Path: "/opt/homebrew/bin/cargo"},                                     // heavy: slowed
	}
	st := gabState{holding: true, frontOK: true, front: focus.App{Name: "Chrome", PID: 200, BundlePath: "/Applications/Chrome.app"}}
	got := c.heavyOutside(ps, t0, map[int]bool{306: true}, st, g, []int{100}, nil)
	pids := slices.Sorted(func(yield func(int) bool) {
		for pid := range got {
			if !yield(pid) {
				return
			}
		}
	})
	if !slices.Equal(pids, []int{300, 308}) {
		t.Errorf("slowed %v, want the Docker backend and cargo", pids)
	}
	if got[300].Start != 5 || got[300].CPU != 180 {
		t.Errorf("kept %+v", got[300])
	}
	// One already slowed stays so while it runs light, until a process
	// with its pid but another start takes its place.
	ps[3].CPU = 10
	held := map[int]outProc{301: {Start: 6, CPU: 70}, 300: {Start: 99, CPU: 180}}
	got = c.heavyOutside(ps, t0, nil, st, g, []int{100}, held)
	if _, ok := got[301]; !ok {
		t.Errorf("a slowed app that now runs light was let go: %v", got)
	}
	if o, ok := got[300]; !ok || o.Start != 5 {
		t.Errorf("a new process with a slowed one's pid: %+v", o)
	}
}

func TestSystemAndSpared(t *testing.T) {
	for path, want := range map[string]bool{"/System/Library/X": true, "/usr/libexec/mdworker": true, "/usr/local/bin/x": false, "/opt/homebrew/bin/go": false, "go": false} {
		if systemPath(path) != want {
			t.Errorf("systemPath(%q) = %v", path, !want)
		}
	}
	spare := []string{"zoom.us", "Music", "QuickTime Player"}
	for path, want := range map[string]bool{
		"/Applications/zoom.us.app/Contents/MacOS/zoom.us":                          true,
		"/System/Applications/Music.app/Contents/MacOS/Music":                       true,
		"/System/Applications/QuickTime Player.app/Contents/MacOS/QuickTime Player": true,
		"/Applications/Musicbox.app/Contents/MacOS/Musicbox":                        false,
		"/opt/homebrew/bin/ffmpeg":                                                  false,
	} {
		if spared(path, spare) != want {
			t.Errorf("spared(%q) = %v", path, !want)
		}
	}
}

func TestOutsideSurvivesTheCore(t *testing.T) {
	c := &Core{paths: paths.Paths{State: t.TempDir()}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	held := map[int]outProc{424242: {Start: 1, Path: "/opt/x", CPU: 90}}
	c.saveOutside(held)
	if _, err := os.Stat(c.outsideFile()); err != nil {
		t.Fatalf("not kept for the next core: %v", err)
	}
	// The next core takes it back; one long gone is simply dropped.
	c.releaseOutside()
	if _, err := os.Stat(c.outsideFile()); !os.IsNotExist(err) {
		t.Errorf("the file outlived the release: %v", err)
	}
	c.saveOutside(nil)
	if _, err := os.Stat(c.outsideFile()); !os.IsNotExist(err) {
		t.Errorf("nothing slowed, yet a file: %v", err)
	}
}
