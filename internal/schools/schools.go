// Package schools holds Common Schools (docs/proposals/common-schools.md,
// DESIGN §4.17), the Charters plugin's second part: how public schools
// began in the United States, and who builds and keeps them up today, as
// pie charts that open into smaller ones. Its texts are kept as the
// charters are, word for word from the bodies that keep them, each
// checked against another copy; they are the common-schools skill's own
// files, read with internal/founding's parser, and a text the two share,
// such as the Northwest Ordinance, is read from the charters' files. Its
// figures come from public sources, each named beside them (figures.go),
// and the page draws them as pies (tree.go, page.go).
package schools

import (
	"fmt"
	"sync"

	"github.com/irie-people/irie/internal/founding"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/skills"
)

// Skill is the skill whose files are the texts.
const Skill = "common-schools"

// Document is one of the texts: its id, which the links and the page name
// it by, the skill and the file it is in, its short name, the date it is
// of, its kind, the place that made it, the starting pie whose cards give
// it, and the passages the page lights.
type Document struct {
	ID string
	// Skill is the skill whose file it is: this one's when empty, or the
	// charters' (founding.Skill) for a text the two share.
	Skill string
	File  string
	Short string
	// When is the date the text is of, as RFC 3339 writes a date, or a
	// year alone where the print gives no day.
	When string
	// Kind is law, constitution, ordinance, resolution, statute, letter,
	// covenant, declaration or amendments.
	Kind string
	// Place is the colony, state or union that made it.
	Place string
	// Lens is the starting pie whose cards give it: began, or church.
	Lens string
	// Lit are the passages the page lights, about schools or about the
	// pie it is in, each word for word in the file: the tests check it.
	Lit []string
}

// skill is the skill the document's file is in.
func (d Document) skill() string {
	if d.Skill == "" {
		return Skill
	}
	return d.Skill
}

