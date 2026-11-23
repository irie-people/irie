package localmodel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// chatServer is an Ollama with qwen3.5:9b for chat, loaded or not.
func chatServer(t *testing.T, loaded bool) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"version":"0.40.16"}`) })
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[{"name":"qwen3.5:9b","size":6590000000,"details":{"parameter_size":"9.7B"}}]}`)
	})
	mux.HandleFunc("POST /api/show", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"capabilities":["completion","tools"],"model_info":{"general.architecture":"qwen35"}}`)
	})
	mux.HandleFunc("GET /api/ps", func(w http.ResponseWriter, r *http.Request) {
		if loaded {
			fmt.Fprint(w, `{"models":[{"name":"qwen3.5:9b","size":7120000000,"context_length":8192}]}`)
			return
		}
		fmt.Fprint(w, `{"models":[]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAChatModelIsLoadedOnOneServerOnly(t *testing.T) {
	ctx := context.Background()
	own, users := chatServer(t, false), chatServer(t, true)
	// On 2026-11-22 one model was loaded twice, on Irie's own Ollama and on
	// the user's, which share their models. The one that has it loaded
	// takes the chat, named or picked, though it comes second.
	for _, want := range []string{"qwen3.5:9b", ""} {
		m, err := FindChatModel(ctx, []string{own, users}, want)
		if err != nil || m.Name != "qwen3.5:9b" || m.URL != users {
			t.Errorf("want %q: %+v, %v; want qwen3.5:9b where it is loaded, %s", want, m, err, users)
		}
	}
	// Loaded on neither: the first server's, as before.
	if m, err := FindChatModel(ctx, []string{own, chatServer(t, false)}, "qwen3.5:9b"); err != nil || m.URL != own {
		t.Errorf("%+v, %v; want the first server, %s", m, err, own)
	}
}

func TestNoServerLeftMeansLocalModelsAreOffHere(t *testing.T) {
	_, err := FindChatModel(context.Background(), nil, "bonsai-2:9b")
	if !errors.Is(err, ErrNoChatModel) || !errors.Is(err, ErrOffHere) {
		t.Errorf("%v; want no chat model, as local models are off here", err)
	}
}
