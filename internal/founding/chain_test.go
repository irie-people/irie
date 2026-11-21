package founding

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/chain"
)

// TestTheChain: the first look puts every text and link on the chain; the
// same again make no version; a changed text makes one that names the
// version before it, and so does a changed link; the check reads back
// every version, the log lists them, and the export gives their bytes,
// oldest first.
func TestTheChain(t *testing.T) {
	c := OpenChain(t.TempDir())
	if st := c.Check(); st.Verified || st.Problem != "Nothing is on the chain yet." {
		t.Fatalf("empty chain: %+v", st)
	}
	ts, err := Texts()
	if err != nil || len(ts) != len(Documents) {
		t.Fatalf("texts: %d, %v", len(ts), err)
	}
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	first, made, err := c.Record(ts, Links, t0)
	if err != nil || !made || !chain.Valid(first) {
		t.Fatalf("first: %q, %v, %v", first, made, err)
	}
	if again, made, err := c.Record(ts, Links, t0.Add(time.Hour)); err != nil || made || again != first {
		t.Fatalf("the same texts again: %q, %v, %v", again, made, err)
	}
	changed := slices.Clone(ts)
	changed[0].Bytes = append(slices.Clone(ts[0].Bytes), "\nA change.\n"...)
	second, made, err := c.Record(changed, Links, t0.Add(2*time.Hour))
	if err != nil || !made || second == first {
		t.Fatalf("a changed text: %q, %v, %v", second, made, err)
	}
	links := slices.Clone(Links)
	links[0].Why += " And more."
	third, made, err := c.Record(changed, links, t0.Add(3*time.Hour))
	if err != nil || !made || third == second {
		t.Fatalf("a changed link: %q, %v, %v", third, made, err)
	}
	st := c.Check()
	if !st.Verified || st.Versions != 3 || st.Head != third || st.Created != "2026-11-21T15:00:00Z" || st.Texts != len(Documents) || st.Links != len(Links) || st.Problem != "" {
		t.Errorf("check: %+v", st)
	}
	log, err := c.Log(0)
	if err != nil || len(log) != 3 || log[0].Name != third || log[0].Parent != second || log[1].Parent != first || log[2].Parent != "" {
		t.Fatalf("log: %+v, %v", log, err)
	}
	for _, v := range log {
		if len(v.Files) != len(Documents) || !strings.HasPrefix(v.Files[0], Documents[0].File+" sha512:") || v.Format != Format || v.Links != len(Links) {
			t.Errorf("%s: files %v, form %s, %d links", chain.Short(v.Name), v.Files, v.Format, v.Links)
		}
	}
	if one, err := c.Log(1); err != nil || len(one) != 1 || one[0].Name != third {
		t.Errorf("log of one: %+v, %v", one, err)
	}
	ex, err := c.Export()
	if err != nil || len(ex) != 3 || chain.Name([]byte(ex[0])) != first || chain.Name([]byte(ex[1])) != second || chain.Name([]byte(ex[2])) != third {
		t.Errorf("export: %d versions, %v", len(ex), err)
	}
	for _, line := range ex {
		if strings.Contains(line, "\n") {
			t.Error("a version's bytes hold a line break")
		}
	}
}

