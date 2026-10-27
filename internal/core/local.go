package core

// Local models (DESIGN §6.1, §6.3). A local profile runs Claude Code on a
// model of an Ollama server: Irie's own, which runs in a window of
// irie-agents like everything else Irie runs, or one the user runs at
// local.url. Irie's own server takes its context, cache type and slots from
// Irie's config, so the user's own Ollama settings never change, and it
// shares the user's model store, so nothing downloads twice. Before an
// agent is spawned on a model, the model is checked: its context, and what
// it takes of the machine's memory.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/harness/claude"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/localmodel"
	"github.com/irie-hq/irie/internal/tmux"
)

// localPrefix names a local model as a profile: +ollama:qwen3.5:9b is the
// local profile on that model.
const localPrefix = "ollama:"

// runtimeWindow names the window of Irie's own Ollama server.
const runtimeWindow = "irie-ollama"

// runtimeWait is how long a new server has to answer.
var runtimeWait = 15 * time.Second

// loadTimeout bounds a check, which may load a model's weights from disk.
const loadTimeout = 3 * time.Minute

// modelsTTL is how long a server's model list is reused.
const modelsTTL = 10 * time.Second

// errLocalDown means Irie's own server is not running and was not started.
var errLocalDown = errors.New("Irie's Ollama server is not running")

// localState is local.json: what Irie keeps about local models.
type localState struct {
	// Model is what local profiles with no model of their own run: the
	// one `irie setup local` chose.
	Model string `json:"model,omitempty"`
	// Measured is what models took in memory once loaded, by model,
	// context and cache type, which beats an estimate.
	Measured map[string]int64 `json:"measured,omitempty"`
}

func measureKey(model string, context int, kv string) string {
	return model + "|" + strconv.Itoa(context) + "|" + kv
}

// modelList is a server's models, weighed, as the core last read them.
type modelList struct {
	url    string
	at     time.Time
	models []ipc.LocalModel
	infos  map[string]localmodel.Info // by digest
}

func (c *Core) loadLocal() {
	b, err := os.ReadFile(c.paths.LocalFile())
	if err != nil {
		return
	}
	if err := json.Unmarshal(b, &c.local); err != nil {
		c.log.Warn("local.json", "err", err)
	}
}

