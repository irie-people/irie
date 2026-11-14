package vpn

// Tailscale, through its CLI: `tailscale status --json` for the state,
// `tailscale up` and `tailscale down`, and an exit node for a place. Only
// the backend's state, the tailnet's name and the exit node's name are
// read: the JSON also lists this machine's and every peer's addresses.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/irie-people/irie/internal/brand"
)

func init() {
	kinds = append(kinds, Kind{
		Name:  "tailscale",
		Title: "Tailscale",
		Get:   "https://tailscale.com/download",
		find: func(s Settings) (string, error) {
			return findCLI(s.CLI, []string{"tailscale"}, []string{
				// The Mac app's own binary is the CLI too.
				"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
				"/usr/local/bin/tailscale", "/opt/homebrew/bin/tailscale", "/usr/bin/tailscale",
			})
		},
		open: func(bin string, s Settings) (VPN, error) {
			r := newRunner(bin, "tailscale", s.Timeout)
			// The Mac app's binary is its CLI only when it knows it is.
			r.env = []string{"TAILSCALE_BE_CLI=1"}
			return Tailscale{run: r}, nil
		},
	})
}

// Tailscale is Tailscale, through its CLI.
type Tailscale struct{ run runner }

// Kind is Tailscale.
func (t Tailscale) Kind() Kind { k, _ := KindOf("tailscale"); return k }

// tailscaleStatus is what Irie reads of `tailscale status --json`.
type tailscaleStatus struct {
	BackendState   string `json:"BackendState"`
	CurrentTailnet *struct {
		Name string `json:"Name"`
	} `json:"CurrentTailnet"`
	ExitNodeStatus *struct {
		ID string `json:"ID"`
	} `json:"ExitNodeStatus"`
	Peer map[string]struct {
		ID       string `json:"ID"`
		HostName string `json:"HostName"`
		DNSName  string `json:"DNSName"`
		ExitNode bool   `json:"ExitNode"`
	} `json:"Peer"`
}

// ParseTailscale reads `tailscale status --json`. While it is up, where it
// goes is the exit node, else the tailnet, unless the tailnet is named
// after a person's address.
func ParseTailscale(out []byte) (State, bool) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 || out[0] != '{' {
		return State{}, false
	}
	var st tailscaleStatus
	if json.Unmarshal(out, &st) != nil || st.BackendState == "" {
		return State{}, false
	}
	s := State{}
	switch st.BackendState {
	case "Running":
		s.State = Connected
	case "Starting":
		s.State = Connecting
	case "Stopped":
		s.State = Disconnected
	case "NeedsLogin":
		s.State, s.Cause = Disconnected, "Tailscale needs you to sign in"
	case "NeedsMachineAuth":
		s.State, s.Cause = Disconnected, "this device waits for the tailnet's admin to approve it"
	case "InUseOtherUser":
		s.State, s.Cause = Error, "another user of this computer has Tailscale"
	case "NoState":
		s.State, s.Cause = Disconnected, "tailscaled is starting"
	default:
		s.State, s.Cause = Error, "Tailscale is in a state "+brand.Name+" doesn't know: "+st.BackendState
	}
	if s.State == Connected || s.State == Connecting {
		s.Host = tailscaleWhere(st)
	}
	return s, true
}

func tailscaleWhere(st tailscaleStatus) string {
	if st.ExitNodeStatus != nil {
		for _, p := range st.Peer {
			if p.ExitNode || (st.ExitNodeStatus.ID != "" && p.ID == st.ExitNodeStatus.ID) {
				// Its MagicDNS name, which :vpn NAME takes, else the
				// machine's own name for itself.
				name, _, _ := strings.Cut(p.DNSName, ".")
				if name == "" {
					name = p.HostName
				}
				if name != "" {
					return "exit node " + name
				}
			}
		}
		return "an exit node"
	}
	if t := st.CurrentTailnet; t != nil && t.Name != "" && !strings.Contains(t.Name, "@") {
		return t.Name
	}
	return ""
}

// Status is the tunnel's state now. tailscale prints its JSON whatever the
// state, and says why when it can't, as when tailscaled isn't running.
func (t Tailscale) Status() (State, error) {
	out, code, err := t.run.exec(t.run.timeout, "status", "--json")
	if err != nil {
		return State{}, err
	}
	if s, ok := ParseTailscale([]byte(out)); ok {
		return s, nil
	}
	return State{}, t.run.failed([]string{"status", "--json"}, out, code)
}

// Connect brings Tailscale up as it was set up: `tailscale up` with no
// flags changes nothing else. Signed out, or waiting for its admin, up
// would wait for ever on a page no one sees, so the person is told
// instead.
func (t Tailscale) Connect() error {
	if out, _, err := t.run.exec(t.run.timeout, "status", "--json"); err == nil {
		var st tailscaleStatus
		if json.Unmarshal([]byte(out), &st) == nil {
			switch st.BackendState {
			case "NeedsLogin":
				return errors.New("Tailscale needs you to sign in first: open the Tailscale app, or run `tailscale login`")
			case "NeedsMachineAuth":
				return errors.New("Tailscale waits for the tailnet's admin to approve this device, in the admin console")
			}
		}
	}
	_, err := t.run.long("up")
	return err
}

// Disconnect takes Tailscale down; it stays signed in.
func (t Tailscale) Disconnect() error {
	_, err := t.run.run("down")
	return err
}

// tailscaleNode is an exit node by name or address.
var tailscaleNode = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]*$`)

// CheckPlace and SetPlace make Tailscale a Placer: its places are its exit
// nodes, by name or address, and none goes out directly again.
func (t Tailscale) CheckPlace(where []string) error {
	if len(where) != 1 || !tailscaleNode.MatchString(where[0]) {
		return fmt.Errorf("a place for Tailscale is an exit node, by its name or address, such as :vpn office-gw, or :vpn none to go out directly (`tailscale exit-node list` lists them)")
	}
	return nil
}

func (t Tailscale) SetPlace(where []string, _ bool) error {
	node := where[0]
	if node == "none" {
		node = ""
	}
	_, err := t.run.run("set", "--exit-node="+node)
	return err
}
