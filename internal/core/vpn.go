package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/tmux"
	"github.com/irie-people/irie/internal/vpn"
)

// vpnState is Mullvad VPN as the core follows it, while vpn.on
// (docs/proposals/extensions.md §4). The caller holds c.mu.
type vpnState struct {
	cancel   context.CancelFunc // stops following it
	bin      string             // the mullvad CLI in use
	state    vpn.State
	read     bool // a state has come
	lockdown bool
	expires  time.Time
	problem  string // why Irie can't read it
}

// vpnExpiryEvery is how often the account's time is read again.
const vpnExpiryEvery = time.Hour

// keepVPN starts following the VPN when vpn.on turns on, and stops when it
// turns off. The caller holds c.mu.
func (c *Core) keepVPN() {
	on := c.cfg().VPN.On
	switch {
	case on && c.vpn.cancel == nil:
		ctx, cancel := context.WithCancel(context.Background())
		c.vpn.cancel = cancel
		go c.followVPN(ctx, c.cfg().VPN.Mullvad)
	case !on && c.vpn.cancel != nil:
		c.vpn.cancel()
		c.vpn = vpnState{}
		go c.publishHeader()
	}
}

// setVPN changes what the core knows of the VPN, and tells the clients.
func (c *Core) setVPN(f func(*vpnState)) {
	c.mu.Lock()
	if c.vpn.cancel != nil {
		f(&c.vpn)
	}
	c.mu.Unlock()
	c.publishHeader()
}

// followVPN keeps up with the daemon through `mullvad status listen`, which
// it starts again, after a pause, whenever it ends, until ctx ends.
func (c *Core) followVPN(ctx context.Context, configured string) {
	pause := time.Second
	for ctx.Err() == nil {
		bin, err := vpn.Find(configured)
		if err != nil {
			c.setVPN(func(v *vpnState) { v.problem = err.Error() })
			if !sleepCtx(ctx, 30*time.Second) {
				return
			}
			continue
		}
		cli := vpn.CLI{Bin: bin}
		c.vpnExtras(cli)
		stopExpiry := c.everyVPN(ctx, cli)
		started := time.Now()
		err = cli.Listen(ctx, func(s vpn.State) {
			c.setVPN(func(v *vpnState) { v.bin, v.state, v.read, v.problem = bin, s, true, "" })
		}, func(on bool) {
			c.setVPN(func(v *vpnState) { v.lockdown = on })
		})
		stopExpiry()
		if ctx.Err() != nil {
			return
		}
		why := "mullvad status listen ended"
		if err != nil {
			why += ": " + err.Error()
		}
		c.setVPN(func(v *vpnState) { v.problem = why })
		c.log.Warn("vpn", "err", why)
		if time.Since(started) > time.Minute {
			pause = time.Second
		}
		if !sleepCtx(ctx, pause) {
			return
		}
		pause = min(pause*2, 30*time.Second)
	}
}

// vpnExtras reads what the listener doesn't say at once: lockdown mode and
// the account's time.
func (c *Core) vpnExtras(cli vpn.CLI) {
	lockdown, lerr := cli.Lockdown()
	expires, eerr := cli.Expiry()
	c.setVPN(func(v *vpnState) {
		v.bin = cli.Bin
		if lerr == nil {
			v.lockdown = lockdown
		}
		if eerr == nil {
			v.expires = expires
		}
	})
}

// everyVPN reads the account's time every hour while the listener runs.
func (c *Core) everyVPN(ctx context.Context, cli vpn.CLI) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		t := time.NewTicker(vpnExpiryEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if e, err := cli.Expiry(); err == nil {
					c.setVPN(func(v *vpnState) { v.expires = e })
				}
			}
		}
	}()
	return cancel
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// vpnHeader is the header's mark, while vpn.on. The caller holds c.mu.
func (c *Core) vpnHeader(now time.Time) *ipc.VPN {
	if !c.cfg().VPN.On {
		return nil
	}
	v := c.vpn
	h := &ipc.VPN{Lockdown: v.lockdown, Problem: v.problem}
	if v.read {
		h.State, h.City, h.Country, h.Host, h.Blocked = v.state.State, v.state.City, v.state.Country, v.state.Host, v.state.Blocked
	}
	if !v.expires.IsZero() {
		e := v.expires
		h.Expires = &e
	}
	h.Text = vpnWords(v, now)
	return h
}

// vpnWords is the VPN in one line: its state, lockdown and the time left.
func vpnWords(v vpnState, now time.Time) string {
	if !v.read {
		if v.problem != "" {
			return "VPN unknown: " + v.problem
		}
		return "VPN: reading…"
	}
	parts := []string{v.state.Words()}
	if v.lockdown && v.state.State != vpn.Disconnected {
		parts = append(parts, "lockdown on")
	}
	if !v.expires.IsZero() {
		parts = append(parts, timeLeft(v.expires, now))
	}
	return strings.Join(parts, ", ")
}

