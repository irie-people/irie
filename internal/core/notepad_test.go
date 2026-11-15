package core

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/tmux"
)

// padCore is a core with no tmux behind it, its files under home, with the
// notepad it finds there loaded, as a core that starts loads it.
func padCore(t *testing.T, home string, now func() time.Time) *Core {
	t.Helper()
	p, err := paths.New(func(k string) string { return map[string]string{"HOME": home}[k] }, 1000)
	if err != nil {
		t.Fatal(err)
	}
	c := newCore(Options{Config: config.Default(), Paths: p, Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{})
	c.pad.now = now
	c.loadNotepad()
	return c
}

func (c *Core) padDo(t *testing.T, a ipc.NotepadArgs) ipc.NotepadResult {
	t.Helper()
	r, err := c.notepadOp(a)
	if err != nil {
		t.Fatalf("notepad %+v: %v", a, err)
	}
	return r
}

func texts(notes []ipc.Note) []string {
	out := []string{}
	for _, n := range notes {
		mark := "[ ] "
		if n.Done {
			mark = "[x] "
		}
		out = append(out, mark+n.Text)
	}
	return out
}

// A day at a time from 9 Nov 2026, at noon.
type padDays struct{ n int }

func (d *padDays) now() time.Time { return time.Date(2026, 11, 9+d.n, 12, 0, 0, 0, time.Local) }

func TestTheNotepadKeepsItsNotesAcrossARestart(t *testing.T) {
	home := t.TempDir()
	c := padCore(t, home, nil)
	if r := c.padDo(t, ipc.NotepadArgs{}); len(r.Notes) != 0 || r.Problem != "" || r.ReadOnly {
		t.Fatalf("a new notepad: %+v", r)
	}
	milk := c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "6F1D2C3B-milk", Text: "Look at the flaky park test"}).Note
	if milk == nil || milk.ID != "6F1D2C3B-milk" || milk.Created.IsZero() {
		t.Fatalf("add with the client's id: %+v", milk)
	}
	core := c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, Text: "Rename the dog's notice"}).Note
	if core == nil || !noteID.MatchString(core.ID) || core.ID == milk.ID {
		t.Fatalf("add with an id of the core's: %+v", core)
	}
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, Text: "Write the phone's\nnotes view", After: milk.ID})
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadCheck, ID: core.ID, Done: true})
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadEdit, ID: milk.ID, Text: "Fix the flaky park test"})
	r := c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadMove, ID: core.ID, Before: milk.ID})
	want := []string{"[x] Rename the dog's notice", "[ ] Fix the flaky park test", "[ ] Write the phone's\nnotes view"}
	if got := texts(r.Notes); !slices.Equal(got, want) {
		t.Fatalf("notes = %q, want %q", got, want)
	}

	// The file is the person's alone, and whole.
	fi, err := os.Stat(c.paths.Notepad())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("notepad.json: %v, %v", fi, err)
	}
	if r.Path != c.paths.Notepad() || !strings.HasPrefix(r.Path, filepath.Join(home, ".local", "share")) {
		t.Errorf("path %q: the notes are data, not state", r.Path)
	}

	// A new core finds them as they were, and counts on from the changes
	// the file has had.
	again := padCore(t, home, nil)
	r2 := again.padDo(t, ipc.NotepadArgs{})
	if got := texts(r2.Notes); !slices.Equal(got, want) || r2.Rev != r.Rev || r.Rev != 6 {
		t.Errorf("after a restart: %q at rev %d, want %q at rev %d (6)", got, r2.Rev, want, r.Rev)
	}
}

// Each change is the same however often it is sent, so a client sends again
// whatever it is not sure went through, and nothing doubles or goes.
func TestNotepadChangesSentTwiceChangeOnce(t *testing.T) {
	c := padCore(t, t.TempDir(), nil)
	add := ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "a1", Text: "one"}
	c.padDo(t, add)
	if r := c.padDo(t, add); len(r.Notes) != 1 || r.Note == nil || r.Note.ID != "a1" {
		t.Fatalf("the same add twice: %+v", r)
	}
	edit := ipc.NotepadArgs{Do: ipc.NotepadEdit, ID: "a1", Text: "one, edited"}
	first := c.padDo(t, edit).Note.Changed
	before, _ := os.ReadFile(c.paths.Notepad())
	if r := c.padDo(t, edit); r.Note.Changed != first {
		t.Errorf("the same edit again changed the note: %v, then %v", first, r.Note.Changed)
	}
	if after, _ := os.ReadFile(c.paths.Notepad()); !bytes.Equal(before, after) {
		t.Errorf("the same edit again wrote the file")
	}
	del := ipc.NotepadArgs{Do: ipc.NotepadDelete, ID: "a1"}
	c.padDo(t, del)
	if r := c.padDo(t, del); len(r.Notes) != 0 || len(r.Deleted) != 1 {
		t.Errorf("the same delete twice: %+v", r)
	}
	// An add sent again once its note was deleted leaves it deleted.
	if r := c.padDo(t, add); len(r.Notes) != 0 || len(r.Deleted) != 1 {
		t.Errorf("an add sent again after the delete: %+v", r)
	}
	res := ipc.NotepadArgs{Do: ipc.NotepadRestore, ID: "a1"}
	c.padDo(t, res)
	if r := c.padDo(t, res); !slices.Equal(texts(r.Notes), []string{"[ ] one, edited"}) || len(r.Deleted) != 0 {
		t.Errorf("the same restore twice: %+v", r)
	}
}

