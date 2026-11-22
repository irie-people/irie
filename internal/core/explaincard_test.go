package core

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/genui"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/materialize"
)

const flowCard = `{"title":"A kept card","body":[{"type":"Flow","items":[{"title":"Ask"},{"title":"Keep"}]}]}`

func TestACardExplainsWhatIsAskedAndIsKeptForTheQuestion(t *testing.T) {
	f := &cardOllama{reply: flowCard}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := cardCore(t, srv.URL)
	ask := ipc.MaterializeArgs{Explain: "  How is a card kept?\n", Wait: 10 * time.Second}
	r, err := c.materializeOp(ask)
	if err != nil || r.State != "fresh" || !strings.HasPrefix(r.ID, "explain/") || !strings.Contains(string(r.Spec), "Flow") {
		t.Fatalf("%+v, %v; want a fresh card that explains, made at once", r, err)
	}
	msgs := f.lastAsked()
	if len(msgs) != 2 || !strings.Contains(msgs[0].Content, `"type": "Diagram"`) || !strings.Contains(msgs[1].Content, "Explain this: How is a card kept?") {
		t.Errorf("asked %+v; want the instructions for a card that explains, and the question", msgs)
	}
	// The same question asked again is the kept card, with no chat.
	ask.Explain, ask.Wait = "How is a card kept?", 0
	if again, _ := c.materializeOp(ask); again.ID != r.ID || again.State != "fresh" || f.count() != 1 {
		t.Errorf("asked again: %s %s, %d chats; want the kept card", again.ID, again.State, f.count())
	}
	// Another question is another card.
	if other, _ := c.materializeOp(ipc.MaterializeArgs{Explain: "What is a gap?"}); other.ID == r.ID || other.State != "cold" || !other.Began {
		t.Errorf("another question: %+v; want a card of its own, begun", other)
	}
	// Its remake writes the cards' file: let it finish before the test's
	// folder goes.
	if done, _ := c.materializeOp(ipc.MaterializeArgs{Explain: "What is a gap?", Wait: 10 * time.Second}); done.State != "fresh" {
		t.Errorf("the other question's card is %s; want it made", done.State)
	}
	// Forget finds the card by its question.
	if gone, err := c.materializeOp(ipc.MaterializeArgs{Explain: "How is a card kept?", Forget: true}); err != nil || gone.ID != r.ID || gone.State != "cold" {
		t.Errorf("forget: %+v, %v", gone, err)
	}
	if _, err := c.materializeOp(ipc.MaterializeArgs{Explain: " \x07\n"}); err == nil {
		t.Error("an ask with nothing to explain was taken")
	}
}

func TestAQuestionInASessionIsAnsweredFromItsConversation(t *testing.T) {
	f := &cardOllama{reply: flowCard}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := cardCore(t, srv.URL)
	c.sessions["s1"] = &session{id: "s1", name: "Teach tile", pane: "%1"}
	r, err := c.materializeOp(ipc.MaterializeArgs{Explain: "What is this session doing?", Session: "s1", Wait: 10 * time.Second})
	if err != nil || !strings.HasPrefix(r.ID, "explain/s1/") || r.State != "fresh" {
		t.Fatalf("%+v, %v; want the session's own card for the question", r, err)
	}
	if msgs := f.lastAsked(); len(msgs) != 2 || !strings.Contains(msgs[1].Content, "Session: Teach tile") {
		t.Errorf("asked %+v; want the session's context", msgs)
	}
	if _, err := c.materializeOp(ipc.MaterializeArgs{Explain: "Why?", Session: "nobody"}); err == nil {
		t.Error("a question was answered for a session that is not there")
	}
}

func TestTheCardsThatExplainAreTrimmedToTheNewest(t *testing.T) {
	f := &cardOllama{reply: flowCard}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := cardCore(t, srv.URL)
	s := c.cardStore()
	old := time.Now().Add(-time.Hour)
	for i := range explainKept {
		r := materialize.Recipe{ID: explainIDs + strings.Repeat("o", i+1), Prompt: "p", Catalog: genui.Catalog, Kind: genui.KindExplain}
		_, _, rm, _ := s.Want(r, old)
		if _, _, err := s.Finish(*rm, json.RawMessage(flowCard), "qwen3:4b", nil, old.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	r, err := c.materializeOp(ipc.MaterializeArgs{Explain: "the newest", Wait: 10 * time.Second})
	if err != nil || r.State != "fresh" {
		t.Fatalf("%+v, %v", r, err)
	}
	if _, st := s.Peek(materialize.Recipe{ID: explainIDs + "o", Prompt: "p", Catalog: genui.Catalog, Kind: genui.KindExplain}, time.Now()); st != materialize.Cold {
		t.Errorf("the oldest card is %s; want it trimmed", st)
	}
	if _, st := s.Peek(recipeOf(ipc.MaterializeArgs{ID: r.ID, Prompt: "Explain this: the newest", Explain: "the newest"}), time.Now()); st != materialize.Fresh {
		t.Errorf("the newest card is %s; want it kept", st)
	}
}

func TestExplainTextKeepsLinesAndDropsControls(t *testing.T) {
	if got := explainText("\tfunc f() {\r\n\t\treturn 1 \x1b[2J\n}\n\n"); got != "func f() {\n  return 1 [2J\n}" {
		t.Errorf("explainText = %q", got)
	}
	long := strings.Repeat("é", ipc.MaxExplain)
	if got := explainText(long); len(got) > ipc.MaxExplain || !strings.HasPrefix(long, got) {
		t.Errorf("a long question kept %d bytes, cut mid-rune %v", len(got), !strings.HasPrefix(long, got))
	}
}

func TestAnExplanationIsBehindWhenTheSessionSaysMore(t *testing.T) {
	conv := ipc.Conversation{Items: []ipc.Item{
		{ID: "u1", Kind: ipc.ItemUser, Text: "how is a card kept?"},
		{ID: "t1", Kind: ipc.ItemText, Text: "In cards.json."},
	}}
	if got := explainInputs(conv); got != "t1" {
		t.Errorf("inputs %q; want the newest message", got)
	}
	// A step, a note or a helper is not a message: the card is not behind.
	conv.Items = append(conv.Items, ipc.Item{ID: "x1", Kind: ipc.ItemTool, Phrase: "Read cards.json"}, ipc.Item{ID: "n1", Kind: ipc.ItemNote, Text: "compacted"})
	if got := explainInputs(conv); got != "t1" {
		t.Errorf("after a step and a note, inputs %q; want them unchanged", got)
	}
	conv.Items = append(conv.Items, ipc.Item{ID: "u2", Kind: ipc.ItemUser, Text: "and when is it made again?"})
	if got := explainInputs(conv); got != "u2" {
		t.Errorf("after the person's message, inputs %q; want it", got)
	}
	if got := explainInputs(ipc.Conversation{}); got != "" {
		t.Errorf("an empty conversation gives %q", got)
	}
}
