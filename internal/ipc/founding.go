package ipc

// OpFounding asks for the charters, the founding plugin's (DESIGN §4.17):
// texts of rights kept word for word, from Magna Carta to India's Right to
// Information Act,
// in the parts the Mac app's Charters tab draws, the passages a search
// finds, how the texts bear on one another, and their local chain.
const OpFounding = "founding"

// FoundingArgs ask the founding op for the documents and, with Find, for
// the passages that hold those words.
type FoundingArgs struct {
	// Find is the words to look for, whatever their spelling or capitals;
	// "" = none.
	Find string `json:"find,omitempty"`
	// Log asks for up to this many of the chain's versions, newest first.
	Log int `json:"log,omitempty"`
	// Export asks for every version's bytes, oldest first, in Export.
	Export bool `json:"export,omitempty"`
	// Graph asks for the Charters Graph page, the explorer, which the core
	// writes to a file of its own: Page says where, and nothing else comes.
	Graph bool `json:"graph,omitempty"`
}

// FoundingResult is the founding op's answer: the documents, and the
// passages Find found; or, for Graph, only where the page is.
type FoundingResult struct {
	Docs  []FoundingDoc `json:"docs,omitempty"`
	Found []FoundingHit `json:"found,omitempty"`
	// Links are how the texts bear on one another, the edges of the graph
	// the chain carries with them; a search leaves them out.
	Links []FoundingLink `json:"links,omitempty"`
	// Chain is the documents' local chain, as the core checked it now; a
	// search leaves it out.
	Chain *FoundingChain `json:"chain,omitempty"`
	// Versions are the chain's versions Log asked for, newest first.
	Versions []FoundingVersion `json:"versions,omitempty"`
	// Export is every version's bytes, oldest first, when asked for: each
	// canonical JSON with no line break in it.
	Export []string `json:"export,omitempty"`
	// Page is the Charters Graph page's file, when Graph asked for it: one
	// HTML file that loads nothing from elsewhere.
	Page string `json:"page,omitempty"`
}

// FoundingLink is how one text bears on an earlier one: an edge of the
// graph, pointing from the later text back to the one it grew from.
type FoundingLink struct {
	From string `json:"from"` // the later text's id
	To   string `json:"to"`   // the earlier text's id
	// How is drew on, amends, replaced, beside or follows.
	How string `json:"how"`
	// Why says in a sentence what the one took from the other. A
	// quotation in straight single quotes is in one of the two texts word
	// for word, so the inference can be checked against the chain itself.
	Why string `json:"why"`
}

// FoundingChain is the charters' chain on this computer: each version holds
// every text, word for word, with its SHA-512, and the links, and names the
// version before it (docs/proposals/founding-documents.md).
type FoundingChain struct {
	Folder string `json:"folder"` // where it is kept
	// Head names the newest version: "sha512:" and 128 hex digits; "" =
	// none yet.
	Head     string `json:"head,omitempty"`
	Created  string `json:"created,omitempty"` // when the newest was made, RFC 3339 in UTC
	Versions int    `json:"versions"`
	Texts    int    `json:"texts"` // how many texts the newest holds
	Links    int    `json:"links"` // how many links the newest holds
	// Verified says every version was read back now and matched its name,
	// every text its SHA-512, and every link the texts.
	Verified bool `json:"verified"`
	// Problem says what failed the check, or why nothing is on it.
	Problem string `json:"problem,omitempty"`
}

// FoundingVersion is one version on the chain.
type FoundingVersion struct {
	Name    string `json:"name"`
	Created string `json:"created"`
	Parent  string `json:"parent,omitempty"`
	Format  string `json:"format"` // its form: irie.charters/2, or the first build's irie.founding/1
	// Files are its texts' files, each with its SHA-512, as "file
	// sha512:…".
	Files []string `json:"files"`
	Links int      `json:"links"` // how many links it holds
}

// FoundingDoc is one of the documents.
type FoundingDoc struct {
	ID     string          `json:"id"`     // declaration, constitution, rights, foia, magna-carta, …
	Title  string          `json:"title"`  // The Declaration of Independence
	Short  string          `json:"short"`  // its name in the tab's choice: Declaration
	When   string          `json:"when"`   // the date it is of, as RFC 3339 writes one, or a year
	Kind   string          `json:"kind"`   // charter, statute, declaration, articles, ordinance, constitution, amendments, proclamation, address, covenant or convention
	About  string          `json:"about"`  // where its text comes from, in a sentence
	SHA512 string          `json:"sha512"` // of its file's bytes, in hex
	Blocks []FoundingBlock `json:"blocks"`
}

// FoundingBlock is one piece of a document, in order.
type FoundingBlock struct {
	// Kind is h2, h3 or h4 for a heading, text for the document's own
	// words, note for an editorial line, or list for names, one a line.
	Kind  string   `json:"kind"`
	Text  string   `json:"text,omitempty"`  // a heading, a paragraph or a note, with **bold** kept
	Items []string `json:"items,omitempty"` // a list's lines
}

// FoundingHit is a passage a search found.
type FoundingHit struct {
	Doc   string `json:"doc"`   // its document's ID
	Block int    `json:"block"` // its block's index in the document
	Where string `json:"where"` // the headings it sits under, such as "Article. I. › Section. 8."
}
