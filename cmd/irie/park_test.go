package main

import (
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

// Park and unpark find their session as close does, and their usage says
// which of them it is.
func TestParkAndUnparkTargetTheNamedSessionOrYourOwn(t *testing.T) {
	sessions := []ipc.Session{{ID: "s01a", Name: "warm-look", Parked: true}, {ID: "s02b", Name: "fix-login"}}
	for _, verb := range []string{"park", "unpark"} {
		if got, err := sessionTarget(verb, []string{"Warm-Look"}, "s02b", sessions); err != nil || got != "s01a" {
			t.Errorf("%s warm-look: %q, %v; want s01a", verb, got, err)
		}
		if got, err := sessionTarget(verb, nil, "s02b", sessions); err != nil || got != "s02b" {
			t.Errorf("%s with no SESSION: %q, %v; want its own, s02b", verb, got, err)
		}
		want := "usage: irie ctl " + verb + " [SESSION]: with no SESSION it " + verb + "s the session it runs in, and it runs in none"
		if _, err := sessionTarget(verb, nil, "", sessions); err == nil || err.Error() != want {
			t.Errorf("%s with no SESSION and no session of its own: %v, want %q", verb, err, want)
		}
	}
}

// The plain list draws the parked sessions last, as the sidebars do, under
// a heading of their own and in the list's order.
func TestListPrintsTheParkedSessionsLast(t *testing.T) {
	r := ipc.ListResult{Sessions: []ipc.Session{
		{ID: "s0k3j9x2m4a7q", State: ipc.StateWorking, Name: "api-fix", Detail: "Editing main.go"},
		{ID: "s1b4c8d2e6f0g", State: ipc.StateDone, Name: "docs", Detail: "Wrote the guide", Parked: true},
		{ID: "s2h5j7k9m1n3p", State: ipc.StateIdle, Name: "website-copy", Detail: "Waiting"},
		{ID: "s3q5r7t9v1w3x", State: ipc.StateDone, Name: "old-spike", Detail: "Ran the tests", Parked: true},
	}}
	var b strings.Builder
	if err := printList(&b, r); err != nil {
		t.Fatal(err)
	}
	want := `SESSION        STATE    NAME          DETAIL
s0k3j9x2m4a7q  working  api-fix       Editing main.go
s2h5j7k9m1n3p  idle     website-copy  Waiting

PARKED
SESSION        STATE  NAME       DETAIL
s1b4c8d2e6f0g  done   docs       Wrote the guide
s3q5r7t9v1w3x  done   old-spike  Ran the tests
`
	if b.String() != want {
		t.Errorf("printed\n%s\nwant\n%s", b.String(), want)
	}
	// With nothing parked there is no heading.
	r.Sessions = r.Sessions[:1]
	b.Reset()
	printList(&b, r)
	if strings.Contains(b.String(), "PARKED") {
		t.Errorf("with nothing parked:\n%s", b.String())
	}
}
