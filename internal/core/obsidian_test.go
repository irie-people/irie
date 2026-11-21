//go:build !noobsidian

package core

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/obsidian"
)

// vaultCore is padCore with a vault: a folder Obsidian has opened, as its
// folder of settings says, with the notepad kept in it.
func vaultCore(t *testing.T, home, vault string) *Core {
	t.Helper()
	c := padCore(t, home, nil)
	cfg := *c.cfg()
	cfg.Obsidian.Vault = vault
	c.useConfig(cfg)
	return c
}

func readNote(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeNote(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The notepad is kept in the vault as Irie Notes.md: what the note changes
// comes to the notepad as the notepad's own changes, the person's own
// lines stay where they wrote them, the notepad's changes go to the note
// once it is still, and the next core knows what the last one saw.
func TestTheNotepadIsKeptInTheVault(t *testing.T) {
	home, vault := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(vault, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := vaultCore(t, home, vault)
	note := filepath.Join(vault, brand.Name+" Notes.md")
	printer := c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "6F1D-printer", Text: "Call the printer"}).Note.ID
	flights := c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "6F1D-flights", Text: "Book flights"}).Note.ID
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	c.syncVaultNotes(t0)
	text := readNote(t, note)
	want := vaultIntro() + "\n\n- [ ] Call the printer ^" + obsidian.Mark(printer) + "\n- [ ] Book flights ^" + obsidian.Mark(flights) + "\n"
	if text != want {
		t.Fatalf("the new note:\n%s\nwant:\n%s", text, want)
	}
	if st, _ := os.Stat(note); st.Mode().Perm() != 0o600 {
		t.Errorf("a new note's mode is %v, want 0600", st.Mode().Perm())
	}
	r := c.padDo(t, ipc.NotepadArgs{})
	if r.Vault == nil || r.Vault.Path != note || r.Vault.Problem != "" || r.Vault.Synced.IsZero() {
		t.Fatalf("the notepad says of its note: %+v", r.Vault)
	}

	// Obsidian ticks one off, adds a line and a heading of the person's.
	edited := strings.Replace(text, "- [ ] Call the printer", "## Today\n- [x] Call the printer", 1) + "- [ ] Water the plants\n"
	writeNote(t, note, edited)
	c.syncVaultNotes(t0.Add(2 * time.Second))
	r = c.padDo(t, ipc.NotepadArgs{})
	if got, want := texts(r.Notes), []string{"[x] Call the printer", "[ ] Book flights", "[ ] Water the plants"}; !slices.Equal(got, want) {
		t.Fatalf("the notepad after the note changed: %q, want %q", got, want)
	}
	// Not yet written: the note was just changed by another program.
	if got := readNote(t, note); got != edited {
		t.Fatalf("the core wrote over a note changed a moment ago:\n%s", got)
	}
	c.syncVaultNotes(t0.Add(8 * time.Second))
	plants := r.Notes[2].ID
	text = readNote(t, note)
	if !strings.Contains(text, "## Today\n- [x] Call the printer ^"+obsidian.Mark(printer)) || !strings.Contains(text, "- [ ] Water the plants ^"+obsidian.Mark(plants)) {
		t.Fatalf("the note once still:\n%s", text)
	}

	// The notepad changes: the note follows at once, while it is still.
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadCheck, ID: flights, Done: true})
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadEdit, ID: plants, Text: "Water the plants\nand the herbs"})
	c.syncVaultNotes(t0.Add(9 * time.Second))
	text = readNote(t, note)
	if !strings.Contains(text, "- [x] Book flights ^"+obsidian.Mark(flights)) || !strings.Contains(text, "- [ ] Water the plants\n  and the herbs ^"+obsidian.Mark(plants)) {
		t.Fatalf("the note after the notepad changed:\n%s", text)
	}

	// A line deleted in the note waits in Recently Deleted.
	writeNote(t, note, strings.Replace(text, "- [x] Book flights ^"+obsidian.Mark(flights)+"\n", "", 1))
	c.syncVaultNotes(t0.Add(20 * time.Second))
	r = c.padDo(t, ipc.NotepadArgs{})
	if len(r.Notes) != 2 || len(r.Deleted) != 1 || r.Deleted[0].ID != flights {
		t.Fatalf("after a line was deleted in the note: notes %q, deleted %q", texts(r.Notes), texts(r.Deleted))
	}

	// The note gone from the vault deletes nothing: it is written again,
	// once it has stayed gone a while, as a sync may put it back itself.
	if err := os.Remove(note); err != nil {
		t.Fatal(err)
	}
	c.syncVaultNotes(t0.Add(30 * time.Second))
	if _, err := os.Stat(note); err == nil {
		t.Fatal("a note just gone was written again at once")
	}
	c.syncVaultNotes(t0.Add(36 * time.Second))
	r = c.padDo(t, ipc.NotepadArgs{})
	if len(r.Notes) != 2 || !strings.Contains(readNote(t, note), "Call the printer ^"+obsidian.Mark(printer)) {
		t.Fatalf("after the note went: notes %q, note:\n%s", texts(r.Notes), readNote(t, note))
	}

	// A restart: the next core knows what this one saw, so a line deleted
	// while no core ran is a delete, not a note new in the notepad.
	text = readNote(t, note)
	c2 := vaultCore(t, home, vault)
	writeNote(t, note, strings.Replace(text, "- [x] Call the printer ^"+obsidian.Mark(printer)+"\n", "", 1))
	c2.syncVaultNotes(t0.Add(40 * time.Second))
	r = c2.padDo(t, ipc.NotepadArgs{})
	if got, want := texts(r.Notes), []string{"[ ] Water the plants\nand the herbs"}; !slices.Equal(got, want) {
		t.Fatalf("after a restart and a delete in the note: %q, want %q", got, want)
	}

	op, err := c2.obsidianOp(ipc.ObsidianArgs{})
	if err != nil || op.State != ipc.PartReady || op.Vault != vault || op.Name != filepath.Base(vault) || !op.Opened || op.Notes == nil || op.Notes.Path != note {
		t.Fatalf("the obsidian op: %+v, %v", op, err)
	}
}

