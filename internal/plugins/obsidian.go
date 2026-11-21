//go:build !noobsidian

package plugins

import (
	"os"
	"path/filepath"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/repo"
)

// The obsidian plugin's parts, by name. Bitwarden's vault is another
// plugin's part, so the vault's card is "card".
const (
	VaultCard  = "card"
	VaultNotes = "notes"
)

func init() { built["obsidian"] = obsidianPlugin }

// obsidianPlugin is the obsidian plugin: an Obsidian vault beside the
// sessions, read and written by Irie itself, so Obsidian is the person's
// to have or not, and the notepad kept in it as a note, both ways.
func obsidianPlugin() Plugin {
	return Plugin{
		Name:    "obsidian",
		Title:   "Obsidian",
		Summary: "Your Obsidian vault beside your sessions: its notes on a Vault card of the panes, read and written by " + brand.Name + " itself, and your notepad kept in it as a note, both ways. Obsidian itself is yours to have or not.",
		key:     "plugins.obsidian",
		on:      func(c config.Config) bool { return c.Plugins.Obsidian },
		Parts: []Part{
			{
				Name:    VaultCard,
				Title:   "Vault card",
				Summary: "A card of the panes beside a session with your vault's notes: [[links]] that open, #tags, properties, backlinks and quick find, saved as you type and taken in as Obsidian, a sync or an agent changes them, and Open in Obsidian for the rest.",
				Brings:  []string{"the Vault card in the Mac app's panes", "the Notes panel's Open in a Pane"},
				own:     always,
				check:   checkVault,
			},
			{
				Name:    VaultNotes,
				Title:   "Notes in the vault",
				Summary: "The notepad above the task bar kept as a note in the vault too, a task a note: tick, add or change one there, in Obsidian or on your phone through the vault's sync, and the notepad follows, and back.",
				Switch:  "obsidian.notes", On: true, Off: false,
				Brings:  []string{"obsidian.note in the vault, " + brand.Name + " Notes.md unless it says otherwise"},
				Refused: "Notes in the vault are off: `" + brand.Slug + " plugins on obsidian notes` keeps the notepad in the vault too.",
				own:     func(c config.Config) bool { return c.Obsidian.Notes },
				check:   checkVaultNotes,
			},
		},
	}
}

// VaultPlace is where the Obsidian vault is: obsidian.vault, ~ expanded; its
// name, its folder's, as Obsidian names a vault; whether Obsidian has
// opened it, so it holds Obsidian's own folder of settings; and its notes
// note, obsidian.note in it.
type VaultPlace struct {
	Folder, Name, Notes string
	Opened              bool
}

// FindVault finds the vault that cfg names. What it lacks, and what fixes
// that, is in a sentence, or "" when the vault is there.
func FindVault(cfg config.Config) (VaultPlace, string) {
	if cfg.Obsidian.Vault == "" {
		return VaultPlace{}, "No vault yet: choose your vault's folder, or make a new one, on a Vault card in the Mac app (View › New Pane › Vault), or set obsidian.vault in config.toml. A vault is a folder of Markdown notes, so Obsidian itself is optional."
	}
	h, _ := os.UserHomeDir()
	v := VaultPlace{Folder: expand(cfg.Obsidian.Vault)}
	v.Name = filepath.Base(v.Folder)
	if config.NoteInVault(cfg.Obsidian.Note) {
		v.Notes = filepath.Join(v.Folder, cfg.Obsidian.Note)
	}
	if st, err := os.Stat(v.Folder); err != nil || !st.IsDir() {
		return v, "obsidian.vault names " + repo.Tilde(v.Folder, h) + ", which isn't a folder here: choose another on a Vault card, or change obsidian.vault in config.toml."
	}
	if st, err := os.Stat(filepath.Join(v.Folder, ".obsidian")); err == nil && st.IsDir() {
		v.Opened = true
	}
	return v, ""
}

func checkVault(e Env, _ bool) (bool, string) {
	v, why := FindVault(e.Config)
	if why != "" {
		return false, why
	}
	h, _ := os.UserHomeDir()
	s := "Ready: your vault is " + repo.Tilde(v.Folder, h) + ". A Vault card in the Mac app's panes shows its notes, and the Notes panel's Open in a Pane opens one."
	if !v.Opened {
		s += " Obsidian hasn't opened it yet, which it needs to for Open in Obsidian: in Obsidian, Open folder as vault."
	}
	return true, s
}

func checkVaultNotes(e Env, on bool) (bool, string) {
	if !on {
		return true, "Off: the notepad stays in " + brand.Name + " alone. `" + brand.Slug + " plugins on obsidian notes` keeps it in the vault too."
	}
	v, why := FindVault(e.Config)
	if why != "" {
		return false, why
	}
	h, _ := os.UserHomeDir()
	return true, "Ready: your notes are kept in " + repo.Tilde(v.Notes, h) + " too, both ways: tick, add or change a task there and the notepad follows, within seconds. The notepad stays the copy kept whole."
}
