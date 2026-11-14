package vpn

// FortiClient, Fortinet's VPN, through its Linux CLI: `forticlient vpn
// status`, `forticlient vpn connect NAME` and `forticlient vpn disconnect`
// from FortiClient 7.4, and FortiClient 7.2's /opt/forticlient/fortivpn
// with the same words. Its status names the user and the tunnel's address,
// and Irie never keeps them. FortiClient's Mac app has no VPN command
// line.

import (
	"path/filepath"
	"strings"
)

func init() {
	kinds = append(kinds, Kind{
		Name:      "forticlient",
		Title:     "FortiClient",
		Get:       "your company's IT, or Fortinet's repository for Linux",
		Server:    "the VPN connection's name, as `forticlient vpn list` shows it",
		Needs:     true,
		Systems:   []string{"linux"},
		Elsewhere: "its Mac app has no VPN command line, so the command VPN follows it through commands of your own (vpn.provider = \"command\")",
		find: func(s Settings) (string, error) {
			return findCLI(s.CLI, []string{"forticlient"}, []string{"/usr/bin/forticlient", "/opt/forticlient/fortivpn"})
		},
		open: func(bin string, s Settings) (VPN, error) {
			f := FortiClient{run: newRunner(bin, filepath.Base(bin), s.Timeout), name: s.Server}
			// FortiClient 7.4's command takes vpn first; 7.2's fortivpn is
			// the VPN's own.
			if filepath.Base(bin) != "fortivpn" {
				f.pre = []string{"vpn"}
			}
			return f, nil
		},
	})
}

// FortiClient is FortiClient, through its Linux CLI.
type FortiClient struct {
	run  runner
	pre  []string // the words before each command: vpn, for forticlient
	name string   // the VPN connection
}

// Kind is FortiClient.
func (f FortiClient) Kind() Kind { k, _ := KindOf("forticlient"); return k }

func (f FortiClient) args(more ...string) []string {
	return append(append([]string{}, f.pre...), more...)
}

// ParseFortiClient reads `forticlient vpn status`: "Status: Connected",
// with the connection's name, user, address and counts under it, or
// "Status: Connecting...", "Status: Disconnected" or "Status: Not
// Running". Only the state and the connection's name are kept.
func ParseFortiClient(out string) (State, bool) {
	state, name, found := "", "", false
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "status":
			if !found {
				state, found = strings.ToLower(strings.TrimRight(strings.TrimSpace(v), ".")), true
			}
		case "vpn name":
			name = strings.TrimSpace(v)
		}
	}
	if !found {
		return State{}, false
	}
	switch state {
	case "connected":
		return State{State: Connected, Host: name}, true
	case "connecting", "re-connecting":
		return State{State: Connecting, Host: name}, true
	case "disconnecting":
		return State{State: Disconnecting}, true
	case "disconnected", "not running":
		return State{State: Disconnected}, true
	}
	return State{State: Error, Cause: "FortiClient says " + state}, true
}

// Status is the tunnel's state now.
func (f FortiClient) Status() (State, error) {
	out, code, err := f.run.exec(f.run.timeout, f.args("status")...)
	if err != nil {
		return State{}, err
	}
	s, ok := ParseFortiClient(out)
	if !ok {
		return State{}, f.run.failed(f.args("status"), out, code)
	}
	if (s.State == Connected || s.State == Connecting) && s.Host == "" {
		s.Host = f.name
	}
	return s, nil
}

// Connect connects the named VPN. One that wants a password FortiClient
// hasn't saved fails and says so.
func (f FortiClient) Connect() error {
	_, err := f.run.long(f.args("connect", f.name)...)
	return err
}

// Disconnect disconnects it.
func (f FortiClient) Disconnect() error {
	args := f.args("disconnect")
	if f.name != "" {
		args = append(args, f.name)
	}
	_, err := f.run.run(args...)
	return err
}
