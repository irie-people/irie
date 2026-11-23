package core

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/irie-people/irie/internal/brand"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/lessons"
	"github.com/irie-people/irie/internal/localmodel"
)

// lessonsCore is a core with no tmux behind it whose latest draft of
// lessons, two of them, session s1 asked for; s2 is another agent's.
func lessonsCore(t *testing.T) *Core {
	t.Helper()
	c, _ := dogCore(t)
	c.sessions["s1"] = &session{id: "s1", name: "lessons", pane: "%1", harness: "claude-code"}
	c.sessions["s2"] = &session{id: "s2", name: "other", pane: "%2", harness: "claude-code"}
	k := lessons.Kind{N: 1, Kind: "Edit: File has not been read yet. Read it first before writing to it.", By: lessons.ByTool, Calls: 134, Sessions: 52, Then: "Read"}
	c.lessons.book.Take([]lessons.Lesson{
		{N: 1, Text: "Read a file with Read before you Edit or Write it.", Kinds: []lessons.Kind{k}, Days: 14, By: "qwen3:4b"},
		{N: 2, Text: "Wait on a log with Monitor, never with sleep.", Kinds: []lessons.Kind{k}, Days: 14, By: "qwen3:4b"},
	}, lessons.Evidence{}, "qwen3:4b", "s1", time.Now())
	return c
}

func TestOnlyTheUsersWordPicksALesson(t *testing.T) {
	c := lessonsCore(t)
	keep := ipc.LessonsArgs{Do: ipc.LessonsKeep, Lessons: []int{1}, OK: true}
	no := ipc.LessonsArgs{Do: ipc.LessonsNo, Lessons: []int{2}}
	// Another agent can't pick for the user.
	inAgent(c, "%2")
	for _, a := range []ipc.LessonsArgs{keep, no, {Do: ipc.LessonsDrop, Lessons: []int{1}}} {
		if _, err := c.lessonsOp(os.Getpid(), a); err == nil || !strings.Contains(err.Error(), "An agent can't pick lessons") {
			t.Errorf("%s from another agent: %v", a.Do, err)
		}
	}
	// Nor can the session that asked for the draft, until the user messages it.
	inAgent(c, "%1")
	if _, err := c.lessonsOp(os.Getpid(), no); err == nil || !strings.Contains(err.Error(), "have not messaged you here") {
		t.Errorf("no from the asking session, unasked: %v", err)
	}
	c.markWord("s1")
	if _, err := c.lessonsOp(os.Getpid(), no); err != nil {
		t.Errorf("no from the asking session, just after the user's message: %v", err)
	}
	// A keep asks first, with the words every session will hear.
	inAgent(c, "")
	if _, err := c.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsKeep, Lessons: []int{1}}); err == nil || !strings.Contains(err.Error(), "Every session hears this as it starts") || !strings.Contains(err.Error(), "- Read a file with Read") {
		t.Errorf("a keep without OK: %v", err)
	}
	if _, err := c.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsKeep, Lessons: []int{3}, OK: true}); err == nil || !strings.Contains(err.Error(), "no lesson 3") {
		t.Errorf("a keep of a lesson not drafted: %v", err)
	}
	if _, err := c.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsKeep, Lessons: []int{1}, OK: true, Of: time.Now().Add(time.Hour)}); err == nil || !strings.Contains(err.Error(), "newer draft") {
		t.Errorf("a keep from a draft since replaced: %v", err)
	}
	r, err := c.lessonsOp(0, keep)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Book.Kept) != 1 || r.Book.Drafts[1].Said != lessons.No || !strings.Contains(r.Note, "Read a file with Read") {
		t.Errorf("after the user's word: kept %+v, drafts %+v, note %q", r.Book.Kept, r.Book.Drafts, r.Note)
	}
	// Kept across a restart.
	c2 := testCore(t)
	c2.paths = c.paths
	c2.loadLessons()
	if got := c2.lessonsReport(); len(got.Book.Kept) != 1 || got.Book.Asked != "s1" {
		t.Errorf("after a restart: %+v", got.Book)
	}
	// Not from a socket, the lessons are only shown.
	raw, _ := json.Marshal(keep)
	if _, err := c.Call(ipc.OpLessons, raw); err == nil {
		t.Error("a keep not over the socket went through")
	}
	if _, err := c.Call(ipc.OpLessons, []byte(`{}`)); err != nil {
		t.Errorf("the lessons not over the socket: %v", err)
	}
	if _, err := c.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsDraft}); err == nil || !strings.Contains(err.Error(), "A local model on this computer") {
		t.Errorf("a draft without OK: %v", err)
	}
}

