package core

// The + list says what each profile runs and costs (DESIGN §6.3). A Claude
// Code profile costs tokens: its note gives its model and effort, how much
// of the plan's five hours is used, and what its sessions have cost on
// average. Every profile also costs memory: what a session of it takes,
// measured, and for a local one what its model takes against what this
// machine can give. Local models come after the profiles, the cheapest in
// memory first.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/localmodel"
)

// tabWait bounds what Tab waits for the local server's model list.
const tabWait = 2 * time.Second

// server states for the + list's notes.
const (
	serverNone     = ""         // no local server, and no ollama to start one
	serverStarting = "starting" // Irie's own is starting; a later Tab lists its models
	serverDown     = "down"     // it does not answer
	serverUp       = "up"
)

// tabModels returns the local server's models for the + list. Irie's own
// server starts in the background when Ollama is installed, so a later Tab
// lists its models; Tab never waits for it.
func (c *Core) tabModels() ([]ipc.LocalModel, string) {
	o, own, err := c.ollama(false)
	if errors.Is(err, errLocalDown) {
		if _, err := localmodel.FindOllama(c.cfg.Local.Ollama); err != nil {
			return nil, serverNone
		}
		go func() {
			if _, _, err := c.ollama(true); err != nil {
				c.log.Warn("local server", "err", err)
			}
		}()
		return nil, serverStarting
	}
	ctx, cancel := context.WithTimeout(context.Background(), tabWait)
	defer cancel()
	ms, err := c.localModels(ctx, o, own)
	if err != nil {
		return nil, serverDown
	}
	return ms, serverUp
}

// completeProfiles returns the + tokens that complete text, with a note on
// what each runs and costs.
func (c *Core) completeProfiles(text string) ([]string, []string) {
	prefix := strings.TrimPrefix(text, "+")
	items, notes := []string{}, []string{}
	var models []ipc.LocalModel
	server := serverNone
	if strings.HasPrefix(localPrefix, prefix) || strings.HasPrefix(prefix, localPrefix) || strings.HasPrefix("local", prefix) {
		models, server = c.tabModels()
	} else {
		for name, p := range c.cfg.Profiles {
			if p.Local != nil && strings.HasPrefix(name, prefix) {
				models, server = c.tabModels()
				break
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(c.cfg.Profiles)) {
		if strings.HasPrefix(config.Auto, prefix) && config.Auto < name && !slices.Contains(items, "+"+config.Auto) {
			items = append(items, "+"+config.Auto)
			notes = append(notes, c.autoNote())
		}
		if strings.HasPrefix(name, prefix) {
			items = append(items, "+"+name)
			notes = append(notes, c.profileNote(name, c.cfg.Profiles[name], models, server))
		}
	}
	if strings.HasPrefix(config.Auto, prefix) && !slices.Contains(items, "+"+config.Auto) {
		items = append(items, "+"+config.Auto)
		notes = append(notes, c.autoNote())
	}
	for _, m := range models {
		if name := localPrefix + m.Name; runnable(m) && strings.HasPrefix(name, prefix) && c.cfg.Profiles[name].Harness == "" {
			items = append(items, "+"+name)
			c.mu.Lock()
			mem := c.memoryNow()
			c.mu.Unlock()
			notes = append(notes, strings.Join([]string{"local, $0", memoryNote(m, mem)}, " · "))
		}
	}
	return items, notes
}

// profileNote is what a profile runs and costs.
func (c *Core) profileNote(name string, p config.Profile, models []ipc.LocalModel, server string) string {
	c.mu.Lock()
	chosen, plan, mem := c.local.Model, c.plan, c.memoryNow()
	perSession, perCost := c.sessionCosts(name)
	c.mu.Unlock()

	var parts []string
	add := func(s string) {
		if s != "" {
			parts = append(parts, s)
		}
	}
	switch {
	case p.Local != nil:
		model := p.Model
		if model == "" {
			model = chosen
		}
		cost := "local, $0"
		if p.Cost != "" {
			cost = p.Cost
		}
		if model == "" {
			add(cost)
			add("no model yet: irie setup local")
			break
		}
		add(model)
		add(cost)
		if i := slices.IndexFunc(models, func(m ipc.LocalModel) bool { return m.Name == model }); i >= 0 {
			add(memoryNote(models[i], mem))
		} else if server == serverStarting {
			add("starting Irie's Ollama server")
		}
	case p.Harness == "claude-code":
		add(strings.TrimSpace(strings.TrimPrefix(p.Model, "claude-") + " " + p.Effort))
		if base := p.Env["ANTHROPIC_BASE_URL"]; base != "" {
			if u, err := url.Parse(base); err == nil && u.Host != "" {
				base = u.Host
			}
			add("via " + base)
		} else if plan.FiveHour != nil {
			add(fmt.Sprintf("plan 5h %.0f%%", plan.FiveHour.Used))
		}
		add(p.Cost)
		if perCost > 0 {
			add(fmt.Sprintf("~$%.2f a session", perCost))
		}
	default:
		add(p.Command)
		add(p.Cost)
	}
	if perSession > 0 {
		add("~" + localmodel.Bytes(perSession) + " a session")
	}
	return strings.Join(parts, " · ")
}

// sessionCosts is what the live sessions of a profile take on average: in
// memory, and in dollars at list price as their status lines said. The
// caller holds c.mu.
func (c *Core) sessionCosts(profile string) (memory int64, cost float64) {
	var mems, costs int
	for _, x := range c.sessions {
		if x.profile != profile || x.dead {
			continue
		}
		if x.memory > 0 {
			memory += x.memory
			mems++
		}
		if x.usage.CostUSD != nil && *x.usage.CostUSD > 0 && !x.local {
			cost += *x.usage.CostUSD
			costs++
		}
	}
	if mems > 0 {
		memory /= int64(mems)
	}
	if costs > 0 {
		cost /= float64(costs)
	}
	return memory, cost
}

// memoryNote is what a local model takes in memory, against what the
// machine can give: "7.9 GB", "~7.9 GB" for an estimate, "in memory" when
// it is loaded, "too big" past the budget, "short of memory" when the
// machine lacks the room now.
func memoryNote(m ipc.LocalModel, mem ipc.Memory) string {
	size := localmodel.Bytes(m.Need)
	if !m.Measured {
		size = "~" + size
	}
	switch {
	case m.Loaded:
		return size + ", in memory"
	case mem.Budget > 0 && m.Need > mem.Budget:
		return size + ", too big: " + localmodel.Bytes(mem.Budget) + " allowed"
	case mem.Total > 0 && m.Need > mem.Available+mem.Local-localmodel.Reserve(localmodel.Machine{Total: mem.Total}):
		return size + ", short of memory: " + localmodel.Bytes(mem.Available) + " free"
	}
	return size
}
