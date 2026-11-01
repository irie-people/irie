// Package vpn reads and drives Mullvad VPN through its own CLI, mullvad
// (docs/proposals/extensions.md §4). It never keeps the account number,
// which is the account's only secret, and never keeps the machine's own
// address or place, which the status carries while the VPN is off.
package vpn

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// The tunnel's states, as the daemon names them.
const (
	Connected     = "connected"
	Connecting    = "connecting"
	Disconnected  = "disconnected"
	Disconnecting = "disconnecting"
	Error         = "error"
)

// State is what Irie keeps of the tunnel: its state and, while it is up or
// coming up, the relay's place. Nothing of the machine's own address.
type State struct {
	State    string `json:"state"`
	City     string `json:"city,omitempty"`
	Country  string `json:"country,omitempty"`
	Host     string `json:"host,omitempty"`     // the relay, such as se-got-wg-001
	Blocked  bool   `json:"blocked,omitempty"`  // off, and lockdown mode blocks the network
	Cause    string `json:"cause,omitempty"`    // why it is in error, such as is_offline
	Blocking bool   `json:"blocking,omitempty"` // in error, and the daemon blocks traffic meanwhile
}

// status is the daemon's TunnelState as `mullvad status --json` prints it:
// {"state": …, "details": …}.
type status struct {
	State   string          `json:"state"`
	Details json.RawMessage `json:"details"`
}

type tunnel struct {
	Location *struct {
		City     *string `json:"city"`
		Country  string  `json:"country"`
		Location map[string]json.RawMessage
	} `json:"location"`
	LockedDown bool `json:"locked_down"`
}

type errState struct {
	Cause struct {
		Reason string `json:"reason"`
	} `json:"cause"`
	BlockFailure json.RawMessage `json:"block_failure"`
}

// Parse reads one line of `mullvad status --json` or of `status listen
// --json`. It reports false for a line that is not a tunnel's state: the
// listener also prints settings, the relay list and device events, and a
// warning line when the device is logged out.
func Parse(line []byte) (State, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return State{}, false
	}
	var s status
	if json.Unmarshal(line, &s) != nil {
		return State{}, false
	}
	out := State{State: s.State}
	switch s.State {
	case Connected, Connecting:
		var t tunnel
		if json.Unmarshal(s.Details, &t) == nil && t.Location != nil {
			if t.Location.City != nil {
				out.City = *t.Location.City
			}
			out.Country = t.Location.Country
			out.Host = relayHost(t.Location.Location)
		}
	case Disconnected:
		// Its location is the machine's own: only the lockdown is kept.
		var t tunnel
		if json.Unmarshal(s.Details, &t) == nil {
			out.Blocked = t.LockedDown
		}
	case Error:
		var e errState
		if json.Unmarshal(s.Details, &e) == nil {
			out.Cause = e.Cause.Reason
			out.Blocking = len(e.BlockFailure) == 0 || string(e.BlockFailure) == "null"
		}
	case Disconnecting:
	default:
		return State{}, false
	}
	return out, true
}

// relayHost is the host name in the relay's location constraint, which the
// daemon writes as {"hostname": [country, city, host]}.
func relayHost(loc map[string]json.RawMessage) string {
	var parts []string
	if json.Unmarshal(loc["hostname"], &parts) == nil && len(parts) == 3 {
		return parts[2]
	}
	return ""
}

// Words is the state in one line.
func (s State) Words() string {
	where := strings.TrimSpace(strings.Join(nonEmpty(s.City, s.Country), ", "))
	if s.Host != "" {
		where = strings.TrimSpace(where + " (" + s.Host + ")")
	}
	switch s.State {
	case Connected:
		return "VPN on: " + orWords(where, "connected")
	case Connecting:
		return "VPN connecting" + prefix(": ", where)
	case Disconnecting:
		return "VPN disconnecting"
	case Disconnected:
		if s.Blocked {
			return "VPN off, and lockdown mode blocks the network"
		}
		return "VPN off"
	case Error:
		w := "VPN in error"
		if s.Cause != "" {
			w += ": " + strings.ReplaceAll(s.Cause, "_", " ")
		}
		if s.Blocking {
			return w + "; the network is blocked meanwhile"
		}
		return w + "; the network is NOT blocked"
	}
	return "VPN state unknown"
}

