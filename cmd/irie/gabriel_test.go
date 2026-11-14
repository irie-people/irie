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
