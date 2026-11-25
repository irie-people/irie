package schools

import (
	"strconv"
	"strings"
)

// Who builds and keeps up the schools today: the buildings and the buses,
// each figure as its source gives it, read there on 2026-11-25. The owner
// asked for these from another card's research the day Common Schools
// began; each figure was checked in its own source, and where the
// research's differed, the source's stands.

// The State of Our Schools 2025 report, in 2024 dollars: building
// (capital outlay) a year on average over fiscal 2014–2023 and the gap
// to its standard of 4% of the buildings' replacement value (Table 3);
// upkeep (maintenance and operations) over fiscal 2019–2023 and the gap
// to 3% (Table 2), as corrected on 27 January 2026.
const (
	BuildingSpent = 82337545087.0
	BuildingShort = 56098579815.0
	BuildingNeed  = 138436124903.0
	UpkeepSpent   = 74372656612.0
	UpkeepShort   = 29273479379.0
	UpkeepNeed    = 103646135991.0
)

// The buses: S&P Global Mobility's count of new Type C and D school buses
// registered in the US and Canada in the year to September 2025, from
// Blue Bird's investor presentation (slide 8), and Blue Bird's own buses
// in fiscal 2025 (Annual Report): 9,409 sold anywhere in the world, 5,275
// of them propane, gasoline or electric, 901 of those electric.
const (
	TypeCDRegistered = 31020
	BlueBirdSold     = 9409
	BlueBirdAlt      = 5275
	BlueBirdElectric = 901
	// Its net sales by segment, in thousands of dollars.
	BlueBirdBusSales  = 1377125
	BlueBirdPartSales = 102974
)

// ESCOs are the energy service companies' 2024 revenue by market, in
// millions of dollars (LBNL, p. 12, Share of Revenue by Market Segment).
var ESCOs = []struct {
	Market   string
	Millions float64
}{
	{"K-12 schools", 3220},
	{"State and local government", 1800},
	{"Not known", 1740},
	{"Federal", 1600},
	{"Public housing and other", 840},
	{"Universities and colleges", 620},
	{"Commercial and industrial", 590},
	{"Healthcare", 280},
}

// ESCOTotal is the table's total, $10.69 billion.
const ESCOTotal = 10690.0

// Contractors are the four largest school bus contractors' yellow buses
// on School Bus Fleet's 2026 list (North America); Student Transportation
// of America's is the magazine's estimate.
var Contractors = []struct {
	Name  string
	Buses int
	Note  string
}{
	{"First Student", 47000, ""},
	{"Student Transportation of America", 18000, "School Bus Fleet's estimate"},
	{"Summit School Services", 17000, ""},
	{"Beacon Mobility", 14650, ""},
}

// Owned are the yellow buses by owner in the 31 states that School Bus
// Fleet's 2024-25 statistics count both ways.
const (
	OwnedByContractors = 92576
	OwnedInAll         = 302466
)