func nonEmpty(xs ...string) []string {
	var out []string
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func orWords(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

func prefix(p, s string) string {
	if s == "" {
		return ""
	}
	return p + s
}

// ParseExpiry finds the account's expiry in `mullvad account get`. Only the
// line that starts "Expires at:" is read; the account number on the line
// before it is never looked at, kept or logged.
func ParseExpiry(out string) (time.Time, bool) {
	for _, line := range strings.Split(out, "\n") {
		v, ok := strings.CutPrefix(strings.TrimSpace(line), "Expires at:")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		for _, layout := range []string{"2006-01-02 15:04:05.999999999 -07:00", "2006-01-02 15:04:05 -07:00", time.RFC3339Nano} {
			if t, err := time.Parse(layout, v); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}
	return time.Time{}, false
}

// ParseLockdown reads `mullvad lockdown-mode get`: "Block traffic when the
// VPN is disconnected: on".
func ParseLockdown(out string) (bool, bool) {
	_, v, ok := strings.Cut(strings.TrimSpace(out), ":")
	if !ok {
		return false, false
	}
	switch strings.TrimSpace(v) {
	case "on":
		return true, true
	case "off":
		return false, true
	}
	return false, false
}

// Find returns the mullvad CLI: the configured one, else mullvad on the
// PATH, else where Mullvad's own packages put it.
func Find(configured string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("vpn.mullvad: %v", err)
		}
		return configured, nil
	}
	if p, err := exec.LookPath("mullvad"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/usr/local/bin/mullvad", "/usr/bin/mullvad"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("Mullvad VPN's CLI, mullvad, is not here: install the Mullvad VPN app, which brings it")
}

// CLI runs mullvad.
type CLI struct {
	Bin     string
	Timeout time.Duration // per command; 30 s when zero
}

// run runs one command and returns what it printed. Its output never goes
// to a log: `account get` prints the account number.
func (c CLI) run(args ...string) (string, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Bin, args...).CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("mullvad %s took more than %s", args[0], timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("mullvad %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}

// Status is the tunnel's state now.
func (c CLI) Status() (State, error) {
	out, err := c.run("status", "--json")
	if err != nil {
		return State{}, err
	}
	for _, line := range strings.Split(out, "\n") {
		if s, ok := Parse([]byte(line)); ok {
			return s, nil
		}
	}
	return State{}, errors.New("mullvad status --json printed no state; this mullvad may be too old for --json")
}

// Expiry is when the account's time runs out.
func (c CLI) Expiry() (time.Time, error) {
	out, err := c.run("account", "get")
	if err != nil {
		return time.Time{}, errors.New("mullvad account get failed")
	}
	if t, ok := ParseExpiry(out); ok {
		return t, nil
	}
	return time.Time{}, errors.New("this device is not logged in to a Mullvad account")
}

// Lockdown is whether lockdown mode is on.
func (c CLI) Lockdown() (bool, error) {
	out, err := c.run("lockdown-mode", "get")
	if err != nil {
		return false, err
	}
	on, ok := ParseLockdown(out)
	if !ok {
		return false, fmt.Errorf("mullvad lockdown-mode get: %q", strings.TrimSpace(out))
	}
	return on, nil
}

// Connect, Disconnect and Reconnect wait until the tunnel is there.
func (c CLI) Connect() error    { _, err := c.run("connect", "--wait"); return err }
func (c CLI) Disconnect() error { _, err := c.run("disconnect", "--wait"); return err }
func (c CLI) Reconnect() error  { _, err := c.run("reconnect", "--wait"); return err }

// SetLockdown turns lockdown mode on or off.
func (c CLI) SetLockdown(on bool) error {
	v := "off"
	if on {
		v = "on"
	}
	_, err := c.run("lockdown-mode", "set", v)
	return err
}

var (
	country = regexp.MustCompile(`^([a-z]{2}|any)$`)
	city    = regexp.MustCompile(`^[a-z]{3}$`)
	host    = regexp.MustCompile(`^[a-z]{2}-[a-z]{3}-[a-z0-9-]+$`)
)

// SetLocation picks the relays: a country, a country and a city, those and
// a host, or a host alone, as `mullvad relay set location` takes them.
func (c CLI) SetLocation(where []string) error {
	if err := CheckLocation(where); err != nil {
		return err
	}
	_, err := c.run(append([]string{"relay", "set", "location"}, where...)...)
	return err
}

// CheckLocation says whether where is a location mullvad takes.
func CheckLocation(where []string) error {
	ok := false
	switch len(where) {
	case 1:
		ok = country.MatchString(where[0]) || host.MatchString(where[0])
	case 2:
		ok = country.MatchString(where[0]) && city.MatchString(where[1])
	case 3:
		ok = country.MatchString(where[0]) && city.MatchString(where[1]) && host.MatchString(where[2])
	}
	if !ok {
		return fmt.Errorf("a place is a country code, such as se; a country and a city, such as se got; or a server, such as se-got-wg-001 (`mullvad relay list` lists them)")
	}
	return nil
}

// LockdownSetting reads lockdown mode from a line of the daemon's settings, which
// `status listen --json` prints when they change: {"lockdown_mode": …}.
func LockdownSetting(line []byte) (bool, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return false, false
	}
	var s struct {
		Lockdown *bool `json:"lockdown_mode"`
	}
	if json.Unmarshal(line, &s) != nil || s.Lockdown == nil {
		return false, false
	}
	return *s.Lockdown, true
}

// Listen runs `mullvad status listen --json` until ctx ends or it exits, and
// calls each with every state it prints, and lockdown, when it is not nil,
// with lockdown mode whenever the settings change. Lines can be long: the
// relay list is one.
func (c CLI) Listen(ctx context.Context, each func(State), lockdown func(bool)) error {
	cmd := exec.CommandContext(ctx, c.Bin, "status", "listen", "--json")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	r := bufio.NewReaderSize(out, 64<<10)
	for {
		line, err := readLine(r)
		if len(line) > 0 {
			if s, ok := Parse(line); ok {
				each(s)
			} else if on, ok := LockdownSetting(line); ok && lockdown != nil {
				lockdown(on)
			}
		}
		if err != nil {
			break
		}
	}
	return cmd.Wait()
}

// readLine reads a line of any length; a long line that is not a state is
// dropped as it is read, past a megabyte.
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(buf) < 1<<20 {
			buf = append(buf, chunk...)
		}
		if err != bufio.ErrBufferFull {
			return buf, err
		}
	}
}
