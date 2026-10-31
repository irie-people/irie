package core

// Irie's own llama-server (DESIGN §6.5): llama.cpp's server, for local
// profiles with runtime = "llama-server", such as the built-in +big. It runs
// whichever llama.cpp build the user names in local.llama_server, so builds
// that Ollama does not ship run here too: PrismML's, for Bonsai's smallest
// packings, and builds that stream a model's weights from disk. It runs one
// model at a time: a GGUF file of local.models, or one of Ollama's models,
// read from Ollama's store as it is, with no copy and without its vision
// projector. Like Irie's own Ollama server it runs in a window of
// irie-agents, under `irie runtime`, on 127.0.0.1.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/repo"
	"github.com/irie-people/irie/internal/tmux"
)

// llamaRuntime is the runtime of local profiles that run on Irie's own
// llama-server.
const llamaRuntime = "llama-server"

// llamaWindow names the window of Irie's own llama-server.
const llamaWindow = "irie-llama"

// llamaPrefix names a model of Irie's llama-server as a profile:
// +llama:Ternary-Bonsai-2-27B-PTQ1_0 is +big on that model.
const llamaPrefix = "llama:"

// isLlama reports whether profile p runs on Irie's llama-server.
func isLlama(p config.Profile) bool { return p.Local != nil && p.Local.Runtime == llamaRuntime }

// ggufEntry is a GGUF file's header as the core last read it.
type ggufEntry struct {
	size int64
	mod  time.Time
	g    localmodel.GGUF
	err  error
}

// buildEntry is what a llama-server binary said it is.
type buildEntry struct {
	mod   time.Time
	build localmodel.Build
	err   error
}

// llamaModel is a model Irie's llama-server can run.
type llamaModel struct {
	name string // Ollama's name for it, else its file's without .gguf
	path string
	g    localmodel.GGUF
}

func (c *Core) llamaURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", c.cfg().Local.LlamaPort)
}

// modelsDir is the folder of GGUF files Irie's llama-server may run.
func (c *Core) modelsDir() string {
	if d := c.cfg().Local.Models; d != "" {
		return repo.Expand(d, home())
	}
	return c.paths.Models()
}

// readGGUF reads a GGUF file's header, again only once the file changes.
func (c *Core) readGGUF(path string) (localmodel.GGUF, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return localmodel.GGUF{}, err
	}
	c.ggufMu.Lock()
	defer c.ggufMu.Unlock()
	if e, ok := c.ggufs[path]; ok && e.size == fi.Size() && e.mod.Equal(fi.ModTime()) {
		return e.g, e.err
	}
	g, err := localmodel.ReadGGUF(path)
	if c.ggufs == nil {
		c.ggufs = map[string]ggufEntry{}
	}
	c.ggufs[path] = ggufEntry{size: fi.Size(), mod: fi.ModTime(), g: g, err: err}
	return g, err
}

// llamaBuild finds the llama-server binary and says which build it is,
// asking it again only once the binary changes.
func (c *Core) llamaBuild() (string, localmodel.Build, error) {
	bin, err := localmodel.FindLlamaServer(c.cfg().Local.LlamaServer)
	if errors.Is(err, localmodel.ErrNoLlamaServer) {
		return "", localmodel.Build{}, errors.New("llama-server is not installed: `brew install llama.cpp`, or name a llama.cpp build such as PrismML's in local.llama_server")
	}
	if err != nil {
		return "", localmodel.Build{}, err
	}
	fi, err := os.Stat(bin)
	if err != nil {
		return "", localmodel.Build{}, err
	}
	c.ggufMu.Lock()
	e, ok := c.builds[bin]
	c.ggufMu.Unlock()
	if !ok || !e.mod.Equal(fi.ModTime()) {
		e.mod = fi.ModTime()
		e.build, e.err = localmodel.ReadBuild(context.Background(), bin)
		c.ggufMu.Lock()
		if c.builds == nil {
			c.builds = map[string]buildEntry{}
		}
		c.builds[bin] = e
		c.ggufMu.Unlock()
	}
	return bin, e.build, e.err
}

