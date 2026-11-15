package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

func TestTheRebuildsReportRanksTodayAndTheWeek(t *testing.T) {
	today := time.Date(2026, 11, 14, 0, 0, 0, 0, time.Local)
	r := ipc.RebuildsResult{On: true,
		Today: ipc.RebuildsSpan{From: today, Count: 3, Tokens: 900_000, USD: 7.20,
			Sessions: []ipc.RebuildsPart{
				{ID: "s38bvad9qgve0", Name: "fix-cart-total", Open: true, Count: 1, Tokens: 500_000, USD: 4},
				{ID: "s38bvaf2kq7m1", Name: "migrate-db", Count: 2, Tokens: 400_000, USD: 3.20},
			},
			Causes: []ipc.RebuildsPart{
				{Name: "you", Words: "your message after a long idle", Count: 2, Tokens: 600_000, USD: 4.80},
				{Name: "trod-thaw", Words: "the trod bot's thaw", Count: 1, Tokens: 300_000, USD: 2.40},
			}},
		Week: ipc.RebuildsSpan{From: today.AddDate(0, 0, -6), Count: 4, Tokens: 1_500_000, USD: 12,
			Sessions: []ipc.RebuildsPart{{ID: "s38bvad9qgve0", Name: "fix-cart-total", Open: true, Count: 2, Tokens: 1_100_000, USD: 8.80}},
			Causes:   []ipc.RebuildsPart{{Name: "you", Words: "your message after a long idle", Count: 3, Tokens: 1_200_000, USD: 9.60}}},
	}
	var b bytes.Buffer
	if err := printRebuilds(&b, r); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"Prompt-cache rebuilds at list price: today 3, $7.20; this week 4, $12.00.",
		"TODAY, WHAT WOKE THEM           REBUILDS  TOKENS  COST\n",
		"your message after a long idle  2         600k    $4.80\n",
		"the trod bot's thaw             1         300k    $2.40\nTODAY, BY SESSION",
		"fix-cart-total                  1         500k    $4.00\n",
		"migrate-db (closed)             2         400k    $3.20\n",
		"THIS WEEK, WHAT WOKE THEM",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	b.Reset()
	printRebuilds(&b, ipc.RebuildsResult{On: true})
	if got := b.String(); got != "Prompt-cache rebuilds at list price: today none; this week none.\n" {
		t.Errorf("none: %q", got)
	}
	b.Reset()
	printRebuilds(&b, ipc.RebuildsResult{})
	if !strings.Contains(b.String(), "off (usage.rebuilds)") {
		t.Errorf("off: %q", b.String())
	}
}

func TestPsSaysTodaysRebuilds(t *testing.T) {
	now := time.Now()
	usd := 9.5
	rows := []ipc.Session{
		{ID: "s38bvad9qgve0", Name: "fix-cart-total", State: ipc.StateDone, Created: now.Add(-time.Hour), CostUSD: &usd,
			Rebuilds: &ipc.Rebuilds{Today: 3, TodayUSD: 4.1, Week: 5, WeekUSD: 7.3, Words: "$4.10 today: woke cold 3 times"}},
		{ID: "s38bvaf2kq7m1", Name: "migrate-db", State: ipc.StateWorking, Created: now.Add(-time.Hour)},
	}
	var b bytes.Buffer
	printProcs(&b, procsOf(rows, now))
	got := b.String()
	if !strings.Contains(got, "caches rebuilt 3× today, $4.10") || !strings.Contains(got, "REBUILT") || !strings.Contains(got, "3 $4.10") {
		t.Errorf("ps:\n%s", got)
	}
}
