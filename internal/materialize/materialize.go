// Package materialize keeps the cards a model made, so that a view draws
// one at once and a model makes it again only when it is out of date
// (docs/proposals/materialized-cards.md). A card is a small JSON tree from
// Irie's catalog (docs/proposals/generative-ui.md §7.3), which this
// package keeps compacted, and never reads.
//
// The ideas are Macro's AI projections (ideas only, no code; PROVENANCE.md):
// a recipe's hash versions all that makes a card, so a change to it leaves
// the kept card cold; one remake runs at a time, under a claim that a crash
// cannot strand; and only the newest remake's card is kept.
package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/filetx"
)

// Recipe is what makes a card, and what a view asks for.
type Recipe struct {
	ID      string // the card's name, chosen by whoever asks for it, such as "session/abc/status"
	Prompt  string // what the model is to make
	Profile string // the profile that makes it; "" for the router's pick
	Catalog int    // the version of the catalog the card is drawn from

	// Inputs is a digest of what the card reads, such as a session's turn
	// count: a card made from other inputs is behind, not wrong. "" when it
	// reads nothing that changes.
	Inputs string
	Every  time.Duration // the most a card may age; 0 when age alone never puts it out of date
	Gap    time.Duration // the least time between two cards, which bounds what one costs; 0 for none
}

// Hash versions what makes the card: its prompt, its profile and its
// catalog. A card made from another hash answers another question. Inputs,
// Every and Gap stay out of it: a change to them can put a card out of
// date, never make it wrong.
func (r Recipe) Hash() string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%q %q %d", r.Prompt, r.Profile, r.Catalog))
	return hex.EncodeToString(sum[:8])
}

// State is how a view draws a card.
type State string

const (
	Cold       State = "cold"       // nothing this recipe made is kept: a placeholder while it is made
	Fresh      State = "fresh"      // the kept card, up to date
	Stale      State = "stale"      // the kept card, out of date, with no remake running
	Refreshing State = "refreshing" // the kept card, while a remake makes the next one
)

// Card is what is kept for a recipe.
type Card struct {
	ID     string          `json:"id"`
	Spec   json.RawMessage `json:"spec,omitempty"`   // the card as the model made it; nil until one is made
	Recipe string          `json:"recipe,omitempty"` // the hash of the recipe Spec was made from
	Inputs string          `json:"inputs,omitempty"` // the inputs Spec was made from
	Made   time.Time       `json:"made,omitzero"`    // when Spec was made

	Err    string    `json:"err,omitempty"`   // why the last remake failed; "" when it did not
	Failed time.Time `json:"failed,omitzero"` // when it failed

	// Gen is the number of the newest remake begun, so that one finishing
	// after a newer one began is known to be outdated. Running is when it
	// began, zero once it finished, and For the hash of the recipe it works
	// from.
	Gen     int       `json:"gen,omitempty"`
	Running time.Time `json:"running,omitzero"`
	For     string    `json:"for,omitempty"`
}

// Remake is a remake that Want began: the caller makes the card from
// Recipe and gives what it made, or why it could not, to Finish.
type Remake struct {
	Recipe Recipe
	Gen    int
}

const (
	// claimTTL is how long a remake may run before it is taken for dead,
	// as when its model hung, so that another may begin.
	claimTTL = 5 * time.Minute
	// retryAfter is how long a failed remake waits before another begins.
	retryAfter = time.Minute
)

// Store keeps the cards in one file, mode 0600, so that a core that starts
// again draws them at once.
type Store struct {
	path  string
	mu    sync.Mutex
	cards map[string]*Card
	gen   int // the number of the last remake begun, in any card
}

