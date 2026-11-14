//go:build !novpn

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/vpn"
)

const vpnBuilt = true

// vpnState is the VPN as the core follows it, while vpn.on (DESIGN
// §4.18): whichever VPN vpn.provider names. The caller holds c.mu.
type vpnState struct {
	cancel   context.CancelFunc // stops following it
	gen      int                // which following this is, so a stopped one changes nothing
	settings vpn.Settings       // what it follows the VPN by
	vpn      vpn.VPN            // the VPN, once its command is found
	state    vpn.State
	read     bool // a state has come
	lockdown bool
	expires  time.Time
	problem  string // why Irie can't read it
}

// vpnExpiryEvery is how often an account's time is read again.
const vpnExpiryEvery = time.Hour

// vpnPollEvery is how often the core asks a VPN that doesn't say its own
// changes, which is every VPN but Mullvad.
var vpnPollEvery = 10 * time.Second

// keepVPN starts following the VPN when vpn.on turns on, follows it again
// when its settings change, as for another provider, and stops when it
// turns off. The caller holds c.mu.
func (c *Core) keepVPN() {
	cfg := *c.cfg()
	on := cfg.VPNOn()
	s := plugins.VPNSettings(cfg)
	if c.vpn.cancel != nil && (!on || !reflect.DeepEqual(s, c.vpn.settings)) {
		c.vpn.cancel()
		c.vpn = vpnState{gen: c.vpn.gen}
		go c.publishHeader()
	}
	if on && c.vpn.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		c.vpn = vpnState{cancel: cancel, gen: c.vpn.gen + 1, settings: s}
		go c.followVPN(ctx, c.vpn.gen, s)
	}
}

// setVPN changes what the core knows of the VPN, while gen is the
// following in hand, and tells the clients.
func (c *Core) setVPN(gen int, f func(*vpnState)) {
	c.mu.Lock()
	if c.vpn.cancel != nil && c.vpn.gen == gen {
		f(&c.vpn)
	}
	c.mu.Unlock()
	c.publishHeader()
}

