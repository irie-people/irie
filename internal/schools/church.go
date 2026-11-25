package schools

import (
	"strconv"
	"strings"
)

// Church and state in public schools: the Supreme Court's 22 decisions on
// religion in public schools and on public money for religious schools,
// and the state laws of 2024 to 2026 that put the Ten Commandments in
// classrooms. The owner asked for these from another card's research on
// 2026-11-25, and said yes to Irie naming these laws in its own plain,
// legal words; the religious words themselves appear only inside the
// texts, quoted word for word. Every
// case was read in the United States Reports or the Court's own opinion,
// and every figure in its own source, on 2026-11-25.

// Case is one of the Court's decisions: its name as cited, its short name,
// its citation, the day it was decided, how the Court divided, what it
// held in plain words, which group it falls in, and where to read it.
type Case struct {
	Name, Short, Cite, Date, Vote, Held, Group, URL string
	// Texts are the texts it bears on, by id.
	Texts []string
}

// The groups the cases fall in, by what they did.
const (
	Ended     = "ended"
	Protected = "protected"
	Money     = "money"
	NoRuling  = "none"
)

// Groups name the groups, in the order the pie gives them.
var Groups = []struct{ ID, Name, About string }{
	{Ended, "Ended religion in a school's own program",
		"Prayers, Bible reading, religious classes and displays that the school or the state itself arranged, and laws that shaped what science classes taught for religious reasons: each struck down under the Establishment Clause."},
	{Protected, "Protected a person's own faith or speech",
		"A student's, a family's or a coach's own beliefs, and religion beside the school day: the Court upheld the right not to salute the flag, release for religious classes off school grounds, student religious clubs and their meetings, a coach's personal prayer and parents' right to take their children out of lessons."},
	{Money, "Public money and religious schools",
		"Whether public money may reach religious schools and their families: allowed for bus fares in 1947, held back in 1971, allowed in 2002 where families choose the school, and since 2017 a state that opens a programme to others may not shut religious schools out of it for being religious."},
	{NoRuling, "An even split, no ruling",
		"Four justices to four, so the judgment below stood and no precedent was set."},
}

