package schools

import (
	"math"
	"testing"
)

// TestTheCases: the 22 decisions are each named, cited and dated, in date
// order, in the four groups as the research and the Reports have them,
// and each gives a source the page can link; Alabama's certified vote
// gives its percentages, and the scholarship list covers the 50 states.
func TestTheCases(t *testing.T) {
	groups := map[string]int{}
	for i, c := range Cases {
		if c.Name == "" || c.Short == "" || c.Cite == "" || c.Vote == "" || c.Held == "" || len(c.Date) != 10 {
			t.Errorf("case %+v", c)
		}
		if i > 0 && c.Date < Cases[i-1].Date {
			t.Errorf("%s (%s) is before %s", c.Short, c.Date, Cases[i-1].Short)
		}
		if _, ok := SourceByID("case-" + slug(c.Short)); !ok {
			t.Errorf("%s: no source", c.Short)
		}
		groups[c.Group]++
	}
	if len(Cases) != 22 || groups[Ended] != 9 || groups[Protected] != 6 || groups[Money] != 6 || groups[NoRuling] != 1 {
		t.Errorf("%d cases, groups %v", len(Cases), groups)
	}
	if yes := float64(AmendmentYes) / float64(AmendmentYes+AmendmentNo); math.Abs(yes-0.6258) > 0.00005 {
		t.Errorf("Amendment 3: yes is %.4f of the votes; the certified share is 62.58%%", yes)
	}
	if CreditIn+CreditOut != 50 {
		t.Errorf("the scholarship credit's states add to %d", CreditIn+CreditOut)
	}
	root := church()
	var cases float64
	for _, p := range root.Parts {
		cases += p.Value
	}
	if cases != 22 {
		t.Errorf("the pie holds %v decisions", cases)
	}
}