// saveLocal writes local.json. The caller holds c.mu.
func (c *Core) saveLocal() {
	if err := writeJSON(c.paths.LocalFile(), c.local); err != nil {
		c.log.Warn("local.json", "err", err)
	}
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// profile returns the named profile, ready to change: one of the config's,
// or ollama:<model>, the local profile on that model.
func (c *Core) profile(name string) (config.Profile, bool) {
	p, ok := c.cfg.Profiles[name]
	if !ok {
		model, isLocal := strings.CutPrefix(name, localPrefix)
		if !isLocal || model == "" {
			return config.Profile{}, false
		}
		if p, ok = c.cfg.Profiles["local"]; !ok || p.Local == nil {
			p = config.Default().Profiles["local"]
		}
		p.Model = model
	}
	p.Args, p.Env = slices.Clone(p.Args), maps.Clone(p.Env)
	if p.Env == nil {
		p.Env = map[string]string{}
	}
	if p.Local != nil {
		l := *p.Local
		p.Local = &l
	}
	return p, true
}

func (c *Core) ownURL() string { return fmt.Sprintf("http://127.0.0.1:%d", c.cfg.Local.Port) }

// runtimePane is the pane of Irie's own server. The caller holds c.mu.
func (c *Core) runtimePane() *tmux.Pane {
	if ps := c.snap.ByRole(tmux.RoleRuntime); len(ps) > 0 {
		return &ps[0]
	}
	return nil
}

// ollama returns the server local profiles run on, and whether it is
// Irie's own. Irie's own is started when start is set and it is not
// running. The caller holds neither c.mu nor c.localMu.
func (c *Core) ollama(start bool) (localmodel.Ollama, bool, error) {
	if c.cfg.Local.URL != "" {
		return localmodel.Ollama{URL: c.cfg.Local.URL, HTTP: c.http}, false, nil
	}
	o := localmodel.Ollama{URL: c.ownURL(), HTTP: c.http}
	c.mu.Lock()
	p := c.runtimePane()
	c.mu.Unlock()
	switch {
	case p != nil && !p.Dead:
		return o, true, nil
	case !start:
		return o, true, errLocalDown
	}
	return o, true, c.startRuntime(o)
}

// runtimeEnv is the environment of Irie's own server: its address,
// context, cache type and slots, then local.env. One slot and one model at
// a time keep its memory to one model and one context cache.
func (c *Core) runtimeEnv() []string {
	l := c.cfg.Local
	env := map[string]string{
		"OLLAMA_HOST":              fmt.Sprintf("127.0.0.1:%d", l.Port),
		"OLLAMA_CONTEXT_LENGTH":    strconv.Itoa(l.Context),
		"OLLAMA_KV_CACHE_TYPE":     l.KVCache,
		"OLLAMA_NUM_PARALLEL":      "1",
		"OLLAMA_MAX_LOADED_MODELS": "1",
	}
	if l.KVCache != "f16" {
		// A quantised cache needs flash attention.
		env["OLLAMA_FLASH_ATTENTION"] = "1"
	}
	maps.Copy(env, l.Env)
	out := make([]string, 0, len(env))
	for _, k := range slices.Sorted(maps.Keys(env)) {
		out = append(out, k+"="+env[k])
	}
	return out
}

// parallel is how many requests a model of the local server serves at
// once, each with its own context cache.
func (c *Core) parallel() int {
	if n, err := strconv.Atoi(c.cfg.Local.Env["OLLAMA_NUM_PARALLEL"]); err == nil && n > 0 {
		return n
	}
	return 1
}

// startRuntime starts Irie's own server, `ollama serve` under `irie
// runtime`, which stops its model runners with it, in its window, and waits
// until it answers. A server that dies first is an error with the last
// lines it printed.
func (c *Core) startRuntime(o localmodel.Ollama) error {
	c.localMu.Lock()
	defer c.localMu.Unlock()
	bin, err := localmodel.FindOllama(c.cfg.Local.Ollama)
	if errors.Is(err, localmodel.ErrNoOllama) {
		return errors.New("ollama is not installed; `irie setup local` installs it")
	}
	if err != nil {
		return err
	}

	c.mu.Lock()
	if err := c.refresh(); err != nil {
		c.mu.Unlock()
		return err
	}
	p := c.runtimePane()
	if p != nil && !p.Dead {
		c.mu.Unlock()
		return nil // another call started it meanwhile
	}
	if p == nil {
		// Nothing of Irie's has the port, so something else does.
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, err := o.Version(ctx)
		cancel()
		if err == nil {
			c.mu.Unlock()
			return fmt.Errorf("%s already runs a server Irie did not start; stop it, or set local.port", o.URL)
		}
	}
	args := []string{"new-window", "-d", "-t", tmux.AgentsSession + ":", "-n", runtimeWindow, "-P", "-F", "#{pane_id}"}
	for _, kv := range c.runtimeEnv() {
		args = append(args, "-e", kv)
	}
	args = append(args, "--", c.bin, "runtime", "--", bin, "serve")
	var cmds [][]string
	if p != nil {
		cmds = append(cmds, []string{"kill-pane", "-t", p.ID})
	}
	out, err := c.tmux.RunList(append(cmds, args))
	if err != nil {
		c.mu.Unlock()
		return err
	}
	pane := strings.TrimSpace(out)
	if _, err := c.tmux.Run("set-option", "-p", "-t", pane, tmux.OptRole, tmux.RoleRuntime); err != nil {
		c.tmux.Run("kill-pane", "-t", pane)
		c.mu.Unlock()
		return err
	}
	c.refresh()
	c.mu.Unlock()
	c.log.Info("local server starting", "url", o.URL, "bin", bin, "pane", pane)

	deadline := time.Now().Add(runtimeWait)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, err := o.Version(ctx)
		cancel()
		if err == nil {
			return nil
		}
		c.mu.Lock()
		c.refresh()
		p := c.runtimePane()
		var tail []string
		if p == nil || p.Dead {
			tail = c.tail(pane)
		}
		c.mu.Unlock()
		if p == nil || p.Dead {
			return fmt.Errorf("Irie's Ollama server exited:%s", strings.Join(append([]string{""}, tail...), "\n  "))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Irie's Ollama server did not answer at %s within %v: %v", o.URL, runtimeWait, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// machine reads the machine's memory, keeping the last reading when it
// cannot.
func (c *Core) readMachine() localmodel.Machine {
	m, err := localmodel.ReadMachine()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.machine = m
	}
	return c.machine
}

// serverContext is the context the server gives a model, as far as Irie
// knows: its own server's, else Ollama's guidance for agents.
func (c *Core) serverContext(own bool) int {
	if own {
		return c.cfg.Local.Context
	}
	return config.DefaultMinContext
}

func (c *Core) serverKV(own bool) string {
	if own {
		return c.cfg.Local.KVCache
	}
	return "f16"
}

// localModels returns the server's models, each weighed against the
// machine's memory: what it takes at the server's context, measured once it
// has been loaded, else estimated from its weights and its context cache's
// shape. The cheapest come first. A list newer than modelsTTL is reused.
func (c *Core) localModels(ctx context.Context, o localmodel.Ollama, own bool) ([]ipc.LocalModel, error) {
	c.modelMu.Lock()
	defer c.modelMu.Unlock()
	if c.modelCache.url == o.URL && time.Since(c.modelCache.at) < modelsTTL {
		return slices.Clone(c.modelCache.models), nil
	}
	models, err := o.Models(ctx)
	if err != nil {
		return nil, err
	}
	loaded, err := o.Running(ctx)
	if err != nil {
		return nil, err
	}
	if c.modelCache.infos == nil {
		c.modelCache.infos = map[string]localmodel.Info{}
	}
	m := c.readMachine()
	budget := localmodel.Budget(c.cfg.Local.MemoryGiB, m)
	serverCtx, kv := c.serverContext(own), c.serverKV(own)
	c.mu.Lock()
	measured := maps.Clone(c.local.Measured)
	c.mu.Unlock()

	var out []ipc.LocalModel
	for _, md := range models {
		info, ok := c.modelCache.infos[md.Digest]
		if !ok || md.Digest == "" {
			if info, err = o.Show(ctx, md.Name); err != nil {
				continue // removed meanwhile
			}
			c.modelCache.infos[md.Digest] = info
		}
		effective := serverCtx
		if info.Context > 0 && info.Context < effective {
			effective = info.Context
		}
		lm := ipc.LocalModel{Name: md.Name, Size: md.Size, Remote: md.Remote}
		lm.Need = localmodel.Estimate(md.Size, info, serverCtx, kv, c.parallel()).Total
		if n, ok := measured[measureKey(md.Name, effective, kv)]; ok {
			lm.Need, lm.Measured = n, true
		}
		for _, l := range loaded {
			if l.Name == md.Name {
				lm.Loaded = true
				if l.Size > 0 {
					lm.Need, lm.Measured = l.Size, true
				}
			}
		}
		lm.Fits = md.Remote || lm.Need <= budget
		switch {
		case !info.Tools():
			lm.Why = "cannot call tools"
		case info.Context > 0 && info.Context < config.DefaultMinContext:
			lm.Why = "takes at most " + localmodel.Tokens(info.Context) + " tokens of context"
		}
		out = append(out, lm)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Need != out[j].Need {
			return out[i].Need < out[j].Need
		}
		return out[i].Name < out[j].Name
	})
	c.modelCache.url, c.modelCache.at, c.modelCache.models = o.URL, time.Now(), out
	return slices.Clone(out), nil
}

// forgetModels drops the reused model list, after a load or a pull.
func (c *Core) forgetModels() {
	c.modelMu.Lock()
	c.modelCache.at = time.Time{}
	c.modelMu.Unlock()
}

// runnable reports whether a coding agent can run on a local model here.
func runnable(m ipc.LocalModel) bool { return m.Why == "" && m.Fits && !m.Remote }

// localModel picks the model a local profile runs: the profile's own, else
// the one `irie setup local` chose, else the only one the server has that
// an agent can run on and the machine can hold. Several are listed, the
// cheapest in memory first, rather than guessed between.
func (c *Core) localModel(ctx context.Context, o localmodel.Ollama, own bool, name string, p config.Profile) (string, error) {
	if p.Model != "" {
		return p.Model, nil
	}
	c.mu.Lock()
	chosen := c.local.Model
	c.mu.Unlock()
	if chosen != "" {
		return chosen, nil
	}
	ms, err := c.localModels(ctx, o, own)
	if err != nil {
		return "", ipc.Errorf(ipc.CodeBadArgs, "+%s: Ollama at %s: %v", name, o.URL, err)
	}
	var ok []string
	for _, m := range ms {
		if runnable(m) {
			ok = append(ok, fmt.Sprintf("+%s%s (%s)", localPrefix, m.Name, localmodel.Bytes(m.Need)))
		}
	}
	switch len(ok) {
	case 0:
		return "", ipc.Errorf(ipc.CodeNotFound, "+%s: no local model that fits this machine yet; `irie setup local` picks one and pulls it", name)
	case 1:
		model, _, _ := strings.Cut(strings.TrimPrefix(ok[0], "+"+localPrefix), " (")
		return model, nil
	}
	return "", ipc.Errorf(ipc.CodeAmbiguous, "+%s has no model yet; pick one of %s, or run `irie setup local` to choose one for it", name, strings.Join(ok, ", "))
}

// check checks model for profile p (DESIGN §6.1) and keeps what a loaded
// model took in memory, so later estimates are measurements.
func (c *Core) check(ctx context.Context, o localmodel.Ollama, own bool, model string, p config.Profile, force bool) localmodel.Check {
	m := c.readMachine()
	w := localmodel.Want{
		MinContext: p.Local.MinContext, KVCache: c.serverKV(own), Parallel: c.parallel(), Own: own,
		Budget: localmodel.Budget(c.cfg.Local.MemoryGiB, m), Machine: m, Force: force,
	}
	if own {
		w.Context = c.cfg.Local.Context
	}
	chk := localmodel.Preflight(ctx, o, model, w)
	c.forgetModels()
	if chk.Loaded && chk.Need > 0 && chk.Context > 0 {
		c.mu.Lock()
		if c.local.Measured == nil {
			c.local.Measured = map[string]int64{}
		}
		if key := measureKey(model, chk.Context, w.KVCache); c.local.Measured[key] != chk.Need {
			c.local.Measured[key] = chk.Need
			c.saveLocal()
		}
		c.mu.Unlock()
	}
	return chk
}

// evictions warns that loading model on Irie's own server, which holds one
// model at a time, unloads one that live sessions run on, so the two take
// turns in memory.
func (c *Core) evictions(model string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	by := map[string][]string{}
	for _, x := range c.sessions {
		if x.local && !x.dead && x.model != "" && x.model != model {
			by[x.model] = append(by[x.model], x.name)
		}
	}
	var out []string
	for _, other := range slices.Sorted(maps.Keys(by)) {
		names := by[other]
		slices.Sort(names)
		out = append(out, fmt.Sprintf("%s takes the place of %s in memory, which %s runs on; they will take turns loading", model, other, strings.Join(names, ", ")))
	}
	return out
}

// prepareLocal readies local profile p for a spawn (DESIGN §6.1): the
// server runs, the model is picked and checked, and p gets the model and
// the environment that points Claude Code at it, under the profile's own
// env. A failed check refuses the spawn, unless force or the profile's
// enforce = false lets it go ahead with warnings.
func (c *Core) prepareLocal(name string, p *config.Profile, force bool) ([]string, error) {
	o, own, err := c.ollama(true)
	if err != nil {
		return nil, ipc.Errorf(ipc.CodeBadArgs, "+%s: %v", name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
	defer cancel()
	model, err := c.localModel(ctx, o, own, name, *p)
	if err != nil {
		return nil, err
	}
	chk := c.check(ctx, o, own, model, *p, force)
	var warnings []string
	if !chk.OK() {
		if !force && p.Local.Enforce {
			return nil, ipc.Errorf(ipc.CodeBadArgs, "+%s on %s: %s", name, model, strings.Join(chk.Problems, "; "))
		}
		warnings = append(warnings, chk.Problems...)
	}
	warnings = append(warnings, chk.Warnings...)
	if own {
		warnings = append(warnings, c.evictions(model)...)
	}
	p.Model = model
	env := claude.LocalEnv(model, o.URL, chk.Context)
	maps.Copy(env, p.Env)
	p.Env = env
	return warnings, nil
}

// preflight is `irie ctl preflight`: the check of a spawn, without the
// spawn. With remember, a model that passes becomes the one local profiles
// with no model of their own run.
func (c *Core) preflight(a ipc.PreflightArgs) (ipc.PreflightResult, error) {
	name := a.Profile
	if name == "" {
		name = "local"
	}
	p, ok := c.profile(name)
	if !ok {
		return ipc.PreflightResult{}, ipc.Errorf(ipc.CodeNotFound, "no profile +%s", name)
	}
	if p.Local == nil {
		return ipc.PreflightResult{}, ipc.Errorf(ipc.CodeBadArgs, "+%s does not run on a local model", name)
	}
	o, own, err := c.ollama(true)
	if err != nil {
		return ipc.PreflightResult{URL: o.URL, Problems: []string{err.Error()}}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
	defer cancel()
	if a.Model != "" {
		p.Model = a.Model
	}
	model, err := c.localModel(ctx, o, own, name, p)
	if err != nil {
		return ipc.PreflightResult{URL: o.URL, Problems: []string{err.Error()}}, nil
	}
	chk := c.check(ctx, o, own, model, p, a.Force)
	if own {
		chk.Warnings = append(chk.Warnings, c.evictions(model)...)
	}
	if a.Remember && chk.OK() {
		c.mu.Lock()
		c.local.Model = model
		c.saveLocal()
		c.mu.Unlock()
	}
	return ipc.PreflightResult{
		OK: chk.OK(), URL: o.URL, Model: model, Context: chk.Context, Need: chk.Need, VRAM: chk.VRAM,
		Problems: chk.Problems, Warnings: chk.Warnings,
	}, nil
}

// localStatus is `irie ctl local`: the server, the machine's memory and the
// models weighed against it.
func (c *Core) localStatus(a ipc.LocalArgs) (ipc.LocalResult, error) {
	o, own, err := c.ollama(a.Start)
	c.readMachine()
	c.mu.Lock()
	r := ipc.LocalResult{URL: o.URL, Own: own, Chosen: c.local.Model, Context: c.serverContext(own), KVCache: c.serverKV(own), Memory: c.memoryNow(), Models: []ipc.LocalModel{}}
	c.mu.Unlock()
	if err != nil {
		r.Error = err.Error()
		return r, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if r.Version, err = o.Version(ctx); err != nil {
		r.Error = fmt.Sprintf("not reachable at %s: %v", o.URL, err)
		return r, nil
	}
	r.Running = true
	if ms, err := c.localModels(ctx, o, own); err != nil {
		r.Error = err.Error()
	} else {
		r.Models = ms
	}
	return r, nil
}

// httpClient is for local servers; every call carries a context.
func httpClient() *http.Client { return &http.Client{Transport: &http.Transport{Proxy: nil}} }
