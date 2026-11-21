package founding

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/chain"
)

// TestTheChain: the first look puts every text on the chain; the same
// texts again make no version; a changed text makes one that names the
// version before it; the check reads back both, and the log lists them.
func TestTheChain(t *testing.T) {
	c := OpenChain(t.TempDir())
	if st := c.Check(); st.Verified || st.Problem != "Nothing is on the chain yet." {
		t.Fatalf("empty chain: %+v", st)
	}
	ts, err := Texts()
	if err != nil || len(ts) != len(Files) {
		t.Fatalf("texts: %d, %v", len(ts), err)
	}
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	first, made, err := c.Record(ts, t0)
	if err != nil || !made || !chain.Valid(first) {
		t.Fatalf("first: %q, %v, %v", first, made, err)
	}
	if again, made, err := c.Record(ts, t0.Add(time.Hour)); err != nil || made || again != first {
		t.Fatalf("the same texts again: %q, %v, %v", again, made, err)
	}
	changed := slices.Clone(ts)
	changed[0].Bytes = append(slices.Clone(ts[0].Bytes), "\nA change.\n"...)
	second, made, err := c.Record(changed, t0.Add(2*time.Hour))
	if err != nil || !made || second == first {
		t.Fatalf("a changed text: %q, %v, %v", second, made, err)
	}
	st := c.Check()
	if !st.Verified || st.Versions != 2 || st.Head != second || st.Created != "2026-11-21T14:00:00Z" || st.Problem != "" {
		t.Errorf("check: %+v", st)
	}
	log, err := c.Log(0)
	if err != nil || len(log) != 2 || log[0].Name != second || log[0].Parent != first || log[1].Parent != "" {
		t.Fatalf("log: %+v, %v", log, err)
	}
	for _, v := range log {
		if len(v.Files) != len(Files) || !strings.HasPrefix(v.Files[0], "declaration.md sha512:") {
			t.Errorf("%s: files %v", chain.Short(v.Name), v.Files)
		}
	}
	if one, err := c.Log(1); err != nil || len(one) != 1 || one[0].Name != second {
		t.Errorf("log of one: %+v, %v", one, err)
	}
}

// TestAVersionIsCanonical: a version is canonical JSON named by its
// SHA-512, holding each text whole, so a tool with a JSON canonicalizer and
// SHA-512 can check it without Irie.
func TestAVersionIsCanonical(t *testing.T) {
	dir := t.TempDir()
	ts, _ := Texts()
	name, _, err := OpenChain(dir).Record(ts, time.Now())
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
	for i, d := range doc.Documents {
		if d.Text != string(ts[i].Bytes) || d.SHA512 != chain.Name(ts[i].Bytes) {
			t.Errorf("%s isn't held whole", d.File)
		}
	}
}

// TestTheChainCatchesTampering: a version changed on disk after it was
// written fails the check, and so does a text that doesn't match its own
// SHA-512, even in a version whose name matches its bytes.
func TestTheChainCatchesTampering(t *testing.T) {
	dir := t.TempDir()
	c := OpenChain(dir)
	ts, _ := Texts()
	name, _, err := c.Record(ts, time.Now())
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

	dir2 := t.TempDir()
	doc := Version{Created: "2026-11-21T12:00:00Z", Format: Format,
		Documents: []Recorded{{File: "declaration.md", ID: "declaration", SHA512: chain.Name([]byte("one")), Text: "two", Title: "A title"}}}
	b2, err := chain.Canonical(doc)
	if err != nil {
		t.Fatal(err)
	}
	s := chain.Store{Dir: dir2}
	n2, err := s.Put(b2)
	if err != nil || s.SetHead(n2) != nil {
		t.Fatal(err)
	}
	if st := OpenChain(dir2).Check(); st.Verified || !strings.Contains(st.Problem, "doesn't match its SHA-512") {
		t.Errorf("a text that isn't its fingerprint's: %+v", st)
	}
}
