package core

import (
	"maps"
	"os"
	"path/filepath"
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
// it: the mail op still turns the agent away. A code card may name a file
// to open: absolute, clean, a regular file of at most ipc.CardFileMax
// bytes, with a cursor line and column from 1.
func (c *Core) cardOp(a ipc.CardArgs) error {
	if !slices.Contains(ipc.CardKinds, a.Kind) {
		return ipc.Errorf(ipc.CodeBadArgs, "No card is called %q: they are %s.", a.Kind, strings.Join(ipc.CardKinds, ", "))
	}
	if err := checkCardFile(a); err != nil {
		return err
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
	f := ipc.Frame{T: ipc.FrameCard, ID: a.Session, Kind: a.Kind, Path: a.Path, Line: a.Line, Col: a.Col}
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

// checkCardFile refuses a path a card can't open, or one on a card that
// takes none, and a cursor before the file's first line or column.
func checkCardFile(a ipc.CardArgs) error {
	if a.Line < 0 || a.Col < 0 {
		return ipc.Errorf(ipc.CodeBadArgs, "A line or column starts at 1: give 0 or leave it out for none.")
	}
	if a.Path == "" {
		return nil
	}
	if a.Kind != "code" {
		return ipc.Errorf(ipc.CodeBadArgs, "Only a code card opens a file: leave the path out of a %s card.", a.Kind)
	}
	if !filepath.IsAbs(a.Path) || filepath.Clean(a.Path) != a.Path {
		return ipc.Errorf(ipc.CodeBadArgs, "The path %q is not absolute and clean: give the file's full path, with no . or .. in it.", a.Path)
	}
	fi, err := os.Stat(a.Path)
	switch {
	case err != nil:
		return ipc.Errorf(ipc.CodeBadArgs, "No file at %q to open: give the path of one that exists.", a.Path)
	case !fi.Mode().IsRegular():
		return ipc.Errorf(ipc.CodeBadArgs, "%q is not a regular file: give the path of a file, not a folder or device.", a.Path)
	case fi.Size() > ipc.CardFileMax:
		return ipc.Errorf(ipc.CodeBadArgs, "%q is over %d MiB, too big for a code card: open a smaller file.", a.Path, ipc.CardFileMax>>20)
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