// Notes gone from the note at once, as from a file emptied by a sync, are
// kept, written back, and the notepad says why.
func TestNotesGoneFromTheVaultAtOnceAreKept(t *testing.T) {
	home, vault := t.TempDir(), t.TempDir()
	c := vaultCore(t, home, vault)
	note := filepath.Join(vault, brand.Name+" Notes.md")
	for _, s := range []string{"one", "two", "three", "four", "five"} {
		c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, Text: s})
	}
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	c.syncVaultNotes(t0)
	writeNote(t, note, "")
	c.syncVaultNotes(t0.Add(time.Second))
	r := c.padDo(t, ipc.NotepadArgs{})
	if len(r.Notes) != 5 || len(r.Deleted) != 0 || r.Vault == nil || !strings.Contains(r.Vault.Problem, "5 notes went from") {
		t.Fatalf("after the note was emptied: notes %q, deleted %d, vault %+v", texts(r.Notes), len(r.Deleted), r.Vault)
	}
	c.syncVaultNotes(t0.Add(10 * time.Second))
	if got := readNote(t, note); strings.Count(got, "- [ ] ") != 5 {
		t.Fatalf("the notes weren't written back:\n%s", got)
	}
}

// The notepad stays in Irie alone while the plugin, or its part, is off,
// and with no vault; a vault that isn't there is said, and nothing is
// written; nor is a notepad a newer core wrote.
func TestTheNotepadStaysInIrieWhenItShould(t *testing.T) {
	home, vault := t.TempDir(), t.TempDir()
	note := filepath.Join(vault, brand.Name+" Notes.md")
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	for _, off := range []string{"plugin", "part", "vault"} {
		c := vaultCore(t, home, vault)
		cfg := *c.cfg()
		switch off {
		case "plugin":
			cfg.Plugins.Obsidian = false
		case "part":
			cfg.Obsidian.Notes = false
		case "vault":
			cfg.Obsidian.Vault = ""
		}
		c.useConfig(cfg)
		c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, Text: "kept in Irie"})
		c.syncVaultNotes(t0)
		if _, err := os.Stat(note); err == nil {
			t.Fatalf("%s off, the note was written", off)
		}
		if r := c.padDo(t, ipc.NotepadArgs{}); r.Vault != nil {
			t.Fatalf("%s off, the notepad says %+v", off, r.Vault)
		}
	}

	c := vaultCore(t, home, filepath.Join(vault, "not there"))
	c.syncVaultNotes(t0)
	if r := c.padDo(t, ipc.NotepadArgs{}); r.Vault == nil || r.Vault.Path != "" || !strings.Contains(r.Vault.Problem, "isn't a folder here") {
		t.Fatalf("a vault that isn't there: %+v", r.Vault)
	}
	if op, _ := c.obsidianOp(ipc.ObsidianArgs{}); op.State != ipc.PartNeeds || !strings.Contains(op.Text, "isn't a folder here") {
		t.Fatalf("the op of a vault that isn't there: %+v", op)
	}

	// A newer core's notepad is shown only, and its notes go nowhere.
	home2 := t.TempDir()
	c = vaultCore(t, home2, vault)
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, Text: "a newer core's"})
	b := readNote(t, c.paths.Notepad())
	writeNote(t, c.paths.Notepad(), strings.Replace(b, `"version": 1`, `"version": 9`, 1))
	c = vaultCore(t, home2, vault)
	c.syncVaultNotes(t0)
	if _, err := os.Stat(note); err == nil {
		t.Fatal("a newer core's notepad was written to the vault")
	}
}

// Looks that find the note and the notepad agreeing tell no client: the
// notepad's frame changes only as they come to agree.
func TestAVaultInStepIsQuiet(t *testing.T) {
	home, vault := t.TempDir(), t.TempDir()
	c := vaultCore(t, home, vault)
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, Text: "one"})
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	c.syncVaultNotes(t0)
	first := c.vaultView()
	for i := 1; i <= 5; i++ {
		c.syncVaultNotes(t0.Add(time.Duration(i) * time.Second))
	}
	if now := c.vaultView(); first == nil || now == nil || *now != *first {
		t.Fatalf("the notepad's word on its note changed with nothing to say: %+v, then %+v", first, now)
	}
}
