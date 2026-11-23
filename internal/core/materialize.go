package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/genui"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/ledger"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/materialize"
)

// Materialized cards (docs/proposals/materialized-cards.md): the core keeps
// the cards local models made in cards.json, gives a view what is kept at
// once, and has a card made again only when it is out of date.

// remakeTimeout bounds one remake: finding a local model, loading it and
// its answer. It is under the store's claim, so a remake that runs this
// long is still the one that counts.
const remakeTimeout = 4 * time.Minute

// cardState is the store of materialized cards and the remakes under way.
// It has a lock of its own, never held with mu.
type cardState struct {
	once  sync.Once
	store *materialize.Store
	turn  chan struct{} // one remake at a time, so local models load one by one

	mu    sync.Mutex
	runs  map[string]chan struct{} // each card's newest remake, closed when it is done
	about map[string]explainAbout  // what a card that explains, being made, is about, for the library
	fresh map[string]bool          // the cards that explain the person asked to have made again
	lib   sync.Mutex               // the library's files, one change at a time (explainlib.go)
}

// cardStore is the store, read from cards.json the first time a card is
// asked for. A file that does not read is a cache lost, not a fault: the
// cards are made again, and the file written over.
func (c *Core) cardStore() *materialize.Store {
	c.cards.once.Do(func() {
		c.cards.turn = make(chan struct{}, 1)
		c.cards.runs = map[string]chan struct{}{}
		c.cards.about = map[string]explainAbout{}
		c.cards.fresh = map[string]bool{}
		s, err := materialize.Open(c.paths.Cards())
		if err != nil {
			c.log.Warn("materialize: the cards' file does not read; its cards will be made again", "err", err)
		}
		c.cards.store = s
	})
	return c.cards.store
}

// materializeOp answers the materialize op: the card a recipe makes, as
// kept, with a remake begun when one is due, and waited for when asked.
func (c *Core) materializeOp(a ipc.MaterializeArgs) (ipc.MaterializeResult, error) {
	var err error
	var about *explainAbout
	switch {
	case a.Explain != "":
		var ab explainAbout
		a, ab, err = c.explainCardArgs(a)
		about = &ab
	case a.Session != "":
		a, err = c.sessionCardArgs(a)
	}
	if err != nil {
		return ipc.MaterializeResult{}, err
	}
	if about != nil && a.Keep != nil {
		return c.keepExplained(*about, a.ID, *a.Keep)
	}
	if about != nil && a.Forget {
		// Made again on the next ask, by a model, not from the library.
		c.cards.mu.Lock()
		c.cards.fresh[a.ID] = true
		c.cards.mu.Unlock()
	}
	switch {
	case a.ID == "" || len(a.ID) > ipc.MaxMaterializeID:
		return ipc.MaterializeResult{}, ipc.Errorf(ipc.CodeBadArgs, "materialize: a card's id is 1 to %d bytes", ipc.MaxMaterializeID)
	case a.Forget:
		if err := c.cardStore().Forget(a.ID); err != nil {
			c.log.Warn("materialize: the cards' file", "err", err)
		}
		return ipc.MaterializeResult{ID: a.ID, State: string(materialize.Cold)}, nil
	case strings.TrimSpace(a.Prompt) == "":
		return ipc.MaterializeResult{}, ipc.Errorf(ipc.CodeBadArgs, "materialize: say what the card is to show")
	case len(a.Prompt) > ipc.MaxMaterializePrompt || len(a.Context) > ipc.MaxMaterializeText:
		return ipc.MaterializeResult{}, ipc.Errorf(ipc.CodeBadArgs, "materialize: a prompt of at most %d bytes, and context of at most %d", ipc.MaxMaterializePrompt, ipc.MaxMaterializeText)
	case a.Every < 0 || a.Gap < 0 || a.Wait < 0:
		return ipc.MaterializeResult{}, ipc.Errorf(ipc.CodeBadArgs, "materialize: every, gap and wait are not below zero")
	}
	s := c.cardStore()
	r := recipeOf(a)
	card, st, rm, err := s.Want(r, time.Now())
	if err != nil {
		c.log.Warn("materialize: the cards' file", "err", err)
	}
	if rm != nil && about != nil {
		if e := c.savedFor(*about, a.ID); e != nil {
			// A question the library has answered: its newest version, at
			// once, with no model.
			v := e.Latest()
			card, _, err = s.Finish(*rm, v.Card, v.By, nil, time.Now())
			if err != nil {
				c.log.Warn("materialize: the cards' file", "err", err)
			}
			st, rm = materialize.Fresh, nil
		} else {
			c.cards.mu.Lock()
			c.cards.about[a.ID] = *about
			c.cards.mu.Unlock()
		}
	}
	if rm != nil {
		c.remakeCard(s, *rm)
	}
	if a.Wait > 0 && (st == materialize.Cold || st == materialize.Refreshing) {
		c.cards.mu.Lock()
		done := c.cards.runs[a.ID]
		c.cards.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-time.After(min(a.Wait, ipc.MaxMaterializeWait)):
			}
			card, st = s.Peek(r, time.Now())
		}
	}
	res := ipc.MaterializeResult{ID: card.ID, State: string(st), Spec: card.Spec, Made: card.Made, By: card.By, Err: card.Err, Began: rm != nil}
	if about != nil {
		c.describeSaved(&res, *about)
	}
	return res, nil
}

