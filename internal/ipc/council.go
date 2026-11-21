package ipc

import (
	"time"

	"github.com/irie-people/irie/internal/council"
)

// Raguel's council (DESIGN §4.11, proposals/council.md): a free tally of
// what the agents did and what it cost, and a session that runs as him,
// which reads it and brings the user a few plans to say yes or no to.
const (
	// OpCouncil is Raguel's report: the latest council, its plans and when
	// the next is due. With Do it tallies, convenes a council, takes a
	// plan or the council's word that it is done from the council's own
	// session, or takes the user's yes or no to plans. It answers a
	// CouncilResult.
	OpCouncil = "council"
)

// What OpCouncil does, CouncilArgs' Do.
const (
	CouncilReport  = ""
	CouncilTally   = "tally"
	CouncilConvene = "convene"
	CouncilPropose = "propose"
	CouncilDone    = "done"
	CouncilYes     = "yes"
	CouncilNo      = "no"
)

// CouncilArgs asks Raguel for his report, or with Do for more: tally
// counts Days of the journal, and as many before; convene, with OK, holds
// a council now; propose puts Plan in, and done ends the council with its
// Summary, both only from the council's own session; yes, with OK, starts
// Plans as jots, and no turns them down, saying Why, both the user's word
// alone.
type CouncilArgs struct {
	Do      string        `json:"do,omitempty"`
	Days    int           `json:"days,omitempty"`    // tally: how long a stretch is; 7 when 0
	OK      bool          `json:"ok,omitempty"`      // convene, yes: the person said OK, having read what it starts
	Plan    *council.Plan `json:"plan,omitempty"`    // propose
	Summary string        `json:"summary,omitempty"` // done
	Plans   []int         `json:"plans,omitempty"`   // yes, no: the plans, by their numbers in the latest council
	Why     string        `json:"why,omitempty"`     // no: the user's reason
}

// CouncilResult is Raguel's report (OpCouncil), with what Do did.
type CouncilResult struct {
	On      bool             `json:"on"`               // arch.raguel.on
	Running bool             `json:"running"`          // a council sits now
	Latest  *council.Council `json:"latest,omitempty"` // the latest council and its plans, without its tally
	// Next is when a council is due by himself, from which he holds one
	// at a quiet time; nil while arch.raguel.every is 0s.
	Next     *time.Time     `json:"next,omitempty"`
	Tally    *council.Tally `json:"tally,omitempty"`    // tally
	Markdown string         `json:"markdown,omitempty"` // tally: the tally as a council reads it
	Session  *Session       `json:"session,omitempty"`  // convene: the council's session
	Plan     int            `json:"plan,omitempty"`     // propose: the number the plan got
	Minutes  []string       `json:"minutes,omitempty"`  // done: the files of the minutes
	Jots     []CouncilJot   `json:"jots,omitempty"`     // yes: the jots the plans started
}

// CouncilJot is a jot a plan's yes started.
type CouncilJot struct {
	Plan    int    `json:"plan"`
	Session string `json:"session"`
	Name    string `json:"name"`
	Cwd     string `json:"cwd"`
}