// llamaModels lists what Irie's llama-server can run: the GGUF files of
// local.models, then Ollama's models that are GGUF files. A file whose
// name is a model of Ollama's comes first. Vision projectors, the parts of
// a split model after its first, and files that are not GGUF are left out.
func (c *Core) llamaModels() []llamaModel {
	var out []llamaModel
	seen := map[string]bool{}
	dir := c.modelsDir()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".gguf")
		if !ok || e.IsDir() || strings.HasPrefix(name, ".") || splitPart(name) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		g, err := c.readGGUF(path)
		if err != nil || g.Arch() == "" || g.Arch() == "clip" {
			continue
		}
		out = append(out, llamaModel{name: name, path: path, g: g})
		seen[localmodel.Canonical(name)] = true
	}
	store, _ := localmodel.OllamaStore(localmodel.ModelsDir(c.cfg().Local.Env))
	for _, s := range store {
		if seen[localmodel.Canonical(s.Name)] {
			continue
		}
		g, err := c.readGGUF(s.Path)
		if err != nil || g.Arch() == "" {
			continue
		}
		out = append(out, llamaModel{name: s.Name, path: s.Path, g: g})
		seen[localmodel.Canonical(s.Name)] = true
	}
	return out
}

// splitPart reports whether name is a later part of a model split across
// files, such as model-00002-of-00003; llama.cpp finds those from the first.
func splitPart(name string) bool {
	i := strings.LastIndex(name, "-of-")
	if i < 6 || name[i-6] != '-' {
		return false
	}
	n, err := strconv.Atoi(name[i-5 : i])
	return err == nil && n > 1
}

// llamaNeed is what a model takes in Irie's llama-server: its weights, its
// context cache at local.context and local.kv_cache in one slot, and
// working room.
func (c *Core) llamaNeed(m llamaModel) localmodel.Need {
	l := c.cfg().Local
	return localmodel.Estimate(m.g.Weights(), m.g.Info(), l.Context, l.KVCache, 1)
}

// llamaWhy is why a coding agent cannot run on a model on build b, in a
// few words, or "".
func llamaWhy(m llamaModel, b localmodel.Build) string {
	info := m.g.Info()
	switch rotation, types := m.g.Prism(); {
	case !info.Tools():
		return "cannot call tools"
	case info.Context > 0 && info.Context < config.DefaultMinContext:
		return "takes at most " + localmodel.Tokens(info.Context) + " tokens of context"
	case b.Prism:
	case types:
		return "needs PrismML's llama.cpp"
	case rotation && b.Number > 0 && b.Number < localmodel.MinRotationBuild:
		return fmt.Sprintf("needs llama.cpp build %d or later, or PrismML's", localmodel.MinRotationBuild)
	}
	return ""
}

// llamaRunning is the model Irie's llama-server runs, and its pane. The
// caller holds c.mu.
func (c *Core) llamaRunning() (string, *tmux.Pane) {
	ps := c.snap.ByRole(tmux.RoleLlama)
	if len(ps) == 0 || ps[0].Dead {
		return "", nil
	}
	return ps[0].Name, &ps[0]
}

