package schools

import (
	"math"
	"strconv"
	"strings"
)

// The pies the page draws: each Lens starts from one, and a slice with
// parts is a pie of its own one click on. The figures are figures.go's,
// and the words are ours, each saying where in its source it comes from.

// Node is a slice of a pie and, when it has parts, a pie of its own.
type Node struct {
	// ID is its place among the pies, its parents' ids and its own slug
	// joined by slashes, which the page's address carries.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Value is its size in its parent's pie, counted in the parent's Unit.
	Value float64 `json:"value"`
	// Figure is its own amount as it reads: "12 states", "724,266 acres".
	Figure string `json:"figure"`
	// Whole is its pie's total as it reads, where that isn't its Figure:
	// a state's slice is its school sections, its pie all its lands.
	Whole string `json:"whole,omitempty"`
	// Unit is what its parts' values count, in the plural: "states",
	// "acres", "dollars". Empty without parts.
	Unit string `json:"unit,omitempty"`
	// Title heads its pie, when it has parts.
	Title string `json:"title,omitempty"`
	// About says what it is, in our words.
	About string `json:"about,omitempty"`
	// Source names the source of its figures, and Where the place in it;
	// More are other sources its words draw on, by id.
	Source string   `json:"source,omitempty"`
	Where  string   `json:"where,omitempty"`
	More   []string `json:"more,omitempty"`
	// Note is a caveat on its figures.
	Note string `json:"note,omitempty"`
	// Texts are the texts behind it, by id.
	Texts []string `json:"texts,omitempty"`
	// List names what it holds that has no slice of its own.
	List  []string `json:"list,omitempty"`
	Parts []*Node  `json:"parts,omitempty"`
	// Also are pies its card opens besides its own: another way to count
	// the same thing, such as who makes the buses beside who runs them.
	Also []*Node `json:"also,omitempty"`
}

// Lens is one of the pies the page starts from: its id, the address's
// first step, and the name of the choice above the pie.
type Lens struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Root *Node  `json:"root"`
}

// Lenses are the pies the page starts from, the first the one it opens on.
func Lenses() []Lens {
	return []Lens{
		{ID: "began", Name: "How they began", Root: began()},
		{ID: "buildings", Name: "The buildings", Root: buildings()},
		{ID: "buses", Name: "The buses", Root: buses()},
		{ID: "church", Name: "Church and state", Root: church()},
	}
}

// began is the 48 states of 1922 by the land Congress gave them for
// their schools (Swift, Tables 2, 3 and 5), each group of many states
// opening first into the Census Bureau's divisions.
func began() *Node {
	root := &Node{
		ID: "began", Name: "The 48 states of 1922", Figure: "48 states", Unit: "states",
		Title:  "How the 48 states got land for their schools",
		About:  "By 1921, as the Bureau of Education counted it, Congress had granted the states 73 million acres to keep their public schools: school sections in every township of each state made out of the nation's public lands. Section 16 went to the states admitted before California, sections 16 and 36 to California and the states after it, and sections 2, 16, 32 and 36 to Utah, Arizona and New Mexico. The thirteen original states, and five more that held no public lands, received none, and Tennessee set its school land aside itself. Alaska's sections were reserved but not yet granted, and Hawaii was a territory.",
		Source: "swift-1922", Where: "Tables 2 and 3, pages 9 and 10",
	}
	original := withoutLand(Original, "original", "The thirteen original states",
		"The thirteen states that made the Union contained none of the nation's public lands, so Congress granted them none for schools; in 1821 Maryland asked for an equal share for the states left out, others joined it, and there the matter ended. Their schools rest on their own laws: Massachusetts Bay's orders of 1642 and 1647 that every town see its children taught, the first state constitutions that called for schools, and the school laws that followed.")
	later := withoutLand(Later, "later", "Five states admitted later",
		"Vermont, Kentucky, Maine and West Virginia, like the original thirteen, contained no public lands, and the United States held no land in Texas, which kept the school lands its Republic had set aside in every county. So none of the five received a federal grant for schools.")
	root.Parts = append(root.Parts,
		original, later,
		&Node{
			ID: "began/tennessee", Name: "Tennessee", Value: 1, Figure: "1 state, about " + acres(TennesseeAcres),
			About:  "Admitted in 1796, Tennessee received a grant of public lands only in 1806, and by its own act set aside 640 acres in each six-mile square for the use of schools: only about 24,000 acres in all, where the federal rule would have given it over 700,000. Those lands are never counted among the federal school grants.",
			Source: "swift-1922", Where: "page 9, and Table 5's note 3",
		},
		landGroup(Sixteen, "section-16", "Section 16",
			"Section 16 of every township",
			"Each state admitted from the public lands before California received section 16 of every township for its schools: the lot the Land Ordinance of 1785 reserved for them in every township, granted first to Ohio in 1802.",
			[]string{"land-ordinance", "northwest", "ohio-company", "ohio-enabling-act"}),
		landGroup(SixteenThirtySix, "sections-16-36", "Sections 16 and 36",
			"Sections 16 and 36 of every township",
			"California and the states admitted after it received two sections of every township for their schools, 16 and 36, all but Utah, Arizona and New Mexico. Oklahoma was a case of its own: Congress could reserve no school land in Indian Territory, whose lands belonged to the members of Indian tribes, so when the two were admitted as one state in 1907 it gave $5,000,000 to the state's school fund in place of the usual grant there.",
			nil),
		landGroup(FourSections, "sections-2-16-32-36", "Sections 2, 16, 32 and 36",
			"Sections 2, 16, 32 and 36 of every township",
			"Utah, Arizona and New Mexico received four sections of every township for their schools, sections 2 and 32 as well as 16 and 36.",
			nil),
	)
	return root
}

