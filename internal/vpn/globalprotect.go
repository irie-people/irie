package vpn

// GlobalProtect, Palo Alto Networks' VPN, through its Linux CLI,
// globalprotect: `globalprotect show --status` for the state,
// `globalprotect connect --portal PORTAL` and `globalprotect disconnect`.
// `show --details` and `show --host-state` name the machine's addresses,
// and Irie never asks for them. Its Mac app has no command line, and the
// VPN it keeps in System Settings reads disconnected even while it is up,
// so on a Mac GlobalProtect goes through commands of the company's own.

import (
	"strings"
)

func init() {
	kinds = append(kinds, Kind{
		Name:    "globalprotect",
		Title:   "GlobalProtect",
		Get:     "your company's GlobalProtect portal",
		Server:  "the GlobalProtect portal, such as vpn.example.com",
		Systems: []string{"linux"},
		// Its Mac app has no command line, and its VPN in System Settings
		// reads disconnected while it is up.
		Elsewhere: "its Mac app has no command line, so the command VPN follows it through commands of your own (vpn.provider = \"command\")",
		find: func(s Settings) (string, error) {
			return findCLI(s.CLI, []string{"globalprotect"}, []string{"/usr/bin/globalprotect", "/opt/paloaltonetworks/globalprotect/globalprotect"})
		},
		open: func(bin string, s Settings) (VPN, error) {
			return GlobalProtect{run: newRunner(bin, "globalprotect", s.Timeout), portal: s.Server}, nil
		},
	})
}

// GlobalProtect is GlobalProtect, through its Linux CLI.
type GlobalProtect struct {
	run    runner
	portal string
}

// Kind is GlobalProtect.
func (g GlobalProtect) Kind() Kind { k, _ := KindOf("globalprotect"); return k }

// ParseGlobalProtect reads `globalprotect show --status`, such as
// "GlobalProtect status: Connected", or the bare command's "Current
// GlobalProtect status: disabled".
func ParseGlobalProtect(out string) (State, bool) {
	state, found := "", false
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.HasSuffix(strings.ToLower(strings.TrimSpace(k)), "globalprotect status") {
			state, found = strings.ToLower(strings.TrimRight(strings.TrimSpace(v), ".")), true
			break
		}
	}
	if !found {
		return State{}, false
	}
	switch state {
	case "connected":
		return State{State: Connected}, true
	case "connecting":
		return State{State: Connecting}, true
	case "disconnecting":
		return State{State: Disconnecting}, true
	case "disconnected":
		return State{State: Disconnected}, true
	case "disabled":
		return State{State: Disconnected, Cause: "GlobalProtect is disabled"}, true
	}
	return State{State: Error, Cause: "GlobalProtect says " + state}, true
}

// Status is the tunnel's state now.
func (g GlobalProtect) Status() (State, error) {
	out, code, err := g.run.exec(g.run.timeout, "show", "--status")
	if err != nil {
		return State{}, err
	}
	s, ok := ParseGlobalProtect(out)
	if !ok {
		return State{}, g.run.failed([]string{"show", "--status"}, out, code)
	}
	if s.State == Connected || s.State == Connecting {
		s.Host = g.portal
	}
	return s, nil
}

// Connect connects through the portal, or the one GlobalProtect last used.
// A portal that asks for a password the CLI has none for fails and says
// so; single sign-on, from GlobalProtect 6.2.1, opens the browser.
func (g GlobalProtect) Connect() error {
	args := []string{"connect"}
	if g.portal != "" {
		args = append(args, "--portal", g.portal)
	}
	_, err := g.run.long(args...)
	return err
}

// Disconnect disconnects. In always-on mode GlobalProtect may want a
// reason or a passcode first, and then says so.
func (g GlobalProtect) Disconnect() error {
	_, err := g.run.run("disconnect")
	return err
}
