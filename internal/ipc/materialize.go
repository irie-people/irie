package ipc

import (
	"encoding/json"
	"time"
)

// Materialized cards (docs/proposals/materialized-cards.md): a card a local
// model made from a recipe, from the catalog of internal/genui, which the
// core keeps in cards.json and gives a view at once, and has made again
// only when it is out of date.
const (
	// OpMaterialize asks for the card a recipe makes: what is kept, the
	// state to draw it in, and, when one is due, a remake begun on a local
	// model, which costs no tokens. With Forget, it drops the card instead.
	OpMaterialize = "materialize"
)

// The most an ask may carry, in bytes, past which it is refused.
const (
	MaxMaterializeID     = 200
	MaxMaterializePrompt = 4 << 10
	MaxMaterializeText   = 64 << 10
	MaxExplain           = 2 << 10 // what an explaining card is asked to explain
)

// MaxMaterializeWait is the longest an ask waits for its card's remake.
const MaxMaterializeWait = 5 * time.Minute

// MaterializeArgs are a recipe by its parts, and how to ask for it.
type MaterializeArgs struct {
	// Session asks for a session's own card, where it stands, which the
	// core makes from its row and its conversation: the core writes ID,
	// Prompt, Context, Inputs and Gap. Off unless materialize.sessions.
	Session string `json:"session,omitempty"`
	// Explain asks instead for a card that explains what it says, a
	// picture first, as the Teach tile does: the core writes the rest of
	// the recipe, and with Session it reads that session's conversation,
	// so that a question about its work is answered from it. Made on a
	// local model like every card, so it costs no tokens, and kept, so the
	// same question asked again draws at once.
	Explain string `json:"explain,omitempty"`
	ID      string `json:"id,omitempty"`     // the card's name, such as "repo/irie/recent"
	Prompt  string `json:"prompt,omitempty"` // what the card is to show; a new one leaves the kept card cold
	// Context is the text the model makes the card from. Its digest is
	// the card's inputs unless Inputs is given, so new text puts the card
	// out of date, and the old card stays on show while the next is made.
	Context string        `json:"context,omitempty"`
	Inputs  string        `json:"inputs,omitempty"`
	Every   time.Duration `json:"every,omitempty"` // the most the card may age; 0 for no limit
	Gap     time.Duration `json:"gap,omitempty"`   // the least time between two cards; 0 for none
	// Wait is how long the reply waits for the card's remake, when one
	// runs, up to MaxMaterializeWait; 0 answers at once.
	Wait   time.Duration `json:"wait,omitempty"`
	Forget bool          `json:"forget,omitempty"`
}

// MaterializeResult is the card as it stands after the ask: State is cold,
// fresh, stale or refreshing, and Spec the card in the catalog's JSON,
// none while cold. Err is why the last remake failed, and Began whether
// this ask began one.
type MaterializeResult struct {
	ID    string          `json:"id"`
	State string          `json:"state"`
	Spec  json.RawMessage `json:"spec,omitempty"`
	Made  time.Time       `json:"made,omitzero"`
	By    string          `json:"by,omitempty"` // the local model that made it
	Err   string          `json:"err,omitempty"`
	Began bool            `json:"began,omitempty"`
}
