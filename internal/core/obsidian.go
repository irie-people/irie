//go:build !noobsidian

package core

// The obsidian plugin (DESIGN §4.17): the Obsidian vault's place, for the
// Mac app's Vault cards, and the notepad (§4.20) kept as a note in the
// vault too, both ways (internal/obsidian).
//
// The notepad stays the copy kept whole. What the note changes reaches the
// notepad only as the notepad's own ops, each written as a transaction,
// so a task deleted in the note waits in Recently Deleted as any deleted
// note does; a note gone from the vault deletes nothing, and is written
// again; and notes gone at once, as from a file a sync emptied, are held
// and written back. The core writes the note only once it has been still
// for a while, so it never writes over what Obsidian is saving, and only
// once it has read it again and found it as it was. What the next merge
// weighs the note against is kept in obsidian-notes.json, so a restart
// tells a task deleted in the note from a note new in the notepad.
//
// vault.mu is taken before pad.mu, never after: a sync holds it while it
// changes the notepad. The notepad's frame reads vault.pub, which takes
// no lock.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/obsidian"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/repo"
)

// obsidianBuilt says whether this build has the obsidian plugin; go build
// -tags noobsidian leaves it out, and obsidian_off.go stands in.
const obsidianBuilt = true

const (
	// vaultQuiet is how long the note must be still after another
	// program changed it before the core writes it.
	vaultQuiet = 5 * time.Second
	// vaultEvery is the most often the core writes it.
	vaultEvery = time.Second
	// vaultMax is the largest note the core reads.
	vaultMax = 4 << 20
	// vaultRetry is how long a write that failed waits to be tried again.
	vaultRetry = 30 * time.Second
	// vaultSaid is how long the notepad says what a merge did, as notes
	// held, once the two agree again.
	vaultSaid = 10 * time.Minute
	// vaultStateVersion is obsidian-notes.json's version.
	vaultStateVersion = 1
)

// vaultNotes is the notepad's note in the vault, as the core last saw it.
type vaultNotes struct {
	once sync.Once
	kick chan struct{}
	// pub is what the notepad says of the note; nil while the notepad is
	// kept in Irie alone.
	pub atomic.Pointer[ipc.NotepadVault]

	mu      sync.Mutex
	path    string            // the note's file the rest is of; "" = none
	loaded  bool              // obsidian-notes.json was read for path
	base    []obsidian.Synced // its tasks when it and the notepad last agreed; nil = never
	doc     *obsidian.Doc     // the note as last read or written, its tasks named
	text    string            // its text then
	had     bool              // the file was there then
	outside time.Time         // when another program last changed it
	wrote   time.Time         // when the core last wrote it
	synced  time.Time         // when it and the notepad last agreed
	problem string
	said    time.Time // when problem was first said
}

// vaultState is obsidian-notes.json.
type vaultState struct {
	Version int               `json:"version"`
	Path    string            `json:"path"`
	Base    []obsidian.Synced `json:"base"`
	Synced  time.Time         `json:"synced,omitzero"`
}

func (v *vaultNotes) kicks() chan struct{} {
	v.once.Do(func() { v.kick = make(chan struct{}, 1) })
	return v.kick
}

// kickVault says the notepad changed, so the note follows at once rather
// than at the next look. The caller may hold any lock.
func (c *Core) kickVault() {
	select {
	case c.vault.kicks() <- struct{}{}:
	default:
	}
}

// watchVaultNotes keeps the notepad and its note in the vault in step: a
// look each second, and one as the notepad changes.
func (c *Core) watchVaultNotes() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-c.vault.kicks():
		}
		c.syncVaultNotes(time.Now())
	}
}

// vaultView is what the notepad says of its note in the vault.
func (c *Core) vaultView() *ipc.NotepadVault {
	if p := c.vault.pub.Load(); p != nil {
		v := *p
		return &v
	}
	return nil
}

// vaultNotesFile is the notepad's note in the vault: "" while the notepad
// is kept in Irie alone; then why, when it can't be kept where the config
// says, as a vault that isn't there.
func vaultNotesFile(c config.Config) (string, string) {
	if !c.ObsidianOn() || !c.Obsidian.Notes || c.Obsidian.Vault == "" {
		return "", ""
	}
	v, lacks := plugins.FindVault(c)
	if lacks != "" {
		return "", lacks
	}
	return v.Notes, ""
}

// padNotes are the notes and Recently Deleted as the merge needs them, and
// whether the notepad is shown only.
func (c *Core) padNotes() ([]obsidian.Note, []obsidian.Note, bool) {
	p := &c.pad
	p.mu.Lock()
	defer p.mu.Unlock()
	notes := make([]obsidian.Note, len(p.notes))
	for i, n := range p.notes {
		notes[i] = obsidian.Note{ID: n.ID, Text: n.Text, Done: n.Done}
	}
	deleted := make([]obsidian.Note, len(p.deleted))
	for i, n := range p.deleted {
		deleted[i] = obsidian.Note{ID: n.ID, Text: n.Text, Done: n.Done}
	}
	return notes, deleted, p.readOnly
}

