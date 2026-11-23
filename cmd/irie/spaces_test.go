package main

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
)

// The spaces list counts each space's sessions: a row whose space is "" or
// gone is in the first, and a peer's rows are in none.
func TestSpacesListCountsEachSpacesSessions(t *testing.T) {
	spaces := []ipc.Space{{ID: "main", Name: "Main"}, {ID: "pvideo0000000", Name: "Video Lessons"}, {ID: "pwriting00000", Name: "Writing"}}
	rows := []ipc.Session{
		{ID: "s1"},
		{ID: "s2", Space: "pvideo0000000"},
		{ID: "s3", Space: "pvideo0000000"},
		{ID: "s4", Space: "pgone00000000"},
		{ID: "s5", Space: "pvideo0000000", Host: "Asahi"},
	}
	var b strings.Builder
	if err := printSpaces(&b, spaces, rows, false); err != nil {
		t.Fatal(err)
	}
	want := `SPACE          NAME           SESSIONS
main           Main           2
pvideo0000000  Video Lessons  2
pwriting00000  Writing        0
`
	if b.String() != want {
		t.Errorf("printed\n%s\nwant\n%s", b.String(), want)
	}
	b.Reset()
	if err := printSpaces(&b, spaces, rows, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"name": "Video Lessons",`+"\n      \"sessions\": 2") {
		t.Errorf("--json printed\n%s", b.String())
	}
}

// A space is found by its id, or by its name in any case.
func TestFindSpaceByIDOrName(t *testing.T) {
	spaces := []ipc.Space{{ID: "main", Name: "Main"}, {ID: "pvideo0000000", Name: "Video Lessons"}}
	for ref, want := range map[string]int{"main": 0, "pvideo0000000": 1, "video lessons": 1, " Video  Lessons ": 1, "Writing": -1, "": -1} {
		if got := ipc.FindSpace(spaces, ref); got != want {
			t.Errorf("FindSpace(%q) = %d, want %d", ref, got, want)
		}
	}
}

// reopen --lost brings back the next session only while a quarter of the
// memory is free: it waits, saying so once, and gives up in the end. A
// machine whose memory can't be read has room.
func TestReopenLostWaitsForMemory(t *testing.T) {
	read, poll, wait := readMemory, lostMemoryPoll, lostMemoryWait
	t.Cleanup(func() { readMemory, lostMemoryPoll, lostMemoryWait = read, poll, wait })
	lostMemoryPoll, lostMemoryWait = time.Millisecond, 5*time.Second
	free := []int64{10, 20, 30}
	readMemory = func() (localmodel.Machine, error) {
		f := free[0]
		if len(free) > 1 {
			free = free[1:]
		}
		return localmodel.Machine{Total: 100, Available: f}, nil
	}
	var b strings.Builder
	if !roomForOne(&b) {
		t.Fatal("no room once 30% was free")
	}
	if n := strings.Count(b.String(), "waiting for memory: 10% is free"); n != 1 || strings.Count(b.String(), "\n") != 1 {
		t.Errorf("said %q, want the wait once", b.String())
	}
	readMemory = func() (localmodel.Machine, error) { return localmodel.Machine{Total: 100, Available: 5}, nil }
	lostMemoryWait = 20 * time.Millisecond
	if roomForOne(io.Discard) {
		t.Error("room with 5% free")
	}
	readMemory = func() (localmodel.Machine, error) { return localmodel.Machine{}, errors.New("no sysctl") }
	if !roomForOne(io.Discard) {
		t.Error("no room on a machine whose memory can't be read")
	}
}
