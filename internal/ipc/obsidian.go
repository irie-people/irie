package ipc

import "time"

// OpObsidian is the obsidian plugin's vault (DESIGN §4.17): where it is,
// whether Obsidian has opened it, and how the notepad's note in it stands,
// for the Mac app's Vault cards and the Notes panel. It only reads: the
// vault is chosen with the config op, obsidian.vault.
const OpObsidian = "obsidian"

// ObsidianArgs asks where the vault is.
type ObsidianArgs struct{}

// ObsidianResult is where the vault is and how it stands. State is ready,
// needs (Text says what it lacks and what fixes it) or plugin_off.
type ObsidianResult struct {
	State string `json:"state"`
	Text  string `json:"text"`
	// Vault is the vault's folder, obsidian.vault with ~ expanded, and
	// Name its name, the folder's, as Obsidian names a vault.
	Vault string `json:"vault,omitempty"`
	Name  string `json:"name,omitempty"`
	// Opened says Obsidian has opened the folder as a vault: it holds
	// Obsidian's own folder of settings, so obsidian:// links reach it.
	Opened bool `json:"opened,omitempty"`
	// Notes is how the notepad's note in the vault stands.
	Notes *NotepadVault `json:"notes,omitempty"`
}

// NotepadVault is the notepad's note in the Obsidian vault (DESIGN §4.20),
// while obsidian.notes keeps it there: its file, when it and the notepad
// last agreed, and what keeps them apart, when something does.
type NotepadVault struct {
	Path    string    `json:"path"`
	Synced  time.Time `json:"synced,omitzero"`
	Problem string    `json:"problem,omitempty"`
}