// Cases are the Court's decisions, by date.
var Cases = []Case{
	{Name: "West Virginia State Board of Education v. Barnette", Short: "West Virginia v. Barnette", Cite: "319 U.S. 624", Date: "1943-06-14", Vote: "6–3", Group: Protected,
		Held: "A state may not compel public-school children to salute the flag and recite the Pledge: the compulsory salute, as applied to children expelled for refusing, violates the First and Fourteenth Amendments.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep319/usrep319624/usrep319624.pdf"},
	{Name: "Everson v. Board of Education of the Township of Ewing", Short: "Everson v. Board of Education", Cite: "330 U.S. 1", Date: "1947-02-10", Vote: "5–4", Group: Money,
		Held: "New Jersey's reimbursement of parents' bus fares, including for children at Catholic schools, did not violate the Establishment Clause, which applies to the states through the Fourteenth Amendment.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep330/usrep330001/usrep330001.pdf", Texts: []string{"rights"}},
	{Name: "Illinois ex rel. McCollum v. Board of Education", Short: "McCollum v. Board of Education", Cite: "333 U.S. 203", Date: "1948-03-08", Vote: "8–1", Group: Ended,
		Held: "Religious instruction given by outside religious teachers inside public school buildings during school hours, using the compulsory-attendance system, violates the First Amendment.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep333/usrep333203/usrep333203.pdf"},
	{Name: "Zorach v. Clauson", Short: "Zorach v. Clauson", Cite: "343 U.S. 306", Date: "1952-04-28", Vote: "6–3", Group: Protected,
		Held: "New York City's programme releasing students during school hours, on their parents' written request, for religious instruction off school grounds does not violate the First Amendment.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep343/usrep343306/usrep343306.pdf"},
	{Name: "Engel v. Vitale", Short: "Engel v. Vitale", Cite: "370 U.S. 421", Date: "1962-06-25", Vote: "6–1", Group: Ended,
		Held: "State officials may not compose an official prayer and require its daily recitation in public schools, even if it is denominationally neutral and pupils may stay silent or be excused.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep370/usrep370421/usrep370421.pdf", Texts: []string{"school-prayer"}},
	{Name: "School District of Abington Township v. Schempp", Short: "Abington v. Schempp", Cite: "374 U.S. 203", Date: "1963-06-17", Vote: "8–1", Group: Ended,
		Held: "No state law or school board may require devotional Bible reading or the recitation of a prayer at the start of the school day, even when students may be excused on request.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep374/usrep374203/usrep374203.pdf"},
	{Name: "Epperson v. Arkansas", Short: "Epperson v. Arkansas", Cite: "393 U.S. 97", Date: "1968-11-12", Vote: "9–0", Group: Ended,
		Held: "Arkansas's statute forbidding the teaching of evolution in public schools violates the Establishment Clause, applied to the states through the Fourteenth Amendment.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep393/usrep393097/usrep393097.pdf"},
	{Name: "Lemon v. Kurtzman", Short: "Lemon v. Kurtzman", Cite: "403 U.S. 602", Date: "1971-06-28", Vote: "8–0 and 8–1", Group: Money,
		Held: "Pennsylvania's reimbursement, and Rhode Island's salary supplements, for teachers of secular subjects in church-affiliated schools violate the Religion Clauses through excessive entanglement of government and religion. Its three-part test stood until the Court set it aside in 2022.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep403/usrep403602/usrep403602.pdf"},
	{Name: "Stone v. Graham", Short: "Stone v. Graham", Cite: "449 U.S. 39", Date: "1980-11-17", Vote: "5–4", Group: Ended,
		Held: "Kentucky's statute requiring a privately funded copy of the Ten Commandments on the wall of each public classroom had no secular legislative purpose and violated the Establishment Clause; decided without a full hearing.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep449/usrep449039/usrep449039.pdf"},
	{Name: "Wallace v. Jaffree", Short: "Wallace v. Jaffree", Cite: "472 U.S. 38", Date: "1985-06-04", Vote: "6–3", Group: Ended,
		Held: "Alabama's 1981 statute authorizing a period of silence \"for meditation or voluntary prayer\" is a law respecting an establishment of religion.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep472/usrep472038/usrep472038.pdf"},
	{Name: "Edwards v. Aguillard", Short: "Edwards v. Aguillard", Cite: "482 U.S. 578", Date: "1987-06-19", Vote: "7–2", Group: Ended,
		Held: "Louisiana's Creationism Act, requiring \"creation science\" to be taught whenever evolution is, is invalid under the Establishment Clause because it lacks a clear secular purpose.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep482/usrep482578/usrep482578.pdf"},
	{Name: "Board of Education of Westside Community Schools v. Mergens", Short: "Westside v. Mergens", Cite: "496 U.S. 226", Date: "1990-06-04", Vote: "8–1", Group: Protected,
		Held: "The school violated the Equal Access Act by refusing to recognize a student religious club, and the Act does not violate the Establishment Clause.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep496/usrep496226/usrep496226.pdf", Texts: []string{"equal-access"}},
	{Name: "Lee v. Weisman", Short: "Lee v. Weisman", Cite: "505 U.S. 577", Date: "1992-06-24", Vote: "5–4", Group: Ended,
		Held: "Including clergy who offer prayers as part of an official public school graduation ceremony is forbidden by the Establishment Clause.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep505/usrep505577/usrep505577.pdf"},
	{Name: "Santa Fe Independent School District v. Doe", Short: "Santa Fe v. Doe", Cite: "530 U.S. 290", Date: "2000-06-19", Vote: "6–3", Group: Ended,
		Held: "The district's policy permitting student-led, student-initiated prayer at football games violates the Establishment Clause.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep530/usrep530290/usrep530290.pdf"},
	{Name: "Good News Club v. Milford Central School", Short: "Good News Club v. Milford", Cite: "533 U.S. 98", Date: "2001-06-11", Vote: "6–3", Group: Protected,
		Held: "Excluding a Christian children's club from after-hours use of school buildings violated its freedom of speech; letting it meet would not have violated the Establishment Clause.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep533/usrep533098/usrep533098.pdf"},
	{Name: "Zelman v. Simmons-Harris", Short: "Zelman v. Simmons-Harris", Cite: "536 U.S. 639", Date: "2002-06-27", Vote: "5–4", Group: Money,
		Held: "Ohio's Cleveland scholarship programme, whose vouchers families could use at religious schools, does not offend the Establishment Clause.",
		URL:  "https://tile.loc.gov/storage-services/service/ll/usrep/usrep536/usrep536639/usrep536639.pdf"},
	{Name: "Trinity Lutheran Church of Columbia, Inc. v. Comer", Short: "Trinity Lutheran v. Comer", Cite: "582 U.S. 449", Date: "2017-06-26", Vote: "7–2", Group: Money,
		Held: "Missouri violated the Free Exercise Clause by denying a church's preschool an otherwise available grant to resurface its playground solely because it is a church.",
		URL:  "https://www.supremecourt.gov/opinions/16pdf/15-577_khlp.pdf"},
	{Name: "Espinoza v. Montana Department of Revenue", Short: "Espinoza v. Montana", Cite: "591 U.S. 464", Date: "2020-06-30", Vote: "5–4", Group: Money,
		Held: "Applying Montana's no-aid provision to bar religious schools from a tax-credit scholarship programme discriminated against religious schools and families, in violation of the Free Exercise Clause.",
		URL:  "https://www.supremecourt.gov/opinions/19pdf/18-1195_g314.pdf"},
	{Name: "Carson v. Makin", Short: "Carson v. Makin", Cite: "596 U.S. 767", Date: "2022-06-21", Vote: "6–3", Group: Money,
		Held: "Maine's \"nonsectarian\" requirement for its otherwise generally available tuition assistance violates the Free Exercise Clause.",
		URL:  "https://www.supremecourt.gov/opinions/21pdf/596us2r49_7l48.pdf"},
	{Name: "Kennedy v. Bremerton School District", Short: "Kennedy v. Bremerton", Cite: "597 U.S. 507", Date: "2022-06-27", Vote: "6–3", Group: Protected,
		Held: "The Free Exercise and Free Speech Clauses protect a school employee's personal religious observance from government reprisal; the Court set aside the Lemon test for one of history and coercion.",
		URL:  "https://www.supremecourt.gov/opinions/21pdf/597us2r62_d92h.pdf", Texts: []string{"rights"}},
	{Name: "Oklahoma Statewide Charter School Board v. Drummond", Short: "Oklahoma Charter School Board v. Drummond", Cite: "605 U.S. 165", Date: "2025-05-22", Vote: "4–4", Group: NoRuling,
		Held: "No opinion: by an equally divided Court, with one justice taking no part, the Oklahoma Supreme Court's judgment blocking a religious charter school stands, setting no precedent.",
		URL:  "https://www.supremecourt.gov/opinions/24pdf/605us1r33_gnbf.pdf"},
	{Name: "Mahmoud v. Taylor", Short: "Mahmoud v. Taylor", Cite: "606 U.S. 522", Date: "2025-06-27", Vote: "6–3", Group: Protected,
		Held: "Parents who objected to a school board's introduction of LGBTQ-inclusive storybooks, with its refusal of opt-outs, are entitled to a preliminary injunction under the Free Exercise Clause.",
		URL:  "https://www.supremecourt.gov/opinions/24pdf/606us2r63_n648.pdf"},
}

