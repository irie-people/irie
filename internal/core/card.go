package core

import (
	"maps"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// cardOp asks every Mac app open to show a card of a kind beside a session
// (ipc.OpCard): the app adds one when the session has none of that kind,
// and turns the session's panes to it when the session is on show. A
// plugin's card waits for its plugin. The app alone reads what the card
// shows, so an agent that opens its person's mail beside it sees none of
// it: the mail op still turns the agent away.
func (c *Core) cardOp(a ipc.CardArgs) error {
	if !slices.Contains(ipc.CardKinds, a.Kind) {
		return ipc.Errorf(ipc.CodeBadArgs, "No card is called %q: they are %s.", a.Kind, strings.Join(ipc.CardKinds, ", "))
	}
	cfg := c.cfg()
	switch {
	case a.Kind == "mail" && !cfg.MailOn():
		return ipc.Errorf(ipc.CodeBadArgs, "Mail is off, as its plugin is: `%s plugins on mail` turns it on.", brand.Slug)
	case a.Kind == "markets" && !cfg.MarketsOn():
		return ipc.Errorf(ipc.CodeBadArgs, "Markets is off, as its plugin is: `%s plugins on markets` turns it on.", brand.Slug)
	}
	c.mu.Lock()
	x := c.sessions[a.Session]
	subs := slices.Collect(maps.Keys(c.subs))
	c.mu.Unlock()
	if x == nil {
		return ipc.Errorf(ipc.CodeNotFound, "No session %q.", a.Session)
	}
	f := ipc.Frame{T: ipc.FrameCard, ID: a.Session, Kind: a.Kind}
	n := 0
	for _, s := range subs {
		if s.noteCard(f) {
			n++
		}
	}
	if n == 0 {
		return ipc.Errorf(ipc.CodeNotFound, "No %s app is open to show it.", brand.Name)
	}
	return nil
}

// noteCard queues a card frame for a Mac app's subscription, and says
// whether this is one.
func (s *subscriber) noteCard(f ipc.Frame) bool {
	if s.view != ipc.ViewApp {
		return false
	}
	s.mu.Lock()
	s.queue = append(s.queue, f)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return true
}