func TestEverySessionHearsTheLessonsKept(t *testing.T) {
	c := lessonsCore(t)
	x := &session{id: "s3", harness: "claude-code", out: &outcome{}}
	c.mu.Lock()
	before := c.withLessons(c.startNote(x))
	c.mu.Unlock()
	if strings.Contains(before, "Lessons from") {
		t.Errorf("a session hears lessons no one kept: %q", before)
	}
	if _, err := c.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsKeep, Lessons: []int{2, 1}, OK: true}); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	told := c.withLessons(c.startNote(x))
	c.mu.Unlock()
	if !strings.Contains(told, "which your user picked; follow them:\n- Wait on a log with Monitor, never with sleep.\n- Read a file with Read before you Edit or Write it.") {
		t.Errorf("the start note:\n%s", told)
	}
	if _, err := c.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsDrop, Lessons: []int{1}}); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	told = c.withLessons(c.startNote(x))
	c.mu.Unlock()
	if strings.Contains(told, "Wait on a log") || !strings.Contains(told, "Read a file with Read") {
		t.Errorf("after a drop, the start note:\n%s", told)
	}
	// Lessons give way first when what a session hears runs long, so
	// that Claude Code passes it on whole.
	long := strings.Repeat("x", startRoom-60)
	if got := c.withLessons(long); got != long {
		t.Errorf("a lesson pushed a long start note past %d characters: %d", startRoom, len(got))
	}
}

func TestRaffisRowCarriesTheLessons(t *testing.T) {
	c := lessonsCore(t)
	if _, err := c.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsKeep, Lessons: []int{2}, OK: true}); err != nil {
		t.Fatal(err)
	}
	var raffi *ipc.ArchAgent
	for _, a := range c.archList().Agents {
		if a.Name == "raphael" {
			raffi = &a
		}
	}
	if raffi == nil {
		t.Fatal("no " + brand.Raffi + " in the arch op")
	}
	var draft *ipc.ArchAction
	for i, a := range raffi.Actions {
		if a.Label == "Draft lessons" {
			draft = &raffi.Actions[i]
		}
	}
	if draft == nil || draft.Op != ipc.OpLessons || !strings.Contains(draft.Ask, "costs no tokens") {
		t.Errorf(brand.Raffi+"'s actions: %+v", raffi.Actions)
	}
	var waiting, kept *ipc.ArchItem
	for i, it := range raffi.Items {
		switch {
		case strings.HasPrefix(it.Text, "Lesson 1: Read a file"):
			waiting = &raffi.Items[i]
		case it.Text == "Every session hears: Wait on a log with Monitor, never with sleep.":
			kept = &raffi.Items[i]
		}
	}
	if waiting == nil || kept == nil {
		t.Fatalf(brand.Raffi+"'s items: %+v", raffi.Items)
	}
	if len(waiting.Actions) != 2 || waiting.Actions[0].Label != "Keep" || !strings.Contains(waiting.Actions[0].Ask, "Every session hears this") || !strings.Contains(waiting.Detail, "134 calls in 52 sessions") {
		t.Errorf("the waiting lesson: %+v", waiting)
	}
	// Its Keep, as a client sends it, keeps it, once the person says OK.
	var a ipc.LessonsArgs
	if err := json.Unmarshal(waiting.Actions[0].Args, &a); err != nil {
		t.Fatal(err)
	}
	a.OK = true
	if _, err := c.lessonsOp(0, a); err != nil {
		t.Errorf("Keep from the row: %v", err)
	}
	if len(kept.Actions) != 1 || kept.Actions[0].Label != "Drop" || kept.Level != ipc.ArchOK || !strings.Contains(kept.Detail, "Kept ") {
		t.Errorf("the kept lesson: %+v", kept)
	}
}

