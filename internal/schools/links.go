package schools

import "github.com/irie-people/irie/internal/founding"

// Link is how one text bears on an earlier one, as in the charters
// (internal/founding, links.go): From is the later text and To the
// earlier, How one of founding.Hows, and Why a sentence whose quotations,
// in curly double quotes, are in one of the two texts word for word,
// which the tests check.
type Link = founding.Link

// Links are the edges between the texts, each from a later text back to
// an earlier one.
var Links = []Link{
	// How public schools began: each later text back to the one it grew
	// from, quoting both.
	{From: "massachusetts-1647", To: "massachusetts-1642", How: founding.Follows,
		Why: "Five years after the colony charged each town's chosen men to see that children could “read & understand the principles of religion & the capitall lawes of this country”, it made every town of fifty households “appoint one wthin their towne to teach all such children as shall resort to him to write & reade”, and every town of a hundred families keep a grammar school."},
	{From: "north-carolina-1776", To: "pennsylvania-1776", How: founding.DrewOn,
		Why: "Article XLI takes Pennsylvania's Section 44 of three months before almost word for word: “a school or schools shall be established by the Legislature, for the convenient instruction of youth, with such salaries to the masters, paid by the public, as may enable them to instruct at low prices” for “A school or schools shall be established in each county by the legislature, for the convenient instruction of youth, with such salaries to the masters paid by the public, as may enable them to instruct youth at low prices”."},
	{From: "georgia-1777", To: "pennsylvania-1776", How: founding.Follows,
		Why: "Five months after Pennsylvania's “A school or schools shall be established in each county by the legislature”, Georgia orders that “Schools shall be erected in each county, and supported at the general expense of the State”."},
	{From: "vermont-1777", To: "pennsylvania-1776", How: founding.DrewOn,
		Why: "Section XL is Pennsylvania's Section 44 made for towns: “A school or schools shall be established in each town, by the legislature, for the convenient instruction of youth” for “A school or schools shall be established in each county by the legislature, for the convenient instruction of youth”, the masters paid “by each town” where Pennsylvania's are “paid by the public”."},
	{From: "massachusetts-1780", To: "massachusetts-1647", How: founding.Follows,
		Why: "Chapter V charges the legislature and magistrates to cherish “public schools and grammar schools in the towns”, the schools the colony's order of 1647 had every town keep, a teacher in each town of fifty households and, where a town grew to a hundred families, “they shall set up a gram̄er schoole”."},
	{From: "new-hampshire-1784", To: "massachusetts-1780", How: founding.DrewOn,
		Why: "New Hampshire's article on literature is Massachusetts's of four years before in other words: “KNOWLEDGE, and learning, generally diffused through a community, being essential to the preservation of a free government” for “Wisdom, and knowledge, as well as virtue, diffused generally among the body of the people, being necessary for the preservation of their rights and liberties”, and the duty “to cherish the interest of literature and the sciences, and all seminaries and public schools” for “to cherish the interests of literature and the sciences, and all seminaries of them”."},
	{From: "land-ordinance", To: "massachusetts-1647", How: founding.Follows,
		Why: "Where the colony's order made a teacher the charge of “evry towneship in this iurisdiction” of fifty householders, the Ordinance reserves in every township a lot “for the maintenance of public schools, within the said township”: schools kept township by township."},
	{From: "northwest", To: "land-ordinance", How: founding.Follows,
		Why: "Two years after the Land Ordinance reserved “the lot N 16, of every township, for the maintenance of public schools”, Congress's ordinance for the territory's government declared that “schools and the means of education shall forever be encouraged”."},
	{From: "ohio-company", To: "land-ordinance", How: founding.DrewOn,
		Why: "The terms have the tract laid into lots “according to the land Ordinance of the 20th. of May 1785” and give “The lot N 16 in each township or fractional part of a township” “for the purposes contained in the said Ordinance”: the Ordinance's “lot N 16, of every township, for the maintenance of public schools”."},
	{From: "ohio-enabling-act", To: "land-ordinance", How: founding.DrewOn,
		Why: "The Act offers the new state “the section, number sixteen, in every township” “for the use of schools”, the lot the Land Ordinance had reserved in every township: “the lot N 16, of every township, for the maintenance of public schools”."},
	{From: "ohio-enabling-act", To: "northwest", How: founding.Follows,
		Why: "The state it enables is made from the territory the Ordinance governed, its constitution to be “republican, and not repugnant to the ordinance of the thirteenth of July, one thousand seven hundred and eighty-seven”."},
	{From: "massachusetts-1852", To: "massachusetts-1642", How: founding.Follows,
		Why: "Two centuries after the colony had towns see that children could “read & understand the principles of religion & the capitall lawes of this country”, the Commonwealth made every person with a child of eight to fourteen “send such child to some public school” at least twelve weeks a year."},
	// Church and state: the links the separation-of-church-and-state
	// card made with its texts, each quotation checked in both texts.
	{From: "jefferson-danbury", To: "rights", How: founding.DrewOn,
		Why: "Jefferson quotes the First Amendment, that the legislature should “make no law respecting an establishment of religion, or prohibiting the free exercise thereof”, and reads it as “building a wall of separation between Church & State”."},
	{From: "jefferson-danbury", To: "virginia-statute", How: founding.Follows,
		Why: "The letter's “the legitimate powers of government reach actions only, & not opinions” is the rule of Jefferson's own Statute, that government's officers are “to interfere when principles break out into overt acts against peace and good order”."},
	{From: "equal-access", To: "rights", How: founding.Follows,
		Why: "The Act opens a high school's clubs to student meetings whatever “the religious, political, philosophical, or other content of the speech”, while forbidding government “to influence the form or content of any prayer or other religious activity”: free exercise and free speech, kept apart from an establishment."},
	{From: "school-prayer", To: "rights", How: founding.Follows,
		Why: "The section has the Secretary of Education issue guidance “on constitutionally protected prayer in public elementary schools and secondary schools”, and each district certify that no policy of its “prevents, or otherwise denies participation in” that prayer, or lose the Act's funds."},
	{From: "louisiana-ten-commandments", To: "northwest", How: founding.DrewOn,
		Why: "Finding (8) quotes Article 3, that “schools and the means of education shall forever be encouraged”, and Paragraph B(4)(a) lets schools display the Ordinance beside the Ten Commandments; the finding's “extended the fundamental principles of civil and religious liberty” is the Ordinance's “for extending the fundamental principles of civil and religious liberty” put in the past tense."},
	{From: "louisiana-ten-commandments", To: "mayflower", How: founding.DrewOn,
		Why: "Finding (7) says the Compact “made a Covenant with Almighty God” to “form a civil body politic”; the Compact's own words are that its signers, “in the Presence of God and one another”, “covenant and combine ourselves together into a civil Body Politick”."},
	{From: "arkansas-ten-commandments", To: "louisiana-ten-commandments", How: founding.DrewOn,
		Why: "Act 573's poster carries Louisiana's text of the Ten Commandments line for line, its first line in lower case where Louisiana's has capitals, and asks for a larger poster, “at least sixteen inches by twenty inches”, where Louisiana asks for one “at least eleven inches by fourteen inches”."},
	{From: "texas-ten-commandments", To: "louisiana-ten-commandments", How: founding.DrewOn,
		Why: "Section 1.0041(c) requires Louisiana's text of the Ten Commandments word for word, on a poster “at least 16 inches wide and 20 inches tall” where Louisiana asks for one “at least eleven inches by fourteen inches”."},
	{From: "alabama-ten-commandments", To: "louisiana-ten-commandments", How: founding.DrewOn,
		Why: "Alabama's findings take Louisiana's words: in both the Compact “was the first purely American document of self-government and affirmed the link between civil society and God”, and both tell school boards they are “encouraged to use documents that are printed and made available to the schools free of charge”."},
	{From: "alabama-ten-commandments", To: "northwest", How: founding.DrewOn,
		Why: "The poster quotes Article 3, that “schools and the means of education shall forever be encouraged”, and says the Ordinance “extended the fundamental principles of civil and religious liberty”, where the Ordinance has “for extending the fundamental principles of civil and religious liberty”."},
	{From: "alabama-ten-commandments", To: "mayflower", How: founding.DrewOn,
		Why: "The findings and the poster call the Compact a covenant with “Almighty God” to “form a civil body politic”; the Compact's own words are that its signers, “in the Presence of God and one another”, “covenant and combine ourselves together into a civil Body Politick”."},
	{From: "alabama-ten-commandments", To: "declaration", How: founding.DrewOn,
		Why: "The findings and the poster quote the Declaration, that all men are “created equal” and “endowed by their Creator with certain unalienable Rights”."},
	{From: "alabama-ten-commandments", To: "constitution", How: founding.DrewOn,
		Why: "The poster quotes the Preamble's “the Blessings of Liberty to ourselves and our Posterity”, and the findings the Constitution's date, “in the Year of our Lord one thousand seven hundred and eighty-seven”, which the Constitution writes “in the Year of our Lord one thousand seven hundred and Eighty seven”."},
}