func TestDeletedNotesGoBackWhereTheyWere(t *testing.T) {
	c := padCore(t, t.TempDir(), nil)
	for _, id := range []string{"a", "b", "c", "d"} {
		c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: id, Text: strings.ToUpper(id)})
	}
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "blank", Text: ""})
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadEdit, ID: "blank", Text: "  "})
	// Deleted together, as Delete Done Notes does; a line never written in
	// keeps nothing.
	r := c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadDelete, IDs: []string{"b", "c", "blank"}})
	if got := texts(r.Notes); !slices.Equal(got, []string{"[ ] A", "[ ] D"}) {
		t.Fatalf("after the delete: %q", got)
	}
	if got := texts(r.Deleted); !slices.Equal(got, []string{"[ ] C", "[ ] B"}) || r.Deleted[0].Deleted.IsZero() {
		t.Fatalf("recently deleted, the last first: %q", got)
	}
	r = c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadRestore, IDs: []string{"b", "c"}})
	if got := texts(r.Notes); !slices.Equal(got, []string{"[ ] A", "[ ] B", "[ ] C", "[ ] D"}) {
		t.Errorf("after the restore: %q", got)
	}
	if !r.Notes[1].Deleted.IsZero() {
		t.Errorf("a note put back still says it was deleted: %+v", r.Notes[1])
	}
	// The first goes back first, and one whose neighbour went too goes
	// back to its place.
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadDelete, ID: "a"})
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadDelete, ID: "c"})
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadDelete, ID: "b"})
	r = c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadRestore, ID: "c"})
	if got := texts(r.Notes); !slices.Equal(got, []string{"[ ] C", "[ ] D"}) {
		t.Errorf("c without its neighbour: %q", got)
	}
	r = c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadRestore, IDs: []string{"a", "b"}})
	if got := texts(r.Notes); !slices.Equal(got, []string{"[ ] A", "[ ] B", "[ ] C", "[ ] D"}) {
		t.Errorf("all back: %q", got)
	}
}

// What was typed is never lost: an edit that crossed a delete brings the
// note back with it, and one of a note the core never had adds it.
func TestAnEditKeepsWhatWasTyped(t *testing.T) {
	c := padCore(t, t.TempDir(), nil)
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "a", Text: "A"})
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "b", Text: "B"})
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadDelete, ID: "a"})
	r := c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadEdit, ID: "a", Text: "A, typed on"})
	if got := texts(r.Notes); !slices.Equal(got, []string{"[ ] A, typed on", "[ ] B"}) || len(r.Deleted) != 0 {
		t.Errorf("an edit of a deleted note: %q, deleted %d", got, len(r.Deleted))
	}
	r = c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadEdit, ID: "lost-add", Text: "C"})
	if got := texts(r.Notes); !slices.Equal(got, []string{"[ ] A, typed on", "[ ] B", "[ ] C"}) {
		t.Errorf("an edit of a note the core never had: %q", got)
	}
	// Checking or moving a note that is not there says so.
	if _, err := c.notepadOp(ipc.NotepadArgs{Do: ipc.NotepadCheck, ID: "nope", Done: true}); code(err) != ipc.CodeNotFound {
		t.Errorf("check of no note: %v", err)
	}
	// A note emptied and then taken away keeps its words in Recently
	// Deleted, as a select-all and two Backspaces would leave it.
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "emptied", Text: "words to keep"})
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadEdit, ID: "emptied", Text: ""})
	r = c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadDelete, ID: "emptied"})
	if len(r.Deleted) != 1 || r.Deleted[0].Text != "words to keep" {
		t.Errorf("an emptied note deleted: %+v", r.Deleted)
	}
	r = c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadRestore, ID: "emptied"})
	if got := texts(r.Notes); got[len(got)-1] != "[ ] words to keep" {
		t.Errorf("the emptied note restored: %q", got)
	}
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadDelete, ID: "emptied"})
	// An empty note is a line to type in, and deleted still empty it keeps
	// nothing.
	if r := c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "line", Text: "", After: "a"}); len(r.Notes) != 4 || r.Notes[1].ID != "line" {
		t.Errorf("an empty add after a: %+v", r.Notes)
	}
	if r := c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadDelete, ID: "line"}); len(r.Notes) != 3 || slices.ContainsFunc(r.Deleted, func(n ipc.Note) bool { return n.ID == "line" }) {
		t.Errorf("an empty note deleted: %+v", r)
	}
	if _, err := c.notepadOp(ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "../x", Text: "x"}); code(err) != ipc.CodeBadArgs {
		t.Errorf("a bad id: %v", err)
	}
	long := strings.Repeat("x", ipc.MaxNoteText+1)
	if _, err := c.notepadOp(ipc.NotepadArgs{Do: ipc.NotepadEdit, ID: "a", Text: long}); code(err) != ipc.CodeBadArgs {
		t.Errorf("a note past the most: %v", err)
	}
	if got := texts(c.padDo(t, ipc.NotepadArgs{}).Notes); got[0] != "[ ] A, typed on" {
		t.Errorf("a refused edit changed the note: %q", got)
	}
}

