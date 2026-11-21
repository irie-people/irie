//go:build !noobsidian

package obsidian

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

func texts(d *Doc) []string {
	var out []string
	for _, t := range d.Tasks() {
		mark := " "
		if t.Done {
			mark = "x"
		}
		out = append(out, fmt.Sprintf("[%s] %q ^%s", mark, t.Text, t.Block))
	}
	return out
}

// A task is a task list's line at the top level, with the lines indented
// under it and its block identifier at its end; properties, fences of
// code and comments hold none, and every other line is kept as it is.
func TestParseFindsTheTasks(t *testing.T) {
	note := strings.Join([]string{
		"---",
		"tags: [irie]",
		"- [ ] not a task: a property",
		"---",
		"# Notes",
		"",
		"- [ ] Call the printer ^iaaaaaa",
		"- [x] Draft the launch post",
		"  with the reel ^launch-1",
		"* [X] starred",
		"+ [/] in hand",
		"1. [ ] numbered",
		"- [ ]",
		"- [ ] two",
		"",
		"  lines apart",
		"",
		"Some words of the person's.",
		"```",
		"- [ ] code, not a task",
		"```",
		"%% a comment",
		"- [ ] hidden",
		"%%",
		"> - [ ] quoted",
		"- [x]no space: no task",
		"- [ ] caret 2^10 kept ^iabcdef",
		"",
	}, "\n")
	d := Parse(note)
	want := []string{
		`[ ] "Call the printer" ^iaaaaaa`,
		`[x] "Draft the launch post\nwith the reel" ^launch-1`,
		`[x] "starred" ^`,
		`[x] "in hand" ^`,
		`[ ] "numbered" ^`,
		`[ ] "" ^`,
		`[ ] "two\n\nlines apart" ^`,
		`[ ] "caret 2^10 kept" ^iabcdef`,
	}
	if got := texts(d); !slices.Equal(got, want) {
		t.Fatalf("tasks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// No task is a note of the notepad's yet, so all stay as written.
	if got, _ := d.Render(nil); got != note {
		t.Fatalf("rendered with no notes:\n%s", got)
	}
}

func TestParseKeepsLineBreaksAndMarks(t *testing.T) {
	for _, note := range []string{
		"- [ ] a ^iaaaaaa\r\n- [x] b ^ibbbbbb\r\n",
		"\ufeff- [ ] a ^iaaaaaa\n",
		"- [ ] no break at the end ^iaaaaaa",
		"",
		"\n",
		"words\n\n\n",
	} {
		d := Parse(note)
		for _, task := range d.Tasks() {
			task.ID = "x" + task.Block
		}
		var notes []Note
		for _, task := range d.Tasks() {
			notes = append(notes, Note{ID: task.ID, Text: task.Text, Done: task.Done})
		}
		if got, _ := d.Render(notes); got != note {
			t.Errorf("Render(Parse(%q)) = %q", note, got)
		}
	}
}

func TestMarkIsSteadyAndShort(t *testing.T) {
	a, b := Mark("3F2A9C1B-0000-4000-8000-000000000001"), Mark("n0123456789ab")
	if a != Mark("3F2A9C1B-0000-4000-8000-000000000001") || a == b {
		t.Fatalf("marks %q %q", a, b)
	}
	for _, m := range []string{a, b} {
		if !irieMark.MatchString(m) {
			t.Errorf("mark %q isn't Irie's shape", m)
		}
	}
}

// pad is a notepad in memory with the core's rules for each change, enough
// for a merge's plan to land on (internal/core tests the real one).
type pad struct {
	notes, deleted []Note
}

func (p *pad) at(id string) int {
	return slices.IndexFunc(p.notes, func(n Note) bool { return n.ID == id })
}

func (p *pad) apply(t *testing.T, c Change) {
	t.Helper()
	switch c.Do {
	case ipc.NotepadAdd:
		if p.at(c.ID) >= 0 {
			return
		}
		at := len(p.notes)
		if i := p.at(c.After); c.After != "" && i >= 0 {
			at = i + 1
		}
		p.notes = slices.Insert(p.notes, at, Note{ID: c.ID, Text: c.Text})
	case ipc.NotepadEdit:
		if i := p.at(c.ID); i >= 0 {
			p.notes[i].Text = c.Text
			return
		}
		if j := slices.IndexFunc(p.deleted, func(n Note) bool { return n.ID == c.ID }); j >= 0 {
			n := p.deleted[j]
			p.deleted = slices.Delete(p.deleted, j, j+1)
			n.Text = c.Text
			p.notes = append(p.notes, n)
		}
	case ipc.NotepadCheck:
		if i := p.at(c.ID); i >= 0 {
			p.notes[i].Done = c.Done
		}
	case ipc.NotepadMove:
		i := p.at(c.ID)
		if i < 0 {
			t.Fatalf("move of %s, which isn't there", c.ID)
		}
		n := p.notes[i]
		rest := slices.Delete(slices.Clone(p.notes), i, i+1)
		to := len(rest)
		if c.Before != "" {
			to = slices.IndexFunc(rest, func(m Note) bool { return m.ID == c.Before })
			if to < 0 {
				t.Fatalf("move of %s before %s, which isn't there", c.ID, c.Before)
			}
		}
		p.notes = slices.Insert(rest, to, n)
	case ipc.NotepadDelete:
		for _, id := range c.IDs {
			if i := p.at(id); i >= 0 {
				p.deleted = append(p.deleted, p.notes[i])
				p.notes = slices.Delete(p.notes, i, i+1)
			}
		}
	default:
		t.Fatalf("change %q", c.Do)
	}
}

// sync is one round of the core's: the note's text merged into the
// notepad, then the note written from it. It gives the note's new text
// and what the next merge weighs it against.
func (p *pad) sync(t *testing.T, text string, base []Synced, ids *int) (string, []Synced, Plan) {
	t.Helper()
	d := Parse(text)
	plan := Merge(d, base, p.notes, p.deleted, func() string { *ids++; return fmt.Sprintf("n%03d", *ids) })
	for _, c := range plan.Changes {
		p.apply(t, c)
	}
	out, named := d.Render(p.notes)
	d2 := Parse(out)
	d2.Name(named)
	return out, d2.Synced(), plan
}

func notesOf(p *pad) []string {
	var out []string
	for _, n := range p.notes {
		mark := " "
		if n.Done {
			mark = "x"
		}
		out = append(out, "["+mark+"] "+n.Text)
	}
	return out
}

// A new notepad's note: its notes after the line that says what it is,
// each with its mark; then what Obsidian, or anyone, does to the note
// comes back to the notepad, and the notepad's changes go to the note,
// with the person's own lines kept where they wrote them.
func TestTheNoteAndTheNotepadKeepInStep(t *testing.T) {
	p := &pad{notes: []Note{{ID: "a", Text: "Call the printer"}, {ID: "b", Text: "Draft the launch post", Done: true}, {ID: "c", Text: "Book flights"}}}
	ids := 0
	d := Fresh("%% Irie keeps your notes here %%")
	text, ns := d.Render(p.notes)
	want := "%% Irie keeps your notes here %%\n\n- [ ] Call the printer ^" + Mark("a") + "\n- [x] Draft the launch post ^" + Mark("b") + "\n- [ ] Book flights ^" + Mark("c") + "\n"
	if text != want {
		t.Fatalf("fresh note:\n%s\nwant:\n%s", text, want)
	}
	d2 := Parse(text)
	d2.Name(ns)
	base := d2.Synced()

	// Obsidian ticks one off, changes another's words, adds a line under
	// the first, and the person writes a heading of their own.
	edited := strings.NewReplacer(
		"- [ ] Call the printer ^"+Mark("a"), "## Today\n- [x] Call the printer ^"+Mark("a")+"\n- [ ] Water the plants",
		"Book flights", "Book flights to Lisbon",
	).Replace(text)
	text, base, plan := p.sync(t, edited, base, &ids)
	if plan.Held != 0 {
		t.Fatalf("held %d", plan.Held)
	}
	if got, want := notesOf(p), []string{"[x] Call the printer", "[ ] Water the plants", "[x] Draft the launch post", "[ ] Book flights to Lisbon"}; !slices.Equal(got, want) {
		t.Fatalf("notepad after the note changed: %q, want %q", got, want)
	}
	wantText := "%% Irie keeps your notes here %%\n\n## Today\n- [x] Call the printer ^" + Mark("a") + "\n- [ ] Water the plants ^" + Mark("n001") +
		"\n- [x] Draft the launch post ^" + Mark("b") + "\n- [ ] Book flights to Lisbon ^" + Mark("c") + "\n"
	if text != wantText {
		t.Fatalf("note after:\n%s\nwant:\n%s", text, wantText)
	}

	// The notepad deletes one, ticks another back on, moves the last up
	// and adds a note at its end.
	p.apply(t, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadDelete, IDs: []string{"b"}}})
	p.apply(t, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadCheck, ID: "a", Done: false}})
	p.apply(t, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadMove, ID: "c", Before: "a"}})
	p.apply(t, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: "d", Text: "Two lines\nin one note"}})
	text, base, plan = p.sync(t, text, base, &ids)
	if len(plan.Changes) != 0 {
		t.Fatalf("an unchanged note asked for %v", plan.Changes)
	}
	wantText = "%% Irie keeps your notes here %%\n\n## Today\n- [ ] Book flights to Lisbon ^" + Mark("c") + "\n- [ ] Call the printer ^" + Mark("a") +
		"\n- [ ] Water the plants ^" + Mark("n001") + "\n- [ ] Two lines\n  in one note ^" + Mark("d") + "\n"
	if text != wantText {
		t.Fatalf("note after the notepad changed:\n%s\nwant:\n%s", text, wantText)
	}

	// The note's order changes: the notepad takes it.
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	lines[3], lines[4] = lines[4], lines[3]
	text, _, _ = p.sync(t, strings.Join(lines, "\n")+"\n", base, &ids)
	if got, want := notesOf(p), []string{"[ ] Call the printer", "[ ] Book flights to Lisbon", "[ ] Water the plants", "[ ] Two lines\nin one note"}; !slices.Equal(got, want) {
		t.Fatalf("notepad after a move in the note: %q, want %q", got, want)
	}
	if !strings.Contains(text, "## Today\n- [ ] Call the printer") {
		t.Fatalf("note after a move:\n%s", text)
	}
}

