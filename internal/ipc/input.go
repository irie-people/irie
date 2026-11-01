package ipc

import "time"

// OpInput reads what a person typed in a task bar or command bar, and does
// it: starts a task, answers a status question or runs a : command (DESIGN
// §4.2). Every client sends its typing here, so the words mean the same in
// all of them, and a client keeps no rules of its own (docs/CORE.md).
const OpInput = "input"

// What an input did, InputResult's Did.
const (
	InputSpawned  = "spawned"  // it started the task: Session
	InputAdded    = "added"    // the task went to Session's agent, as more for the jot before it: Until
	InputAnswered = "answered" // it answered a question and changed nothing
	InputRan      = "ran"      // it ran a : command
	InputAsked    = "asked"    // nothing changed yet: Choice asks what to do
)

// InputArgs is what a person typed, and what their client had selected.
type InputArgs struct {
	// Text is what they typed, verbatim. Only its first line is read for
	// words; the rest goes to the task as it is.
	Text string `json:"text"`
	// Pastes are long pastes that the client shows as labels in Text. Each
	// goes back whole in place of its label once the words are read, so a
	// paste's own @ and + words stay text.
	Pastes []Paste `json:"pastes,omitempty"`
	// Session is the selected session, which :rename, :rate, :close,
	// :freeze, :thaw, :prio, :park and :unpark act on.
	Session string `json:"session,omitempty"`
	// Outside is the selected session outside Irie, by its key (its vendor
	// id, else id:<short id>), which :move with no name brings in.
	Outside string `json:"outside,omitempty"`
	// Repo, InPlace, Profile, Model and Tutor say where, on what and how a
	// task runs when its words do not: the client's own menus. The words
	// win. Tutor is off, explain or teach (docs/proposals/learning.md).
	Repo    string `json:"repo,omitempty"`
	InPlace bool   `json:"in_place,omitempty"`
	Profile string `json:"profile,omitempty"`
	Model   string `json:"model,omitempty"`
	Tutor   string `json:"tutor,omitempty"`
	// Apart is the session a task typed before was added to, as more for
	// its jot: the task is taken back from it and started on its own.
	Apart string `json:"apart,omitempty"`
}

// Paste is a long paste that a client shows as Label in what was typed.
type Paste struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// InputResult says what an input did, in words a client shows as they are.
type InputResult struct {
	Did string `json:"did"`
	// Op is the op the input ran, such as spawn, park or undo; empty for a
	// question it answered.
	Op string `json:"op,omitempty"`
	// Text is what to tell the person.
	Text string `json:"text"`
	// Session is the session it started or acted on, when one.
	Session *Session `json:"session,omitempty"`
	// Show asks the client to show Session and give it the keyboard: a tool
	// session, such as :setup's, waits on its user.
	Show bool `json:"show,omitempty"`
	// Choice is the question to answer when Did is asked: send Op (undo or
	// redo) with the choice's id and the key of the option picked.
	Choice *Choice `json:"choice,omitempty"`
	// Until is, when Did is added, when the task is typed to the agent;
	// until then the same text sent with Apart takes it back.
	Until time.Time `json:"until,omitzero"`
}