// A file that does not read is moved aside, never written over, and the
// notes come from the newest daily copy.
func TestABadNotepadFileComesBackFromItsCopy(t *testing.T) {
	home := t.TempDir()
	d := &padDays{}
	c := padCore(t, home, d.now)
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "a", Text: "Monday's note"})
	if copies := c.notepadCopyFiles(); len(copies) != 0 {
		t.Errorf("a copy of no file: %q", copies)
	}
	d.n = 1
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "b", Text: "Tuesday's note"})
	copies := c.notepadCopyFiles()
	if len(copies) != 1 || filepath.Base(copies[0]) != "2026-11-10.json" {
		t.Fatalf("copies: %q", copies)
	}
	// A second change the same day keeps the day's copy as it stood.
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadCheck, ID: "b", Done: true})
	if f, err := readNotepad(copies[0]); err != nil || len(f.Notes) != 1 {
		t.Errorf("the day's copy: %+v, %v", f, err)
	}

	bad := []byte(`{"version": 1, "notes": [{"id": "a", "te`)
	if err := os.WriteFile(c.paths.Notepad(), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	again := padCore(t, home, d.now)
	r := again.padDo(t, ipc.NotepadArgs{})
	if got := texts(r.Notes); !slices.Equal(got, []string{"[ ] Monday's note"}) {
		t.Errorf("notes from the copy: %q", got)
	}
	aside := c.paths.Notepad() + ".bad-" + d.now().UTC().Format("20060102T150405Z")
	if kept, err := os.ReadFile(aside); err != nil || !bytes.Equal(kept, bad) {
		t.Errorf("the file that did not read: %q, %v", kept, err)
	}
	if !strings.Contains(r.Problem, "2026-11-10") || !strings.Contains(r.Problem, aside) || r.ReadOnly {
		t.Errorf("problem %q, read only %v", r.Problem, r.ReadOnly)
	}
	// The copy is written back whole, and the notes change again.
	if f, err := readNotepad(c.paths.Notepad()); err != nil || len(f.Notes) != 1 {
		t.Errorf("notepad.json after the recovery: %+v, %v", f, err)
	}
	again.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, Text: "after"})
}

// A file of a newer version, as after going back to an older build, is
// shown and never written.
func TestANewerNotepadFileIsOnlyShown(t *testing.T) {
	home := t.TempDir()
	c := padCore(t, home, nil)
	newer := []byte(`{"version": 2, "notes": [{"id": "a", "text": "from a newer core", "colour": "sun"}], "deleted": []}` + "\n")
	if err := os.MkdirAll(filepath.Dir(c.paths.Notepad()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.paths.Notepad(), newer, 0o600); err != nil {
		t.Fatal(err)
	}
	c = padCore(t, home, nil)
	r := c.padDo(t, ipc.NotepadArgs{})
	if !r.ReadOnly || r.Problem == "" || !slices.Equal(texts(r.Notes), []string{"[ ] from a newer core"}) {
		t.Fatalf("a newer file: %+v", r)
	}
	if _, err := c.notepadOp(ipc.NotepadArgs{Do: ipc.NotepadAdd, Text: "x"}); code(err) != ipc.CodeBusy {
		t.Errorf("an add to a newer file: %v", err)
	}
	c.noteSent("a", "from a newer core", ipc.InputResult{Did: ipc.InputSpawned})
	if b, _ := os.ReadFile(c.paths.Notepad()); !bytes.Equal(b, newer) {
		t.Errorf("the newer file was written: %s", b)
	}
}

func TestTheNotepadKeepsThirtyDaysOfCopiesAndDeletedNotes(t *testing.T) {
	d := &padDays{}
	c := padCore(t, t.TempDir(), d.now)
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "old", Text: "deleted on day 0"})
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadDelete, ID: "old"})
	for d.n = 1; d.n <= 35; d.n++ {
		c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, Text: "day"})
	}
	copies := c.notepadCopyFiles()
	if len(copies) != notepadCopies || filepath.Base(copies[0]) != "2026-12-14.json" || filepath.Base(copies[len(copies)-1]) != "2026-11-15.json" {
		t.Errorf("%d copies, %s to %s", len(copies), filepath.Base(copies[len(copies)-1]), filepath.Base(copies[0]))
	}
	if r := c.padDo(t, ipc.NotepadArgs{}); len(r.Deleted) != 0 {
		t.Errorf("a note deleted 35 days ago is still kept: %+v", r.Deleted)
	}
}

