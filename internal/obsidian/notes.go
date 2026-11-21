//go:build !noobsidian

// Package obsidian is the obsidian plugin's own code (DESIGN §4.17): the
// notepad (DESIGN §4.20) kept as a note in an Obsidian vault, both ways.
//
// A vault is a folder of Markdown notes that any program may read and
// write, and Obsidian keeps up with changes made outside it (Obsidian's
// help, "How Obsidian stores data"). The notes note is a task list in one
// of them: each task a note of the notepad, "- [ ] words", ticked off as
// "- [x] words", and marked at its end by a block identifier ("Internal
// links": a caret and letters, digits and dashes): Irie's own, i and six
// letters and digits of the note's id, or one the person gave the task.
// The marks are how each side finds a note again whatever is done to the
// text around it; every line that is no task stays as it was written.
package obsidian

import (
	"crypto/sha256"
	"encoding/base32"
	"regexp"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/ipc"
)

// Note is a note of the notepad, as the notes note shows it.
type Note struct {
	ID   string
	Text string
	Done bool
}

// Synced is a task of the notes note as it stood when the note and the
// notepad last agreed: the note it is, and its block identifier, words and
// tick then. The next merge weighs the note against these, so it can tell
// what the note changed from what the notepad did.
type Synced struct {
	ID    string `json:"id"`
	Block string `json:"block,omitempty"`
	Text  string `json:"text"`
	Done  bool   `json:"done,omitempty"`
}

// Task is a task of the notes note.
type Task struct {
	// ID is the note of the notepad the task is, once known; "" = none,
	// and the task stays as written.
	ID string
	// Text is its words: its lines, without the box, the indent of its
	// lines after the first, or its block identifier.
	Text string
	Done bool
	// Block is its block identifier, without the caret; "" = none.
	Block string

	lead  string   // what comes before the box: the indent and the list marker, as "- "
	box   string   // what is in the box: a space, x, or another mark
	inset int      // how far its lines after the first are indented
	raw   []string // its lines as written
}

// Doc is the notes note's text: the lines kept as written, and its tasks.
type Doc struct {
	parts []part
	crlf  bool   // its lines end in CR LF
	bom   bool   // it starts with a byte order mark
	end   bool   // its last line ends in a line break
	open  string // what closes the properties, fence or comment it ends inside; "" = none
}

type part struct {
	line string // a line kept as written, when task is nil
	task *Task
}

var (
	// A task's first line: up to three spaces, a list marker, a space,
	// the box with one mark in it, and its words after a space.
	taskLine = regexp.MustCompile(`^( {0,3}(?:[-*+]|[0-9]{1,9}[.)])[ \t]+)\[([^\]])\](?:[ \t](.*))?$`)
	// A fence of code, which a task never starts inside.
	fenceLine = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	// A block identifier at a line's end, after a space or alone.
	blockAtEnd = regexp.MustCompile(`(?:^|[ \t])\^([A-Za-z0-9-]+)[ \t]*$`)
	// Irie's own mark.
	irieMark = regexp.MustCompile(`^i[a-z2-7]{6}$`)
)

// Mark is the block identifier Irie gives note id: i and six letters and
// digits of the id's SHA-256, the same wherever and whenever it is made,
// so a note is found again without a table of marks.
func Mark(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "i" + strings.ToLower(base32.StdEncoding.EncodeToString(sum[:]))[:6]
}

