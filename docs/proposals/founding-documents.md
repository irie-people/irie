# The founding documents

On 2026-11-21 the owner compared copies of the Constitution and the Bill of Rights, then asked for an official, verified copy in Irie's skills, and a plugin for the Bill of Rights and the Declaration of Independence. The owner added that a chain would start from it. Later that day they asked for the texts to be on a chain locally, and for the Freedom of Information Act too.

## What it is

- **A skill**, `/irie:founding-documents`. Its files hold four documents word for word:
  - the Declaration of Independence, the Constitution with all 27 amendments, and the Bill of Rights, as the National Archives transcribes them;
  - the Freedom of Information Act, 5 U.S.C. § 552, as the United States Code sets it out.

  Agents quote from these files, never from memory, and say so when a quotation isn't there.
- **A plugin**, Founding documents, with one part. It brings the skill and the Founding tab in the Mac app's right sidebar, which shows the four. Its search finds a passage whatever its spelling, so "choose" finds the parchment's "chuse". The tab also shows each text's SHA-512 and how the local chain checked.
- **A local chain** of the texts, which the core keeps. `irie ctl founding` checks it, and `irie ctl founding log` lists its versions.
- **A record**, `verification.md`, beside the texts. It says what each text was checked against and what was found, and lists the marks that careful copies still disagree on.

## The texts

The National Archives' transcriptions are the founding documents' source: the Declaration's engrossed parchment, the Constitution as it was inscribed on parchment, the Bill of Rights' enrolled original, and Amendments XI–XXVII. The Freedom of Information Act comes from the United States Code, in the Government Publishing Office's 2024 edition. Title 5 is positive law, so the Code's text is legal evidence of the law (1 U.S.C. § 204). The words, spelling, capitals and punctuation are kept. Only the markup and the lines in italics are Irie's: headings, dates and notes.

Each text was then checked against every other copy at hand:

- **The Declaration**: the Avalon Project's copy.
- **The Constitution**: a literal print, a government print, the National Constitution Center's copy and a 19th-century printing.
- **The Bill of Rights**: the Archives' handout, ConstitutionFacts.com's copy, the amendments in each copy of the Constitution, and the Archives' own transcription of the 1789 joint resolution.
- **The Freedom of Information Act**:
  - the Code's 2023 edition, which is the same;
  - Cornell's Legal Information Institute, whose copy hashed the same word for word and mark for mark;
  - the Department of Justice's copy, whose eight differences are each its own slip.

The checks went word for word, and mark for mark for the Constitution's articles, the Bill of Rights and the Freedom of Information Act. Every difference found is in `verification.md`.

## Fingerprints and the local chain

Each text's file is pinned by its SHA-512. The hash covers the file's bytes, UTF-8 with a line feed ending each line, so anyone can check a copy with `shasum -a 512`. The tests fail when a file's SHA-512 isn't the one `verification.md` gives, so no text changes without its record changing with it.

The core keeps the texts on a chain of their own, in `$XDG_STATE_HOME/irie/chain/founding`, on the same `internal/chain` that keeps zen's versions. It is the first piece of the local chain [local-chain.md](local-chain.md) plans for the sidebar.

- **A version** is canonical JSON (RFC 8785) holding every text whole, each with its SHA-512 as `sha512:` and 128 hex digits. It is named by the SHA-512 of its own bytes, and names the version before it the same way, so the newest names the whole history.
- **When.** The core adds a version when it starts and at each look, and only when a text has changed. A new build whose texts are unchanged adds nothing.
- **Checking.** Each look reads every version back. The store checks each against its name, so a version changed on disk after it was written fails. The check also hashes each text against its own SHA-512.
- **Only public standards** shape a version, so any tool with a JSON canonicalizer and SHA-512 can check one without Irie.
- **Like the local chain**, it carries no tokens, no money and no identity, and nothing on it leaves the computer. Anchoring a version's name on a public chain would be the local chain's opt-in hand-off, made by another tool.

## How it is built

- The texts are the skill's own files (`internal/skills/bundled/founding-documents`), embedded by `bundled_founding.go`, so an agent and the tab read the same bytes.
- `internal/founding` reads them into headings, the documents' words, notes and lists of names, finds passages in them, and keeps their chain (`chain.go`).
- The core's `founding` op gives the tab the documents, their chain as it just checked it, and, with `find`, the passages that hold some words, or, with `log`, the chain's versions. Anyone may ask, an agent too: the texts are public, and the op only reads them and keeps their chain.
- `-tags nofounding` leaves the plugin out, texts and all. A company's copy leaves it out with the other personal plugins.
- It is off by default: the documents are a person's own reading, and a skill that is on costs every session a line of context.

## Open questions

- **Four marks still disagree.** The Archives' transcription and other careful copies differ on four points: "enumeration" or "Enumeration" (Art. I §9), "proper," or "proper;" (Art. II §2), "another State," or "another State;" (Art. III §2), and "In Suits" or "In suits" (Amendment VII). The parchments would settle them.
- **The House's online Code** was down for maintenance on 21 November 2026. Once it's back, check § 552 against it, current through Public Law 119-142 (November 10, 2026). Cornell follows it and matched.
- **More documents.** The Articles of Confederation, other statutes, or other countries' founding documents would fit the same skill, tab and chain.

## Sources

- National Archives, Declaration of Independence: A Transcription: https://www.archives.gov/founding-docs/declaration-transcript
- National Archives, The Constitution of the United States: A Transcription: https://www.archives.gov/founding-docs/constitution-transcript
- National Archives, The Bill of Rights: A Transcription: https://www.archives.gov/founding-docs/bill-of-rights-transcript
- National Archives, The Constitution: Amendments 11-27: https://www.archives.gov/founding-docs/amendments-11-27
- The Avalon Project, Yale Law School, The Declaration of Independence: https://avalon.law.yale.edu/18th_century/declare.asp
- 5 U.S.C. § 552, United States Code, 2024 edition (GPO): https://www.govinfo.gov/content/pkg/USCODE-2024-title5/html/USCODE-2024-title5-partI-chap5-subchapII-sec552.htm, and its 2023 edition
- 5 U.S. Code § 552, Legal Information Institute, Cornell Law School: https://www.law.cornell.edu/uscode/text/5/552
- Department of Justice, Office of Information Policy, Freedom of Information Act, 5 U.S.C. § 552: https://www.justice.gov/oip/freedom-information-act-5-usc-552
- RFC 8785, the JSON Canonicalization Scheme, which `internal/chain` follows: https://www.rfc-editor.org/rfc/rfc8785