// buildings is what the school buildings need a year, by the State of Our
// Schools 2025 report's measure: what districts spend and how far short.
func buildings() *Node {
	root := &Node{
		ID: "buildings", Name: "The buildings", Unit: "dollars",
		Title:  "What the public school buildings need a year",
		Figure: "$242 billion a year", Whole: "$242 billion a year",
		About:  "What the nation's public school districts spend each year on their buildings, and how far that falls short of what the buildings need by the State of Our Schools 2025 report's measure: 4% of their replacement value a year to build and renew them, 3% to run and keep them up, not counting new schools for growing enrolments. Building is the yearly average over fiscal 2014–2023, upkeep over 2019–2023, both in 2024 dollars. The school board decides and pays: it hires the architect, puts bonds to the voters and awards the contracts in public; private firms do nearly all the work.",
		Source: "sos-2025", Where: "Tables 2 and 3, pages 22 to 27",
	}
	building := &Node{
		ID: "buildings/building", Name: "Spent on building and renewal", Value: BuildingSpent, Figure: billions(BuildingSpent) + " a year",
		Unit: "percent", Title: "Who paid for building and renewal, fiscal 2014–2023",
		About:  "School construction and capital equipment: new and replacement schools, renovations, system replacements and retrofits. The districts paid 80.5% of it from local money, the states about 16% ($135.5 billion over the ten years) and the federal government 3.2% ($27 billion); the report's Chart 13 rounds them to 80, 17 and 3, the slices here.",
		Source: "sos-2025", Where: "pages 27 and 32 to 34, Chart 13",
		Parts: []*Node{
			{ID: "buildings/building/local", Name: "School districts", Value: 80, Figure: "80%", About: "Paid from the districts' own money: 80.5% of school construction and capital equipment, fiscal 2014–2023.", Source: "sos-2025", Where: "page 32"},
			{ID: "buildings/building/state", Name: "States", Value: 17, Figure: "17%", About: "About 16% in the report's words ($135.5 billion over the ten years), 17% in its chart; some states paid nothing toward their districts' buildings, and a few paid more than half.", Source: "sos-2025", Where: "pages 32 and 33"},
			{ID: "buildings/building/federal", Name: "The federal government", Value: 3, Figure: "3%", About: "$27 billion over the ten years, 3.2%.", Source: "sos-2025", Where: "page 34"},
		},
	}
	upkeep := &Node{
		ID: "buildings/upkeep", Name: "Spent on running and upkeep", Value: UpkeepSpent, Figure: billions(UpkeepSpent) + " a year",
		About:  "Maintenance and operations of the buildings, the yearly average over fiscal 2019–2023. Most of the daily work is the districts' own crews; for bigger jobs they call in mechanical contractors, the equipment makers' own service arms, facility firms that take over a district's custodial and maintenance work, and energy service companies paid over years from the savings they guarantee.",
		Source: "sos-2025", Where: "Table 2, pages 23 and 24",
		Also: []*Node{escos(), hvac()},
	}
	root.Parts = []*Node{
		building,
		{ID: "buildings/building-short", Name: "Short of what building needs", Value: BuildingShort, Figure: billions(BuildingShort) + " a year",
			About:  "What districts would add each year to meet the report's standard for building: 4% of the buildings' replacement value, which is 2% to renew equipment, parts and finishes, 1% to catch up on deferred maintenance and 1% to alter buildings for new uses, " + billions(BuildingNeed) + " a year in all, not counting new schools for growth.",
			Source: "sos-2025", Where: "pages 22 and 27, Table 3"},
		upkeep,
		{ID: "buildings/upkeep-short", Name: "Short of what upkeep needs", Value: UpkeepShort, Figure: billions(UpkeepShort) + " a year",
			About:  "What districts would add each year to meet the report's standard for upkeep, 3% of the buildings' replacement value, " + billions(UpkeepNeed) + " a year: $810 a student.",
			Source: "sos-2025", Where: "pages 23 and 24, Table 2"},
	}
	return root
}

// escos is the energy service companies' revenue by market, 2024.
func escos() *Node {
	n := &Node{ID: "buildings/upkeep/escos", Name: "Energy service companies' revenue", Unit: "dollars",
		Title:  "Energy service companies' revenue by market, 2024",
		Figure: "$10.69 billion in 2024",
		About:  "Energy service companies replace heating, cooling, lighting and controls, and are paid over ten to twenty years from the energy savings they guarantee. Schools were their largest market in 2024: $3.22 billion, 30% of the industry's $10.69 billion, by the companies' own reports to Lawrence Berkeley National Laboratory.",
		Source: "lbnl-esco-2026", Where: "page 12"}
	for _, e := range ESCOs {
		n.Parts = append(n.Parts, &Node{ID: n.ID + "/" + slug(e.Market), Name: e.Market, Value: e.Millions * 1e6, Figure: billions(e.Millions * 1e6),
			Source: "lbnl-esco-2026", Where: "page 12"})
	}
	n.Value = ESCOTotal * 1e6
	return n
}