// TestAVersionIsCanonical: a version is canonical JSON named by its
// SHA-512, holding each text whole with its date and kind, and the links,
// so a tool with a JSON canonicalizer and SHA-512 can check it without
// Irie.
func TestAVersionIsCanonical(t *testing.T) {
	dir := t.TempDir()
	ts, _ := Texts()
	name, _, err := OpenChain(dir).Record(ts, Links, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "objects", strings.TrimPrefix(name, chain.Prefix)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	again, err := chain.Canonical(v)
	if err != nil || !bytes.Equal(again, b) || chain.Name(b) != name {
		t.Errorf("the version isn't canonical, or its name isn't its bytes' SHA-512: %v", err)
	}
	var doc Version
	_ = json.Unmarshal(b, &doc)
	if doc.Format != Format || !slices.Equal(doc.Links, Links) {
		t.Errorf("form %q, %d links", doc.Format, len(doc.Links))
	}
	for i, d := range doc.Documents {
		if d.Text != string(ts[i].Bytes) || d.SHA512 != chain.Name(ts[i].Bytes) || d.When != ts[i].When || d.Kind != ts[i].Kind || d.Title != ts[i].Title {
			t.Errorf("%s isn't held whole: %+v", d.File, Recorded{File: d.File, ID: d.ID, Kind: d.Kind, SHA512: d.SHA512, Title: d.Title, When: d.When})
		}
	}
}

// TestTheChainCatchesTampering: a version changed on disk after it was
// written fails the check; so does a text that doesn't match its own
// SHA-512, even in a version whose name matches its bytes; and so does a
// link that names a text the version doesn't hold.
func TestTheChainCatchesTampering(t *testing.T) {
	dir := t.TempDir()
	c := OpenChain(dir)
	ts, _ := Texts()
	name, _, err := c.Record(ts, Links, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "objects", strings.TrimPrefix(name, chain.Prefix)+".json")
	b, _ := os.ReadFile(path)
	if err := os.WriteFile(path, bytes.Replace(b, []byte("In Congress"), []byte("In Kongress"), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := c.Check(); st.Verified || !strings.Contains(st.Problem, "changed after it was written") {
		t.Errorf("a version changed by hand: %+v", st)
	}

	put := func(t *testing.T, doc Version) Chain {
		dir := t.TempDir()
		b, err := chain.Canonical(doc)
		if err != nil {
			t.Fatal(err)
		}
		s := chain.Store{Dir: dir}
		n, err := s.Put(b)
		if err != nil || s.SetHead(n) != nil {
			t.Fatal(err)
		}
		return OpenChain(dir)
	}
	wrong := put(t, Version{Created: "2026-11-21T12:00:00Z", Format: Format,
		Documents: []Recorded{{File: "declaration.md", ID: "declaration", SHA512: chain.Name([]byte("one")), Text: "two", Title: "A title"}}})
	if st := wrong.Check(); st.Verified || !strings.Contains(st.Problem, "doesn't match its SHA-512") {
		t.Errorf("a text that isn't its fingerprint's: %+v", st)
	}
	loose := put(t, Version{Created: "2026-11-21T12:00:00Z", Format: Format,
		Documents: []Recorded{{File: "declaration.md", ID: "declaration", SHA512: chain.Name([]byte("one")), Text: "one", Title: "A title"}},
		Links:     []Link{{From: "declaration", To: "nowhere", How: DrewOn, Why: "It doesn't."}}})
	if st := loose.Check(); st.Verified || !strings.Contains(st.Problem, "names a text it doesn't hold") {
		t.Errorf("a link to a text the version lacks: %+v", st)
	}
	other := put(t, Version{Created: "2026-11-21T12:00:00Z", Format: "someone-else/1",
		Documents: []Recorded{{File: "declaration.md", ID: "declaration", SHA512: chain.Name([]byte("one")), Text: "one", Title: "A title"}}})
	if st := other.Check(); st.Verified || !strings.Contains(st.Problem, "is of the form") {
		t.Errorf("a version of another form: %+v", st)
	}
}

// TestTheFirstBuildsChainGoesOn: the chain the first build kept, at
// chain/founding in its first form, moves into the charters' folder once,
// reads whole, and the next version names its newest, so the history stays
// one; an old folder beside a chain that has versions is left be.
func TestTheFirstBuildsChainGoesOn(t *testing.T) {
	top := t.TempDir()
	old := filepath.Join(top, "founding")
	ts, err := Texts()
	if err != nil {
		t.Fatal(err)
	}
	first := Version{Created: "2026-11-21T16:35:39Z", Format: FormatFirst}
	for _, id := range []string{"declaration", "constitution", "rights", "foia"} {
		i := slices.IndexFunc(ts, func(x Text) bool { return x.ID == id })
		first.Documents = append(first.Documents, Recorded{File: ts[i].File, ID: id, SHA512: chain.Name(ts[i].Bytes), Text: string(ts[i].Bytes), Title: ts[i].Title})
	}
	b, err := chain.Canonical(first)
	if err != nil {
		t.Fatal(err)
	}
	s := chain.Store{Dir: old}
	name, err := s.Put(b)
	if err != nil || s.SetHead(name) != nil {
		t.Fatal(err)
	}
	// Only the keys the first build wrote: no dates, kinds or links.
	if strings.Contains(string(b), `"when"`) || strings.Contains(string(b), `"kind"`) || strings.Contains(string(b), `"links"`) {
		t.Errorf("the first form carries new keys: %.200s", b)
	}

	c := OpenChain(filepath.Join(top, "charters"))
	if err := c.Adopt(old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the old folder stayed: %v", err)
	}
	if st := c.Check(); !st.Verified || st.Versions != 1 || st.Head != name || st.Texts != 4 || st.Links != 0 {
		t.Fatalf("the first build's chain, moved in: %+v", st)
	}
	second, made, err := c.Record(ts, Links, time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC))
	if err != nil || !made {
		t.Fatalf("the next version: %v, %v", made, err)
	}
	st := c.Check()
	if !st.Verified || st.Versions != 2 || st.Head != second || st.Texts != len(Documents) || st.Links != len(Links) {
		t.Errorf("both: %+v", st)
	}
	log, err := c.Log(0)
	if err != nil || len(log) != 2 || log[0].Parent != name || log[0].Format != Format || log[1].Format != FormatFirst || log[1].Links != 0 || len(log[1].Files) != 4 {
		t.Errorf("log: %+v, %v", log, err)
	}
	if ex, err := c.Export(); err != nil || len(ex) != 2 || ex[0] != string(b) || chain.Name([]byte(ex[1])) != second {
		t.Errorf("export: %d, %v", len(ex), err)
	}

	// Another old folder, beside a chain with versions, stays where it is.
	s2 := chain.Store{Dir: old}
	n2, err := s2.Put(b)
	if err != nil || s2.SetHead(n2) != nil {
		t.Fatal(err)
	}
	if err := c.Adopt(old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(old, "HEAD")); err != nil {
		t.Errorf("the second old folder moved: %v", err)
	}
	if st := c.Check(); !st.Verified || st.Versions != 2 || st.Head != second {
		t.Errorf("after the second adopt: %+v", st)
	}
}