// laterAbout is what Swift says of each state admitted later without
// public lands (page 8).
var laterAbout = map[string]string{
	"Texas": "Admitted in 1845. The United States held no land in Texas, so Congress could make it no grant; Texas kept the school lands its Republic had set aside in every county, 13,284 acres a county from 1839 and 17,712 from 1840.",
}

// withoutLand is the states of Table 2 that received no federal land for
// schools, as a pie of the Census Bureau's divisions by how many states,
// each division giving its states and the texts from them.
func withoutLand(s Sections, id, name, about string) *Node {
	n := &Node{ID: "began/" + id, Name: name, Unit: "states", About: about,
		Title: name + ", by the Census Bureau's divisions", Source: "swift-1922", Where: "Table 2 and page 8"}
	byDiv := map[string][]Grant{}
	var order []string
	count := 0
	for _, g := range Grants {
		if g.Sections != s {
			continue
		}
		count++
		d := DivisionOf(g.State)
		if _, ok := byDiv[d]; !ok {
			order = append(order, d)
		}
		byDiv[d] = append(byDiv[d], g)
	}
	n.Value, n.Figure = float64(count), states(count)
	for _, d := range order {
		gs := byDiv[d]
		var names, texts []string
		for _, g := range gs {
			label := g.State
			if g.Admitted > 0 {
				label += " (" + strconv.Itoa(g.Admitted) + ")"
			}
			names = append(names, label)
			texts = append(texts, textsFrom(g.State)...)
		}
		if len(gs) == 1 {
			g := gs[0]
			a := laterAbout[g.State]
			if a == "" {
				a = "Admitted in " + strconv.Itoa(g.Admitted) + ". Like the original thirteen, it contained no public lands, so it received no federal grant for schools."
			}
			n.Parts = append(n.Parts, &Node{ID: n.ID + "/" + slug(g.State), Name: g.State, Value: 1, Figure: "1 state",
				About: a, Source: "swift-1922", Where: "Table 2 and page 8", Texts: texts})
			continue
		}
		n.Parts = append(n.Parts, &Node{ID: n.ID + "/" + slug(d), Name: d, Value: float64(len(gs)), Figure: states(len(gs)),
			About:  strings.Join(statesOnly(gs), ", ") + ": in the Census Bureau's " + d + " division.",
			Source: "census-divisions", List: names, Texts: texts})
	}
	sortParts(n.Parts)
	return n
}

func statesOnly(gs []Grant) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.State)
	}
	return out
}

// textsFrom are the texts a state made about how its schools began, by id.
func textsFrom(state string) []string {
	var out []string
	for _, d := range Documents {
		if d.Place == state && d.Lens == "began" {
			out = append(out, d.ID)
		}
	}
	return out
}