// hvac is the GAO's estimate of the districts whose heating, ventilation
// and air conditioning needs work in at least half their schools.
func hvac() *Node {
	return &Node{ID: "buildings/upkeep/hvac", Name: "Districts needing HVAC work", Value: 100, Unit: "percent",
		Title:  "School districts whose heating and cooling needs updating or replacing in at least half their schools, 2020",
		Figure: "41% of districts, about 36,000 schools",
		About:  "In 2020 the GAO estimated, from a national survey of districts, that 41% of them needed to update or replace the heating, ventilation and air conditioning in at least half their schools: about 36,000 schools, a low estimate with a margin of 9,000. About 28,000 schools needed to update or replace their roofing.",
		Source: "gao-20-494", Where: "pages 8 and 12–13",
		Parts: []*Node{
			{ID: "buildings/upkeep/hvac/most", Name: "In at least half their schools", Value: 41, Figure: "41%", Source: "gao-20-494", Where: "page 8"},
			{ID: "buildings/upkeep/hvac/fewer", Name: "In fewer of their schools, or none", Value: 59, Figure: "59%", About: "The rest of the districts, by subtraction.", Source: "gao-20-494", Where: "page 8"},
		}}
}

// buses is who makes the full-size school buses, with who owns them.
func buses() *Node {
	others := TypeCDRegistered - BlueBirdSold
	blue := &Node{ID: "buses/blue-bird", Name: "Blue Bird", Value: BlueBirdSold, Figure: commas(BlueBirdSold) + " buses in fiscal 2025",
		Unit: "buses", Title: "What Blue Bird's buses ran on, fiscal 2025",
		About:  "Blue Bird, of Fort Valley, Georgia, has built school buses since 1927, and is the only one of the three makers that publishes its sales. It sold 9,409 buses in fiscal 2025, anywhere in the world, 92.6% of them through its dealers; 5,275 ran on propane, gasoline or electricity, 901 of them electric, and it reckons it sold about 64% of the alternative-powered school buses from fiscal 2015 through 2025.",
		Note:   "Its 9,409 are its sales anywhere in the world, and the 31,020 the new registrations in the US and Canada, so its slice of the pie before, about 30%, is our arithmetic, not a share it reports.",
		Source: "blue-bird-2025", Where: "Form 10-K pages 5 to 9",
		Parts: []*Node{
			{ID: "buses/blue-bird/diesel", Name: "Diesel", Value: BlueBirdSold - BlueBirdAlt, Figure: commas(BlueBirdSold-BlueBirdAlt) + " buses", About: "The rest of its 9,409, by subtraction.", Source: "blue-bird-2025", Where: "Form 10-K pages 5 and 9"},
			{ID: "buses/blue-bird/propane-gasoline", Name: "Propane and gasoline", Value: BlueBirdAlt - BlueBirdElectric, Figure: commas(BlueBirdAlt-BlueBirdElectric) + " buses", About: "Its 5,275 propane, gasoline and electric buses, less the 901 electric.", Source: "blue-bird-2025", Where: "Form 10-K page 9"},
			{ID: "buses/blue-bird/electric", Name: "Electric", Value: BlueBirdElectric, Figure: commas(BlueBirdElectric) + " buses", About: "Type C and Type D electric buses, 28% more than the year before.", Source: "blue-bird-2025", Where: "Form 10-K page 9"},
		},
		Also: []*Node{{ID: "buses/blue-bird/sales", Name: "Blue Bird's sales", Value: BlueBirdBusSales + BlueBirdPartSales, Unit: "dollars",
			Title: "Where Blue Bird's sales came from, fiscal 2025", Figure: "$1.48 billion in fiscal 2025",
			About: "Its net sales by segment: buses, and the parts it sells for them.", Source: "blue-bird-2025", Where: "Form 10-K, net sales by segment",
			Parts: []*Node{
				{ID: "buses/blue-bird/sales/buses", Name: "Buses", Value: BlueBirdBusSales * 1000, Figure: "$1,377.1 million", Source: "blue-bird-2025", Where: "Form 10-K"},
				{ID: "buses/blue-bird/sales/parts", Name: "Parts", Value: BlueBirdPartSales * 1000, Figure: "$103.0 million", Source: "blue-bird-2025", Where: "Form 10-K"},
			}}},
	}
	rest := &Node{ID: "buses/others", Name: "Thomas Built, IC Bus and others", Value: float64(others), Figure: "about " + commas(int64(others)) + " buses",
		About:  "Blue Bird names its two principal competitors: Thomas Built Buses, of Daimler Truck North America, and IC Bus, of International Motors. Neither publishes its sales, so they share this slice with the smaller makers.",
		Source: "blue-bird-2025", Where: "Form 10-K, competition"}
	owners := &Node{ID: "buses/owners", Name: "Who owns the buses", Value: OwnedInAll, Unit: "buses",
		Title:  "Who owns the yellow buses, in the 31 states that count both, 2024–25",
		Figure: commas(OwnedInAll) + " buses in 31 states",
		About:  "More than 480,000 school buses carry more than 25 million children a day, by the American School Bus Council's count, more riders than any other transport in the country. Most belong to the districts themselves; contractors own the rest. School Bus Fleet's 2024–25 statistics count both in 31 states.",
		Source: "sbf-stats-2025", More: []string{"asbc-2019"},
		Parts: []*Node{
			{ID: "buses/owners/districts", Name: "Districts and states", Value: OwnedInAll - OwnedByContractors, Figure: commas(OwnedInAll-OwnedByContractors) + " buses", About: "The buses the districts, or their states, own and run, by subtraction.", Source: "sbf-stats-2025"},
			{ID: "buses/owners/contractors", Name: "Contractors", Value: OwnedByContractors, Figure: commas(OwnedByContractors) + " buses",
				About: "Private companies that own the buses and run the routes for districts, under contracts the boards award.", Source: "sbf-stats-2025",
				Also: []*Node{largest()}},
		}}
	return &Node{
		ID: "buses", Name: "The buses", Unit: "buses",
		Title:  "Who makes the full-size school buses",
		Figure: commas(TypeCDRegistered) + " new in 2025",
		About:  "New full-size school buses, Types C and D, registered in the United States and Canada in the year to September 2025, by S&P Global Mobility's count as Blue Bird gives it: 31,020, against a 1985–2025 average of about 30,500 a year. Three makers build nearly all of them.",
		Source: "blue-bird-deck-2025", Where: "slide 8", More: []string{"blue-bird-2025"},
		Parts: []*Node{blue, rest},
		Also:  []*Node{owners},
	}
}

