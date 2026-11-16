package core

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/tmux"
)

// The Mac app shows one session beside its panes and another without, so
// it sizes each session's terminal: each agent's window keeps its own
// session's size, a session comes into the slot at its own size, and once
// the app is gone every window takes the slot's size again.
func TestTheAppSizesEachSessionsWindow(t *testing.T) {
	c := setup(t)
	app, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, err := app.Subscribe(ipc.SubscribeArgs{View: ipc.ViewApp}); err != nil {
		t.Fatal(err)
	}
	zoomed := func() bool {
		return strings.TrimSpace(c.Tmux(t, "display-message", "-p", "-t", tmux.MainSession+":0", "#{window_zoomed_flag}")) == "1"
	}
	testutil.Eventually(t, 2*time.Second, "the slot did not zoom for the app", zoomed)
	a := c.spawn("beside its panes")
	b := c.spawn("with none")
	c.show(b.ID, false)
	num := func(p map[string]string, k string) int {
		n, _ := strconv.Atoi(p[k])
		return n
	}
	slot := c.slot()
	all := ipc.Size{W: num(slot, "pane_width"), H: num(slot, "pane_height")}
	if all.W < 40 {
		t.Fatalf("slot = %v", slot)
	}
	narrow := ipc.Size{W: all.W - 12, H: all.H}
	width := func(id string) int {
		for _, p := range c.panes() {
			if p["@irie_session"] == id && p["@irie_role"] == tmux.RoleAgent {
				return num(p, "pane_width")
			}
		}
		return 0
	}

	for _, bad := range []ipc.SizesArgs{
		{Size: ipc.Size{W: 0, H: 5}},
		{Size: ipc.Size{W: ipc.SizeMax + 1, H: 5}},
		{Size: all, Sessions: map[string]ipc.Size{a.ID: {W: -1, H: 3}}},
	} {
		if err := c.call(ipc.OpSizes, bad, nil); err == nil {
			t.Errorf("sizes %+v were taken", bad)
		}
	}
	if err := c.call(ipc.OpSizes, ipc.SizesArgs{Size: all, Sessions: map[string]ipc.Size{a.ID: narrow, "Asahi/" + a.ID: narrow}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := width(a.ID); got != narrow.W {
		t.Errorf("A, its panes shown, is %d wide, want %d", got, narrow.W)
	}
	if got := width(b.ID); got != all.W {
		t.Errorf("B, shown, is %d wide, want %d", got, all.W)
	}
	c.show(a.ID, false)
	if s := c.slot(); s["@irie_session"] != a.ID || num(s, "pane_width") != narrow.W {
		t.Errorf("A came in at %s, want its own %d", s["pane_width"], narrow.W)
	}
	if got := width(b.ID); got != all.W {
		t.Errorf("B went home at %d, want %d", got, all.W)
	}
	c.show(b.ID, false)
	if s := c.slot(); s["@irie_session"] != b.ID || num(s, "pane_width") != all.W {
		t.Errorf("B came in at %s, want its own %d", s["pane_width"], all.W)
	}
	if got := width(a.ID); got != narrow.W {
		t.Errorf("A went home at %d, want %d", got, narrow.W)
	}
	// Held a moment, then irie:0 follows its client again.
	testutil.Eventually(t, 5*time.Second, "irie:0 still holds the size a swap gave it", func() bool {
		return strings.TrimSpace(c.Tmux(t, "display-message", "-p", "-t", tmux.MainSession+":0", "#{window-size}")) == "latest"
	})

	// The app is gone: the terminal sidebar's slot sizes every agent.
	app.Close()
	testutil.Eventually(t, 5*time.Second, "the agents did not take the slot's size again", func() bool {
		w := num(c.slot(), "pane_width")
		return !zoomed() && width(a.ID) == w && width(b.ID) == w
	})
}
