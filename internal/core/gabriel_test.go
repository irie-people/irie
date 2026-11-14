package core

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/focus"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/sysload"
)

func TestAlwaysHoldsBackOnlyWhileTheUserIsThere(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		on, present, busy, boosting, always bool
		holds                               bool
	}{
		{"room to spare", true, true, false, false, false, false},
		{"busy", true, true, true, false, false, true},
		{"a boost", true, true, false, true, false, true},
		{"always, with room to spare", true, true, false, false, true, true},
		{"always, while away", true, false, true, true, true, false},
		{"always, with Gabriel off", false, true, true, true, true, false},
	} {
		if got := holds(tc.on, tc.present, tc.busy, tc.boosting, tc.always); got != tc.holds {
			t.Errorf("%s: holds = %v, want %v", tc.name, got, tc.holds)
		}
	}
}

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

func TestAShowComesIntoTheFocusAtOnce(t *testing.T) {
	c := &Core{sessions: map[string]*session{
		"s-before":  {id: "s-before"},
		"s-shown":   {id: "s-shown"},
		"s-install": {id: "s-install"},
	}, selected: "s-shown", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.useConfig(config.Default())
	// With Gabriel off, as in a test core, a show leaves his state be.
	t.Setenv("IRIE_GABRIEL", "off")
	c.gab.focus = map[string]bool{"s-before": true}
	c.refocus()
	if want := map[string]bool{"s-before": true}; !reflect.DeepEqual(c.gab.focus, want) || c.gab.top != nil {
		t.Errorf("Gabriel off: focus %v, top %v", c.gab.focus, c.gab.top)
	}
	// Holding back: the session now on show is in the focus, and the one
	// before steps back with the rest, before the show returns rather than
	// at the next sweep. An install stays in it. The session on show is
	// the top of the focus at once too.
	t.Setenv("IRIE_GABRIEL", "")
	c.gab.holding, c.gab.present, c.gab.irieFront = true, true, true
	c.gab.installs = map[string]bool{"s-install": true}
	c.refocus()
	if want := map[string]bool{"s-shown": true, "s-install": true}; !reflect.DeepEqual(c.gab.focus, want) {
		t.Errorf("holding back: focus %v, want %v", c.gab.focus, want)
	}
	if want := map[string]bool{"s-shown": true}; !reflect.DeepEqual(c.gab.top, want) || c.gab.topOf != "s-shown" {
		t.Errorf("holding back: top %v of %q, want s-shown", c.gab.top, c.gab.topOf)
	}
	// A show is the user's own doing. Back from away, or from another app,
	// the tab they turn to goes on top at once, so a promotion never keeps
	// it in the background, and the next sweep reads the idle time afresh.
	read := time.Now()
	c.gab = gabState{idleRead: read}
	c.sessions["s-before"].track.priority = 1
	c.selected = "s-install"
	c.refocus()
	if want := map[string]bool{"s-install": true}; !reflect.DeepEqual(c.gab.top, want) || !c.gab.present || !c.gab.irieFront || c.gab.shownAt.IsZero() {
		t.Errorf("back from away: top %v, present %v, Irie in front %v", c.gab.top, c.gab.present, c.gab.irieFront)
	}
	if !c.gab.idleRead.IsZero() {
		t.Error("back from away: the idle time is not read afresh")
	}
	if c.gab.holding {
		t.Error("back from away: holding back before a sweep said so")
	}
}

func TestShowingAPeerRowTakesTheTopAway(t *testing.T) {
	t.Setenv("IRIE_GABRIEL", "")
	c := &Core{sessions: map[string]*session{"s-shown": {id: "s-shown"}}, selected: "s-shown",
		log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.useConfig(config.Default())
	c.gab = gabState{present: true, irieFront: true, holding: true, topBusy: true, topOf: "s-shown", top: map[string]bool{"s-shown": true}}
	c.selected = "peer:s-elsewhere"
	c.refocus()
	if c.gab.top != nil || c.gab.topBusy {
		t.Errorf("a peer's row on show: top %v, busy %v", c.gab.top, c.gab.topBusy)
	}
}

func TestTheTabOnTopTakesItsSubtasks(t *testing.T) {
	now := time.Now()
	sub := func(id, parent string) *session {
		return &session{id: id, out: &outcome{parent: parent}}
	}
	c := &Core{sessions: map[string]*session{
		"s-shown":  {id: "s-shown"},
		"s-sub":    sub("s-sub", "s-shown"),
		"s-subsub": sub("s-subsub", "s-sub"),
		"s-gone":   sub("s-gone", "s-shown"),
		"s-told":   {id: "s-told", userWord: now.Add(-time.Minute)},
		"s-helper": sub("s-helper", "s-told"),
		"s-other":  {id: "s-other"},
		"s-alone":  sub("s-alone", "s-other"),
	}, selected: "s-shown"}
	c.sessions["s-gone"].dead = true
	if got, want := c.onTop(true), map[string]bool{"s-shown": true, "s-sub": true, "s-subsub": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("top = %v, want the session on show and what it delegated", got)
	}
	// Another app in front, or nothing on show: no tab is on top.
	if got := c.onTop(false); got != nil {
		t.Errorf("another app in front: top %v", got)
	}
	c.selected = ""
	if got := c.onTop(true); got != nil {
		t.Errorf("nothing on show: top %v", got)
	}
	// What a session in the focus delegated is in the focus too.
	got := c.inFocus(false, 10*time.Minute, now)
	if want := map[string]bool{"s-told": true, "s-helper": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("focus = %v, want %v", got, want)
	}
}

func TestTopBusyNextCalmsDownSlowly(t *testing.T) {
	now := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		cpu       float64
		critical  bool
		was       bool
		calmSince time.Time
		at        time.Time
		busy      bool
	}{
		{"waits on the user", 3, false, false, time.Time{}, now, false},
		{"its agent thinks", 25, false, false, time.Time{}, now, false},
		{"runs tests", 480, false, false, time.Time{}, now, true},
		{"just heavy", 50, false, false, time.Time{}, now, true},
		{"memory critically short", 3, true, false, time.Time{}, now, true},
		{"light again, not for long", 10, false, true, now, now.Add(10 * time.Second), true},
		{"light again, for long enough", 10, false, true, now, now.Add(gabrielCalm), false},
	} {
		busy, _ := topBusyNext(tc.cpu, 50, tc.critical, tc.was, tc.calmSince, tc.at)
		if busy != tc.busy {
			t.Errorf("%s: busy = %v, want %v", tc.name, busy, tc.busy)
		}
	}
	busy, since := topBusyNext(10, 50, false, true, time.Time{}, now)
	if !busy || !since.Equal(now) {
		t.Errorf("first light reading: %v, %v", busy, since)
	}
}

func TestPlaceTopReadsWhetherTheTabWorks(t *testing.T) {
	now := time.Now()
	c := &Core{sessions: map[string]*session{
		"s-shown": {id: "s-shown", cpu: 20},
		"s-sub":   {id: "s-sub", cpu: 40, out: &outcome{parent: "s-shown"}},
		"s-next":  {id: "s-next", cpu: 5},
	}, selected: "s-shown"}
	c.useConfig(config.Default())
	// At the computer there is a top, so a promotion never puts it in the
	// background; while Gabriel holds nothing back, no one steps back for
	// it however it works.
	st := gabState{present: true, irieFront: true}
	c.placeTop(&st, now)
	if len(st.top) != 2 || st.topBusy {
		t.Errorf("not holding back: top %v, busy %v", st.top, st.topBusy)
	}
	// Holding back: the tab and its subtask use 60% of a CPU between them,
	// over arch.gabriel.heavy.
	st.holding = true
	c.placeTop(&st, now)
	if len(st.top) != 2 || !st.topBusy || st.topOf != "s-shown" {
		t.Errorf("holding back: top %v, busy %v, of %q", st.top, st.topBusy, st.topOf)
	}
	// Running light, it stays at work until it has been light a while.
	c.sessions["s-sub"].cpu = 0
	c.placeTop(&st, now.Add(5*time.Second))
	if !st.topBusy {
		t.Error("light for a moment, the rest came back at once")
	}
	c.placeTop(&st, now.Add(5*time.Second+gabrielCalm))
	if st.topBusy {
		t.Error("light for long enough, still at work")
	}
	// A tab the user turns to starts afresh, light as it is.
	c.sessions["s-shown"].cpu = 300
	c.placeTop(&st, now.Add(time.Minute))
	c.selected = "s-next"
	c.placeTop(&st, now.Add(time.Minute+5*time.Second))
	if st.topBusy || st.topOf != "s-next" {
		t.Errorf("turned to a light tab: busy %v, of %q", st.topBusy, st.topOf)
	}
	// Memory critically short: the tab on top comes first, light or not.
	c.load.Pressure = sysload.PressureCritical
	c.placeTop(&st, now.Add(2*time.Minute))
	if !st.topBusy {
		t.Error("memory critically short, the tab on top does not come first")
	}
	// The user away, or another app in front: nothing is on top.
	st.present = false
	c.placeTop(&st, now)
	if st.top != nil || st.topBusy {
		t.Errorf("away: top %v, busy %v", st.top, st.topBusy)
	}
	st = gabState{present: true, holding: true}
	c.placeTop(&st, now)
	if st.top != nil {
		t.Errorf("another app in front: top %v", st.top)
	}
}

func TestTheOrderOfTheFocus(t *testing.T) {
	c := &Core{}
	none := map[string]bool{}
	raised := map[string]bool{"s-raised": true, "s-raised-sub": true}
	for _, tc := range []struct {
		name   string
		gab    gabState
		raised map[string]bool
		back   []string
	}{
		{"nothing to step back for", gabState{}, none, nil},
		{"holding back", gabState{holding: true, focus: map[string]bool{"s-top": true, "s-told": true}, top: map[string]bool{"s-top": true}},
			none, []string{"s-other", "s-install", "s-raised", "s-raised-sub"}},
		{"the tab on top at work", gabState{holding: true, topBusy: true, focus: map[string]bool{"s-top": true, "s-told": true, "s-install": true},
			top: map[string]bool{"s-top": true}, installs: map[string]bool{"s-install": true}},
			none, []string{"s-told", "s-other", "s-raised", "s-raised-sub"}},
		// The promoted step back for the tab on top, and while it works an
		// install does not step back for them either.
		{"the tab on top at work, with a promotion", gabState{holding: true, topBusy: true, focus: map[string]bool{"s-top": true, "s-told": true, "s-install": true, "s-raised": true},
			top: map[string]bool{"s-top": true}, installs: map[string]bool{"s-install": true}},
			raised, []string{"s-told", "s-other", "s-raised", "s-raised-sub"}},
		{"a promotion", gabState{top: map[string]bool{"s-top": true}}, raised, []string{"s-told", "s-other", "s-install"}},
		{"a promotion, no tab on top", gabState{}, raised, []string{"s-top", "s-told", "s-other", "s-install"}},
	} {
		c.gab = tc.gab
		var back []string
		for _, id := range []string{"s-top", "s-told", "s-other", "s-install", "s-raised", "s-raised-sub"} {
			if c.stepsBack(id, tc.raised) {
				back = append(back, id)
			}
		}
		if !slices.Equal(back, tc.back) {
			t.Errorf("%s: %v step back, want %v", tc.name, back, tc.back)
		}
	}
}

func TestTheReportPutsTheTabOnTopFirst(t *testing.T) {
	cfg := config.Default()
	c := &Core{sessions: map[string]*session{
		"s-a":     {id: "s-a", name: "a sub", cpu: 30, demoted: false},
		"s-top":   {id: "s-top", name: "the tab", cpu: 200},
		"s-told":  {id: "s-told", name: "told", demoted: true},
		"s-inst":  {id: "s-inst", name: "install"},
		"s-other": {id: "s-other", name: "other", demoted: true},
	}, selected: "s-top"}
	c.useConfig(cfg)
	c.gab = gabState{present: true, holding: true, topBusy: true,
		top:   map[string]bool{"s-a": true, "s-top": true},
		focus: map[string]bool{"s-a": true, "s-top": true, "s-told": true, "s-inst": true}}
	r := c.gabrielReport()
	if len(r.Top) != 2 || r.Top[0].ID != "s-top" || r.Top[1].ID != "s-a" || r.TopCPU != 230 || !r.TopBusy {
		t.Errorf("top %+v at %v%%, busy %v", r.Top, r.TopCPU, r.TopBusy)
	}
	if len(r.Back) != 1 || r.Back[0].ID != "s-told" || len(r.Held) != 1 || r.Held[0].ID != "s-other" {
		t.Errorf("back %+v, held %+v", r.Back, r.Held)
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

func TestBoostLastsItsTimeAndSaysSo(t *testing.T) {
	// A boost wakes a sweep, which must not slow this machine's processes.
	t.Setenv("IRIE_GABRIEL", "off")
	cfg := config.Default()
	cfg.Local.URL = "http://127.0.0.1:1" // not Irie's own Ollama: no models to free
	c := &Core{sessions: map[string]*session{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.useConfig(cfg)
	for _, bad := range []ipc.GabrielArgs{{Boost: true, Stop: true}, {Boost: true, For: "10s"}, {Boost: true, For: "13h"}, {Boost: true, For: "soon"}} {
		if _, err := c.gabrielOp(bad); err == nil {
			t.Errorf("%+v: no error", bad)
		}
	}
	r, err := c.gabrielOp(ipc.GabrielArgs{Boost: true, For: "30m"})
	if err != nil {
		t.Fatal(err)
	}
	if r.BoostUntil == nil || time.Until(*r.BoostUntil) < 29*time.Minute || time.Until(*r.BoostUntil) > 30*time.Minute {
		t.Errorf("boost until %v, want in half an hour", r.BoostUntil)
	}
	r, _ = c.gabrielOp(ipc.GabrielArgs{Boost: true})
	if r.BoostUntil == nil || time.Until(*r.BoostUntil) < 59*time.Minute {
		t.Errorf("a boost of the configured hour ends at %v", r.BoostUntil)
	}
	if r, _ = c.gabrielOp(ipc.GabrielArgs{Stop: true}); r.BoostUntil != nil || r.Always {
		t.Errorf("stopped, still until %v or always", r.BoostUntil)
	}
	// Boosting always is the config's, and the report says so.
	cfg.Arch.Gabriel.Always = true
	c.useConfig(cfg)
	if r, _ = c.gabrielOp(ipc.GabrielArgs{}); !r.Always || r.BoostUntil != nil {
		t.Errorf("always: %+v", r)
	}
}

func TestTheHeaderSaysHowGabrielBoosts(t *testing.T) {
	t.Setenv("IRIE_GABRIEL", "")
	cfg := config.Default()
	c := &Core{sessions: map[string]*session{}}
	c.useConfig(cfg)
	now := time.Now()
	if !proc.CanBackground {
		if b := c.boostHeader(now); b != nil {
			t.Errorf("no background state here, yet a boost: %+v", b)
		}
		return
	}
	if b := c.boostHeader(now); b != nil {
		t.Errorf("no boost: %+v", b)
	}
	c.gab.boostUntil = now.Add(time.Hour)
	if b := c.boostHeader(now); b == nil || b.Until == nil || !b.Until.Equal(c.gab.boostUntil) || b.Always {
		t.Errorf("a boost of an hour: %+v", b)
	}
	if b := c.boostHeader(now.Add(2 * time.Hour)); b != nil {
		t.Errorf("a boost whose time is up: %+v", b)
	}
	c.gab.boostUntil = time.Time{}
	cfg.Arch.Gabriel.Always = true
	c.useConfig(cfg)
	if b := c.boostHeader(now); b == nil || !b.Always || b.Until != nil {
		t.Errorf("always: %+v", b)
	}
	// With Gabriel off nothing boosts, always or not.
	t.Setenv("IRIE_GABRIEL", "off")
	if b := c.boostHeader(now); b != nil {
		t.Errorf("Gabriel off: %+v", b)
	}
}

func TestStealsFocus(t *testing.T) {
	for cmd, want := range map[string]bool{
		"open mac/build/Irie.app":                                true,
		"open https://example.com":                               true,
		"open -a Safari":                                         true,
		"cd /tmp && open report.pdf":                             true,
		"open -R build/out.txt":                                  true,
		"FOO=1 open file.html":                                   true,
		`osascript -e 'tell application "Messages" to activate'`: true,
		`osascript -e 'tell application "System Events" to keystroke "v"'`:                                                   true,
		`"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --user-data-dir=/tmp/p --remote-debugging-port=9334`: true,
		"mac/build/Irie.app/Contents/MacOS/Irie":                                                                             true,
		"open -g mac/build/Irie.app --args --background":                                                                     false,
		"open -gj -a Safari": false,
		"open -j report.pdf": false,
		"open":               false,
		`"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --user-data-dir=/tmp/p`: false,
		"mac/build/Irie.app/Contents/MacOS/Irie --snapshot /tmp/shots":                                         false,
		"mac/build/Irie.app/Contents/MacOS/Irie --record /tmp/first-look.mp4 --reel docs/demo/first-look.reel": false,
		`osascript -e 'display notification "done"'`:                                                           false,
		"go test ./... && git commit -m 'open the door'":                                                       false,
		"opencode run":   false,
		"grep -r open .": false,
	} {
		if got := stealsFocus(cmd); got != want {
			t.Errorf("stealsFocus(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestAppSwapWaitsUntilTheUserLooksAway(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Irie.app")
	stamp := filepath.Join(dir, "app.commit")
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(s), 0o644)
	}
	write(filepath.Join(app, "build"), "old")
	write(filepath.Join(app+".next", "build"), "new")
	write(stamp, "aaa")
	write(stamp+".next", "bbb")
	front, idle := irieBundleID, time.Second
	var opened []bool
	s := appSwap{
		app: app, stamp: stamp,
		front:    func() (string, bool) { return front, true },
		idle:     func() (time.Duration, bool) { return idle, true },
		stop:     func() bool { return true },
		open:     func(_ string, inFront bool) error { opened = append(opened, inFront); return nil },
		register: func(string) {},
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	// In use: in front, input a second ago. It waits.
	if err := s.run(); err != nil || read(filepath.Join(app, "build")) != "old" || len(opened) != 0 {
		t.Fatalf("swapped while in use: %v, %q, %v", err, read(filepath.Join(app, "build")), opened)
	}
	// Another app in front: it swaps, and opens behind it.
	front = "com.google.Chrome"
	if err := s.run(); err != nil {
		t.Fatal(err)
	}
	if read(filepath.Join(app, "build")) != "new" || read(stamp) != "bbb" || !slices.Equal(opened, []bool{false}) {
		t.Errorf("after the swap: %q, stamp %q, opened %v", read(filepath.Join(app, "build")), read(stamp), opened)
	}
	if _, err := os.Stat(app + ".next"); !os.IsNotExist(err) {
		t.Error("the next build is still there")
	}
	// In front but the user away: it swaps and comes back in front.
	write(filepath.Join(app+".next", "build"), "newer")
	front, idle = irieBundleID, 5*time.Minute
	if err := s.run(); err != nil || read(filepath.Join(app, "build")) != "newer" || !slices.Equal(opened, []bool{false, true}) {
		t.Errorf("user away: %v, %q, %v", err, read(filepath.Join(app, "build")), opened)
	}
	// Nothing waits: nothing happens.
	if err := s.run(); err != nil || len(opened) != 2 {
		t.Errorf("with nothing waiting: %v, %v", err, opened)
	}
}

func TestAnInstallIsNeverHeldBack(t *testing.T) {
	ps := `  100 -zsh
  200 sh tools/live.sh
  201 go test ./...
  300 sh /tmp/scratch/install-loop.sh
  301 /bin/sh tools/live.sh --rollback
  400 grep live.sh
`
	if got := installPIDs(ps); !slices.Equal(got, []int{200, 301}) {
		t.Errorf("installs = %v, want 200 and 301", got)
	}
	// Session s1's pane is 10; the install runs two levels under it. s2's
	// pane is 20, with nothing installing.
	tbl := proc.NewTable([]proc.Proc{
		{PID: 10, PPID: 1}, {PID: 11, PPID: 10}, {PID: 200, PPID: 11},
		{PID: 20, PPID: 1}, {PID: 21, PPID: 20},
		{PID: 301, PPID: 1},
	})
	got := sessionsOf([]int{200, 301}, tbl, map[int]string{10: "s1", 20: "s2"})
	if want := map[string]bool{"s1": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("sessions installing = %v, want %v", got, want)
	}
}
