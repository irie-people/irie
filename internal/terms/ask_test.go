package terms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// TestAskAnswersFromThePoints: a question on a topic the terms speak to
// gets the point's words and its clause; one they don't speak to says so
// with the closest passages; "what's the catch" gets what is against you;
// the date and the length answer for themselves; an empty question asks
// for one.
func TestAskAnswersFromThePoints(t *testing.T) {
	d := fixture(t)
	cases := []struct {
		q, topic, says string
		found          bool
	}{
		{"Can I get a refund?", "no-refunds", "they don't give refunds", true},
		{"can i sue them if something goes wrong", "arbitration", "can't take it to court", true},
		{"Who owns my photos?", "you-own", "You keep ownership", true},
		{"do they sell my data", "sell-data", "don't sell your personal information", true},
		{"How do I cancel my subscription?", "you-can-leave", "You can close your account", true},
		{"What's the catch?", "", "What to watch out for:", true},
		{"how long is this", "", "words, about", true},
		{"when was it last updated", "", "March 3, 2026", true},
		{"Do they read my private messages?", "monitor", "don't say anything", false},
	}
	for _, c := range cases {
		a := d.Ask(c.q)
		if a.Found != c.found || !strings.Contains(a.Says, c.says) || a.By != Code {
			t.Errorf("%q: found %v, by %q, says %q; want %v and %q", c.q, a.Found, a.By, a.Says, c.found, c.says)
		}
		if c.topic != "" && (len(a.Topics) == 0 || a.Topics[0] != c.topic) && !strings.Contains(c.q, "photos") {
			t.Errorf("%q: topics %v, want %s first", c.q, a.Topics, c.topic)
		}
		if c.found && c.topic != "" && len(a.Quotes) == 0 {
			t.Errorf("%q: no quotes", c.q)
		}
	}
	if a := d.Ask("Who owns my photos?"); !strings.HasPrefix(a.Says, "You keep ownership") {
		t.Errorf("ownership comes first: %q", a.Says)
	}
	if a := d.Ask("  "); a.Found || !strings.Contains(a.Says, "Ask anything") {
		t.Errorf("empty: %+v", a)
	}
	if a := d.Ask("zebra quantum xylophone"); a.Found || len(a.Quotes) != 0 || !strings.Contains(a.Says, "can't find") {
		t.Errorf("nothing: %+v", a)
	}
	sug := d.Suggestions()
	if len(sug) == 0 || len(sug) > 4 || sug[0] != "What's the catch?" || !slices.Contains(sug, "Can I get a refund?") {
		t.Errorf("suggestions %q", sug)
	}
}

// TestPassagesForAModel: the passages a model answers from are the points'
// clauses on the question's topics first, then the closest by their words.
func TestPassagesForAModel(t *testing.T) {
	d := fixture(t)
	ps := d.Passages("Can I get my money back?", 4)
	if len(ps) == 0 || !strings.Contains(ps[0].Text, "non-refundable") {
		t.Fatalf("passages %+v", ps)
	}
	if len(ps) > 4 {
		t.Errorf("%d passages, at most 4", len(ps))
	}
}

// TestStemAndTokens: the words of a search, without the common ones, cut
// to their stems.
func TestStemAndTokens(t *testing.T) {
	got := tokens("The refunds were terminated, and they're refundable!")
	want := []string{"refund", "terminat", "refundabl"}
	if !slices.Equal(got, want) {
		t.Errorf("tokens %q, want %q", got, want)
	}
}

// fakeOllama answers the parts of Ollama's API the terms' model uses:
// its version, its models, what each can do, and a chat streamed a word at
// a time, which it notes.
type fakeOllama struct {
	models []map[string]any
	caps   map[string][]string
	reply  string
	asked  []string
	cut    bool // the chat stops before its last word
}