// caseSources are the cases as sources the page can name and link.
func caseSources() []Source {
	var out []Source
	for _, c := range Cases {
		out = append(out, Source{ID: "case-" + slug(c.Short), Title: c.Name + ", " + c.Cite + " (" + c.Date[:4] + ")",
			By: "Supreme Court of the United States, " + niceDate(c.Date), URL: c.URL, Read: "2026-11-25"})
	}
	return out
}

// The Ten Commandments laws, where each stood on 2026-11-25.
var TenCommandments = []struct {
	State, Text, Status, About, Source string
}{
	{"Louisiana", "louisiana-ten-commandments", "In force",
		"Act 676 of 2024, in force since 19 June 2024, with displays due by 1 January 2025. On 20 February 2026 the Fifth Circuit, sitting with all its judges, found the families' challenge premature, 11 to 7, and set aside the order that had blocked the law. The state's own petition to the Supreme Court is set for its conference of 11 December 2026.",
		"ca5-roake"},
	{"Texas", "texas-ten-commandments", "In force",
		"Senate Bill 10, signed on 20 June 2025 and in force from 1 September. On 21 April 2026 the Fifth Circuit, with all its judges, upheld it 9 to 8. The families' petition to the Supreme Court, No. 26-257, is set for its conference of 11 December 2026; Texas asks the Court to deny it.",
		"ca5-nathan"},
	{"Arkansas", "arkansas-ten-commandments", "Blocked in six districts",
		"Act 573 of 2025, approved on 14 April 2025. A federal court barred it in six school districts, with a permanent injunction on 16 March 2026; it holds elsewhere in the state. The Eighth Circuit heard the appeal on 22 September 2026 and had not ruled by 25 November.",
		"stinson"},
	{"Alabama", "alabama-ten-commandments", "Not yet due",
		"Senate Bill 99 of 2026, in force from 1 October 2026: a poster of the Ten Commandments beside the founding documents in history classrooms from the fifth grade up and in a common area, due from 1 January 2027 where donations pay for them.",
		"al-sb99"},
}