// largest is the four largest contractors' yellow buses.
func largest() *Node {
	n := &Node{ID: "buses/owners/contractors/largest", Name: "The largest contractors", Unit: "buses",
		Title:  "The four largest school bus contractors' yellow buses, 2026",
		About:  "Their yellow buses across North America on School Bus Fleet's 2026 list, from the companies' answers to its survey.",
		Source: "sbf-contractors-2026"}
	var sum int
	for _, c := range Contractors {
		x := &Node{ID: n.ID + "/" + slug(c.Name), Name: c.Name, Value: float64(c.Buses), Figure: commas(int64(c.Buses)) + " buses", Source: "sbf-contractors-2026", Note: c.Note}
		n.Parts = append(n.Parts, x)
		sum += c.Buses
	}
	n.Value, n.Figure = float64(sum), commas(int64(sum))+" buses"
	return n
}

// billions writes dollars as billions: whole from a hundred billion, to
// a tenth from ten, to a hundredth below, without a trailing nought.
func billions(d float64) string {
	b := d / 1e9
	places := 2
	switch {
	case b >= 100:
		return "$" + commas(int64(b+0.5)) + " billion"
	case b >= 10:
		places = 1
	}
	s := strconv.FormatFloat(b, 'f', places, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	}
	return "$" + s + " billion"
}
