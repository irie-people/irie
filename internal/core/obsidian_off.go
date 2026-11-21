//go:build noobsidian

package core

import "github.com/irie-people/irie/internal/ipc"

// This build leaves the obsidian plugin out (go build -tags noobsidian,
// DESIGN §4.17): this stands in for obsidian.go, answering that it isn't
// in this build, and the notepad is kept in Irie alone.

const obsidianBuilt = false

type vaultNotes struct{}

func (c *Core) kickVault()                   {}
func (c *Core) watchVaultNotes()             {}
func (c *Core) vaultView() *ipc.NotepadVault { return nil }
func (c *Core) obsidianOp(ipc.ObsidianArgs) (ipc.ObsidianResult, error) {
	return ipc.ObsidianResult{}, notBuilt("The obsidian plugin")
}
