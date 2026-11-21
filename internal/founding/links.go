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
	// The Constitution, 1787.
	{From: "constitution", To: "declaration", How: Follows,
		Why: "The Constitution counts its years from the Declaration, dating itself “of the Independance of the United States of America the Twelfth”."},
	{From: "constitution", To: "articles", How: Replaced,
		Why: "Article VII put the Constitution in place of the Articles on the “Ratification of the Conventions of nine States”, where the Articles' own Article XIII had made “the union shall be perpetual” and allowed no alteration unless “agreed to in a congress of the united states” and then confirmed by every state."},
	// The Bill of Rights, 1791.
	{From: "rights", To: "magna-carta", How: DrewOn,
		Why: "The Fifth Amendment's “nor be deprived of life, liberty, or property, without due process of law” descends from chapter 39, which lets no freeman be taken or imprisoned “except by the lawful judgment of his peers or by the law of the land”."},
	{From: "rights", To: "english-rights", How: DrewOn,
		Why: "The Eighth Amendment's “Excessive bail shall not be required, nor excessive fines imposed, nor cruel and unusual punishments inflicted” follows the Act's words; the First's right “to petition the Government for a redress of grievances” and the Second's “right of the people to keep and bear Arms” answer its “Right of the Subjects to petition the King” and the subjects' “Arms for their Defence”."},
	{From: "rights", To: "virginia", How: DrewOn,
		Why: "Madison's amendments take up Mason's sections: the free exercise of religion, “the freedom of the press”, trial by jury, no general warrants, and no excessive bail, excessive fines or cruel and unusual punishments."},
	{From: "rights", To: "constitution", How: Amends,
		Why: "The ten are Articles “in addition to, and Amendment of the Constitution of the United States of America”, proposed under its Article V and carried in it as Amendments I to X, word for word."},
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
	{From: "privacy", To: "foia", How: Beside,
		Why: "The Privacy Act is § 552a, the section after the Freedom of Information Act's § 552 in the Code, and lets a record out without the person's consent only where, among its exceptions, disclosure is “required under section 552 of this title”."},
}
