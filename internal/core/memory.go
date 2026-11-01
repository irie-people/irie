package core

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
)

// Memory is a cost too (DESIGN §6.3): every agent takes the machine's
// memory, and a local model takes gigabytes of it. The core measures what
// each session's processes take, what the models of Irie's own server
// take, and what the machine has free, every sampleEvery ticks.
const sampleEvery = 5

// availableStep rounds the free memory the header carries, so it changes
// when the free memory does and not with every page.
const availableStep = 100 << 20

// sample measures memory and tells subscribers what changed. One runs at a
// time; the caller holds no lock.
func (c *Core) sample() {
	if !c.sampling.CompareAndSwap(false, true) {
		return
	}
	defer c.sampling.Store(false)
	c.mu.Lock()
	roots := map[string]int{}
	for id, x := range c.sessions {
		if p := c.snap.Pane(x.pane); p != nil && !p.Dead && p.PID > 0 {
			roots[id] = p.PID
		}
	}
	rt := c.runtimePane()
	own := c.cfg().Local.URL == "" && rt != nil && !rt.Dead
	// Irie's llama-server, by a key no session has.
	const llamaKey = "\x00llama"
	if _, lp := c.llamaRunning(); lp != nil && lp.PID > 0 {
		roots[llamaKey] = lp.PID
	}
	c.mu.Unlock()

	mem := treeMemory(roots)
	llama := mem[llamaKey]
	var local int64
	if own {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		if loaded, err := (localmodel.Ollama{URL: c.ownURL(), HTTP: c.http}).Running(ctx); err == nil {
			for _, l := range loaded {
				local += l.Size
			}
		}
		cancel()
	}
	c.readMachine()
	c.mu.Lock()
	for id, x := range c.sessions {
		x.memory = mem[id]
	}
	c.localMem, c.llamaMem = local, llama
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	c.publishHeader()
}

// treeMemory returns what the process tree under each root takes in
// memory (resident size), by key, from one run of ps.
func treeMemory(roots map[string]int) map[string]int64 {
	out := map[string]int64{}
	if len(roots) == 0 {
		return out
	}
	b, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,rss=").Output()
	if err != nil {
		return out
	}
	rss, kids := parsePS(b)
	for key, root := range roots {
		var sum int64
		for todo := []int{root}; len(todo) > 0; {
			pid := todo[len(todo)-1]
			todo = append(todo[:len(todo)-1], kids[pid]...)
			sum += rss[pid]
		}
		out[key] = sum
	}
	return out
}

// parsePS reads `ps -o pid=,ppid=,rss=`: each process's resident size in
// bytes (ps gives KiB), and each process's children.
func parsePS(b []byte) (rss map[int]int64, kids map[int][]int) {
	rss, kids = map[int]int64{}, map[int][]int{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		kb, err3 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || pid == ppid {
			continue
		}
		rss[pid] = kb << 10
		kids[ppid] = append(kids[ppid], pid)
	}
	return rss, kids
}

// memoryNow is the machine's memory and what Irie's work takes of it. The
// caller holds c.mu.
func (c *Core) memoryNow() ipc.Memory {
	m := c.machine
	out := ipc.Memory{
		Total: m.Total, Available: m.Available / availableStep * availableStep,
		Local: c.localMem + c.llamaMem, Budget: localmodel.Budget(c.cfg().Local.MemoryGiB, m),
		Low: m.Total > 0 && m.Available < localmodel.Reserve(m),
	}
	for _, x := range c.sessions {
		out.Agents += x.memory
	}
	return out
}

// headerNow is the header subscribers see. The caller holds c.mu.
func (c *Core) headerNow() ipc.Header {
	h := ipc.Header{Version: c.version, RedOnDone: c.cfg().UI.RedOnDone, CloseWhenDone: c.cfg().Spawn.CloseWhenDone, HelperStyle: c.cfg().UI.HelperStyle}
	if c.plan.FiveHour != nil || c.plan.SevenDay != nil {
		p := c.plan
		h.Plan = &p
	}
	if c.machine.Total > 0 {
		m := c.memoryNow()
		h.Memory = &m
	}
	h.Trod = c.trodHeader(c.awakeNow())
	h.VPN = c.vpnHeader(time.Now())
	return h
}

// publishHeader sends a header frame to every subscriber when the header
// changed since the last one.
func (c *Core) publishHeader() {
	c.mu.Lock()
	h := c.headerNow()
	changed := !reflect.DeepEqual(h, c.lastHeader)
	c.lastHeader = h
	subs := make([]*subscriber, 0, len(c.subs))
	for s := range c.subs {
		subs = append(subs, s)
	}
	c.mu.Unlock()
	if changed {
		for _, s := range subs {
			s.noteHeader()
		}
	}
}
