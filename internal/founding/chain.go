package founding

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/skills"
)

// The charters on a local chain (docs/proposals/founding-documents.md,
// local-chain.md). Each version is canonical JSON holding every text this
// build carries, word for word, with its SHA-512, its date and its kind,
// and the links that say how the texts bear on one another, and it names
// the version before it, so the newest names the whole history. Together
// the texts and the links are a graph: the texts its nodes, the links its
// edges, each pointing from a later text back to the one it grew from. A
// version is added only when a text or a link changes, so the chain is the
// texts' history on this computer. Only public standards shape it
// (internal/chain): any tool with a JSON canonicalizer and SHA-512 can
// check it without Irie.

// Format names a version's form, as the product names it: the chain's
// second form, which carries each text's date and kind and the links.
const Format = brand.Slug + ".charters/2"

// FormatFirst is the first build's form: the four first texts, with no
// dates, kinds or links. A chain begun then is read whole and goes on under
// Format, so its history stays one.
const FormatFirst = brand.Slug + ".founding/1"

// Version is a version's document, which its canonical bytes and so its
// name come from.
type Version struct {
	Created   string     `json:"created"` // RFC 3339 in UTC, to the second
	Documents []Recorded `json:"documents"`
	Format    string     `json:"format"`
	// Links are the graph's edges, each from a later text to an earlier.
	Links  []Link `json:"links,omitempty"`
	Parent string `json:"parent,omitempty"`
}

// Recorded is one text as a version holds it.
type Recorded struct {
	File string `json:"file"`
	ID   string `json:"id"`
	Kind string `json:"kind,omitempty"`
	// SHA512 is the text's bytes' digest, as the chain names: "sha512:"
	// and 128 hex digits.
	SHA512 string `json:"sha512"`
	Text   string `json:"text"`
	Title  string `json:"title"`
	When   string `json:"when,omitempty"`
}

// Text is one document's file as this build carries it.
type Text struct {
	Document
	Title string
	Bytes []byte
}

// Texts are the documents' files as this build carries them, in order.
func Texts() ([]Text, error) {
	return texts(skills.Bundled())
}

func texts(all []skills.Skill) ([]Text, error) {
	fs, err := files(all)
	if err != nil {
		return nil, err
	}
	var out []Text
	for _, f := range Documents {
		b, ok := fs[f.File]
		if !ok {
			return nil, fmt.Errorf("the %s skill has no %s", Skill, f.File)
		}
		d, err := Parse(f.ID, b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.File, err)
		}
		out = append(out, Text{Document: f, Title: d.Title, Bytes: b})
	}
	return out, nil
}

// Chain is the charters' chain, kept in its folder.
type Chain struct {
	dir   string
	store chain.Store
}

// OpenChain is the chain kept in dir, which its first version makes.
func OpenChain(dir string) Chain { return Chain{dir: dir, store: chain.Store{Dir: dir}} }

// Adopt moves a chain kept in old, where the first build kept it, into
// this chain's folder, once: when this folder has no newest version yet
// and old has one. Its versions keep their form, and the next version
// names the newest of them, so the history stays one. A folder of this
// chain's that is empty gives way to it.
func (c Chain) Adopt(old string) error {
	if _, err := os.Lstat(filepath.Join(c.dir, "HEAD")); err == nil {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(old, "HEAD")); err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.dir), 0o700); err != nil {
		return err
	}
	if err := os.Remove(c.dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s is in the way of the chain kept in %s: %w", c.dir, old, err)
	}
	return os.Rename(old, c.dir)
}

// Record makes ts and links the newest version, unless the newest holds
// them already: the same files, in the same order, with the same SHA-512s,
// dates and kinds, and the same links. It gives the newest version's name,
// and says whether it made one. One process writes a chain: the core,
// which serializes its calls.
func (c Chain) Record(ts []Text, links []Link, now time.Time) (string, bool, error) {
	head, err := c.store.Head()
	if err != nil {
		return "", false, err
	}
	doc := Version{Created: now.UTC().Format(time.RFC3339), Format: Format, Links: links, Parent: head}
	for _, t := range ts {
		doc.Documents = append(doc.Documents, Recorded{File: t.File, ID: t.ID, Kind: t.Kind, SHA512: chain.Name(t.Bytes), Text: string(t.Bytes), Title: t.Title, When: t.When})
	}
	if head != "" {
		v, err := c.get(head)
		if err != nil {
			return "", false, err
		}
		if v.Format == Format && same(v.Documents, doc.Documents) && slices.Equal(v.Links, doc.Links) {
			return head, false, nil
		}
	}
	b, err := chain.Canonical(doc)
	if err != nil {
		return "", false, err
	}
	name, err := c.store.Put(b)
	if err != nil {
		return "", false, err
	}
	return name, true, c.store.SetHead(name)
}

