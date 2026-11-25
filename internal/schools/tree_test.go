package schools

import (
	"math"
	"strings"
	"testing"
)

// walk calls f on n and every node under it, with its parent.
func walk(n, parent *Node, f func(n, parent *Node)) {
	f(n, parent)
	for _, p := range n.Parts {
		walk(p, n, f)
	}
	for _, p := range n.Also {
		walk(p, n, f)
	}
}

// TestThePies: every pie holds two slices or more, each slice a part of
// it: its id under its parent's, a size above nothing, an amount to read,
// and a source the page can name; no id twice; every text a node names is
// one of the texts.
func TestThePies(t *testing.T) {
	ids := map[string]bool{}
	for _, l := range Lenses() {
		if l.Root.ID != l.ID {
			t.Errorf("lens %s: its root is %s", l.ID, l.Root.ID)
		}
		walk(l.Root, nil, func(n, parent *Node) {
			if ids[n.ID] {
				t.Errorf("%s twice", n.ID)
			}
			ids[n.ID] = true
			if parent != nil && !strings.HasPrefix(n.ID, parent.ID+"/") {
				t.Errorf("%s isn't under %s", n.ID, parent.ID)
			}
			if strings.Contains(n.ID, "~") {
				t.Errorf("%s: ~ is the page's mark for a pie's rest", n.ID)
			}
			if n.Name == "" || n.Figure == "" {
				t.Errorf("%s: name %q, figure %q", n.ID, n.Name, n.Figure)
			}
			if parent != nil && !(n.Value > 0) {
				t.Errorf("%s: value %v", n.ID, n.Value)
			}
			if len(n.Parts) == 1 {
				t.Errorf("%s: a pie of one slice", n.ID)
			}
			if len(n.Parts) > 0 && (n.Unit == "" || n.Title == "") {
				t.Errorf("%s: a pie with no unit or title", n.ID)
			}
			if n.Source != "" {
				if _, ok := SourceByID(n.Source); !ok {
					t.Errorf("%s: no source %q", n.ID, n.Source)
				}
			}
			for _, id := range n.Texts {
				if _, ok := ByID(id); !ok {
					t.Errorf("%s: no text %q", n.ID, id)
				}
			}
		})
	}
}

// TestHowTheyBegan: the first pie is the 48 states of 1922 in Swift's six
// groups, the land groups sized by acres that add to their states', and
// each state's own pie to its lands in Table 5.
func TestHowTheyBegan(t *testing.T) {
	root := began()
	var states float64
	for _, p := range root.Parts {
		states += p.Value
	}
	if len(root.Parts) != 6 || states != 48 {
		t.Fatalf("%d slices, %v states", len(root.Parts), states)
	}
	byID := map[string]*Node{}
	walk(root, nil, func(n, _ *Node) { byID[n.ID] = n })
	sum := func(n *Node) float64 {
		s := 0.0
		for _, p := range n.Parts {
			s += p.Value
		}
		return s
	}
	for id, want := range map[string]float64{
		"began/section-16":                         11055565,
		"began/sections-16-36":                     43806496,
		"began/sections-2-16-32-36":                18293014,
		"began/section-16/east-north-central":      996320 + 668578 + 1021867 + 724266 + 982329,
		"began/sections-16-36/pacific":             5534293 + 3399360 + 2376391,
		"began/section-16/east-north-central/ohio": 1793805.93,
	} {
		n := byID[id]
		if n == nil {
			t.Errorf("no %s", id)
			continue
		}
		if math.Abs(sum(n)-want) > 0.005 {
			t.Errorf("%s: its slices add to %.2f, want %.2f", id, sum(n), want)
		}
	}
	// A pie never needs more slices than it has colours.
	walk(root, nil, func(n, _ *Node) {
		if len(n.Parts) > len(hueOrder) {
			t.Errorf("%s: %d slices", n.ID, len(n.Parts))
		}
	})
	// Ohio's school sections lead to the act that granted them.
	if o := byID["began/section-16/east-north-central/ohio"]; o == nil || len(o.Texts) == 0 || o.Texts[0] != "ohio-enabling-act" {
		t.Errorf("Ohio: %+v", o)
	}
	// The original states' divisions carry the texts made in them.
	ne := byID["began/original/new-england"]
	if ne == nil || !strings.Contains(strings.Join(ne.Texts, " "), "massachusetts-1647") {
		t.Errorf("New England: %+v", ne)
	}
}
