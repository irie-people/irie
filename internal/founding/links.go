package founding

import "github.com/irie-people/irie/internal/ipc"

// Link is how one text bears on an earlier one: an edge of the graph the
// chain carries with the texts (docs/proposals/founding-documents.md, "The
// graph"). From is the later text and To the earlier, so each edge points
// back along the lineage. How is one of a few plain words, and Why says in
// a sentence what the one took from the other, quoting the texts where it
// can: a quotation in curly double quotes is in one of the two texts word
// for word, which the tests check, so each inference can be checked
// against the chain itself.
type Link = ipc.FoundingLink

// How a text bears on an earlier one.
const (
	DrewOn   = "drew on"  // took words or ideas from it
	Amends   = "amends"   // changes its text
	Replaced = "replaced" // took its place
	Beside   = "beside"   // stands beside it in the same law
	Follows  = "follows"  // came after it and builds on it, without its words
)

// Hows are the kinds of link, in the order they print.
var Hows = []string{DrewOn, Amends, Replaced, Beside, Follows}

// Links are the edges, each from a later text back to an earlier one, in
// the order of the later text and then of the earlier.
var Links = []Link{
	// The Petition of Right, 1628, which recites Magna Carta and names due
	// process of law.
	{From: "petition", To: "magna-carta", How: DrewOn,
		Why: "Section III recites “the Statute called The great Charter of the Liberties of England”, that no freeman may be taken or imprisoned “but by the lawfull Judgment of his Peeres or by the Law of the Land”: the charter's “except by the lawful judgment of his peers or by the law of the land”."},
	// Virginia, June 1776: Mason's declaration, which the Declaration of
	// Independence and the Bill of Rights both drew on.
	{From: "virginia", To: "magna-carta", How: DrewOn,
		Why: "Section 8 lets no man be deprived of his liberty “except by the law of the land or the judgment of his peers”, chapter 39's “except by the lawful judgment of his peers or by the law of the land”."},
	{From: "virginia", To: "english-rights", How: DrewOn,
		Why: "Section 9, “That excessive bail ought not to be required, nor excessive fines imposed, nor cruel and unusual punishments inflicted”, is the Act's “excessive Baile ought not to be required nor excessive Fines imposed nor cruell and unusuall Punishments inflicted” in Virginia's spelling."},
	// The Declaration of Independence, July 1776.
	{From: "declaration", To: "virginia", How: DrewOn,
		Why: "Jefferson's “all men are created equal” and “certain unalienable Rights” follow Mason's “all men are by nature equally free and independent and have certain inherent rights”, adopted three weeks before."},
	// The Articles of Confederation, 1777.
	{From: "articles", To: "declaration", How: Follows,
		Why: "The Articles bind the thirteen “Free and Independent States” the Declaration made into “a firm league of friendship with each other”, agreed by the same Congress sixteen months on."},
	// The Northwest Ordinance, July 1787.
	{From: "northwest", To: "habeas", How: DrewOn,
		Why: "Article 2 promises the territory's inhabitants “the benefits of the writ of habeas corpus”, the writ the Act of 1679 made good against delay, with its rules for “Writts of Habeas Corpus” and the penalties for refusing them."},
	{From: "northwest", To: "articles", How: Follows,
		Why: "The Ordinance is an act of the Articles' Congress, ordained “by the United States in Congress assembled”, the body the Articles name “the united states, in congress assembled”; its articles of compact carry a bill of rights for the territory."},
	// The Constitution, 1787.
	{From: "constitution", To: "habeas", How: DrewOn,
		Why: "Article I, Section 9's “The Privilege of the Writ of Habeas Corpus shall not be suspended” keeps the writ the Act of 1679, “for the better secureing the Liberty of the Subject”, made good against imprisonment without trial."},
	{From: "constitution", To: "northwest", How: DrewOn,
		Why: "The Thirteenth Amendment's “Neither slavery nor involuntary servitude, except as a punishment for crime whereof the party shall have been duly convicted” is Article 6 of the Ordinance, “There shall be neither slavery nor involuntary servitude in the said territory, otherwise than in the punishment of crimes whereof the party shall have been duly convicted”, carried to the whole country."},
	{From: "constitution", To: "declaration", How: Follows,
		Why: "The Constitution counts its years from the Declaration, dating itself “of the Independance of the United States of America the Twelfth”."},
	{From: "constitution", To: "articles", How: Replaced,
		Why: "Article VII put the Constitution in place of the Articles on the “Ratification of the Conventions of nine States”, where the Articles' own Article XIII had made “the union shall be perpetual” and allowed no alteration unless “agreed to in a congress of the united states” and then confirmed by every state."},
	// The Bill of Rights, 1791.
	{From: "rights", To: "magna-carta", How: DrewOn,
		Why: "The Fifth Amendment's “nor be deprived of life, liberty, or property, without due process of law” descends from chapter 39, which lets no freeman be taken or imprisoned “except by the lawful judgment of his peers or by the law of the land”."},
	{From: "rights", To: "petition", How: DrewOn,
		Why: "The Fifth Amendment's “without due process of law” is Section IV's “by due p[ro]cesse of Lawe”, and the Third Amendment's “No Soldier shall, in time of peace be quartered in any house, without the consent of the Owner” answers Section VI's grievance that “the inhabitants against their wills have been compelled to receive them into their houses”."},
	{From: "rights", To: "english-rights", How: DrewOn,
		Why: "The Eighth Amendment's “Excessive bail shall not be required, nor excessive fines imposed, nor cruel and unusual punishments inflicted” follows the Act's words; the First's right “to petition the Government for a redress of grievances” and the Second's “right of the people to keep and bear Arms” answer its “Right of the Subjects to petition the King” and the subjects' “Arms for their Defence”."},
	{From: "rights", To: "northwest", How: DrewOn,
		Why: "The Ordinance's Article 2 promised the territory “the benefits of the writ of habeas corpus, and of the trial by jury” and that “no cruel or unusual punishments shall be inflicted”; the Seventh Amendment keeps “the right of trial by jury” and the Eighth forbids “cruel and unusual punishments”."},
	{From: "rights", To: "virginia", How: DrewOn,
		Why: "Madison's amendments take up Mason's sections: the free exercise of religion, “the freedom of the press”, trial by jury, no general warrants, and no excessive bail, excessive fines or cruel and unusual punishments."},
	{From: "rights", To: "constitution", How: Amends,
		Why: "The ten are Articles “in addition to, and Amendment of the Constitution of the United States of America”, proposed under its Article V and carried in it as Amendments I to X, word for word."},
	// The Declaration of Sentiments, Seneca Falls, July 1848.
	{From: "seneca", To: "declaration", How: DrewOn,
		Why: "“We hold these truths to be self-evident; that all men and women are created equal” rewrites the Declaration's “We hold these truths to be self-evident, that all men are created equal”, and its grievances take the Declaration's form, a list of “He has”, for women against men: “He has never permitted her to exercise her inalienable right to the elective franchise”."},
	// The Emancipation Proclamation, January 1863.
	{From: "emancipation", To: "constitution", How: Follows,
		Why: "Lincoln acts “by virtue of the power in me vested as Commander-in-Chief, of the Army and Navy of the United States”, Article II's “The President shall be Commander in Chief of the Army and Navy of the United States”, as a war measure against the rebellion."},
	// The Gettysburg Address, November 1863.
	{From: "gettysburg", To: "emancipation", How: Follows,
		Why: "Ten months after the Proclamation declared that persons held as slaves “are, and henceforward shall be free”, the Address resolves that the nation “shall have a new birth of freedom”."},
	{From: "gettysburg", To: "declaration", How: Follows,
		Why: "“Four score and seven years ago” counts from the Declaration, and the Address holds the nation “dedicated to the proposition that all men are created equal”, the Declaration's “all men are created equal”, as what the war would decide."},
	// The Universal Declaration of Human Rights, 1948.
	{From: "udhr", To: "declaration", How: DrewOn,
		Why: "Article 1's “All human beings are born free and equal in dignity and rights” and the Preamble's “equal and inalienable rights” carry the Declaration's “all men are created equal” and “unalienable Rights” to everyone."},
	{From: "udhr", To: "rights", How: DrewOn,
		Why: "Articles 18 to 20 declare the First Amendment's freedoms of religion, expression and assembly for everyone; Article 5's “cruel, inhuman or degrading treatment or punishment” takes up the Eighth's “cruel and unusual punishments”; Articles 9 to 11 the Fifth and Sixth's fair trial."},
	// The Freedom of Information Act, 1966.
	{From: "foia", To: "constitution", How: Follows,
		Why: "An Act of Congress under Article I, set out as 5 U.S.C. § 552, which makes each agency's records available “to any person” unless one of its exemptions holds."},
	{From: "foia", To: "udhr", How: Follows,
		Why: "Article 19 declares everyone's freedom “to seek, receive and impart information and ideas through any media and regardless of frontiers”; eighteen years on, the Act gives a right to the government's own records."},
	// The Privacy Act of 1974.
	{From: "privacy", To: "constitution", How: Follows,
		Why: "An Act of Congress under Article I, set out as 5 U.S.C. § 552a, on the records agencies keep about individuals."},
	{From: "privacy", To: "udhr", How: Follows,
		Why: "Article 12 holds that “No one shall be subjected to arbitrary interference with his privacy”; the Act gives that right form for the records the government keeps on a person."},
	// India's Right to Information Act, 2005.
	{From: "rti", To: "udhr", How: Follows,
		Why: "The Act's long title sets out “the practical regime of right to information for citizens to secure access to information under the control of public authorities”, giving form in law to Article 19's freedom “to seek, receive and impart information and ideas through any media and regardless of frontiers”."},
	{From: "rti", To: "foia", How: Follows,
		Why: "Section 3, “all citizens shall have the right to information”, does for India what FOIA did for the United States in 1966, where an agency's records go “to any person”; both set out what may be withheld and who hears an appeal."},
	{From: "privacy", To: "foia", How: Beside,
		Why: "The Privacy Act is § 552a, the section after the Freedom of Information Act's § 552 in the Code, and lets a record out without the person's consent only where, among its exceptions, disclosure is “required under section 552 of this title”."},
}
