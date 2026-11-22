//go:build !nobitwarden

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// bitwardenHere points the plugin's look for bw at a test's own folder: a
// bw program when program is true, and data, when not "", as bw's data
// file.
func bitwardenHere(t *testing.T, program bool, data string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin", "bw")
	if program {
		if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(dir, "Bitwarden CLI", "data.json")
	if data != "" {
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	places, find := bwPlaces, bwData
	bwPlaces, bwData = []string{filepath.Join(dir, "none", "bw"), bin}, func() string { return file }
	t.Cleanup(func() { bwPlaces, bwData = places, find })
}

func bitwardenStatus(t *testing.T, e Env) ipc.Plugin {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == "bitwarden" {
			return p
		}
	}
	t.Fatal("no bitwarden plugin")
	return ipc.Plugin{}
}

// TestBitwardenSaysWhatItLacks: on by default, the autofill part needs the
// Bitwarden CLI, then its sign-in, and is ready once both are there; the
// data file's active account is all it reads of bw's.
func TestBitwardenSaysWhatItLacks(t *testing.T) {
	e := env(t)
	if !e.Config.Plugins.Bitwarden || !e.Config.BitwardenOn() {
		t.Fatal("the Bitwarden plugin is off by default")
	}
	for _, c := range []struct {
		program bool
		data    string
		state   string
		says    string
	}{
		{false, "", ipc.PartNeeds, "brew install bitwarden-cli"},
		{true, "", ipc.PartNeeds, "bw login"},
		{true, `{"global_account_activeAccountId":null,"global_account_accounts":{}}`, ipc.PartNeeds, "isn't signed in"},
		{true, `{"global_account_activeAccountId":"00000000-0000-4000-8000-000000000001","global_account_accounts":{}}`, ipc.PartReady, "Ready: unlock Bitwarden"},
		// A shape Irie doesn't know: unlocking says what is wrong.
		{true, `{"accounts":{}}`, ipc.PartReady, "Ready"},
		{true, `not json`, ipc.PartReady, "Ready"},
	} {
		bitwardenHere(t, c.program, c.data)
		p := bitwardenStatus(t, e)
		if !p.On || p.Title != "Bitwarden" || len(p.Parts) != 2 {
			t.Fatalf("bitwarden = %+v", p)
		}
		if x := part(t, p, Autofill); x.State != c.state || !strings.Contains(x.Detail, c.says) {
			t.Errorf("bw %v, data %q: autofill = %q (%q), want %q saying %q", c.program, c.data, x.State, x.Detail, c.state, c.says)
		}
		if x := part(t, p, Vault); x.State != ipc.PartHeld || x.Title != "Bitwarden for agents" {
			t.Errorf("vault = %+v", x)
		}
	}
}

// TestBitwardenOff: the plugin off turns autofill off; Bitwarden for agents
// stays held whatever the switch says, and refuses to be turned on.
func TestBitwardenOff(t *testing.T) {
	bitwardenHere(t, true, `{"global_account_activeAccountId":"00000000-0000-4000-8000-000000000001"}`)
	e := env(t)
	e.Config.Plugins.Bitwarden = false
	if e.Config.BitwardenOn() {
		t.Error("BitwardenOn with its plugin off")
	}
	p := bitwardenStatus(t, e)
	if p.On || part(t, p, Autofill).State != ipc.PartPluginOff || part(t, p, Vault).State != ipc.PartHeld {
		t.Errorf("bitwarden off = %+v", p)
	}
	if why := Why(config.Default(), Vault); !strings.Contains(why, "isn't in "+brand.Name+" yet") {
		t.Errorf("Why(vault) = %q", why)
	}
	if !strings.Contains(Text(Status(e)), "  ○ Bitwarden for agents: ") {
		t.Errorf("Text lacks the held part:\n%s", Text(Status(e)))
	}
	for _, c := range []struct{ plugin, part, want string }{
		{"bitwarden", "vault", "isn't in " + brand.Name + " yet"},
		{"bitwarden", "autofill", "no switch of its own"},
		{"bitwarden", "nothing", "its parts are autofill, vault"},
	} {
		if _, err := Edits(c.plugin, c.part, true); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Edits(%q, %q) = %v, want %q", c.plugin, c.part, err, c.want)
		}
	}
	got, err := Edits("bitwarden", "", true)
	if err != nil || len(got) != 1 || got[0].Key != "plugins.bitwarden" || got[0].Value != true {
		t.Errorf("Edits(bitwarden on) = %+v, %v", got, err)
	}
	if _, err := config.Apply(nil, got); err != nil {
		t.Errorf("config.Apply(%+v): %v", got, err)
	}
}
