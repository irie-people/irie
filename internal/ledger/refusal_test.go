package ledger

import (
	"testing"
	"time"
)

// TestARefusalBeforeOutputIsBilledOnlyInItsCategories: a refusal before any
// output costs its input only in the categories billed then
// (billedRefusals), and nothing in any other or in none.
func TestARefusalBeforeOutputIsBilledOnlyInItsCategories(t *testing.T) {
	for cat, want := range map[string]bool{"bio": true, "frontier_llm": true, "reasoning_extraction": true, "other": false, "": false} {
		if got := RefusalBilled(cat, t0); got != want {
			t.Errorf("RefusalBilled(%q) = %v, want %v", cat, got, want)
		}
	}
	if !RefusalBilled("bio", t0.Add(365*24*time.Hour)) {
		t.Error("a billed category stopped being billed")
	}
}