// syncVaultNotes is one look at the notepad's note in the vault: what the
// note changed since the last look goes to the notepad, then, once the
// note is still, the notepad goes to the note.
func (c *Core) syncVaultNotes(now time.Time) {
	v := &c.vault
	v.mu.Lock()
	defer v.mu.Unlock()
	cfg := *c.cfg()
	path, why := vaultNotesFile(cfg)
	if path == "" {
		v.reset("")
		c.publishVault(why, path)
		return
	}
	if path != v.path {
		v.reset(path)
	}
	if !v.loaded {
		v.load(c.paths.VaultNotes())
	}
	notes, deleted, readOnly := c.padNotes()
	if readOnly {
		// The notepad is a newer core's, or doesn't read: it says so itself.
		c.publishVault("", path)
		return
	}
	text, there, err := readVaultNote(path)
	if err != nil {
		v.say(fmt.Sprintf("Your notes' note in the vault doesn't read, so it and the notepad wait: %v.", err), now)
		c.publishVault(v.problem, path)
		return
	}
	merged := false
	switch {
	case !there:
		// Gone from the vault, or never there: it is written again from
		// the notepad, every note; nothing is deleted. A note just gone,
		// as a sync replaces it, waits as one just changed does.
		if v.doc == nil || v.had {
			if v.had {
				v.outside = now
			}
			v.doc, v.text, v.had = obsidian.Fresh(vaultIntro()), "", false
		}
	case v.doc == nil || !v.had || text != v.text:
		if v.doc != nil && v.had {
			v.outside = now
		}
		d := obsidian.Parse(text)
		plan := obsidian.Merge(d, v.base, notes, deleted, newNoteID)
		v.say(c.applyVaultPlan(plan, path), now)
		v.doc, v.text, v.had = d, text, true
		v.base = d.Synced()
		v.keep(c.paths.VaultNotes(), c.log)
		notes, _, _ = c.padNotes()
		merged = true
	}
	want, ids := v.doc.Render(notes)
	if want == v.text && there {
		if v.problem != "" && now.Sub(v.said) > vaultSaid {
			v.say("", now)
		}
		// When they came to agree, not each look that finds them so, or
		// every look would tell every client.
		if v.problem == "" && (merged || v.synced.IsZero()) {
			v.synced = now
		}
		c.publishVault(v.problem, path)
		return
	}
	if now.Sub(v.outside) < vaultQuiet || now.Sub(v.wrote) < vaultEvery {
		c.publishVault(v.problem, path)
		return
	}
	// Once more, just before: a change since is weighed first, next look.
	if again, ok, err := readVaultNote(path); err != nil || ok != there || again != text {
		return
	}
	if err := writeVaultNote(path, want); err != nil {
		why := fmt.Sprintf("Your notes couldn't be written to the vault, so it and the notepad wait: %v.", err)
		if why != v.problem {
			c.log.Warn("obsidian: writing the notes' note", "err", err)
		}
		v.say(why, now)
		v.wrote = now.Add(vaultRetry - vaultEvery)
		c.publishVault(v.problem, path)
		return
	}
	d := obsidian.Parse(want)
	d.Name(ids)
	v.doc, v.text, v.had, v.wrote = d, want, true, now
	v.base = d.Synced()
	if v.problem == "" {
		v.synced = now
	}
	v.keep(c.paths.VaultNotes(), c.log)
	c.publishVault(v.problem, path)
}

// applyVaultPlan makes the changes a merge planned, each an op of the
// notepad's own. A change the notepad turns down leaves its task as it
// was written, so its words stay in the note. It says what the person
// should know: changes turned down, and notes held.
func (c *Core) applyVaultPlan(plan obsidian.Plan, path string) string {
	var refused []string
	for _, ch := range plan.Changes {
		if _, err := c.notepadOp(ch.NotepadArgs); err != nil {
			var e *ipc.Error
			if errors.As(err, &e) && e.Code == ipc.CodeNotFound && ch.Do != ipc.NotepadAdd && ch.Do != ipc.NotepadEdit {
				continue // a note the notepad deleted meanwhile
			}
			if ch.Task != nil {
				ch.Task.Unname()
			}
			refused = append(refused, err.Error())
			c.log.Warn("obsidian: a change from the notes' note", "do", ch.Do, "id", ch.ID, "err", err)
		}
	}
	if len(plan.Changes) > 0 {
		c.log.Info("obsidian: the notes' note changed the notepad", "changes", len(plan.Changes), "held", plan.Held)
	}
	var says []string
	if plan.Held > 0 {
		says = append(says, fmt.Sprintf("%d notes went from %s at once, so they were written back, in case a sync or a slip took them: delete them in the notepad if you meant to.", plan.Held, filepath.Base(path)))
	}
	if len(refused) > 0 {
		says = append(says, fmt.Sprintf("The notepad turned down what the note asked, so those tasks stay as written: %s", strings.Join(slices.Compact(refused), " ")))
	}
	return strings.Join(says, " ")
}

