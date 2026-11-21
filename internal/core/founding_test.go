//go:build !nofounding

package core

import (
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// TestFoundingOpWaitsForItsPlugin: while the founding plugin is off, the op
// says which switch turns it on; on, it gives the four documents, their
// local chain, checked, and a search's passages, to anyone, an agent too.
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
	if strings.Join(ids, ",") != "declaration,constitution,rights,foia" || r.Found != nil {
		t.Errorf("docs %v, found %v", ids, r.Found)
	}
	// The texts are on the local chain, checked: one version, made by the
	// first look, and no other by the next.
	if ch := r.Chain; ch == nil || !ch.Verified || ch.Versions != 1 || !chain.Valid(ch.Head) || ch.Folder != c.paths.FoundingChain() {
		t.Fatalf("chain: %+v", r.Chain)
	}
	head := r.Chain.Head
	r, err = c.foundingOp(0, ipc.FoundingArgs{Log: 5})
	if err != nil || r.Chain == nil || r.Chain.Head != head || len(r.Versions) != 1 || len(r.Versions[0].Files) != 4 {
		t.Fatalf("again, with the log: %+v, %v", r.Versions, err)
	}
	r, err = c.foundingOp(0, ipc.FoundingArgs{Find: "  unreasonable searches and seizures  "})
	if err != nil {
		t.Fatal(err)
	}
	where := map[string]bool{}
	for _, h := range r.Found {
		where[h.Doc+": "+h.Where] = true
	}
	if !where["constitution: Amendments › Amendment IV"] || !where["rights: Amendments › Amendment IV"] || len(r.Found) != 2 || r.Chain != nil {
		t.Errorf("found %v", r.Found)
	}
}
