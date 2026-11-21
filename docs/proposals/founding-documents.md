# The founding documents

On 2026-11-21 the owner compared copies of the Constitution and the Bill of Rights, then asked for two things: an official, verified copy in Irie's skills, and a plugin for the Bill of Rights and the Declaration of Independence. The owner added that a chain would start from it.

## What it is

- **A skill**, `/irie:founding-documents`. Its files hold three documents, word for word as the National Archives transcribes them: the Declaration of Independence, the Constitution with all 27 amendments, and the Bill of Rights. Agents quote from these files, never from memory, and say so when a quotation isn't there.
- **A plugin**, Founding documents, with one part. It brings the skill and the Founding tab in the Mac app's right sidebar, which shows the three. Its search finds a passage whatever its spelling, so "choose" finds the parchment's "chuse". The tab also shows each text's SHA-512.
- **A record**, `verification.md`, beside the texts. It says what each text was checked against and what was found, and lists the marks that careful copies still disagree on.

## The texts

The National Archives' transcriptions are the source: of the Declaration's engrossed parchment, of the Constitution as it was inscribed on parchment, of the Bill of Rights' enrolled original, and of Amendments XI–XXVII. Their words, spelling, capitals and punctuation are kept. Only the markup and the lines in italics are Irie's: headings, dates and notes.

Each text was then checked against every other copy at hand. The Declaration was checked against the Avalon Project's. The Constitution was checked against a literal print, a government print, the National Constitution Center's copy and a 19th-century printing. The Bill of Rights was checked against the Archives' handout, ConstitutionFacts.com's copy, the amendments in each copy of the Constitution, and the Archives' own transcription of the 1789 joint resolution. The checks went word for word, and for the Constitution's articles and the Bill of Rights, mark for mark. Every difference found is in `verification.md`.

## Fingerprints

Each text's file is pinned by its SHA-512. The hash covers the file's bytes, UTF-8 with a line feed ending each line, so anyone can check a copy with `shasum -a 512`. The tests fail when a file's SHA-512 isn't the one `verification.md` gives, so no text changes without its record changing with it.

SHA-512 is also the hash the local chain names its versions by ([local-chain.md](local-chain.md)), so a record there can name these texts by their fingerprints. Like the local chain, this carries no tokens and no money.

## How it is built

- The texts are the skill's own files (`internal/skills/bundled/founding-documents`), embedded by `bundled_founding.go`, so an agent and the tab read the same bytes.
- `internal/founding` reads them into headings, the documents' words, notes and lists of names, and finds passages in them.
- The core's `founding` op gives the tab the documents and, with `find`, the passages that hold some words. Anyone may ask, an agent too: the texts are public, and the op only reads.
- `-tags nofounding` leaves the plugin out, texts and all. A company's copy leaves it out with the other personal plugins.
- It is off by default: the documents are a person's own reading, and a skill that is on costs every session a line of context.

## Open questions

- **Four marks still disagree.** The Archives' transcription and other careful copies differ on four points: "enumeration" or "Enumeration" (Art. I §9), "proper," or "proper;" (Art. II §2), "another State," or "another State;" (Art. III §2), and "In Suits" or "In suits" (Amendment VII). The parchments would settle them, ideally before a chain records the texts.
- **More documents.** The Articles of Confederation, or other countries' founding documents, would fit the same skill and tab.

## Sources

- National Archives, Declaration of Independence: A Transcription: https://www.archives.gov/founding-docs/declaration-transcript
- National Archives, The Constitution of the United States: A Transcription: https://www.archives.gov/founding-docs/constitution-transcript
- National Archives, The Bill of Rights: A Transcription: https://www.archives.gov/founding-docs/bill-of-rights-transcript
- National Archives, The Constitution: Amendments 11-27: https://www.archives.gov/founding-docs/amendments-11-27
- The Avalon Project, Yale Law School, The Declaration of Independence: https://avalon.law.yale.edu/18th_century/declare.asp
