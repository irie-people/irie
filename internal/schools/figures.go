package schools

// The figures the pies draw, each from a public source named beside it.
// The tests hold them to the sources' own totals, so a figure copied wrong
// shows up as a sum that doesn't add.

// Source is where figures come from: its title, who made it and in what
// print, where to read it, and when we read it there.
type Source struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	By    string `json:"by"`
	URL   string `json:"url"`
	Read  string `json:"read"`
}

// Sources are the figures' sources, by id.
var Sources = []Source{
	{ID: "swift-1922", Title: "Federal Aid to Public Schools",
		By:  "Fletcher Harper Swift, Bureau of Education, Department of the Interior, Bulletin, 1922, No. 47 (Government Printing Office, 1923)",
		URL: "https://www.govinfo.gov/content/pkg/GOVPUB-I16-722522d860470d9328c978a17b85a02f/pdf/GOVPUB-I16-722522d860470d9328c978a17b85a02f.pdf", Read: "2026-11-25"},
	{ID: "census-divisions", Title: "Census Regions and Divisions of the United States",
		By:  "U.S. Census Bureau",
		URL: "https://www2.census.gov/geo/pdfs/maps-data/maps/reference/us_regdiv.pdf", Read: "2026-11-25"},
	{ID: "sos-2025", Title: "State of Our Schools: America's PK–12 Public School Facilities 2025",
		By:  "Mary Filardo; 21st Century School Fund, National Council on School Facilities and International WELL Building Institute (2025, its upkeep figures corrected 27 January 2026)",
		URL: "https://www.21csf.org/uploads/pub/2025%20State%20of%20Schools%20020226_2.pdf", Read: "2026-11-25"},
	{ID: "gao-20-494", Title: "K-12 Education: School Districts Frequently Identified Multiple Building Systems Needing Updates or Replacement (GAO-20-494)",
		By:  "U.S. Government Accountability Office, June 2020",
		URL: "https://www.gao.gov/assets/gao-20-494.pdf", Read: "2026-11-25"},
	{ID: "lbnl-esco-2026", Title: "U.S. ESCO Industry: Industry Size and Recent Market Trends, 2022–2024",
		By:  "Lawrence Berkeley National Laboratory, September 2026",
		URL: "https://eta-publications.lbl.gov/sites/default/files/2026-09/lbnl_esco_market_survey_final_for_publication_090926.pdf", Read: "2026-11-25"},
	{ID: "blue-bird-2025", Title: "Annual Report, fiscal 2025 (with its Form 10-K for the year ended September 27, 2025)",
		By:  "Blue Bird Corporation",
		URL: "https://s23.q4cdn.com/152250413/files/doc_financials/2025/ar/2025-AR_BLUEBIRD-Final.pdf", Read: "2026-11-25"},
	{ID: "blue-bird-deck-2025", Title: "Investor Presentation, December 2025 (slide 8, from S&P Global Mobility's registrations)",
		By:  "Blue Bird Corporation",
		URL: "https://s23.q4cdn.com/152250413/files/doc_presentations/2025/Dec/06/BLBD-Investor-Presentation-December-2025.pdf", Read: "2026-11-25"},
	{ID: "asbc-2019", Title: "About the American School Bus Council (its own page, as the Internet Archive kept it on 12 February 2019; the council's domain has since passed to others)",
		By:  "American School Bus Council",
		URL: "https://web.archive.org/web/20190212170236/http://www.americanschoolbuscouncil.org:80/about/", Read: "2026-11-25"},
	{ID: "sbf-stats-2025", Title: "K-12 Student Transportation Statistics 2024-25",
		By:  "School Bus Fleet, 8 December 2025",
		URL: "https://www.schoolbusfleet.com/news/k-12-student-transportation-statistics-2024-25", Read: "2026-11-25"},
	{ID: "sbf-contractors-2026", Title: "The 20 Largest School Bus Contractors in 2026",
		By:  "School Bus Fleet, 28 May 2026",
		URL: "https://www.schoolbusfleet.com/articles/the-20-largest-school-bus-contractors-in-2026", Read: "2026-11-25"},
}

// AllSources are every source the pies name: the figures', the church and
// state laws' and courts', and each of the Supreme Court's decisions.
func AllSources() []Source {
	out := append([]Source{}, Sources...)
	out = append(out, churchSources...)
	return append(out, caseSources()...)
}

