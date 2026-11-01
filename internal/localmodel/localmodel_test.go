package localmodel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fakeModel is one model of fakeServer.
type fakeModel struct {
	name   string
	size   int64
	caps   []string
	info   map[string]any // model_info
	remote bool
}

// fakeServer answers the parts of Ollama's API that Irie uses. A model it
// loads takes size plus cacheBytes and gets the server's context, cut to
// the model's own.
type fakeServer struct {
	mu         sync.Mutex
	models     []fakeModel
	loaded     map[string]bool
	context    int
	cacheBytes int64
	vramShare  float64 // the part of a loaded model in GPU memory; 0 = all
	loads      int
	failLoad   string
}

func qwenInfo() map[string]any {
	kv := make([]any, 32)
	for i := range kv {
		kv[i] = 0.0
		if i%4 == 3 {
			kv[i] = 4.0
		}
	}
	return map[string]any{
		"general.architecture":           "qwen35",
		"qwen35.block_count":             32.0,
		"qwen35.context_length":          262144.0,
		"qwen35.attention.head_count":    16.0,
		"qwen35.attention.head_count_kv": kv,
		"qwen35.attention.key_length":    256.0,
		"qwen35.attention.value_length":  256.0,
		"qwen35.embedding_length":        4096.0,
		"qwen35.full_attention_interval": 4.0,
	}
}

func (f *fakeServer) handler() http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	find := func(name string) *fakeModel {
		for i := range f.models {
			if f.models[i].name == name {
				return &f.models[i]
			}
		}
		return nil
	}
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) { reply(w, map[string]string{"version": "0.40.6"}) })
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var ms []map[string]any
		for _, m := range f.models {
			e := map[string]any{"name": m.name, "model": m.name, "size": m.size, "digest": "d-" + m.name, "details": map[string]any{"parameter_size": "9.7B", "quantization_level": "Q4_K_M"}}
			if m.remote {
				e["remote_model"], e["remote_host"] = m.name, "https://ollama.com:443"
			}
			ms = append(ms, e)
		}
		reply(w, map[string]any{"models": ms})
	})
	mux.HandleFunc("POST /api/show", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model string }
		json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		m := find(req.Model)
		if m == nil {
			w.WriteHeader(http.StatusNotFound)
			reply(w, map[string]string{"error": "model '" + req.Model + "' not found"})
			return
		}
		reply(w, map[string]any{"capabilities": m.caps, "model_info": m.info})
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.loads++
		if f.failLoad != "" || find(req.Model) == nil || req.MaxTokens != 1 {
			w.WriteHeader(http.StatusInternalServerError)
			reply(w, map[string]any{"type": "error", "error": map[string]string{"type": "api_error", "message": "model failed to load"}})
			return
		}
		if f.loaded == nil {
			f.loaded = map[string]bool{}
		}
		f.loaded[req.Model] = true
		reply(w, map[string]any{"type": "message", "role": "assistant", "content": []map[string]string{{"type": "text", "text": "OK"}}})
	})
	mux.HandleFunc("GET /api/ps", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var ms []map[string]any
		for _, m := range f.models {
			if !f.loaded[m.name] {
				continue
			}
			ctx := f.context
			if c, _ := m.info[m.info["general.architecture"].(string)+".context_length"].(float64); c > 0 && int(c) < ctx {
				ctx = int(c)
			}
			size := m.size + f.cacheBytes
			vram := size
			if f.vramShare > 0 {
				vram = int64(float64(size) * f.vramShare)
			}
			ms = append(ms, map[string]any{"name": m.name, "model": m.name, "size": size, "size_vram": vram, "context_length": ctx})
		}
		reply(w, map[string]any{"models": ms})
	})
	mux.HandleFunc("POST /api/pull", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model string }
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model == "nope:1b" {
			reply(w, map[string]string{"status": "pulling manifest"})
			reply(w, map[string]string{"error": "pull model manifest: file does not exist"})
			return
		}
		for _, s := range []map[string]any{
			{"status": "pulling manifest"},
			{"status": "pulling abc", "digest": "abc", "total": 100, "completed": 50},
			{"status": "pulling abc", "digest": "abc", "total": 100, "completed": 100},
			{"status": "success"},
		} {
			reply(w, s)
		}
		f.mu.Lock()
		f.models = append(f.models, fakeModel{name: req.Model, size: 100, caps: []string{"completion", "tools"}, info: qwenInfo()})
		f.mu.Unlock()
	})
	return mux
}

