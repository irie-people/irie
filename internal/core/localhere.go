package core

// Local models and the machine's room (DESIGN §6.3). With local.here false,
// no model loads on this computer: Irie's own Ollama and llama-server never
// start, and stop once no session runs on them, and no model call goes to
// a server here, so what would use one does without, or runs where a model
// may, as +dory on a peer. Where models do load, a side task's load that
// the free memory cannot hold, or that comes while the disk is under the
// floor the swap needs, waits; and under memory pressure, what Irie's own
// servers hold for no session at work lets go.

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/sysload"
	"github.com/irie-people/irie/internal/tmux"
)

// modelsOff says why Irie may not call a model on the server at url, ""
// being its own servers, or nil when it may (config.Local.LoadsAt).
func (c *Core) modelsOff(url string) error {
	if c.cfg().Local.LoadsAt(url) {
		return nil
	}
	return localmodel.ErrOffHere
}

// chatServers are the Ollama servers a side task may chat on, such as a
// card or a draft of lessons: localmodel.ChatServers, less those on this
// computer while local models are off here, which leaves none unless
// local.url names another computer's.
func (c *Core) chatServers() []string {
	l := c.cfg().Local
	return slices.DeleteFunc(localmodel.ChatServers(l.URL, l.Port), func(u string) bool { return !l.LoadsAt(u) })
}

// sideContext is the context a side task chats at: cards, the terms'
// answers and the lessons' drafts all ask for 8192 tokens.
const sideContext = 8192

// chatModel finds the local model a side task chats with (FindChatModel)
// on the servers it may use, and makes sure the machine has room to load
// it now (roomFor). With local models off here, and no other computer's
// server named, it is localmodel.ErrOffHere.
func (c *Core) chatModel(ctx context.Context, want string) (localmodel.ChatModel, error) {
	urls := c.chatServers()
	if len(urls) == 0 {
		return localmodel.ChatModel{}, localmodel.ErrOffHere
	}
	m, err := localmodel.FindChatModel(ctx, urls, want)
	if err != nil {
		return m, err
	}
	if err := c.roomFor(ctx, m, sideContext); err != nil {
		return localmodel.ChatModel{}, err
	}
	return m, nil
}

