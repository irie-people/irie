package ui

import (
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// The sidebar says each tell the header carries in its status line once,
// and leaves it standing for the Mac app.
func TestTheSidebarSaysEachTellOnce(t *testing.T) {
	m := New(&fakeBackend{}, Options{Now: time.Now})
	tell := ipc.Tell{ID: "panic:a", From: "raphael", Level: "warn", Text: "Your Mac crashed and restarted at 07:18 on 23 Nov."}
	m.apply(ipc.Frame{T: ipc.FrameHeader, Header: &ipc.Header{Tells: []ipc.Tell{tell}}})
	if m.status != tell.Text || !m.statusErr {
		t.Fatalf("status %q, err %v", m.status, m.statusErr)
	}
	m.setStatus("", false)
	m.apply(ipc.Frame{T: ipc.FrameHeader, Header: &ipc.Header{Tells: []ipc.Tell{tell}}})
	if m.status != "" {
		t.Errorf("said it again: %q", m.status)
	}
	note := ipc.Tell{ID: "room:b", Level: "note", Text: "Your disk is short of room."}
	m.apply(ipc.Frame{T: ipc.FrameSnapshot, Header: &ipc.Header{Tells: []ipc.Tell{tell, note}}})
	if m.status != note.Text || m.statusErr {
		t.Errorf("status %q, err %v", m.status, m.statusErr)
	}
}
