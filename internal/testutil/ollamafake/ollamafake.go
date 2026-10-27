// Package ollamafake is a stand-in for an Ollama server in Irie's tests: the
// parts of Ollama's API Irie uses (version, tags, show, ps, the Anthropic
// messages endpoint and pull), as Ollama's API reference describes them, for
// a few synthetic models. A model it loads gets the server's context, cut to
// the model's own.
package ollamafake

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Model is one model of the server.
type Model struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`    // its weights, in bytes
	Tools   bool   `json:"tools"`   // it can call tools
	Context int    `json:"context"` // the longest context it takes; 0 = 262144
}

// Server answers Ollama's API.
type Server struct {
	Context int // what a loaded model gets, as OLLAMA_CONTEXT_LENGTH says; 0 = 4096

	mu     sync.Mutex
	models []Model
	loaded map[string]bool
	Loads  int // POST /v1/messages calls
}

// New returns a server with models.
func New(context int, models []Model) *Server {
	return &Server{Context: context, models: models, loaded: map[string]bool{}}
}

// cache is the context cache a loaded model takes besides its weights.
const cache = 64 << 20

func (s *Server) find(name string) *Model {
	for i := range s.models {
		if s.models[i].Name == name {
			return &s.models[i]
		}
	}
	return nil
}

func (s *Server) context(m Model) int {
	ctx := s.Context
	if ctx == 0 {
		ctx = 4096
	}
	if max := m.Context; max > 0 && max < ctx {
		ctx = max
	}
	return ctx
}

func reply(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }

// Handler serves the API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"version": "0.0.0-fake"})
	})
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		ms := []map[string]any{}
		for _, m := range s.models {
			ms = append(ms, map[string]any{
				"name": m.Name, "model": m.Name, "size": m.Size, "digest": "fake-" + m.Name,
				"details": map[string]any{"format": "gguf", "parameter_size": "1B", "quantization_level": "Q4_K_M"},
			})
		}
		reply(w, map[string]any{"models": ms})
	})
	mux.HandleFunc("POST /api/show", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model string }
		json.NewDecoder(r.Body).Decode(&req)
		s.mu.Lock()
		defer s.mu.Unlock()
		m := s.find(req.Model)
		if m == nil {
			w.WriteHeader(http.StatusNotFound)
			reply(w, map[string]string{"error": "model '" + req.Model + "' not found"})
			return
		}
		caps := []string{"completion"}
		if m.Tools {
			caps = append(caps, "tools")
		}
		max := m.Context
		if max == 0 {
			max = 262144
		}
		reply(w, map[string]any{"capabilities": caps, "model_info": map[string]any{
			"general.architecture": "fake", "fake.block_count": 8, "fake.context_length": max,
			"fake.attention.head_count": 8, "fake.attention.head_count_kv": 2, "fake.embedding_length": 1024,
		}})
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.Loads++
		if s.find(req.Model) == nil {
			w.WriteHeader(http.StatusNotFound)
			reply(w, map[string]any{"type": "error", "error": map[string]string{"type": "not_found_error", "message": "model '" + req.Model + "' not found"}})
			return
		}
		// One model at a time, as Irie runs its own server.
		s.loaded = map[string]bool{req.Model: true}
		reply(w, map[string]any{"id": "msg_fake", "type": "message", "role": "assistant", "model": req.Model,
			"content": []map[string]string{{"type": "text", "text": "OK"}}, "stop_reason": "max_tokens"})
	})
	mux.HandleFunc("GET /api/ps", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		ms := []map[string]any{}
		for _, m := range s.models {
			if s.loaded[m.Name] {
				size := m.Size + cache
				ms = append(ms, map[string]any{"name": m.Name, "model": m.Name, "size": size, "size_vram": size, "context_length": s.context(m)})
			}
		}
		reply(w, map[string]any{"models": ms})
	})
	mux.HandleFunc("POST /api/pull", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model string }
		json.NewDecoder(r.Body).Decode(&req)
		reply(w, map[string]string{"status": "pulling manifest"})
		reply(w, map[string]any{"status": "pulling fake", "digest": "fake", "total": 100, "completed": 100})
		reply(w, map[string]string{"status": "success"})
		s.mu.Lock()
		if s.find(req.Model) == nil {
			s.models = append(s.models, Model{Name: req.Model, Size: 100 << 20, Tools: true})
		}
		s.mu.Unlock()
	})
	return mux
}
