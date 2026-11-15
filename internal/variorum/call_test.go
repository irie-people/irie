package variorum

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/version"
)

// tabSetup is the tab's setup for app's door.
func tabSetup(app string) Setup { return Setup{Command: doorOf(app), Key: tabKey} }

// calls are the tools/call messages the stand-in got, after checking that
// the client said initialize first, as Irie, and initialized before any
// call, and sent nothing before them.
func calls(t *testing.T, app string) []message {
	t.Helper()
	r := got(t, app)
	if len(r.Lines) < 2 {
		t.Fatalf("the door got %d lines", len(r.Lines))
	}
	hello := decode(t, r.Lines[0])
	info, _ := hello.Params["clientInfo"].(map[string]any)
	if hello.Method != "initialize" || hello.Params["protocolVersion"] != protocolVersion || info["name"] != brand.Slug || info["version"] != version.Base {
		t.Errorf("the first line: %s", r.Lines[0])
	}
	if m := decode(t, r.Lines[1]); m.Method != "notifications/initialized" || m.ID != nil {
		t.Errorf("the second line: %s", r.Lines[1])
	}
	var out []message
	for _, l := range r.Lines[2:] {
		m := decode(t, l)
		if m.Method != "tools/call" {
			t.Errorf("a line that isn't a call: %s", l)
		}
		out = append(out, m)
	}
	return out
}

// TestNotesListsWhatIsShared: one call, list_notes with the most a page
// holds, past a notification and another id's answer, over one connection
// with the tab's key and nothing else in the door's environment.
func TestNotesListsWhatIsShared(t *testing.T) {
	app := fakeApp(t, "io.lduarte.Variorum")
	setScript(t, app, script{Answers: map[string][]answer{"list_notes": {{Text: `{"notes":[{"id":"n1","title":"Chapter 3","edited":"2026-11-14T10:00:00Z","wing_id":""},{"id":"n2","title":"","edited":null}],"next_cursor":"more"}`}}}})
	notes, err := tabSetup(app).Notes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].ID != "n1" || notes[0].Title != "Chapter 3" || notes[0].Edited != "2026-11-14T10:00:00Z" || notes[1].ID != "n2" || notes[1].Edited != "" {
		t.Errorf("notes = %+v", notes)
	}
	c := calls(t, app)
	if len(c) != 1 || c[0].Params["name"] != "list_notes" {
		t.Fatalf("calls = %+v", c)
	}
	if args := c[0].Params["arguments"].(map[string]any); len(args) != 1 || args["limit"] != float64(200) {
		t.Errorf("list_notes(%v)", args)
	}
	env := got(t, app).Env
	slices.Sort(env)
	if want := []string{"HOME=" + home(), "PATH=/usr/bin:/bin", "VARIORUM_MCP_KEY=" + tabKey}; !slices.Equal(env, want) {
		t.Errorf("the door's environment = %q, want %q", env, want)
	}

	// None shared is [] on the wire; an answer to anything else has no notes.
	setScript(t, app, script{Answers: map[string][]answer{"list_notes": {{Text: `{"notes":[]}`}}}})
	notes, err = tabSetup(app).Notes(context.Background())
	if err != nil || notes == nil || len(notes) != 0 {
		t.Fatalf("no notes: %#v, %v", notes, err)
	}
	if b, _ := json.Marshal(ipc.VariorumResult{State: ipc.VariorumReady, Notes: notes}); !strings.Contains(string(b), `"notes":[]`) {
		t.Errorf("no notes on the wire: %s", b)
	}
	if b, _ := json.Marshal(ipc.VariorumResult{State: ipc.VariorumReady}); strings.Contains(string(b), "notes") {
		t.Errorf("a status on the wire: %s", b)
	}
}