// A line deleted in the note goes to Recently Deleted; many gone at once
// with none left, as from a file emptied by a slip or a sync, are held and
// written back.
func TestTasksGoneFromTheNote(t *testing.T) {
	mk := func(n int) *pad {
		p := &pad{}
		for i := range n {
			p.notes = append(p.notes, Note{ID: fmt.Sprintf("n%d", i), Text: fmt.Sprintf("note %d", i)})
		}
		return p
	}
	start := func(p *pad) (string, []Synced) {
		text, ns := Fresh("").Render(p.notes)
		d := Parse(text)
		d.Name(ns)
		return text, d.Synced()
	}
	ids := 100

	p := mk(5)
	text, base := start(p)
	lines := strings.Split(text, "\n")
	text, _, plan := p.sync(t, strings.Join(slices.Delete(lines, 1, 2), "\n"), base, &ids)
	if plan.Held != 0 || len(p.notes) != 4 || len(p.deleted) != 1 || p.deleted[0].ID != "n1" {
		t.Fatalf("one line deleted: held %d, notes %v, deleted %v", plan.Held, p.notes, p.deleted)
	}
	if strings.Contains(text, "note 1") {
		t.Fatalf("the deleted line came back:\n%s", text)
	}

	for _, emptied := range []string{"", "\n", "# Notes\n"} {
		p = mk(5)
		_, base = start(p)
		text, _, plan = p.sync(t, emptied, base, &ids)
		if plan.Held != 5 || len(p.notes) != 5 || len(p.deleted) != 0 {
			t.Fatalf("emptied to %q: held %d, notes %v", emptied, plan.Held, p.notes)
		}
		for i := range 5 {
			if !strings.Contains(text, fmt.Sprintf("- [ ] note %d ^", i)) {
				t.Fatalf("emptied to %q, the notes weren't written back:\n%s", emptied, text)
			}
		}
	}

	// Three may go together, as a person deletes the done ones.
	p = mk(3)
	_, base = start(p)
	_, _, plan = p.sync(t, "", base, &ids)
	if plan.Held != 0 || len(p.notes) != 0 || len(p.deleted) != 3 {
		t.Fatalf("three gone: held %d, notes %v", plan.Held, p.notes)
	}
	// So may most of many, while some stay.
	p = mk(12)
	text, base = start(p)
	lines = strings.Split(text, "\n")
	_, _, plan = p.sync(t, strings.Join(lines[9:], "\n"), base, &ids)
	if plan.Held != 0 || len(p.notes) != 3 {
		t.Fatalf("nine of twelve gone: held %d, %d notes", plan.Held, len(p.notes))
	}
}

