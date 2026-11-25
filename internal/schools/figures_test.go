package schools

import (
	"math"
	"testing"
)

// cents is an amount in acres to the hundredth, as Swift's Table 5 prints
// them, so sums compare exactly.
func cents(a float64) int64 { return int64(math.Round(a * 100)) }

// TestTheGrantsAddUp: the acres are Swift's, copied right, because they
// add up to the totals he prints: each state's row to its total, each
// column of Table 5 and Table 3's school sections to theirs, and Table 3's
// groups and Table 2's states to the 48 of 1922.
func TestTheGrantsAddUp(t *testing.T) {
	var township, salt, improvement, swamp, total int64
	count := map[Sections]int{}
	seen := map[string]bool{}
	for _, g := range Grants {
		if seen[g.State] {
			t.Errorf("%s twice", g.State)
		}
		seen[g.State] = true
		count[g.Sections]++
		row := cents(g.Township) + cents(g.Salt) + cents(g.Improvement) + cents(g.Swamp)
		switch g.Sections {
		case Original, Later, OwnAct:
			if row != 0 || g.Total != 0 {
				t.Errorf("%s: %d hundredths of an acre, but Table 5 gives it none", g.State, row)
			}
		default:
			if row != cents(g.Total) {
				t.Errorf("%s: its lands add to %.2f acres, but Table 5 prints %.2f", g.State, float64(row)/100, g.Total)
			}
		}
		if DivisionOf(g.State) == "" {
			t.Errorf("%s is in no division", g.State)
		}
		township += cents(g.Township)
		salt += cents(g.Salt)
		improvement += cents(g.Improvement)
		swamp += cents(g.Swamp)
		total += cents(g.Total)
	}
	for _, c := range []struct {
		name      string
		got, want int64
	}{
		{"school sections", township, cents(TownshipTotal)},
		{"salt lands", salt, cents(SaltTotal)},
		{"internal improvement lands", improvement, cents(ImprovementTotal)},
		{"swamp lands", swamp, cents(SwampTotal)},
		{"all the lands", total, cents(GrandTotal)},
	} {
		if c.got != c.want {
			t.Errorf("%s add to %.2f acres; Swift prints %.2f", c.name, float64(c.got)/100, float64(c.want)/100)
		}
	}
	for s, want := range map[Sections]int{Original: 13, Later: 5, OwnAct: 1, Sixteen: 12, SixteenThirtySix: 14, FourSections: 3} {
		if count[s] != want {
			t.Errorf("sections %d: %d states, want %d", s, count[s], want)
		}
	}
	if len(Grants) != 48 {
		t.Errorf("%d states, want the 48 of 1922", len(Grants))
	}
	// Table 3's groups, as Swift's own sums would have them: section 16's
	// with Illinois's 996,320 (Table 5), not the 966,320 Table 3 prints.
	groups := map[Sections]int64{}
	for _, g := range Grants {
		groups[g.Sections] += cents(g.Township)
	}
	for s, want := range map[Sections]float64{Sixteen: 11055565, SixteenThirtySix: 43806496, FourSections: 18293014} {
		if groups[s] != cents(want) {
			t.Errorf("sections %d: %.0f acres, want %.0f", s, float64(groups[s])/100, want)
		}
	}
	if cents(TownshipTotal+AlaskaReserved) != cents(94164284) {
		t.Errorf("Table 3's grand total with Alaska's reserve is 94,164,284 acres")
	}
}

// TestTheDivisions: the Census Bureau's nine divisions hold every state
// once, and the District of Columbia.
func TestTheDivisions(t *testing.T) {
	seen := map[string]string{}
	n := 0
	for _, d := range Divisions {
		for _, s := range d.States {
			if was, ok := seen[s]; ok {
				t.Errorf("%s in %s and %s", s, was, d.Name)
			}
			seen[s] = d.Name
			n++
		}
	}
	if len(Divisions) != 9 || n != 51 {
		t.Errorf("%d divisions, %d places; want 9 and the 50 states and the District", len(Divisions), n)
	}
}

// TestEverySourceIsNamed: every figure's source is one Sources names, read
// on a date, with a link.
func TestEverySourceIsNamed(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range AllSources() {
		if seen[s.ID] {
			t.Errorf("source %s twice", s.ID)
		}
		seen[s.ID] = true
		if s.ID == "" || s.Title == "" || s.By == "" || s.URL == "" || len(s.Read) != len("2006-01-02") {
			t.Errorf("source %+v", s)
		}
	}
}

// TestTodaysFiguresAddUp: today's figures agree with their sources' own
// totals: building and upkeep spent and short add to the report's need
// (to the dollar, as its tables round), the energy service companies'
// markets to their table's total, Blue Bird's buses by fuel to its sales,
// and the buses by owner to the 31 states' count.
func TestTodaysFiguresAddUp(t *testing.T) {
	if d := BuildingSpent + BuildingShort - BuildingNeed; math.Abs(d) > 1 {
		t.Errorf("building: spent and short miss the need by $%.0f", d)
	}
	if d := UpkeepSpent + UpkeepShort - UpkeepNeed; math.Abs(d) > 1 {
		t.Errorf("upkeep: spent and short miss the need by $%.0f", d)
	}
	var esco float64
	for _, e := range ESCOs {
		esco += e.Millions
	}
	if math.Abs(esco-ESCOTotal) > 0.5 {
		t.Errorf("the markets add to $%.0f million, the table's total is $%.0f million", esco, ESCOTotal)
	}
	if !(BlueBirdElectric < BlueBirdAlt && BlueBirdAlt < BlueBirdSold && BlueBirdSold < TypeCDRegistered) {
		t.Error("Blue Bird's buses don't nest: electric within alternative within all, all within the industry's count")
	}
	if !(OwnedByContractors < OwnedInAll) {
		t.Error("contractors own more buses than there are")
	}
	if got := billions(BuildingSpent); got != "$82.3 billion" {
		t.Errorf("billions = %q", got)
	}
	if got := billions(BuildingNeed); got != "$138 billion" {
		t.Errorf("billions = %q", got)
	}
	if got := billions(3220e6); got != "$3.22 billion" {
		t.Errorf("billions = %q", got)
	}
}
