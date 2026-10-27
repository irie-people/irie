// Package localmodel runs coding agents on local models (DESIGN §6.1,
// §6.3). It talks to an Ollama server, estimates what a model takes in
// memory at a context, weighs that against what the machine has, and checks
// a model before an agent is spawned on it, so that an agent never runs on a
// context too short for its instructions or on a machine that cannot hold
// its model.
package localmodel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Ollama is a client of one Ollama server's API.
type Ollama struct {
	URL  string       // such as http://127.0.0.1:11435
	HTTP *http.Client // nil = http.DefaultClient; every call takes a context
}

// APIError is a reply other than 200 OK.
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("ollama: HTTP %d", e.Status)
	}
	return "ollama: " + e.Msg
}

// NotFound reports whether err is Ollama's answer that a model is not there.
func NotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

func (o Ollama) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return http.DefaultClient
}

func (o Ollama) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(o.URL, "/")+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var e struct {
			Error any `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		msg := strings.TrimSpace(string(b))
		if json.Unmarshal(b, &e) == nil && e.Error != nil {
			// Ollama's own API says {"error":"…"}; its Anthropic API says
			// {"error":{"message":"…"}}.
			switch v := e.Error.(type) {
			case string:
				msg = v
			case map[string]any:
				if m, ok := v["message"].(string); ok {
					msg = m
				}
			}
		}
		return nil, &APIError{Status: resp.StatusCode, Msg: msg}
	}
	return resp, nil
}

func (o Ollama) call(ctx context.Context, method, path string, body, out any) error {
	resp, err := o.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Version is GET /api/version.
func (o Ollama) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	err := o.call(ctx, http.MethodGet, "/api/version", nil, &v)
	return v.Version, err
}

// Model is one model the server has, from GET /api/tags.
type Model struct {
	Name     string
	Size     int64 // on disk, in bytes: the weights
	Digest   string
	Modified time.Time
	Params   string // such as 9.7B
	Quant    string // such as Q4_K_M
	Remote   bool   // a cloud model, which runs on someone else's machine
}

// Models is GET /api/tags.
func (o Ollama) Models(ctx context.Context) ([]Model, error) {
	var v struct {
		Models []struct {
			Name       string    `json:"name"`
			Size       int64     `json:"size"`
			Digest     string    `json:"digest"`
			ModifiedAt time.Time `json:"modified_at"`
			Remote     string    `json:"remote_model"`
			RemoteHost string    `json:"remote_host"`
			Details    struct {
				ParameterSize     string `json:"parameter_size"`
				QuantizationLevel string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := o.call(ctx, http.MethodGet, "/api/tags", nil, &v); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(v.Models))
	for _, m := range v.Models {
		out = append(out, Model{
			Name: m.Name, Size: m.Size, Digest: m.Digest, Modified: m.ModifiedAt,
			Params: m.Details.ParameterSize, Quant: m.Details.QuantizationLevel,
			Remote: m.Remote != "" || m.RemoteHost != "",
		})
	}
	return out, nil
}

// Info is what POST /api/show says about a model.
type Info struct {
	Capabilities []string
	Context      int // the longest context the model was trained for; 0 when unknown
	// What its context cache takes per token: the K/V heads of each layer
	// that keeps a cache, and each head's key and value width.
	KVHeads  []int
	KeyLen   int
	ValueLen int
}

// Tools reports whether the model can call tools, which a coding agent
// needs.
func (i Info) Tools() bool { return slices.Contains(i.Capabilities, "tools") }

// Show is POST /api/show.
func (o Ollama) Show(ctx context.Context, model string) (Info, error) {
	var v struct {
		Capabilities []string       `json:"capabilities"`
		ModelInfo    map[string]any `json:"model_info"`
	}
	if err := o.call(ctx, http.MethodPost, "/api/show", map[string]any{"model": model}, &v); err != nil {
		return Info{}, err
	}
	info := parseModelInfo(v.ModelInfo)
	info.Capabilities = v.Capabilities
	return info, nil
}

// parseModelInfo reads a model's GGUF metadata, whose keys carry its
// architecture's name: qwen35.block_count and so on. head_count_kv is one
// number, or one per layer, 0 for a layer without a cache; a model whose
// layers alternate with ones that keep no cache says how often a full
// attention layer comes.
func parseModelInfo(mi map[string]any) Info {
	arch, _ := mi["general.architecture"].(string)
	num := func(key string) (float64, bool) {
		v, ok := mi[arch+"."+key].(float64)
		return v, ok
	}
	n := func(key string) int { v, _ := num(key); return int(v) }
	var info Info
	info.Context = n("context_length")
	layers := n("block_count")
	heads := n("attention.head_count")
	info.KeyLen, info.ValueLen = n("attention.key_length"), n("attention.value_length")
	if info.KeyLen == 0 && heads > 0 {
		info.KeyLen = n("embedding_length") / heads
	}
	if info.ValueLen == 0 {
		info.ValueLen = info.KeyLen
	}
	switch kv := mi[arch+".attention.head_count_kv"].(type) {
	case []any:
		for _, h := range kv {
			if f, ok := h.(float64); ok && f > 0 {
				info.KVHeads = append(info.KVHeads, int(f))
			}
		}
	case float64:
		every := max(n("full_attention_interval"), 1)
		for i := range layers {
			if (i+1)%every == 0 {
				info.KVHeads = append(info.KVHeads, int(kv))
			}
		}
	default:
		// No K/V head count: every layer keeps a cache, one per head.
		for range layers {
			info.KVHeads = append(info.KVHeads, heads)
		}
	}
	return info
}

// Loaded is a model in memory, from GET /api/ps.
type Loaded struct {
	Name    string
	Size    int64 // what it takes in memory, its context cache included
	VRAM    int64 // the part in GPU memory
	Context int   // the context it was loaded with
}

// Running is GET /api/ps.
func (o Ollama) Running(ctx context.Context) ([]Loaded, error) {
	var v struct {
		Models []struct {
			Name          string `json:"name"`
			Model         string `json:"model"`
			Size          int64  `json:"size"`
			SizeVRAM      int64  `json:"size_vram"`
			ContextLength int    `json:"context_length"`
		} `json:"models"`
	}
	if err := o.call(ctx, http.MethodGet, "/api/ps", nil, &v); err != nil {
		return nil, err
	}
	out := make([]Loaded, 0, len(v.Models))
	for _, m := range v.Models {
		name := m.Name
		if name == "" {
			name = m.Model
		}
		out = append(out, Loaded{Name: name, Size: m.Size, VRAM: m.SizeVRAM, Context: m.ContextLength})
	}
	return out, nil
}

// Load loads a model the way Claude Code will, with a one-token request to
// the Anthropic API Ollama serves (POST /v1/messages), so it gets the same
// context an agent gets.
func (o Ollama) Load(ctx context.Context, model string) error {
	return o.call(ctx, http.MethodPost, "/v1/messages", map[string]any{
		"model":      model,
		"max_tokens": 1,
		"messages":   []map[string]any{{"role": "user", "content": "Reply with OK."}},
	}, nil)
}

// Progress is one status line of a pull.
type Progress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
}

// Pull is POST /api/pull. It calls progress for each status Ollama streams.
func (o Ollama) Pull(ctx context.Context, model string, progress func(Progress)) error {
	resp, err := o.do(ctx, http.MethodPost, "/api/pull", map[string]any{"model": model, "stream": true})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var p struct {
			Progress
			Error string `json:"error"`
		}
		if err := json.Unmarshal(sc.Bytes(), &p); err != nil {
			continue
		}
		if p.Error != "" {
			return &APIError{Status: http.StatusOK, Msg: p.Error}
		}
		if progress != nil {
			progress(p.Progress)
		}
		if p.Status == "success" {
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("ollama: the pull ended without success")
}
