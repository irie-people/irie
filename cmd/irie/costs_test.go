package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

func TestTheCostsReportSaysWhatHasNoPrice(t *testing.T) {
	claude := 0.0
	r := ipc.CostsResult{
		Spans: []ipc.CostsSpan{
			{Name: "today", From: "2026-11-23", Total: ipc.CostsTally{Calls: 41, Free: 41, Words: "free"}},
			{Name: "week", From: "2026-11-17", Total: ipc.CostsTally{Calls: 1203, USD: 12.40, Unpriced: 3, Words: "$12.40, plus 3 calls with no price"},
				Purposes: []ipc.CostsPart{
					{Name: "turn", CostsTally: ipc.CostsTally{Calls: 1100, USD: 12.40, Unpriced: 3, Words: "$12.40, plus 3 calls with no price"}},
					{Name: "route", CostsTally: ipc.CostsTally{Calls: 41, Free: 41, Words: "free"}},
				},
				Models: []ipc.CostsPart{{Name: "claude-opus-5-5", CostsTally: ipc.CostsTally{Calls: 1100, USD: 12.40, Words: "$12.40"}}}},
			{Name: "month", From: "2026-10-25", Total: ipc.CostsTally{}},
		},
		Sessions: []ipc.CostsSession{{ID: "s38bvad9qgve0", Name: "fix-cart-total", Ledger: ipc.CostsTally{Calls: 1034, USD: 81.02, Words: "$81.02"}, Claude: &claude}},
	}
	var b bytes.Buffer
	if err := printCosts(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"today      41 calls: free",
		"this week  1203 calls: $12.40, plus 3 calls with no price",
		"30 days    no calls",
		"THIS WEEK, BY PURPOSE",
		"route",
		"fix-cart-total (closed)",
		"$81.02",
		"$0.00", // what Claude Code's status line said after a reopen
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
}