// llamaList weighs what Irie's llama-server can run against the machine's
// memory, as localModels does Ollama's, the cheapest first.
func (c *Core) llamaList(b localmodel.Build) ([]llamaModel, []ipc.LocalModel) {
	ms := c.llamaModels()
	m := c.readMachine()
	budget := localmodel.Budget(c.cfg().Local.MemoryGiB, m)
	c.mu.Lock()
	running, _ := c.llamaRunning()
	c.mu.Unlock()
	out := make([]ipc.LocalModel, 0, len(ms))
	for _, lm := range ms {
		x := ipc.LocalModel{Name: lm.name, Size: lm.g.Weights(), Need: c.llamaNeed(lm).Total, Loaded: lm.name == running, Why: llamaWhy(lm, b)}
		x.Fits = x.Need <= budget
		if sg, ok := localmodel.Suggested(lm.name); ok {
			x.About = sg.About
		}
		out = append(out, x)
	}
	order := make([]int, len(ms))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := out[order[i]], out[order[j]]
		if a.Need != b.Need {
			return a.Need < b.Need
		}
		return a.Name < b.Name
	})
	sortedMs, sortedOut := make([]llamaModel, len(ms)), make([]ipc.LocalModel, len(ms))
	for k, i := range order {
		sortedMs[k], sortedOut[k] = ms[i], out[i]
	}
	return sortedMs, sortedOut
}

// llamaPick picks the model a llama-server profile runs, as localModel
// does for Ollama: the profile's own, else the best one Irie suggests that
// this build runs and the machine can hold, else the only such one.
func llamaPick(name string, p config.Profile, list []ipc.LocalModel) (string, error) {
	if p.Model != "" {
		if i := slices.IndexFunc(list, func(m ipc.LocalModel) bool { return localmodel.SameModel(m.Name, p.Model) }); i >= 0 {
			return list[i].Name, nil
		}
		return "", ipc.Errorf(ipc.CodeNotFound, "+%s: no model %s among Ollama's models and the GGUF files Irie's llama-server may run", name, p.Model)
	}
	if best := bestBy(list, localmodel.LlamaRank); best != "" {
		return best, nil
	}
	var ok []string
	for _, m := range list {
		if runnable(m) {
			ok = append(ok, m.Name)
		}
	}
	switch len(ok) {
	case 0:
		return "", ipc.Errorf(ipc.CodeNotFound, "+%s: no model this llama-server can run that fits this machine; put a GGUF file in local.models, or pull one with Ollama", name)
	case 1:
		return ok[0], nil
	}
	return "", ipc.Errorf(ipc.CodeAmbiguous, "+%s has no model yet; name one in its model, such as %s", name, strings.Join(ok, ", "))
}

// startLlama starts Irie's own llama-server on model m, in place of the
// one it runs, and waits until the model is loaded: llama-server under
// `irie runtime`, which stops it with its window, in its own window, with
// Irie's context and cache type in one slot, then local.llama_args. A
// server that exits first is an error with the last lines it printed.
func (c *Core) startLlama(bin string, m llamaModel) error {
	l := c.cfg().Local
	o := localmodel.Llama{URL: c.llamaURL(), HTTP: c.http}
	answers := func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_, err := o.Ready(ctx)
		return err == nil
	}
	c.mu.Lock()
	if err := c.refresh(); err != nil {
		c.mu.Unlock()
		return err
	}
	if olds := c.snap.ByRole(tmux.RoleLlama); len(olds) > 0 {
		var cmds [][]string
		for _, p := range olds {
			cmds = append(cmds, []string{"kill-pane", "-t", p.ID})
		}
		c.tmux.RunList(cmds)
		c.refresh()
		c.mu.Unlock()
		// The old server lets go of its port as it exits, which `irie
		// runtime` gives it a few seconds for.
		for deadline := time.Now().Add(15 * time.Second); answers() && time.Now().Before(deadline); {
			time.Sleep(100 * time.Millisecond)
		}
		c.mu.Lock()
	} else if answers() {
		c.mu.Unlock()
		return fmt.Errorf("%s already runs a server Irie did not start; stop it, or set local.llama_port", o.URL)
	}
	args := []string{"new-window", "-d", "-t", tmux.AgentsSession + ":", "-n", llamaWindow, "-P", "-F", "#{pane_id}", "--",
		c.bin, "runtime", "--", bin, "-m", m.path, "--alias", m.name, "--host", "127.0.0.1", "--port", strconv.Itoa(l.LlamaPort),
		"-c", strconv.Itoa(l.Context), "-ctk", l.KVCache, "-ctv", l.KVCache, "-fa", "on", "-np", "1"}
	args = append(args, l.LlamaArgs...)
	out, err := c.tmux.Run(args...)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	pane := strings.TrimSpace(out)
	if _, err := c.tmux.RunList([][]string{
		{"set-option", "-p", "-t", pane, tmux.OptRole, tmux.RoleLlama},
		{"set-option", "-p", "-t", pane, tmux.OptName, m.name},
	}); err != nil {
		c.tmux.Run("kill-pane", "-t", pane)
		c.mu.Unlock()
		return err
	}
	c.refresh()
	c.mu.Unlock()
	c.log.Info("llama-server starting", "url", o.URL, "bin", bin, "model", m.name, "path", m.path, "pane", pane)

	ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
	defer cancel()
	var tail []string
	err = o.WaitReady(ctx, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.refresh()
		if p := c.snap.Pane(pane); p == nil || p.Dead {
			tail = c.tail(pane)
			return true
		}
		return false
	})
	if err != nil && tail != nil {
		return fmt.Errorf("llama-server exited:%s", strings.Join(append([]string{""}, tail...), "\n  "))
	}
	return err
}

