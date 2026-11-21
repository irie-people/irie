package ipc

import "time"

// Zen, a calm place (docs/proposals/zen.md): your zen's settings and their
// versions, zen starting and ending in an app, and the one thing zen
// offers at a time.
const (
	// OpZen gives your zen as it is (ZenArgs with nothing set), says that
	// zen started or ended in an app (On), changes your zen as a new version
	// (Set, Restore), answers what zen offers (Answer) or lists the
	// versions (Log).
	OpZen = "zen"
)

// ZenProfile is your zen's settings (zen.md §3).
type ZenProfile struct {
	Layout     string `json:"layout"`      // centred, edge or watch
	Columns    int    `json:"columns"`     // Centred's reading column, in columns of text: 80, 100 or 120
	TextSize   int    `json:"text_size"`   // zen's own text size in points; 0 keeps the window's
	FullScreen bool   `json:"full_screen"` // zen fills the screen
	Gentle     bool   `json:"gentle"`      // zen asks the agent to be gentle (zen.md §8)
	Breaks     int    `json:"breaks"`      // minutes at the computer before zen offers a rest; 0 for none
	Night      bool   `json:"night"`       // late at night, zen says once that you can rest
}

// ZenChange is part of your zen to change: the keys it names.
type ZenChange struct {
	Layout     *string `json:"layout,omitempty"`
	Columns    *int    `json:"columns,omitempty"`
	TextSize   *int    `json:"text_size,omitempty"`
	FullScreen *bool   `json:"full_screen,omitempty"`
	Gentle     *bool   `json:"gentle,omitempty"`
	Breaks     *int    `json:"breaks,omitempty"`
	Night      *bool   `json:"night,omitempty"`
}

// Zen is zen as the header gives it, to every client.
type Zen struct {
	// Profile is your zen, or, while a step's change is on trial, your zen
	// with that change: what a client draws.
	Profile ZenProfile `json:"profile"`
	// Head names the newest version; "" before the first.
	Head string `json:"head,omitempty"`
	// On says an app shows zen now.
	On bool `json:"on,omitempty"`
	// Trying names the step whose change is on trial.
	Trying string `json:"trying,omitempty"`
	// Offer is what zen offers now, if anything: one thing at a time,
	// until it is answered.
	Offer *ZenOffer `json:"offer,omitempty"`
}

// ZenOffer is the one thing zen offers at a time: a step of its path, a
// rest, or a word late at night. It stays until the person answers it.
type ZenOffer struct {
	ID   string `json:"id"`             // the step's id, or ZenRest or ZenNight
	Kind string `json:"kind"`           // one of the ZenKind values
	Text string `json:"text"`           // one sentence or two, in plain words
	Yes  string `json:"yes"`            // the first button's words
	No   string `json:"no,omitempty"`   // the second button's, when there is one
	Show string `json:"show,omitempty"` // what the first button also opens in the app: ZenShowHistory
	Link string `json:"link,omitempty"` // a page the text names, which a client may link
}

// What a ZenOffer is.
const (
	ZenKindLearn = "learn" // a lesson: the first button says it was read
	ZenKindTry   = "try"   // a change to try: the first button shows it, live
	ZenKindKeep  = "keep"  // a change on trial: keep it, or go back
	ZenKindRest  = "rest"  // a rest, with a breath to follow
	ZenKindNight = "night" // late at night: you can rest
)

// The ids of the offers that are not steps of the path, and what a step
// may open.
const (
	ZenRest        = "rest"
	ZenNight       = "night"
	ZenShowHistory = "history"
)

// The answers to an offer, ZenArgs' Answer: its first button or its
// second.
const (
	ZenYes = "yes"
	ZenNo  = "no"
)

// ZenArgs ask for one thing; with none, the answer is zen as it is.
type ZenArgs struct {
	// On says zen started (true) or ended (false) in an app, showing
	// Session, which it also says when zen goes on to another session.
	On      *bool  `json:"on,omitempty"`
	Session string `json:"session,omitempty"`
	// WindowText is the window's text size in points, which a step that
	// makes zen's text larger starts from while zen keeps the window's.
	WindowText int `json:"window_text,omitempty"`
	// Set changes your zen as a new version; Why says what changed, in
	// plain words, and is made from the change when empty.
	Set *ZenChange `json:"set,omitempty"`
	Why string     `json:"why,omitempty"`
	// Answer answers the offer named Offer: ZenYes or ZenNo.
	Offer  string `json:"offer,omitempty"`
	Answer string `json:"answer,omitempty"`
	// Log lists that many versions, newest first, up to MaxZenLog.
	Log int `json:"log,omitempty"`
	// Restore goes back to the zen of a version, named whole or by the
	// start of its hash, as a new version.
	Restore string `json:"restore,omitempty"`
}

// ZenResult is zen as it is after the call, the version a change made,
// and the versions asked for.
type ZenResult struct {
	Zen      Zen          `json:"zen"`
	Version  *ZenVersion  `json:"version,omitempty"`
	Versions []ZenVersion `json:"versions,omitempty"`
}

// ZenVersion is one version of your zen (zen.md §4).
type ZenVersion struct {
	// Name is the SHA-512 of the version's canonical bytes, as
	// "sha512:" and 128 hex digits; Parent names the version before it.
	Name    string    `json:"name"`
	Parent  string    `json:"parent,omitempty"`
	Created time.Time `json:"created"`
	Why     string    `json:"why"`
	// State names the settings alone: every version with the same
	// settings shares it, which is what the graph explorer draws.
	State string     `json:"state"`
	Zen   ZenProfile `json:"zen"`
}

// Limits on what OpZen takes.
const (
	MaxZenWhy = 200  // characters in a version's why
	MaxZenLog = 1000 // versions in one answer
)