// Open reads the cards kept at path, none when there is no file yet. The
// remakes the file lists as running are dropped: they ran in a core that
// has stopped, and nothing would finish them.
func Open(path string) (*Store, error) {
	s := &Store{path: path, cards: map[string]*Card{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var list []*Card
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, c := range list {
		// The file is indented to be read, and so are the specs in it.
		if c.Spec != nil {
			if c.Spec, err = compact(c.Spec); err != nil {
				return nil, fmt.Errorf("%s: %s: %w", path, c.ID, err)
			}
		}
		c.Running, c.For = time.Time{}, ""
		s.cards[c.ID] = c
		s.gen = max(s.gen, c.Gen)
	}
	return s, nil
}

// Want is a view's ask for the card a recipe makes. It gives what is kept,
// the state to draw it in and, when a remake is due and none runs, the
// remake the caller is to run. Want claims that remake, so that only one
// asker gets it. A cold card comes with no Spec, since what is kept, if
// anything, answers another recipe.
//
// An error is the file's alone: the card, its state and the remake stand,
// kept in memory, and the next change writes the file again.
func (s *Store) Want(r Recipe, now time.Time) (Card, State, *Remake, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, hash := s.cards[r.ID], r.Hash()
	st := stateOf(c, r, now)
	switch {
	case st == Fresh || st == Refreshing:
		return copyOf(c, r.ID, st), st, nil, nil
	case c != nil && running(c, hash, now):
		// The first card under way: cold, and nothing more to begin.
		return copyOf(c, r.ID, st), st, nil, nil
	case c != nil && !c.Failed.IsZero() && now.Sub(c.Failed) < retryAfter:
		return copyOf(c, r.ID, st), st, nil, nil
	}
	if c == nil {
		c = &Card{ID: r.ID}
		s.cards[r.ID] = c
	}
	s.gen++
	c.Gen, c.Running, c.For = s.gen, now, hash
	if st == Stale {
		st = Refreshing
	}
	err := s.save()
	return copyOf(c, r.ID, st), st, &Remake{Recipe: r, Gen: c.Gen}, err
}

// Finish keeps what a remake made, or why it could not make it, and gives
// the card as it now stands. A remake that is no longer the newest,
// because the recipe changed meanwhile, or its claim ran out and another
// began, or the card was forgotten, is outdated: what it made is dropped,
// and newest is false. As with Want, an error is the file's alone.
func (s *Store) Finish(rm Remake, spec json.RawMessage, failed error, now time.Time) (card Card, newest bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cards[rm.Recipe.ID]
	if c == nil || c.Gen != rm.Gen {
		return copyOf(c, rm.Recipe.ID, ""), false, nil
	}
	if failed == nil {
		if spec, err = compact(spec); err != nil {
			failed = errors.New("what the model made is not JSON")
		}
	}
	c.Running, c.For = time.Time{}, ""
	if failed != nil {
		c.Err, c.Failed = failed.Error(), now
	} else {
		c.Spec, c.Recipe, c.Inputs, c.Made = spec, rm.Recipe.Hash(), rm.Recipe.Inputs, now
		c.Err, c.Failed = "", time.Time{}
	}
	return copyOf(c, rm.Recipe.ID, ""), true, s.save()
}

// Forget drops the card kept under id, as when what it showed is gone. A
// remake of it that is still running finishes outdated.
func (s *Store) Forget(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cards[id]; !ok {
		return nil
	}
	delete(s.cards, id)
	return s.save()
}

// stateOf is how a view draws c, the card kept under r's name (nil when
// none is), when r is asked for at now.
func stateOf(c *Card, r Recipe, now time.Time) State {
	hash := r.Hash()
	switch {
	case c == nil || c.Spec == nil || c.Recipe != hash:
		return Cold
	case running(c, hash, now):
		return Refreshing
	case outOfDate(*c, r, now):
		return Stale
	}
	return Fresh
}

// outOfDate reports whether c, a card made from the recipe r asks for, is
// due to be made again at now: what it read has changed, or it has grown
// older than r.Every. Gap comes first, whatever the reason: it is the
// ceiling on what a card costs, so inputs that change every few seconds
// remake it once a Gap, and an Every shorter than the Gap waits for it too.
// A card held back is not lost: the next ask after the Gap remakes it. It
// is asked only while no remake runs.
func outOfDate(c Card, r Recipe, now time.Time) bool {
	age := now.Sub(c.Made)
	if r.Gap > 0 && age < r.Gap {
		return false
	}
	return c.Inputs != r.Inputs || r.Every > 0 && age >= r.Every
}

// running reports whether a remake of c from the recipe hashed hash is
// under way, and not yet taken for dead.
func running(c *Card, hash string, now time.Time) bool {
	return !c.Running.IsZero() && c.For == hash && now.Sub(c.Running) < claimTTL
}

// copyOf is c for a caller to keep, or an empty card named id when none is
// kept. A cold card's Spec stays behind: it is not one to draw.
func copyOf(c *Card, id string, st State) Card {
	if c == nil {
		return Card{ID: id}
	}
	out := *c
	out.Spec = slices.Clone(c.Spec)
	if st == Cold {
		out.Spec = nil
	}
	return out
}

// compact is spec without the spaces between its tokens, in a buffer of
// its own, or an error when it is not JSON.
func compact(spec json.RawMessage) (json.RawMessage, error) {
	var b bytes.Buffer
	if err := json.Compact(&b, spec); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// save writes every card, by name, in one transaction on the file.
func (s *Store) save() error {
	list := slices.SortedFunc(maps.Values(s.cards), func(a, b *Card) int { return strings.Compare(a.ID, b.ID) })
	if list == nil {
		list = []*Card{}
	}
	data, err := json.MarshalIndent(list, "", "\t")
	if err != nil {
		return err
	}
	return filetx.WriteFile(s.path, append(data, '\n'), 0o600)
}
