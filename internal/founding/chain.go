package founding

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/skills"
)

// The founding documents on a local chain (docs/proposals/founding-documents.md,
// local-chain.md). Each version is canonical JSON holding every text this
// build carries, word for word, with its SHA-512, and names the version
// before it, so the newest names the whole history. A version is added only
// when a text changes, so the chain is the texts' history on this
// computer. Only public standards shape it (internal/chain): any tool with
// a JSON canonicalizer and SHA-512 can check it without Irie.

// Format names a founding version's form, as the product names it.
const Format = brand.Slug + ".founding/1"

// Version is a founding version's document, which its canonical bytes and
// so its name come from.
type Version struct {
	Created   string     `json:"created"` // RFC 3339 in UTC, to the second
	Documents []Recorded `json:"documents"`
	Format    string     `json:"format"`
	Parent    string     `json:"parent,omitempty"`
}

// Recorded is one text as a version holds it.
type Recorded struct {
	File string `json:"file"`
	ID   string `json:"id"`
	// SHA512 is the text's bytes' digest, as the chain names: "sha512:"
	// and 128 hex digits.
	SHA512 string `json:"sha512"`
	Text   string `json:"text"`
	Title  string `json:"title"`
}

// Text is one document's file as this build carries it.
type Text struct {
	ID, Title, File string
	Bytes           []byte
}

// Texts are the documents' files as this build carries them, in order.
func Texts() ([]Text, error) {
	return texts(skills.Bundled())
}

func texts(all []skills.Skill) ([]Text, error) {
	var files map[string][]byte
	for _, s := range all {
		if s.Name == Skill {
			files = s.Files
		}
	}
	if files == nil {
		return nil, fmt.Errorf("this build has no %s skill", Skill)
	}
	var out []Text
	for _, f := range Files {
		b, ok := files[f.File]
		if !ok {
			return nil, fmt.Errorf("the %s skill has no %s", Skill, f.File)
		}
		d, err := Parse(f.ID, b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.File, err)
		}
		out = append(out, Text{ID: f.ID, Title: d.Title, File: f.File, Bytes: b})
	}
	return out, nil
}

// Chain is the founding documents' chain, kept in its folder.
type Chain struct {
	dir   string
	store chain.Store
}

// OpenChain is the chain kept in dir, which its first version makes.
func OpenChain(dir string) Chain { return Chain{dir: dir, store: chain.Store{Dir: dir}} }

// Record makes ts the newest version, unless the newest holds them
// already: the same files, in the same order, with the same SHA-512s. It
// gives the newest version's name, and says whether it made one. One
// process writes a chain: the core, which serializes its calls.
func (c Chain) Record(ts []Text, now time.Time) (string, bool, error) {
	head, err := c.store.Head()
	if err != nil {
		return "", false, err
	}
	doc := Version{Created: now.UTC().Format(time.RFC3339), Format: Format, Parent: head}
	for _, t := range ts {
		doc.Documents = append(doc.Documents, Recorded{File: t.File, ID: t.ID, SHA512: chain.Name(t.Bytes), Text: string(t.Bytes), Title: t.Title})
	}
	if head != "" {
		v, err := c.get(head)
		if err != nil {
			return "", false, err
		}
		if same(v.Documents, doc.Documents) {
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

// same says whether two versions hold the same texts: the same files and
// fingerprints, in the same order.
func same(a, b []Recorded) bool {
	return slices.EqualFunc(a, b, func(x, y Recorded) bool {
		return x.File == y.File && x.ID == y.ID && x.SHA512 == y.SHA512
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
	if v.Format != Format {
		return Version{}, fmt.Errorf("version %s is of the form %q, not %s", chain.Short(name), v.Format, Format)
	}
	return v, nil
}

// Check reads back every version, newest first: the store checks each
// against its name, and this each text against its SHA-512. Any
// mismatch, a missing version or a version of another form fails it.
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
		}
		for _, d := range v.Documents {
			if chain.Name([]byte(d.Text)) != d.SHA512 {
				st.Problem = fmt.Sprintf("version %s: the text of %s doesn't match its SHA-512", chain.Short(name), d.File)
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
		fv := ipc.FoundingVersion{Name: name, Created: v.Created, Parent: v.Parent}
		for _, d := range v.Documents {
			fv.Files = append(fv.Files, d.File+" "+d.SHA512)
		}
		out = append(out, fv)
		name = v.Parent
	}
	return out, nil
}
