// Package vpn reads and drives a VPN through the VPN's own command line
// (DESIGN §4.18): Mullvad VPN, Tailscale, NetBird, Cloudflare WARP,
// NordVPN, Cisco Secure Client, GlobalProtect, FortiClient, the system's
// own VPNs, or commands of the person's own for any other. Each is a Kind, and Open makes the one that
// Settings name, so a company's copy of Irie drives the VPN it approves.
//
// Whatever the VPN, Irie keeps only the tunnel's state and, while it is up,
// the name of where it goes: a relay, a gateway, a portal or a network.
// Never the machine's own address or place, which several of these VPNs
// print, and never an account's number, a sign-in or a password.
package vpn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
)

// The tunnel's states. Every kind of VPN says its own in these words.
const (
	Connected     = "connected"
	Connecting    = "connecting"
	Disconnected  = "disconnected"
	Disconnecting = "disconnecting"
	Error         = "error"
)

// State is what Irie keeps of the tunnel: its state and, while it is up or
// coming up, where it goes. Nothing of the machine's own address.
type State struct {
	State   string `json:"state"`
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
	// Host is where the tunnel goes, as the VPN names it: a relay such as
	// se-got-wg-001, a gateway, a portal, a network or an exit node.
	Host     string `json:"host,omitempty"`
	Blocked  bool   `json:"blocked,omitempty"`  // off, and lockdown mode blocks the network
	Cause    string `json:"cause,omitempty"`    // why it is off or in error, such as is_offline or needs login
	Blocking bool   `json:"blocking,omitempty"` // in error, and the VPN blocks traffic meanwhile
	Leaks    bool   `json:"leaks,omitempty"`    // in error, and the VPN could not block traffic
}

// Words is the state in one line.
func (s State) Words() string {
	where := strings.TrimSpace(strings.Join(nonEmpty(s.City, s.Country), ", "))
	if s.Host != "" {
		if where == "" {
			where = s.Host
		} else {
			where += " (" + s.Host + ")"
		}
	}
	cause := strings.ReplaceAll(s.Cause, "_", " ")
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
		return "VPN off" + prefix(": ", cause)
	case Error:
		w := "VPN in error" + prefix(": ", cause)
		switch {
		case s.Blocking:
			return w + "; the network is blocked meanwhile"
		case s.Leaks:
			return w + "; the network is NOT blocked"
		}
		return w
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

// VPN is one VPN as Irie reads and drives it. Connect and Disconnect
// return once the VPN's command has; the next Status says how it went.
type VPN interface {
	Kind() Kind
	Status() (State, error)
	Connect() error
	Disconnect() error
}

// Placer is a VPN that leaves through a place the person picks: Mullvad's
// relays, Tailscale's exit nodes, NordVPN's countries and cities.
type Placer interface {
	// CheckPlace says whether where is a place this VPN takes, and if not,
	// what a place is.
	CheckPlace(where []string) error
	// SetPlace makes where the VPN's place, and moves the tunnel there
	// when it is up.
	SetPlace(where []string, up bool) error
}

// Lockdowner is a VPN with a mode that blocks the network while it is off.
type Lockdowner interface {
	Lockdown() (bool, error)
	SetLockdown(on bool) error
}

// Timer is a VPN whose account's time runs out.
type Timer interface {
	Expiry() (time.Time, error)
}

// Listener is a VPN that says each change as it comes, until ctx ends or
// it stops; the core asks the others every few seconds instead.
type Listener interface {
	Listen(ctx context.Context, each func(State), lockdown func(bool)) error
}

// What a VPN's words do beyond on and off, as Can lists them.
const (
	CanPlace    = "place"
	CanLockdown = "lockdown"
	CanTime     = "time"
)

// Can is what v does beyond on and off, for the clients' menus and the
// :vpn words: place, lockdown and time.
func Can(v VPN) []string {
	var out []string
	if _, ok := v.(Placer); ok {
		out = append(out, CanPlace)
	}
	if _, ok := v.(Lockdowner); ok {
		out = append(out, CanLockdown)
	}
	if _, ok := v.(Timer); ok {
		out = append(out, CanTime)
	}
	return out
}

// Settings choose the VPN and say how to reach it: config.toml's [vpn],
// as internal/plugins reads it.
type Settings struct {
	Provider string // a Kind's Name
	// CLI is the VPN's own command, when it isn't where Irie looks.
	CLI string
	// Server is what it connects to, for the kinds that take one: Cisco
	// Secure Client's host, GlobalProtect's portal, or the system's VPN by
	// its name.
	Server string
	// Status, Connect and Disconnect are the command kind's commands, each
	// a program and its arguments, and Connected and Connecting the
	// regular expressions its status's output matches in those states.
	Status, Connect, Disconnect []string
	Connected, Connecting       string
	// Timeout limits each command; 30 s when zero, and twice that for
	// connecting, which may wait on a sign-in.
	Timeout time.Duration
}

// Kind is one kind of VPN Irie knows.
type Kind struct {
	Name  string // vpn.provider's value, such as mullvad
	Title string // Mullvad VPN
	// Get says where its app comes from, which brings the command Irie
	// uses, in words that follow "from", or "" when every system has it.
	Get string
	// Server says what vpn.server names for this kind, or "" when it takes
	// none, and Needs whether it must have one.
	Server string
	Needs  bool
	// Systems are the GOOS values it runs on; nil = any. Elsewhere says
	// what to do on another, after "isn't for macOS".
	Systems   []string
	Elsewhere string

	find func(Settings) (string, error)
	open func(bin string, s Settings) (VPN, error)
}

// kinds are the kinds of VPN. Each kind's file adds its own as it starts,
// so they come in the files' order; Kinds puts them in order's.
var kinds []Kind

// order is the order the kinds show in: personal VPNs, then the ones
// companies run, then the system's own and the person's commands.
var order = []string{"mullvad", "tailscale", "netbird", "warp", "nordvpn", "cisco", "globalprotect", "forticlient", "system", "command"}

// Kinds are every kind of VPN Irie knows, in order.
func Kinds() []Kind {
	out := slices.Clone(kinds)
	slices.SortStableFunc(out, func(a, b Kind) int { return rank(a.Name) - rank(b.Name) })
	return out
}

func rank(name string) int {
	if i := slices.Index(order, name); i >= 0 {
		return i
	}
	return len(order)
}

// Names are the kinds' names, in order.
func Names() []string {
	var out []string
	for _, k := range Kinds() {
		out = append(out, k.Name)
	}
	return out
}

// KindOf is the kind called name.
func KindOf(name string) (Kind, bool) {
	for _, k := range kinds {
		if k.Name == name {
			return k, true
		}
	}
	return Kind{}, false
}

// Runs says whether the kind runs on this system.
func (k Kind) Runs() bool { return k.Systems == nil || slices.Contains(k.Systems, runtime.GOOS) }

// NotHere is the sentence that says the kind isn't for this system, and
// what to do instead.
func (k Kind) NotHere() string {
	s := k.Title + " isn't for " + osName()
	if k.Elsewhere != "" {
		s += ": " + k.Elsewhere
	}
	return s
}

// Find is the kind's own command: the configured one, else where its app
// puts it on this system. Not there, the error says where it comes from.
func (k Kind) Find(s Settings) (string, error) {
	if !k.Runs() {
		return "", errors.New(k.NotHere())
	}
	bin, err := k.find(s)
	if errors.Is(err, errMissing) {
		return "", errors.New(k.Missing())
	}
	return bin, err
}

// errMissing is a VPN's command that isn't where its app puts it.
var errMissing = errors.New("not installed")

// Missing is the sentence that says the kind's command isn't here, and
// where it comes from.
func (k Kind) Missing() string {
	if k.Get == "" {
		return k.Title + " isn't here."
	}
	return k.Title + " isn't installed: its app, from " + k.Get + ", brings the command " + brand.Name + " uses."
}

// Open makes the VPN that s names, once its command is here.
func Open(s Settings) (VPN, error) {
	k, ok := KindOf(s.Provider)
	if !ok {
		return nil, fmt.Errorf("vpn.provider = %q: "+brand.Name+" knows %s", s.Provider, strings.Join(Names(), ", "))
	}
	if k.Needs && s.Server == "" {
		return nil, fmt.Errorf("%s needs vpn.server: %s", k.Title, k.Server)
	}
	bin, err := k.Find(s)
	if err != nil {
		return nil, err
	}
	return k.open(bin, s)
}

// osName is this system's name, as people know it.
func osName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	}
	return runtime.GOOS
}

