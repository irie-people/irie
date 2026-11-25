//go:build !nofounding

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/founding"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/schools"
)

// TestFoundingOpWaitsForItsPlugin: while the founding plugin is off, the op
// says which switch turns it on; on, it gives the texts, the links
// between them, their local chain, checked, and a search's passages, to
// anyone, an agent too.
func TestFoundingOpWaitsForItsPlugin(t *testing.T) {
	cfg := config.Default()
	c := bareCore(t, cfg)
	if _, err := c.foundingOp(0, ipc.FoundingArgs{}); err == nil || !strings.Contains(err.Error(), "plugins on founding") {
		t.Fatalf("off: %v", err)
	}

	cfg.Plugins.Founding = true
	c = bareCore(t, cfg)
	r, err := c.foundingOp(0, ipc.FoundingArgs{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range r.Docs {
		ids = append(ids, d.ID)
		if len(d.Blocks) == 0 || len(d.SHA512) != 128 {
			t.Errorf("%s: %d blocks, SHA512 %q", d.ID, len(d.Blocks), d.SHA512)
		}
	}
	var want []string
	for _, d := range founding.Documents {
		want = append(want, d.ID)
	}
	if strings.Join(ids, ",") != strings.Join(want, ",") || r.Found != nil || len(r.Links) != len(founding.Links) {
		t.Errorf("docs %v, found %v, %d links", ids, r.Found, len(r.Links))
	}
	// The texts are on the local chain, checked: one version, made by the
	// first look, and no other by the next.
	if ch := r.Chain; ch == nil || !ch.Verified || ch.Versions != 1 || !chain.Valid(ch.Head) || ch.Folder != c.paths.Charters() || ch.Texts != len(founding.Documents) || ch.Links != len(founding.Links) {
		t.Fatalf("chain: %+v", r.Chain)
	}
	head := r.Chain.Head
	r, err = c.foundingOp(0, ipc.FoundingArgs{Log: 5, Export: true})
	if err != nil || r.Chain == nil || r.Chain.Head != head || len(r.Versions) != 1 || len(r.Versions[0].Files) != len(founding.Documents) || len(r.Export) != 1 || chain.Name([]byte(r.Export[0])) != head {
		t.Fatalf("again, with the log and the export: %+v, %d exported, %v", r.Versions, len(r.Export), err)
	}
	r, err = c.foundingOp(0, ipc.FoundingArgs{Find: "  unreasonable searches and seizures  "})
	if err != nil {
		t.Fatal(err)
	}
	where := map[string]bool{}
	for _, h := range r.Found {
		where[h.Doc+": "+h.Where] = true
	}
	// The Fourth Amendment, in the Constitution and the Bill of Rights, and
	// Massachusetts's Article XIV, which it drew on.
	if !where["constitution: Amendments › Amendment IV"] || !where["rights: Amendments › Amendment IV"] || !where["massachusetts: Article XIV."] || len(r.Found) != 3 || r.Chain != nil || r.Links != nil {
		t.Errorf("found %v", r.Found)
	}
}

// TestTheChartersGraphPage: the op writes the Charters Graph page into a
// folder of its own, private to the person, gives its path and nothing
// else, and leaves a file that already holds the page alone.
func TestTheChartersGraphPage(t *testing.T) {
	cfg := config.Default()
	c := bareCore(t, cfg)
	if _, err := c.foundingOp(0, ipc.FoundingArgs{Graph: true}); err == nil {
		t.Fatal("the page came while the plugin was off")
	}
	cfg.Plugins.Founding = true
	c = bareCore(t, cfg)
	r, err := c.foundingOp(0, ipc.FoundingArgs{Graph: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Page != c.paths.ChartersPage() || r.Docs != nil || r.Links != nil || r.Chain != nil {
		t.Fatalf("page %q, %d docs, %d links, chain %v", r.Page, len(r.Docs), len(r.Links), r.Chain)
	}
	want, err := founding.Page()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(r.Page)
	if err != nil || string(got) != string(want) {
		t.Fatalf("the file holds %d bytes of the page's %d: %v", len(got), len(want), err)
	}
	st, err := os.Stat(r.Page)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the page's mode: %v, %v", st.Mode(), err)
	}
	before := st.ModTime()
	if _, err := c.foundingOp(0, ipc.FoundingArgs{Graph: true}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(r.Page); !st.ModTime().Equal(before) {
		t.Error("the page was written again, unchanged")
	}
	// A page an older build wrote is replaced.
	if err := os.WriteFile(r.Page, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.foundingOp(0, ipc.FoundingArgs{Graph: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(r.Page); string(got) != string(want) {
		t.Error("an older page stayed")
	}
}

// TestTheCommonSchoolsPage: the founding op, asked for schools, writes the
// Common Schools page this build makes to its own folder and says where,
// only while the plugin is on.
func TestTheCommonSchoolsPage(t *testing.T) {
	cfg := config.Default()
	c := bareCore(t, cfg)
	if _, err := c.foundingOp(0, ipc.FoundingArgs{Schools: true}); err == nil {
		t.Fatal("the page came while the plugin was off")
	}
	cfg.Plugins.Founding = true
	c = bareCore(t, cfg)
	r, err := c.foundingOp(0, ipc.FoundingArgs{Schools: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Page != c.paths.SchoolsPage() || r.Docs != nil || r.Links != nil || r.Chain != nil {
		t.Fatalf("page %q, %d docs, %d links, chain %v", r.Page, len(r.Docs), len(r.Links), r.Chain)
	}
	want, err := schools.Page()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(r.Page)
	if err != nil || string(got) != string(want) {
		t.Fatalf("the file holds %d bytes of the page's %d: %v", len(got), len(want), err)
	}
	if st, err := os.Stat(r.Page); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the page's mode: %v", err)
	}
	if filepath.Dir(r.Page) == filepath.Dir(c.paths.ChartersPage()) {
		t.Error("the two pages share a folder, so each window could read the other's")
	}
}

// TestTheFirstBuildsFoundingChainMovesIn: a chain the first build kept at
// chain/founding, in its first form, moves into chain/charters as the core
// first looks, and the texts this build carries go on after its version.
func TestTheFirstBuildsFoundingChainMovesIn(t *testing.T) {
	cfg := config.Default()
	cfg.Plugins.Founding = true
	c := bareCore(t, cfg)
	ts, err := founding.Texts()
	if err != nil {
		t.Fatal(err)
	}
	v := founding.Version{Created: "2026-11-21T16:35:39Z", Format: founding.FormatFirst}
	for _, x := range ts {
		if x.ID == "declaration" || x.ID == "constitution" || x.ID == "rights" || x.ID == "foia" {
			v.Documents = append(v.Documents, founding.Recorded{File: x.File, ID: x.ID, SHA512: chain.Name(x.Bytes), Text: string(x.Bytes), Title: x.Title})
		}
	}
	b, err := chain.Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	s := chain.Store{Dir: c.paths.FoundingChainFirst()}
	old, err := s.Put(b)
	if err != nil || s.SetHead(old) != nil {
		t.Fatal(err)
	}
	r, err := c.foundingOp(0, ipc.FoundingArgs{Log: 5})
	if err != nil {
		t.Fatal(err)
	}
	if ch := r.Chain; !ch.Verified || ch.Versions != 2 || ch.Folder != c.paths.Charters() || len(r.Versions) != 2 || r.Versions[1].Name != old || r.Versions[0].Parent != old {
		t.Errorf("chain %+v, versions %+v", r.Chain, r.Versions)
	}
	if _, err := os.Lstat(c.paths.FoundingChainFirst()); err == nil {
		t.Error("the first build's folder stayed")
	}
}
