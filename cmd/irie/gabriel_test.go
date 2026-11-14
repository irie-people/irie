package main

import (
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

func TestGabrielSaysWhenHeBoostsAlways(t *testing.T) {
	report := func(r ipc.GabrielResult) string {
		var b strings.Builder
		if err := printGabriel(&b, r); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	calm := ipc.GabrielResult{On: true, Present: true, CPUs: 12, RunQueue: 3}
	if got := report(calm); !strings.Contains(got, "room to spare") || strings.Contains(got, "Boost:") {
		t.Errorf("calm, no boost:\n%s", got)
	}
	always := calm
	always.Always, always.Holding = true, true
	got := report(always)
	for _, want := range []string{"boosting, so what is out of your focus steps back", "Boost: always", "arch.gabriel.always", "boost off` ends it"} {
		if !strings.Contains(got, want) {
			t.Errorf("always: no %q in\n%s", want, got)
		}
	}
	// Always says it once, over a boost of an hour on as well.
	until := time.Now().Add(time.Hour)
	always.BoostUntil = &until
	if got := report(always); strings.Count(got, "Boost:") != 1 || !strings.Contains(got, "Boost: always") {
		t.Errorf("always, with a boost on too:\n%s", got)
	}
}

func TestGabrielNamesTheTabOnTop(t *testing.T) {
	report := func(r ipc.GabrielResult) string {
		var b strings.Builder
		if err := printGabriel(&b, r); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	tab := ipc.GabrielSession{ID: "s1", Name: "the tab"}
	told := ipc.GabrielSession{ID: "s2", Name: "told"}
	r := ipc.GabrielResult{On: true, Present: true, Busy: true, Holding: true, CPUs: 12, RunQueue: 20,
		Top: []ipc.GabrielSession{tab}, Focus: []ipc.GabrielSession{tab, told}}
	got := report(r)
	for _, want := range []string{"On top: the tab, the tab you are on; it never steps back", "In your focus: told\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("light: no %q in\n%s", want, got)
		}
	}
	// Holding nothing back, the tab is on top for a promotion.
	calm := r
	calm.Busy, calm.Holding = false, false
	if got := report(calm); !strings.Contains(got, "it never steps back, not even for a promotion") {
		t.Errorf("a promotion: no top line in\n%s", got)
	}
	r.TopBusy, r.TopCPU, r.Back = true, 230, []ipc.GabrielSession{told}
	got = report(r)
	for _, want := range []string{"it is at work (230% of a CPU now), so every other session but an install steps back for it", "Stepping back for the tab you are on: told"} {
		if !strings.Contains(got, want) {
			t.Errorf("at work: no %q in\n%s", want, got)
		}
	}
}
