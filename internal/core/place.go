package core

// The balancer (docs/proposals/multi-host.md §4.3): which machine a task
// typed here starts on when its words name none. With place.mode = "local"
// it is always this one. With "auto" a task goes to a peer only while this
// machine is busy, only to a peer that runs it as this one would (the same
// Claude Code hooks, with every program they start; Claude Code signed in;
// the profile; a repo listed for it), and only when that peer has clearly
// more room. It is a few comparisons of what the machines advertise, made
// once at the spawn, the same way every time, and it says why it chose.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/sysload"
)

const (
	// placeMarginCPUs and placeMarginGiB are how much more room a peer
	// must have than this machine to take a task from it.
	placeMarginCPUs = 2
	placeMarginGiB  = 2
	// placeMinPTYs are the ptys a machine keeps free: a pane takes one, and
	// running out of them is what stops a spawn.
	placeMinPTYs = 64
	// placeHold is how long a task placed on a peer counts against it, a
	// CPU and a GiB, before its load shows it: so a burst of tasks does not
	// all go to one machine.
	placeHold = 2 * time.Minute
	// placeMinDisk is the free disk a peer needs to take a task in a repo,
	// for its worktree.
	placeMinDisk = 5 << 30
)

// room is what a machine has free: CPUs that the work waiting to run
// leaves, memory, ptys (-1 when unknown), and the kernel's memory pressure.
type room struct {
	cpus, gib float64
	ptys      int
	pressure  int
	load      *ipc.Load
}

func roomOf(l *ipc.Load, m *ipc.Memory) (room, bool) {
	if l == nil || l.CPUs == 0 || m == nil || m.Total == 0 {
		return room{}, false
	}
	r := room{cpus: float64(l.CPUs) - l.RunQueue, gib: float64(m.Available) / (1 << 30), ptys: -1, pressure: l.Pressure, load: l}
	if l.PTYMax > 0 {
		r.ptys = l.PTYMax - l.PTYs
	}
	return r, true
}

// busyWhy says why a machine with room r is busy, or "" when it is not.
func busyWhy(r room, pl config.Place) string {
	switch {
	case r.cpus < float64(pl.HeadroomCPUs):
		return fmt.Sprintf("run queue %g on %d CPUs", r.load.RunQueue, r.load.CPUs)
	case r.pressure >= sysload.PressureWarn:
		return "the kernel warns of memory pressure"
	case r.gib < float64(pl.HeadroomGiB):
		return fmt.Sprintf("%.1f GiB of memory free", r.gib)
	case r.ptys >= 0 && r.ptys < placeMinPTYs:
		return fmt.Sprintf("%d ptys free", r.ptys)
	}
	return ""
}

// hooksDiffer says why a task would run on p without a hook that it runs
// with here, or "" when p runs every hook this machine runs. The caller
// holds c.mu.
func (c *Core) hooksDiffer(p *peerState) string {
	if p.header == nil || p.header.Machine == nil {
		return p.name + " runs an Irie too old to say which Claude Code hooks it runs; install this Irie there"
	}
	m := p.header.Machine
	if len(m.HooksMissing) > 0 {
		return fmt.Sprintf("%s lacks %s, which its Claude Code hooks start", p.name, strings.Join(m.HooksMissing, ", "))
	}
	if c.mach == nil {
		return "this machine's own hooks are not read yet; try again in a few seconds"
	}
	for _, h := range c.mach.Hooks {
		if !slices.Contains(m.Hooks, h) {
			return fmt.Sprintf("%s doesn't run the Claude Code hooks %s does (~/.claude/settings.json)", p.name, c.machineName())
		}
	}
	return ""
}

// recent is how many tasks were placed on p in the last placeHold. The
// caller holds c.mu.
func (p *peerState) recent(now time.Time) int {
	p.placed = slices.DeleteFunc(p.placed, func(t time.Time) bool { return now.Sub(t) > placeHold })
	return len(p.placed)
}

// placeAuto picks the peer a spawn typed here goes to, or none to keep it
// here, and says why. The caller holds c.mu.
func (c *Core) placeAuto(a ipc.SpawnArgs, now time.Time) (*peerState, string) {
	cfg := c.cfg()
	if cfg.Place.Mode != config.PlaceAuto || len(c.peers) == 0 || a.Delegate || a.Parent != "" || a.InPlace || a.Continues != "" {
		return nil, ""
	}
	mem := c.memoryNow()
	here, ok := roomOf(c.loadNow(), &mem)
	if !ok {
		return nil, ""
	}
	why := busyWhy(here, cfg.Place)
	if why == "" {
		return nil, ""
	}
	var best *peerState
	var bestRoom room
	for _, name := range c.peerNames() {
		p := c.peers[name]
		if !p.up || p.header == nil || p.header.Machine == nil || c.hooksDiffer(p) != "" {
			continue
		}
		m := p.header.Machine
		switch {
		case m.Claude != "ok":
			continue
		case a.Profile != "" && !slices.Contains(m.Profiles, a.Profile):
			continue
		case a.Repo != "" && !slices.Contains(cfg.Peers[name].Repos, a.Repo):
			continue
		case a.Repo != "" && m.Disk > 0 && m.Disk < placeMinDisk:
			continue
		}
		r, ok := roomOf(p.header.Load, p.header.Memory)
		if !ok {
			continue
		}
		n := float64(p.recent(now))
		r.cpus, r.gib = r.cpus-n, r.gib-n
		if busyWhy(r, cfg.Place) != "" || r.cpus < here.cpus+placeMarginCPUs || r.gib < here.gib+placeMarginGiB {
			continue
		}
		if best == nil || r.cpus > bestRoom.cpus {
			best, bestRoom = p, r
		}
	}
	if best == nil {
		return nil, ""
	}
	return best, fmt.Sprintf("%s was busy (%s); %s had %.1f CPUs and %.1f GiB free", c.machineName(), why, best.name, bestRoom.cpus, bestRoom.gib)
}

// refusedThere reports whether err, from a peer a task was placed on,
// means the task did not start there, so it may start here instead: the
// peer's core refused it. A call that failed on the way may have started
// it, and stays an error.
func refusedThere(err error) bool {
	var e *ipc.Error
	return errors.As(err, &e) && e.Code != ipc.CodePeerDown
}

// placeTask starts a task typed here on the peer the balancer picks, and
// says whether it did. A task that reads as more for the jot before it
// stays with that jot's agent, and one the peer refuses starts here.
func (c *Core) placeTask(a ipc.InputArgs, sa ipc.SpawnArgs) (ipc.InputResult, bool, error) {
	now := time.Now()
	c.mu.Lock()
	if _, _, follows := c.addable(sa, now); follows || sa.Apart != "" {
		c.mu.Unlock()
		return ipc.InputResult{}, false, nil
	}
	p, why := c.placeAuto(sa, now)
	if p != nil {
		p.placed = append(p.placed, now)
	}
	c.mu.Unlock()
	if p == nil {
		return ipc.InputResult{}, false, nil
	}
	c.log.Info("place", "peer", p.name, "why", why)
	r, err := c.inputOn(p, a)
	if refusedThere(err) {
		c.log.Info("place: refused there, so it starts here", "peer", p.name, "err", err)
		return ipc.InputResult{}, false, nil
	}
	if err == nil && r.Text != "" {
		r.Text = strings.TrimSuffix(r.Text, ".") + ": " + why + "."
	}
	return r, true, err
}
