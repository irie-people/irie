package localmodel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/testutil"
)

func TestReadGGUF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.gguf")
	kv := []int{0, 0, 0, 4, 0, 0, 0, 4}
	testutil.WriteGGUF(t, path, map[string]any{
		"general.architecture":              "qwen35moe",
		"general.alignment":                 32,
		"qwen35moe.block_count":             8,
		"qwen35moe.context_length":          262144,
		"qwen35moe.attention.head_count":    16,
		"qwen35moe.attention.head_count_kv": kv,
		"qwen35moe.attention.key_length":    256,
		"qwen35moe.attention.value_length":  256,
		"qwen35moe.expert_count":            128,
		"qwen35moe.rope.freq_base":          10000000.0,
		"tokenizer.chat_template":           "{% if tools %}x{% endif %}",
		"tokenizer.ggml.tokens":             []string{"a", "b", "c"},
		"prism.hadamard.version":            1,
	}, []testutil.GGUFTensor{
		{Name: "token_embd.weight", Type: 12, Bytes: 1000},
		{Name: "blk.0.attn_qkv.weight", Type: 12, Bytes: 64},
		{Name: "blk.0.ffn_up_exps.weight", Type: 143, Bytes: 4096},
		{Name: "blk.1.ffn_gate_up_exps.weight", Type: 12, Bytes: 2048},
		{Name: "output.weight", Type: 12, Bytes: 500},
	})
	g, err := ReadGGUF(path)
	if err != nil {
		t.Fatal(err)
	}
	if g.Arch() != "qwen35moe" || g.Num("block_count") != 8 || g.Num("expert_count") != 128 || len(g.Tensors) != 5 {
		t.Fatalf("gguf = %s %d %d %d", g.Arch(), g.Num("block_count"), g.Num("expert_count"), len(g.Tensors))
	}
	if _, ok := g.Meta["tokenizer.ggml.tokens"]; ok {
		t.Error("the tokenizer's tables were kept")
	}
	// A tensor's bytes run to the next one's data, aligned to 32.
	want := map[string]int64{"token_embd.weight": 1024, "blk.0.attn_qkv.weight": 64, "blk.0.ffn_up_exps.weight": 4096, "blk.1.ffn_gate_up_exps.weight": 2048, "output.weight": 512}
	for _, tn := range g.Tensors {
		if tn.Bytes != want[tn.Name] {
			t.Errorf("%s takes %d bytes, want %d", tn.Name, tn.Bytes, want[tn.Name])
		}
	}
	if g.Weights() != 1024+64+4096+2048+512 {
		t.Errorf("weights = %d", g.Weights())
	}
	layers, experts, rest := g.Layers()
	if len(layers) != 8 || layers[0] != 64+4096 || experts[0] != 4096 || experts[1] != 2048 || rest != 1024+512 {
		t.Errorf("layers = %v, experts = %v, rest = %d", layers, experts, rest)
	}
	info := g.Info()
	if !info.Tools() || info.Context != 262144 || len(info.KVHeads) != 2 || info.KeyLen != 256 {
		t.Errorf("info = %+v", info)
	}
	if rot, types := g.Prism(); !rot || !types {
		t.Errorf("prism = %v, %v", rot, types)
	}

	// Not a GGUF file.
	bad := filepath.Join(t.TempDir(), "bad.gguf")
	os.WriteFile(bad, []byte("GGML not this"), 0o600)
	if _, err := ReadGGUF(bad); err == nil || !strings.Contains(err.Error(), "not a GGUF file") {
		t.Errorf("bad file: %v", err)
	}
}

func TestBuilds(t *testing.T) {
	for _, c := range []struct {
		out, bin string
		number   int
		prism    bool
	}{
		{"version: 10180 (11b068d06)\nbuilt with AppleClang 16.0.0.16000026 for Darwin arm64\n", "/opt/homebrew/bin/llama-server", 10180, false},
		{"version: 0.7.0 (build 11600, commit abc1234)\n", "/usr/local/bin/llama-server", 11600, false},
		{"version: 10777 (e3de715)\n", "/home/alice/llama-prism-b10777-e3de715/llama-server", 10777, true},
		{"something else\n", "/usr/bin/llama-server", 0, false},
	} {
		b := ParseBuild(c.out, c.bin)
		if b.Number != c.number || b.Prism != c.prism {
			t.Errorf("%q at %s = %+v", c.out, c.bin, b)
		}
	}

	ternary := GGUF{Meta: map[string]any{"prism.hadamard.version": 1.0}, Tensors: []Tensor{{Type: 42}}}
	packed := GGUF{Meta: map[string]any{"prism.hadamard.version": 1.0}, Tensors: []Tensor{{Type: 143}}}
	plain := GGUF{Meta: map[string]any{}, Tensors: []Tensor{{Type: 12}}}
	for _, c := range []struct {
		b             Build
		g             GGUF
		problem, warn string
	}{
		{Build{Number: 10180}, ternary, "writes nonsense", ""},
		{Build{Number: 11556}, ternary, "", ""},
		{Build{}, ternary, "", "cannot tell"},
		{Build{Number: 12000}, packed, "only PrismML's", ""},
		{Build{Number: 10777, Prism: true}, packed, "", ""},
		{Build{Number: 10180}, plain, "", ""},
	} {
		p, w := c.b.Runs(c.g)
		if (c.problem == "") != (p == "") || !strings.Contains(p, c.problem) || (c.warn == "") != (w == "") || !strings.Contains(w, c.warn) {
			t.Errorf("%s runs %v: %q, %q", c.b, c.g.Tensors, p, w)
		}
	}
}

