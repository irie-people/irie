//go:build !noobsidian

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

func obsidianStatus(t *testing.T, e Env) ipc.Plugin {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == "obsidian" {
			return p
		}
	}
	t.Fatal("no obsidian plugin")
	return ipc.Plugin{}
}

// TestObsidianWaitsForAVault: on by default, its card needs a vault and
// says how to choose one; the notes in the vault go on with it, by their
// own switch, and wait for the vault too.
func TestObsidianWaitsForAVault(t *testing.T) {
	e := env(t)
	p := obsidianStatus(t, e)
	if !p.On || p.Title != "Obsidian" || len(p.Parts) != 2 {
		t.Fatalf("obsidian = %+v", p)
	}
	card, notes := part(t, p, VaultCard), part(t, p, VaultNotes)
	if card.State != ipc.PartNeeds || card.Switch != "" || !strings.Contains(card.Detail, "No vault yet") || card.Pane != nil {
		t.Errorf("card = %+v", card)
	}
	if notes.State != ipc.PartNeeds || notes.Switch != "obsidian.notes" || !notes.On {
		t.Errorf("notes = %+v", notes)
	}
	if why := Why(e.Config, VaultCard); why != "" {
		t.Errorf("Why(card) = %q, want nothing: it is on", why)
	}
	if ed, err := Edits("obsidian", VaultNotes, false); err != nil || len(ed) != 1 || ed[0].Key != "obsidian.notes" || ed[0].Value != false {
		t.Errorf("Edits of the notes = %+v, %v", ed, err)
	}
	e.Config.Obsidian.Notes = false
	if why := Why(e.Config, VaultNotes); !strings.Contains(why, brand.Slug+" plugins on obsidian notes") {
		t.Errorf("Why(notes) off = %q", why)
	}
	if x := part(t, obsidianStatus(t, e), VaultNotes); x.State != ipc.PartOff || !strings.Contains(x.Detail, "stays in "+brand.Name+" alone") {
		t.Errorf("notes off = %+v", x)
	}
	e.Config.Plugins.Obsidian = false
	if x := part(t, obsidianStatus(t, e), VaultCard); x.State != ipc.PartPluginOff {
		t.Errorf("card with the plugin off = %+v", x)
	}
	if why := Why(e.Config, VaultCard); !strings.Contains(why, "the obsidian plugin is") {
		t.Errorf("Why(card) with the plugin off = %q", why)
	}
}

// TestAVaultIsAFolder: a vault that isn't a folder says so; a folder is
// ready, its notes' note in it, and says so until Obsidian has opened it.
func TestAVaultIsAFolder(t *testing.T) {
	e := env(t)
	dir := t.TempDir()
	e.Config.Obsidian.Vault = filepath.Join(dir, "Notes")
	if x := part(t, obsidianStatus(t, e), VaultCard); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "isn't a folder here") {
		t.Fatalf("a vault that isn't there = %+v", x)
	}
	if err := os.Mkdir(e.Config.Obsidian.Vault, 0o755); err != nil {
		t.Fatal(err)
	}
	v, why := FindVault(e.Config)
	if why != "" || v.Name != "Notes" || v.Opened || v.Notes != filepath.Join(dir, "Notes", brand.Name+" Notes.md") {
		t.Fatalf("FindVault = %+v, %q", v, why)
	}
	p := obsidianStatus(t, e)
	if x := part(t, p, VaultCard); x.State != ipc.PartReady || !strings.Contains(x.Detail, "Open folder as vault") {
		t.Errorf("card = %+v", x)
	}
	if x := part(t, p, VaultNotes); x.State != ipc.PartReady || !strings.Contains(x.Detail, brand.Name+" Notes.md") {
		t.Errorf("notes = %+v", x)
	}
	if err := os.Mkdir(filepath.Join(e.Config.Obsidian.Vault, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	if v, _ := FindVault(e.Config); !v.Opened {
		t.Errorf("a vault Obsidian opened: %+v", v)
	}
	if x := part(t, obsidianStatus(t, e), VaultCard); strings.Contains(x.Detail, "Open folder as vault") {
		t.Errorf("card once opened = %+v", x)
	}
	h, _ := os.UserHomeDir()
	e.Config.Obsidian.Vault = "~/" + strings.TrimPrefix(filepath.Join(dir, "Notes"), h+"/")
	if strings.HasPrefix(dir, h+"/") {
		if v, why := FindVault(e.Config); why != "" || v.Folder != filepath.Join(dir, "Notes") {
			t.Errorf("a ~ vault = %+v, %q", v, why)
		}
	}
}