// landGroup is the states whose schools received the sections named, as a
// pie by the acres of their school sections: by the Census Bureau's
// divisions where it holds more states than a pie has room for, each
// division a pie of its states, the largest first.
func landGroup(s Sections, id, name, title, about string, texts []string) *Node {
	n := &Node{ID: "began/" + id, Name: name, Unit: "acres", Title: title, About: about,
		Source: "swift-1922", Where: "Table 3, page 10, with Illinois from Table 5", Texts: texts}
	var sum float64
	var gs []Grant
	for _, g := range Grants {
		if g.Sections == s {
			gs = append(gs, g)
			sum += g.Township
		}
	}
	n.Value = float64(len(gs))
	n.Figure = states(len(gs)) + ", " + acres(sum)
	if len(gs) <= 6 {
		for _, g := range gs {
			n.Parts = append(n.Parts, state(n.ID, g))
		}
		sortParts(n.Parts)
		return n
	}
	n.Title += ", by the Census Bureau's divisions"
	byDiv := map[string][]Grant{}
	var order []string
	for _, g := range gs {
		d := DivisionOf(g.State)
		if _, ok := byDiv[d]; !ok {
			order = append(order, d)
		}
		byDiv[d] = append(byDiv[d], g)
	}
	for _, d := range order {
		ds := byDiv[d]
		if len(ds) == 1 {
			n.Parts = append(n.Parts, state(n.ID, ds[0]))
			continue
		}
		dn := &Node{ID: n.ID + "/" + slug(d), Name: d, Unit: "acres", Title: name + " in the " + d + " division",
			About:  strings.Join(statesOnly(ds), ", ") + ": the states of the Census Bureau's " + d + " division that received " + strings.ToLower(name[:1]) + name[1:] + ".",
			Source: "swift-1922", Where: "Table 3, page 10, with Illinois from Table 5"}
		var dsum float64
		for _, g := range ds {
			dn.Parts = append(dn.Parts, state(dn.ID, g))
			dsum += g.Township
		}
		sortParts(dn.Parts)
		dn.Value, dn.Figure = dsum, states(len(ds))+", "+acres(dsum)
		n.Parts = append(n.Parts, dn)
	}
	sortParts(n.Parts)
	return n
}

// state is one land state: a slice by its school sections, and a pie of
// the lands it could use for schools, by kind, where it had more than one.
func state(parent string, g Grant) *Node {
	id := parent + "/" + slug(g.State)
	n := &Node{ID: id, Name: g.State, Value: g.Township, Figure: acres(g.Township) + " of school sections",
		Source: "swift-1922", Where: "Table 5, page 15"}
	kinds := []struct {
		slug, name string
		acres      float64
		about      string
	}{
		{"sections", "School sections", g.Township, "Granted for its common schools, in every township."},
		{"salt", "Salt lands", g.Salt, "Granted with the state's salt springs, from 1802 on. Arkansas, Indiana, Missouri and Ohio gave what they brought to their common-school funds."},
		{"improvement", "Internal improvement lands", g.Improvement, "Granted under the act of 1841 for roads, canals and the like, 500,000 acres to most of the nineteen states that received them. Nothing in the act suggested schools, but some states gave the lands to them."},
		{"swamp", "Swamp lands", g.Swamp, "Granted under the swamp land act of 1850 for the states to drain and reclaim; some states gave them to their schools."},
	}
	var parts []*Node
	for _, k := range kinds {
		if k.acres > 0 {
			parts = append(parts, &Node{ID: id + "/" + k.slug, Name: k.name, Value: k.acres, Figure: acres(k.acres), About: k.about,
				Source: "swift-1922", Where: "Table 5, page 15"})
		}
	}
	n.Texts = landTexts[g.State]
	if len(parts) < 2 {
		n.About = g.State + " received " + acres(g.Township) + " of school sections, and no other federal lands it could use for schools."
		return n
	}
	n.Unit, n.Parts = "acres", parts
	n.Whole = acres(g.Total) + " it could use for schools"
	n.Title = "The lands " + g.State + " could use for its schools"
	n.About = g.State + " received " + acres(g.Township) + " of school sections; with the other federal lands it could use for schools, " + acres(g.Total) + " in all."
	n.Note = "The other lands were granted for other ends, and in some states the grant's terms kept them from schools: the Bureau counted them as lands some states used for schools, not lands every state could."
	return n
}

// landTexts are the texts that granted a land state its school sections,
// by state.
var landTexts = map[string][]string{
	"Ohio": {"ohio-enabling-act"},
}

// sortParts puts the largest first, keeping the order of equals.
func sortParts(ps []*Node) {
	for i := 1; i < len(ps); i++ {
		for j := i; j > 0 && ps[j].Value > ps[j-1].Value; j-- {
			ps[j], ps[j-1] = ps[j-1], ps[j]
		}
	}
}

// states says how many states: "1 state", "13 states".
func states(n int) string {
	if n == 1 {
		return "1 state"
	}
	return strconv.Itoa(n) + " states"
}

// acres writes an area in whole acres, with commas: "724,266 acres".
func acres(a float64) string {
	return commas(int64(math.Round(a))) + " acres"
}

// commas writes n with a comma between each three digits.
func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// slug is a name as an address's step: "New Mexico" is "new-mexico".
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(name) {
		switch {
		case c >= 'a' && c <= 'z' || c >= '0' && c <= '9':
			b.WriteRune(c)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