// The notepad comes to each client after its first snapshot, then again
// whenever it changes.
func TestNotepadFramesComeAfterTheSnapshotAndEachChange(t *testing.T) {
	c := padCore(t, t.TempDir(), nil)
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "a", Text: "first"})
	next := frames(t, c)
	pad := func(why string) ipc.Notepad {
		t.Helper()
		for {
			f, ok := next(5 * time.Second)
			if !ok {
				t.Fatalf("no notepad frame %s", why)
			}
			if f.T == ipc.FrameNotepad {
				return *f.Notepad
			}
		}
	}
	if n := pad("at the start"); !slices.Equal(texts(n.Notes), []string{"[ ] first"}) {
		t.Errorf("the first frame: %+v", n)
	}
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadCheck, ID: "a", Done: true})
	if n := pad("after a check"); !slices.Equal(texts(n.Notes), []string{"[x] first"}) {
		t.Errorf("after a check: %+v", n)
	}
	// A sent note is ticked off and says when it went, and where.
	c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "b", Text: "second"})
	pad("after an add")
	c.noteSent("b", "second", ipc.InputResult{Did: ipc.InputSpawned, Session: &ipc.Session{ID: "s0000000000ab"}})
	n := pad("after a send")
	if b := n.Notes[1]; !b.Done || b.Sent.IsZero() || b.Session != "s0000000000ab" {
		t.Errorf("a sent note: %+v", b)
	}
	// A note sent before its add came is taken in from the text that went,
	// and the add, when it comes, leaves it be.
	c.noteSent("early", "sent before its add", ipc.InputResult{Did: ipc.InputSpawned})
	n = pad("after a send of a note the core never had")
	if e := n.Notes[2]; e.ID != "early" || e.Text != "sent before its add" || !e.Done || e.Sent.IsZero() {
		t.Errorf("a note sent before its add: %+v", e)
	}
	if r := c.padDo(t, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "early", Text: "sent befo"}); len(r.Notes) != 3 || r.Notes[2].Text != "sent before its add" {
		t.Errorf("its add, late: %+v", r.Notes)
	}
}

// A note sent from the notepad starts its task as if typed in the task bar,
// and is ticked off with the session it started; the notes outlive the
// core.
func TestANoteSentAsATaskIsTickedOff(t *testing.T) {
	c := setup(t)
	var r ipc.NotepadResult
	if err := c.call(ipc.OpNotepad, ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "n1", Text: "task from a note"}, &r); err != nil {
		t.Fatal(err)
	}
	in, err := c.input(ipc.InputArgs{Text: "task from a note", Note: "n1"})
	if err != nil || in.Did != ipc.InputSpawned || in.Session == nil {
		t.Fatalf("input: %+v, %v", in, err)
	}
	testutil.Eventually(t, 5*time.Second, "the note was not ticked off", func() bool {
		r = ipc.NotepadResult{}
		return c.call(ipc.OpNotepad, ipc.NotepadArgs{}, &r) == nil && len(r.Notes) == 1 && r.Notes[0].Done
	})
	if n := r.Notes[0]; n.Session != in.Session.ID || n.Sent.IsZero() {
		t.Errorf("the sent note: %+v", n)
	}
	c.restartCore()
	r = ipc.NotepadResult{}
	if err := c.call(ipc.OpNotepad, ipc.NotepadArgs{}, &r); err != nil || len(r.Notes) != 1 || r.Notes[0].Session != in.Session.ID {
		t.Errorf("after a restart: %+v, %v", r, err)
	}
	var h ipc.HelloResult
	if err := c.call(ipc.OpHello, ipc.HelloArgs{Client: "test", API: ipc.API}, &h); err != nil || !slices.Contains(h.Features, ipc.OpNotepad) {
		t.Errorf("hello features %v, %v", h.Features, err)
	}
}