// With nothing to weigh the note against, the first time or after the
// core's own record was lost, the notepad's words and ticks stand, a task
// with no note is a new one, and nothing is deleted.
func TestTheFirstMergeTakesThePersonsTasks(t *testing.T) {
	p := &pad{notes: []Note{{ID: "a", Text: "From Irie"}}}
	ids := 0
	text, _, plan := p.sync(t, "# My list\n- [ ] Mine ^call-bob\n- [x] Done of mine\n- [ ] Stale words ^"+Mark("a")+"\n", nil, &ids)
	if got, want := notesOf(p), []string{"[ ] Mine", "[x] Done of mine", "[ ] From Irie"}; !slices.Equal(got, want) {
		t.Fatalf("notes: %q, want %q (plan %v)", got, want, plan.Changes)
	}
	want := "# My list\n- [ ] Mine ^call-bob\n- [x] Done of mine ^" + Mark("n002") + "\n- [ ] From Irie ^" + Mark("a") + "\n"
	if text != want {
		t.Fatalf("note:\n%s\nwant:\n%s", text, want)
	}
}

// The person's own block identifier stays the task's mark, and the task
// is found by it the next time.
func TestThePersonsOwnBlockIdentifier(t *testing.T) {
	p := &pad{}
	ids := 0
	text, base, _ := p.sync(t, "- [ ] Mine ^call-bob\n", nil, &ids)
	text = strings.Replace(text, "Mine", "Mine, changed", 1)
	text, _, plan := p.sync(t, text, base, &ids)
	if len(p.notes) != 1 || p.notes[0].Text != "Mine, changed" || text != "- [ ] Mine, changed ^call-bob\n" {
		t.Fatalf("notes %v, note %q, plan %v", p.notes, text, plan.Changes)
	}
}

