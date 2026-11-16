package ipc

// Links clicked in a client's terminals (docs/MAC.md, Links in the
// terminal). The client hands the core what was clicked and in whose
// terminal; the core says what it opens as, by ui.terminal_links and the
// session's folder, so every client opens a link alike and keeps no rule
// of its own. Served when hello's features name OpLink.
const (
	// OpLink says where a link clicked in a session's terminal opens: a
	// card of the session's panes, or the computer's own app for it. It
	// changes nothing; the client opens it.
	OpLink = "link"
)

// LinkArgs are what was clicked, and where.
type LinkArgs struct {
	// Session is the session whose terminal, or whose card's shell, showed
	// the link; none for a terminal of no session of Irie's, whose link
	// opens in the computer's own app.
	Session string `json:"session,omitempty"`
	// Link is the link as the terminal gives it: an address it found in
	// the text or one a program wrote (OSC 8), or a file's path as shown,
	// relative to the session's folder, perhaps with :LINE or :LINE:COL.
	Link string `json:"link"`
	// Other opens it in the other place than ui.terminal_links names, as a
	// ⇧⌘-click asks.
	Other bool `json:"other,omitempty"`
}

// LinkMax is the longest link the op reads.
const LinkMax = 8 << 10

// Where a link opens (LinkResult.In).
const (
	// LinkInCard is a card of the session's panes: Kind browser showing
	// URL, or code with Path open at Line and Col.
	LinkInCard = "card"
	// LinkInSystem is the computer's own app for URL: the default browser
	// for a page, the app that takes its scheme (mail for mailto:), and a
	// file's own app, or the file manager for a folder.
	LinkInSystem = "system"
	// LinkInReveal is the file manager with Path picked out, never opened:
	// an app, an installer, or a file that would run something if opened,
	// which a link's text may not show.
	LinkInReveal = "reveal"
)

// LinkResult is where a link opens, and as what.
type LinkResult struct {
	In   string `json:"in"`             // LinkInCard, LinkInSystem or LinkInReveal
	Kind string `json:"kind,omitempty"` // a card's: browser or code
	// URL is the page a browser card shows, or what the computer's own
	// app opens: a file's as a file:// URL.
	URL string `json:"url,omitempty"`
	// Path is the file a code card opens, absolute and clean, a regular
	// file of text of at most CardFileMax bytes, or the one to pick out in
	// the file manager; Line and Col are where a code card's cursor goes,
	// from 1, or 0 for none.
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}
