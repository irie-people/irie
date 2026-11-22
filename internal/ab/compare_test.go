package ab

import "testing"

// Clear-cut cases only: where the line between a change and noise falls is
// Compare's own choice.

func TestCompareSeesAClearChange(t *testing.T) {
	gold := []float64{1.00, 1.02, 0.99, 1.01, 1.00, 0.98, 1.03, 1.00, 1.01, 0.99}
	faster := []float64{0.31, 0.30, 0.32, 0.31, 0.30, 0.33, 0.31, 0.30, 0.32, 0.31}
	if c := Compare(gold, faster); c != Lower {
		t.Errorf("about 70%% less on every run: got %q, want %q", c, Lower)
	}
	if c := Compare(faster, gold); c != Higher {
		t.Errorf("about three times as much on every run: got %q, want %q", c, Higher)
	}
}

func TestCompareCallsNoiseNoChange(t *testing.T) {
	gold := []float64{1.00, 1.10, 0.92, 1.05, 0.97, 1.08, 0.95, 1.02, 0.99, 1.04}
	again := []float64{1.01, 0.94, 1.07, 0.98, 1.09, 0.96, 1.03, 1.00, 0.93, 1.05}
	if c := Compare(gold, again); c != Same {
		t.Errorf("medians half a percent apart in runs that move 18%%: got %q, want %q", c, Same)
	}
	if c := Compare(gold, gold); c != Same {
		t.Errorf("the same samples: got %q, want %q", c, Same)
	}
}

func TestCompareLetsAStrayRunPass(t *testing.T) {
	gold := []float64{0.99, 1.00, 1.01, 1.00, 0.98, 1.02, 1.00, 3.00, 1.01, 0.99}
	faster := []float64{0.80, 0.81, 0.79, 0.80, 0.82, 0.78, 0.80, 0.81, 0.79, 0.80}
	if c := Compare(gold, faster); c != Lower {
		t.Errorf("20%% less with one slow gold run: got %q, want %q", c, Lower)
	}
}

func TestCompareWantsDoubleFromTooFewRuns(t *testing.T) {
	for _, c := range []struct {
		gold, new float64
		want      Change
	}{{1, 0.6, Same}, {1, 0.4, Lower}, {1, 1.8, Same}, {1, 2.5, Higher}} {
		if got := Compare([]float64{c.gold}, []float64{c.new}); got != c.want {
			t.Errorf("one run, %v against %v: got %q, want %q", c.new, c.gold, got, c.want)
		}
	}
}

func TestCompareCallsATinyChangeNoChange(t *testing.T) {
	gold := []float64{1.000, 1.000, 1.000, 1.000, 1.000}
	new := []float64{0.995, 0.995, 0.995, 0.995, 0.995}
	if c := Compare(gold, new); c != Same {
		t.Errorf("half a percent, however steady: got %q, want %q", c, Same)
	}
}
