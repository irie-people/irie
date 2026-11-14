package core

// The boost after API errors (DESIGN §4.3). When a session's turn fails on
// an API error that may pass (passing: server_error, overloaded, unknown,
// max_output_tokens) twice in a row, Irie boosts it for a while: its whole
// process tree comes out of the background, as a promoted one's is, but
// its card stays where it is, with no promotion's badge, and no other
// session steps back for it. Irie then types its agent one short message
// to carry its turn on. The boost ends with the first turn that ends
// without an API error, or after boostFor at most. The journal keeps both
// ends (boost.on, boost.off), so a restarted core goes on with a boost, and
// the card says it in plain words while it lasts.

import (
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
)

// boostAfter is how many turns in a row must fail on an API error that may
// pass before Irie boosts a session.
const boostAfter = 2

// boostFor is the longest a boost lasts.
const boostFor = 15 * time.Minute

// carryOnDelay is how long Irie waits after the second failed turn before
// it types the carry-on message, so that its agent is back at its prompt.
var carryOnDelay = 2 * time.Second

// boostedLine is what a boosted session's card says first.
const boostedLine = "Boosted after two API errors"

// carryOnText is the message Irie types to a boosted session's agent.
const carryOnText = "[" + brand.Name + "] Your turn ended on an API error. Carry on where you stopped."

// Why a boost ended (boost.off's why).
const (
	boostWell    = "turn" // a turn ended without an API error
	boostTimeout = "time" // boostFor passed
)

// boostTurn starts or ends session x's boost at event e, which was just
// recorded, and reports whether it did: a turn that failed on an API
// error that may pass, the second in a row, starts one; a turn that ends
// without an API error ends it. The caller holds c.mu, and moves the trees
// (background) when it changed.
func (c *Core) boostTurn(x *session, e event.Event) bool {
	if e.Agent != event.Main {
		return false
	}
	switch {
	case x.track.boost == nil && e.Kind == event.TurnFailed && x.track.apiFails >= boostAfter:
		// The failed turn as the journal stamped it.
		failed := x.track.needs[event.Main].Time()
		c.record(x, event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.BoostOn,
			Data: map[string]any{"error": e.Str("error"), "turns": boostAfter, "until": event.Stamp(time.Now().Add(boostFor))}})
		c.log.Info("boosted", "session", x.id, "name", x.name, "error", e.Str("error"))
		if x.harness == "claude-code" {
			go c.carryOn(x.id, failed)
		}
		return true
	case x.track.boost != nil && endsWell(e):
		c.unboost(x, boostWell)
		return true
	}
	return false
}

// endsWell reports whether e is the end of a turn without an API error: its
// stop, or the stop the jot dog or the trod bot sent on in its place.
func endsWell(e event.Event) bool {
	switch e.Kind {
	case event.TurnEnded, event.DogNudged:
		return true
	case event.TrodSent:
		return e.Str("how") == trodAtStop
	}
	return false
}

// unboost ends session x's boost. The caller holds c.mu, and moves the
// trees (background).
func (c *Core) unboost(x *session, why string) {
	c.record(x, event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.BoostOff, Data: map[string]any{"why": why}})
	c.log.Info("unboosted", "session", x.id, "name", x.name, "why", why)
}

// endBoosts ends each boost whose time is up, and reports whether it ended
// any. The caller holds c.mu, and moves the trees (background).
func (c *Core) endBoosts(now time.Time) bool {
	ended := false
	for _, x := range c.sessions {
		if b := x.track.boost; b != nil && !now.Before(b.Time().Add(boostFor)) {
			c.unboost(x, boostTimeout)
			ended = true
		}
	}
	return ended
}

// carryOn types the carry-on message to session id's agent once the turn
// that failed at failed has settled: as send does, but only into the
// agent's own prompt with nothing typed in it, as continue types its word
// (send.go), so it never joins what the user writes there. A long message
// wraps in a narrow terminal, which Claude Code's prompt grows to hold, so
// Enter waits on no look at the prompt. It leaves a session that no longer
// waits on that turn: one sent on meanwhile, by the user, a follow-up or the
// trod bot, one whose boost ended, and one frozen or closed. It holds no
// lock.
func (c *Core) carryOn(id string, failed time.Time) {
	time.Sleep(carryOnDelay)
	err := c.typeTo(id, carryOnText, func(x *session, _ string) error {
		switch {
		case x.dead || x.track.boost == nil || x.track.frozen != nil:
			return ipc.Errorf(ipc.CodeBusy, "%s is not boosted any more", x.name)
		case !failedTurn(x) || !x.track.needs[event.Main].Time().Equal(failed) || typedSince(x, failed):
			return ipc.Errorf(ipc.CodeBusy, "%s went on without it", x.name)
		}
		typed, shows, err := c.promptIn(x.pane)
		switch {
		case err != nil:
			return err
		case !shows:
			return ipc.Errorf(ipc.CodeBusy, "%s shows no prompt to type to", x.name)
		case typed != "":
			return ipc.Errorf(ipc.CodeBusy, "%s's prompt has something typed in it", x.name)
		}
		return nil
	}, nil)
	if err != nil {
		c.log.Info("boost: no carry-on", "session", id, "err", err)
	}
}

// typedSince reports whether a message went to session x's agent after at
// that has not come in as a prompt yet. The caller holds c.mu.
func typedSince(x *session, at time.Time) bool {
	if x.conv == nil {
		return false
	}
	for _, it := range x.conv.pending {
		if it.Time.After(at) {
			return true
		}
	}
	return false
}

// boosted reports whether session id is boosted after its API errors now.
// The caller holds c.mu.
func (c *Core) boosted(id string) bool {
	x := c.sessions[id]
	return x != nil && x.track.boost != nil
}