func (f *fakeOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/version":
		fmt.Fprint(w, `{"version":"0.40.0"}`)
	case "/api/tags":
		json.NewEncoder(w).Encode(map[string]any{"models": f.models})
	case "/api/show":
		var v struct{ Model string }
		json.NewDecoder(r.Body).Decode(&v)
		json.NewEncoder(w).Encode(map[string]any{"capabilities": f.caps[v.Model], "model_info": map[string]any{}})
	case "/api/chat":
		var v struct {
			Model    string
			Messages []struct{ Content string }
			Think    *bool
		}
		json.NewDecoder(r.Body).Decode(&v)
		f.asked = append(f.asked, v.Model+"|"+v.Messages[len(v.Messages)-1].Content)
		for _, word := range strings.SplitAfter(f.reply, " ") {
			b, _ := json.Marshal(map[string]any{"message": map[string]string{"role": "assistant", "content": word}, "done": false})
			fmt.Fprintf(w, "%s\n", b)
		}
		if !f.cut {
			fmt.Fprint(w, `{"message":{"role":"assistant","content":""},"done":true}`+"\n")
		}
	default:
		http.NotFound(w, r)
	}
}

// TestALocalModelWordsTheAnswer: the model found is the biggest that
// chats, never a cloud one or one for embeddings; it words the answer from
// the passages Irie's code found, which the answer quotes; its words come
// as they are written; and with no Ollama here, there is no model.
func TestALocalModelWordsTheAnswer(t *testing.T) {
	f := &fakeOllama{
		models: []map[string]any{
			{"name": "embeddinggemma:300m", "size": 300 << 20, "details": map[string]any{"parameter_size": "300M"}},
			{"name": "big-cloud:cloud", "size": 1000, "remote_host": "https://ollama.com", "details": map[string]any{"parameter_size": "600B"}},
			{"name": "qwen-small:4b", "size": 3 << 30, "details": map[string]any{"parameter_size": "4.0B"}},
			{"name": "qwen-mid:9b", "size": 6 << 30, "details": map[string]any{"parameter_size": "9.1B"}},
			{"name": "huge:70b", "size": 40 << 30, "details": map[string]any{"parameter_size": "70B"}},
		},
		caps: map[string][]string{
			"embeddinggemma:300m": {"embedding"},
			"big-cloud:cloud":     {"completion"},
			"qwen-small:4b":       {"completion"},
			"qwen-mid:9b":         {"completion", "thinking"},
			"huge:70b":            {"completion"},
		},
		reply: "No: payments are final [1], except where the law says otherwise.",
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	ctx := context.Background()
	m, err := FindModel(ctx, []string{"http://127.0.0.1:1", srv.URL}, "")
	if err != nil || m.Name != "qwen-mid:9b" || !m.think {
		t.Fatalf("FindModel: %+v, %v", m, err)
	}
	if m, err := FindModel(ctx, []string{srv.URL}, "qwen-small:4b"); err != nil || m.Name != "qwen-small:4b" {
		t.Errorf("FindModel by name: %+v, %v", m, err)
	}
	if _, err := FindModel(ctx, []string{srv.URL}, "not-here:1b"); !errors.Is(err, ErrNoModel) {
		t.Errorf("a model that isn't here: %v", err)
	}
	if _, err := FindModel(ctx, []string{"http://127.0.0.1:1"}, ""); !errors.Is(err, ErrNoModel) {
		t.Errorf("no Ollama: %v", err)
	}

	d := fixture(t)
	var parts []string
	a, err := d.AskModel(ctx, m, "Can I get a refund?", func(s string) { parts = append(parts, s) })
	if err != nil {
		t.Fatal(err)
	}
	if a.Says != f.reply || a.By != "qwen-mid:9b" || !a.Found || len(parts) < 3 {
		t.Errorf("answer %+v, parts %q", a, parts)
	}
	if len(a.Quotes) == 0 || !strings.Contains(a.Quotes[0].Text, "non-refundable") {
		t.Errorf("quotes %+v", a.Quotes)
	}
	if len(f.asked) != 1 || !strings.Contains(f.asked[0], "[1]") || !strings.Contains(f.asked[0], "Question: Can I get a refund?") || !strings.Contains(f.asked[0], "non-refundable") {
		t.Errorf("the model was asked %q", f.asked)
	}
	if got := stripThinking("<think>hmm</think>Yes."); got != "Yes." {
		t.Errorf("stripThinking: %q", got)
	}
	// A reply that ends before its last word is no answer: Irie's code
	// answers, and says why.
	f.cut = true
	a, err = d.AskModel(ctx, m, "Can I get a refund?", nil)
	if err == nil || a.By != Code {
		t.Errorf("a cut reply: %+v, %v", a, err)
	}
}
