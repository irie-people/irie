package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/testutil"
)

// qwenGGUF is the metadata of a small Qwen3.5-family model in GGUF, with a
// chat template that takes tools unless tools is false.
func qwenGGUF(tools bool, extra map[string]any) map[string]any {
	tmpl := "{% for m in messages %}{{ m.content }}{% endfor %}"
	if tools {
		tmpl = "{% if tools %}{{ tools | tojson }}{% endif %}" + tmpl
	}
	meta := map[string]any{
		"general.architecture":           "qwen35",
		"qwen35.block_count":             8,
		"qwen35.context_length":          262144,
		"qwen35.attention.head_count":    16,
		"qwen35.attention.head_count_kv": 4,
		"qwen35.attention.key_length":    256,
		"qwen35.attention.value_length":  256,
		"qwen35.full_attention_interval": 4,
		"tokenizer.chat_template":        tmpl,
		"tokenizer.ggml.tokens":          []string{"a", "b"},
	}
	for k, v := range extra {
		meta[k] = v
	}
	return meta
}

// layerTensors are a model's tensors: an embedding and layers layers of
// size bytes each, of ggml type typ.
func layerTensors(layers int, size int64, typ uint32) []testutil.GGUFTensor {
	ts := []testutil.GGUFTensor{{Name: "token_embd.weight", Type: typ, Bytes: size}}
	for i := range layers {
		ts = append(ts, testutil.GGUFTensor{Name: fmt.Sprintf("blk.%d.ffn_down.weight", i), Type: typ, Bytes: size})
	}
	return ts
}

