package main

import (
	"slices"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

func TestMatchClosedNeedsEveryWord(t *testing.T) {
	closed := []ipc.ClosedSession{{ID: "s02b", Name: "fix-login"}, {ID: "s01a", Name: "warm-look"}}
	for _, c := range []struct {
		q    []string
		want int
	}{{nil, 2}, {[]string{"LOGIN"}, 1}, {[]string{"s01"}, 1}, {[]string{"look", "login"}, 0}, {[]string{"zzz"}, 0}} {
		if got := matchClosed(closed, c.q); len(got) != c.want {
			t.Errorf("%v: %d matches, want %d", c.q, len(got), c.want)
		}
	}
}

func TestLastLostIsTheLastRestartsOldestFirst(t *testing.T) {
	at := time.Date(2026, 11, 14, 10, 36, 0, 0, time.UTC)
	closed := []ipc.ClosedSession{
		{ID: "s3", Closed: at, Lost: true},
		{ID: "s2", Closed: at, Lost: true},
		{ID: "s9", Closed: at.Add(-time.Second)},
		{ID: "s1", Closed: at.Add(-time.Second), Lost: true},
		{ID: "s0", Closed: at.Add(-72 * time.Hour), Lost: true},
	}
	var got []string
	for _, c := range lastLost(closed) {
		got = append(got, c.ID)
	}
	if want := []string{"s1", "s2", "s3"}; !slices.Equal(got, want) {
		t.Errorf("lastLost = %v, want %v", got, want)
	}
	if got := lastLost(closed[2:3]); len(got) != 0 {
		t.Errorf("lastLost with none lost = %+v", got)
	}
}
