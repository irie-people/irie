---
name: founding-documents
description: Ten texts of rights, word for word from the bodies that keep them and checked against other copies, with how each bears on the others: Magna Carta (1215), the English Bill of Rights (1689), the Virginia Declaration of Rights (1776), the Declaration of Independence, the Articles of Confederation, the Constitution with all 27 amendments, the Bill of Rights, the Universal Declaration of Human Rights (1948), the Freedom of Information Act and the Privacy Act of 1974. Use to quote, cite or check any of them, such as "what does the Fourth Amendment say", "Article I, Section 8", "FOIA's exemptions", "what did the Bill of Rights take from the English one" or "is this really in the Constitution".
license: Apache-2.0
---

# The charters

This skill holds Irie's verified copies of ten texts of rights, each in a file beside this one, the oldest first:

- **magna-carta.md**: Magna Carta, the charter King John granted at Runnymede in 1215, in William Sharp McKechnie's translation as a 1915 edition printed it: its preamble and 63 chapters.
- **english-bill-of-rights.md**: the English Bill of Rights of 1689, "An Act declareing the Rights and Liberties of the Subject and Setleing the Succession of the Crowne", 1 Will. & Mar. sess. 2 c. 2, as enacted, in the Statutes of the Realm's spelling.
- **virginia-declaration-of-rights.md**: the Virginia Declaration of Rights of June 12, 1776, in the National Archives' modernized rendering: its spelling and punctuation are today's, not 1776's, and verification.md says where it differs from the adopted text.
- **declaration.md**: the Declaration of Independence, with its 56 signers.
- **articles-of-confederation.md**: the Articles of Confederation, agreed in 1777 and ratified in 1781, their thirteen articles.
- **constitution.md**: the Constitution of the United States, its signers, and all 27 amendments.
- **bill-of-rights.md**: the Bill of Rights, the first ten amendments, with the preamble of the 1789 joint resolution that proposed them, the two articles not ratified in 1791 and the attestation.
- **universal-declaration-of-human-rights.md**: the Universal Declaration of Human Rights, proclaimed on 10 December 1948: its preamble and 30 articles, following the resolution's official record.
- **freedom-of-information-act.md**: the Freedom of Information Act, 5 U.S.C. § 552, as the United States Code sets it out today, with its source credit, the laws that made and amended it.
- **privacy-act.md**: the Privacy Act of 1974, 5 U.S.C. § 552a, as the Code sets it out, with its source credit.

verification.md says where each text comes from, what it was checked against, the four marks of the Constitution and the Bill of Rights settled from the parchments themselves, the readings still unsettled in the other texts, and each file's SHA-512 fingerprint.

## Quote from the files, never from memory

- **Read the file and copy the words exactly.** The spelling, capitals and punctuation are the sources': "chuse", "Independance", "Brittish", "the Ratification of the Conventions of nine States", the 1689 Act's "cruell and unusuall Punishments" and "excessive Baile", the Articles' "Stile". Don't modernize them unless asked, and say so when you do.
- **Say whose English it is.** Magna Carta's words are McKechnie's translation of the Latin, and the Virginia Declaration is the Archives' modernized text; the Universal Declaration follows the 1948 official record where the UN's web text differs. Say so when you quote them as the original's words.
- **Lines in italics are editorial.** Dates, notes and the amendments' headings are not the documents' words, so never quote them as the document. The English Bill of Rights' headings are the Statutes of the Realm's marginal titles, not the Act's words.
- **Cite by the documents' own parts.** Use "Article I, Section 8" (the parchment writes "Article. I." and "Section. 8."), "Amendment XIV, Section 1", the Declaration's paragraph, "Magna Carta, chapter 39", "Virginia Declaration of Rights, Section 8", "Universal Declaration, Article 19", "5 U.S.C. § 552(b)(6)" or "5 U.S.C. § 552a(b)(2)". The Code's paragraphs each start with their letter or number, so trace a citation down from its subsection: (b), then (6).
- **Say when words can't be found.** When a quotation isn't in the file word for word, say so and give the closest passage. "Life, Liberty and the pursuit of Happiness" is in the Declaration, not the Constitution.
- **Say when a clause was changed.** The Constitution's articles stand as written in 1787, including clauses later amendments changed. The notes under each amendment say which part it changed. When you quote a changed clause, say it was changed and by which amendment.

## How they bear on one another

Irie keeps links between the texts, each from a later text back to an earlier one, with a sentence saying what the one took from the other and quoting both; Irie's tests check every quotation against the texts. `irie ctl charters` lists them, and `--json` gives each one's sentence. In short:

- The Virginia Declaration drew on Magna Carta (the law of the land, Section 8) and on the English Bill of Rights (bail, fines and punishments, Section 9).
- The Declaration of Independence drew on the Virginia Declaration ("created equal", "unalienable Rights").
- The Articles of Confederation follow the Declaration. The Constitution follows the Declaration, which it dates itself from, and replaced the Articles.
- The Bill of Rights amends the Constitution and drew on Magna Carta (due process), the English Bill of Rights (the Eighth Amendment's words, petition, arms) and the Virginia Declaration.
- The Universal Declaration drew on the Declaration of Independence and the Bill of Rights.
- The Freedom of Information Act and the Privacy Act follow the Constitution, as Acts of Congress, and the Universal Declaration (Articles 19 and 12); the Privacy Act stands beside FOIA in the Code.

The links are Irie's reading of the texts, not the documents' words: give one as Irie's link, never as a source.

## Check a copy

To check whether another copy is this one, compare its SHA-512 with verification.md's: `shasum -a 512 FILE`. To check a quotation, search the files for its words.

## On the local chain

Irie keeps these texts on a chain of its own on this computer, the Charters chain, in `$XDG_STATE_HOME/irie/chain/charters` (`~/.local/state/irie/chain/charters` unless that is set). Each version holds every text word for word, with its SHA-512, its date and its kind, and the links, and names the version before it by its own SHA-512, so the newest names the whole history. Together the texts and the links are a graph: the texts its nodes, the links its edges. A version is added only when a text or a link changes. `irie ctl charters` shows the newest version and checks every one: each version against its name, each text against its fingerprint, and each link against the texts. `irie ctl charters log` lists them, and `irie ctl charters export` prints every version's bytes, oldest first, one a line, for another tool to read the chain whole. The first build kept the chain as `chain/founding`, holding the four first texts in the form `irie.founding/1`; Irie moves it into the Charters chain and goes on from its newest version, so the history stays one. Nothing on it leaves this computer.

## In Irie

The Charters plugin shows the ten in the Charters tab of the Mac app's right sidebar, each with the links that say how it bears on the others. Its search finds a passage whatever its spelling or capitals, and its foot shows the chain's newest version.