// checkLlama checks model m for profile p (DESIGN §6.1, §6.5), the way
// Preflight checks an Ollama model, and loads it: it refuses a model the
// build cannot run right, one that cannot call tools, one trained for less
// than the profile's context, and one the machine cannot hold, then
// starts Irie's llama-server on it unless it runs it already, and checks
// the context the server gave it. With force a model short of free memory,
// or one in place of another that live sessions run on, loads anyway.
func (c *Core) checkLlama(bin string, b localmodel.Build, m llamaModel, p config.Profile, force bool) localmodel.Check {
	chk := localmodel.Check{URL: c.llamaURL(), Model: m.name}
	problem := func(format string, a ...any) { chk.Problems = append(chk.Problems, fmt.Sprintf(format, a...)) }
	warn := func(format string, a ...any) { chk.Warnings = append(chk.Warnings, fmt.Sprintf(format, a...)) }
	info := m.g.Info()
	if prob, w := b.Runs(m.g); prob != "" {
		problem("%s: %s", m.name, prob)
	} else if w != "" {
		warn("%s: %s", m.name, w)
	}
	if !info.Tools() {
		problem("%s cannot call tools, which Claude Code needs: its chat template takes none", m.name)
	}
	if info.Context > 0 && info.Context < p.Local.MinContext {
		problem("%s takes at most %s tokens of context and the profile needs %s", m.name, localmodel.Tokens(info.Context), localmodel.Tokens(p.Local.MinContext))
	}
	if !chk.OK() {
		return chk
	}

	c.llamaMu.Lock()
	defer c.llamaMu.Unlock()
	c.mu.Lock()
	c.refresh()
	running, _ := c.llamaRunning()
	byProfile := map[string][]string{}
	for _, x := range c.sessions {
		if x.local && !x.dead && x.model == running && running != m.name {
			byProfile[x.profile] = append(byProfile[x.profile], x.name)
		}
	}
	c.mu.Unlock()
	var users []string
	for prof, names := range byProfile {
		if q, ok := c.profile(prof); ok && isLlama(q) {
			users = append(users, names...)
		}
	}
	slices.Sort(users)
	need := c.llamaNeed(m)
	chk.Need = need.Total
	if running != m.name {
		machine := c.readMachine()
		budget := localmodel.Budget(c.cfg().Local.MemoryGiB, machine)
		c.mu.Lock()
		freed := c.llamaMem // what the model it runs now gives back
		c.mu.Unlock()
		short := false
		if need.Total > budget {
			short = true
			problem("%s needs about %s of memory at a %s context (%s of weights, %s of context cache), more than the %s local models may take here; pick a smaller model or packing, or raise local.memory_gib", m.name, localmodel.Bytes(need.Total), localmodel.Tokens(c.cfg().Local.Context), localmodel.Bytes(need.Weights), localmodel.Bytes(need.Cache), localmodel.Bytes(budget))
		}
		if free := machine.Available + freed - localmodel.Reserve(machine); machine.Total > 0 && need.Total > free && !short {
			msg := fmt.Sprintf("%s needs about %s of memory and the machine has %s free, of which Irie leaves %s for everything else", m.name, localmodel.Bytes(need.Total), localmodel.Bytes(machine.Available+freed), localmodel.Bytes(localmodel.Reserve(machine)))
			if force {
				warn("%s; loading it anyway", msg)
			} else {
				chk.Short = true
				problem("%s; close something first, pick a smaller model, or spawn with --force", msg)
			}
		}
		if len(users) > 0 {
			msg := fmt.Sprintf("Irie's llama-server runs %s for %s, and runs one model at a time", running, strings.Join(users, ", "))
			if force {
				warn("%s; %s takes its place", msg, m.name)
			} else {
				problem("%s; wait for them, or spawn with --force to switch it to %s", msg, m.name)
			}
		}
		if !chk.OK() {
			return chk
		}
		if err := c.startLlama(bin, m); err != nil {
			problem("llama-server could not load %s: %v", m.name, err)
			return chk
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	props, err := localmodel.Llama{URL: c.llamaURL(), HTTP: c.http}.Props(ctx)
	if err != nil {
		problem("llama-server at %s: %v", c.llamaURL(), err)
		return chk
	}
	chk.Context, chk.Loaded = props.Context, true
	if chk.Context < p.Local.MinContext {
		problem("%s runs with a %s context at %s and the profile needs %s; raise local.context in Irie's config", m.name, localmodel.Tokens(chk.Context), c.llamaURL(), localmodel.Tokens(p.Local.MinContext))
	}
	return chk
}

// llamaTarget finds the model profile p runs on Irie's llama-server.
func (c *Core) llamaTarget(name string, p config.Profile) (string, localmodel.Build, llamaModel, error) {
	bin, b, err := c.llamaBuild()
	if err != nil {
		return "", b, llamaModel{}, ipc.Errorf(ipc.CodeBadArgs, "+%s: %v", name, err)
	}
	ms, list := c.llamaList(b)
	model, err := llamaPick(name, p, list)
	if err != nil {
		return bin, b, llamaModel{}, err
	}
	i := slices.IndexFunc(ms, func(m llamaModel) bool { return m.name == model })
	return bin, b, ms[i], nil
}

// prepareLlama readies llama-server profile p for a spawn, as prepareLocal
// does an Ollama one: the model is picked, checked and loaded, and p gets
// it and the environment that points Claude Code at the server.
func (c *Core) prepareLlama(name string, p *config.Profile, force bool) ([]string, error) {
	bin, b, m, err := c.llamaTarget(name, *p)
	if err != nil {
		return nil, err
	}
	chk := c.checkLlama(bin, b, m, *p, force)
	var warnings []string
	if !chk.OK() {
		if !force && p.Local.Enforce {
			msg := strings.Join(chk.Problems, "; ")
			if chk.Short {
				_, list := c.llamaList(b)
				c.mu.Lock()
				freed := c.llamaMem
				c.mu.Unlock()
				if alt := fitsNow(list, m.name, c.readMachine(), freed, localmodel.LlamaRank); alt != "" {
					msg += fmt.Sprintf("; %s fits now (+%s%s)", alt, llamaPrefix, alt)
				}
			}
			return nil, ipc.Errorf(ipc.CodeBadArgs, "+%s on %s: %s", name, m.name, msg)
		}
		warnings = append(warnings, chk.Problems...)
	}
	warnings = append(warnings, chk.Warnings...)
	p.Model = m.name
	env := claude.LocalEnv(m.name, c.llamaURL(), chk.Context)
	maps.Copy(env, p.Env)
	p.Env = env
	return warnings, nil
}

// llamaStatus is the llama-server part of `irie ctl local`.
func (c *Core) llamaStatus() *ipc.LlamaStatus {
	r := &ipc.LlamaStatus{URL: c.llamaURL(), Dir: c.modelsDir(), Models: []ipc.LocalModel{}}
	bin, b, err := c.llamaBuild()
	r.Bin = bin
	if err != nil {
		r.Error = err.Error()
	} else {
		r.Build = b.String()
	}
	_, r.Models = c.llamaList(b)
	c.mu.Lock()
	r.Running, _ = c.llamaRunning()
	c.mu.Unlock()
	if p, ok := c.profile("big"); ok && isLlama(p) {
		r.Pick, _ = llamaPick("big", p, r.Models)
	}
	return r
}

// llamaReady says whether a llama-server profile can take a task now, as
// localReady does for Ollama's: llama-server is there, the profile has a
// model this build runs, and the machine can hold it.
func (c *Core) llamaReady(name string, p config.Profile) (need, budget int64, note string, ok bool) {
	_, b, err := c.llamaBuild()
	if err != nil {
		return 0, 0, err.Error(), false
	}
	_, list := c.llamaList(b)
	model, err := llamaPick(name, p, list)
	if err != nil {
		return 0, 0, err.Error(), false
	}
	m := list[slices.IndexFunc(list, func(m ipc.LocalModel) bool { return m.Name == model })]
	machine := c.readMachine()
	budget = localmodel.Budget(c.cfg().Local.MemoryGiB, machine)
	switch {
	case m.Why != "":
		return 0, 0, model + " " + m.Why, false
	case !m.Fits:
		return 0, 0, model + " is too big for this machine", false
	case m.Loaded:
		return m.Need, budget, model + " is in memory", true
	}
	c.mu.Lock()
	freed := c.llamaMem
	c.mu.Unlock()
	if free := machine.Available + freed - localmodel.Reserve(machine); machine.Total > 0 && m.Need > free {
		return 0, 0, fmt.Sprintf("%s needs %s and %s is free", model, localmodel.Bytes(m.Need), localmodel.Bytes(max(free, 0))), false
	}
	return m.Need, budget, model, true
}

// llamaNote is what a llama-server profile runs and costs, for the + list.
func (c *Core) llamaNote(name string, p config.Profile) string {
	cost := "local, $0"
	if p.Cost != "" {
		cost = p.Cost
	}
	_, b, err := c.llamaBuild()
	if err != nil {
		return cost + " · no llama-server: brew install llama.cpp"
	}
	_, list := c.llamaList(b)
	model, err := llamaPick(name, p, list)
	if err != nil {
		return cost + " · no model it can run here yet"
	}
	m := list[slices.IndexFunc(list, func(m ipc.LocalModel) bool { return m.Name == model })]
	c.mu.Lock()
	mem := c.memoryNow()
	c.mu.Unlock()
	parts := []string{model, cost, memoryNote(m, mem)}
	if m.About != "" {
		parts = append(parts, m.About)
	}
	return strings.Join(parts, " · ")
}

// fitsNow is the best model of list, other than model, that an agent can
// run on and the machine can hold now, with freed given back: the best one
// Irie suggests, else the largest. "" when there is none.
func fitsNow(list []ipc.LocalModel, model string, m localmodel.Machine, freed int64, rank func(string) int) string {
	free := m.Available + freed - localmodel.Reserve(m)
	var ok []ipc.LocalModel
	for _, x := range list {
		if x.Name != model && runnable(x) && (x.Need <= free || x.Loaded) {
			ok = append(ok, x)
		}
	}
	if best := bestBy(ok, rank); best != "" {
		return best
	}
	best, need := "", int64(0)
	for _, x := range ok {
		if x.Need > need {
			best, need = x.Name, x.Need
		}
	}
	return best
}
