package main

import (
	"testing"

	"github.com/irie-hq/irie/internal/ipc"
)

func TestCloseTargetIsTheNamedSessionOrYourOwn(t *testing.T) {
	sessions := []ipc.Session{{ID: "s01a", Name: "warm-look"}, {ID: "s02b", Name: "fix-login"}}
	for _, c := range []struct {
		args      []string
		own, want string
	}{
		{nil, "s02b", "s02b"},
		{[]string{"warm-look"}, "s02b", "s01a"},
		{[]string{"Fix-Login"}, "", "s02b"},
		{[]string{"s01a"}, "", "s01a"},
		{[]string{"gone"}, "s02b", "gone"},
	} {
		if got, err := closeTarget(c.args, c.own, sessions); err != nil || got != c.want {
			t.Errorf("%v, own %q: %q, %v; want %q", c.args, c.own, got, err, c.want)
		}
	}
	if got, err := closeTarget(nil, "", sessions); err == nil {
		t.Errorf("no SESSION and no session of its own: %q, want an error", got)
	}
}