func TestBigRunsAModelOnIriesOwnLlamaServer(t *testing.T) {
	dir := t.TempDir()
	models, store := filepath.Join(dir, "gguf"), filepath.Join(dir, "ollama")
	for _, d := range []string{models, filepath.Join(store, "blobs"), filepath.Join(store, "manifests/registry.ollama.ai/library/bonsai-2")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// A model of Ollama's: Bonsai 2 9B in Q2_0, whose weights Ollama keeps
	// beside a vision projector that Irie leaves out.
	rotation := map[string]any{"prism.hadamard.version": 1}
	testutil.WriteGGUF(t, filepath.Join(store, "blobs", "sha256-0b0b"), qwenGGUF(true, rotation), layerTensors(8, 4<<20, 42))
	manifest := `{"layers":[{"mediaType":"application/vnd.ollama.image.model","digest":"sha256:0b0b"},{"mediaType":"application/vnd.ollama.image.projector","digest":"sha256:0c0c"}]}`
	if err := os.WriteFile(filepath.Join(store, "manifests/registry.ollama.ai/library/bonsai-2/9b"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	// Files of its own: one an agent can run, one that takes no tools, and
	// Bonsai's smallest packing, which only PrismML's build runs.
	testutil.WriteGGUF(t, filepath.Join(models, "coder.gguf"), qwenGGUF(true, nil), layerTensors(8, 2<<20, 12))
	testutil.WriteGGUF(t, filepath.Join(models, "chat.gguf"), qwenGGUF(false, nil), layerTensors(8, 2<<20, 12))
	testutil.WriteGGUF(t, filepath.Join(models, "Ternary-Bonsai-2-27B-PTQ1_0.gguf"), qwenGGUF(true, rotation), layerTensors(8, 3<<20, 143))
	// A vision projector and the second part of a split model are no models.
	testutil.WriteGGUF(t, filepath.Join(models, "mmproj.gguf"), map[string]any{"general.architecture": "clip"}, layerTensors(1, 1<<20, 1))
	testutil.WriteGGUF(t, filepath.Join(models, "coder-00002-of-00002.gguf"), qwenGGUF(true, nil), layerTensors(1, 1<<20, 12))

	log := filepath.Join(dir, "llama-args.json")
	t.Setenv("IRIE_FAKE_LLAMA_LOG", log)
	port := testutil.FreePort(t)
	fake := testutil.BuildFakeAgent(t)
	a := agentConfig{
		extra: fmt.Sprintf("[profiles.big]\ncommand = %q\n\n[profiles.coder]\ncommand = %q\nmodel = \"coder\"\nlocal = { runtime = \"llama-server\" }\n", fake, fake),
		local: fmt.Sprintf("ollama = \"/nonexistent/ollama\"\nllama_server = %q\nllama_port = %d\nllama_args = [\"--flash-attn-extra\"]\nmodels = %q\nenv = { OLLAMA_MODELS = %q }\n",
			testutil.BuildFakeLlama(t), port, models, store),
	}
	c := setupWith(t, a)
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	// ctl local lists what the llama-server can run, the best of it first
	// for +big: Ollama's Bonsai, which this build runs; the smaller packing
	// needs PrismML's build, and one model takes no tools.
	var lr ipc.LocalResult
	if err := c.call("local", ipc.LocalArgs{Llama: true}, &lr); err != nil {
		t.Fatal(err)
	}
	l := lr.Llama
	if l == nil || l.Pick != "bonsai-2:9b" || l.Build != "build 12000" || l.Dir != models || l.Running != "" || l.URL != url {
		t.Fatalf("llama = %+v", l)
	}
	why := map[string]string{}
	for _, m := range l.Models {
		why[m.Name] = m.Why
	}
	if want := map[string]string{"bonsai-2:9b": "", "coder": "", "chat": "cannot call tools", "Ternary-Bonsai-2-27B-PTQ1_0": "needs PrismML's llama.cpp"}; !mapsEqual(why, want) {
		t.Errorf("models = %v, want %v", why, want)
	}

	// The + list says what +big runs.
	var cr ipc.CompleteResult
	if err := c.call("complete", ipc.CompleteArgs{Text: "+bi"}, &cr); err != nil || !slices.Equal(cr.Items, []string{"+big"}) || !strings.Contains(cr.Notes[0], "bonsai-2:9b · local, $0") {
		t.Fatalf("complete +bi = %+v, %v", cr, err)
	}

	// +big starts Irie's own llama-server on it, with Irie's settings, and
	// points the agent at it.
	var r ipc.SpawnResult
	if err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "big"}, &r); err != nil {
		t.Fatal(err)
	}
	if row := c.waitState(r.Session.ID, ipc.StateWorking); !row.Local || row.Model != "bonsai-2:9b" {
		t.Errorf("row = %+v", row)
	}
	args := readArgs(t, log)
	for _, want := range [][]string{
		{"-m", filepath.Join(store, "blobs", "sha256-0b0b")}, {"--alias", "bonsai-2:9b"}, {"--host", "127.0.0.1"},
		{"--port", fmt.Sprint(port)}, {"-c", "65536"}, {"-ctk", "q8_0"}, {"-ctv", "q8_0"}, {"-fa", "on"}, {"-np", "1"},
	} {
		if i := slices.Index(args, want[0]); i < 0 || i+1 >= len(args) || args[i+1] != want[1] {
			t.Errorf("llama-server %s = ?, want %s, in %q", want[0], want[1], args)
		}
	}
	if args[len(args)-1] != "--flash-attn-extra" {
		t.Errorf("local.llama_args are not last: %q", args)
	}
	var server map[string]string
	for _, p := range c.panes() {
		if p["@irie_role"] == "llama" {
			server = p
		}
	}
	if server == nil || server["session_name"] != "irie-agents" || server["pane_current_command"] != "irie" || server["@irie_name"] != "bonsai-2:9b" {
		t.Fatalf("server pane = %v", server)
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(r.Session.ID))
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"ANTHROPIC_BASE_URL": url, "ANTHROPIC_DEFAULT_OPUS_MODEL": "bonsai-2:9b", "CLAUDE_CODE_SUBAGENT_MODEL": "bonsai-2:9b",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "65536", "ANTHROPIC_API_KEY": "",
	} {
		if v, ok := spec.Env[k]; !ok || v != want {
			t.Errorf("agent %s = %q (set %v), want %q", k, v, ok, want)
		}
	}
	if !spec.Local || spec.Model != "bonsai-2:9b" {
		t.Errorf("spec = %+v", spec)
	}

	// The server runs one model, and a live session runs on it: another
	// model is refused without --force, and nothing is spawned.
	n := c.agentPanes()
	err = c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "coder"}, nil)
	if err == nil || !strings.Contains(err.Error(), "runs bonsai-2:9b for") || !strings.Contains(err.Error(), "--force") {
		t.Errorf("+coder: %v", err)
	}
	if c.agentPanes() != n {
		t.Error("a refused spawn made a pane")
	}
	// With --force the server switches to it.
	if err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "coder", Force: true}, &r); err != nil {
		t.Fatal(err)
	}
	if row := c.waitState(r.Session.ID, ipc.StateWorking); row.Model != "coder" {
		t.Errorf("+coder runs %q", row.Model)
	}
	if args := readArgs(t, log); !slices.Contains(args, filepath.Join(models, "coder.gguf")) {
		t.Errorf("llama-server args after the switch = %q", args)
	}
	servers := 0
	for _, p := range c.panes() {
		if p["@irie_role"] == "llama" {
			servers++
		}
	}
	if servers != 1 {
		t.Errorf("%d llama-servers, want 1", servers)
	}

	// +llama:<model> runs one model for one task, and the + list offers the
	// ones this build runs.
	if err := c.call("complete", ipc.CompleteArgs{Text: "+llama:"}, &cr); err != nil || !slices.Equal(cr.Items, []string{"+llama:coder", "+llama:bonsai-2:9b"}) {
		t.Errorf("complete +llama: = %q, %v", cr.Items, err)
	}
	if err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "llama:coder"}, &r); err != nil {
		t.Fatal(err)
	}
	if row := c.waitState(r.Session.ID, ipc.StateWorking); row.Model != "coder" || row.Profile != "llama:coder" {
		t.Errorf("+llama:coder = %+v", row)
	}

	// A model only PrismML's build runs is refused on this one, saying so.
	var pr ipc.PreflightResult
	if err := c.call("preflight", ipc.PreflightArgs{Profile: "big", Model: "Ternary-Bonsai-2-27B-PTQ1_0"}, &pr); err != nil || pr.OK || !strings.Contains(strings.Join(pr.Problems, " "), "PrismML") {
		t.Errorf("preflight = %+v, %v", pr, err)
	}
}