// followVPN keeps up with the VPN until ctx ends: through its listener,
// which it starts again after a pause whenever it ends, or by asking it
// every vpnPollEvery.
func (c *Core) followVPN(ctx context.Context, gen int, s vpn.Settings) {
	pause := time.Second
	for ctx.Err() == nil {
		v, err := vpn.Open(s)
		if err != nil {
			c.setVPN(gen, func(st *vpnState) { st.problem = err.Error() })
			if !sleepCtx(ctx, 30*time.Second) {
				return
			}
			continue
		}
		c.setVPN(gen, func(st *vpnState) { st.vpn = v })
		c.vpnExtras(gen, v)
		stopExtras := c.everyVPN(ctx, gen, v)
		l, listens := v.(vpn.Listener)
		if !listens {
			c.pollVPN(ctx, gen, v)
			stopExtras()
			return
		}
		started := time.Now()
		err = l.Listen(ctx, func(st vpn.State) {
			c.setVPN(gen, func(x *vpnState) { x.state, x.read, x.problem = st, true, "" })
		}, func(on bool) {
			c.setVPN(gen, func(x *vpnState) { x.lockdown = on })
		})
		stopExtras()
		if ctx.Err() != nil {
			return
		}
		why := v.Kind().Title + "'s listener ended"
		if err != nil {
			why += ": " + err.Error()
		}
		c.setVPN(gen, func(x *vpnState) { x.problem = why })
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

// pollVPN asks the VPN its state now and every vpnPollEvery until ctx
// ends. A VPN that can't say is unknown, not as it was.
func (c *Core) pollVPN(ctx context.Context, gen int, v vpn.VPN) {
	for {
		st, err := v.Status()
		if ctx.Err() != nil {
			return
		}
		c.setVPN(gen, func(x *vpnState) {
			if err != nil {
				x.read, x.problem = false, err.Error()
				return
			}
			x.state, x.read, x.problem = st, true, ""
		})
		if !sleepCtx(ctx, vpnPollEvery) {
			return
		}
	}
}

// vpnExtras reads what the state doesn't say, for the VPNs that have it:
// lockdown mode and the account's time.
func (c *Core) vpnExtras(gen int, v vpn.VPN) {
	var lockdown, hasLockdown bool
	var expires time.Time
	if l, ok := v.(vpn.Lockdowner); ok {
		on, err := l.Lockdown()
		lockdown, hasLockdown = on, err == nil
	}
	if t, ok := v.(vpn.Timer); ok {
		if e, err := t.Expiry(); err == nil {
			expires = e
		}
	}
	c.setVPN(gen, func(x *vpnState) {
		if hasLockdown {
			x.lockdown = lockdown
		}
		if !expires.IsZero() {
			x.expires = expires
		}
	})
}

// everyVPN reads the account's time every hour, for a VPN that has one,
// until the returned func is called.
func (c *Core) everyVPN(ctx context.Context, gen int, v vpn.VPN) func() {
	t, ok := v.(vpn.Timer)
	if !ok {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		tick := time.NewTicker(vpnExpiryEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if e, err := t.Expiry(); err == nil {
					c.setVPN(gen, func(x *vpnState) { x.expires = e })
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
	if !c.cfg().VPNOn() {
		return nil
	}
	v := c.vpn
	h := &ipc.VPN{Lockdown: v.lockdown, Problem: v.problem}
	provider := v.settings.Provider
	if provider == "" {
		provider = c.cfg().VPN.Provider
	}
	if k, ok := vpn.KindOf(provider); ok {
		h.Kind, h.Title = k.Name, k.Title
	}
	if v.vpn != nil {
		h.Can = vpn.Can(v.vpn)
	}
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
	if a.Do == ipc.VPNUse {
		if c.fromAgent(peer) {
			return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "An agent can't change the VPN: it is yours to change, from any task bar.")
		}
		return c.vpnUse(a.Args)
	}
	if why := plugins.Why(*c.cfg(), plugins.VPN); why != "" {
		return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", why)
	}
	c.mu.Lock()
	v := c.vpn
	c.mu.Unlock()
	if v.settings.Provider == "" {
		v.settings = plugins.VPNSettings(*c.cfg())
	}
	title := "The VPN"
	if k, ok := vpn.KindOf(v.settings.Provider); ok {
		title = k.Title
	}
	now := time.Now()
	if a.Do == "" || a.Do == ipc.VPNTime {
		if a.Do == ipc.VPNTime {
			if v.vpn == nil {
				if opened, err := vpn.Open(v.settings); err == nil {
					v.vpn = opened
				}
			}
			if _, ok := v.vpn.(vpn.Timer); v.vpn != nil && !ok {
				return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s has no account time to tell: only Mullvad VPN's runs out.", title)
			}
			if v.expires.IsZero() {
				return ipc.VPNResult{}, ipc.Errorf(ipc.CodeNotFound, brand.Name+" doesn't know the account's time yet: is this Mac logged in to a Mullvad account?")
			}
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
	if v.vpn == nil {
		opened, err := vpn.Open(v.settings)
		if err != nil {
			return ipc.VPNResult{}, ipc.Errorf(ipc.CodeNotFound, "%v", err)
		}
		v.vpn = opened
	}
	var err error
	did := ""
	switch a.Do {
	case ipc.VPNOn:
		err = v.vpn.Connect()
	case ipc.VPNOff:
		err = v.vpn.Disconnect()
	case ipc.VPNLockdown:
		l, ok := v.vpn.(vpn.Lockdowner)
		if !ok {
			return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s has no lockdown mode: :vpn lockdown is Mullvad VPN's.", title)
		}
		if len(a.Args) != 1 || a.Args[0] != "on" && a.Args[0] != "off" {
			return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, ":vpn lockdown takes on or off.")
		}
		on := a.Args[0] == "on"
		if err = l.SetLockdown(on); err == nil {
			c.setVPN(v.gen, func(x *vpnState) { x.lockdown = on })
			did = "Lockdown mode is " + a.Args[0]
		}
	case ipc.VPNPlace:
		p, ok := v.vpn.(vpn.Placer)
		if !ok {
			return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s has no places to choose from here: :vpn on, :vpn off, :vpn use NAME and :vpn are its words.", title)
		}
		if err = p.CheckPlace(a.Args); err != nil {
			return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
		}
		up := v.state.State == vpn.Connected || v.state.State == vpn.Connecting
		if err = p.SetPlace(a.Args, up); err == nil {
			did = "The VPN goes through " + strings.Join(a.Args, " ")
		}
	default:
		return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "vpn does not %q; it does on, off, use, place, lockdown and time.", a.Do)
	}
	if err != nil {
		return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	// A listener says the new state soon, and polling within seconds; the
	// answer reads it now.
	st, serr := v.vpn.Status()
	if serr == nil {
		c.setVPN(v.gen, func(x *vpnState) { x.state, x.read, x.problem = st, true, "" })
	}
	if did == "" {
		did = vpnDid(title, a.Do == ipc.VPNOn, st.State, serr == nil)
	}
	c.mu.Lock()
	h := c.vpnHeader(time.Now())
	c.mu.Unlock()
	if h == nil {
		return ipc.VPNResult{Text: did + "."}, nil
	}
	return ipc.VPNResult{Text: did + ". " + h.Text + ".", VPN: h}, nil
}

// vpnDid says what came of :vpn on or :vpn off: done, under way, or asked
// for, since some VPNs return before their tunnel has changed.
func vpnDid(title string, on bool, state string, read bool) string {
	switch {
	case on && read && state == vpn.Connected:
		return "Connected"
	case on && read && state == vpn.Connecting:
		return "Connecting"
	case on:
		return "Asked " + title + " to connect"
	case read && state == vpn.Disconnected:
		return "Disconnected"
	case read && state == vpn.Disconnecting:
		return "Disconnecting"
	}
	return "Asked " + title + " to disconnect"
}

// vpnUse is :vpn use NAME: the VPN that the core follows and the words
// drive becomes NAME, one this copy offers, and VPN control comes on.
func (c *Core) vpnUse(args []string) (ipc.VPNResult, error) {
	offered := config.VPNOffered()
	if len(args) != 1 {
		return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, ":vpn use takes one VPN: %s.", strings.Join(offered, ", "))
	}
	k, ok := vpn.KindOf(args[0])
	if !ok || !slices.Contains(offered, k.Name) {
		return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s doesn't drive %q: it drives %s.", brand.Name, args[0], strings.Join(offered, ", "))
	}
	if !k.Runs() {
		return ipc.VPNResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s.", k.NotHere())
	}
	sets := []ipc.ConfigSet{{Key: "vpn.provider", Value: json.RawMessage(fmt.Sprintf("%q", k.Name))}, {Key: "vpn.on", Value: json.RawMessage("true")}, {Key: "plugins.vpn", Value: json.RawMessage("true")}}
	if err := c.configure(c.paths.ConfigFile(), sets); err != nil {
		return ipc.VPNResult{}, err
	}
	c.mu.Lock()
	c.keepVPN()
	c.mu.Unlock()
	text := brand.Name + " follows " + k.Title + " now"
	if _, why := plugins.FindVPN(*c.cfg()); why != "" {
		text += ", which needs setting up: " + strings.TrimSuffix(why, ".")
	}
	return ipc.VPNResult{Text: text + "."}, nil
}
