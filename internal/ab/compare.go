package ab

import (
	"math"
	"slices"
)

// Change is how new's runs stand against gold's on one measure where less
// is better: time, CPU or peak memory.
type Change string

const (
	Lower  Change = "lower"  // new clearly takes less
	Higher Change = "higher" // new clearly takes more
	Same   Change = "same"   // no clear difference: noise could explain it
)

// Compare says whether new's samples are clearly lower or higher than
// gold's, or not clearly different. Each slice holds one sample per timed
// run, in run order; both have the same length, at least 1, and every
// sample is above 0.
//
// A difference counts when the medians are more than three standard errors
// apart, and at least 1%: the medians move little with a stray run, and
// more runs narrow the error, so they can show a smaller change for real.
// With fewer than 3 runs the noise cannot be measured, and only a change of
// more than double counts.
func Compare(gold, new []float64) Change {
	gm, gse := center(gold)
	nm, nse := center(new)
	d := nm - gm
	if len(gold) < 3 || len(new) < 3 {
		switch {
		case nm*2 < gm:
			return Lower
		case nm > gm*2:
			return Higher
		}
		return Same
	}
	if math.Abs(d) < gm/100 || math.Abs(d) <= 3*math.Hypot(gse, nse) {
		return Same
	}
	if d < 0 {
		return Lower
	}
	return Higher
}

// center is some samples' median and its standard error, √(π/2)·σ/√n for
// normal samples, with σ taken from the spread of the middle half of them
// (1.349σ in a normal distribution), so that a stray run (Spotlight waking,
// a cold cache) moves it little.
func center(v []float64) (median, se float64) {
	s := slices.Sorted(slices.Values(v))
	n := len(s)
	sigma := (s[n-1-n/4] - s[n/4]) / 1.349
	return summarize(s).Median, math.Sqrt(math.Pi/2) * sigma / math.Sqrt(float64(n))
}
