package genui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/terms"
)

const store = `{"title":"Three ways to keep sessions","body":[` +
	`{"type":"Table","columns":["Store","Writes/s"],"rows":[["SQLite (WAL)",5000],["JSON files","~200"]]},` +
	`{"type":"Facts","facts":[{"label":"Branch","value":"main"},{"label":"Tests","value":212}]},` +
	`{"type":"List","items":[{"title":"Store","detail":"built","status":"ok"},{"title":"Core","status":"warn"}]},` +
	`{"type":"Progress","label":"Steps","value":"2","total":5},` +
	`{"type":"Divider"},` +
	`{"type":"Text","style":"caption","text":"Rough figures; measure before choosing."}]}`

func TestCheckTakesACardOfTheCatalog(t *testing.T) {
	c, err := Check([]byte(store))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Body[0].Rows[0][1]; got != "5000" {
		t.Errorf("a number in a cell reads %q, want it as written", got)
	}
	if got := c.Body[3].Value; got != 2 {
		t.Errorf("a progress value written \"2\" reads %v", got)
	}
}

func TestCheckSaysWhatToFix(t *testing.T) {
	many := `{"title":"t","body":[{"type":"List","items":[` + strings.Repeat(`{"title":"x"},`, MaxRows) + `{"title":"x"}]}]}`
	for name, tc := range map[string]struct{ spec, says string }{
		"no title":       {`{"body":[]}`, "no title"},
		"a part unknown": {`{"title":"t","body":[{"type":"Chart"}]}`, "use Text, List, Facts, Table, Progress, Divider"},
		"too many rows":  {many, "more than 100 rows"},
		"not an object":  {`["title"]`, "not one JSON object"},
		"a nested cell":  {`{"title":"t","body":[{"type":"Table","rows":[[{"a":1}]]}]}`, "words or a number"},
		"too big":        {`{"title":"` + strings.Repeat("x", MaxBytes) + `"}`, "at most"},
	} {
		if _, err := Check([]byte(tc.spec)); err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: %v; want it to say %q", name, err, tc.says)
		}
	}
}

func TestCardJSONTakesTheObjectFromAReply(t *testing.T) {
	for reply, want := range map[string]string{
		`{"title": "a", "body": []}`:                                       `{"title":"a","body":[]}`,
		"Here it is:\n```json\n{\"title\": \"a\"}\n```":                    `{"title":"a"}`,
		"<think>the title {maybe}</think>{\"title\": \"b\"}":               `{"title":"b"}`,
		"{\"title\": \"a {nested}\", \"body\": [{\"type\": \"Divider\"}]}": `{"title":"a {nested}","body":[{"type":"Divider"}]}`,
	} {
		got, err := cardJSON(reply)
		if err != nil || string(got) != want {
			t.Errorf("%q: %s, %v; want %s", reply, got, err, want)
		}
	}
	for _, reply := range []string{"I can't make that card.", `{"title": "a",`} {
		if got, err := cardJSON(reply); err == nil {
			t.Errorf("%q: took %s from it", reply, got)
		}
	}
}

// fakeOllama answers a chat with reply, a word at a time, as Ollama
// streams it, and notes what it was asked.
type fakeOllama struct {
	reply string
	asked struct {
		Model    string
		Format   string
		Think    *bool
		Messages []struct{ Role, Content string }
	}
}

func (f *fakeOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/chat" {
		http.NotFound(w, r)
		return
	}
	json.NewDecoder(r.Body).Decode(&f.asked)
	for _, word := range strings.SplitAfter(f.reply, " ") {
		b, _ := json.Marshal(map[string]any{"message": map[string]string{"role": "assistant", "content": word}, "done": false})
		fmt.Fprintf(w, "%s\n", b)
	}
	fmt.Fprint(w, `{"message":{"role":"assistant","content":""},"done":true}`+"\n")
}

func TestALocalModelMakesTheCard(t *testing.T) {
	f := &fakeOllama{reply: "```json\n" + store + "\n```"}
	srv := httptest.NewServer(f)
	defer srv.Close()
	m := terms.Model{URL: srv.URL, Name: "qwen3:4b"}
	spec, err := Make(context.Background(), m, "Which store should keep sessions?", "SQLite does 5000 writes a second.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check(spec); err != nil || strings.ContainsAny(string(spec), "\n`") {
		t.Errorf("the card made: %s, %v; want it compacted and whole", spec, err)
	}
	if f.asked.Format != "json" || f.asked.Model != "qwen3:4b" {
		t.Errorf("asked %q for format %q; want json", f.asked.Model, f.asked.Format)
	}
	user := f.asked.Messages[len(f.asked.Messages)-1].Content
	if !strings.Contains(user, "Which store") || !strings.Contains(user, "5000 writes") {
		t.Errorf("the model was not given the prompt and its context: %q", user)
	}
	f.reply = `{"title": "Three ways", "body": [{"type": "Chart"}]}`
	if _, err := Make(context.Background(), m, "Which store?", ""); err == nil || !strings.Contains(err.Error(), "qwen3:4b") {
		t.Errorf("a card with a part the catalog lacks: %v; want it refused, naming the model", err)
	}
}

func TestTextDrawsACard(t *testing.T) {
	c, err := Check([]byte(store))
	if err != nil {
		t.Fatal(err)
	}
	got := Text(c, 60)
	for _, want := range []string{
		"Three ways to keep sessions\n───────────────────────────\n",
		"Store         Writes/s\nSQLite (WAL)  5000\nJSON files    ~200\n",
		"Branch  main\nTests   212\n",
		"✓ Store — built\n! Core\n",
		"Steps  ████████░░░░░░░░░░░░ 2 of 5\n",
		"  Rough figures; measure before choosing.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the drawing lacks %q:\n%s", want, got)
		}
	}
}

func TestTextShowsWhatCouldRewriteTheTerminal(t *testing.T) {
	c := Card{Title: "a\x1b[2Jb", Body: []Part{{Type: PartText, Text: "left\u202eright"}}}
	got := Text(c, 40)
	if strings.ContainsAny(got, "\x1b\u202e") || !strings.Contains(got, "a^[[2Jb") || !strings.Contains(got, "left^?right") {
		t.Errorf("drew %q; want the escape and the bidi mark shown, not sent", got)
	}
}

func TestBar(t *testing.T) {
	for _, tc := range []struct {
		value, total float64
		want         string
	}{
		{0, 10, "░░░░░░░░░░"},
		{5, 10, "█████░░░░░"},
		{10, 10, "██████████"},
		{12, 10, "██████████"},
		{-3, 10, "░░░░░░░░░░"},
		{3, 0, "░░░░░░░░░░"},
	} {
		if got := bar(tc.value, tc.total, 10); got != tc.want {
			t.Errorf("bar(%v, %v) = %q, want %q", tc.value, tc.total, got, tc.want)
		}
	}
}

func TestWrapKeepsToTheWidth(t *testing.T) {
	got := wrap("one two three four supercalifragilistic", 10)
	want := []string{"one two", "three four", "supercalif", "ragilistic"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("wrap: %q, want %q", got, want)
	}
}
