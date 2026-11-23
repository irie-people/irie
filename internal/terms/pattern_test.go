package terms

import (
	"regexp"
	"testing"
)

// TestEveryPatternCompiles: the package's patterns compile on their first
// use, so a bad one would fail only then; this compiles each, as starting
// the package once did.
func TestEveryPatternCompiles(t *testing.T) {
	if len(made) < 200 {
		t.Fatalf("%d patterns made as the package started; the catalog alone has hundreds", len(made))
	}
	for _, p := range made {
		if _, err := regexp.Compile(p.src); err != nil {
			t.Errorf("%.60s: %v", p.src, err)
		}
	}
	n := len(made)
	re("made later")
	if len(made) != n {
		t.Error("a pattern made after the start is kept for good")
	}
}