// Church sources other than the cases.
var churchSources = []Source{
	{ID: "ca5-roake", Title: "Roake v. Brumley, No. 24-30706 (5th Cir. Feb. 20, 2026) (en banc)", By: "U.S. Court of Appeals for the Fifth Circuit",
		URL: "https://www.ca5.uscourts.gov/opinions/pub/24/24-30706-CV1.pdf", Read: "2026-11-25"},
	{ID: "ca5-nathan", Title: "Nathan v. Alamo Heights Independent School District, No. 25-50695 (5th Cir. Apr. 21, 2026) (en banc)", By: "U.S. Court of Appeals for the Fifth Circuit",
		URL: "https://www.ca5.uscourts.gov/opinions/pub/25/25-50695-CV0.pdf", Read: "2026-11-25"},
	{ID: "stinson", Title: "Stinson v. Fayetteville School District No. 1, No. 5:25-cv-5127, Memorandum Opinion and Order (W.D. Ark. Mar. 16, 2026)", By: "U.S. District Court for the Western District of Arkansas, as the Arkansas Advocate published it",
		URL: "https://arkansasadvocate.com/wp-content/uploads/2026/03/20260316_Stinson-v-Fayetteville_-Order.pdf", Read: "2026-11-25"},
	{ID: "al-sb99", Title: "SB 99, enrolled (2026 Regular Session)", By: "Alabama Legislature",
		URL: "https://alison.legislature.state.al.us/files/pdf/SearchableInstruments/2026RS/SB99-enr.pdf", Read: "2026-11-25"},
	{ID: "scotus-docket", Title: "Docket of No. 26-257, Nathan v. Alamo Heights Independent School District", By: "Supreme Court of the United States",
		URL: "https://www.supremecourt.gov/docket/docketfiles/html/public/26-257.html", Read: "2026-11-25"},
	{ID: "al-amendment-3", Title: "State Certification of General Election Results, 3 November 2026 (Statewide Amendment 3, p. 203)", By: "Alabama Secretary of State, certified 23 November 2026",
		URL: "https://www.sos.alabama.gov/sites/default/files/election-2026/2026StateCertificationofGeneralElectionResults-Canvassing.pdf", Read: "2026-11-25"},
	{ID: "irs-fstc", Title: "Federal Scholarship Tax Credit (FSTC): participating states", By: "Internal Revenue Service, as of 13 November 2026",
		URL: "https://www.irs.gov/government-entities/federal-state-local-governments/federal-scholarship-tax-credit-fstc-participating-states", Read: "2026-11-25"},
}

