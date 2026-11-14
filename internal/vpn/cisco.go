package vpn

// Cisco Secure Client, once AnyConnect, through its CLI, vpn: `vpn state`
// for the state, `vpn connect HOST` and `vpn disconnect`. Its statistics
// (`vpn stats`) name the machine's addresses, and Irie never asks for
// them.
//
// The CLI can't connect while Cisco's own app holds the connection, which
// on a Mac it usually does, nor with single sign-on, which Cisco supports
// only in its app. Then the person connects in the app, and Irie says so;
// it never opens a window itself (DESIGN §4.11, "Out of sight").

import (
	"errors"
	"strings"
)

func init() {
	kinds = append(kinds, Kind{
		Name:   "cisco",
		Title:  "Cisco Secure Client",
		Get:    "your company's IT, or the page your company's VPN gateway serves",
		Server: "the VPN's host, such as vpn.example.com, or an entry's name in its profile",
		Needs:  true,
		find: func(s Settings) (string, error) {
			// Never vpn on the PATH: the name is too common to be Cisco's.
			return findCLI(s.CLI, nil, []string{"/opt/cisco/secureclient/bin/vpn", "/opt/cisco/anyconnect/bin/vpn"})
		},
		open: func(bin string, s Settings) (VPN, error) {
			return Cisco{run: newRunner(bin, "vpn", s.Timeout), host: s.Server}, nil
		},
	})
}

// Cisco is Cisco Secure Client, through its CLI.
type Cisco struct {
	run  runner
	host string
}

// Kind is Cisco Secure Client.
func (c Cisco) Kind() Kind { k, _ := KindOf("cisco"); return k }

// ParseCisco reads `vpn state`: after a banner, lines such as
// "  >> state: Connected", "State" in Secure Client 5's own sample. The
// last says how it is now; Unknown, which Secure Client 5 prints before
// the real state, counts only when nothing else comes.
func ParseCisco(out string) (State, bool) {
	state, found := "", false
	for _, line := range strings.Split(out, "\n") {
		_, v, ok := strings.Cut(strings.ToLower(line), ">> state:")
		if !ok {
			continue
		}
		found = true
		if v = strings.TrimSpace(v); v != "unknown" || state == "" {
			state = v
		}
	}
	if !found {
		return State{}, false
	}
	switch state {
	case "connected":
		return State{State: Connected}, true
	case "connecting", "reconnecting":
		return State{State: Connecting}, true
	case "disconnecting":
		return State{State: Disconnecting}, true
	case "disconnected":
		return State{State: Disconnected}, true
	case "unknown":
		return State{State: Error, Cause: "Cisco Secure Client doesn't know the tunnel's state yet"}, true
	}
	return State{State: Error, Cause: "Cisco Secure Client says " + state}, true
}

// Status is the tunnel's state now.
func (c Cisco) Status() (State, error) {
	out, code, err := c.run.exec(c.run.timeout, "state")
	if err != nil {
		return State{}, err
	}
	s, ok := ParseCisco(out)
	if !ok {
		return State{}, c.run.failed([]string{"state"}, out, code)
	}
	if s.State == Connected || s.State == Connecting {
		s.Host = c.host
	}
	return s, nil
}

// Connect connects to the host through the CLI, with nothing to answer a
// prompt with. Where the CLI can't, the person connects in Cisco's app.
func (c Cisco) Connect() error {
	out, code, err := c.run.exec(2*c.run.timeout, "connect", c.host)
	if err == nil && code == 0 && !strings.Contains(strings.ToLower(out), ">> error:") {
		return nil
	}
	if err == nil {
		err = c.run.failed([]string{"connect", c.host}, out, code)
	}
	return errors.New("Cisco Secure Client connects here in its own app: its command line can't while the app holds the connection, nor with single sign-on (" + err.Error() + ")")
}

// Disconnect disconnects.
func (c Cisco) Disconnect() error {
	out, code, err := c.run.exec(c.run.timeout, "disconnect")
	if err != nil {
		return err
	}
	if code != 0 || strings.Contains(strings.ToLower(out), ">> error:") {
		return c.run.failed([]string{"disconnect"}, out, code)
	}
	return nil
}
