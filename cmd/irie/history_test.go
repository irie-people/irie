package main

import (
	"testing"

	"github.com/irie-hq/irie/internal/ipc"
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