// timeLeft is the account's time in words.
func timeLeft(expires, now time.Time) string {
	d := expires.Sub(now)
	switch {
	case d <= 0:
		return "the account's time has run out"
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours left", int(d.Hours()))
	}
	return fmt.Sprintf("%d days left", int(d.Hours()/24))
}

// vpnOp answers the vpn op: the state, or a change. A change that comes
// from inside an agent is refused: the VPN is the person's, and an agent at
// work would pull the network from under the others.
func (c *Core) vpnOp(peer int, a ipc.VPNArgs) (ipc.VPNResult, error) {
	if !c.cfg().VPN.On {
		return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "Mullvad VPN control is off: [vpn] on = true in config.toml turns it on.")
	}
	c.mu.Lock()
	v := c.vpn
	c.mu.Unlock()
	now := time.Now()
	if a.Do == "" || a.Do == ipc.VPNTime {
		if a.Do == ipc.VPNTime && v.expires.IsZero() {
			return ipc.VPNResult{}, ipc.Errorf(ipc.CodeNotFound, "Irie doesn't know the account's time yet: is this Mac logged in to a Mullvad account?")
		}
		c.mu.Lock()
		h := c.vpnHeader(now)
		c.mu.Unlock()
		text := h.Text
		if a.Do == ipc.VPNTime {
			text = "Mullvad: " + timeLeft(v.expires, now) + ", until " + v.expires.Local().Format("2 Jan 2006") + "."
		}
		return ipc.VPNResult{Text: text, VPN: h}, nil
	}
	if c.fromAgent(peer) {
		return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "An agent can't change the VPN: it is yours to change, from any task bar.")
	}
	if v.bin == "" {
		bin, err := vpn.Find(c.cfg().VPN.Mullvad)
		if err != nil {
			return ipc.VPNResult{}, ipc.Errorf(ipc.CodeNotFound, "%v", err)
		}
		v.bin = bin
	}
	cli := vpn.CLI{Bin: v.bin}
	var err error
	did := ""
	switch a.Do {
	case ipc.VPNOn:
		err, did = cli.Connect(), "Connected"
	case ipc.VPNOff:
		err, did = cli.Disconnect(), "Disconnected"
	case ipc.VPNLockdown:
		if len(a.Args) != 1 || a.Args[0] != "on" && a.Args[0] != "off" {
			return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, ":vpn lockdown takes on or off.")
		}
		on := a.Args[0] == "on"
		if err = cli.SetLockdown(on); err == nil {
			c.setVPN(func(v *vpnState) { v.lockdown = on })
			did = "Lockdown mode is " + a.Args[0]
		}
	case ipc.VPNPlace:
		if err = vpn.CheckLocation(a.Args); err != nil {
			return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
		}
		if err = cli.SetLocation(a.Args); err == nil {
			did = "The VPN goes through " + strings.Join(a.Args, " ")
			if v.state.State == vpn.Connected || v.state.State == vpn.Connecting {
				err = cli.Reconnect()
			}
		}
	default:
		return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "vpn does not %q; it does on, off, lockdown, place and time.", a.Do)
	}
	if err != nil {
		return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	// The listener says the new state soon; the answer reads it now.
	if s, err := cli.Status(); err == nil {
		c.setVPN(func(v *vpnState) { v.state, v.read = s, true })
	}
	c.mu.Lock()
	h := c.vpnHeader(time.Now())
	c.mu.Unlock()
	return ipc.VPNResult{Text: did + ". " + h.Text + ".", VPN: h}, nil
}

// fromAgent reports whether the process pid runs inside one of Irie's
// agents: whether an agent pane's first process, its host, is among its
// parents. It stops an agent that goes through Irie, not one that runs
// mullvad itself, which lockdown mode holds against. An unknown pid is not
// an agent's.
func (c *Core) fromAgent(pid int) bool {
	if pid <= 0 {
		return false
	}
	c.mu.Lock()
	hosts := map[int]bool{}
	for _, p := range c.snap.ByRole(tmux.RoleAgent) {
		if p.PID > 0 {
			hosts[p.PID] = true
		}
	}
	c.mu.Unlock()
	t, err := proc.Read()
	if err != nil {
		c.log.Warn("vpn", "err", err)
		return false
	}
	for range 64 {
		if hosts[pid] {
			return true
		}
		p, ok := t.Get(pid)
		if !ok || p.PPID <= 1 || p.PPID == pid {
			return false
		}
		pid = p.PPID
	}
	return false
}