// A line typed since the last write has no mark yet; it is found by its
// words, and while it is still being typed, by its place.
func TestALineBeingTypedIsOneNote(t *testing.T) {
	p := &pad{notes: []Note{{ID: "a", Text: "First"}}}
	ids := 0
	text, base := func() (string, []Synced) {
		text, ns := Fresh("").Render(p.notes)
		d := Parse(text)
		d.Name(ns)
		return text, d.Synced()
	}()
	typed := text + "- [ ] Wat"
	d := Parse(typed)
	plan := Merge(d, base, p.notes, nil, func() string { ids++; return fmt.Sprintf("n%03d", ids) })
	for _, c := range plan.Changes {
		p.apply(t, c)
	}
	// Not written yet: the core waits until the note is still.
	base = d.Synced()
	for _, more := range []string{"- [ ] Water", "- [ ] Water the", "- [ ] Water the plants"} {
		d = Parse(text + more)
		plan = Merge(d, base, p.notes, p.deleted, func() string { ids++; return fmt.Sprintf("n%03d", ids) })
		for _, c := range plan.Changes {
			p.apply(t, c)
		}
		base = d.Synced()
	}
	if got, want := notesOf(p), []string{"[ ] First", "[ ] Water the plants"}; !slices.Equal(got, want) || len(p.deleted) != 0 {
		t.Fatalf("notes %q, deleted %v; want %q", got, p.deleted, want)
	}
}

