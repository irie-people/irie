// Package zen is your zen (docs/proposals/zen.md): the settings of zen
// mode, a calm place where the person comes before the work, kept as a
// chain of versions (internal/chain); the path, which teaches one small
// thing at a time; and the rules for when zen may offer a step or a rest.
//
// It records no moods, feelings or health, and nothing in it leaves the
// computer.
package zen

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/ipc"
)

// Format names the version document's form, version 1: "irie-zen/1",
// under the product's own name (internal/brand).
const Format = brand.Slug + "-zen/1"

// Layouts are zen's layouts, in the order the apps list them.
var Layouts = []string{"centred", "edge", "watch"}

// Widths are the reading columns Centred offers, in columns of text.
var Widths = []int{80, 100, 120}

// The bounds of zen's own text size, in points, and of the minutes before a
// rest.
const (
	MinText   = 9
	MaxText   = 28
	MinBreaks = 15
	MaxBreaks = 180
)

// Default is the zen a person starts with: Centred, 100 columns, the
// window's text, full screen, a gentle agent and a word at night. Rests
// start off: zen asks before it ever interrupts, and its path offers them
// early.
func Default() ipc.ZenProfile {
	return ipc.ZenProfile{Layout: "centred", Columns: 100, FullScreen: true, Gentle: true, Night: true}
}

// Check says what is wrong with p, if anything.
func Check(p ipc.ZenProfile) error {
	switch {
	case !slices.Contains(Layouts, p.Layout):
		return fmt.Errorf("layout %q: must be one of %s", p.Layout, strings.Join(Layouts, ", "))
	case !slices.Contains(Widths, p.Columns):
		return fmt.Errorf("columns %d: must be 80, 100 or 120", p.Columns)
	case p.TextSize != 0 && (p.TextSize < MinText || p.TextSize > MaxText):
		return fmt.Errorf("text_size %d: must be 0, for the window's, or %d to %d points", p.TextSize, MinText, MaxText)
	case p.Breaks != 0 && (p.Breaks < MinBreaks || p.Breaks > MaxBreaks):
		return fmt.Errorf("breaks %d: must be 0, for none, or %d to %d minutes", p.Breaks, MinBreaks, MaxBreaks)
	}
	return nil
}

// Apply is p with ch's keys set.
func Apply(p ipc.ZenProfile, ch ipc.ZenChange) ipc.ZenProfile {
	if ch.Layout != nil {
		p.Layout = *ch.Layout
	}
	if ch.Columns != nil {
		p.Columns = *ch.Columns
	}
	if ch.TextSize != nil {
		p.TextSize = *ch.TextSize
	}
	if ch.FullScreen != nil {
		p.FullScreen = *ch.FullScreen
	}
	if ch.Gentle != nil {
		p.Gentle = *ch.Gentle
	}
	if ch.Breaks != nil {
		p.Breaks = *ch.Breaks
	}
	if ch.Night != nil {
		p.Night = *ch.Night
	}
	return p
}

// LayoutName is a layout as the apps name it.
func LayoutName(l string) string {
	switch l {
	case "centred":
		return "Centred"
	case "edge":
		return "Edge to Edge"
	case "watch":
		return "Keep Watch"
	}
	return l
}

// Why says in plain words how b differs from a, as a version's why:
// "Edge to Edge, text at 16 pt".
func Why(a, b ipc.ZenProfile) string {
	var w []string
	if a.Layout != b.Layout {
		w = append(w, LayoutName(b.Layout))
	}
	if a.Columns != b.Columns {
		w = append(w, fmt.Sprintf("a column %d wide", b.Columns))
	}
	if a.TextSize != b.TextSize {
		if b.TextSize == 0 {
			w = append(w, "the window's text size")
		} else {
			w = append(w, fmt.Sprintf("text at %d pt", b.TextSize))
		}
	}
	if a.FullScreen != b.FullScreen {
		w = append(w, pick(b.FullScreen, "full screen", "in the window"))
	}
	if a.Gentle != b.Gentle {
		w = append(w, pick(b.Gentle, "a gentle agent", "the agent as usual"))
	}
	if a.Breaks != b.Breaks {
		if b.Breaks == 0 {
			w = append(w, "no rests")
		} else {
			w = append(w, fmt.Sprintf("a rest every %d minutes", b.Breaks))
		}
	}
	if a.Night != b.Night {
		w = append(w, pick(b.Night, "a word at night", "no word at night"))
	}
	return capital(strings.Join(w, ", "))
}

func pick(on bool, yes, no string) string {
	if on {
		return yes
	}
	return no
}

func capital(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}

// CleanWhy is a why as a version keeps it: one line, with no controls, at
// most ipc.MaxZenWhy characters.
func CleanWhy(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(s); len(r) > ipc.MaxZenWhy {
		s = strings.TrimSpace(string(r[:ipc.MaxZenWhy-1])) + "…"
	}
	return s
}

// Version is a version's document (zen.md §4), which its canonical bytes
// and so its name come from.
type Version struct {
	Created string         `json:"created"` // RFC 3339 in UTC, to the second
	Format  string         `json:"format"`
	Parent  string         `json:"parent,omitempty"`
	Why     string         `json:"why"`
	Zen     ipc.ZenProfile `json:"zen"`
}

// State names p alone, apart from any version: every version with p's
// settings shares it.
func State(p ipc.ZenProfile) string {
	b, err := chain.Canonical(p)
	if err != nil {
		// A profile holds only small whole numbers, text and switches.
		panic(err)
	}
	return chain.Name(b)
}

