package localmodel

import (
	"context"
	"fmt"
	"strings"
)

// Want is what a spawn needs of a local model, and what the machine and the
// server give it.
type Want struct {
	MinContext int    // the profile's min_context
	Context    int    // the context the server loads models with, when Irie runs it; 0 = unknown
	KVCache    string // the server's context cache type
	Parallel   int    // requests a loaded model serves at once
	Own        bool   // the server is Irie's own, which holds one model at a time
	Budget     int64  // the most local models may take
	Machine    Machine
	Force      bool // load the model even when it does not fit the memory now
}

// Check is a model checked before an agent is spawned on it (DESIGN §6.1).
type Check struct {
	URL      string   `json:"url"`
	Model    string   `json:"model"`
	Context  int      `json:"context,omitempty"`  // the context the model was loaded with
	Need     int64    `json:"need,omitempty"`     // what it takes in memory: measured once loaded, else estimated
	VRAM     int64    `json:"vram,omitempty"`     // the part of it in GPU memory
	Loaded   bool     `json:"loaded,omitempty"`   // it is in memory now
	Problems []string `json:"problems,omitempty"` // why the spawn is refused; none means it may go ahead
	Warnings []string `json:"warnings,omitempty"` // what to know anyway
}

// OK reports whether the spawn may go ahead.
func (c Check) OK() bool { return len(c.Problems) == 0 }

// Preflight checks model on o before an agent is spawned on it. It loads
// the model the way the agent will, unless the model cannot fit, then reads
// the context it got and what it takes in memory. A model whose context is
// shorter than w.MinContext is refused, since Ollama cuts a longer prompt
// without an error and the agent would lose its instructions.
func Preflight(ctx context.Context, o Ollama, model string, w Want) Check {
	c := Check{URL: o.URL, Model: model}
	problem := func(format string, a ...any) { c.Problems = append(c.Problems, fmt.Sprintf(format, a...)) }
	warn := func(format string, a ...any) { c.Warnings = append(c.Warnings, fmt.Sprintf(format, a...)) }

	if _, err := o.Version(ctx); err != nil {
		problem("Ollama is not reachable at %s: %v", o.URL, err)
		return c
	}
	models, err := o.Models(ctx)
	if err != nil {
		problem("Ollama at %s cannot list its models: %v", o.URL, err)
		return c
	}
	var m *Model
	for i := range models {
		if SameModel(models[i].Name, model) {
			m = &models[i]
		}
	}
	if m == nil {
		problem("Ollama at %s has no model %s; `irie setup local --model %s` pulls it", o.URL, model, model)
		return c
	}
	// The server's own name for it, which /api/ps uses too: qwen3.8 is
	// qwen3.8:latest.
	model, c.Model = m.Name, m.Name
	info, err := o.Show(ctx, model)
	if err != nil {
		problem("Ollama cannot describe %s: %v", model, err)
		return c
	}
	if !info.Tools() {
		problem("%s cannot call tools, which Claude Code needs; pick a model whose capabilities include tools", model)
	}
	if info.Context > 0 && info.Context < w.MinContext {
		problem("%s takes at most %s tokens of context and the profile needs %s; pick a model with a longer context, or lower the profile's local.min_context", model, Tokens(info.Context), Tokens(w.MinContext))
	}
	if m.Remote {
		warn("%s runs on Ollama's cloud, not on this machine", model)
	}
	if !c.OK() {
		return c
	}

	loaded, err := o.Running(ctx)
	if err != nil {
		problem("Ollama at %s cannot list its loaded models: %v", o.URL, err)
		return c
	}
	var freed int64 // what loading this model unloads from Irie's own server first
	for _, l := range loaded {
		if SameModel(l.Name, model) && l.Context >= w.MinContext {
			c.Loaded = true
		} else if w.Own {
			freed += l.Size
		}
	}
	if !c.Loaded && !m.Remote {
		ctxLen := w.Context
		if ctxLen == 0 {
			ctxLen = w.MinContext
		}
		need := Estimate(m.Size, info, ctxLen, w.KVCache, w.Parallel)
		c.Need = need.Total
		short := false
		if w.Budget > 0 && need.Total > w.Budget {
			short = true
			problem("%s needs about %s of memory at a %s context (%s of weights, %s of context cache), more than the %s local models may take here; pick a smaller model, or raise local.memory_gib", model, Bytes(need.Total), Tokens(ctxLen), Bytes(need.Weights), Bytes(need.Cache), Bytes(w.Budget))
		}
		if free := w.Machine.Available + freed - Reserve(w.Machine); w.Machine.Total > 0 && need.Total > free && !short {
			msg := fmt.Sprintf("%s needs about %s of memory and the machine has %s free, of which Irie leaves %s for everything else", model, Bytes(need.Total), Bytes(w.Machine.Available+freed), Bytes(Reserve(w.Machine)))
			if w.Force {
				warn("%s; loading it anyway", msg)
			} else {
				problem("%s; close something first, pick a smaller model, or spawn with --force", msg)
			}
		}
		if !c.OK() {
			return c
		}
		if err := o.Load(ctx, model); err != nil {
			problem("Ollama could not load %s: %v", model, err)
			return c
		}
		if loaded, err = o.Running(ctx); err != nil {
			problem("Ollama at %s cannot list its loaded models: %v", o.URL, err)
			return c
		}
	}
	for _, l := range loaded {
		if !SameModel(l.Name, model) {
			continue
		}
		c.Context, c.Need, c.VRAM, c.Loaded = l.Context, l.Size, l.VRAM, true
	}
	if !c.Loaded {
		if m.Remote {
			return c
		}
		problem("Ollama at %s does not list %s as loaded after loading it", o.URL, model)
		return c
	}
	if c.Context < w.MinContext {
		fix := "set OLLAMA_CONTEXT_LENGTH for that server and restart it (for the macOS app: its context length setting, or `launchctl setenv OLLAMA_CONTEXT_LENGTH " + fmt.Sprint(w.MinContext) + "`, then restart Ollama), or leave local.url empty so Irie runs its own server"
		if w.Own {
			fix = "raise local.context in Irie's config"
		}
		problem("%s runs with a %s context at %s and the profile needs %s; Ollama would cut longer prompts without an error. To fix it, %s", model, Tokens(c.Context), o.URL, Tokens(w.MinContext), fix)
	}
	if c.VRAM > 0 && c.VRAM < c.Need {
		warn("only %s of %s's %s is in GPU memory; the rest runs on the CPU, slowly", Bytes(c.VRAM), model, Bytes(c.Need))
	}
	return c
}

// Tokens renders a token count the way contexts are named: 64k, 4k, 1M.
func Tokens(n int) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%dM", n>>20)
	case n >= 1024 && n%1024 == 0:
		return fmt.Sprintf("%dk", n>>10)
	case n >= 1000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	}
	return fmt.Sprint(n)
}