// A note deleted in the notepad goes from the note, unless the note
// changed it since: then what it says brings it back.
func TestADeletedNoteChangedInTheNoteComesBack(t *testing.T) {
	p := &pad{notes: []Note{{ID: "a", Text: "Keep"}, {ID: "b", Text: "Gone"}}}
	ids := 0
	text, ns := Fresh("").Render(p.notes)
	d := Parse(text)
	d.Name(ns)
	base := d.Synced()
	p.apply(t, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadDelete, IDs: []string{"b"}}})
	out, _, _ := p.sync(t, text, base, &ids)
	if strings.Contains(out, "Gone") {
		t.Fatalf("a note deleted in the notepad stayed in the note:\n%s", out)
	}
	p = &pad{notes: []Note{{ID: "a", Text: "Keep"}}, deleted: []Note{{ID: "b", Text: "Gone"}}}
	out, _, _ = p.sync(t, strings.Replace(text, "Gone", "Gone, but wanted", 1), base, &ids)
	if got, want := notesOf(p), []string{"[ ] Keep", "[ ] Gone, but wanted"}; !slices.Equal(got, want) || !strings.Contains(out, "Gone, but wanted ^"+Mark("b")) {
		t.Fatalf("notes %q, note:\n%s", got, out)
	}
}

// A line copied whole, its mark and all, is a note of its own.
func TestACopiedLineIsANewNote(t *testing.T) {
	p := &pad{notes: []Note{{ID: "a", Text: "Twice"}}}
	ids := 0
	text, ns := Fresh("").Render(p.notes)
	d := Parse(text)
	d.Name(ns)
	out, _, _ := p.sync(t, text+text, d.Synced(), &ids)
	if len(p.notes) != 2 || p.notes[1].ID != "n001" {
		t.Fatalf("notes %v", p.notes)
	}
	if strings.Count(out, "^"+Mark("a")) != 1 || !strings.Contains(out, "^"+Mark("n001")) {
		t.Fatalf("note:\n%s", out)
	}
}

// Whatever the notes and whatever the person does to the note, a note just
// written and read back asks for nothing, its tasks line up with the notes
// they are, and every word the person wrote that is no task stays.
func TestRandomNotesRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	words := []string{"call", "Bob", "^2", "%%", "```", "- [ ] inner", "  spaced", "", "#tag", "[[Link]]", "x ^abc", "\t tab", "~~~", "==hi=="}
	for round := range 400 {
		p := &pad{}
		for i := range r.IntN(8) {
			var lines []string
			for range 1 + r.IntN(3) {
				lines = append(lines, words[r.IntN(len(words))]+" "+words[r.IntN(len(words))])
			}
			p.notes = append(p.notes, Note{ID: fmt.Sprintf("r%d-%d", round, i), Text: strings.Join(lines, "\n"), Done: r.IntN(2) == 0})
		}
		around := []string{"# Head", "words of mine", "", "---", "> quote", "```", "%%", "- plain item"}
		d := Fresh(around[r.IntN(len(around))])
		text, ns := d.Render(p.notes)
		d2 := Parse(text)
		if len(d2.Tasks()) != len(ns) {
			t.Fatalf("round %d: %d tasks for %d notes in:\n%s", round, len(d2.Tasks()), len(ns), text)
		}
		d2.Name(ns)
		for i, task := range d2.Tasks() {
			n := p.notes[slices.IndexFunc(p.notes, func(n Note) bool { return n.ID == ns[i] })]
			if task.Text != Clean(n.Text) || task.Done != n.Done {
				t.Fatalf("round %d: task %q (done %v) for note %q (done %v) in:\n%s", round, task.Text, task.Done, n.Text, n.Done, text)
			}
		}
		base := d2.Synced()
		ids := 0
		again, _, plan := p.sync(t, text, base, &ids)
		if len(plan.Changes) != 0 || again != text {
			t.Fatalf("round %d: a note just written asked for %v, and came out as:\n%s\nfrom:\n%s", round, plan.Changes, again, text)
		}
	}
}

