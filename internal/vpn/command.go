package vpn

// The command kind: any VPN with a command line, through commands the
// person names in config.toml, for a company's VPN that Irie doesn't know
// by name.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

func init() {
	kinds = append(kinds, Kind{
		Name:   "command",
		Title:  "Custom VPN",
		Server: "what it connects to, shown while it is up",
		find: func(s Settings) (string, error) {
			if len(s.Status) == 0 {
				return "", errors.New("the custom VPN needs vpn.status: the command that prints your VPN's state, with vpn.connected, the pattern its output matches while connected")
			}
			if _, err := CommandPattern("vpn.connected", s.Connected, true); err != nil {
				return "", err
			}
			if _, err := CommandPattern("vpn.connecting", s.Connecting, false); err != nil {
				return "", err
			}
			return lookCommand("vpn.status", s.Status[0])
		},
		open: openCommand,
	})
}

// Command is a VPN driven by commands of the person's own: Status prints
// the state, which Connected and Connecting match; Connect and Disconnect
// change it.
type Command struct {
	run                         runner
	status, connect, disconnect []string
	connected, connecting       *regexp.Regexp
	server                      string
}

func openCommand(bin string, s Settings) (VPN, error) {
	connected, err := CommandPattern("vpn.connected", s.Connected, true)
	if err != nil {
		return nil, err
	}
	connecting, err := CommandPattern("vpn.connecting", s.Connecting, false)
	if err != nil {
		return nil, err
	}
	c := Command{
		run:        newRunner(bin, s.Status[0], s.Timeout),
		status:     s.Status,
		connect:    s.Connect,
		disconnect: s.Disconnect,
		connected:  connected,
		connecting: connecting,
		server:     s.Server,
	}
	return c, nil
}

// CommandPattern checks one of the command kind's patterns, a regular
// expression, and compiles it; nil for one that is empty and may be.
func CommandPattern(key, pattern string, must bool) (*regexp.Regexp, error) {
	if pattern == "" {
		if must {
			return nil, fmt.Errorf("%s is empty: it is the pattern your VPN's status prints while connected, and never otherwise, such as \"(?m)^State: Connected$\" (\"connected\" alone also matches Disconnected)", key)
		}
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s = %q: %v", key, pattern, err)
	}
	return re, nil
}

// Kind is the command kind.
func (c Command) Kind() Kind { k, _ := KindOf("command"); return k }

// Status runs vpn.status. Its output, never its exit code, says the
// state: many VPNs exit with an error while they are off.
func (c Command) Status() (State, error) {
	out, _, err := c.run.exec(c.run.timeout, c.status[1:]...)
	if err != nil {
		return State{}, err
	}
	return c.Parse(out), nil
}

// Parse is the state in what vpn.status printed.
func (c Command) Parse(out string) State {
	switch {
	case c.connecting != nil && c.connecting.MatchString(out):
		return State{State: Connecting, Host: c.server}
	case c.connected.MatchString(out):
		return State{State: Connected, Host: c.server}
	}
	return State{State: Disconnected}
}

// Connect and Disconnect run vpn.connect and vpn.disconnect.
func (c Command) Connect() error { return c.do("vpn.connect", c.connect) }

func (c Command) Disconnect() error { return c.do("vpn.disconnect", c.disconnect) }

func (c Command) do(key string, argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("%s is empty: set it in config.toml to the command that does it", key)
	}
	bin, err := lookCommand(key, argv[0])
	if err != nil {
		return err
	}
	_, err = newRunner(bin, argv[0], c.run.timeout).long(argv[1:]...)
	return err
}

// lookCommand finds a command's program: a path as it is, else by name on
// the PATH.
func lookCommand(key, prog string) (string, error) {
	if strings.ContainsRune(prog, '/') {
		if _, err := os.Stat(prog); err != nil {
			return "", fmt.Errorf("%s: %v", key, err)
		}
		return prog, nil
	}
	p, err := exec.LookPath(prog)
	if err != nil {
		return "", fmt.Errorf("%s: %s is not on the PATH; give its full path", key, prog)
	}
	return p, nil
}
