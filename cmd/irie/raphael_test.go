package main

import (
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

func TestRaphaelsReportSaysTheRoomWhatHeClearedAndWhatHeTold(t *testing.T) {
	at := time.Date(2026, 11, 23, 14, 2, 0, 0, time.Local)
	r := ipc.RaphaelResult{On: true, Findings: []ipc.RaphaelFinding{}, Fine: []string{},
		Room: "22 GB of the disk is free. He keeps 26.8 GB free for swap, clearing caches by himself below it; under 10.7 GB, new heavy work waits.",
		Cleared: []ipc.RaphaelCleared{
			{At: at, ID: "go-build", What: "Go's build cache: the 900 oldest entries, untouched for 3 hours or more", Path: "~/Library/Caches/go-build", Size: 3_100_000_000},
			{At: at, ID: "pip", What: "pip's download cache", Path: "~/Library/Caches/pip", Size: 800_000_000, Error: "permission denied"},
		},
		Told: []ipc.Tell{{ID: "panic:x", At: at, Level: "warn", Text: "Your Mac crashed and restarted at 07:18 on 23 Nov: a made-up reason.", Lines: []string{"The swap had run out of disk: the disk was too full for it to grow."}}},
	}
	var b strings.Builder
	if err := printRaphael(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		r.Room,
		"Cleared by himself, to keep room for swap:",
		"  - 14:02 on 23 Nov Go's build cache: the 900 oldest entries, untouched for 3 hours or more: 3.1 GB, in ~/Library/Caches/go-build\n",
		"pip's download cache: 800 MB, in ~/Library/Caches/pip (it stopped short: permission denied)",
		"Told you:\n  - 14:02 on 23 Nov Your Mac crashed and restarted",
		"\n      The swap had run out of disk",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
}