func serve(t *testing.T, f *fakeServer) Ollama {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return Ollama{URL: srv.URL}
}

const gb = 1_000_000_000

// roomy is a machine with memory to spare: 64 GiB, half of it free.
var roomy = Machine{Total: 64 * GiB, Available: 32 * GiB}

func want(minContext int) Want {
	return Want{MinContext: minContext, Context: 65536, KVCache: "q8_0", Parallel: 1, Own: true, Budget: 40 * GiB, Machine: roomy}
}

func TestClientReadsTheAPI(t *testing.T) {
	f := &fakeServer{context: 65536, models: []fakeModel{
		{name: "qwen3.5:9b", size: 6_550_825_373, caps: []string{"completion", "tools", "thinking"}, info: qwenInfo()},
		{name: "big:cloud", size: 384, caps: []string{"completion", "tools"}, info: qwenInfo(), remote: true},
	}}
	o := serve(t, f)
	ctx := context.Background()
	if v, err := o.Version(ctx); err != nil || v != "0.40.6" {
		t.Fatalf("version = %q, %v", v, err)
	}
	ms, err := o.Models(ctx)
	if err != nil || len(ms) != 2 {
		t.Fatalf("models = %+v, %v", ms, err)
	}
	if m := ms[0]; m.Name != "qwen3.5:9b" || m.Size != 6_550_825_373 || m.Params != "9.7B" || m.Quant != "Q4_K_M" || m.Remote {
		t.Errorf("models[0] = %+v", m)
	}
	if !ms[1].Remote {
		t.Errorf("a cloud model is not remote: %+v", ms[1])
	}
	info, err := o.Show(ctx, "qwen3.5:9b")
	if err != nil || !info.Tools() || info.Context != 262144 {
		t.Fatalf("show = %+v, %v", info, err)
	}
	if _, err := o.Show(ctx, "missing:1b"); !NotFound(err) || !strings.Contains(err.Error(), "not found") {
		t.Errorf("show of a missing model: %v", err)
	}
	if err := o.Load(ctx, "qwen3.5:9b"); err != nil {
		t.Fatal(err)
	}
	loaded, err := o.Running(ctx)
	if err != nil || len(loaded) != 1 || loaded[0].Name != "qwen3.5:9b" || loaded[0].Context != 65536 {
		t.Fatalf("running = %+v, %v", loaded, err)
	}
	f.failLoad = "x"
	if err := o.Load(ctx, "qwen3.5:9b"); err == nil || !strings.Contains(err.Error(), "model failed to load") {
		t.Errorf("a failed load: %v", err)
	}
	var seen []string
	if err := o.Pull(ctx, "qwen3.5:4b", func(p Progress) { seen = append(seen, p.Status) }); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || seen[3] != "success" {
		t.Errorf("pull statuses = %v", seen)
	}
	if err := o.Pull(ctx, "nope:1b", nil); err == nil || !strings.Contains(err.Error(), "file does not exist") {
		t.Errorf("a failed pull: %v", err)
	}
	if _, err := (Ollama{URL: "http://127.0.0.1:1"}).Version(ctx); err == nil {
		t.Error("no error from a server that is not there")
	}
}