// recipeOf is the recipe an ask names. Its inputs are the context's digest
// unless the ask gives its own, so new text puts the card out of date; a
// card that explains reads its inputs from explainCardArgs alone, since
// its context moves with every step of the session it is asked in.
func recipeOf(a ipc.MaterializeArgs) materialize.Recipe {
	inputs, kind := a.Inputs, genui.KindSummary
	if a.Explain != "" {
		kind = genui.KindExplain
	} else if inputs == "" && a.Context != "" {
		sum := sha256.Sum256([]byte(a.Context))
		inputs = hex.EncodeToString(sum[:8])
	}
	return materialize.Recipe{
		ID: a.ID, Prompt: a.Prompt, Profile: "local", Catalog: genui.Catalog, Kind: kind,
		Inputs: inputs, Every: a.Every, Gap: a.Gap, Context: a.Context,
	}
}

// remakeCard runs rm on a local model, in its turn, and keeps what it
// made. A remake overtaken while it waited for its turn is dropped before
// it loads anything.
func (c *Core) remakeCard(s *materialize.Store, rm materialize.Remake) {
	done := make(chan struct{})
	c.cards.mu.Lock()
	c.cards.runs[rm.Recipe.ID] = done
	c.cards.mu.Unlock()
	go func() {
		defer func() {
			c.cards.mu.Lock()
			if c.cards.runs[rm.Recipe.ID] == done {
				delete(c.cards.runs, rm.Recipe.ID)
			}
			c.cards.mu.Unlock()
			close(done)
		}()
		c.cards.turn <- struct{}{}
		defer func() { <-c.cards.turn }()
		if !s.Newest(rm) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), remakeTimeout)
		defer cancel()
		spec, by, err := c.makeCard(ctx, rm.Recipe)
		card, newest, ferr := s.Finish(rm, spec, by, err, time.Now())
		if ferr == nil && rm.Recipe.Kind == genui.KindExplain {
			ferr = s.Trim(explainIDs, explainKept)
		}
		// A card that explains is kept in the library too, as the newest
		// version of its question.
		c.cards.mu.Lock()
		ab, explains := c.cards.about[rm.Recipe.ID]
		delete(c.cards.about, rm.Recipe.ID)
		c.cards.mu.Unlock()
		if explains && newest && err == nil {
			c.saveExplained(ab, spec, by)
		}
		if ferr != nil {
			c.log.Warn("materialize: the cards' file", "err", ferr)
		}
		c.log.Info("materialize", "id", rm.Recipe.ID, "by", by, "kept", newest, "failed", card.Err)
	}()
}

// makeCard has a local model make the card r asks for: materialize.model,
// else the best one for chat on this computer's Ollama, never one in the
// cloud, so a card costs no tokens. Its calls go into the ledger as Irie's
// own.
func (c *Core) makeCard(ctx context.Context, r materialize.Recipe) (json.RawMessage, string, error) {
	cfg := c.cfg()
	m, err := localmodel.FindChatModel(ctx, localmodel.ChatServers(cfg.Local.URL, cfg.Local.Port), cfg.Materialize.Model)
	if err != nil {
		return nil, "", err
	}
	spec, err := genui.Make(c.metered(ctx, ledger.Card), m, r.Kind, r.Prompt, r.Context)
	return spec, m.Name, err
}