// Whatever the person does to the note between syncs, no line of theirs
// that is no task is lost, every task with words is a note after it, no
// note goes but to Recently Deleted, and a second sync finds nothing to do.
func TestRandomEditsSettle(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	words := []string{"call", "Bob", "plants", "Lisbon", "#tag", "[[Note]]", "x", "y ^own", "%% c %%", "2^3"}
	say := func() string { return words[r.IntN(len(words))] + " " + words[r.IntN(len(words))] }
	ids := 0
	for round := range 300 {
		p := &pad{}
		for i := range r.IntN(7) {
			p.notes = append(p.notes, Note{ID: fmt.Sprintf("s%d-%d", round, i), Text: say(), Done: r.IntN(3) == 0})
		}
		text, ns := Fresh("%% intro %%").Render(p.notes)
		d := Parse(text)
		d.Name(ns)
		base := d.Synced()
		for step := range 6 {
			lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
			for range 1 + r.IntN(3) {
				i := r.IntN(len(lines) + 1)
				switch r.IntN(7) {
				case 0: // a new task
					lines = slices.Insert(lines, i, "- [ ] "+say())
				case 1: // words of the person's own
					lines = slices.Insert(lines, i, "Prose "+say())
				case 2: // a line deleted
					if i < len(lines) {
						lines = slices.Delete(lines, i, i+1)
					}
				case 3: // two lines swapped
					if i+1 < len(lines) {
						lines[i], lines[i+1] = lines[i+1], lines[i]
					}
				case 4: // a task's words changed
					if i < len(lines) && strings.HasPrefix(lines[i], "- [") {
						lines[i] = strings.Replace(lines[i], "] ", "] "+say()+" ", 1)
					}
				case 5: // a task ticked, or on again
					if i < len(lines) && strings.HasPrefix(lines[i], "- [ ]") {
						lines[i] = "- [x]" + lines[i][5:]
					} else if i < len(lines) && strings.HasPrefix(lines[i], "- [x]") {
						lines[i] = "- [ ]" + lines[i][5:]
					}
				case 6: // the notepad changes
					if len(p.notes) > 0 {
						n := r.IntN(len(p.notes))
						p.notes[n].Done = !p.notes[n].Done
					}
					ids++
					p.notes = append(p.notes, Note{ID: fmt.Sprintf("p%d", ids), Text: say()})
				}
			}
			edited := strings.Join(lines, "\n") + "\n"
			before := map[string]bool{}
			for _, n := range p.notes {
				before[n.ID] = true
			}
			out, nextBase, _ := p.sync(t, edited, base, &ids)
			for _, l := range lines {
				if strings.HasPrefix(l, "Prose") && !strings.Contains(out, l) {
					t.Fatalf("round %d step %d: the person's line %q went:\n%s\nfrom:\n%s", round, step, l, out, edited)
				}
			}
			have := map[string]bool{}
			for _, n := range p.notes {
				have[Clean(n.Text)] = true
			}
			for _, task := range Parse(edited).Tasks() {
				if strings.TrimSpace(task.Text) != "" && !have[task.Text] {
					t.Fatalf("round %d step %d: the task %q is no note: %q\nfrom:\n%s", round, step, task.Text, notesOf(p), edited)
				}
			}
			for id := range before {
				if p.at(id) < 0 && !slices.ContainsFunc(p.deleted, func(n Note) bool { return n.ID == id }) {
					t.Fatalf("round %d step %d: note %s went nowhere", round, step, id)
				}
			}
			again, _, plan := p.sync(t, out, nextBase, &ids)
			if len(plan.Changes) != 0 || again != out {
				t.Fatalf("round %d step %d: a second sync asked for %v:\n%s\nthen:\n%s", round, step, plan.Changes, out, again)
			}
			text, base = out, nextBase
		}
	}
}
