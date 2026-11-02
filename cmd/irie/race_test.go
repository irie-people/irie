package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

func TestRaceRanksDoneThenPassedThenFastestThenCheapest(t *testing.T) {
	cost := func(f float64) *float64 { return &f }
	race := &Race{ID: "20261101-120000", Task: "fix the parser", Check: "go test ./...", Racers: []*Racer{
		{Profile: "slow-pass", State: ipc.StateDone, Seconds: 90, Check: &CheckResult{Pass: true}, CostUSD: cost(0.10)},
		{Profile: "asks", State: ipc.StateNeedsYou, Seconds: 10, Check: &CheckResult{Pass: true}},
		{Profile: "fast-fail", State: ipc.StateDone, Seconds: 20, Check: &CheckResult{Pass: false}},
		{Profile: "fast-pass-dear", State: ipc.StateDone, Seconds: 30, Check: &CheckResult{Pass: true}, CostUSD: cost(2)},
		{Profile: "fast-pass-cheap", State: ipc.StateDone, Seconds: 30, Check: &CheckResult{Pass: true}, CostUSD: cost(0.5)},
		{Profile: "never", State: "not started"},
	}}
	rank(race)
	var got []string
	for _, r := range race.Racers {
		got = append(got, r.Profile)
	}
	if want := "fast-pass-cheap fast-pass-dear slow-pass asks fast-fail never"; strings.Join(got, " ") != want {
		t.Fatalf("ranked %v, want %s", got, want)
	}
	dir := t.TempDir()
	if err := saveRace(dir, race); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(dir, race.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "| 1 | +fast-pass-cheap |  | done | 30 s | - | - | $0.50 | pass |") {
		t.Fatalf("the page:\n%s", md)
	}
	if _, err := os.Stat(filepath.Join(dir, race.ID+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestARacerIsDoneOnlyOnceSeenAtWork(t *testing.T) {
	for _, c := range []struct {
		state  string
		worked bool
		want   bool
	}{
		{ipc.StateIdle, false, false}, // a resumed fork before its prompt
		{ipc.StateIdle, true, true},
		{ipc.StateDone, true, true},
		{ipc.StateWorking, true, false},
		{ipc.StateStarting, false, false},
		{ipc.StateNeedsYou, false, true},
		{ipc.StateExited, false, true},
	} {
		if got := over(c.state, c.worked); got != c.want {
			t.Errorf("over(%s, %v) = %v", c.state, c.worked, got)
		}
	}
}

func TestPsLetters(t *testing.T) {
	now := ipc.Session{State: ipc.StateWorking}
	if stateLetter(now) != "R" {
		t.Fatal(stateLetter(now))
	}
	frozen := ipc.Session{State: ipc.StateWorking, Frozen: &[]ipc.Session{{}}[0].Created}
	if stateLetter(frozen) != "T" {
		t.Fatal(stateLetter(frozen))
	}
	if count(1500) != "2k" || count(3_400_000) != "3.4M" || count(0) != "-" {
		t.Fatal(count(1500), count(3_400_000))
	}
}