// TestReadFollowsTheCursor: a read over two pages, each a call on the
// same connection, its blocks in order without archived takes; and a read
// past the most it follows says there is more.
func TestReadFollowsTheCursor(t *testing.T) {
	app := fakeApp(t, "io.lduarte.Variorum")
	page1 := `{"note_id":"n1","title":"Chapter 3","block_count":3,"tags":[],"blocks":[
		{"block_id":"b1","name":"Opening","shown_take_id":"t1","takes":[
			{"take_id":"t1","chosen":true,"text":"It was late."},
			{"take_id":"t2","chosen":false,"text":"It was very late.","label":"longer"},
			{"take_id":"t3","chosen":false,"text":"Old words.","archived":true}],
		 "comments":[{"comment_id":"c1","text":"Too short?"}]},
		{"block_id":"b2","shown_take_id":"t4","takes":[{"take_id":"t4","chosen":true,"text":"The end."}],"comments":[]}],
	"next_cursor":"p2"}`
	page2 := `{"note_id":"n1","title":"Chapter 3","blocks":[{"block_id":"b3","takes":[],"comments":[]}]}`
	setScript(t, app, script{Answers: map[string][]answer{"read_note": {{Text: page1}, {Text: page2}}}})
	before := time.Now().UTC().Add(-time.Second)
	note, err := tabSetup(app).Read(context.Background(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if note.ID != "n1" || note.Title != "Chapter 3" || note.More || len(note.Blocks) != 3 || note.Read.Before(before) {
		t.Fatalf("note = %+v", note)
	}
	b1 := note.Blocks[0]
	if b1.ID != "b1" || b1.Name != "Opening" || b1.Shown != "t1" || len(b1.Takes) != 2 || !b1.Takes[0].Chosen || b1.Takes[0].Text != "It was late." ||
		b1.Takes[1].ID != "t2" || b1.Takes[1].Label != "longer" || b1.Takes[1].Chosen || len(b1.Comments) != 1 || b1.Comments[0].Text != "Too short?" {
		t.Errorf("the first block = %+v", b1)
	}
	if b3 := note.Blocks[2]; b3.ID != "b3" || b3.Takes == nil {
		t.Errorf("the last block = %+v", b3)
	}
	if b, _ := json.Marshal(note.Blocks[2]); !strings.Contains(string(b), `"takes":[]`) {
		t.Errorf("a block with no takes reads %s", b)
	}
	c := calls(t, app)
	if len(c) != 2 {
		t.Fatalf("%d calls, want 2 on one connection", len(c))
	}
	first, second := c[0].Params["arguments"].(map[string]any), c[1].Params["arguments"].(map[string]any)
	if first["note_id"] != "n1" || first["with_ids"] != true || first["limit"] != float64(200) || first["cursor"] != nil {
		t.Errorf("the first page: read_note(%v)", first)
	}
	if second["note_id"] != "n1" || second["cursor"] != "p2" || second["limit"] != float64(200) {
		t.Errorf("the second page: read_note(%v)", second)
	}

	old := maxBlocks
	maxBlocks = 2
	t.Cleanup(func() { maxBlocks = old })
	note, err = tabSetup(app).Read(context.Background(), "n1")
	if err != nil || !note.More || len(note.Blocks) != 2 || len(calls(t, app)) != 1 {
		t.Errorf("past the most it follows: %+v, %v", note, err)
	}

	// An empty note's blocks are [] on the wire.
	setScript(t, app, script{Answers: map[string][]answer{"read_note": {{Text: `{"note_id":"n2","title":"","blocks":[]}`}}}})
	note, err = tabSetup(app).Read(context.Background(), "n2")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(note); !strings.Contains(string(b), `"blocks":[]`) {
		t.Errorf("an empty note on the wire: %s", b)
	}
}

// TestShowAnswersShownOrOffered: show_in_app with the note, and the block
// only when there is one; Variorum's answer said as shown or offered.
func TestShowAnswersShownOrOffered(t *testing.T) {
	app := fakeApp(t, "io.lduarte.Variorum")
	setScript(t, app, script{Answers: map[string][]answer{"show_in_app": {
		{Text: `{"shown":false,"offered":true,"landed":"Variorum offers it."}`},
		{Text: `{"shown":true}`},
	}}})
	s := tabSetup(app)
	if got, err := s.Show(context.Background(), ShowTakes, "n1", "b2"); err != nil || got != "offered" {
		t.Errorf("takes: %q, %v", got, err)
	}
	if args := calls(t, app)[0].Params["arguments"].(map[string]any); args["target"] != "takes" || args["note_id"] != "n1" || args["block_id"] != "b2" {
		t.Errorf("show_in_app(%v)", args)
	}
	setScript(t, app, script{Answers: map[string][]answer{"show_in_app": {{Text: `{"shown":true}`}}}})
	if got, err := s.Show(context.Background(), "", "n1", ""); err != nil || got != "shown" {
		t.Errorf("note: %q, %v", got, err)
	}
	if args := calls(t, app)[0].Params["arguments"].(map[string]any); len(args) != 2 || args["target"] != "note" || args["note_id"] != "n1" {
		t.Errorf("show_in_app(%v)", args)
	}
	for _, tc := range [][3]string{{"lens", "n1", ""}, {"note", "", ""}, {"takes", "n1", ""}} {
		if _, err := s.Show(context.Background(), tc[0], tc[1], tc[2]); err == nil {
			t.Errorf("show %v went through", tc)
		}
	}
}

// TestTheDoorsOwnWordsComeThrough: a refused initialize, a call answered
// with isError and a door that ends mid-answer each come back in the
// door's own words; a door that never answers, in Irie's.
func TestTheDoorsOwnWordsComeThrough(t *testing.T) {
	app := fakeApp(t, "io.lduarte.Variorum")
	s := tabSetup(app)
	ctx := context.Background()

	setScript(t, app, script{Refuse: "Variorum turned this connection away: it isn't let in yet."})
	if _, err := s.Notes(ctx); err == nil || err.Error() != "Variorum turned this connection away: it isn't let in yet." {
		t.Errorf("refused: %v", err)
	}
	if r := got(t, app); len(r.Lines) != 1 {
		t.Errorf("after a refusal the client sent %d lines", len(r.Lines))
	}

	setScript(t, app, script{Answers: map[string][]answer{"read_note": {{Text: "That note isn't shared with this client.", IsError: true}}}})
	if _, err := s.Read(ctx, "n9"); err == nil || err.Error() != "That note isn't shared with this client." {
		t.Errorf("isError: %v", err)
	}

	setScript(t, app, script{Die: "Variorum ended the connection."})
	if _, err := s.Notes(ctx); err == nil || err.Error() != "Variorum ended the connection." {
		t.Errorf("a door that ends mid-answer: %v", err)
	}

	setScript(t, app, script{Hang: true})
	old := callTimeout
	callTimeout = 300 * time.Millisecond
	t.Cleanup(func() { callTimeout = old })
	start := time.Now()
	if _, err := s.Notes(ctx); err != errTimeout {
		t.Errorf("a door that never answers: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("it waited %v", d)
	}

	// A program that fails its checks never starts.
	s.Command = strings.Replace(s.Command, Program, "variorum-mcp2", 1)
	if _, err := s.Notes(ctx); err != errNotDoor {
		t.Errorf("another program: %v", err)
	}
}
