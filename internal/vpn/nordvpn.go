package vpn

// NordVPN, through its Linux CLI, nordvpn: `nordvpn status` for the state,
// `nordvpn connect [PLACE]` and `nordvpn disconnect`. Its status also
// names the server's address, which Irie never reads. NordVPN's Mac app
// has no command line.

import (
	"fmt"
	"regexp"
	"strings"
)

func init() {
	kinds = append(kinds, Kind{
		Name:      "nordvpn",
		Title:     "NordVPN",
		Get:       "https://nordvpn.com/download/linux/",
		Systems:   []string{"linux"},
		Elsewhere: "its Mac app has no command line",
		find: func(s Settings) (string, error) {
			return findCLI(s.CLI, []string{"nordvpn"}, []string{"/usr/bin/nordvpn"})
		},
		open: func(bin string, s Settings) (VPN, error) {
			return NordVPN{run: newRunner(bin, "nordvpn", s.Timeout)}, nil
		},
	})
}

// NordVPN is NordVPN, through its Linux CLI.
type NordVPN struct{ run runner }

// Kind is NordVPN.
func (n NordVPN) Kind() Kind { k, _ := KindOf("nordvpn"); return k }

// ParseNordVPN reads `nordvpn status`: a "Status:" line (Connected,
// Connecting, Paused or Disconnected), and while it is up the server's
// hostname, country and city. The "IP:" line is never kept. Dashes and
// carriage returns before a line are trimmed.
func ParseNordVPN(out string) (State, bool) {
	fields := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(strings.TrimLeft(line, "-\r \t")), ":")
		if !ok {
			continue
		}
		fields[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	st, ok := fields["status"]
	if !ok {
		return State{}, false
	}
	s := State{}
	switch strings.ToLower(st) {
	case "connected":
		s.State = Connected
	case "connecting":
		s.State = Connecting
	case "disconnected":
		return State{State: Disconnected}, true
	case "paused":
		cause := "paused"
		if left := fields["pause time left"]; left != "" {
			cause += ", " + left + " left"
		}
		return State{State: Disconnected, Cause: cause}, true
	default:
		return State{State: Error, Cause: st}, true
	}
	s.City, s.Country = fields["city"], fields["country"]
	if h := fields["hostname"]; h != "" {
		s.Host, _, _ = strings.Cut(h, ".")
	}
	return s, true
}

// Status is the tunnel's state now.
func (n NordVPN) Status() (State, error) {
	out, code, err := n.run.exec(n.run.timeout, "status")
	if err != nil {
		return State{}, err
	}
	if s, ok := ParseNordVPN(out); ok {
		return s, nil
	}
	return State{}, n.run.failed([]string{"status"}, out, code)
}

// Connect and Disconnect switch NordVPN on, to its own choice of server,
// and off.
func (n NordVPN) Connect() error {
	_, err := n.run.long("connect")
	return err
}

func (n NordVPN) Disconnect() error {
	_, err := n.run.run("disconnect")
	return err
}

// nordPlace is a word of a NordVPN place: a country, a city or a server,
// with underscores for spaces.
var nordPlace = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// CheckPlace and SetPlace make NordVPN a Placer: its places are a country,
// a country and a city, or a server, as `nordvpn connect` takes them, and
// it connects there whether it was up or not.
func (n NordVPN) CheckPlace(where []string) error {
	ok := len(where) == 1 || len(where) == 2
	for _, w := range where {
		ok = ok && nordPlace.MatchString(w)
	}
	if !ok {
		return fmt.Errorf("a place for NordVPN is a country, such as sweden; a country and a city, such as united_states new_york; or a server, such as se123 (`nordvpn countries` and `nordvpn cities COUNTRY` list them)")
	}
	return nil
}

func (n NordVPN) SetPlace(where []string, _ bool) error {
	_, err := n.run.long(append([]string{"connect"}, where...)...)
	return err
}
