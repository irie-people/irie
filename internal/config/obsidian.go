package config

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/irie-people/irie/internal/brand"
)

// The obsidian plugin's settings (DESIGN §4.17) live here, so the config
// needs none of the plugin's packages: a build without it (go build -tags
// noobsidian) still reads a config.toml that sets them.

// Obsidian is the obsidian plugin: an Obsidian vault in the Mac app's Vault
// cards, and the notepad kept as a note in it, both ways.
type Obsidian struct {
	// Vault is the vault's folder: a folder of Markdown notes, whether or
	// not Obsidian has opened it yet. "" = none chosen yet.
	Vault string `toml:"vault"`
	// Notes keeps the notepad (DESIGN §4.20) as the note Note in the
	// vault too, both ways.
	Notes bool `toml:"notes"`
	// Note is the notes note, a path in the vault, such as "Irie
	// Notes.md".
	Note string `toml:"note"`
}

// defaultObsidian is the plugin's settings until config.toml says
// otherwise.
func defaultObsidian() Obsidian {
	return Obsidian{Notes: true, Note: brand.Name + " Notes.md"}
}

// ObsidianOn says whether the obsidian plugin runs: plugins.obsidian,
// with the plugin built in.
func (c Config) ObsidianOn() bool { return ObsidianBuilt && c.Plugins.Obsidian }

// NoteInVault says whether note is a Markdown note's path in a vault: a
// path from the vault's folder, ending in .md, that stays inside it and
// out of the folders Obsidian and others keep their own files in, those
// whose names start with a dot.
func NoteInVault(note string) bool {
	if note == "" || filepath.IsAbs(note) || strings.ContainsFunc(note, unicode.IsControl) || strings.Contains(note, `\`) {
		return false
	}
	if !strings.EqualFold(filepath.Ext(note), ".md") || filepath.Clean(note) != note {
		return false
	}
	for _, part := range strings.Split(note, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func checkObsidian(o Obsidian, bad func(string, ...any)) {
	if o.Vault != "" && !pathLike(o.Vault) {
		bad("obsidian.vault = %q: must be an absolute or ~ path to the vault's folder, or empty for none", o.Vault)
	}
	if !NoteInVault(o.Note) {
		bad("obsidian.note = %q: must be a Markdown note's path in the vault, such as %q, outside the folders whose names start with a dot", o.Note, defaultObsidian().Note)
	}
}
