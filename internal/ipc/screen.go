package ipc

import "time"

// Ops for a client that shows no terminal of its own, such as the phone app
// (docs/PHONE.md): it reads what an agent's terminal shows and answers a
// prompt there with a key, through the core, the only writer to tmux. They
// are served when hello's features name them.
const (
	// OpScreen reads what a session's terminal shows now, as plain text,
	// and the numbered choices of a prompt on it.
	OpScreen = "screen"
	// OpKeys presses keys in a session's terminal, or types text there
	// without Enter, as a person at its keyboard would.
	OpKeys = "keys"
)

// ScreenArgs name the session whose terminal to read.
type ScreenArgs struct {
	Session string `json:"session"`
}

// ScreenResult is what a session's terminal shows: its lines, top to
// bottom, without colours or the blank lines below the last one written,
// and the choices of the prompt at its bottom, if one is there. A client
// answers a choice with OpKeys and the choice's Key.
type ScreenResult struct {
	Lines   []string       `json:"lines"`
	Options []ScreenOption `json:"options"`
}

// ScreenOption is one numbered choice of a prompt, such as Claude Code's
// "2. Yes, and don't ask again".
type ScreenOption struct {
	Key      string `json:"key"`   // what to press for it: its number
	Label    string `json:"label"` // its words, as the terminal shows them
	Selected bool   `json:"selected,omitempty"`
}

// FeatureScreenLive is in hello's features when subscribe takes
// SubscribeArgs.Screen: what a session's terminal shows, with its colours,
// as it changes, for a client that draws the terminal itself.
const FeatureScreenLive = "screen-live"

// FrameScreen carries what a session's terminal shows to a client that
// subscribed to that session with SubscribeArgs.Screen: every line first,
// then the lines that changed, as they change.
const FrameScreen = "screen"

// Screen is what a session's terminal shows, or what changed in it. A line
// is as tmux's capture-pane -e writes it: its text, with the SGR escapes
// (ESC [ … m) of its colours and attributes, whose state carries on from
// one line to the next as on the terminal, and its written trailing spaces.
type Screen struct {
	Session string `json:"session"`
	Cols    int    `json:"cols"` // the terminal's size, in cells
	Rows    int    `json:"rows"`
	// Cursor is where the terminal's cursor is, from the top left, 0
	// based; null while the program hides it.
	Cursor *ScreenCursor `json:"cursor"`
	// Reset says Lines holds every line, Rows of them, top to bottom, which
	// replace all the client has; it comes first, and again when the size
	// changes. Otherwise Set holds only the lines that changed.
	Reset bool         `json:"reset,omitempty"`
	Lines []string     `json:"lines,omitempty"`
	Set   []ScreenLine `json:"set,omitempty"`
	// Options are the numbered choices of the prompt at its bottom while
	// the session waits on you, as ScreenResult's; none otherwise.
	Options []ScreenOption `json:"options"`
}

// ScreenCursor is a cell of the terminal.
type ScreenCursor struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// ScreenLine is one line of a Screen that changed: its number from the
// top, 0 based, and what it says now.
type ScreenLine struct {
	N    int    `json:"n"`
	Text string `json:"text"`
}

// KeysArgs press Keys in a session's terminal, in order, then type Text,
// if any, without Enter. A key is one letter or digit, or one of KeyNames.
type KeysArgs struct {
	Session string   `json:"session"`
	Keys    []string `json:"keys,omitempty"`
	Text    string   `json:"text,omitempty"`
}

// KeyNames are the keys OpKeys presses by name, as tmux names them. Ctrl+C
// is not among them: twice, it ends Claude Code; Escape stops a turn.
var KeyNames = []string{"Enter", "Escape", "Up", "Down", "Left", "Right", "Tab", "BTab", "Space", "BSpace", "PageUp", "PageDown", "Home", "End"}

// OpPair opens a code that pairs a phone with this machine's phone app
// (docs/PHONE.md), for a client to show with the setup page's address,
// as `irie remote pair` does in a terminal.
const OpPair = "pair"

// PairArgs say how long the code works, in minutes; 0 is ten.
type PairArgs struct {
	Minutes int `json:"minutes,omitempty"`
}

// PairResult is what a phone needs to set up and pair: the setup page's
// addresses, best first, the six-digit code, and until when it works.
type PairResult struct {
	On    bool      `json:"on"`   // remote.on: the gateway runs
	Name  string    `json:"name"` // what the phone calls this machine
	URLs  []string  `json:"urls"`
	Code  string    `json:"code"`
	Until time.Time `json:"until"`
	// Fingerprint is the start of the SHA-256 fingerprint of Irie's
	// certificate authority, which the person checks on the phone before
	// trusting it, so a stranger on the network can't stand in for it.
	Fingerprint string `json:"fingerprint"`
	// Link pairs the native iPhone app: irie://pair with the gateway's
	// address, the code and the whole fingerprint, for a QR code.
	Link string `json:"link"`
}