// publishVault sets what the notepad says of its note, and tells the
// notepad's subscribers when that changed.
func (c *Core) publishVault(problem, path string) {
	v := &c.vault
	var next *ipc.NotepadVault
	if path != "" || problem != "" {
		next = &ipc.NotepadVault{Path: path, Synced: v.synced.UTC().Truncate(time.Second), Problem: problem}
	}
	old := v.pub.Load()
	if old == nil && next == nil || old != nil && next != nil && *old == *next {
		return
	}
	v.pub.Store(next)
	c.publishNotepad()
}

// say sets what the notepad says of its note, keeping when it was first
// said. The caller holds v.mu.
func (v *vaultNotes) say(problem string, now time.Time) {
	if problem != v.problem {
		v.problem, v.said = problem, now
	}
}

// reset forgets what was seen of a note, as its place changed. The caller
// holds v.mu.
func (v *vaultNotes) reset(path string) {
	if v.path == path {
		return
	}
	v.path, v.loaded, v.base, v.doc, v.text, v.had = path, false, nil, nil, "", false
	v.outside, v.wrote, v.synced, v.problem, v.said = time.Time{}, time.Time{}, time.Time{}, "", time.Time{}
}

// load takes what the next merge weighs the note against from
// obsidian-notes.json, when it is of this note.
func (v *vaultNotes) load(file string) {
	v.loaded = true
	b, err := os.ReadFile(file)
	if err != nil {
		return
	}
	var s vaultState
	if json.Unmarshal(b, &s) != nil || s.Version > vaultStateVersion || s.Path != v.path {
		return
	}
	v.base, v.synced = s.Base, s.Synced
}

// keep writes obsidian-notes.json; one that fails costs only a restart's
// knowledge of deletes made meanwhile.
func (v *vaultNotes) keep(file string, log *slog.Logger) {
	b, err := json.MarshalIndent(vaultState{Version: vaultStateVersion, Path: v.path, Base: v.base, Synced: v.synced.UTC()}, "", "  ")
	if err == nil {
		err = writeWhole(file, append(b, '\n'))
	}
	if err != nil {
		log.Warn("obsidian: keeping the notes' record", "err", err)
	}
}

// readVaultNote reads the note: false when it isn't there.
func readVaultNote(path string) (string, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	if !st.Mode().IsRegular() {
		return "", false, fmt.Errorf("%s isn't a file", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, vaultMax+1))
	if err != nil {
		return "", false, err
	}
	if len(b) > vaultMax {
		return "", false, fmt.Errorf("it is over %d MB", vaultMax>>20)
	}
	return string(b), true, nil
}

// writeVaultNote writes the note whole or not at all, where it lives when
// it is a link, keeping its mode; a new one is the person's alone.
func writeVaultNote(path, text string) error {
	if t, err := filepath.EvalSymlinks(path); err == nil {
		path = t
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, text)
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// vaultIntro is the first line of a new notes' note, which Obsidian hides
// as a comment.
func vaultIntro() string {
	return "%% " + brand.Name + " keeps your notepad here, a task a note: tick, add or change one, and " + brand.Name +
		"'s notepad follows within seconds, and back. The ^ mark at a task's end is how each finds it again. Lines that are no task stay as you write them. %%"
}

// obsidianOp is where the vault is and how the notepad's note in it
// stands, for the Mac app's Vault cards and Notes panel.
func (c *Core) obsidianOp(ipc.ObsidianArgs) (ipc.ObsidianResult, error) {
	cfg := *c.cfg()
	if why := plugins.Why(cfg, plugins.VaultCard); why != "" {
		return ipc.ObsidianResult{State: ipc.PartPluginOff, Text: why}, nil
	}
	v, lacks := plugins.FindVault(cfg)
	r := ipc.ObsidianResult{State: ipc.PartReady, Vault: v.Folder, Name: v.Name, Opened: v.Opened, Notes: c.vaultView()}
	if lacks != "" {
		r.State, r.Text = ipc.PartNeeds, lacks
		return r, nil
	}
	h, _ := os.UserHomeDir()
	r.Text = "Your vault is " + repo.Tilde(v.Folder, h) + "."
	if r.Notes != nil && r.Notes.Problem == "" {
		r.Text += " Your notepad is kept in it too, as " + repo.Tilde(r.Notes.Path, h) + "."
	}
	return r, nil
}