// Parse cuts a note's text into its lines and its tasks. A task is a line
// of a task list at the top level, outside the note's properties, a fence
// of code or a comment, with the lines indented under it; everything else
// is kept as it is.
func Parse(text string) *Doc {
	d := &Doc{}
	if t, ok := strings.CutPrefix(text, "\ufeff"); ok {
		d.bom, text = true, t
	}
	if text == "" {
		return d
	}
	d.end = strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	broken, crlf := len(lines), 0
	if !d.end {
		broken--
	}
	for i, l := range lines {
		if strings.HasSuffix(l, "\r") {
			lines[i] = strings.TrimSuffix(l, "\r")
			if i < broken {
				crlf++
			}
		}
	}
	d.crlf = broken > 0 && crlf == broken
	front := lines[0] == "---"
	fence, comment := "", false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case front:
			d.parts = append(d.parts, part{line: l})
			if i > 0 && (l == "---" || l == "...") {
				front = false
			}
			continue
		case fence != "":
			d.parts = append(d.parts, part{line: l})
			if closes(l, fence) {
				fence = ""
			}
			continue
		case comment:
			d.parts = append(d.parts, part{line: l})
			if strings.Count(l, "%%")%2 == 1 {
				comment = false
			}
			continue
		}
		if m := taskLine.FindStringSubmatch(l); m != nil {
			t, next := task(lines, i, m)
			d.parts = append(d.parts, part{task: t})
			i = next - 1
			continue
		}
		d.parts = append(d.parts, part{line: l})
		if m := fenceLine.FindStringSubmatch(l); m != nil {
			fence = m[1]
		} else if strings.Count(l, "%%")%2 == 1 {
			comment = true
		}
	}
	switch {
	case front:
		d.open = "---"
	case fence != "":
		d.open = fence
	case comment:
		d.open = "%%"
	}
	return d
}

// task reads the task whose first line is lines[i], matched as m, and the
// lines indented under it, and says where the line after it is.
func task(lines []string, i int, m []string) (*Task, int) {
	t := &Task{lead: m[1], box: m[2], inset: width(m[1]), raw: []string{lines[i]}}
	body := []string{strings.TrimRight(m[3], " \t")}
	j := i + 1
	for j < len(lines) {
		l := lines[j]
		if blank(l) {
			// A blank line is the task's only when a line indented under
			// it follows.
			k := j
			for k < len(lines) && blank(lines[k]) {
				k++
			}
			if k == len(lines) || !indented(lines[k]) {
				break
			}
			for ; j < k; j++ {
				t.raw = append(t.raw, lines[j])
				body = append(body, "")
			}
			continue
		}
		if !indented(l) {
			break
		}
		t.raw = append(t.raw, l)
		body = append(body, strings.TrimRight(outdent(l, t.inset), " \t"))
		j++
	}
	last := len(body) - 1
	if at := blockAtEnd.FindStringSubmatchIndex(body[last]); at != nil {
		t.Block = body[last][at[2]:at[3]]
		body[last] = strings.TrimRight(body[last][:at[0]], " \t")
	}
	t.Text = strings.Join(body, "\n")
	t.Done = strings.TrimSpace(t.box) != ""
	return t, j
}

// closes says whether line l ends the fence opened with run.
func closes(l, run string) bool {
	s := strings.TrimLeft(l, " ")
	if len(l)-len(s) > 3 || !strings.HasPrefix(s, run) {
		return false
	}
	return strings.Trim(s, run[:1]+" \t") == ""
}

func blank(l string) bool { return strings.TrimSpace(l) == "" }

// indented says whether line l is indented under a task: by two spaces or
// a tab.
func indented(l string) bool {
	return !blank(l) && (strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "\t"))
}

// width is how wide s is, a tab reaching the next stop of four.
func width(s string) int {
	n := 0
	for _, c := range s {
		if c == '\t' {
			n += 4 - n%4
		} else {
			n++
		}
	}
	return n
}

// outdent takes up to inset columns of indent off line l; a tab reaches
// the inset at once.
func outdent(l string, inset int) string {
	col := 0
	for i := 0; i < len(l); i++ {
		if col >= inset {
			return l[i:]
		}
		switch l[i] {
		case ' ':
			col++
		case '\t':
			col = inset
		default:
			return l[i:]
		}
	}
	return ""
}

// Tasks are the note's tasks, in order.
func (d *Doc) Tasks() []*Task {
	var out []*Task
	for _, p := range d.parts {
		if p.task != nil {
			out = append(out, p.task)
		}
	}
	return out
}

