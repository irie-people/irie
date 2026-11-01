package core

import (
	"reflect"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
)

// heardUntilQuiet collects what a subscription sends until it has been quiet for d.
func heardUntilQuiet(ch <-chan ipc.Frame, d time.Duration) []ipc.Frame {
	var got []ipc.Frame
	for {
		select {
		case f := <-ch:
			got = append(got, f)
		case <-time.After(d):
			return got
		}
	}
}

// An idle session's status line sends the same figures every few seconds.
// Such an update must cost the core no diff and its clients no frame; one
// that changed something changes only its own row
// (docs/proposals/optimisation.md §2.4).
func TestAStatusUpdateSendsOnlyWhatChanged(t *testing.T) {
	c := benchCore(t, 4)
	c.mu.Lock()
	c.changes()
	c.mu.Unlock()
	ch, cancel, err := c.Subscribe(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	heardUntilQuiet(ch, 300*time.Millisecond) // the snapshot and the lists after it

	const id = "s000000000002"
	update := func(cost float64) []ipc.Frame {
		t.Helper()
		if _, err := c.status(ipc.StatusArgs{Session: id, Payload: statusPayload(cost)}); err != nil {
			t.Fatal(err)
		}
		return heardUntilQuiet(ch, 300*time.Millisecond)
	}
	// The first update brings the plan's use, which only the header carries.
	if got := update(1.23); len(got) != 1 || got[0].T != ipc.FrameHeader || got[0].Header.Plan == nil {
		t.Errorf("the first update sent %v, want one header with the plan", types(got))
	}
	if got := update(1.23); len(got) != 0 {
		t.Errorf("an update that changed nothing sent %v", types(got))
	}

	// Another session changes meanwhile, as hooks do between ticks: the
	// status update still sends only its own row, and the other's waits for
	// the tick's diff.
	c.mu.Lock()
	c.sessions["s000000000001"].track.apply(event.Event{Session: "s000000000001", Source: event.SourceHook, Kind: event.TurnEnded,
		TS: "2026-10-31T12:30:00.000Z", Data: map[string]any{}})
	c.mu.Unlock()
	got := update(1.24)
	if len(got) != 1 || got[0].T != ipc.FrameUpsert || got[0].Session.ID != id || *got[0].Session.CostUSD != 1.24 {
		t.Fatalf("a new cost sent %v, want one upsert of %s", types(got), id)
	}
	c.mu.Lock()
	frames := c.changes()
	c.mu.Unlock()
	if len(frames) != 1 || frames[0].Session.ID != "s000000000001" {
		t.Errorf("the tick's diff found %v, want only the other session", types(frames))
	}
	if n := c.perf.Counts()["status.unchanged"]; n != 1 {
		t.Errorf("status.unchanged %d, want 1", n)
	}
}

// The one row a status update diffs must be the row the list would give.
func TestOneRowIsTheListsRow(t *testing.T) {
	for _, trod := range []bool{false, true} {
		c := benchCore(t, 5)
		if trod {
			cfg := *c.cfg()
			cfg.Trod.On = true
			c.useConfig(cfg)
		}
		c.mu.Lock()
		c.changes()
		for i, x := range c.sessions {
			cost := 2.5 + float64(len(i))
			x.usage.CostUSD = &cost
			frames := c.changesOf(x)
			if len(frames) != 1 {
				t.Fatalf("trod %v: %s's new cost gave %d frames", trod, i, len(frames))
			}
			for _, row := range c.list() {
				if row.ID == x.id && !reflect.DeepEqual(row, *frames[0].Session) {
					t.Errorf("trod %v: one row\n%+v\nthe list's\n%+v", trod, *frames[0].Session, row)
				}
			}
		}
		if f := c.changes(); len(f) != 0 {
			t.Errorf("trod %v: the full diff still found %d rows to send", trod, len(f))
		}
		c.mu.Unlock()
	}
}

func types(frames []ipc.Frame) []string {
	out := make([]string, len(frames))
	for i, f := range frames {
		out[i] = f.T
		if f.Session != nil {
			out[i] += " " + f.Session.ID
		}
	}
	return out
}
