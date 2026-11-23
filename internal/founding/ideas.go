package founding

// The rights and ideas the charters name, the graph's third kind of node
// (graph.go). Each is found where its words are, whole words in the texts'
// own passages, whatever their spelling: the page shows the words it
// found, so each tie says why it is there. The words are ours, chosen
// from the texts so as to find the passages that grant, guard or name the
// right, and not the ones that only use a word of it ("vote" alone would
// find every Senate vote); the tests hold each idea to texts it must find.

// Families are the groups the ideas fall in, in the order the page lists
// them.
var Families = []Family{
	{ID: "expression", Name: "Speech, press and belief"},
	{ID: "justice", Name: "Courts and punishment"},
	{ID: "equality", Name: "Equality and dignity"},
	{ID: "liberty", Name: "Life, liberty and the home"},
	{ID: "welfare", Name: "Work, school and welfare"},
	{ID: "government", Name: "Government by consent"},
}

// Ideas are the rights and ideas, by family.
var Ideas = []Idea{
	// Speech, press and belief.
	{ID: "idea/speech", Name: "Freedom of speech", Family: "expression", Words: []string{
		"freedom of speech", "liberty of speech", "freedom of expression", "speech and debate", "speech and debates",
		"speech or debate", "speak write and publish", "speak write and print", "freedom of opinion and expression",
		"freedom of thought and expression",
	}},
	{ID: "idea/press", Name: "Freedom of the press", Family: "expression", Words: []string{
		"freedom of the press", "liberty of the press", "or of the press", "freedom of writing and of the press",
		"in writing or in print", "through any media",
	}},
	{ID: "idea/religion", Name: "Freedom of religion", Family: "expression", Words: []string{
		"free exercise of religion", "free exercise thereof", "freedom of religion", "freedom of thought conscience and religion",
		"dictates of conscience", "dictates of their own consciences", "dictates of his own conscience",
		"rights of conscience", "liberty of conscience", "religious worship", "mode of worship", "religious opinions",
		"religious sentiments", "religious profession", "establishment of religion", "religious test",
	}},
	{ID: "idea/assembly", Name: "Assembly and association", Family: "expression", Words: []string{
		"peaceably to assemble", "peaceable assembly", "peaceful assembly", "freedom of assembly", "assemble together",
		"right to assemble", "freedom of association", "freedom of peaceful assembly",
	}},
	{ID: "idea/petition", Name: "Petition", Family: "expression", Words: []string{
		"petition the king", "petition the government", "petitioned for redress", "petitions or remonstrances",
		"petition or remonstrance", "right to petition", "petitioning",
	}},
	{ID: "idea/information", Name: "Right to information", Family: "expression", Words: []string{
		"right to information", "access to information", "seek receive and impart information", "receive and impart information",
		"freedom of information", "make available to the public", "right of access to information", "access to all archives",
		"gain access to his record",
	}},
	{ID: "idea/privacy", Name: "Privacy", Family: "expression", Words: []string{
		"privacy", "private and family life",
	}},

	// Courts and punishment.
	{ID: "idea/due-process", Name: "Due process and the law of the land", Family: "justice", Words: []string{
		"due process of law", "due p[ro]cesse of lawe", "by the law of the land", "or the law of the land", "judgment of his peers",
		"procedure prescribed by law", "such procedure as are established by law", "express law of the country",
	}},
	{ID: "idea/habeas", Name: "Habeas corpus and arrest", Family: "justice", Words: []string{
		"habeas corpus", "arbitrary arrest", "arbitrary arrest or detention", "arbitrary arrest detention or exile",
		"taken or imprisoned",
	}},
	{ID: "idea/jury", Name: "Trial by jury", Family: "justice", Words: []string{
		"jury", "juries", "jurors",
	}},
	{ID: "idea/fair-trial", Name: "A fair and speedy trial", Family: "justice", Words: []string{
		"speedy and public trial", "speedy trial", "speedy public trial", "fair and public hearing", "fair hearing",
		"presumed innocent", "independent and impartial tribunal", "impartial tribunal",
	}},
	{ID: "idea/counsel", Name: "Counsel and defence", Family: "justice", Words: []string{
		"assistance of counsel", "himself and his counsel", "himself and his council", "himself or his counsel",
		"himself or his council", "legal assistance", "guarantees necessary for his defence", "defend himself in person",
	}},
	{ID: "idea/self-incrimination", Name: "No witness against oneself", Family: "justice", Words: []string{
		"witness against himself", "testify against himself", "evidence against himself", "to confess guilt",
		"confesse any crime against himselfe",
	}},
	{ID: "idea/jeopardy", Name: "Double jeopardy", Family: "justice", Words: []string{
		"twice put in jeopardy", "tried or punished again", "twise sentenced", "tried or punished twice",
	}},
	{ID: "idea/bail", Name: "Bail and fines", Family: "justice", Words: []string{
		"excessive bail", "excessive fines", "shall not be amerced", "excessive fine",
	}},
	{ID: "idea/punishment", Name: "Cruel punishment and torture", Family: "justice", Words: []string{
		"cruel and unusual", "cruel or unusual", "torture", "inhuman or degrading", "inhumane barbarous or cruel",
	}},
	{ID: "idea/searches", Name: "Searches and seizures", Family: "justice", Words: []string{
		"unreasonable searches", "searches and seizures", "search and seizure", "general warrants", "search suspected places",
	}},
	{ID: "idea/ex-post-facto", Name: "No law after the fact", Family: "justice", Words: []string{
		"ex post facto", "bill of attainder", "did not constitute a penal offence", "did not constitute a criminal offence",
		"actions done before the existence of such laws", "heavier penalty",
	}},

	// Equality and dignity.
	{ID: "idea/rights", Name: "Unalienable and human rights", Family: "equality", Words: []string{
		"unalienable rights", "inalienable rights", "inherent rights", "natural rights", "rights of man", "human rights",
		"fundamental freedoms", "natural inherent and inalienable rights", "natural essential and unalienable rights",
		"unalienable right", "inalienable right",
	}},
	{ID: "idea/equality", Name: "Equality", Family: "equality", Words: []string{
		"created equal", "equally free", "free and equal", "equal protection", "equal before the law", "equality before the law",
		"equal rights", "equal in dignity and rights", "equal protection of the laws",
	}},
	{ID: "idea/slavery", Name: "Slavery and servitude", Family: "equality", Words: []string{
		"slavery", "slaves", "slave", "involuntary servitude", "slave trade", "held to service or labour", "slaverie",
	}},
	{ID: "idea/discrimination", Name: "Against discrimination", Family: "equality", Words: []string{
		"discrimination", "on account of race", "without distinction of any kind", "distinction of any kind",
	}},
	{ID: "idea/women", Name: "Women", Family: "equality", Words: []string{
		"women", "woman", "on account of sex", "on the basis of sex", "because of sex",
	}},
	{ID: "idea/children", Name: "Children", Family: "equality", Words: []string{
		"child", "children",
	}},
	{ID: "idea/disability", Name: "Disability", Family: "equality", Words: []string{
		"disability", "disabilities", "disabled", "reasonable accommodation", "reasonable accommodations",
	}, Not: []string{
		// The President's inability, a disqualification from office, and
		// a chairman's absence: other senses of the word.
		"until the disability be removed", "constitutional disability", "remove such disability", "disabled from",
		"absence or disability", "losses or disabilities",
	}},
	{ID: "idea/asylum", Name: "Asylum and refugees", Family: "equality", Words: []string{
		"asylum", "refugee", "refugees", "persecution",
	}},
	{ID: "idea/nationality", Name: "Nationality and citizenship", Family: "equality", Words: []string{
		"nationality", "naturalization", "naturalized", "citizenship", "natural born citizen",
	}},

	// Life, liberty and the home.
	{ID: "idea/life-liberty", Name: "Life, liberty and happiness", Family: "liberty", Words: []string{
		"pursuit of happiness", "happiness and safety", "life liberty", "life and liberty", "lives and liberties",
		"right to life", "deprived of life",
	}},
	{ID: "idea/property", Name: "Property", Family: "liberty", Words: []string{
		"property", "private property", "just compensation", "disseised",
	}},
	{ID: "idea/movement", Name: "Freedom of movement", Family: "liberty", Words: []string{
		"freedom of movement", "liberty of movement", "leave any country", "return to his country",
		"free ingress and regress", "to leave our kingdom and to return",
	}},
	{ID: "idea/family", Name: "Marriage and family", Family: "liberty", Words: []string{
		"marry", "marriage", "found a family", "family life",
	}},
	{ID: "idea/quartering", Name: "Soldiers in the home", Family: "liberty", Words: []string{
		"quartered in any house", "quartering large bodies", "receive them into their houses", "quartering",
	}},
	{ID: "idea/arms", Name: "Bearing arms", Family: "liberty", Words: []string{
		"keep and bear arms", "to keep and to bear arms", "bear arms", "arms for their defence", "well regulated militia",
	}},

	// Work, school and welfare.
	{ID: "idea/work", Name: "Work and fair pay", Family: "welfare", Words: []string{
		"right to work", "conditions of work", "equal pay", "equal remuneration", "trade union", "trade unions",
		"rest and leisure", "free choice of employment", "equal employment opportunity", "unlawful employment practice",
		"work and employment", "wage earning employment", "full employment",
	}},
	{ID: "idea/education", Name: "Education", Family: "welfare", Words: []string{
		"education", "schools",
	}},
	{ID: "idea/health", Name: "Health and welfare", Family: "welfare", Words: []string{
		"right to health", "highest attainable standard of", "physical and mental health", "standard of living",
		"social security", "medical care", "health care", "health services", "health and safety",
	}},

	// Government by consent.
	{ID: "idea/consent", Name: "Consent of the governed", Family: "government", Words: []string{
		"consent of the governed", "derived from the people", "derived from them", "body politic", "social compact",
		"we the people", "we the peoples", "will of the people", "source of all sovereignty",
		"of the people by the people for the people",
	}},
	{ID: "idea/revolution", Name: "The right to alter or abolish", Family: "government", Words: []string{
		"alter or to abolish", "alter or abolish", "reform alter or abolish", "reform alter or totally change",
		"resistance of oppression", "resistance to oppression", "rebellion against tyranny and oppression",
	}},
	{ID: "idea/vote", Name: "The vote and free elections", Family: "government", Words: []string{
		"right to vote", "elective franchise", "suffrage", "free elections", "elections ought to be free",
		"election of members of parliament ought to be free", "right of suffrage", "genuine elections", "periodic elections",
		"voting qualification", "to vote shall not be denied",
	}, Not: []string{
		// The States' votes in the Senate, and regional bodies' in the
		// Conference of States Parties.
		"equal suffrage in the senate", "right to vote in the conference", "exercise its right to vote",
	}},
	{ID: "idea/taxes", Name: "Taxes and consent", Family: "government", Words: []string{
		"tax", "taxes", "taxed", "taxation", "scutage", "tallage", "levying money", "public contributions",
		"duties imposts and excises",
	}, Not: []string{
		// Tax law the Acts only point to: an exemption, tax refunds and
		// records, the collection of a tax as law enforcement.
		"exempt from taxation", "tax administration", "tax refund", "tax information", "income tax", "collection of any tax",
	}},
	{ID: "idea/separation", Name: "Separation of powers", Family: "government", Words: []string{
		"separate and distinct from the judiciary", "separation of powers", "executive and judicial powers",
		"legislative department", "executive power shall be vested", "judicial power of the united states shall be vested",
		"legislative powers herein granted",
	}},
	{ID: "idea/armies", Name: "Standing armies", Family: "government", Words: []string{
		"standing army", "standing armies", "subordination to and governed by the civil power", "civil power",
	}},
}