func TestLlamaClient(t *testing.T) {
	var ready atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			if !ready.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"error":{"code":503,"message":"Loading model"}}`))
				return
			}
			w.Write([]byte(`{"status":"ok"}`))
		case "/props":
			w.Write([]byte(`{"default_generation_settings":{"n_ctx":65536},"total_slots":1,"model_path":"/m.gguf","build_info":"b10180-11b068d06"}`))
		}
	}))
	defer srv.Close()
	l := Llama{URL: srv.URL}
	ctx := context.Background()
	if ok, err := l.Ready(ctx); ok || err != nil {
		t.Errorf("ready while loading = %v, %v", ok, err)
	}
	time.AfterFunc(300*time.Millisecond, func() { ready.Store(true) })
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := l.WaitReady(wctx, nil); err != nil {
		t.Fatal(err)
	}
	p, err := l.Props(ctx)
	if err != nil || p.Context != 65536 || p.Slots != 1 || p.Model != "/m.gguf" || p.Build != "b10180-11b068d06" {
		t.Errorf("props = %+v, %v", p, err)
	}
	// A server that exits while it loads stops the wait.
	ready.Store(false)
	if err := l.WaitReady(ctx, func() bool { return true }); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Errorf("wait on a gone server: %v", err)
	}
	if _, err := (Llama{URL: "http://127.0.0.1:1"}).Ready(ctx); err == nil {
		t.Error("an unreachable server is ready")
	}
}

func TestOllamaStore(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, "manifests", rel)
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte(body), 0o600)
	}
	write("registry.ollama.ai/library/bonsai-2/27b", `{"layers":[{"mediaType":"application/vnd.ollama.image.projector","digest":"sha256:pp"},{"mediaType":"application/vnd.ollama.image.model","digest":"sha256:aa"}]}`)
	write("registry.ollama.ai/alice/coder/latest", `{"layers":[{"mediaType":"application/vnd.ollama.image.model","digest":"sha256:bb"}]}`)
	write("hf.co/alice/model/Q4_K_M", `{"layers":[{"mediaType":"application/vnd.ollama.image.model","digest":"sha256:cc"}]}`)
	write("registry.ollama.ai/library/odd/x", `{"layers":[{"mediaType":"application/vnd.ollama.image.model","digest":"sha256:../../etc"}]}`)
	write("registry.ollama.ai/library/broken/x", `not json`)
	ms, err := OllamaStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range ms {
		got[m.Name] = m.Path
	}
	want := map[string]string{
		"bonsai-2:27b":             filepath.Join(dir, "blobs", "sha256-aa"),
		"alice/coder:latest":       filepath.Join(dir, "blobs", "sha256-bb"),
		"hf.co/alice/model:Q4_K_M": filepath.Join(dir, "blobs", "sha256-cc"),
	}
	if len(got) != len(want) {
		t.Errorf("store = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if ms, err := OllamaStore(filepath.Join(dir, "none")); err != nil || len(ms) != 0 {
		t.Errorf("no store = %v, %v", ms, err)
	}
	if LlamaRank("Ternary-Bonsai-2-27B-PTQ1_0") != 0 || LlamaRank("bonsai-2:27b") <= LlamaRank("Ternary-Bonsai-2-27B-PQ2_0") || LlamaRank("chat:1b") != -1 {
		t.Error("llama ranks")
	}
}

func TestFindLlamaServer(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindLlamaServer(dir + "/llama-server"); err == nil || !strings.Contains(err.Error(), "not an executable") {
		t.Errorf("a missing configured binary: %v", err)
	}
	bin := filepath.Join(dir, "llama-server")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	if got, err := FindLlamaServer(bin); err != nil || got != bin {
		t.Errorf("configured = %s, %v", got, err)
	}
	t.Setenv("PATH", dir)
	if got, err := FindLlamaServer(""); err != nil || got != bin {
		t.Errorf("on PATH = %s, %v", got, err)
	}
}