func TestModelInfoGivesTheCacheShape(t *testing.T) {
	for _, c := range []struct {
		name  string
		mi    map[string]any
		heads []int
		k, v  int
	}{
		// Qwen3.5 lists a K/V head count per layer, 0 for layers without a
		// cache.
		{"per layer", qwenInfo(), []int{4, 4, 4, 4, 4, 4, 4, 4}, 256, 256},
		// One count for every layer, with a full attention layer every
		// fourth.
		{"interval", map[string]any{
			"general.architecture": "qwen35", "qwen35.block_count": 32.0, "qwen35.attention.head_count": 16.0,
			"qwen35.attention.head_count_kv": 4.0, "qwen35.attention.key_length": 256.0, "qwen35.attention.value_length": 256.0,
			"qwen35.full_attention_interval": 4.0,
		}, []int{4, 4, 4, 4, 4, 4, 4, 4}, 256, 256},
		// Plain attention, the head width from the embedding.
		{"plain", map[string]any{
			"general.architecture": "llama", "llama.block_count": 2.0, "llama.attention.head_count": 32.0,
			"llama.attention.head_count_kv": 8.0, "llama.embedding_length": 4096.0,
		}, []int{8, 8}, 128, 128},
		// No K/V head count at all: one cache head per attention head.
		{"no kv count", map[string]any{
			"general.architecture": "gpt2", "gpt2.block_count": 2.0, "gpt2.attention.head_count": 12.0, "gpt2.embedding_length": 768.0,
		}, []int{12, 12}, 64, 64},
	} {
		info := parseModelInfo(c.mi)
		if !reflect.DeepEqual(info.KVHeads, c.heads) || info.KeyLen != c.k || info.ValueLen != c.v {
			t.Errorf("%s: heads %v, key %d, value %d; want %v, %d, %d", c.name, info.KVHeads, info.KeyLen, info.ValueLen, c.heads, c.k, c.v)
		}
	}
}

func TestEstimate(t *testing.T) {
	info := parseModelInfo(qwenInfo())
	// 8 layers × 4 heads × (256 + 256) = 16,384 values per token.
	n := Estimate(6_550_825_373, info, 65536, "q8_0", 1)
	if want := int64(16384 * 65536 * 34 / 32); n.Cache != want {
		t.Errorf("q8_0 cache = %d, want %d", n.Cache, want)
	}
	if want := n.Weights + n.Cache + 655_082_537; n.Total != want {
		t.Errorf("total = %d, want %d", n.Total, want)
	}
	if f16 := Estimate(6_550_825_373, info, 65536, "f16", 1); f16.Cache != 16384*65536*2 {
		t.Errorf("f16 cache = %d", f16.Cache)
	}
	if two := Estimate(6_550_825_373, info, 65536, "q8_0", 2); two.Cache != 2*n.Cache {
		t.Errorf("two slots' cache = %d, want twice %d", two.Cache, n.Cache)
	}
	// A context longer than the model's own is cut to it.
	info.Context = 32768
	if cut := Estimate(6_550_825_373, info, 65536, "q8_0", 1); cut.Cache != n.Cache/2 {
		t.Errorf("cut cache = %d, want %d", cut.Cache, n.Cache/2)
	}
	// A small model still gets the working room's floor.
	if small := Estimate(100, Info{}, 4096, "f16", 1); small.Total != 100+workspaceFloor {
		t.Errorf("small total = %d", small.Total)
	}
}

func TestBudgetAndReserve(t *testing.T) {
	m := Machine{Total: 18 * GiB, Available: 10 * GiB}
	if got := Budget(0, m); got != 12*GiB {
		t.Errorf("budget = %d, want two thirds", got)
	}
	if got := Budget(24, m); got != 24*GiB {
		t.Errorf("budget with memory_gib = %d", got)
	}
	if got := Reserve(m); got != 2*GiB {
		t.Errorf("reserve = %d, want 2 GiB", got)
	}
	if got := Reserve(Machine{Total: 64 * GiB}); got != 64*GiB/10 {
		t.Errorf("reserve on 64 GiB = %d, want a tenth", got)
	}
}

func TestSuggestFitsTheBudget(t *testing.T) {
	for _, c := range []struct {
		budget int64
		want   string
	}{
		// Two thirds of an 18 GiB Mac: Qwen3.8-27B in ternary weights,
		// where its 4-bit build would not fit.
		{12 * GiB, "bonsai-2:27b"},
		{32 * GiB, "bonsai-2:27b"},
		{Budget(0, Machine{Total: 8 * GiB}), "bonsai-2:9b"}, // an 8 GiB laptop
		{4 * GiB, "bonsai-2:4b"},
		{2 * GiB, ""},
	} {
		s, n, ok := Suggest(c.budget, 65536, "q8_0", 1)
		if s.Name != c.want || ok != (c.want != "") || ok && n.Total > c.budget {
			t.Errorf("budget %s: %s (%s), %v; want %s", Bytes(c.budget), s.Name, Bytes(n.Total), ok, c.want)
		}
	}
	// Every suggestion that fits, the best first.
	ss, needs := Fitting(12*GiB, 65536, "q8_0", 1)
	var names []string
	for i, s := range ss {
		names = append(names, s.Name)
		if needs[i].Total > 12*GiB {
			t.Errorf("%s needs %s", s.Name, Bytes(needs[i].Total))
		}
	}
	if want := []string{"bonsai-2:27b", "bonsai-2:9b", "qwen3.5:9b", "bonsai-2:4b", "qwen3.5:4b"}; !reflect.DeepEqual(names, want) {
		t.Errorf("fitting 12 GiB = %q, want %q", names, want)
	}
}

