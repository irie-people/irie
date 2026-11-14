package core

// The notepad (DESIGN §4.20): notes a person keeps for later, ticked off
// like the lines of a checklist, which a sidebar shows between the selected
// session's details and the task bar, and which go to the task bar as if
// typed there when the time comes (InputArgs.Note).
//
// The notes are the person's own words, so nothing is to lose them:
//   - every change is written before the op answers, as a transaction
//     (internal/filetx): the file holds the notes before the change or
//     after it, never part of either, whatever stops the core, and an op
//     whose write fails changes nothing;
//   - the first change of each day first copies the file as it stood to
//     backups/notepad/<day>.json beside it, and the newest 30 copies stay;
//   - a deleted note waits in Recently Deleted for 30 days, the newest 500,
//     and restore puts it back where it was;
//   - a file that does not read is never written over: it is moved aside
//     to notepad.json.bad-<time>, and the notes come from the newest copy
//     that reads, which the notepad's problem says;
//   - a file a newer version wrote, as after going back to an older build,
//     is shown and never written, so the newer core's notes stay whole;
//   - each change is the same however often it is sent (ipc.NotepadArgs),
//     so a client keeps what was typed while the core is away and sends it
//     again once the core is back.
//
// The notepad has a lock of its own, pad.mu, never held with c.mu.

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
)

const (
	// notepadVersion is the version of notepad.json this core writes.
	notepadVersion = 1
	// How long, and how many, deleted notes wait in Recently Deleted.
	notepadKeepDeleted = 30 * 24 * time.Hour
	notepadMaxDeleted  = 500
	// notepadCopies is how many daily copies of the file stay.
	notepadCopies = 30
)

// noteID is the shape of a note's id: a client's own, such as a UUID, or
// one the core made.
var noteID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)

