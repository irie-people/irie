package core

import (
	"encoding/json"
	"maps"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/sysload"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/ui"
)

func TestCloseWhenDoneClosesAFinishedSession(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "close_when_done = true\nclose_delay = \"1s\"\n"})
	a := c.spawn("prompt,stop")
	testutil.Eventually(t, 10*time.Second, "the finished session is still open", func() bool { return len(c.list()) == 0 })
	if r := c.listAll(); len(r.Closed) != 1 || r.Closed[0].ID != a.ID {
		t.Errorf("closed = %+v", r.Closed)
	}
	// Reopen undoes it.
	if _, err := c.reopen(a.ID); err != nil {
		t.Errorf("reopen: %v", err)
	}
}

func TestCloseWhenDoneWaitsForAnotherPrompt(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "close_when_done = true\nclose_delay = \"3s\"\n"})
	a := c.spawn("prompt,stop")
	c.waitState(a.ID, ipc.StateDone)
	// A message inside the delay is another turn: the timer is void.
	time.Sleep(time.Second)
	if err := c.call(ipc.OpSend, ipc.SendArgs{Session: a.ID, Text: "and one more thing"}, nil); err != nil {
		t.Fatalf("send: %v", err)
	}
	time.Sleep(2500 * time.Millisecond)
	if _, ok := c.row(a.ID); !ok {
		t.Fatal("the session closed after it was given more to do")
	}
	// Its new turn ends, and it closes then.
	testutil.Eventually(t, 10*time.Second, "the session never closed", func() bool { _, ok := c.row(a.ID); return !ok })
}

func TestCloseWhenDoneLeavesAPausedSession(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "close_when_done = true\nclose_delay = \"2s\"\n"})
	a := c.spawn("prompt,stop")
	c.waitState(a.ID, ipc.StateDone)
	c.freeze(a.ID, true)
	time.Sleep(3 * time.Second)
	if _, ok := c.row(a.ID); !ok {
		t.Fatal("close_when_done closed a session the user paused")
	}
}

func TestASessionStaysOpenByDefault(t *testing.T) {
	c := setup(t)
	a := c.spawn("prompt,stop")
	c.waitState(a.ID, ipc.StateDone)
	time.Sleep(time.Second)
	if _, ok := c.row(a.ID); !ok {
		t.Fatal("the session closed with close_when_done off")
	}
}

func TestTurningCloseWhenDoneOffStopsAWaitingClose(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "close_when_done = true\nclose_delay = \"2s\"\n"})
	a := c.spawn("prompt,stop")
	c.waitState(a.ID, ipc.StateDone)
	off := ipc.ConfigArgs{Set: []ipc.ConfigSet{{Key: "spawn.close_when_done", Value: json.RawMessage("false")}}}
	if err := c.call(ipc.OpConfig, off, nil); err != nil {
		t.Fatalf("config: %v", err)
	}
	time.Sleep(3 * time.Second)
	if _, ok := c.row(a.ID); !ok {
		t.Fatal("the session closed after close_when_done was turned off")
	}
}

func TestLookTaskKeepsAnswerSessionsOpen(t *testing.T) {
	for task, want := range map[string]bool{
		"status of the padding audit":     true,
		"What changed in the last commit": true,
		"summarize the open PRs":          true,
		"review this diff":                true,
		"rename Foo to Bar in core":       false,
		"add a test for close":            false,
		"prompt,stop":                     false,
	} {
		if got := lookTask.MatchString(task); got != want {
			t.Errorf("lookTask(%q) = %v, want %v", task, got, want)
		}
	}
}

func TestTheHeaderCarriesCloseWhenDone(t *testing.T) {
	c := &Core{version: "test"}
	c.useConfig(config.Default())
	if c.headerNow().CloseWhenDone {
		t.Error("close_when_done is on by default")
	}
	c.cfg().Spawn.CloseWhenDone = true
	if !c.headerNow().CloseWhenDone {
		t.Error("the header does not carry close_when_done")
	}
}

func TestCloseWhenDoneLeavesWorkInTheBackground(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "close_when_done = true\nclose_delay = \"1s\"\n"})
	// Its turn ended on an install it runs in the background, which will
	// wake it: closing it would end the install.
	a := c.spawn("prompt,stop:bg")
	c.waitState(a.ID, ipc.StateDone)
	time.Sleep(2 * time.Second)
	if row, ok := c.row(a.ID); !ok || row.Detail != "Waiting on its work in the background · Install the build" {
		t.Fatalf("the session waiting on its own work: %+v, open %v", row, ok)
	}
}

func TestTheHeaderSaysHowBusyTheMachineIs(t *testing.T) {
	c := &Core{version: "test"}
	c.useConfig(config.Default())
	if c.headerNow().Load != nil {
		t.Error("a load before the first read")
	}
	c.load = sysload.Load{CPUs: 12, RunQueue: 14.3, Pressure: sysload.PressureWarn, PTYs: 134, PTYMax: 511}
	got := c.headerNow().Load
	want := ipc.Load{CPUs: 12, RunQueue: 14, Pressure: 2, PTYs: 128, PTYMax: 511}
	if got == nil || *got != want {
		t.Errorf("header load = %+v, want %+v", got, want)
	}
}

// Once the Mac app names its palette, the header carries its colours, so
// the terminal sidebar, the slot and the phone app draw in it too; a
// palette of the user's own comes with them, a set left out rosewater's.
func TestTheHeaderCarriesTheAppsPalette(t *testing.T) {
	c := &Core{version: "test"}
	c.useConfig(config.Default())
	if c.headerNow().Theme != nil {
		t.Error("a palette before the app said one")
	}
	if err := c.theme(ipc.ThemeArgs{Theme: "lilac"}); err != nil {
		t.Fatal(err)
	}
	lilac, _ := ui.BuiltIn("lilac")
	light, dark := lilac.Colors()
	if h := c.headerNow().Theme; h == nil || h.Name != "lilac" || !maps.Equal(h.Light, light) || !maps.Equal(h.Dark, dark) {
		t.Errorf("lilac: %+v", h)
	}
	own := map[string]string{"accent": "#336699"}
	if err := c.theme(ipc.ThemeArgs{Theme: "harbour", Light: own}); err != nil {
		t.Fatal(err)
	}
	rose, _ := ui.BuiltIn("rosewater")
	_, roseDark := rose.Colors()
	if h := c.headerNow().Theme; h == nil || h.Name != "harbour" || h.Light["accent"] != "#336699" || !maps.Equal(h.Dark, roseDark) {
		t.Errorf("a palette of the user's own: %+v", h)
	}
	if err := c.theme(ipc.ThemeArgs{Theme: "harbour", Light: map[string]string{"accent": "blue"}}); err == nil {
		t.Error("a colour that is not #RRGGBB was taken")
	}
	if h := c.headerNow().Theme; h == nil || h.Light["accent"] != "#336699" {
		t.Errorf("a refused palette changed the header: %+v", h)
	}
}
