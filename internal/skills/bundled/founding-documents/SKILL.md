---
name: founding-documents
description: The Declaration of Independence, the Constitution with all 27 amendments, and the Bill of Rights, word for word as the National Archives transcribes them, checked against other copies. Use to quote, cite or check any of them, such as "what does the Fourth Amendment say", "Article I, Section 8" or "is this really in the Constitution".
license: Apache-2.0
---

# The founding documents

This skill holds Irie's verified copies of three documents, each in a file beside this one:

- **declaration.md**: the Declaration of Independence, with its 56 signers.
- **constitution.md**: the Constitution of the United States, its signers, and all 27 amendments.
- **bill-of-rights.md**: the Bill of Rights, the first ten amendments, with the preamble of the 1789 joint resolution that proposed them, the two articles not ratified in 1791 and the attestation.

verification.md says where each text comes from, what it was checked against, the four marks still unsettled, and each file's SHA-512 fingerprint.

## Quote from the files, never from memory

- **Read the file and copy the words exactly.** The spelling, capitals and punctuation are the originals': "chuse", "Independance", "Brittish", "the Ratification of the Conventions of nine States". Don't modernize them unless asked, and say so when you do.
- **Lines in italics are editorial.** Dates, notes and the amendments' headings are not the documents' words, so never quote them as the document.
- **Cite by the documents' own parts.** Use "Article I, Section 8" (the parchment writes "Article. I." and "Section. 8."), "Amendment XIV, Section 1", or the Declaration's paragraph.
- **Say when words can't be found.** When a quotation isn't in the file word for word, say so and give the closest passage. "Life, Liberty and the pursuit of Happiness" is in the Declaration, not the Constitution.
- **Say when a clause was changed.** The Constitution's articles stand as written in 1787, including clauses later amendments changed. The notes under each amendment say which part it changed. When you quote a changed clause, say it was changed and by which amendment.

## Check a copy

To check whether another copy is this one, compare its SHA-512 with verification.md's: `shasum -a 512 FILE`. To check a quotation, search the files for its words.

## In Irie

The Founding documents plugin shows the three in the Founding tab of the Mac app's right sidebar. Its search finds a passage whatever its spelling or capitals.
