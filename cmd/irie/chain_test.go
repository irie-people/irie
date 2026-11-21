package main

import (
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/ipc"
)

func TestChainLogAndShowRead(t *testing.T) {
	at := time.Date(2026, 11, 21, 14, 5, 9, 0, time.UTC)
	name := "sha512:3f2a9c1e00ab" + strings.Repeat("0", 116)
	doc := &ipc.ChainVersion{Format: ipc.ChainFormat, At: at.UnixMilli(), Org: ipc.ChainOrg{
		Spaces: []ipc.Space{{ID: "main", Name: "Main"}, {ID: "pwork", Name: "Work"}},
		Groups: []ipc.Group{{ID: "gsteak", Name: "Steak"}, {ID: "girie", Name: "Irie", Parent: "gsteak"}},
		Cards: []ipc.ChainCard{
			{ID: "s38fyj3p0c4fr", Name: "orchestrator", Pinned: true},
			{ID: "s38fyaba87zvs", Group: "girie"},
			{ID: "s38f4649rzfhn", Name: "api fix", Group: "gsteak"},
			{ID: "s38f4ay27vznt", Name: "old", Parked: true},
			{ID: "s38dv4jjmkbjw", Name: "elsewhere", Space: "pwork"},
		},
	}}
	e := ipc.ChainEntry{Name: name, State: name, At: at, What: `park "old"`, Cards: 5, Groups: 2, Spaces: 2, Doc: doc,
		Names: map[string]string{"s38fyaba87zvs": "Build the sidebar chain"}}
	var b strings.Builder
	if err := printChainLog(&b, ipc.ChainResult{Versions: []ipc.ChainEntry{e}}, time.UTC); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); !strings.Contains(got, "3f2a9c1e00ab  2026-11-21 14:05:09  park \"old\"  5 cards, 2 groups, 2 spaces") {
		t.Errorf("log:\n%s", got)
	}
	b.Reset()
	if err := printChainShow(&b, ipc.ChainResult{Version: &e}, time.UTC); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	// Pinned first, then the groups as a tree with their cards, the rest,
	// and Parked; another space's cards under it. A name in brackets is
	// one the version does not keep.
	want := []string{"Main", "Pinned", "orchestrator", "Steak/", "api fix", "Irie/", "(Build the sidebar chain)", "Parked", "old", "Work", "elsewhere"}
	at0 := 0
	for _, w := range want {
		i := strings.Index(got[at0:], w)
		if i < 0 {
			t.Fatalf("show lacks %q after byte %d:\n%s", w, at0, got)
		}
		at0 += i + len(w)
	}
	if !strings.Contains(got, "the first version") {
		t.Errorf("show:\n%s", got)
	}
}

// The status says plainly how the chain is kept at rest, and above all
// when the computer keeps no keys, so the key is in a file.
func TestChainStatusSaysWhereTheKeyIs(t *testing.T) {
	for kept, want := range map[string]string{
		chain.KeptInKeychain: "login Keychain",
		chain.KeptInSecrets:  "Secret Service",
		chain.KeptInFile:     "this computer has no key store: anyone who can read your files can read the chain",
	} {
		var b strings.Builder
		printChainStatus(&b, ipc.ChainResult{Dir: "/home/alice/.local/state/irie/chain", Head: "sha512:00", Count: 1, Vault: kept})
		if got := b.String(); !strings.Contains(got, want) || !strings.Contains(got, "AES-256-GCM") {
			t.Errorf("%s:\n%s", kept, got)
		}
	}
}