// same says whether two versions hold the same texts: the same files, in
// the same order, each with the same fingerprint, date and kind.
func same(a, b []Recorded) bool {
	return slices.EqualFunc(a, b, func(x, y Recorded) bool {
		return x.File == y.File && x.ID == y.ID && x.SHA512 == y.SHA512 && x.When == y.When && x.Kind == y.Kind && x.Title == y.Title
	})
}

// get reads the version named: the store checks its bytes against its
// name, and this its form.
func (c Chain) get(name string) (Version, error) {
	b, err := c.store.Get(name)
	if err != nil {
		return Version{}, err
	}
	var v Version
	if err := json.Unmarshal(b, &v); err != nil {
		return Version{}, fmt.Errorf("version %s: %w", chain.Short(name), err)
	}
	if v.Format != Format && v.Format != FormatFirst {
		return Version{}, fmt.Errorf("version %s is of the form %q, not %s", chain.Short(name), v.Format, Format)
	}
	return v, nil
}

// Check reads back every version, newest first: the store checks each
// against its name, and this each text against its SHA-512 and each link
// against the texts the version holds. Any mismatch, a missing version or
// a version of another form fails it.
func (c Chain) Check() ipc.FoundingChain {
	st := ipc.FoundingChain{Folder: c.dir}
	head, err := c.store.Head()
	if err != nil {
		st.Problem = err.Error()
		return st
	}
	if head == "" {
		st.Problem = "Nothing is on the chain yet."
		return st
	}
	st.Head = head
	for name := head; name != ""; st.Versions++ {
		if st.Versions > 1_000_000 {
			st.Problem = "The chain comes round on itself."
			return st
		}
		v, err := c.get(name)
		if err != nil {
			st.Problem = err.Error()
			return st
		}
		if name == head {
			st.Created = v.Created
			st.Texts, st.Links = len(v.Documents), len(v.Links)
		}
		ids := map[string]bool{}
		for _, d := range v.Documents {
			if chain.Name([]byte(d.Text)) != d.SHA512 {
				st.Problem = fmt.Sprintf("version %s: the text of %s doesn't match its SHA-512", chain.Short(name), d.File)
				return st
			}
			ids[d.ID] = true
		}
		for _, l := range v.Links {
			if !ids[l.From] || !ids[l.To] {
				st.Problem = fmt.Sprintf("version %s: the link from %s to %s names a text it doesn't hold", chain.Short(name), l.From, l.To)
				return st
			}
		}
		name = v.Parent
	}
	st.Verified = true
	return st
}

// Log is up to n versions, newest first, all of them when n is 0 or less.
func (c Chain) Log(n int) ([]ipc.FoundingVersion, error) {
	head, err := c.store.Head()
	if err != nil || head == "" {
		return nil, err
	}
	var out []ipc.FoundingVersion
	for name := head; name != "" && (n <= 0 || len(out) < n); {
		v, err := c.get(name)
		if err != nil {
			return out, err
		}
		fv := ipc.FoundingVersion{Name: name, Created: v.Created, Parent: v.Parent, Format: v.Format, Links: len(v.Links)}
		for _, d := range v.Documents {
			fv.Files = append(fv.Files, d.File+" "+d.SHA512)
		}
		out = append(out, fv)
		name = v.Parent
	}
	return out, nil
}

// Export is every version's bytes, oldest first, for another tool to read
// the chain whole, as a graph or as history: each is canonical JSON with
// no line break in it, so one a line carries them, and `shasum -a 512`
// of a line is its version's name.
func (c Chain) Export() ([]string, error) {
	head, err := c.store.Head()
	if err != nil || head == "" {
		return nil, err
	}
	var out []string
	for name := head; name != ""; {
		b, err := c.store.Get(name)
		if err != nil {
			return nil, err
		}
		out = append(out, string(b))
		if name, err = chain.Parent(b); err != nil {
			return nil, err
		}
	}
	slices.Reverse(out)
	return out, nil
}