func TestFitsNow(t *testing.T) {
	m := localmodel.Machine{Total: 18 * localmodel.GiB, Available: 9 * localmodel.GiB}
	list := []ipc.LocalModel{
		{Name: "bonsai-2:27b", Need: 11_400_000_000, Fits: true},
		{Name: "other:7b", Need: 5_000_000_000, Fits: true},
		{Name: "bonsai-2:9b", Need: 4_100_000_000, Fits: true},
		{Name: "chat:1b", Need: 1_000_000_000, Fits: true, Why: "cannot call tools"},
	}
	// 9 GiB free less the 2 GiB reserve: the best suggested model that fits.
	if got := fitsNow(list, "bonsai-2:27b", m, 0, localmodel.Rank); got != "bonsai-2:9b" {
		t.Errorf("fits now = %q", got)
	}
	// No suggested model fits: the largest that does.
	if got := fitsNow(list[:2], "bonsai-2:27b", m, 0, localmodel.Rank); got != "other:7b" {
		t.Errorf("fits now without suggestions = %q", got)
	}
	// What the server's model gives back counts, and nothing else fits.
	if got := fitsNow(list[:1], "x", m, 5*localmodel.GiB, localmodel.Rank); got != "bonsai-2:27b" {
		t.Errorf("fits now with memory given back = %q", got)
	}
	if got := fitsNow(list, "bonsai-2:27b", localmodel.Machine{Total: 18 * localmodel.GiB, Available: localmodel.GiB}, 0, localmodel.Rank); got != "" {
		t.Errorf("fits now on a full machine = %q", got)
	}
}

func TestSplitParts(t *testing.T) {
	for name, want := range map[string]bool{
		"model-00001-of-00003": false, "model-00002-of-00003": true, "model": false, "x-of-y": false, "a-0000x-of-00002": false,
	} {
		if got := splitPart(name); got != want {
			t.Errorf("splitPart(%q) = %v", name, got)
		}
	}
}

func readArgs(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal(b, &args); err != nil {
		t.Fatal(err)
	}
	return args
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
