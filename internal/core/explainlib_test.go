package core

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/explained"
	"github.com/irie-people/irie/internal/ipc"
)

const keysCard = `{"title":"Your groups, with keys","body":[{"type":"Flow","items":[{"title":"Pick","detail":"⌘-click the cards"},{"title":"New Group","detail":"in the card's menu"}]}]}`

func TestTheLibraryKeepsExplainsAndReusesEveryExplanation(t *testing.T) {
	f := &cardOllama{reply: flowCard}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := cardCore(t, srv.URL)
	repo := filepath.Join(t.TempDir(), "irie")
	os.MkdirAll(repo, 0o755)
	c.sessions["s1"] = &session{id: "s1", name: "Groups", pane: "%1", cwd: repo, repo: repo}
	q := "How do groups work?"
	ask := ipc.MaterializeArgs{Explain: q, Session: "s1", Topic: "Your groups", Wait: 10 * time.Second}

	// Made by the model, and kept as files in the project's folder of the
	// library, under the core's own home.
	r, err := c.materializeOp(ask)
	if err != nil || r.State != "fresh" || r.Versions != 1 || r.Kept {
		t.Fatalf("first: %+v, %v", r, err)
	}
	lib := explained.Library{Dir: filepath.Join(c.paths.Home, "Documents", brand.Name, "Explained")}
	md, js := lib.Files("irie", q)
	if r.Saved != md {
		t.Errorf("saved at %q; want %q", r.Saved, md)
	}
	for _, p := range []string{md, js} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("no file: %v", err)
		}
	}
	if e, _ := lib.Load("irie", q); e == nil || e.Topic != "Your groups" || e.Latest().By != "qwen3:4b" {
		t.Errorf("kept: %+v", e)
	}

	// Lost from the cards' cache, it is answered from the library, with no
	// model.
	c.cardStore().Forget(r.ID)
	if again, _ := c.materializeOp(ask); again.State != "fresh" || !strings.Contains(string(again.Spec), "A kept card") || f.count() != 1 {
		t.Errorf("from the library: %s, %d chats; want the kept card and no chat", again.State, f.count())
	}

	// A turn of the chat makes a version from the newest, with what was
	// said, and the question shows it after.
	f.say(keysCard)
	turn := ask
	turn.Refine = "add the keys"
	r2, err := c.materializeOp(turn)
	if err != nil || r2.ID == r.ID || r2.Versions != 2 || !strings.Contains(string(r2.Spec), "with keys") {
		t.Fatalf("a turn: %+v, %v", r2, err)
	}
	msgs := f.lastAsked()
	if user := msgs[len(msgs)-1].Content; !strings.Contains(user, "says of it: add the keys") || !strings.Contains(user, "The card made before:\n"+flowCard) {
		t.Errorf("the turn asked %q; want what was said and the card it changes", user)
	}
	if e, _ := lib.Load("irie", q); e == nil || len(e.Versions) != 2 || e.Latest().Asked != "add the keys" {
		t.Errorf("versions: %+v", e)
	}
	if shown, _ := c.materializeOp(ask); !strings.Contains(string(shown.Spec), "with keys") {
		t.Errorf("the question shows %s; want the newest version", shown.Spec)
	}

	// Kept, and listed for the project.
	on := true
	keep := ask
	keep.Keep = &on
	if k, err := c.materializeOp(keep); err != nil || !k.Kept {
		t.Errorf("keep: %+v, %v", k, err)
	}
	l, err := c.learnOp(ipc.LearnArgs{Session: "s1"})
	if err != nil || len(l.Saved) != 1 || !l.Saved[0].Kept || l.Saved[0].Versions != 2 || l.Saved[0].File != md {
		t.Errorf("listed: %+v, %v", l.Saved, err)
	}

	// Made again at the person's word: by the model, as a new version.
	f.say(flowCard)
	forget := ask
	forget.Forget, forget.Wait = true, 0
	c.materializeOp(forget)
	if r3, _ := c.materializeOp(ask); r3.Versions != 3 || f.count() != 3 {
		t.Errorf("made again: %d versions, %d chats; want 3 and 3", r3.Versions, f.count())
	}
}

func TestATurnOfTheChatNeedsACardToChange(t *testing.T) {
	c := cardCore(t, "http://127.0.0.1:1")
	if _, err := c.materializeOp(ipc.MaterializeArgs{Explain: "What is a space?", Refine: "simpler"}); err == nil || !strings.Contains(err.Error(), "ask it first") {
		t.Errorf("a turn with no card: %v", err)
	}
}

func TestTheLibraryCanBeOff(t *testing.T) {
	f := &cardOllama{reply: flowCard}
	srv := httptest.NewServer(f)
	defer srv.Close()
	cfg := config.Default()
	cfg.Local.URL, cfg.Learn.Library = srv.URL, "off"
	c := bareCore(t, cfg)
	r, err := c.materializeOp(ipc.MaterializeArgs{Explain: "What is a space?", Wait: 10 * time.Second})
	if err != nil || r.Saved != "" || r.State != "fresh" {
		t.Errorf("%+v, %v; want a card, kept nowhere", r, err)
	}
	if _, err := os.Stat(filepath.Join(c.paths.Home, "Documents")); err == nil {
		t.Error("the library is off, yet a folder was made")
	}
	on := true
	if _, err := c.materializeOp(ipc.MaterializeArgs{Explain: "What is a space?", Keep: &on}); err == nil {
		t.Error("kept with the library off")
	}
}