// roomFor says why model m may not be loaded on its server now for a side
// task at ctxLen tokens, or nil when it may: it is loaded there already;
// else the disk is above the floor that leaves the swap room to grow, and
// the memory free now, with what Irie's own server lets go of first, since
// it holds one model at a time, holds the model with localmodel.Reserve
// beside. On 2026-11-23 a side task loaded a 9B model, 7.1 GB, into a Mac
// already paging, with its disk 97% full, and the Mac panicked.
func (c *Core) roomFor(ctx context.Context, m localmodel.ChatModel, ctxLen int) error {
	o := localmodel.Ollama{URL: m.URL, HTTP: c.http}
	loaded, _ := o.Running(ctx) // none, as far as it knows, from a server that does not say
	own := c.cfg().Local.URL == "" && m.URL == c.ownURL()
	var freed int64
	for _, l := range loaded {
		if localmodel.SameModel(l.Name, m.Name) {
			return nil
		}
		if own {
			freed += l.Size
		}
	}
	if why := c.loadWaits(); why != "" {
		return fmt.Errorf("%s, so %s was not loaded", why, m.Name)
	}
	models, err := o.Models(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(models, func(x localmodel.Model) bool { return localmodel.SameModel(x.Name, m.Name) })
	if i < 0 {
		return fmt.Errorf("the Ollama at %s has no model %s", m.URL, m.Name)
	}
	info, err := o.Show(ctx, m.Name)
	if err != nil {
		return err
	}
	kv := "f16"
	if own {
		kv = c.cfg().Local.KVCache
	}
	mach := c.readMachine()
	if mach.Total == 0 {
		return nil // nothing to weigh it against
	}
	mach.Available += freed
	if err := localmodel.RoomToLoad(localmodel.Estimate(models[i].Size, info, ctxLen, kv, 1), mach); err != nil {
		return fmt.Errorf("%w, so %s was not loaded", err, m.Name)
	}
	return nil
}

// loadWaits is why no model may be loaded now, in a sentence, or "" when
// one may: the disk is under the floor that leaves the swap room to grow
// (sysload.Room.HeavyWaits), as the last memory sample read it. A disk it
// cannot read holds nothing up.
func (c *Core) loadWaits() string { return c.roomNow().HeavyWaits() }

// easeGrace is how long one of Irie's own servers must have had no
// session on it before it is stopped, so that a spawn that has just
// loaded it, and whose session is not yet listed, keeps it.
var easeGrace = 30 * time.Second

// easeState is what easeModels keeps between samples.
type easeState struct {
	running sync.Mutex           // one pass at a time
	mu      sync.Mutex           // guards idle
	idle    map[string]time.Time // since when each server's pane has had no live session on it
}

// memoryPressed says memory runs short: the kernel warns of pressure, or
// less than localmodel.Reserve is free. A test core, which reads
// FakeMemoryEnv, goes by that alone, so that the user's own programs never
// press it. The caller holds c.mu.
func (c *Core) memoryPressed() bool {
	m := c.machine
	if m.Total > 0 && m.Available < localmodel.Reserve(m) {
		return true
	}
	if _, fake := fakeMemory(os.Getenv(FakeMemoryEnv)); fake {
		return false
	}
	return c.load.Pressure >= sysload.PressureWarn
}

// easeModels lets go of what Irie's own model servers hold for nobody,
// after each memory sample, in the background and one pass at a time.
// While local models are off here, its Ollama and its llama-server stop
// once no live session has run on them for easeGrace. Under memory
// pressure, the models of its Ollama that no session at work runs on go
// (keep_alive 0), Ollama loading one again at its session's next request,
// and its llama-server stops once no live session has run on it for
// easeGrace, since nothing would start it again for a session's next
// turn. A model is never taken from under a session at work.
func (c *Core) easeModels() {
	cfg := c.cfg()
	c.mu.Lock()
	pressed, off := c.memoryPressed(), !cfg.Local.Here
	var ollamaPane, llamaPane *tmux.Pane
	if rt := c.runtimePane(); cfg.Local.URL == "" && rt != nil && !rt.Dead {
		p := *rt
		ollamaPane = &p
	}
	llamaModel, lp := c.llamaRunning()
	if lp != nil {
		p := *lp
		llamaPane = &p
	}
	working := map[string]bool{} // the models sessions at work run on
	onOllama, onLlama := false, false
	for _, x := range c.sessions {
		if !x.local || x.dead || x.model == "" {
			continue
		}
		p, ok := c.profile(x.profile)
		if !ok || p.Local == nil {
			continue
		}
		switch {
		case !isLlama(p):
			onOllama = true
			if x.track.state(x.dead) == ipc.StateWorking {
				working[x.model] = true
			}
		case p.Local.Peer == "" && x.model == llamaModel:
			onLlama = true
		}
	}
	c.mu.Unlock()
	if !pressed && !off || ollamaPane == nil && llamaPane == nil {
		c.ease.mu.Lock()
		clear(c.ease.idle)
		c.ease.mu.Unlock()
		return
	}
	if !c.ease.running.TryLock() {
		return
	}
	go func() {
		defer c.ease.running.Unlock()
		now := time.Now()
		if ollamaPane != nil {
			switch {
			case off && c.idleFor(ollamaPane.ID, onOllama, now):
				c.stopServer(*ollamaPane, "local models are off on this computer (local.here = false)")
			case pressed:
				c.unloadIdle(working)
			}
		}
		if llamaPane != nil && (off || pressed) && c.idleFor(llamaPane.ID, onLlama, now) {
			why := "memory runs short"
			if off {
				why = "local models are off on this computer (local.here = false)"
			}
			c.stopServer(*llamaPane, why)
		}
	}()
}

// idleFor says the server in pane id has had no live session on it (used
// false) for easeGrace, keeping since when.
func (c *Core) idleFor(id string, used bool, now time.Time) bool {
	c.ease.mu.Lock()
	defer c.ease.mu.Unlock()
	if c.ease.idle == nil {
		c.ease.idle = map[string]time.Time{}
	}
	if used {
		delete(c.ease.idle, id)
		return false
	}
	since, ok := c.ease.idle[id]
	if !ok {
		c.ease.idle[id] = now
		return false
	}
	return now.Sub(since) >= easeGrace
}

// unloadIdle lets the models of Irie's own Ollama go that no session at
// work runs on (keep_alive 0), and says so in the log.
func (c *Core) unloadIdle(working map[string]bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o := localmodel.Ollama{URL: c.ownURL(), HTTP: c.http}
	loaded, err := o.Running(ctx)
	if err != nil {
		return
	}
	for _, l := range loaded {
		atWork := false
		for m := range working {
			atWork = atWork || localmodel.SameModel(m, l.Name)
		}
		if atWork {
			continue
		}
		if err := o.Unload(ctx, l.Name); err != nil {
			c.log.Warn("local models: unload", "model", l.Name, "err", err)
			continue
		}
		c.log.Info("local models: unloaded under memory pressure", "model", l.Name, "size", localmodel.Bytes(l.Size))
	}
	c.forgetModels()
}

// stopServer stops one of Irie's own model servers by its pane: `irie
// runtime` turns the hangup into SIGTERM for the server's process group,
// and Ollama unloads its runners as it goes (DESIGN §6.3). The core's next
// look at tmux sees the window gone.
func (c *Core) stopServer(p tmux.Pane, why string) {
	if _, err := c.tmux.Run("kill-pane", "-t", p.ID); err != nil {
		c.log.Warn("local models: stop", "pane", p.ID, "err", err)
		return
	}
	c.log.Info("local models: server stopped", "pane", p.ID, "role", p.Role, "why", why)
	c.ease.mu.Lock()
	delete(c.ease.idle, p.ID)
	c.ease.mu.Unlock()
	c.forgetModels()
}
