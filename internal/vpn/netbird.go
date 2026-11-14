package vpn

// NetBird, through its CLI: `netbird status --json` for the state, which
// it prints whatever the state, `netbird up` and `netbird down`. Only the
// daemon's state and the management server's link are read: the JSON
// also carries this machine's NetBird address, name and endpoints.

import (
	"bytes"
	"encoding/json"
	"errors"
)

func init() {
	kinds = append(kinds, Kind{
		Name:  "netbird",
		Title: "NetBird",
		Get:   "https://docs.netbird.io/get-started/install",
		find: func(s Settings) (string, error) {
			return findCLI(s.CLI, []string{"netbird"}, []string{"/usr/local/bin/netbird", "/opt/homebrew/bin/netbird", "/usr/bin/netbird"})
		},
		open: func(bin string, s Settings) (VPN, error) {
			return NetBird{run: newRunner(bin, "netbird", s.Timeout)}, nil
		},
	})
}

// NetBird is NetBird, through its CLI.
type NetBird struct{ run runner }

// Kind is NetBird.
func (n NetBird) Kind() Kind { k, _ := KindOf("netbird"); return k }

// netbirdStatus is what Irie reads of `netbird status --json`.
type netbirdStatus struct {
	DaemonStatus string `json:"daemonStatus"`
	Management   *struct {
		Connected bool `json:"connected"`
	} `json:"management"`
}

// ParseNetBird reads `netbird status --json`: its daemonStatus (Idle,
// Connecting, Connected, NeedsLogin, LoginFailed, SessionExpired), or,
// from a NetBird older than 0.67, which has none, whether it reaches its
// management server.
func ParseNetBird(out []byte) (State, bool) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 || out[0] != '{' {
		return State{}, false
	}
	var st netbirdStatus
	if json.Unmarshal(out, &st) != nil {
		return State{}, false
	}
	switch st.DaemonStatus {
	case "Connected":
		return State{State: Connected}, true
	case "Connecting":
		return State{State: Connecting}, true
	case "Idle":
		return State{State: Disconnected}, true
	case "NeedsLogin":
		return State{State: Disconnected, Cause: "NetBird needs you to sign in"}, true
	case "SessionExpired":
		return State{State: Disconnected, Cause: "NetBird's sign-in has expired"}, true
	case "LoginFailed":
		return State{State: Error, Cause: "NetBird's sign-in failed"}, true
	case "":
		if st.Management == nil {
			return State{}, false
		}
		if st.Management.Connected {
			return State{State: Connected}, true
		}
		return State{State: Disconnected}, true
	}
	return State{State: Error, Cause: "NetBird says " + st.DaemonStatus}, true
}

// Status is the tunnel's state now.
func (n NetBird) Status() (State, error) {
	out, code, err := n.run.exec(n.run.timeout, "status", "--json")
	if err != nil {
		return State{}, err
	}
	if s, ok := ParseNetBird([]byte(out)); ok {
		return s, nil
	}
	return State{}, n.run.failed([]string{"status", "--json"}, out, code)
}

// Connect brings NetBird up. Signed out, up would open a sign-in page and
// wait on it, so the person is told to sign in instead.
func (n NetBird) Connect() error {
	if s, err := n.Status(); err == nil && s.Cause != "" && s.State != Connected {
		return errors.New(s.Cause + ": run `netbird up` in a terminal, or sign in from NetBird's app")
	}
	_, err := n.run.long("up")
	return err
}

// Disconnect takes NetBird down.
func (n NetBird) Disconnect() error {
	_, err := n.run.run("down")
	return err
}