func TestADraftComesFromALocalModelAndWaitsForTheUsersWord(t *testing.T) {
	f := &cardOllama{reply: `{"lessons":[{"lesson":"Read a file before you edit it.","kinds":[1]}]}`}
	srv := httptest.NewServer(f)
	defer srv.Close()
	cfg := config.Default()
	cfg.Local.URL = srv.URL
	c := bareCore(t, cfg)
	// What this machine has free is no test's business.
	c.lessons.room = func(context.Context, localmodel.ChatModel) error { return nil }
	j, err := journal.Open(c.paths.Journal(), journal.Options{KeepDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range []string{"s0000000000a1", "s0000000000a2"} {
		id := string(rune('a'+i)) + "1"
		for _, e := range []event.Event{
			{Session: s, Agent: event.Main, Source: event.SourceHook, Harness: "claude-code", Kind: event.ToolStarted, Vendor: &event.Vendor{ToolUseID: id}, Data: map[string]any{"tool": "Edit"}},
			{Session: s, Agent: event.Main, Source: event.SourceHook, Harness: "claude-code", Kind: event.ToolFailed, Vendor: &event.Vendor{ToolUseID: id}, Data: map[string]any{"tool": "Edit", "error": "File has not been read yet. Read it first before writing to it."}},
		} {
			if _, err := j.Append(e); err != nil {
				t.Fatal(err)
			}
		}
	}
	j.Close()
	r, err := c.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsDraft, OK: true, Journal: true})
	if err != nil || !r.Drafting {
		t.Fatalf("draft: %+v, %v", r, err)
	}
	if _, err := c.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsDraft, OK: true}); err == nil || !strings.Contains(err.Error(), "under way") {
		t.Errorf("a second draft while one runs: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for r.Drafting && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		r = c.lessonsReport()
	}
	b := r.Book
	if r.Drafting || len(b.Drafts) != 1 || b.By != "qwen3:4b" || b.Asked != "" || b.Problem != "" || b.Evidence == nil || b.Evidence.Failed != 2 {
		t.Fatalf("after the draft: drafting %v, %+v", r.Drafting, b)
	}
	if l := b.Drafts[0]; l.Text != "Read a file before you edit it." || l.Said != lessons.Waiting || l.Kinds[0].Sessions != 2 || len(b.Kept) != 0 {
		t.Errorf("the draft: %+v", l)
	}
	if asked := f.lastAsked(); len(asked) != 2 || !strings.Contains(asked[1].Content, "1. Edit: File has not been read yet.") {
		t.Errorf("the model was asked: %+v", asked)
	}
	// A machine short of memory loads no model: the draft says why, and
	// the lessons that wait stay.
	c.lessons.room = func(context.Context, localmodel.ChatModel) error {
		return errors.New("too little memory is free to load the model now, so qwen3:4b was not loaded")
	}
	chats := f.count()
	if _, err := c.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsDraft, OK: true, Journal: true}); err != nil {
		t.Fatal(err)
	}
	for r = c.lessonsReport(); r.Drafting && time.Now().Before(deadline); r = c.lessonsReport() {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(r.Book.Problem, "Too little memory is free") || len(r.Book.Waiting()) != 1 || f.count() != chats {
		t.Errorf("a draft with no room: %+v after %d chats", r.Book, f.count()-chats)
	}
	// With nothing two sessions share, a draft says so and keeps what waits.
	c2 := bareCore(t, cfg)
	c2.lessons.book = b
	if _, err := c2.lessonsOp(0, ipc.LessonsArgs{Do: ipc.LessonsDraft, OK: true, Journal: true}); err != nil {
		t.Fatal(err)
	}
	for r = c2.lessonsReport(); r.Drafting && time.Now().Before(deadline); r = c2.lessonsReport() {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(r.Book.Problem, "nothing to learn from yet") || len(r.Book.Waiting()) != 1 {
		t.Errorf("a draft with nothing to learn: %+v", r.Book)
	}
}
