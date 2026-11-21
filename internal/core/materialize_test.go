package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// cardOllama is an Ollama with one local model for chat, which answers a
// chat with reply and counts the chats.
type cardOllama struct {
	mu    sync.Mutex
	reply string
	chats int
}

func (f *cardOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/version":
		fmt.Fprint(w, `{"version":"0.40.0"}`)
	case "/api/tags":
		fmt.Fprint(w, `{"models":[{"name":"qwen3:4b","size":2500000000,"details":{"parameter_size":"4.0B"}}]}`)
	case "/api/show":
		fmt.Fprint(w, `{"capabilities":["completion"],"model_info":{}}`)
	case "/api/chat":
		f.mu.Lock()
		f.chats++
		reply := f.reply
		f.mu.Unlock()
		b, _ := json.Marshal(map[string]any{"message": map[string]string{"role": "assistant", "content": reply}, "done": true})
		fmt.Fprintf(w, "%s\n", b)
	default:
		http.NotFound(w, r)
	}
}

func (f *cardOllama) say(reply string) { f.mu.Lock(); f.reply = reply; f.mu.Unlock() }
func (f *cardOllama) count() int       { f.mu.Lock(); defer f.mu.Unlock(); return f.chats }

func cardCore(t *testing.T, url string) *Core {
	t.Helper()
	cfg := config.Default()
	cfg.Local.URL = url
	return bareCore(t, cfg)
}

func TestACardIsMaterializedOnALocalModelAndKept(t *testing.T) {
	f := &cardOllama{reply: `{"title":"What changed","body":[{"type":"Text","text":"A fix and a feature."}]}`}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := cardCore(t, srv.URL)
	ask := ipc.MaterializeArgs{ID: "repo/irie/recent", Prompt: "What changed lately?", Context: "a1 fix: x\nb2 feat: y", Wait: 10 * time.Second}
	r, err := c.materializeOp(ask)
	if err != nil || r.State != "fresh" || !r.Began || r.By != "qwen3:4b" || !strings.Contains(string(r.Spec), "What changed") {
		t.Fatalf("first ask, waited for: %+v, %v; want a fresh card made by the local model", r, err)
	}
	ask.Wait = 0
	if r, _ := c.materializeOp(ask); r.State != "fresh" || r.Began || f.count() != 1 {
		t.Fatalf("asked again: %s, began %v, %d chats; want the kept card, and no second chat", r.State, r.Began, f.count())
	}
	f.say(`{"title":"What changed","body":[{"type":"Text","text":"A fix, a feature and a test."}]}`)
	ask.Context += "\nc3 test: z"
	r, _ = c.materializeOp(ask)
	if r.State != "refreshing" || !r.Began || !strings.Contains(string(r.Spec), "A fix and a feature.") {
		t.Fatalf("new context: %s, began %v, spec %s; want the old card on show while the next is made", r.State, r.Began, r.Spec)
	}
	ask.Wait = 10 * time.Second
	if r, _ := c.materializeOp(ask); r.State != "fresh" || !strings.Contains(string(r.Spec), "and a test") {
		t.Errorf("after the remake: %s, %s; want the new card", r.State, r.Spec)
	}
}

func TestACardWithNoLocalModelSaysWhy(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := cardCore(t, url)
	r, err := c.materializeOp(ipc.MaterializeArgs{ID: "x", Prompt: "Anything?", Wait: 10 * time.Second})
	if err != nil || r.State != "cold" || !strings.Contains(r.Err, "no Ollama answers") {
		t.Errorf("%+v, %v; want it cold, saying there is no local model", r, err)
	}
}

func TestMaterializeModelPicksTheModel(t *testing.T) {
	f := &cardOllama{reply: `{"title":"t","body":[]}`}
	srv := httptest.NewServer(f)
	defer srv.Close()
	cfg := config.Default()
	cfg.Local.URL, cfg.Materialize.Model = srv.URL, "bonsai-2:9b"
	c := bareCore(t, cfg)
	r, _ := c.materializeOp(ipc.MaterializeArgs{ID: "x", Prompt: "Anything?", Wait: 10 * time.Second})
	if r.State != "cold" || !strings.Contains(r.Err, "bonsai-2:9b") || f.count() != 0 {
		t.Errorf("%+v, %d chats; want no chat, and the model named as missing", r, f.count())
	}
}

func TestMaterializeRefusesAnAskItCannotKeep(t *testing.T) {
	c := cardCore(t, "http://127.0.0.1:1")
	for name, a := range map[string]ipc.MaterializeArgs{
		"no id":       {Prompt: "p"},
		"a long id":   {ID: strings.Repeat("x", ipc.MaxMaterializeID+1), Prompt: "p"},
		"no prompt":   {ID: "x"},
		"a long text": {ID: "x", Prompt: "p", Context: strings.Repeat("x", ipc.MaxMaterializeText+1)},
		"a gap below": {ID: "x", Prompt: "p", Gap: -time.Second},
	} {
		if _, err := c.materializeOp(a); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
	if r, err := c.materializeOp(ipc.MaterializeArgs{ID: "x", Forget: true}); err != nil || r.State != "cold" {
		t.Errorf("forget: %+v, %v", r, err)
	}
}