// SourceByID is the source named, if there is one.
func SourceByID(id string) (Source, bool) {
	for _, s := range AllSources() {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

// Sections is how a state got land for its schools, as Swift's Tables 2
// and 3 sort the 48 states of 1922.
type Sections int

const (
	// Original is the thirteen original states, which received no federal
	// land grant for common schools (Table 2).
	Original Sections = iota
	// Later is the five states admitted later that received none either,
	// containing no public lands (Table 2, page 8).
	Later
	// OwnAct is Tennessee, which set aside school lands by its own act
	// from a grant of 1806, never counted among the federal school grants
	// (page 9, Table 5's note 3).
	OwnAct
	// Sixteen is section 16 of every township (Table 3, group 1).
	Sixteen
	// SixteenThirtySix is sections 16 and 36 (Table 3, group 2).
	SixteenThirtySix
	// FourSections is sections 2, 16, 32 and 36 (Table 3, group 3).
	FourSections
)

// Grant is one state's lands available for schools as the General Land
// Office counted them for Swift in 1921, in acres: the school sections of
// Table 3, and Table 5's other lands that some states used for schools.
// Admitted is the year Table 2 gives for the five states admitted later
// that had no public lands; 0 for the rest.
type Grant struct {
	State                              string
	Sections                           Sections
	Admitted                           int
	Township, Salt, Improvement, Swamp float64
	Total                              float64 // Table 5's printed total for the state, 0 where it prints none
}

// Grants are the 48 states of 1922, Table 2's eighteen in its order, then
// Tennessee, then Table 5's 29 in its order. Table 3 prints Illinois's
// school sections as 966,320 acres; Table 5 prints 996,320, which alone
// makes Table 3's total of 73,155,075 and Table 5's row for Illinois add
// up (docs/proposals/common-schools.md, "A misprint, settled").
var Grants = []Grant{
	{State: "Connecticut", Sections: Original},
	{State: "Delaware", Sections: Original},
	{State: "Georgia", Sections: Original},
	{State: "Maryland", Sections: Original},
	{State: "Massachusetts", Sections: Original},
	{State: "New Hampshire", Sections: Original},
	{State: "New Jersey", Sections: Original},
	{State: "New York", Sections: Original},
	{State: "North Carolina", Sections: Original},
	{State: "Pennsylvania", Sections: Original},
	{State: "Rhode Island", Sections: Original},
	{State: "South Carolina", Sections: Original},
	{State: "Virginia", Sections: Original},
	{State: "Vermont", Sections: Later, Admitted: 1791},
	{State: "Kentucky", Sections: Later, Admitted: 1792},
	{State: "Maine", Sections: Later, Admitted: 1820},
	{State: "Texas", Sections: Later, Admitted: 1845},
	{State: "West Virginia", Sections: Later, Admitted: 1863},
	{State: "Tennessee", Sections: OwnAct},
	{State: "Alabama", Sections: Sixteen, Township: 911627, Salt: 23040, Improvement: 500000, Swamp: 439553.61, Total: 1874220.61},
	{State: "Arizona", Sections: FourSections, Township: 8093156, Total: 8093156},
	{State: "Arkansas", Sections: Sixteen, Township: 933778, Salt: 46080, Improvement: 500000, Swamp: 7686335.37, Total: 9166193.37},
	{State: "California", Sections: SixteenThirtySix, Township: 5534293, Improvement: 500000, Swamp: 2140765.19, Total: 8175058.19},
	{State: "Colorado", Sections: SixteenThirtySix, Township: 3685618, Salt: 46080, Improvement: 500000, Total: 4231698},
	{State: "Florida", Sections: Sixteen, Township: 975307, Improvement: 500000, Swamp: 20296443.32, Total: 21771750.32},
	{State: "Idaho", Sections: SixteenThirtySix, Township: 2963698, Total: 2963698},
	{State: "Illinois", Sections: Sixteen, Township: 996320, Salt: 121029, Improvement: 533368.24, Swamp: 1459708.27, Total: 3110425.51},
	{State: "Indiana", Sections: Sixteen, Township: 668578, Salt: 23040, Improvement: 1916804.56, Swamp: 1259150.93, Total: 3867573.49},
	{State: "Iowa", Sections: Sixteen, Township: 988196, Salt: 46080, Improvement: 500000, Swamp: 1195833.40, Total: 2730109.40},
	{State: "Kansas", Sections: SixteenThirtySix, Township: 2907520, Salt: 46080, Improvement: 500000, Total: 3453600},
	{State: "Louisiana", Sections: Sixteen, Township: 807271, Improvement: 500000, Swamp: 9413200.56, Total: 10720471.56},
	{State: "Michigan", Sections: Sixteen, Township: 1021867, Salt: 46080, Improvement: 500000, Swamp: 5679848.25, Total: 7247795.25},
	{State: "Minnesota", Sections: SixteenThirtySix, Township: 2874951, Salt: 46080, Improvement: 500000, Swamp: 4662967.10, Total: 8083998.10},
	{State: "Mississippi", Sections: Sixteen, Township: 824213, Improvement: 500000, Swamp: 3342640.78, Total: 4666853.78},
	{State: "Missouri", Sections: Sixteen, Township: 1221813, Salt: 46080, Improvement: 500000, Swamp: 3427700.39, Total: 5195593.39},
	{State: "Montana", Sections: SixteenThirtySix, Township: 5198258, Total: 5198258},
	{State: "Nebraska", Sections: SixteenThirtySix, Township: 2730951, Salt: 46080, Improvement: 500000, Total: 3277031},
	{State: "Nevada", Sections: SixteenThirtySix, Township: 2061967, Improvement: 500000, Total: 2561967},
	{State: "New Mexico", Sections: FourSections, Township: 4355662, Total: 4355662},
	{State: "North Dakota", Sections: SixteenThirtySix, Township: 2495396, Total: 2495396},
	{State: "Ohio", Sections: Sixteen, Township: 724266, Salt: 24216, Improvement: 1019071.98, Swamp: 26251.95, Total: 1793805.93},
	{State: "Oklahoma", Sections: SixteenThirtySix, Township: 1375000, Total: 1375000},
	{State: "Oregon", Sections: SixteenThirtySix, Township: 3399360, Salt: 46080, Improvement: 500000, Swamp: 264069.01, Total: 4209509.01},
	{State: "South Dakota", Sections: SixteenThirtySix, Township: 2733084, Total: 2733084},
	{State: "Utah", Sections: FourSections, Township: 5844196, Total: 5844196},
	{State: "Washington", Sections: SixteenThirtySix, Township: 2376391, Total: 2376391},
	{State: "Wisconsin", Sections: Sixteen, Township: 982329, Improvement: 500000, Swamp: 3356611.93, Total: 4838940.93},
	{State: "Wyoming", Sections: SixteenThirtySix, Township: 3470009, Total: 3470009},
}

// The totals Swift prints, which the grants must add up to: Table 3's
// school sections without Alaska, and Table 5's columns and grand total.
const (
	TownshipTotal    = 73155075.00
	SaltTotal        = 606045.00
	ImprovementTotal = 11469244.78
	SwampTotal       = 64651080.06
	GrandTotal       = 149881444.84
	// AlaskaReserved is Table 3's sections 16 and 36 reserved in Alaska,
	// "reserved but not granted; area estimated".
	AlaskaReserved = 21009209.00
	// TennesseeAcres is the school land Tennessee set aside by its own
	// act: "only 24,000 acres" (page 9).
	TennesseeAcres = 24000.00
)

// Divisions are the Census Bureau's nine divisions of the states, each
// with its states, as its map of regions and divisions lists them: the
// pies of many states open first into these.
var Divisions = []struct {
	Name   string
	States []string
}{
	{"New England", []string{"Connecticut", "Maine", "Massachusetts", "New Hampshire", "Rhode Island", "Vermont"}},
	{"Middle Atlantic", []string{"New Jersey", "New York", "Pennsylvania"}},
	{"East North Central", []string{"Illinois", "Indiana", "Michigan", "Ohio", "Wisconsin"}},
	{"West North Central", []string{"Iowa", "Kansas", "Minnesota", "Missouri", "Nebraska", "North Dakota", "South Dakota"}},
	{"South Atlantic", []string{"Delaware", "District of Columbia", "Florida", "Georgia", "Maryland", "North Carolina", "South Carolina", "Virginia", "West Virginia"}},
	{"East South Central", []string{"Alabama", "Kentucky", "Mississippi", "Tennessee"}},
	{"West South Central", []string{"Arkansas", "Louisiana", "Oklahoma", "Texas"}},
	{"Mountain", []string{"Arizona", "Colorado", "Idaho", "Montana", "Nevada", "New Mexico", "Utah", "Wyoming"}},
	{"Pacific", []string{"Alaska", "California", "Hawaii", "Oregon", "Washington"}},
}

// DivisionOf is the Census Bureau's division of a state, "" for none.
func DivisionOf(state string) string {
	for _, d := range Divisions {
		for _, s := range d.States {
			if s == state {
				return d.Name
			}
		}
	}
	return ""
}