// Synced is each of the note's tasks that is a note, with its block
// identifier, words and tick as the note stands, for the next merge to
// weigh the note against.
func (d *Doc) Synced() []Synced {
	out := []Synced{}
	for _, t := range d.Tasks() {
		if t.ID != "" {
			out = append(out, Synced{ID: t.ID, Block: t.Block, Text: t.Text, Done: t.Done})
		}
	}
	return out
}

// Name names the note's tasks with the notes they are, ids in order as
// Render gave them for its text ("" for a task kept as written). Should
// the tasks not line up with the ids, each is found by its mark.
func (d *Doc) Name(ids []string) {
	tasks := d.Tasks()
	if len(tasks) == len(ids) {
		for i, t := range tasks {
			t.ID = ids[i]
		}
		return
	}
	byMark := map[string]string{}
	for _, id := range ids {
		if id != "" {
			byMark[Mark(id)] = id
		}
	}
	for _, t := range tasks {
		t.ID = byMark[t.Block]
	}
}

// Fresh is a new notes note: intro, a line the person reads first, then
// the tasks.
func Fresh(intro string) *Doc {
	if intro == "" {
		return &Doc{end: true}
	}
	return Parse(intro + "\n\n")
}

// Clean is a note's words as the notes note holds them: line breaks of
// any kind as one, with no spaces at a line's end, and none after its last
// line.
func Clean(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// Render is the note's text with notes, the notepad's, as its tasks. Each
// task that is a note takes the words and tick of a note in the notepad's
// order, so the lines around the tasks stay where they were; a note that
// is no task yet goes in after the note before it in the notepad, unless
// it is empty; a task whose note is gone goes; a task that is no note
// stays as written. Ids are the notes the result's tasks are, in order.
func (d *Doc) Render(notes []Note) (string, []string) {
	inPad := map[string]bool{}
	for _, n := range notes {
		inPad[n.ID] = true
	}
	// The tasks that are notes the notepad still has: one task a note.
	slot := map[string]*Task{}
	for _, t := range d.Tasks() {
		if t.ID != "" && inPad[t.ID] && slot[t.ID] == nil {
			slot[t.ID] = t
		}
	}
	block := func(n Note) string {
		if t := slot[n.ID]; t != nil && t.Block != "" {
			return t.Block
		}
		return Mark(n.ID)
	}
	var order, lead []Note
	after := map[string][]Note{}
	prev := ""
	for _, n := range notes {
		if slot[n.ID] != nil {
			order = append(order, n)
			prev = n.ID
			continue
		}
		if strings.TrimSpace(n.Text) == "" {
			continue
		}
		if prev == "" {
			lead = append(lead, n)
		} else {
			after[prev] = append(after[prev], n)
		}
	}
	var out, ids []string
	add := func(ns []Note) {
		for _, n := range ns {
			box := " "
			if n.Done {
				box = "x"
			}
			out = append(out, lines("- ", box, 2, Clean(n.Text), block(n))...)
			ids = append(ids, n.ID)
		}
	}
	k, led, lastTask := 0, false, false
	for _, p := range d.parts {
		t := p.task
		if t == nil {
			out = append(out, p.line)
			lastTask = false
			continue
		}
		lastTask = true
		switch {
		case t.ID != "" && !inPad[t.ID]:
			// Its note is gone from the notepad: the task goes too.
			continue
		case t.ID == "" || slot[t.ID] != t:
			// No note of the notepad's, or a second task of one: as written.
			out = append(out, t.raw...)
			ids = append(ids, "")
			continue
		}
		if !led {
			add(lead)
			led = true
		}
		n := order[k]
		k++
		out = append(out, t.as(n, block(n))...)
		ids = append(ids, n.ID)
		add(after[n.ID])
	}
	if !led && len(lead) > 0 {
		// No task to go by: the notes go at the note's end, after a blank
		// line when what comes before is words of the person's. A note that
		// ends inside its properties, a fence of code or a comment has it
		// closed first, or the tasks would be in it.
		if d.open != "" {
			out = append(out, d.open)
			lastTask = false
		}
		if len(out) > 0 && !blank(out[len(out)-1]) && !lastTask {
			out = append(out, "")
		}
		add(lead)
	}
	return d.join(out), ids
}

// as is task t's lines with note n's words and tick, and its block
// identifier: as written while they are the same.
func (t *Task) as(n Note, block string) []string {
	text := Clean(n.Text)
	if n.ID == t.ID && text == t.Text && n.Done == t.Done && block == t.Block {
		return t.raw
	}
	box := " "
	if n.Done {
		box = "x"
		if t.Done {
			box = t.box // the person's own mark for a done task
		}
	}
	return lines(t.lead, box, t.inset, text, block)
}

// lines is a task's lines: lead, the box with box in it, the words, each
// line after the first indented by inset, and the block identifier at the
// end of the last.
func lines(lead, box string, inset int, text, block string) []string {
	ls := strings.Split(text, "\n")
	pad := strings.Repeat(" ", inset)
	out := make([]string, len(ls))
	for i, l := range ls {
		switch {
		case i == 0:
			out[0] = lead + "[" + box + "]"
			if l != "" {
				out[0] += " " + l
			}
		case l != "":
			out[i] = pad + l
		}
	}
	out[len(out)-1] += " ^" + block
	return out
}

// join is lines as the note's text, with the note's line breaks.
func (d *Doc) join(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	br := "\n"
	if d.crlf {
		br = "\r\n"
	}
	s := strings.Join(lines, br)
	if d.end || len(d.parts) == 0 {
		s += br
	}
	if d.bom {
		s = "\ufeff" + s
	}
	return s
}

// Change is a change the notes note asks of the notepad: an ipc.NotepadArgs
// for one of its ops, and the task it comes from.
type Change struct {
	ipc.NotepadArgs
	// Task is the task an add or an edit comes from: when the notepad
	// turns the change down, the task stays as written rather than lose
	// its words (Task.Unname).
	Task *Task
}

// Unname makes t a task that is no note, kept as written.
func (t *Task) Unname() { t.ID = "" }

// Plan is what a merge asks of the notepad, in the order to make the
// changes: edits and ticks, deletes, moves, then adds.
type Plan struct {
	Changes []Change
	// Held is how many notes went from the note at once that the merge
	// keeps rather than delete: the next write puts them back.
	Held int
}

// Merge weighs the notes note as it stands, d, against base, its tasks
// when it and the notepad last agreed, and the notepad's notes and those
// in its Recently Deleted. It names each task of d with the note it is,
// and plans what the note changed since: a task's new words or tick
// change its note; a task new to the note is a new note, after the note
// before it, with an id from newID; a task gone takes its note to
// Recently Deleted; and a new order moves the notes. A task finds its note
// by its block identifier, else by the words it had, else by its place
// while it is new. With no base, as the first time, every task with a note
// keeps the notepad's words and tick, and nothing is deleted. The notepad
// is the copy kept whole, so notes gone at once are held rather than
// deleted where that looks like a slip or a sync, not the person: more
// than three with none of the note's tasks left, or more than ten that
// are more than half.
func Merge(d *Doc, base []Synced, notes []Note, deleted []Note, newID func() string) Plan {
	tasks := d.Tasks()
	pad := map[string]Note{}
	gone := map[string]Note{}
	marks := map[string][]string{}
	for _, n := range notes {
		pad[n.ID] = n
		marks[Mark(n.ID)] = append(marks[Mark(n.ID)], n.ID)
	}
	for _, n := range deleted {
		if _, ok := pad[n.ID]; !ok {
			gone[n.ID] = n
			marks[Mark(n.ID)] = append(marks[Mark(n.ID)], n.ID)
		}
	}
	known := func(id string) bool {
		_, a := pad[id]
		_, b := gone[id]
		return a || b
	}
	was := map[string]Synced{}
	theirs := map[string]string{} // a block identifier of the person's own → its note
	for _, b := range base {
		was[b.ID] = b
		if b.Block != "" && b.Block != Mark(b.ID) {
			theirs[b.Block] = b.ID
		}
	}
	used := map[string]bool{}
	take := func(t *Task, id string) {
		t.ID = id
		used[id] = true
	}
	// By block identifier: Irie's mark, or one the person gave the task.
	for _, t := range tasks {
		t.ID = ""
		if t.Block == "" {
			continue
		}
		id := ""
		if ids := marks[t.Block]; len(ids) == 1 {
			id = ids[0]
		} else if b, ok := theirs[t.Block]; ok && known(b) {
			id = b
		}
		if id != "" && !used[id] {
			take(t, id)
		}
	}
	// By the words it had: a task whose identifier went or changed, or was
	// never written, as a line new since the last write.
	for _, t := range tasks {
		if t.ID != "" {
			continue
		}
		for _, b := range base {
			if !used[b.ID] && known(b.ID) && b.Text == t.Text {
				take(t, b.ID)
				break
			}
		}
	}
	// By its place, while it is new: a line typed since the last write,
	// still being written.
	for i, t := range tasks {
		if t.ID != "" || t.Block != "" || i >= len(base) {
			continue
		}
		if b := base[i]; b.Block == "" && !used[b.ID] && known(b.ID) {
			take(t, b.ID)
		}
	}

	var plan Plan
	var edits, adds []Change
	for _, t := range tasks {
		if t.ID == "" {
			continue
		}
		b, had := was[t.ID]
		if n, ok := pad[t.ID]; ok {
			if !had {
				continue // the notepad's words and tick stand
			}
			if t.Text != b.Text && t.Text != Clean(n.Text) {
				edits = append(edits, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadEdit, ID: t.ID, Text: t.Text}, Task: t})
			}
			if t.Done != b.Done && t.Done != n.Done {
				edits = append(edits, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadCheck, ID: t.ID, Done: t.Done}})
			}
			continue
		}
		// Deleted in the notepad: a task changed since brings its note
		// back with what it says, as an edit of a deleted note does.
		if had && (t.Text != b.Text || t.Done != b.Done) && strings.TrimSpace(t.Text) != "" {
			edits = append(edits, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadEdit, ID: t.ID, Text: t.Text}, Task: t})
			if t.Done != gone[t.ID].Done {
				edits = append(edits, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadCheck, ID: t.ID, Done: t.Done}})
			}
		}
	}
	// New tasks are new notes, each after the note before it.
	blocks := map[string]bool{}
	for _, t := range tasks {
		if t.ID != "" && t.Block != "" {
			blocks[t.Block] = true
		}
	}
	prev := ""
	for _, t := range tasks {
		if t.ID != "" {
			if _, ok := pad[t.ID]; ok {
				prev = t.ID
			}
			continue
		}
		if strings.TrimSpace(t.Text) == "" {
			continue // an empty task stays as written
		}
		id := newID()
		// A block identifier of the person's own stays the task's, unless
		// another task has it; Irie's own mark is the new note's.
		if t.Block != "" && (blocks[t.Block] || irieMark.MatchString(t.Block)) {
			t.Block = ""
		}
		if t.Block != "" {
			blocks[t.Block] = true
		}
		take(t, id)
		adds = append(adds, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadAdd, ID: id, Text: t.Text, After: prev}, Task: t})
		if prev == "" {
			// Before every note the note shows: an add with nothing to go
			// after goes last, so it moves up before the first of them.
			if next := firstAfter(tasks, t, pad); next != "" {
				adds = append(adds, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadMove, ID: id, Before: next}})
			}
		}
		if t.Done {
			adds = append(adds, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadCheck, ID: id, Done: true}})
		}
		prev = id
	}
	// Tasks gone from the note since the last sync.
	var lost []string
	left := 0
	for _, b := range base {
		if used[b.ID] {
			left++
		} else if _, ok := pad[b.ID]; ok {
			lost = append(lost, b.ID)
		}
	}
	var dels []Change
	if len(lost) > 3 && left == 0 || len(lost) > 10 && len(lost)*2 > len(base) {
		plan.Held = len(lost)
	} else if len(lost) > 0 {
		dels = append(dels, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadDelete, IDs: lost}})
	}
	plan.Changes = append(append(edits, dels...), moves(tasks, base, notes, lost, plan.Held > 0)...)
	plan.Changes = append(plan.Changes, adds...)
	return plan
}

