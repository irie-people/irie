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
	// Magna Carta as Edward I confirmed it in 1297, the charter on the
	// statute book.
	{From: "magna-carta-1297", To: "magna-carta", How: Follows,
		Why: "Edward's confirmation of 1297 puts on the statute book the charter of 1215 as Henry III cut it in 1225, 37 chapters for 63; its chapter 29 is 1215's chapters 39 and 40, “but by lawful Judgment of his Peers, or by the Law of the Land” for “except by the lawful judgment of his peers or by the law of the land”."},
	// The Petition of Right, 1628, which recites Magna Carta and names due
	// process of law.
	{From: "petition", To: "magna-carta-1297", How: DrewOn,
		Why: "Section III recites the statute book's words: “no Freeman may be taken or imprisoned or be disseised of his Freehold or Liberties or his free Customes or be outlawed or exiled or in any manner destroyed” is chapter 29's “NO Freeman shall be taken, or imprisoned, or be disseised of his Freehold, or Liberties, or free Customs, or be outlawed, or exiled, or any otherwise destroyed”."},
	{From: "petition", To: "magna-carta", How: DrewOn,
		Why: "Section III recites “the Statute called The great Charter of the Liberties of England”, that no freeman may be taken or imprisoned “but by the lawfull Judgment of his Peeres or by the Law of the Land”: the charter's “except by the lawful judgment of his peers or by the law of the land”."},
	// The Massachusetts Body of Liberties, 1641, the colony's first code.
	{From: "body-of-liberties", To: "magna-carta-1297", How: DrewOn,
		Why: "Liberty 1, that no man's life, name, person, goods or estate be touched “unlesse it be by vertue or equitie of some expresse law of the Country waranting the same”, is the colony's chapter 29, “NO Freeman shall be taken, or imprisoned, or be disseised of his Freehold, or Liberties, or free Customs, or be outlawed, or exiled, or any otherwise destroyed” but by the law of the land."},
	// Virginia, June 1776: Mason's declaration, which the Declaration of
	// Independence and the Bill of Rights both drew on.
	{From: "virginia", To: "magna-carta", How: DrewOn,
		Why: "Section 8 lets no man be deprived of his liberty “except by the law of the land or the judgment of his peers”, chapter 39's “except by the lawful judgment of his peers or by the law of the land”."},
	{From: "virginia", To: "english-rights", How: DrewOn,
		Why: "Section 9, “That excessive bail ought not to be required, nor excessive fines imposed, nor cruel and unusual punishments inflicted”, is the Act's “excessive Baile ought not to be required nor excessive Fines imposed nor cruell and unusuall Punishments inflicted” in Virginia's spelling."},
	// The Declaration of Independence, July 1776.
	{From: "declaration", To: "virginia", How: DrewOn,
		Why: "Jefferson's “all men are created equal” and “certain unalienable Rights” follow Mason's “all men are by nature equally free and independent and have certain inherent rights”, adopted three weeks before."},
	// The Pennsylvania Declaration of Rights, September 1776.
	{From: "pennsylvania", To: "virginia", How: DrewOn,
		Why: "Article I's “That all men are born equally free and independent, and have certain natural, inherent and inalienable rights” follows Mason's “all men are by nature equally free and independent and have certain inherent rights”, three months on."},
	// The Articles of Confederation, 1777.
	{From: "articles", To: "declaration", How: Follows,
		Why: "The Articles bind the thirteen “Free and Independent States” the Declaration made into “a firm league of friendship with each other”, agreed by the same Congress sixteen months on."},
	// The Massachusetts Declaration of Rights, 1780, John Adams's.
	{From: "massachusetts", To: "body-of-liberties", How: Follows,
		Why: "A hundred and forty years on, Article XII keeps the colony's first promise, that no one be punished but by law: liberty 1's “by vertue or equitie of some expresse law of the Country” became “but by the judgment of his peers, or the law of the land”."},
	{From: "massachusetts", To: "virginia", How: DrewOn,
		Why: "Article I's “All men are born free and equal, and have certain natural, essential, and unalienable rights” follows Mason's “all men are by nature equally free and independent and have certain inherent rights”, four years on."},
	{From: "massachusetts", To: "english-rights", How: DrewOn,
		Why: "Article XXVI, “No magistrate or court of law, shall demand excessive bail or sureties, impose excessive fines, or inflict cruel or unusual punishments”, is the Act's “excessive Baile ought not to be required nor excessive Fines imposed nor cruell and unusuall Punishments inflicted”."},
	// The Virginia Statute for Religious Freedom, January 1786.
	{From: "virginia-statute", To: "virginia", How: DrewOn,
		Why: "The Statute makes law of Section 16's “all men are equally entitled to the free exercise of religion”: “no man shall be compelled to frequent or support any religious worship, place, or ministry whatsoever”, and “all men shall be free to profess, and by argument to maintain, their opinion in matters of religion”."},
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
	// The Declaration of the Rights of Man and of the Citizen, August 1789,
	// in Paine's translation.
	{From: "french", To: "virginia", How: DrewOn,
		Why: "Article I's “Men are born, and always continue, free and equal in respect of their rights” answers Mason's “all men are by nature equally free and independent and have certain inherent rights”; Lafayette drafted it with the Virginia declaration before him."},
	{From: "french", To: "declaration", How: DrewOn,
		Why: "Article II's “the natural and imprescriptible rights of man; and these rights are liberty, property, security, and resistance of oppression” answers the Declaration's “certain unalienable Rights, that among these are Life, Liberty and the pursuit of Happiness”; Lafayette drafted it with Jefferson's counsel in Paris."},
	// The Bill of Rights, 1791.
	{From: "rights", To: "magna-carta", How: DrewOn,
		Why: "The Fifth Amendment's “nor be deprived of life, liberty, or property, without due process of law” descends from chapter 39, which lets no freeman be taken or imprisoned “except by the lawful judgment of his peers or by the law of the land”."},
	{From: "rights", To: "magna-carta-1297", How: DrewOn,
		Why: "The chapter Coke read as due process is the statute book's chapter 29, “nor will we not pass upon him, nor condemn him, but by lawful Judgment of his Peers, or by the Law of the Land”, behind the Fifth Amendment's “without due process of law”."},
	{From: "rights", To: "body-of-liberties", How: DrewOn,
		Why: "The Fifth Amendment's “nor shall be compelled in any criminal case to be a witness against himself” and “nor shall any person be subject for the same offence to be twice put in jeopardy of life or limb” have the colony's liberties before them, “No man shall be forced by Torture to confesse any Crime against himselfe” and no man “twise sentenced by Civill Justice for one and the same Crime”; the Eighth's “cruel and unusual punishments” its “inhumane Barbarous or cruel”."},
	{From: "rights", To: "petition", How: DrewOn,
		Why: "The Fifth Amendment's “without due process of law” is Section IV's “by due p[ro]cesse of Lawe”, and the Third Amendment's “No Soldier shall, in time of peace be quartered in any house, without the consent of the Owner” answers Section VI's grievance that “the inhabitants against their wills have been compelled to receive them into their houses”."},
	{From: "rights", To: "english-rights", How: DrewOn,
		Why: "The Eighth Amendment's “Excessive bail shall not be required, nor excessive fines imposed, nor cruel and unusual punishments inflicted” follows the Act's words; the First's right “to petition the Government for a redress of grievances” and the Second's “right of the people to keep and bear Arms” answer its “Right of the Subjects to petition the King” and the subjects' “Arms for their Defence”."},
	{From: "rights", To: "pennsylvania", How: DrewOn,
		Why: "The First Amendment's “or abridging the freedom of speech, or of the press” has Article XII before it, “the people have a right to freedom of speech, and of writing, and publishing their sentiments; therefore the freedom of the press ought not to be restrained”, and the Second Article XIII's “the people have a right to bear arms for the defence of themselves and the state”."},
	{From: "rights", To: "massachusetts", How: DrewOn,
		Why: "The Fourth Amendment's “The right of the people to be secure in their persons, houses, papers, and effects, against unreasonable searches and seizures” is Article XIV's “Every subject has a right to be secure from all unreasonable searches, and seizures, of his person, his houses, his papers, and all his possessions”; the Second's right to arms Article XVII's “The people have a right to keep and to bear arms for the common defence”."},
	{From: "rights", To: "virginia-statute", How: DrewOn,
		Why: "The First Amendment's “Congress shall make no law respecting an establishment of religion, or prohibiting the free exercise thereof” carries Jefferson's Statute, that “no man shall be compelled to frequent or support any religious worship, place, or ministry whatsoever”, from Virginia to the whole country; Madison drove both through."},
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
	// The Universal Declaration of Human Rights, 1948, which the Charter of
	// the United Nations of 1945 called for.
	{From: "udhr", To: "un-charter", How: Follows,
		Why: "The Declaration's preamble rests on the Charter, whose peoples “have in the Charter reaffirmed their faith in fundamental human rights, in the dignity and worth of the human person and in the equal rights of men and women”, the Charter's own “to reaffirm faith in fundamental human rights, in the dignity and worth of the human person, in the equal rights of men and women and of nations large and small”, and sets out the rights the Charter named but did not list."},
	{From: "udhr", To: "declaration", How: DrewOn,
		Why: "Article 1's “All human beings are born free and equal in dignity and rights” and the Preamble's “equal and inalienable rights” carry the Declaration's “all men are created equal” and “unalienable Rights” to everyone."},
	{From: "udhr", To: "french", How: DrewOn,
		Why: "Article 1's “All human beings are born free and equal in dignity and rights” carries the 1789 Declaration's “Men are born, and always continue, free and equal in respect of their rights” to everyone, as its drafters, Cassin among them, meant it to."},
	{From: "udhr", To: "rights", How: DrewOn,
		Why: "Articles 18 to 20 declare the First Amendment's freedoms of religion, expression and assembly for everyone; Article 5's “cruel, inhuman or degrading treatment or punishment” takes up the Eighth's “cruel and unusual punishments”; Articles 9 to 11 the Fifth and Sixth's fair trial."},
	// The European Convention on Human Rights, November 1950.
	{From: "echr", To: "udhr", How: DrewOn,
		Why: "The Convention's preamble takes “the Universal Declaration of Human Rights proclaimed by the General Assembly of the United Nations on 10th December 1948” as what its parties will enforce collectively; its Article 10's freedom “to receive and impart information and ideas without interference by public authority and regardless of frontiers” is the Declaration's “to seek, receive and impart information and ideas through any media and regardless of frontiers” made binding."},
	// The Civil Rights Act of 1964.
	{From: "civil-rights", To: "constitution", How: Follows,
		Why: "Title VI's “No person in the United States shall, on the ground of race, color, or national origin, be excluded from participation in, be denied the benefits of, or be subjected to discrimination under any program or activity receiving Federal financial assistance” enforces the Fourteenth Amendment's “nor deny to any person within its jurisdiction the equal protection of the laws”, under Congress's powers over commerce and the Amendment's own."},
	{From: "civil-rights", To: "udhr", How: Follows,
		Why: "Title II's “without discrimination or segregation on the ground of race, color, religion, or national origin” answers, for public accommodations, the Declaration's Article 2, that everyone's rights hold “without distinction of any kind, such as race, colour, sex, language, religion”."},
	// The Voting Rights Act of 1965.
	{From: "voting-rights", To: "civil-rights", How: Follows,
		Why: "A year after the Civil Rights Act's Title I, “Voting Rights”, which only tightened the rules for registering, the Act strikes at the tests themselves: “No voting qualification or prerequisite to voting” may be applied to deny the vote on account of race or color."},
	{From: "voting-rights", To: "constitution", How: Follows,
		Why: "An Act “To enforce the fifteenth amendment to the Constitution of the United States”, whose “The right of citizens of the United States to vote shall not be denied or abridged by the United States or by any State on account of race, color, or previous condition of servitude” section 2 makes good against every “voting qualification or prerequisite to voting, or standard, practice, or procedure” that denies the vote on account of race or color."},
	// The International Convention on the Elimination of All Forms of
	// Racial Discrimination, December 1965.
	{From: "cerd", To: "udhr", How: DrewOn,
		Why: "The Convention's preamble takes the Declaration as its ground, that all human beings are “born free and equal in dignity and rights and that everyone is entitled to all the rights and freedoms set out therein, without distinction of any kind”, the Declaration's “All human beings are born free and equal in dignity and rights”, and binds its parties to end distinction by race."},
	{From: "cerd", To: "civil-rights", How: Follows,
		Why: "Eighteen months after the Act opened “any place of public accommodation” to all, Article 5 binds its parties to secure “The right of access to any place or service intended for use by the general public, such as transport, hotels, restaurants, cafés, theatres and parks” without distinction as to race."},
	// The Freedom of Information Act, 1966.
	{From: "foia", To: "constitution", How: Follows,
		Why: "An Act of Congress under Article I, set out as 5 U.S.C. § 552, which makes each agency's records available “to any person” unless one of its exemptions holds."},
	{From: "foia", To: "sweden", How: Follows,
		Why: "Two hundred years before FOIA opened an agency's records “to any person”, Sweden's ordinance of 1766 was the first law to open the state's papers to the public: “free access to all archives shall be permitted”, for the documents it names to be copied."},
	{From: "foia", To: "udhr", How: Follows,
		Why: "Article 19 declares everyone's freedom “to seek, receive and impart information and ideas through any media and regardless of frontiers”; eighteen years on, the Act gives a right to the government's own records."},
	// The International Covenant on Civil and Political Rights, December 1966.
	{From: "iccpr", To: "udhr", How: DrewOn,
		Why: "The Covenant binds its parties to the Declaration's rights, its preamble recognizing them “in accordance with the Universal Declaration of Human Rights”; Article 19's freedom “to seek, receive and impart information and ideas of all kinds, regardless of frontiers” is the Declaration's “to seek, receive and impart information and ideas through any media and regardless of frontiers” made treaty."},
	// The International Covenant on Economic, Social and Cultural Rights,
	// adopted the same day.
	{From: "icescr", To: "udhr", How: DrewOn,
		Why: "The Covenant binds its parties to the Declaration's economic, social and cultural rights “in accordance with the Universal Declaration of Human Rights”: its Article 13's “the right of everyone to education” is the Declaration's “Everyone has the right to education” made treaty."},
	{From: "icescr", To: "iccpr", How: Beside,
		Why: "The two Covenants were adopted together in resolution 2200 A (XXI), one for economic, social and cultural rights and one for civil and political, each preamble holding that “the ideal of free human beings enjoying freedom from fear and want” can only be achieved if everyone may enjoy both kinds of right, the other's words being “the ideal of free human beings enjoying civil and political freedom and freedom from fear and want”."},
	// The Privacy Act of 1974.
	{From: "privacy", To: "constitution", How: Follows,
		Why: "An Act of Congress under Article I, set out as 5 U.S.C. § 552a, on the records agencies keep about individuals."},
	{From: "privacy", To: "udhr", How: Follows,
		Why: "Article 12 holds that “No one shall be subjected to arbitrary interference with his privacy”; the Act gives that right form for the records the government keeps on a person."},
	// The Convention on the Rights of the Child, November 1989.
	{From: "crc", To: "udhr", How: DrewOn,
		Why: "The Convention recalls that “in the Universal Declaration of Human Rights, the United Nations has proclaimed that childhood is entitled to special care and assistance”, the Declaration's “Motherhood and childhood are entitled to special care and assistance”, and sets out what that care is in fifty-four articles."},
	// The Americans with Disabilities Act of 1990.
	{From: "ada", To: "civil-rights", How: Follows,
		Why: "Title I takes Title VII's form for disability: where the Act of 1964 holds that “It shall be an unlawful employment practice for an employer” to discriminate by race, colour, religion, sex or national origin, the Act of 1990 holds that “No covered entity shall discriminate against a qualified individual with a disability because of the disability of such individual in regard to job application procedures, the hiring, advancement, or discharge of employees”."},
	// The United Kingdom's Freedom of Information Act 2000.
	{From: "uk-foia", To: "foia", How: Follows,
		Why: "Section 1's “Any person making a request for information to a public authority is entitled” to be told whether it holds the information and to have it, as FOIA's records go “to any person”; both set out what may be withheld and give an appeal, the United Kingdom's thirty-four years after the United States'."},
	{From: "uk-foia", To: "iccpr", How: Follows,
		Why: "An Act “to make provision for the disclosure of information held by public authorities”, in a country party to the Covenant since 1976, whose Article 19 holds the freedom “to seek, receive and impart information and ideas of all kinds”."},
	// The Convention on the Elimination of All Forms of Discrimination
	// against Women, December 1979.
	{From: "cedaw", To: "udhr", How: DrewOn,
		Why: "The Convention's preamble notes that the Declaration proclaims that “all human beings are born free and equal in dignity and rights and that everyone is entitled to all the rights and freedoms set forth therein, without distinction of any kind, including distinction based on sex”, the Declaration's “All human beings are born free and equal in dignity and rights”, and binds its parties to end distinction by sex."},
	{From: "cedaw", To: "seneca", How: Follows,
		Why: "A hundred and thirty-one years after Seneca Falls resolved “That woman is man's equal”, the Convention defines discrimination against women as whatever impairs their rights “on a basis of equality of men and women” and binds its parties to end it."},
	// India's Right to Information Act, 2005.
	{From: "rti", To: "udhr", How: Follows,
		Why: "The Act's long title sets out “the practical regime of right to information for citizens to secure access to information under the control of public authorities”, giving form in law to Article 19's freedom “to seek, receive and impart information and ideas through any media and regardless of frontiers”."},
	{From: "rti", To: "iccpr", How: Follows,
		Why: "India acceded to the Covenant in 1979; the Act gives Article 19's freedom “to seek, receive and impart information and ideas of all kinds” a procedure, a time limit and an appeal, as “the right to information”."},
	{From: "rti", To: "foia", How: Follows,
		Why: "Section 3, “all citizens shall have the right to information”, does for India what FOIA did for the United States in 1966, where an agency's records go “to any person”; both set out what may be withheld and who hears an appeal."},
	{From: "privacy", To: "foia", How: Beside,
		Why: "The Privacy Act is § 552a, the section after the Freedom of Information Act's § 552 in the Code, and lets a record out without the person's consent only where, among its exceptions, disclosure is “required under section 552 of this title”."},
	{From: "massachusetts", To: "mayflower", How: Follows,
		Why: "Massachusetts's preamble, “it is a social compact, by which the whole people covenants with each citizen, and each citizen with the whole people”, says in 1780 what the settlers did in 1620, when they did “covenant and combine ourselves together into a civil Body Politick”."},
	{From: "refugees", To: "un-charter", How: DrewOn,
		Why: "The Convention opens “Considering that the Charter of the United Nations and the Universal Declaration of Human Rights approved on 10 December 1948 by the General Assembly have affirmed the principle that human beings shall enjoy fundamental rights and freedoms without discrimination”, the Charter's “respect for human rights and for fundamental freedoms for all without distinction as to race, sex, language, or religion”, and secures those rights to refugees."},
	{From: "refugees", To: "udhr", How: DrewOn,
		Why: "The Declaration's “Everyone has the right to seek and to enjoy in other countries asylum from persecution” is the right the Convention gives form, “Considering that the grant of asylum may place unduly heavy burdens on certain countries”: it says who is a refugee and what a refugee is owed."},
	{From: "iccpr", To: "refugees", How: Follows,
		Why: "The Covenant's Article 13, “An alien lawfully in the territory of a State Party to the present Covenant may be expelled therefrom only in pursuance of a decision reached in accordance with law”, extends to every alien the Convention's Article 32, “The expulsion of such a refugee shall be only in pursuance of a decision reached in accordance with due process of law”."},
	{From: "crpd", To: "udhr", How: DrewOn,
		Why: "The Convention recalls “that the United Nations, in the Universal Declaration of Human Rights and in the International Covenants on Human Rights, has proclaimed and agreed that everyone is entitled to all the rights and freedoms set forth therein, without distinction of any kind”, the Declaration's “Everyone is entitled to all the rights and freedoms set forth in this Declaration, without distinction of any kind”, and secures them to persons with disabilities."},
	{From: "crpd", To: "iccpr", How: DrewOn,
		Why: "The Convention recalls “the International Covenant on Civil and Political Rights” among the instruments it builds on, and the Covenant's rule, “without distinction of any kind, such as race, colour, sex, language, religion, political or other opinion”, is the one it applies to disability, with its own articles on liberty, justice, expression and taking part in public life."},
	{From: "crpd", To: "icescr", How: DrewOn,
		Why: "The Convention recalls “the International Covenant on Economic, Social and Cultural Rights” and takes up its rights to education, work, health and an adequate standard of living; the Covenant's rule, “without discrimination of any kind as to race, colour, sex, language, religion, political or other opinion”, is the one it applies to disability."},
	{From: "crpd", To: "cerd", How: DrewOn,
		Why: "Its definition, “any distinction, exclusion or restriction on the basis of disability which has the purpose or effect of impairing or nullifying the recognition, enjoyment or exercise” of rights, follows the form the racial discrimination Convention set in 1965: “any distinction, exclusion, restriction or preference based on race, colour, descent, or national or ethnic origin which has the purpose or effect of nullifying or impairing the recognition, enjoyment or exercise”."},
	{From: "crpd", To: "cedaw", How: DrewOn,
		Why: "Its definition follows the women's Convention's too, “any distinction, exclusion or restriction made on the basis of sex which has the effect or purpose of impairing or nullifying the recognition, enjoyment or exercise”, and its Article 6 recognizes “that women and girls with disabilities are subject to multiple discrimination”."},
	{From: "crpd", To: "crc", How: DrewOn,
		Why: "The Convention recalls “obligations to that end undertaken by States Parties to the Convention on the Rights of the Child”, whose Article 23 recognizes “that a mentally or physically disabled child should enjoy a full and decent life”, and its Article 7 secures children with disabilities every right on an equal basis with other children."},
	{From: "crpd", To: "ada", How: Follows,
		Why: "The Act's qualified individual is one who “with or without reasonable accommodation, can perform the essential functions of the employment position”, short of “an undue hardship” on the employer; sixteen years on, the Convention carries the term into international law, as “necessary and appropriate modification and adjustments not imposing a disproportionate or undue burden”."},
}
