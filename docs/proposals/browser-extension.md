# Irie in your browser, and Terms in short

The owner asked on 2026-11-15 for a way to take in a company's terms of service: words a person reads instead of the legal text, worked out by code with no tokens spent (a local model allowed), and a chat bot that answers questions on them. They wanted it as an Irie plugin or a browser plugin, and settled on both: a browser plugin of Irie's, with this as its first tool. DESIGN §4.17 says where it stands; this note says why it is built the way it is.

## 1. What a person sees

1. `irie setup extension` (or `:setup extension`, or Set Up… in Settings › Plugins) writes the extension and lets the browsers here reach Irie. Loading the extension's folder in the browser, once, is the person's own step: Chrome, Brave, Edge and Arc load it under Load unpacked in their extensions page; Firefox under about:debugging, until it quits.
2. On any site, Irie's button in the toolbar opens a side panel. Terms in short reads the page: the site's terms of service when the page is them, else the terms its links point to, else those at the places sites keep them (`/terms`, `/legal/terms` and the like).
3. The panel shows the terms in short: how long they are and who they're written for, a count of points against you, to watch, good to know and for you, then each point in plain words. Each opens to the clause it comes from, its heading, and a link that opens the page at those words.
4. Under them, a chat: suggested questions ("What's the catch?", "Can I get a refund?"), and any question in the person's own words. Each answer quotes the passages it rests on. A box words answers with a local model on this computer, when there is one.
5. In a terminal, `irie terms example.com` prints the same, `irie terms ask example.com "can I get a refund?"` answers, and `irie terms chat` takes questions a line at a time.

## 2. Code first, a model only when asked

Reading terms is mostly finding a few dozen kinds of clause that recur in nearly all of them, and saying what each means. That suits code: it costs nothing to run, gives the same answer every time, and can show exactly which words each point comes from.

- **A catalog of kinds of clause** (`internal/terms/topics.go`). Each has weighted patterns, the headings it sits under, its verdict for the person, and its point in plain words, filled in from the clause (the opt-out's days, the cap's amount, the state whose law applies). The categories follow research on unfair terms of service (CLAUDETTE's eight: arbitration, unilateral change, content removal, jurisdiction, choice of law, limitation of liability, unilateral termination, contract by using) and the EU's indicative list of unfair terms (Directive 93/13/EEC, annex); consumer concerns add the rest (selling and sharing data, AI training, auto-renewal, refunds). The patterns and every word said are Irie's own.
- **Who acts.** "You may close your account at any time" is the person's right, and "we may close your account at any time" is the company's. Patterns of what the company may do drop a match whose subject is the person.
- **Turned round.** A clause that says the opposite of a kind ("we do not sell your personal information") becomes its opposite point, in your favour; terms that say both get one point that says what they may do and that another passage limits it.
- **Questions** go to the kinds their words are about, then to a search of the clauses' own words (BM25), after mapping people's words onto the words terms use: "sue" finds arbitration, "money back" refunds.
- **A local model**, when the person asks for one, only words the answer: the code chooses the passages, and the model is told to answer from them alone and to name the passage it relies on. The answer quotes those passages, so its words can be checked against them. It is the model `extension.model` names, else the biggest of at most 16 billion parameters and 10 GB that an Ollama on this computer has for chat. It is never a cloud model, which would cost money and send the terms away.

What code can't do: read terms in languages other than English (a page with several languages is read from its English), weigh one clause against another as a lawyer would, or know the law where the person lives. The panel and the terminal both say it is not legal advice.

## 3. Why a browser extension, and why this shape

- **Native messaging, not a server.** The extension's one way into Irie is a program the browser starts, `irie extension host`, which it talks to over standard input and output (Chrome's and Firefox's native messaging). Nothing listens on a port, so no page and no other program can call it, and only this extension, by its ID, may start it.
- **A side panel, not a popup.** A popup closes when the person clicks the page, which a chat can't survive. Chrome's side panel and Firefox's sidebar stay beside the page while they read its terms.
- **`activeTab`, not every site.** The extension asks for no site's pages. The browser gives it the page its button was pressed on, for that press, which it reads as the browser shows it, after the page's scripts have built it. So terms behind a script, which a plain fetch reads as empty, still read, and nothing is read before a press.
- **The host fetches the rest.** Following a page's links to its terms happens in Irie, which fetches them without the person's cookies and says who is asking. A page's links never reach this computer or its local network, so a hostile page can't use them to knock on a router or a local app. A site that refuses Irie's fetch is read from the browser instead, once the person opens its terms.
- **Its files are Irie's.** The extension is built into the irie binary and written out by setup, so it updates with Irie. When a newer build brings newer files, the host writes them as the panel connects, and the panel offers to reload. A key in its manifest fixes its ID in Chrome; its private half was never kept, as a store gives a published extension an ID of its own.
- **One tool now, more later.** The panel is drawn for tools; Terms in short is the first. More tools come as more parts of the plugin.

## 4. Later

- A page on the web where anyone can read terms without installing Irie: the same code, built to WebAssembly, reading terms a person pastes.
- Safari, through Xcode's converter, and a store listing for each browser, which a signed and reviewed release needs.
- The tool for agents: a `terms` tool in Irie's browser (§4.15), so a session reads terms for a fraction of the tokens the whole text takes.
- Answers that remember the conversation, and a short summary written by a local model, both only when asked.

## 5. Sources

Chrome's and Firefox's documentation of native messaging (the host's file, its name, where each browser looks, the framing and its limits), of the manifest's key, the side panel, `activeTab` and `scripting`; the text fragments specification; Ollama's API reference for streamed chat; Robertson and Zaragoza on BM25; the Flesch-Kincaid grade; Brysbaert on reading speed; the CLAUDETTE project and Directive 93/13/EEC for the kinds of unfair clause. PROVENANCE.md has each with its date and what it shaped. No code or data was copied: the catalog's patterns and words were written for Irie.