// firstAfter is the first task after t that is a note of the notepad's.
func firstAfter(tasks []*Task, t *Task, pad map[string]Note) string {
	i := slices.Index(tasks, t)
	for _, u := range tasks[i+1:] {
		if _, ok := pad[u.ID]; ok && u.ID != "" {
			return u.ID
		}
	}
	return ""
}

// moves are the moves that give the notepad the note's new order, when the
// note's tasks that were there at the last sync come in another order now
// than they did then: the notes the note shows take its order among
// themselves, and every other note keeps its place. The longest run of
// them already in that order stays put; the rest move.
func moves(tasks []*Task, base []Synced, notes []Note, lost []string, held bool) []Change {
	if len(base) == 0 {
		return nil
	}
	wasAt := map[string]bool{}
	for _, b := range base {
		wasAt[b.ID] = true
	}
	inPad := map[string]bool{}
	for _, n := range notes {
		inPad[n.ID] = true
	}
	var now []string
	shown := map[string]bool{}
	for _, t := range tasks {
		if t.ID != "" && wasAt[t.ID] && inPad[t.ID] && !shown[t.ID] {
			now = append(now, t.ID)
			shown[t.ID] = true
		}
	}
	var then []string
	for _, b := range base {
		if shown[b.ID] {
			then = append(then, b.ID)
		}
	}
	if slices.Equal(now, then) {
		return nil
	}
	going := map[string]bool{}
	if !held {
		for _, id := range lost {
			going[id] = true
		}
	}
	var current []string
	seen := map[string]bool{}
	for _, n := range notes {
		if !going[n.ID] && !seen[n.ID] {
			current = append(current, n.ID)
			seen[n.ID] = true
		}
	}
	want := slices.Clone(current)
	j := 0
	for i, id := range want {
		if shown[id] {
			want[i] = now[j]
			j++
		}
	}
	at := map[string]int{}
	for i, id := range current {
		at[id] = i
	}
	seq := make([]int, len(want))
	for i, id := range want {
		seq[i] = at[id]
	}
	stay := longestRun(seq)
	var out []Change
	for i := len(want) - 1; i >= 0; i-- {
		if stay[i] {
			continue
		}
		before := ""
		if i+1 < len(want) {
			before = want[i+1]
		}
		out = append(out, Change{NotepadArgs: ipc.NotepadArgs{Do: ipc.NotepadMove, ID: want[i], Before: before}})
	}
	return out
}

// longestRun marks the places of seq that make its longest increasing
// subsequence.
func longestRun(seq []int) []bool {
	var tails []int // the place in seq of the least last value of a run of each length
	prev := make([]int, len(seq))
	for i, v := range seq {
		k, _ := slices.BinarySearchFunc(tails, v, func(t, v int) int { return seq[t] - v })
		if k > 0 {
			prev[i] = tails[k-1]
		} else {
			prev[i] = -1
		}
		if k == len(tails) {
			tails = append(tails, i)
		} else {
			tails[k] = i
		}
	}
	out := make([]bool, len(seq))
	if len(tails) == 0 {
		return out
	}
	for i := tails[len(tails)-1]; i >= 0; i = prev[i] {
		out[i] = true
	}
	return out
}