// Book is your zen's versions on disk.
type Book struct {
	store chain.Store
}

// Open is the book kept in dir, which the first version makes, with its
// vault shut: it reads versions kept plain, as before the vault, and
// writes only where there is no vault (OpenVault).
func Open(dir string) Book { return Book{store: chain.Store{Dir: dir}} }

// OpenVault is the book kept in dir with its vault open under kek, the
// key the system keeps for it (chain.OpenVault): it seals each version it
// keeps, and opens each it reads. Seal moves in the versions kept before
// the vault.
func OpenVault(dir string, kek []byte) (Book, error) {
	v, err := chain.OpenVault(dir, kek)
	if err != nil {
		return Book{}, err
	}
	return Book{store: chain.Store{Dir: dir, Vault: v}}, nil
}

// Sealed says whether the book has its vault open.
func (k Book) Sealed() bool { return k.store.Vault != nil }

// Seal moves the versions kept plain, before the vault, into it, and says
// how many it moved.
func (k Book) Seal() (int, error) { return k.store.Seal() }

// Export is every version's document, oldest first, as the exact
// canonical bytes its name is the SHA-512 of (zen.md §9): what another
// tool, such as Mycylic, reads to keep your zen's history, checking each
// name as it goes.
func (k Book) Export() ([]string, error) {
	head, err := k.store.Head()
	if err != nil || head == "" {
		return nil, err
	}
	entries, err := k.store.Log(head, 0)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[len(entries)-1-i] = string(e.Bytes)
	}
	return out, nil
}

// Head is the newest version, and false before the first.
func (k Book) Head() (ipc.ZenVersion, bool, error) {
	name, err := k.store.Head()
	if err != nil || name == "" {
		return ipc.ZenVersion{}, false, err
	}
	v, err := k.Get(name)
	return v, err == nil, err
}

// Get is the version named.
func (k Book) Get(name string) (ipc.ZenVersion, error) {
	b, err := k.store.Get(name)
	if err != nil {
		return ipc.ZenVersion{}, err
	}
	return decode(name, b)
}

// Find is the version whose name starts with prefix.
func (k Book) Find(prefix string) (ipc.ZenVersion, error) {
	name, err := k.store.Find(prefix)
	if err != nil {
		return ipc.ZenVersion{}, err
	}
	return k.Get(name)
}

// Commit makes p your zen as a new version after the newest, saying why,
// and is false, making none, when p is the newest's already.
func (k Book) Commit(p ipc.ZenProfile, why string, now time.Time) (ipc.ZenVersion, bool, error) {
	if err := Check(p); err != nil {
		return ipc.ZenVersion{}, false, err
	}
	head, ok, err := k.Head()
	if err != nil {
		return ipc.ZenVersion{}, false, err
	}
	if ok && head.Zen == p {
		return head, false, nil
	}
	doc := Version{Created: now.UTC().Format(time.RFC3339), Format: Format, Why: CleanWhy(why), Zen: p}
	if ok {
		doc.Parent = head.Name
	}
	b, err := chain.Canonical(doc)
	if err != nil {
		return ipc.ZenVersion{}, false, err
	}
	name, err := k.store.Put(b)
	if err != nil {
		return ipc.ZenVersion{}, false, err
	}
	if err := k.store.SetHead(name); err != nil {
		return ipc.ZenVersion{}, false, err
	}
	v, err := decode(name, b)
	return v, err == nil, err
}

// Log is up to n versions, newest first: all of them when n is 0 or less.
// A version that can't be read ends the list, with the error after the
// versions before it.
func (k Book) Log(n int) ([]ipc.ZenVersion, error) {
	head, err := k.store.Head()
	if err != nil || head == "" {
		return nil, err
	}
	entries, err := k.store.Log(head, n)
	out := make([]ipc.ZenVersion, 0, len(entries))
	for _, e := range entries {
		v, derr := decode(e.Name, e.Bytes)
		if derr != nil {
			return out, derr
		}
		out = append(out, v)
	}
	return out, err
}

// decode reads a version's document, and refuses one of another form or
// with settings zen would not take.
func decode(name string, b []byte) (ipc.ZenVersion, error) {
	var doc Version
	if err := json.Unmarshal(b, &doc); err != nil {
		return ipc.ZenVersion{}, fmt.Errorf("zen: version %s: %w", chain.Short(name), err)
	}
	if doc.Format != Format {
		return ipc.ZenVersion{}, fmt.Errorf("zen: version %s is %q, not %s", chain.Short(name), doc.Format, Format)
	}
	created, err := time.Parse(time.RFC3339, doc.Created)
	if err != nil {
		return ipc.ZenVersion{}, fmt.Errorf("zen: version %s: %w", chain.Short(name), err)
	}
	if err := Check(doc.Zen); err != nil {
		return ipc.ZenVersion{}, fmt.Errorf("zen: version %s: %w", chain.Short(name), err)
	}
	return ipc.ZenVersion{Name: name, Parent: doc.Parent, Created: created, Why: doc.Why, State: State(doc.Zen), Zen: doc.Zen}, nil
}

// ErrNone says that no version has the name asked for.
var ErrNone = chain.ErrNone

// IsNone says whether err is that no version was found.
func IsNone(err error) bool { return errors.Is(err, chain.ErrNone) }

// IsLocked says whether err is that the vault isn't open.
func IsLocked(err error) bool { return errors.Is(err, chain.ErrLocked) }

// IsWrongKey says whether err is that the key given doesn't open the
// vault.
func IsWrongKey(err error) bool { return errors.Is(err, chain.ErrWrongKey) }