// findCLI is a VPN's command: the configured one, else the first of names
// on the PATH, else the first of paths that is there, else errMissing.
func findCLI(configured string, names, paths []string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("vpn.cli: %v", err)
		}
		return configured, nil
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errMissing
}

// runner runs one VPN's commands, each with a time limit.
type runner struct {
	bin     string
	name    string // the command's name in messages, such as tailscale
	timeout time.Duration
	env     []string // more of the environment, such as a CLI's switch to its command mode
}

func newRunner(bin, name string, timeout time.Duration) runner {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return runner{bin: bin, name: name, timeout: timeout}
}

// run runs the VPN's command with args and returns what it printed, or an
// error that carries what it said when it failed. Its output never goes to
// a log: some VPNs print the machine's addresses or an account.
func (r runner) run(args ...string) (string, error) {
	out, code, err := r.exec(r.timeout, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", r.failed(args, out, code)
	}
	return out, nil
}

// long runs a command that may wait on the network or a sign-in, such as a
// connect, with twice the limit.
func (r runner) long(args ...string) (string, error) {
	out, code, err := r.exec(2*r.timeout, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", r.failed(args, out, code)
	}
	return out, nil
}

// exec runs the command and returns its output and exit code; the error
// says only that it could not run or took too long.
func (r runner) exec(limit time.Duration, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Stdin = nil
	if len(r.env) > 0 {
		cmd.Env = append(os.Environ(), r.env...)
	}
	out, err := cmd.CombinedOutput()
	what := r.name
	if len(args) > 0 {
		what += " " + args[0]
	}
	if ctx.Err() != nil {
		return "", 0, fmt.Errorf("%s took more than %s", what, limit)
	}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return string(out), exit.ExitCode(), nil
	case err != nil:
		return "", 0, fmt.Errorf("%s: %v", what, err)
	}
	return string(out), 0, nil
}

// failed is the error of a command that exited with code: its last lines,
// so a long banner doesn't hide the reason.
func (r runner) failed(args []string, out string, code int) error {
	msg := lastLines(out, 3)
	if msg == "" {
		msg = fmt.Sprintf("exit status %d", code)
	}
	return fmt.Errorf("%s %s: %s", r.name, strings.Join(args, " "), msg)
}

// lastLines is out's last n lines that aren't blank, joined by "; ".
func lastLines(out string, n int) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			keep = append(keep, l)
		}
	}
	if len(keep) > n {
		keep = keep[len(keep)-n:]
	}
	return strings.Join(keep, "; ")
}