// copyName is the name of a daily copy: its day.
var copyName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.json$`)

// notepad is the notes, as the file holds them.
type notepad struct {
	mu       sync.Mutex
	notes    []padNote
	deleted  []padNote // the last deleted first
	problem  string    // what went wrong with the file, for the person
	readOnly bool      // the file is a newer version's, or would not read: nothing is written
	rev      uint64    // the changes the file has had (ipc.Notepad.Rev)
	now      func() time.Time
}

// padNote is a note as the file keeps it. A note emptied by an edit keeps
// what it said before, which a delete puts back in Recently Deleted, so
// emptying a note and taking the empty line away never loses its words. A
// deleted one also keeps where it was: the notes it came after and before,
// and its place then.
type padNote struct {
	ipc.Note
	Was  string `json:"was,omitempty"`
	Prev string `json:"prev,omitempty"`
	Next string `json:"next,omitempty"`
	At   int    `json:"at,omitempty"`
}

// padFile is notepad.json.
type padFile struct {
	Version int       `json:"version"`
	Rev     uint64    `json:"rev"`
	Notes   []padNote `json:"notes"`
	Deleted []padNote `json:"deleted"`
}

// badPad is a notepad.json that is there but is not one.
type badPad struct{ err error }

func (b *badPad) Error() string { return b.err.Error() }

func (p *notepad) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// loadNotepad reads the notes a previous core kept.
func (c *Core) loadNotepad() {
	p := &c.pad
	p.mu.Lock()
	defer p.mu.Unlock()
	path := c.paths.Notepad()
	f, err := readNotepad(path)
	var bad *badPad
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		return
	case errors.As(err, &bad):
		f = c.recoverNotepad(path, bad)
	default:
		// It is there and could not be read, as without permission: it
		// may hold every note, so nothing goes over it.
		p.readOnly = true
		p.problem = fmt.Sprintf("Your notes could not be read, so none show and nothing is written over them: %v.", err)
		c.log.Warn("notepad", "err", err)
		return
	}
	if f.Version > notepadVersion {
		p.readOnly = true
		p.problem = fmt.Sprintf("A newer %s wrote these notes, so this one shows them and changes nothing, to keep them whole.", brand.Name)
	}
	p.notes, p.deleted, p.rev = cleanNotes(f.Notes), cleanNotes(f.Deleted), f.Rev
	p.deleted = pruneDeleted(p.deleted, p.clock())
}

// readNotepad reads a notepad file: a *badPad when it is there but is not
// JSON, as a file cut short by another program would be.
func readNotepad(path string) (padFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return padFile{}, err
	}
	var f padFile
	if err := json.Unmarshal(b, &f); err != nil {
		return padFile{}, &badPad{err}
	}
	return f, nil
}

// recoverNotepad moves a notepad.json that does not read aside, where
// nothing writes over it, and takes the notes from the newest daily copy
// that reads, which it writes back as notepad.json. The caller holds p.mu.
func (c *Core) recoverNotepad(path string, bad error) padFile {
	p := &c.pad
	aside := path + ".bad-" + p.clock().UTC().Format("20060102T150405Z")
	if err := os.Rename(path, aside); err != nil {
		p.readOnly = true
		p.problem = fmt.Sprintf("Your notes file does not read (%v) and could not be moved aside, so nothing is written over it.", bad)
		c.log.Warn("notepad", "err", bad, "aside", err)
		return padFile{}
	}
	c.log.Warn("notepad: the file does not read, so it was moved aside", "to", aside, "err", bad)
	for _, cp := range c.notepadCopyFiles() {
		f, err := readNotepad(cp)
		if err != nil || f.Version > notepadVersion {
			continue
		}
		day := strings.TrimSuffix(filepath.Base(cp), ".json")
		p.problem = fmt.Sprintf("Your notes file did not read, so these are your notes as they stood on %s. The file that did not read is kept as %s.", day, aside)
		if err := c.writeNotepad(cleanNotes(f.Notes), cleanNotes(f.Deleted), f.Rev+1); err != nil {
			c.log.Warn("notepad: writing the copy back", "err", err)
		} else {
			f.Rev++
		}
		return f
	}
	p.problem = fmt.Sprintf("Your notes file did not read and there is no copy of it, so the notepad starts empty. The file that did not read is kept as %s.", aside)
	return padFile{}
}

// cleanNotes drops what no note can be: one without a usable id, and a
// second note with an id another has.
func cleanNotes(in []padNote) []padNote {
	seen := map[string]bool{}
	out := make([]padNote, 0, len(in))
	for _, n := range in {
		if !noteID.MatchString(n.ID) || seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		out = append(out, n)
	}
	return out
}

// pruneDeleted keeps the notes deleted in the last notepadKeepDeleted, the
// newest notepadMaxDeleted of them.
func pruneDeleted(deleted []padNote, now time.Time) []padNote {
	out := slices.DeleteFunc(deleted, func(n padNote) bool {
		return !n.Deleted.IsZero() && now.Sub(n.Deleted) > notepadKeepDeleted
	})
	return out[:min(len(out), notepadMaxDeleted)]
}

// writeNotepad writes the notes as notepad.json, its rev-th change, as a
// transaction, after the day's copy of the file as it stood. The caller
// holds p.mu.
func (c *Core) writeNotepad(notes, deleted []padNote, rev uint64) error {
	if notes == nil {
		notes = []padNote{}
	}
	if deleted == nil {
		deleted = []padNote{}
	}
	path := c.paths.Notepad()
	c.copyNotepad(path)
	b, err := json.MarshalIndent(padFile{Version: notepadVersion, Rev: rev, Notes: notes, Deleted: deleted}, "", "  ")
	if err != nil {
		return err
	}
	return filetx.WriteFile(path, append(b, '\n'), 0o600)
}

// copyNotepad copies the notepad's file as it stands to the day's copy,
// the first time a day changes it, and keeps the newest notepadCopies
// copies. A copy that fails costs only itself. The caller holds p.mu.
func (c *Core) copyNotepad(path string) {
	dir := c.paths.NotepadCopies()
	to := filepath.Join(dir, c.pad.clock().Format("2006-01-02")+".json")
	if _, err := os.Stat(to); err == nil {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return // nothing written yet
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		c.log.Warn("notepad: copy", "err", err)
		return
	}
	if err := writeWhole(to, b); err != nil {
		c.log.Warn("notepad: copy", "err", err)
		return
	}
	copies := c.notepadCopyFiles()
	for _, old := range copies[min(notepadCopies, len(copies)):] {
		os.Remove(old)
	}
}

// notepadCopyFiles are the daily copies, the newest first.
func (c *Core) notepadCopyFiles() []string {
	dir := c.paths.NotepadCopies()
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if !e.IsDir() && copyName.MatchString(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	slices.Sort(out)
	slices.Reverse(out)
	return out
}

// writeWhole writes a file no one else writes whole or not at all: a
// temporary file beside it, synced to the disk, then renamed over it.
func writeWhole(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(0o600)
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

func newNoteID() string {
	var b [6]byte
	rand.Read(b[:])
	return "n" + hex.EncodeToString(b[:])
}

func indexNote(notes []padNote, id string) int {
	return slices.IndexFunc(notes, func(n padNote) bool { return n.ID == id })
}

// putBack puts a deleted note back: first when it was first, else after the
// note it came after, or before the one it came before when that one is
// gone too, or last when it was last; else at its place then.
func putBack(notes []padNote, n padNote) []padNote {
	at := min(max(n.At, 0), len(notes))
	i, j := indexNote(notes, n.Prev), indexNote(notes, n.Next)
	switch {
	case n.Prev == "":
		at = 0
	case i >= 0:
		at = i + 1
	case n.Next != "" && j >= 0:
		at = j
	case n.Next == "":
		at = len(notes)
	}
	n.Prev, n.Next, n.At, n.Deleted = "", "", 0, time.Time{}
	return slices.Insert(notes, at, n)
}

// checkNote refuses an id no note can have, and a text past the most a
// note holds. Nothing is ever cut short.
func checkNote(id, text string, needID bool) error {
	if id == "" && needID {
		return ipc.Errorf(ipc.CodeBadArgs, "Which note? It needs an id.")
	}
	if id != "" && !noteID.MatchString(id) {
		return ipc.Errorf(ipc.CodeBadArgs, "%q is not a note's id: up to 64 letters, digits, and _ . : -", id)
	}
	if len(text) > ipc.MaxNoteText {
		return ipc.Errorf(ipc.CodeBadArgs, "A note holds at most %d KB, and this one is %d KB, so nothing changed. Keep the longer text in a file, and the note to point at it.", ipc.MaxNoteText>>10, len(text)>>10)
	}
	return nil
}

// notepadOp lists the notes, or makes a change to them and writes it before
// it answers. A change that changes nothing, such as the same edit sent
// again, writes nothing.
func (c *Core) notepadOp(a ipc.NotepadArgs) (ipc.NotepadResult, error) {
	p := &c.pad
	p.mu.Lock()
	if a.Do == ipc.NotepadList {
		defer p.mu.Unlock()
		return c.notepadResult(""), nil
	}
	if p.readOnly {
		p.mu.Unlock()
		return ipc.NotepadResult{}, ipc.Errorf(ipc.CodeBusy, "Nothing changed: %s", p.problem)
	}
	notes, deleted := slices.Clone(p.notes), slices.Clone(p.deleted)
	now := p.clock().UTC().Truncate(time.Millisecond)
	left, err := changeNotes(&notes, &deleted, a, now)
	if err != nil {
		p.mu.Unlock()
		return ipc.NotepadResult{}, err
	}
	changed := !samePad(notes, p.notes) || !samePad(deleted, p.deleted)
	if changed {
		deleted = pruneDeleted(deleted, now)
		if err := c.writeNotepad(notes, deleted, p.rev+1); err != nil {
			p.mu.Unlock()
			c.log.Warn("notepad", "do", a.Do, "err", err)
			return ipc.NotepadResult{}, ipc.Errorf(ipc.CodeInternal, "Your notes could not be saved, so nothing changed: %v", err)
		}
		p.notes, p.deleted, p.rev = notes, deleted, p.rev+1
	}
	res := c.notepadResult(left)
	p.mu.Unlock()
	if changed {
		if a.Do != ipc.NotepadEdit {
			c.log.Info("notepad", "do", a.Do, "id", cmp.Or(a.ID, strings.Join(a.IDs, " ")), "notes", len(notes), "deleted", len(deleted))
		}
		c.publishNotepad()
	}
	return res, nil
}

// changeNotes makes the change a asks for to notes and deleted, and says
// which note an add or an edit left.
func changeNotes(notes, deleted *[]padNote, a ipc.NotepadArgs, now time.Time) (string, error) {
	switch a.Do {
	case ipc.NotepadAdd:
		if err := checkNote(a.ID, a.Text, false); err != nil {
			return "", err
		}
		id := a.ID
		if id == "" {
			id = newNoteID()
		}
		// The same add again, or once the note was deleted, leaves it be.
		if indexNote(*notes, id) >= 0 {
			return id, nil
		}
		if indexNote(*deleted, id) >= 0 {
			return "", nil
		}
		if len(*notes) >= ipc.MaxNotes {
			return "", ipc.Errorf(ipc.CodeBadArgs, "The notepad holds at most %d notes. Delete some you have done first.", ipc.MaxNotes)
		}
		at := len(*notes)
		if i := indexNote(*notes, a.After); a.After != "" && i >= 0 {
			at = i + 1
		}
		*notes = slices.Insert(*notes, at, padNote{Note: ipc.Note{ID: id, Text: a.Text, Created: now, Changed: now}})
		return id, nil
	case ipc.NotepadEdit:
		if err := checkNote(a.ID, a.Text, true); err != nil {
			return "", err
		}
		if i := indexNote(*notes, a.ID); i >= 0 {
			n := &(*notes)[i]
			if n.Text != a.Text {
				switch {
				case strings.TrimSpace(a.Text) != "":
					n.Was = ""
				case strings.TrimSpace(n.Text) != "":
					n.Was = n.Text
				}
				n.Text, n.Changed = a.Text, now
			}
			return a.ID, nil
		}
		if strings.TrimSpace(a.Text) == "" {
			return "", nil
		}
		// An edit that crossed a delete keeps what was typed: the note
		// comes back with it. One the core never had, as when the add was
		// lost on the way, is added.
		if len(*notes) >= ipc.MaxNotes {
			return "", ipc.Errorf(ipc.CodeBadArgs, "The notepad holds at most %d notes. Delete some you have done first.", ipc.MaxNotes)
		}
		if j := indexNote(*deleted, a.ID); j >= 0 {
			n := (*deleted)[j]
			*deleted = slices.Delete(*deleted, j, j+1)
			n.Text, n.Changed = a.Text, now
			*notes = putBack(*notes, n)
			return a.ID, nil
		}
		*notes = append(*notes, padNote{Note: ipc.Note{ID: a.ID, Text: a.Text, Created: now, Changed: now}})
		return a.ID, nil
	case ipc.NotepadCheck:
		if err := checkNote(a.ID, "", true); err != nil {
			return "", err
		}
		i := indexNote(*notes, a.ID)
		if i < 0 {
			return "", ipc.Errorf(ipc.CodeNotFound, "No note %q.", a.ID)
		}
		if (*notes)[i].Done != a.Done {
			(*notes)[i].Done, (*notes)[i].Changed = a.Done, now
		}
		return "", nil
	case ipc.NotepadMove:
		if err := checkNote(a.ID, "", true); err != nil {
			return "", err
		}
		i := indexNote(*notes, a.ID)
		if i < 0 {
			return "", ipc.Errorf(ipc.CodeNotFound, "No note %q.", a.ID)
		}
		n := (*notes)[i]
		rest := slices.Delete(slices.Clone(*notes), i, i+1)
		to := len(rest)
		if a.Before != "" {
			if to = indexNote(rest, a.Before); to < 0 {
				return "", ipc.Errorf(ipc.CodeNotFound, "No note %q to put it before.", a.Before)
			}
		}
		*notes = slices.Insert(rest, to, n)
		return "", nil
	case ipc.NotepadDelete:
		ids := a.IDs
		if a.ID != "" {
			ids = append(slices.Clone(ids), a.ID)
		}
		if len(ids) == 0 {
			return "", ipc.Errorf(ipc.CodeBadArgs, "Which notes? It needs an id.")
		}
		for _, id := range ids {
			i := indexNote(*notes, id)
			if i < 0 {
				continue // deleted already: the same delete again
			}
			n := (*notes)[i]
			*notes = slices.Delete(*notes, i, i+1)
			if strings.TrimSpace(n.Text) == "" {
				if n.Was == "" {
					continue // a line never written in: nothing to keep
				}
				n.Text = n.Was
			}
			n.Was = ""
			n.Deleted, n.At, n.Prev, n.Next = now, i, "", ""
			if i > 0 {
				n.Prev = (*notes)[i-1].ID
			}
			if i < len(*notes) {
				n.Next = (*notes)[i].ID
			}
			*deleted = slices.Insert(*deleted, 0, n)
		}
		return "", nil
	case ipc.NotepadRestore:
		want := map[string]bool{}
		for _, id := range append(slices.Clone(a.IDs), a.ID) {
			if id != "" {
				want[id] = true
			}
		}
		if len(want) == 0 {
			return "", ipc.Errorf(ipc.CodeBadArgs, "Which notes? It needs an id.")
		}
		// The last deleted first, so notes deleted together go back in
		// their order.
		for j := 0; j < len(*deleted); {
			n := (*deleted)[j]
			if !want[n.ID] {
				j++
				continue
			}
			if indexNote(*notes, n.ID) >= 0 {
				*deleted = slices.Delete(*deleted, j, j+1)
				continue
			}
			if len(*notes) >= ipc.MaxNotes {
				return "", ipc.Errorf(ipc.CodeBadArgs, "The notepad holds at most %d notes, so nothing came back. Delete some you have done first.", ipc.MaxNotes)
			}
			*deleted = slices.Delete(*deleted, j, j+1)
			n.Changed = now
			*notes = putBack(*notes, n)
		}
		return "", nil
	}
	return "", ipc.Errorf(ipc.CodeBadArgs, "unknown notepad do %q", a.Do)
}

// samePad says whether two lists hold the same notes, the same way.
func samePad(a, b []padNote) bool {
	return slices.EqualFunc(a, b, func(x, y padNote) bool { return x == y })
}

// notepadResult is the notepad as clients see it, with the note left
// named. The caller holds p.mu.
func (c *Core) notepadResult(left string) ipc.NotepadResult {
	res := ipc.NotepadResult{Notepad: c.notepadView()}
	for i := range res.Notes {
		if res.Notes[i].ID == left {
			n := res.Notes[i]
			res.Note = &n
		}
	}
	return res
}

// notepadView is the notepad as clients see it. The caller holds p.mu.
func (c *Core) notepadView() ipc.Notepad {
	p := &c.pad
	v := ipc.Notepad{Notes: make([]ipc.Note, 0, len(p.notes)), Deleted: make([]ipc.Note, 0, len(p.deleted)),
		Problem: p.problem, ReadOnly: p.readOnly, Path: c.paths.Notepad(), Rev: p.rev}
	for _, n := range p.notes {
		v.Notes = append(v.Notes, n.Note)
	}
	for _, n := range p.deleted {
		v.Deleted = append(v.Deleted, n.Note)
	}
	return v
}

// noteSent marks note id sent, as its input of text went through: ticked
// off, with when it went and the session it started or went to. A note the
// core never had, as when the client sent it before its add came, is added
// with the text, sent; the add, when it comes, then leaves it be. A note
// deleted meanwhile stays deleted.
func (c *Core) noteSent(id, text string, res ipc.InputResult) {
	p := &c.pad
	p.mu.Lock()
	i := indexNote(p.notes, id)
	if p.readOnly || i < 0 && (indexNote(p.deleted, id) >= 0 || !noteID.MatchString(id) || len(p.notes) >= ipc.MaxNotes || len(text) > ipc.MaxNoteText) {
		p.mu.Unlock()
		c.log.Info("notepad: a sent note is not there to mark", "id", id)
		return
	}
	notes := slices.Clone(p.notes)
	now := p.clock().UTC().Truncate(time.Millisecond)
	if i < 0 {
		notes = append(notes, padNote{Note: ipc.Note{ID: id, Text: text, Created: now}})
		i = len(notes) - 1
	}
	session := ""
	if res.Session != nil {
		session = res.Session.ID
	}
	notes[i].Done, notes[i].Sent, notes[i].Changed, notes[i].Session = true, now, now, session
	if err := c.writeNotepad(notes, p.deleted, p.rev+1); err != nil {
		p.mu.Unlock()
		c.log.Warn("notepad: marking a sent note", "err", err)
		return
	}
	p.notes, p.rev = notes, p.rev+1
	p.mu.Unlock()
	c.log.Info("notepad", "do", "sent", "id", id, "session", session)
	c.publishNotepad()
}

// publishNotepad tells every subscriber that sees the whole list that the
// notepad changed. The caller holds neither lock.
func (c *Core) publishNotepad() {
	c.mu.Lock()
	subs := slices.Collect(maps.Keys(c.subs))
	c.mu.Unlock()
	for _, s := range subs {
		s.notePad()
	}
}

// notepadFrame carries the notepad.
func (c *Core) notepadFrame() ipc.Frame {
	c.pad.mu.Lock()
	defer c.pad.mu.Unlock()
	v := c.notepadView()
	return ipc.Frame{T: ipc.FrameNotepad, Notepad: &v}
}