func TestModelNames(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"qwen3.8", "qwen3.8:latest"},
		{"Bonsai-2:27B", "bonsai-2:27b"},
		{"registry.ollama.ai/library/qwen3.5:9b", "qwen3.5:9b"},
		{"library/qwen3.5:9b", "qwen3.5:9b"},
		{"hf.co/prism-ml/bonsai", "hf.co/prism-ml/bonsai:latest"},
	} {
		if !SameModel(c.a, c.b) {
			t.Errorf("%s and %s are not one model: %s, %s", c.a, c.b, Canonical(c.a), Canonical(c.b))
		}
	}
	// A registry's port is not a tag.
	if got := Canonical("example.com:5000/alice/coder"); got != "example.com:5000/alice/coder:latest" {
		t.Errorf("canonical = %s", got)
	}
	if SameModel("qwen3.5:9b", "qwen3.5:4b") || SameModel("qwen3.5", "qwen3.5:9b") {
		t.Error("different models are one")
	}
	if Rank("bonsai-2:27b") != 0 || Rank("BONSAI-2:27b") != 0 || Rank("chat:1b") != -1 {
		t.Errorf("ranks = %d, %d, %d", Rank("bonsai-2:27b"), Rank("BONSAI-2:27b"), Rank("chat:1b"))
	}
	if s, ok := Suggested("bonsai-2:9b"); !ok || !strings.Contains(s.About, "ternary") {
		t.Errorf("suggested = %+v, %v", s, ok)
	}
}

func TestFreeDisk(t *testing.T) {
	dir := t.TempDir()
	free, err := FreeDisk(dir)
	if err != nil || free <= 0 {
		t.Fatalf("free = %d, %v", free, err)
	}
	// A folder that is not there yet is on its parent's volume.
	if got, err := FreeDisk(dir + "/models/blobs"); err != nil || got <= 0 {
		t.Errorf("free below a missing folder = %d, %v", got, err)
	}
	if got := ModelsDir(map[string]string{"OLLAMA_MODELS": "/srv/models"}); got != "/srv/models" {
		t.Errorf("models dir = %s", got)
	}
	t.Setenv("OLLAMA_MODELS", "")
	t.Setenv("HOME", "/home/alice")
	if got := ModelsDir(nil); got != "/home/alice/.ollama/models" {
		t.Errorf("default models dir = %s", got)
	}
}

