package vpn

// The system kind: a VPN the computer itself keeps, by its name. On macOS
// that is a VPN in System Settings › VPN, which scutil --nc reads and
// drives: IKEv2, L2TP and Cisco IPSec, and the VPN apps that keep their
// tunnels there, such as WireGuard's. On Linux it is a NetworkManager
// connection, through nmcli: OpenVPN, WireGuard, and the openconnect
// plugin for Cisco, GlobalProtect, Fortinet and Pulse gateways.

import (
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
)

func init() {
	kinds = append(kinds, Kind{
		Name:    "system",
		Title:   "The system's VPN",
		Server:  "the VPN's name, as System Settings › VPN (macOS) or NetworkManager (Linux) shows it",
		Needs:   true,
		Systems: []string{"darwin", "linux"},
		find: func(s Settings) (string, error) {
			if runtime.GOOS == "darwin" {
				return findCLI(s.CLI, nil, []string{"/usr/sbin/scutil"})
			}
			bin, err := findCLI(s.CLI, []string{"nmcli"}, []string{"/usr/bin/nmcli"})
			if errors.Is(err, errMissing) {
				return "", errors.New("NetworkManager's nmcli isn't installed: the system's VPNs on Linux are NetworkManager's connections")
			}
			return bin, err
		},
		open: func(bin string, s Settings) (VPN, error) {
			r := newRunner(bin, "scutil", s.Timeout)
			if runtime.GOOS != "darwin" {
				r.name = "nmcli"
				return NetworkManager{run: r, name: s.Server}, nil
			}
			return MacVPN{run: r, name: s.Server}, nil
		},
	})
}

// MacVPN is a VPN in macOS's System Settings › VPN, by its name.
type MacVPN struct {
	run  runner
	name string
}

// Kind is the system kind.
func (m MacVPN) Kind() Kind { k, _ := KindOf("system"); return k }

// Status is `scutil --nc status NAME`'s first line. The extended status
// after it holds the tunnel's addresses, and is never read.
func (m MacVPN) Status() (State, error) {
	out, err := m.run.run("--nc", "status", m.name)
	if err != nil {
		if strings.Contains(err.Error(), "No service") {
			return State{}, fmt.Errorf("macOS has no VPN called %q: System Settings › VPN lists them, and `scutil --nc list` too", m.name)
		}
		return State{}, err
	}
	s, ok := ParseScutil(out)
	if !ok {
		return State{}, fmt.Errorf("scutil --nc status printed no state")
	}
	if s.State == Connected || s.State == Connecting {
		s.Host = m.name
	}
	return s, nil
}

// ParseScutil reads the state on the first line of `scutil --nc status`:
// Connected, Connecting, Disconnected, Disconnecting, or Invalid with the
// reason, as configd's scutil prints them.
func ParseScutil(out string) (State, bool) {
	line, _, _ := strings.Cut(strings.TrimLeft(out, "\n"), "\n")
	word, rest, _ := strings.Cut(strings.TrimSpace(line), ":")
	switch word {
	case "Connected":
		return State{State: Connected}, true
	case "Connecting":
		return State{State: Connecting}, true
	case "Disconnected":
		return State{State: Disconnected}, true
	case "Disconnecting":
		return State{State: Disconnecting}, true
	case "Invalid":
		cause := strings.TrimSpace(rest)
		if cause == "" {
			cause = "the VPN is turned off in System Settings"
		}
		return State{State: Error, Cause: cause}, true
	}
	return State{}, false
}

// Connect and Disconnect start and stop the VPN. scutil returns once it
// has asked; the state follows.
func (m MacVPN) Connect() error {
	_, err := m.run.long("--nc", "start", m.name)
	return err
}

func (m MacVPN) Disconnect() error {
	_, err := m.run.run("--nc", "stop", m.name)
	return err
}

// NetworkManager is a NetworkManager connection, by its name.
type NetworkManager struct {
	run  runner
	name string
}

// Kind is the system kind.
func (n NetworkManager) Kind() Kind { k, _ := KindOf("system"); return k }

// Status finds the connection among the active ones: there, its state;
// not there, off. Only names, types and states are asked for, never the
// connection's addresses.
func (n NetworkManager) Status() (State, error) {
	out, err := n.run.run("-t", "-f", "NAME,TYPE,STATE", "connection", "show", "--active")
	if err != nil {
		return State{}, err
	}
	s := ParseNmcli(out, n.name)
	if s.State == Connected || s.State == Connecting {
		s.Host = n.name
	}
	return s, nil
}

// ParseNmcli is the state of the connection called name in `nmcli -t -f
// NAME,TYPE,STATE connection show --active`: a line for each active
// connection, its fields split by colons, and a colon or backslash in a
// field escaped with a backslash.
func ParseNmcli(out, name string) State {
	for _, line := range strings.Split(out, "\n") {
		f := splitTerse(line)
		if len(f) < 3 || f[0] != name {
			continue
		}
		switch f[len(f)-1] {
		case "activated":
			return State{State: Connected}
		case "activating":
			return State{State: Connecting}
		case "deactivating":
			return State{State: Disconnecting}
		}
	}
	return State{State: Disconnected}
}

// splitTerse splits a line of nmcli's terse output at the colons that
// aren't escaped, and takes the escapes out.
func splitTerse(line string) []string {
	var out []string
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && i+1 < len(line):
			i++
			b.WriteByte(line[i])
		case c == ':':
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	return append(out, b.String())
}

// Connect and Disconnect bring the connection up and down. nmcli waits for
// it, less long than Irie does, so its own reason comes back.
func (n NetworkManager) Connect() error {
	_, err := n.run.long("--wait", fmt.Sprint(int(2*n.run.timeout.Seconds())-5), "connection", "up", "id", n.name)
	return err
}

func (n NetworkManager) Disconnect() error {
	_, err := n.run.run("connection", "down", "id", n.name)
	return err
}

// SystemVPNs are the names of the system's own VPNs, which vpn.server may
// name for the system kind: macOS's in System Settings › VPN, and Linux's
// NetworkManager connections of type vpn or wireguard.
func SystemVPNs() ([]string, error) {
	k, _ := KindOf("system")
	bin, err := k.Find(Settings{})
	if err != nil {
		return nil, err
	}
	r := newRunner(bin, "scutil", 0)
	if runtime.GOOS != "darwin" {
		r.name = "nmcli"
		out, err := r.run("-t", "-f", "NAME,TYPE", "connection", "show")
		if err != nil {
			return nil, err
		}
		return ParseNmcliList(out), nil
	}
	out, err := r.run("--nc", "list")
	if err != nil {
		return nil, err
	}
	return ParseScutilList(out), nil
}

// scutilName is the name on a line of `scutil --nc list`, which configd's
// scutil writes in quotes just before the service's type in brackets.
var scutilName = regexp.MustCompile(`"([^"]*)"\s*\[[^\]]*\]\s*$`)

// ParseScutilList is the names in `scutil --nc list`.
func ParseScutilList(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if m := scutilName.FindStringSubmatch(line); m != nil && m[1] != "" {
			names = append(names, m[1])
		}
	}
	return names
}

// ParseNmcliList is the VPNs' names in `nmcli -t -f NAME,TYPE connection
// show`.
func ParseNmcliList(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		f := splitTerse(line)
		if len(f) == 2 && (f[1] == "vpn" || f[1] == "wireguard") && f[0] != "" {
			names = append(names, f[0])
		}
	}
	return names
}