// Documents are the texts, by date, the oldest first.
var Documents = []Document{
	{ID: "mayflower", Skill: founding.Skill, File: "mayflower-compact.md", Short: "Mayflower Compact", When: "1620-11-11", Kind: "covenant", Place: "Plymouth Colony", Lens: "church",
		Lit: []string{"covenant and combine ourselves together into a civil Body Politick"}},
	{ID: "massachusetts-1642", File: "massachusetts-school-law-1642.md", Short: "Massachusetts, 1642", When: "1642-06-14", Kind: "law", Place: "Massachusetts", Lens: "began",
		Lit: []string{"their ability to read & understand the principles of religion & the capitall lawes of this country"}},
	{ID: "massachusetts-1647", File: "massachusetts-school-law-1647.md", Short: "Massachusetts, 1647", When: "1647-11-11", Kind: "law", Place: "Massachusetts", Lens: "began",
		Lit: []string{"shall then forthwth appoint one wthin their towne to teach all such children as shall resort to him to write & reade", "they shall set up a gram̄er schoole"}},
	{ID: "declaration", Skill: founding.Skill, File: "declaration.md", Short: "Declaration of Independence", When: "1776-07-04", Kind: "declaration", Place: "United States", Lens: "church",
		Lit: []string{"endowed by their Creator with certain unalienable Rights"}},
	{ID: "pennsylvania-1776", File: "pennsylvania-1776-section-44.md", Short: "Pennsylvania, 1776", When: "1776-09-28", Kind: "constitution", Place: "Pennsylvania", Lens: "began",
		Lit: []string{"A school or schools shall be established in each county by the legislature, for the convenient instruction of youth"}},
	{ID: "north-carolina-1776", File: "north-carolina-1776-article-41.md", Short: "North Carolina, 1776", When: "1776-12-18", Kind: "constitution", Place: "North Carolina", Lens: "began",
		Lit: []string{"That a school or schools shall be established by the Legislature, for the convenient instruction of youth"}},
	{ID: "georgia-1777", File: "georgia-1777-article-54.md", Short: "Georgia, 1777", When: "1777-02-05", Kind: "constitution", Place: "Georgia", Lens: "began",
		Lit: []string{"Schools shall be erected in each county, and supported at the general expense of the State"}},
	{ID: "vermont-1777", File: "vermont-1777-section-40.md", Short: "Vermont, 1777", When: "1777-07-08", Kind: "constitution", Place: "Vermont", Lens: "began",
		Lit: []string{"A school or schools shall be established in each town, by the legislature, for the convenient instruction of youth"}},
	{ID: "massachusetts-1780", File: "massachusetts-1780-chapter-5.md", Short: "Massachusetts, 1780", When: "1780-06-15", Kind: "constitution", Place: "Massachusetts", Lens: "began",
		Lit: []string{"public schools and grammar schools in the towns"}},
	{ID: "new-hampshire-1784", File: "new-hampshire-1784-literature.md", Short: "New Hampshire, 1784", When: "1784-06-02", Kind: "constitution", Place: "New Hampshire", Lens: "began",
		Lit: []string{"all seminaries and public schools"}},
	{ID: "land-ordinance", File: "land-ordinance-1785.md", Short: "Land Ordinance, 1785", When: "1785-05-20", Kind: "ordinance", Place: "United States", Lens: "began",
		Lit: []string{"There shall be reserved the lot N 16, of every township, for the maintenance of public schools, within the said township"}},
	{ID: "virginia-statute", Skill: founding.Skill, File: "virginia-statute-for-religious-freedom.md", Short: "Virginia Statute for Religious Freedom", When: "1786-01-16", Kind: "statute", Place: "Virginia", Lens: "church",
		Lit: []string{"to interfere when principles break out into overt acts against peace and good order"}},
	{ID: "northwest", Skill: founding.Skill, File: "northwest-ordinance.md", Short: "Northwest Ordinance, 1787", When: "1787-07-13", Kind: "ordinance", Place: "United States", Lens: "began",
		Lit: []string{"schools and the means of education shall forever be encouraged"}},
	{ID: "ohio-company", File: "ohio-company-terms-1787.md", Short: "Ohio Company's terms, 1787", When: "1787-07-23", Kind: "resolution", Place: "United States", Lens: "began",
		Lit: []string{"The lot N 16 in each township or fractional part of a township to be given perpetually for the purposes contained in the said Ordinance."}},
	{ID: "constitution", Skill: founding.Skill, File: "constitution.md", Short: "Constitution", When: "1787-09-17", Kind: "constitution", Place: "United States", Lens: "church",
		Lit: []string{"secure the Blessings of Liberty to ourselves and our Posterity"}},
	{ID: "rights", Skill: founding.Skill, File: "bill-of-rights.md", Short: "Bill of Rights", When: "1791-12-15", Kind: "amendments", Place: "United States", Lens: "church",
		Lit: []string{"Congress shall make no law respecting an establishment of religion, or prohibiting the free exercise thereof"}},
	{ID: "jefferson-danbury", File: "jefferson-danbury-letter.md", Short: "Jefferson to the Danbury Baptists", When: "1802-01-01", Kind: "letter", Place: "United States", Lens: "church",
		Lit: []string{"building a wall of separation between Church & State"}},
	{ID: "ohio-enabling-act", File: "ohio-enabling-act-1802.md", Short: "Ohio Enabling Act, 1802", When: "1802-04-30", Kind: "statute", Place: "United States", Lens: "began",
		Lit: []string{"That the section, number sixteen, in every township", "shall be granted to the inhabitants of such township, for the use of schools"}},
	{ID: "massachusetts-1852", File: "massachusetts-attendance-act-1852.md", Short: "Massachusetts, 1852", When: "1852-05-18", Kind: "statute", Place: "Massachusetts", Lens: "began",
		Lit: []string{"shall send such child to some public school within the town or city in which he resides, during at least twelve weeks"}},
	{ID: "equal-access", File: "equal-access-act.md", Short: "Equal Access Act", When: "1984-08-11", Kind: "statute", Place: "United States", Lens: "church",
		Lit: []string{"to deny equal access or a fair opportunity to, or discriminate against, any students who wish to conduct a meeting within that limited open forum on the basis of the religious, political, philosophical, or other content of the speech at such meetings"}},
	{ID: "school-prayer", File: "school-prayer-certification.md", Short: "School prayer, ESEA § 8524", When: "2015-12-10", Kind: "statute", Place: "United States", Lens: "church",
		Lit: []string{"a local educational agency shall certify in writing to the State educational agency involved that no policy of the local educational agency prevents, or otherwise denies participation in, constitutionally protected prayer in public elementary schools and secondary schools"}},
	{ID: "louisiana-ten-commandments", File: "louisiana-ten-commandments-act.md", Short: "Louisiana Act 676", When: "2024-06-19", Kind: "statute", Place: "Louisiana", Lens: "church",
		Lit: []string{"each public school governing authority shall display the Ten Commandments in each classroom in each school under its jurisdiction"}},
	{ID: "arkansas-ten-commandments", File: "arkansas-ten-commandments-act.md", Short: "Arkansas Act 573", When: "2025-04-14", Kind: "statute", Place: "Arkansas", Lens: "church",
		Lit: []string{"A durable poster or framed copy of a historical representation of the Ten Commandments."}},
	{ID: "texas-ten-commandments", File: "texas-ten-commandments-act.md", Short: "Texas SB 10", When: "2025", Kind: "statute", Place: "Texas", Lens: "church",
		Lit: []string{"a public elementary or secondary school shall display in a conspicuous place in each classroom of the school a durable poster or framed copy of the Ten Commandments"}},
	{ID: "alabama-ten-commandments", File: "alabama-ten-commandments-act.md", Short: "Alabama Act 2026-428", When: "2026", Kind: "statute", Place: "Alabama", Lens: "church",
		Lit: []string{"each local board of education shall display a poster or framed document that meets the requirements of subsection (b)"}},
}

// ByID is the document named, if there is one.
func ByID(id string) (Document, bool) {
	for _, d := range Documents {
		if d.ID == id {
			return d, true
		}
	}
	return Document{}, false
}

var (
	loadOnce sync.Once
	loaded   []ipc.FoundingDoc
	loadErr  error
)

// Load gives the texts, read once from the skills this build carries: they
// are built into the binary and never change while it runs.
func Load() ([]ipc.FoundingDoc, error) {
	loadOnce.Do(func() { loaded, loadErr = load(skills.Bundled()) })
	return loaded, loadErr
}

func load(all []skills.Skill) ([]ipc.FoundingDoc, error) {
	files := map[string]map[string][]byte{}
	for _, s := range all {
		files[s.Name] = s.Files
	}
	var docs []ipc.FoundingDoc
	for _, d := range Documents {
		fs, ok := files[d.skill()]
		if !ok {
			return nil, fmt.Errorf("this build has no %s skill", d.skill())
		}
		b, ok := fs[d.File]
		if !ok {
			return nil, fmt.Errorf("the %s skill has no %s", d.skill(), d.File)
		}
		x, err := founding.Parse(d.ID, b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.File, err)
		}
		x.Short, x.When, x.Kind = d.Short, d.When, d.Kind
		docs = append(docs, x)
	}
	return docs, nil
}
