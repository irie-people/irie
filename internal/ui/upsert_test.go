package ui

import (
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-people/irie/internal/ipc"
)

// The core sends a snapshot only when a client subscribes; after that each
// changed row comes as an upsert of its own, even when a move renumbers
// every row (docs/proposals/optimisation.md, iteration 2). The sidebar must
// draw the upserts as it would draw the snapshot they stand for.
func TestUpsertsSortAsASnapshotWould(t *testing.T) {
	rows := sessions(4)
	for i := range rows {
		rows[i].Order = i
	}
	// The last row is promoted: it goes first and the others move down one.
	moved := []ipc.Session{rows[3], rows[0], rows[1], rows[2]}
	for i := range moved {
		moved[i].Order = i
	}
	start := func() Model {
		m := New(&fakeBackend{}, Options{Now: func() time.Time { return now }, Home: "/home/alice"})
		m = send(m, tea.WindowSizeMsg{Width: 60, Height: 24})
		return send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: rows, Header: &ipc.Header{}}})
	}
	byUpserts := start()
	for i := range moved {
		byUpserts = send(byUpserts, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &moved[i]}})
	}
	bySnapshot := start()
	bySnapshot = send(bySnapshot, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: moved, Header: &ipc.Header{}}})
	ids := func(m Model) []string {
		var out []string
		for _, s := range m.sessions {
			out = append(out, s.ID)
		}
		return out
	}
	if a, b := ids(byUpserts), ids(bySnapshot); !slices.Equal(a, b) || a[0] != rows[3].ID {
		t.Errorf("after upserts %v, after a snapshot %v; want the promoted row first in both", a, b)
	}
	if a, b := screen(t, byUpserts), screen(t, bySnapshot); !slices.Equal(a, b) {
		t.Errorf("the upserts drew\n%v\nthe snapshot\n%v", a, b)
	}
}
