# How these copies were verified

Checked on 21 November 2026. Each text is the National Archives' transcription, set in plain Markdown. Each was then checked against every other copy at hand: word for word, and for the Bill of Rights and the Constitution's articles, comma for comma and capital for capital.

## The texts

| File | The National Archives' transcription |
|---|---|
| declaration.md | "Declaration of Independence: A Transcription", https://www.archives.gov/founding-docs/declaration-transcript |
| constitution.md | "The Constitution of the United States: A Transcription", https://www.archives.gov/founding-docs/constitution-transcript; Amendments I–X from the Bill of Rights transcription; Amendments XI–XXVII from https://www.archives.gov/founding-docs/amendments-11-27 |
| bill-of-rights.md | "The Bill of Rights: A Transcription", https://www.archives.gov/founding-docs/bill-of-rights-transcript: the preamble, the two articles not ratified in 1791 and the attestation from its transcription of the 1789 joint resolution, and the ten amendments in their original form |

Only the markup and the lines in italics are ours. The Archives marks two passages in Amendments XII and XIV that later amendments changed, with brackets and an asterisk. Here each mark is a note in italics after its passage instead, and the passage's words are unchanged.

## What they were checked against

- **The Declaration**: the Avalon Project's transcription at Yale Law School.
- **The Constitution**: a literal print of the original, a government print with notes ("Constitution of the United States of America—1787"), the National Constitution Center's copy, and a 19th-century printing scanned by the Library of Congress, which was read by text recognition.
- **The Bill of Rights**: the National Archives' handout from its Center for Legislative Archives, ConstitutionFacts.com's copy, the amendments in each of the Constitution's copies, and the Archives' own transcription of the 1789 joint resolution.

## What was found

**The Declaration.** Every word matches the Avalon Project's copy, apart from two kinds of difference. First, Avalon modernizes spellings: "shewn", "endeavoured", "harrass", "Offences", "neighbouring", "burnt", "compleat" and "Brittish". Second, it follows another version in six places. The Archives' transcription of the parchment reads "the dangers of invasion", "their migrations hither", "domestic insurrections amongst us", "Nor have We been wanting in attentions", "by Authority of the good People" and "and to do all other Acts". Avalon also writes "and" for the parchment's "&". All 56 signers are in both.

**The Constitution: the Preamble, the seven articles and the signers.**
- **The literal print and the government print** match it word for word with one exception, "the Independance of the United States". That is the parchment's own spelling, which the Archives keeps and both prints correct to "Independence".
- **The National Constitution Center's copy** modernizes three spellings. "Offences" becomes "Offenses" twice (Art. I §8, Art. II §2), and "encreased" becomes "increased" (Art. II §1).
- **The 19th-century printing** changes two words. Art. I §6 reads "in going to or returning", where the parchment has "and", and Art. I §7 reads "the names of the person voting", where it has "Persons". It also modernizes spellings throughout ("choose", "labor", "guaranty") and writes the signers' names out in full.

**Amendments XI–XXVII.** The literal print and the government print match them word for word. The National Constitution Center's copy differs in nine small words: it drops "the" five times, adds it three times, and drops "elect" in Amendment XX and "any" in Amendment XXIV.

**The Bill of Rights.** The ten amendments match the National Archives' handout and the government print mark for mark. In the other copies only commas and capitals differ:

- Amendment I: ConstitutionFacts and the Constitution Center have "of the press, or the right" for "of the press; or the right".
- Amendment V: ConstitutionFacts has "life or limb, nor" for "life or limb; nor", and no comma after "for public use".
- Amendment VI: ConstitutionFacts has "committed; which district" for "committed, which district", and "assistance of counsel" in lower case.
- Amendment VII: the literal print, the Constitution Center and ConstitutionFacts have no comma after "tried by a jury". The Constitution Center has "In suits" for "In Suits".
- Amendment IX: ConstitutionFacts has no commas around "of certain rights".
- Amendments II, III, IV, VIII and X are the same in every copy. The Second Amendment keeps all three of its commas in all of them.

## Not settled

Four marks, where the Archives' transcription and other careful copies disagree. These copies follow the Archives; checking the parchments would settle each one.

| Where | These copies (the Archives) | The other reading |
|---|---|---|
| Art. I §9 | "the Census or enumeration herein before directed" | "Enumeration": the literal print and the government print |
| Art. II §2 | "as they think proper, in the President alone" | "proper;": the literal print |
| Art. III §2 | "between a State and Citizens of another State,—between Citizens of different States" | "another State;": the literal print and the government print |
| Amendment VII | "In Suits at common law" | "In suits": the Archives' own transcription of the 1789 joint resolution |

## Fingerprints

The SHA-512 of each file's bytes: UTF-8, with a line feed ending each line. A copy whose fingerprint differs is not this one: `shasum -a 512 declaration.md` shows it.

| File | SHA-512 |
|---|---|
| declaration.md | `7447f9380e888a68b2ca72b1452487dabae23325abc3442f78fe5f9dd7474d1e20ab1157d0364064acecdb5c7aa4d87902dfd97c73c0230d9442187b0600d2ad` |
| constitution.md | `1e2a63f6de9bc5080a4f9d6aad9beb0146c5bf1bb9fc45a262b2f57dc52415342dcaf77fd2da3ffcfba8d505b8dc4c1ed736a73a46d42ed339e5c0be03f27cb5` |
| bill-of-rights.md | `03a89a2272ca2bcc2c05abba58853c016433745cdb77a0a7e16e30ee705813bfea43de8cb655c9089dd46852dc978c69fe8949d74234f817a37d56b96331b957` |

Irie's tests pin these fingerprints, so a change to any of the texts fails them until the change is meant and the fingerprints here are updated with it.