// Alabama's Statewide Amendment 3 of 3 November 2026, as certified.
const (
	AmendmentYes = 975291
	AmendmentNo  = 583141
)

// The states in the federal scholarship tax credit for 2027, by the IRS's
// list of 13 November 2026.
const (
	CreditIn  = 31
	CreditOut = 19
)

// church is the Court's cases by what they did, each group by decade.
func church() *Node {
	root := &Node{
		ID: "church", Name: "Church and state", Unit: "cases",
		Title:  "The Supreme Court on religion in public schools, 1943–2025, by what each decision did",
		Figure: strconv.Itoa(len(Cases)) + " decisions",
		About:  "The Supreme Court's decisions on religion in public schools and on public money for religious schools, from 1943 to 2025, each read in the United States Reports or the Court's own opinion. They rest on the First Amendment, whose religion clauses bind the states through the Fourteenth: a school may not lead, compose or arrange prayer or religious teaching, while a person in it keeps their own faith and speech. In 2022 the Court set aside the Lemon test for one of history and coercion, and states have since tested how far that reaches.",
		Texts:  []string{"rights", "jefferson-danbury", "virginia-statute", "school-prayer"},
		Also:   []*Node{tenCommandments(), amendment3(), scholarshipCredit()},
	}
	for _, g := range Groups {
		n := &Node{ID: "church/" + g.ID, Name: g.Name, Unit: "cases", Title: g.Name + ", by decade", About: g.About}
		byDecade := map[string][]Case{}
		var order []string
		for _, c := range Cases {
			if c.Group != g.ID {
				continue
			}
			d := c.Date[:3] + "0s"
			if _, ok := byDecade[d]; !ok {
				order = append(order, d)
			}
			byDecade[d] = append(byDecade[d], c)
		}
		count := 0
		for _, d := range order {
			cs := byDecade[d]
			count += len(cs)
			if len(cs) == 1 {
				n.Parts = append(n.Parts, caseNode(n.ID, cs[0]))
				continue
			}
			dn := &Node{ID: n.ID + "/" + d, Name: "The " + d, Value: float64(len(cs)), Figure: strconv.Itoa(len(cs)) + " decisions", Unit: "cases",
				Title: g.Name + ", the " + d, About: "The decisions of the " + d + " in this group, by date."}
			for _, c := range cs {
				dn.Parts = append(dn.Parts, caseNode(dn.ID, c))
			}
			n.Parts = append(n.Parts, dn)
		}
		n.Value, n.Figure = float64(count), strconv.Itoa(count)+" decision"
		if count != 1 {
			n.Figure += "s"
		}
		if count == 1 {
			// A group of one decision is the decision itself.
			c := caseNode("church", Cases[indexOfGroup(g.ID)])
			c.ID, c.Name, c.About = n.ID, g.Name, g.About+" "+c.About
			n = c
		}
		root.Parts = append(root.Parts, n)
	}
	return root
}

func indexOfGroup(g string) int {
	for i, c := range Cases {
		if c.Group == g {
			return i
		}
	}
	return -1
}

// caseNode is one decision as a slice of its pie.
func caseNode(parent string, c Case) *Node {
	return &Node{ID: parent + "/" + slug(c.Short), Name: c.Short, Value: 1,
		Figure: c.Date[:4] + ", " + c.Vote, About: c.Held,
		Source: "case-" + slug(c.Short), Where: c.Cite, Texts: c.Texts}
}

