package ipc

// OpFounding asks for the founding documents, the founding plugin's (DESIGN
// §4.17): the Declaration of Independence, the Constitution and the Bill of
// Rights, in the parts the Mac app's Founding tab draws, and the passages a
// search finds.
const OpFounding = "founding"

// FoundingArgs ask the founding op for the documents and, with Find, for
// the passages that hold those words.
type FoundingArgs struct {
	// Find is the words to look for, whatever their spelling or capitals;
	// "" = none.
	Find string `json:"find,omitempty"`
}

// FoundingResult is the founding op's answer: always the documents, and
// the passages Find found.
type FoundingResult struct {
	Docs  []FoundingDoc `json:"docs"`
	Found []FoundingHit `json:"found,omitempty"`
}

// FoundingDoc is one of the documents.
type FoundingDoc struct {
	ID     string          `json:"id"`     // declaration, constitution or rights
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
