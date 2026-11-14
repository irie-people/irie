package core

// A local model on a peer (DESIGN §6.5). A llama-server profile with
// `local = { runtime = "llama-server", peer = "Asahi" }` runs its agent
// here, in this machine's folders with its hooks, and its model on that
// peer's llama-server, which it reaches through an ssh tunnel: this
// machine holds none of the model, and the peer's memory and graphics do
// the work. The core keeps the tunnel in a window of its own, as it keeps
// the phone app's gateway, so a restarted core leaves it, and the agents
// on it, alone. When the peer's server does not run the profile's model,
// the core asks the peer to load it, as `irie ctl preflight` there would.

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/tmux"
)

const (
	// tunnelWindow names the window of a tunnel to a peer's llama-server.
	tunnelWindow = brand.Slug + "-tunnel"
	// tunnelRetry is the least time between two starts of one tunnel, so
	// a peer that is away is not dialled every second.
	tunnelRetry = 15 * time.Second
	// tunnelWait is how long a spawn waits for a new tunnel to answer.
	tunnelWait = 20 * time.Second
	// peerLoadTimeout bounds the peer's load of a model, over ssh.
	peerLoadTimeout = 5 * time.Minute
)

// tunnelPeers are the peers whose llama-server a profile's model runs on,
// in order.
func tunnelPeers(cfg *config.Config) []string {
	set := map[string]bool{}
	for _, p := range cfg.Profiles {
		if p.Local != nil && p.Local.Peer != "" {
			set[p.Local.Peer] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// tunnelPort is this machine's end of the tunnel to a peer's llama-server:
// the ports after local.llama_port, one for each such peer. The peer's end
// is its own llama-server, which listens on 127.0.0.1 at local.llama_port
// there; both machines take the same port, 11436 unless the config says
// otherwise.
func tunnelPort(cfg *config.Config, peer string) int {
	return cfg.Local.LlamaPort + 1 + max(slices.Index(tunnelPeers(cfg), peer), 0)
}

func tunnelURL(cfg *config.Config, peer string) string {
	return fmt.Sprintf("http://127.0.0.1:%d", tunnelPort(cfg, peer))
}

// peerSSH is the ssh name of a peer: peers.<name>.ssh, else the one it was
// paired over. The caller holds c.mu.
func (c *Core) peerSSH(p *peerState) string {
	if n := c.cfg().Peers[p.name].SSH; n != "" {
		return n
	}
	return p.conf.SSH
}

// tunnelCommand is what a tunnel's pane runs: ssh forwarding this
// machine's port to the peer's llama-server, under `irie runtime`, with no
// prompt to answer, and gone at once when the port is taken or the peer
// stops answering.
func (c *Core) tunnelCommand(ssh string, local int) []string {
	remote := c.cfg().Local.LlamaPort
	return []string{"--", c.bin, "runtime", "--", "ssh", "-N", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
		"-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
		"-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", local, remote), "--", ssh}
}

// keepTunnels keeps a tunnel to each peer whose llama-server a profile's
// model runs on, while that peer answers, and stops the ones no profile
// needs. A tunnel that died starts again, at most every tunnelRetry. The
// caller holds c.mu.
func (c *Core) keepTunnels() {
	cfg := c.cfg()
	want := map[string]bool{}
	for _, name := range tunnelPeers(cfg) {
		want[name] = true
	}
	var cmds [][]string
	have := map[string]tmux.Pane{}
	for _, p := range c.snap.ByRole(tmux.RoleTunnel) {
		if _, dup := have[p.Name]; dup || !want[p.Name] {
			cmds = append(cmds, []string{"kill-pane", "-t", p.ID})
			continue
		}
		have[p.Name] = p
	}
	now := time.Now()
	for _, name := range slices.Sorted(maps.Keys(want)) {
		p := c.peers[name]
		if p == nil || !p.up {
			continue
		}
		ssh := c.peerSSH(p)
		if ssh == "" {
			continue
		}
		local := tunnelPort(cfg, name)
		spec := fmt.Sprintf("%s %s %d %d", c.version, ssh, local, cfg.Local.LlamaPort)
		pane, ok := have[name]
		if ok && !pane.Dead && pane.Version == spec {
			continue
		}
		if now.Sub(c.tunnelAt[name]) < tunnelRetry {
			continue
		}
		if c.tunnelAt == nil {
			c.tunnelAt = map[string]time.Time{}
		}
		c.tunnelAt[name] = now
		run := c.tunnelCommand(ssh, local)
		if !ok {
			out, err := c.tmux.Run(append([]string{"new-window", "-d", "-t", tmux.AgentsSession + ":", "-n", tunnelWindow, "-P", "-F", "#{pane_id}"}, run...)...)
			if err != nil {
				c.log.Warn("tunnel: start", "peer", name, "err", err)
				continue
			}
			id := strings.TrimSpace(out)
			cmds = append(cmds, []string{"set-option", "-p", "-t", id, tmux.OptRole, tmux.RoleTunnel},
				[]string{"set-option", "-p", "-t", id, tmux.OptName, name},
				[]string{"set-option", "-p", "-t", id, tmux.OptVersion, spec})
			c.log.Info("tunnel: started", "peer", name, "ssh", ssh, "port", local, "pane", id)
			continue
		}
		cmds = append(cmds, append([]string{"respawn-pane", "-k", "-t", pane.ID}, run...),
			[]string{"set-option", "-p", "-t", pane.ID, tmux.OptVersion, spec})
		c.log.Info("tunnel: restarted", "peer", name, "ssh", ssh, "port", local, "pane", pane.ID, "dead", pane.Dead)
	}
	if len(cmds) > 0 {
		if _, err := c.tmux.RunList(cmds); err != nil {
			c.log.Warn("tunnel", "err", err)
		}
	}
}

// modelOf is the name llama-server's /props gives its model by: its file's
// name without .gguf.
func modelOf(path string) string { return strings.TrimSuffix(filepath.Base(path), ".gguf") }

// peerLlamaReady says whether a profile whose model runs on a peer can
// take a task now, for the router: the peer answers and its tunnel is
// there. It costs this machine no memory. The caller holds no lock.
func (c *Core) peerLlamaReady(name string, p config.Profile) (note string, ok bool) {
	peer := p.Local.Peer
	c.mu.Lock()
	defer c.mu.Unlock()
	ps := c.peers[peer]
	switch {
	case ps == nil:
		return fmt.Sprintf("%s is not a peer of this machine's", peer), false
	case !ps.up:
		return peer + " isn't answering just now", false
	case c.peerSSH(ps) == "":
		return fmt.Sprintf("no ssh name for %s: set peers.%s.ssh", peer, peer), false
	}
	for _, pane := range c.snap.ByRole(tmux.RoleTunnel) {
		if pane.Name == peer && !pane.Dead {
			return "on " + peer + "'s llama-server", true
		}
	}
	return "the tunnel to " + peer + " is down", false
}

// preparePeerLlama readies a spawn on a profile whose model runs on a
// peer: the tunnel answers, the peer's llama-server runs the profile's
// model, loaded there for it if need be, and the agent points at the
// tunnel with the context a slot there has.
func (c *Core) preparePeerLlama(name string, p *config.Profile, force bool) ([]string, error) {
	cfg := c.cfg()
	peer := p.Local.Peer
	c.mu.Lock()
	ps := c.peers[peer]
	ssh, up := "", false
	if ps != nil {
		ssh, up = c.peerSSH(ps), ps.up
	}
	c.mu.Unlock()
	switch {
	case ps == nil:
		return nil, ipc.Errorf(ipc.CodeNotFound, "+%s runs on %s's llama-server, and %s is not a peer of this machine's: pair with "+brand.Slug+" peer add", name, peer, peer)
	case !up:
		return nil, ipc.Errorf(ipc.CodePeerDown, "+%s runs on %s's llama-server, and %s isn't answering just now", name, peer, peer)
	case ssh == "":
		return nil, ipc.Errorf(ipc.CodeBadArgs, "+%s runs on %s's llama-server, which it reaches over ssh: set peers.%s.ssh", name, peer, peer)
	}
	c.mu.Lock()
	delete(c.tunnelAt, peer) // a spawn waits on it: start it now if it is down
	if err := c.refresh(); err != nil {
		c.log.Warn("tunnel", "err", err)
	}
	c.keepTunnels()
	c.mu.Unlock()
	l := localmodel.Llama{URL: tunnelURL(cfg, peer), HTTP: c.http}
	props, err := c.waitTunnel(l)
	if err == nil && p.Model != "" && modelOf(props.Model) != p.Model {
		err = fmt.Errorf("its server runs %s", modelOf(props.Model))
	}
	if err != nil {
		// Its server runs another model, or none: ask the peer to load the
		// profile's, as a spawn of its own would.
		if out, lerr := c.peerLoad(ssh, name, force); lerr != nil {
			return nil, ipc.Errorf(ipc.CodeBadArgs, "+%s on %s: %v (%v)", name, peer, lerr, strings.TrimSpace(out))
		}
		if props, err = c.waitTunnel(l); err != nil {
			return nil, ipc.Errorf(ipc.CodeBadArgs, "+%s on %s: its llama-server through the tunnel: %v", name, peer, err)
		}
	}
	model := modelOf(props.Model)
	var warnings []string
	if props.Context < p.Local.MinContext {
		msg := fmt.Sprintf("%s runs %s with a %s context and +%s needs %s; raise local.context there", peer, model, localmodel.Tokens(props.Context), name, localmodel.Tokens(p.Local.MinContext))
		if p.Local.Enforce && !force {
			return nil, ipc.Errorf(ipc.CodeBadArgs, "+%s: %s", name, msg)
		}
		warnings = append(warnings, msg)
	}
	p.Model = model
	env := claude.LocalEnv(model, l.URL, props.Context)
	maps.Copy(env, p.Env)
	p.Env = env
	return warnings, nil
}

// waitTunnel waits for the peer's llama-server to answer through its
// tunnel with its model loaded, and says what it runs.
func (c *Core) waitTunnel(l localmodel.Llama) (localmodel.Props, error) {
	var err error
	for deadline := time.Now().Add(tunnelWait); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var ready bool
		if ready, err = l.Ready(ctx); err == nil && ready {
			var props localmodel.Props
			props, err = l.Props(ctx)
			cancel()
			return props, err
		}
		cancel()
		if err == nil {
			err = fmt.Errorf("it is loading its model")
		}
	}
	return localmodel.Props{}, err
}

// peerLoad has a peer load a profile's model into its llama-server, as
// `irie ctl preflight +PROFILE` there does, over ssh. The peer runs its
// own Irie from where Irie installs itself, with its static tmux on the
// path.
var peerLoad = func(c *Core, ssh, profile string, force bool) (string, error) {
	if !shellWord.MatchString(profile) {
		return "", fmt.Errorf("+%s: a profile's name with odd characters can't go to a peer over ssh", profile)
	}
	args := "ctl preflight"
	if force {
		args += " --force"
	}
	remote := `PATH="$HOME/.local/bin:$PATH" "$HOME/.local/share/` + brand.Slug + `/bin/` + brand.Slug + `" ` + args + " +" + profile
	ctx, cancel := context.WithTimeout(context.Background(), peerLoadTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "--", ssh, remote)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	c.log.Info("tunnel: peer load", "ssh", ssh, "profile", profile, "err", err)
	return string(out), err
}

// shellWord is a word a shell takes as it is.
var shellWord = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

func (c *Core) peerLoad(ssh, profile string, force bool) (string, error) {
	return peerLoad(c, ssh, profile, force)
}