func TestPreflight(t *testing.T) {
	ctx := context.Background()
	newServer := func() *fakeServer {
		return &fakeServer{context: 65536, cacheBytes: gb, models: []fakeModel{
			{name: "qwen3.5:9b", size: 6_550_825_373, caps: []string{"completion", "tools"}, info: qwenInfo()},
			{name: "chat:1b", size: gb, caps: []string{"completion"}, info: qwenInfo()},
			{name: "short:1b", size: gb, caps: []string{"completion", "tools"}, info: map[string]any{"general.architecture": "x", "x.context_length": 8192.0}},
			{name: "big:cloud", size: 384, caps: []string{"completion", "tools"}, info: qwenInfo(), remote: true},
		}}
	}
	has := func(list []string, part string) bool {
		for _, s := range list {
			if strings.Contains(s, part) {
				return true
			}
		}
		return false
	}

	t.Run("ok", func(t *testing.T) {
		f := newServer()
		c := Preflight(ctx, serve(t, f), "qwen3.5:9b", want(65536))
		if !c.OK() || c.Context != 65536 || c.Need != 6_550_825_373+gb || !c.Loaded || len(c.Warnings) != 0 {
			t.Fatalf("check = %+v", c)
		}
		// A model in memory already is not loaded again.
		if c = Preflight(ctx, serve(t, f), "qwen3.5:9b", want(65536)); !c.OK() || f.loads != 1 {
			t.Errorf("second check = %+v, %d loads", c, f.loads)
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		c := Preflight(ctx, Ollama{URL: "http://127.0.0.1:1"}, "qwen3.5:9b", want(65536))
		if c.OK() || !has(c.Problems, "not reachable at http://127.0.0.1:1") {
			t.Errorf("check = %+v", c)
		}
	})
	t.Run("a name without its tag", func(t *testing.T) {
		// Ollama takes qwen3.5 as qwen3.5:latest.
		f := newServer()
		f.models = append(f.models, fakeModel{name: "coder:latest", size: gb, caps: []string{"completion", "tools"}, info: qwenInfo()})
		c := Preflight(ctx, serve(t, f), "coder", want(65536))
		if !c.OK() || c.Model != "coder:latest" || !c.Loaded {
			t.Errorf("check = %+v", c)
		}
	})
	t.Run("missing model", func(t *testing.T) {
		c := Preflight(ctx, serve(t, newServer()), "qwen3.5:27b", want(65536))
		if c.OK() || !has(c.Problems, "no model qwen3.5:27b") || !has(c.Problems, "irie setup local --model qwen3.5:27b") {
			t.Errorf("check = %+v", c)
		}
	})
	t.Run("no tools", func(t *testing.T) {
		c := Preflight(ctx, serve(t, newServer()), "chat:1b", want(65536))
		if c.OK() || !has(c.Problems, "cannot call tools") {
			t.Errorf("check = %+v", c)
		}
	})
	t.Run("model context too short", func(t *testing.T) {
		f := newServer()
		c := Preflight(ctx, serve(t, f), "short:1b", want(65536))
		if c.OK() || !has(c.Problems, "at most 8k tokens") || f.loads != 0 {
			t.Errorf("check = %+v, %d loads", c, f.loads)
		}
	})
	t.Run("server context too short", func(t *testing.T) {
		// The user's own server, at Ollama's default context below 24 GiB of
		// VRAM.
		f := newServer()
		f.context = 4096
		w := want(65536)
		w.Own, w.Context = false, 0
		c := Preflight(ctx, serve(t, f), "qwen3.5:9b", w)
		if c.OK() || c.Context != 4096 || !has(c.Problems, "runs with a 4k context") || !has(c.Problems, "OLLAMA_CONTEXT_LENGTH") || !has(c.Problems, "local.url empty") {
			t.Errorf("check = %+v", c)
		}
		// On Irie's own server the fix is Irie's config.
		w.Own = true
		if c = Preflight(ctx, serve(t, f), "qwen3.5:9b", w); c.OK() || !has(c.Problems, "raise local.context") {
			t.Errorf("own server check = %+v", c)
		}
	})
	t.Run("too big for the budget", func(t *testing.T) {
		f := newServer()
		w := want(65536)
		w.Budget = 6 * GiB
		c := Preflight(ctx, serve(t, f), "qwen3.5:9b", w)
		if c.OK() || !has(c.Problems, "more than the 6.4 GB local models may take") || f.loads != 0 || c.Need == 0 {
			t.Errorf("check = %+v, %d loads", c, f.loads)
		}
	})
	t.Run("too little free memory", func(t *testing.T) {
		f := newServer()
		w := want(65536)
		w.Machine = Machine{Total: 18 * GiB, Available: 5 * GiB}
		c := Preflight(ctx, serve(t, f), "qwen3.5:9b", w)
		if c.OK() || !has(c.Problems, "free, of which Irie leaves 2.1 GB") || !has(c.Problems, "--force") || f.loads != 0 {
			t.Errorf("check = %+v, %d loads", c, f.loads)
		}
		// --force loads it anyway and says so.
		w.Force = true
		if c = Preflight(ctx, serve(t, f), "qwen3.5:9b", w); !c.OK() || !has(c.Warnings, "loading it anyway") || f.loads != 1 {
			t.Errorf("forced check = %+v, %d loads", c, f.loads)
		}
	})
	t.Run("another model gives its memory back", func(t *testing.T) {
		// Irie's own server holds one model, so loading another frees its
		// memory first.
		f := newServer()
		f.models = append(f.models, fakeModel{name: "other:7b", size: 5 * gb, caps: []string{"completion", "tools"}, info: qwenInfo()})
		f.loaded = map[string]bool{"other:7b": true}
		w := want(65536)
		w.Machine = Machine{Total: 18 * GiB, Available: 5 * GiB}
		if c := Preflight(ctx, serve(t, f), "qwen3.5:9b", w); !c.OK() {
			t.Errorf("check = %+v", c)
		}
	})
	t.Run("partly on the CPU", func(t *testing.T) {
		f := newServer()
		f.vramShare = 0.5
		c := Preflight(ctx, serve(t, f), "qwen3.5:9b", want(65536))
		if !c.OK() || !has(c.Warnings, "runs on the CPU") {
			t.Errorf("check = %+v", c)
		}
	})
	t.Run("load fails", func(t *testing.T) {
		f := newServer()
		f.failLoad = "x"
		c := Preflight(ctx, serve(t, f), "qwen3.5:9b", want(65536))
		if c.OK() || !has(c.Problems, "could not load qwen3.5:9b: ollama: model failed to load") {
			t.Errorf("check = %+v", c)
		}
	})
	t.Run("cloud model", func(t *testing.T) {
		f := newServer()
		c := Preflight(ctx, serve(t, f), "big:cloud", want(65536))
		if !c.OK() || !has(c.Warnings, "Ollama's cloud") || f.loads != 0 {
			t.Errorf("check = %+v, %d loads", c, f.loads)
		}
	})
}

func TestFindOllama(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindOllama(dir + "/ollama"); err == nil || !strings.Contains(err.Error(), "not an executable") {
		t.Errorf("a missing configured binary: %v", err)
	}
	t.Setenv("PATH", dir)
	old := installDirs
	installDirs = nil
	defer func() { installDirs = old }()
	if _, err := FindOllama(""); !errors.Is(err, ErrNoOllama) {
		t.Errorf("no ollama anywhere: %v", err)
	}
}

func TestHowToInstall(t *testing.T) {
	brew := func(string) (string, error) { return "/opt/homebrew/bin/brew", nil }
	noBrew := func(string) (string, error) { return "", errors.New("not found") }
	if i := HowToInstall("linux", noBrew); len(i.Argv) != 3 || !strings.Contains(i.Argv[2], "https://ollama.com/install.sh") {
		t.Errorf("linux = %+v", i)
	}
	if i := HowToInstall("darwin", brew); !reflect.DeepEqual(i.Argv, []string{"/opt/homebrew/bin/brew", "install", "ollama"}) {
		t.Errorf("darwin with brew = %+v", i)
	}
	if i := HowToInstall("darwin", noBrew); i.Argv != nil || !strings.Contains(i.Hint, "https://ollama.com/download") {
		t.Errorf("darwin without brew = %+v", i)
	}
	if i := HowToInstall("freebsd", brew); i.Argv != nil {
		t.Errorf("freebsd = %+v", i)
	}
}

func TestFormats(t *testing.T) {
	for n, want := range map[int]string{65536: "64k", 4096: "4k", 262144: "256k", 1 << 20: "1M", 100000: "100k", 1500: "1.5k", 512: "512"} {
		if got := Tokens(n); got != want {
			t.Errorf("Tokens(%d) = %q, want %q", n, got, want)
		}
	}
	for n, want := range map[int64]string{7_912_345_678: "7.9 GB", 640_400_000: "640 MB", 12_000: "12 KB", 0: "0 B"} {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestReadMachine(t *testing.T) {
	m, err := ReadMachine()
	if err != nil {
		t.Skipf("no memory reading here: %v", err)
	}
	if m.Total < GiB || m.Available <= 0 || m.Available > m.Total {
		t.Errorf("machine = %+v", m)
	}
}

func TestUnloadLetsAModelGo(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/generate" {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"done":true}`))
	}))
	defer srv.Close()
	if err := (Ollama{URL: srv.URL}).Unload(context.Background(), "qwen3.5:9b"); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "qwen3.5:9b" || got["keep_alive"] != float64(0) {
		t.Errorf("request = %v, want the model with keep_alive 0", got)
	}
}
