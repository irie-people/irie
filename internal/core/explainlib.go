package core

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/explained"
	"github.com/irie-people/irie/internal/ipc"
)

// The Teach tile's library (learn.library, internal/explained): every card
// that explains, once made, is kept as files, a question asked again is
// answered from them at once, every turn of the tile's chat is a version
// kept with what was said, and the person's word that one helped is kept
// with it.

// savedMost bounds the explanations the learn op lists for a project.
const savedMost = 12

// compact is a card read back from the library, which writes it indented
// for people, compacted: a model reads fewer bytes, and a view the same.
func compact(spec json.RawMessage) json.RawMessage {
	var b bytes.Buffer
	if json.Compact(&b, spec) != nil {
		return spec
	}
	return b.Bytes()
}

// library is where explanations are kept as files, and whether they are
// (learn.library), its ~ the home folder the core runs with.
func (c *Core) library() (explained.Library, bool) {
	dir := c.cfg().Learn.Library
	switch {
	case dir == "" || dir == "off":
		return explained.Library{}, false
	case strings.HasPrefix(dir, "~/"):
		dir = filepath.Join(c.paths.Home, dir[2:])
	}
	return explained.Library{Dir: dir}, true
}

// savedFor is the explanation the library keeps for ab's question, to
// answer it with at once; nil when it keeps none, for a turn of the chat,
// which is made anew, or when the person asked for the card under id to
// be made again.
func (c *Core) savedFor(ab explainAbout, id string) *explained.Explanation {
	c.cards.mu.Lock()
	again := c.cards.fresh[id]
	delete(c.cards.fresh, id)
	c.cards.mu.Unlock()
	lib, ok := c.library()
	if !ok || ab.asked != "" || again {
		return nil
	}
	c.cards.lib.Lock()
	defer c.cards.lib.Unlock()
	if e, err := lib.Load(ab.project, ab.question); err == nil && e != nil && len(e.Versions) > 0 {
		return e
	}
	return nil
}

// saveExplained keeps a card a local model made in the library, as the
// newest version of its question.
func (c *Core) saveExplained(ab explainAbout, spec json.RawMessage, by string) {
	lib, ok := c.library()
	if !ok || len(spec) == 0 {
		return
	}
	c.cards.lib.Lock()
	defer c.cards.lib.Unlock()
	if _, _, err := lib.Add(ab.project, ab.question, ab.topic, explained.Version{Card: spec, Made: time.Now(), By: by, Asked: ab.asked}); err != nil {
		c.log.Warn("explained: the library", "err", err)
	}
}

// describeSaved adds to r what the library keeps of ab's question: its
// file, its versions and whether it was kept; and, for the question asked
// rather than a turn of the chat, its newest version as the card to show,
// so a card made in the chat is what the question shows after.
func (c *Core) describeSaved(r *ipc.MaterializeResult, ab explainAbout) {
	lib, ok := c.library()
	if !ok {
		return
	}
	c.cards.lib.Lock()
	e, err := lib.Load(ab.project, ab.question)
	c.cards.lib.Unlock()
	if err != nil || e == nil || len(e.Versions) == 0 {
		return
	}
	r.Saved, _ = lib.Files(ab.project, ab.question)
	r.Versions, r.Kept = len(e.Versions), e.Kept
	if ab.asked == "" {
		v := e.Latest()
		r.Spec, r.Made, r.By = compact(v.Card), v.Made, v.By
	}
}

// keepExplained marks ab's explanation as one that helped, or not: one kept
// is an example for the ones made after it.
func (c *Core) keepExplained(ab explainAbout, id string, on bool) (ipc.MaterializeResult, error) {
	lib, ok := c.library()
	if !ok {
		return ipc.MaterializeResult{}, ipc.Errorf(ipc.CodeBadArgs, "materialize: the library is off; learn.library keeps explanations")
	}
	c.cards.lib.Lock()
	e, err := lib.Keep(ab.project, ab.question, on)
	c.cards.lib.Unlock()
	switch {
	case err != nil:
		return ipc.MaterializeResult{}, err
	case e == nil:
		return ipc.MaterializeResult{}, ipc.Errorf(ipc.CodeNotFound, "materialize: no explanation of that question is kept yet")
	}
	md, _ := lib.Files(ab.project, ab.question)
	return ipc.MaterializeResult{ID: id, State: "fresh", Saved: md, Versions: len(e.Versions), Kept: e.Kept}, nil
}

// savedListFor is savedList for a learn op asked for a session, and none
// without one.
func (c *Core) savedListFor(session bool, project string) []ipc.SavedExplanation {
	if !session {
		return nil
	}
	return c.savedList(project)
}

// savedList is what the library keeps for project, the newest first, for
// the learn op.
func (c *Core) savedList(project string) []ipc.SavedExplanation {
	lib, ok := c.library()
	if !ok {
		return nil
	}
	c.cards.lib.Lock()
	all := lib.All(project)
	c.cards.lib.Unlock()
	var out []ipc.SavedExplanation
	for _, e := range all {
		if len(out) == savedMost {
			break
		}
		md, _ := lib.Files(e.Project, e.Question)
		out = append(out, ipc.SavedExplanation{Question: e.Question, Topic: e.Topic, File: md, Versions: len(e.Versions), Kept: e.Kept, Made: e.Latest().Made})
	}
	return out
}
