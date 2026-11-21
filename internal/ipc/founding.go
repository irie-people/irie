package ipc

// OpFounding asks for the founding documents, the founding plugin's (DESIGN
// §4.17): the Declaration of Independence, the Constitution, the Bill of
// Rights and the Freedom of Information Act, in the parts the Mac app's
// Founding tab draws, the passages a search finds, and their local chain.
const OpFounding = "founding"

// FoundingArgs ask the founding op for the documents and, with Find, for
// the passages that hold those words.
type FoundingArgs struct {
	// Find is the words to look for, whatever their spelling or capitals;
	// "" = none.
	Find string `json:"find,omitempty"`
	// Log asks for up to this many of the chain's versions, newest first.
	Log int `json:"log,omitempty"`
}

// FoundingResult is the founding op's answer: always the documents, and
// the passages Find found.
type FoundingResult struct {
	Docs  []FoundingDoc `json:"docs"`
	Found []FoundingHit `json:"found,omitempty"`
	// Chain is the documents' local chain, as the core checked it now; a
	// search leaves it out.
	Chain *FoundingChain `json:"chain,omitempty"`
	// Versions are the chain's versions Log asked for, newest first.
	Versions []FoundingVersion `json:"versions,omitempty"`
}

// FoundingChain is the founding documents' chain on this computer: each
// version holds every text, word for word, with its SHA-512, and names the
// version before it (docs/proposals/founding-documents.md).
type FoundingChain struct {
	Folder string `json:"folder"` // where it is kept
	// Head names the newest version: "sha512:" and 128 hex digits; "" =
	// none yet.
	Head     string `json:"head,omitempty"`
	Created  string `json:"created,omitempty"` // when the newest was made, RFC 3339 in UTC
	Versions int    `json:"versions"`
	// Verified says every version was read back now and matched its name,
	// and every text its SHA-512.
	Verified bool `json:"verified"`
	// Problem says what failed the check, or why nothing is on it.
	Problem string `json:"problem,omitempty"`
}

// FoundingVersion is one version on the chain.
type FoundingVersion struct {
	Name    string `json:"name"`
	Created string `json:"created"`
	Parent  string `json:"parent,omitempty"`
	// Files are its texts' files, each with its SHA-512, as "file
	// sha512:…".
	Files []string `json:"files"`
}

// FoundingDoc is one of the documents.
type FoundingDoc struct {
	ID     string          `json:"id"`     // declaration, constitution, rights or foia
	Title  string          `json:"title"`  // The Declaration of Independence
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
