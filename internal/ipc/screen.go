package ipc

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