// tenCommandments is the four states that require the Ten Commandments in
// classrooms, by where their laws stood on 2026-11-25.
func tenCommandments() *Node {
	n := &Node{ID: "church/ten-commandments", Name: "The Ten Commandments laws", Unit: "states",
		Title:  "The states that require the Ten Commandments in public school classrooms, by where each law stood on 25 November 2026",
		About:  "Four states have made public schools display the Ten Commandments since 2024, though Stone v. Graham struck down Kentucky's law of that kind in 1980 and has not been overruled. Louisiana's act lets schools hang the Mayflower Compact, the Declaration of Independence and the Northwest Ordinance beside them, and Alabama's poster quotes those and the Constitution, in words that are not always the documents' own. The Supreme Court meets on 11 December 2026 to decide whether to hear the Texas and Louisiana cases.",
		Source: "scotus-docket", Texts: []string{"mayflower", "declaration", "constitution", "northwest"}}
	byStatus := map[string]*Node{}
	var order []string
	for _, t := range TenCommandments {
		s, ok := byStatus[t.Status]
		if !ok {
			s = &Node{ID: n.ID + "/" + slug(t.Status), Name: t.Status, Unit: "states", Title: "The Ten Commandments laws: " + strings.ToLower(t.Status[:1]) + t.Status[1:]}
			byStatus[t.Status] = s
			order = append(order, t.Status)
		}
		s.Parts = append(s.Parts, &Node{ID: s.ID + "/" + slug(t.State), Name: t.State, Value: 1, Figure: "1 state", About: t.About, Source: t.Source, Texts: []string{t.Text}})
	}
	for _, st := range order {
		s := byStatus[st]
		s.Value = float64(len(s.Parts))
		s.Figure = states(len(s.Parts))
		if len(s.Parts) == 1 {
			// One state is the state itself, named for where it stands.
			only := s.Parts[0]
			only.ID, only.Name = s.ID, st+": "+only.Name
			s = only
		}
		n.Parts = append(n.Parts, s)
	}
	n.Value, n.Figure = float64(len(TenCommandments)), states(len(TenCommandments))
	return n
}

// amendment3 is Alabama's vote of 3 November 2026 on student-led prayer.
func amendment3() *Node {
	all := AmendmentYes + AmendmentNo
	return &Node{ID: "church/alabama-amendment-3", Name: "Alabama's Amendment 3", Value: float64(all), Unit: "votes",
		Title:  "Alabama's 2026 vote on the Pledge and student-led prayer in schools",
		Figure: commas(int64(all)) + " votes, certified 23 November 2026",
		About:  "Statewide Amendment 3, on the ballot of 3 November 2026, has school boards adopt policies for the daily Pledge of Allegiance and authorizing student-initiated and student-led prayer.",
		Source: "al-amendment-3",
		Parts: []*Node{
			{ID: "church/alabama-amendment-3/yes", Name: "Yes", Value: AmendmentYes, Figure: commas(AmendmentYes) + " votes", Source: "al-amendment-3"},
			{ID: "church/alabama-amendment-3/no", Name: "No", Value: AmendmentNo, Figure: commas(AmendmentNo) + " votes", Source: "al-amendment-3"},
		}}
}

// scholarshipCredit is the states in the 2027 federal scholarship credit.
func scholarshipCredit() *Node {
	return &Node{ID: "church/scholarship-credit", Name: "The federal scholarship credit", Value: CreditIn + CreditOut, Unit: "states",
		Title:  "The states taking part in the federal scholarship tax credit for 2027",
		Figure: "50 states, as of 13 November 2026",
		About:  "From 2027 a federal tax credit repays donors for gifts to scholarship groups in the states that elect to take part, and the scholarships may pay for private schools, religious ones among them. By the IRS's list of 13 November 2026, 31 states had elected to; the list may still change.",
		Source: "irs-fstc",
		Parts: []*Node{
			{ID: "church/scholarship-credit/in", Name: "Taking part", Value: CreditIn, Figure: states(CreditIn), Source: "irs-fstc"},
			{ID: "church/scholarship-credit/out", Name: "Not listed", Value: CreditOut, Figure: states(CreditOut), Source: "irs-fstc"},
		}}
}

// niceDate writes "2026-11-25" as "25 November 2026".
func niceDate(when string) string {
	months := []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	if len(when) != 10 {
		return when
	}
	m, err := strconv.Atoi(when[5:7])
	if err != nil || m < 1 || m > 12 {
		return when
	}
	d, _ := strconv.Atoi(when[8:10])
	return strconv.Itoa(d) + " " + months[m-1] + " " + when[:4]
}
