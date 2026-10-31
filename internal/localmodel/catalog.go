package localmodel

import "strings"

// Suggestion is a model Irie knows a coding agent runs well on.
type Suggestion struct {
	Name  string // Ollama's name for it
	Size  int64  // its weights and vision projector, in bytes
	Info  Info   // its context cache's shape
	About string // what it is, in a few words
}

// Suggestions are the models `irie setup local` offers, and that +local
// picks from when no model was chosen, the best first. Sizes are those of
// Ollama's registry and shapes those of the models' own metadata
// (2026-10-31).
//
// Quantization sets much of the order. Bonsai 2 (PrismML) stores a Qwen
// model's weights as ternary values, -1, 0 or +1, at about two bits a
// weight: its 27B takes 8.3 GB where Qwen's own 4-bit 27B takes 17.7 GB,
// and keeps 98% of Qwen3.8-27B's benchmark average. So a ternary model
// comes before a 4-bit one of the same base, which takes twice the memory
// and, since decoding reads every weight, runs at about half the speed, for
// about a point. A larger base comes before a smaller one. Models below 4B
// are too weak for an agent's tool loop and are left out.
var Suggestions = []Suggestion{
	{"bonsai-2:27b", 8_255_266_099, qwen35(64, 4), "Qwen3.8-27B, ternary"},
	{"qwen3.8:27b", 17_741_871_939, qwen35(64, 4), "Qwen3.8-27B, 4-bit"},
	{"qwen3.5:27b", 17_478_556_413, qwen35(64, 4), "Qwen3.5-27B, 4-bit"},
	{"bonsai-2:9b", 3_180_879_995, qwen35(32, 4), "Qwen3.5-9B, ternary"},
	{"qwen3.5:9b", 6_550_825_373, qwen35(32, 4), "Qwen3.5-9B, 4-bit"},
	{"bonsai-2:4b", 1_579_008_347, qwen35(32, 4), "Qwen3.5-4B, ternary"},
	{"qwen3.5:4b", 3_324_173_757, qwen35(32, 4), "Qwen3.5-4B, 4-bit"},
}

// FileSuggestions are GGUF files that Irie's llama-server runs and Ollama
// does not: Bonsai 2's smaller packings, which only PrismML's llama.cpp
// runs, from PrismML's Hugging Face repositories (2026-10-31). They are the
// same ternary weights as Ollama's bonsai-2, which is their Q2_0 packing,
// in fewer bits: the 27B takes 5.95 GB in PTQ1_0 where Q2_0 takes 7.63 GB.
// A file in local.models goes by its name without .gguf.
var FileSuggestions = []Suggestion{
	{"Ternary-Bonsai-2-27B-PTQ1_0", 5_946_648_928, qwen35(64, 4), "Qwen3.8-27B, ternary, 1.75 bits a weight"},
	{"Ternary-Bonsai-2-27B-PQ2_0", 7_206_168_928, qwen35(64, 4), "Qwen3.8-27B, ternary, 2.13 bits a weight"},
	{"Ternary-Bonsai-2-27B-Q2_0", 7_626_008_928, qwen35(64, 4), "Qwen3.8-27B, ternary"},
	{"Ternary-Bonsai-2-9B-PTQ1_0", 1_997_486_976, qwen35(32, 4), "Qwen3.5-9B, ternary, 1.75 bits a weight"},
	{"Ternary-Bonsai-2-9B-PQ2_0", 2_416_851_840, qwen35(32, 4), "Qwen3.5-9B, ternary, 2.13 bits a weight"},
	{"Ternary-Bonsai-2-9B-Q2_0", 2_556_640_128, qwen35(32, 4), "Qwen3.5-9B, ternary"},
}

// llamaOrder ranks what Irie's llama-server may run, the best first: as
// Suggestions, with each model's smaller packings before its larger ones.
var llamaOrder = []string{
	"Ternary-Bonsai-2-27B-PTQ1_0", "Ternary-Bonsai-2-27B-PQ2_0", "bonsai-2:27b", "Ternary-Bonsai-2-27B-Q2_0",
	"qwen3.8:27b", "qwen3.5:27b",
	"Ternary-Bonsai-2-9B-PTQ1_0", "Ternary-Bonsai-2-9B-PQ2_0", "bonsai-2:9b", "Ternary-Bonsai-2-9B-Q2_0",
	"qwen3.5:9b", "bonsai-2:4b", "qwen3.5:4b",
}

// LlamaRank is a model's place among what Irie's llama-server may run, 0
// for the best, and -1 for a model Irie does not suggest.
func LlamaRank(name string) int {
	for i, n := range llamaOrder {
		if SameModel(n, name) {
			return i
		}
	}
	return -1
}

// qwen35 is the cache shape of a Qwen3.5-family model with layers layers,
// of which every fourth keeps a cache of kvHeads 256-wide heads; the
// others are linear attention, whose state does not grow with the context.
func qwen35(layers, kvHeads int) Info {
	info := Info{Capabilities: []string{"completion", "tools"}, Context: 262144, KeyLen: 256, ValueLen: 256}
	for range layers / 4 {
		info.KVHeads = append(info.KVHeads, kvHeads)
	}
	return info
}

// Fitting returns the suggestions whose memory at context fits the budget,
// the best first, with what each takes.
func Fitting(budget int64, context int, kv string, parallel int) ([]Suggestion, []Need) {
	var out []Suggestion
	var needs []Need
	for _, s := range Suggestions {
		if n := Estimate(s.Size, s.Info, context, kv, parallel); n.Total <= budget {
			out, needs = append(out, s), append(needs, n)
		}
	}
	return out, needs
}

// Suggest returns the best suggestion whose memory at context fits the
// budget.
func Suggest(budget int64, context int, kv string, parallel int) (Suggestion, Need, bool) {
	ss, needs := Fitting(budget, context, kv, parallel)
	if len(ss) == 0 {
		return Suggestion{}, Need{}, false
	}
	return ss[0], needs[0], true
}

// Rank is a model's place among the suggestions, 0 for the best, and -1
// for a model that is not one of them.
func Rank(name string) int {
	for i, s := range Suggestions {
		if SameModel(s.Name, name) {
			return i
		}
	}
	return -1
}

// Suggested returns the suggestion that is model, among Ollama's and the
// files.
func Suggested(name string) (Suggestion, bool) {
	if i := Rank(name); i >= 0 {
		return Suggestions[i], true
	}
	for _, s := range FileSuggestions {
		if SameModel(s.Name, name) {
			return s, true
		}
	}
	return Suggestion{}, false
}

// Canonical is a model name as Ollama resolves it: in lower case, without
// its default registry and namespace, and with the tag latest when it names
// none, so that qwen3.8 and qwen3.8:latest are one model.
func Canonical(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "registry.ollama.ai/")
	name = strings.TrimPrefix(name, "library/")
	if name != "" && !strings.Contains(name[strings.LastIndex(name, "/")+1:], ":") {
		name += ":latest"
	}
	return name
}

// SameModel reports whether two names are one model to Ollama.
func SameModel(a, b string) bool { return Canonical(a) == Canonical(b) }
