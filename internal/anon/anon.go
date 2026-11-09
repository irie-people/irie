// Package anon is Irie's anonymous research (DESIGN §4.16). A compartment
// is one line of inquiry: it has an exit of its own, a Mullvad relay's
// SOCKS5 proxy or a Tor port of its own, that nothing else of the user's
// uses, and keeps it for as long as it lives. The package picks exits,
// keeps compartments, checks where an exit leaves from, writes the
// settings of Irie's own Tor and SearXNG, and serves the MCP server that
// gives an agent search and fetch through its compartment's exit.
//
// Nothing here prints an exit's address. A compartment's relay is named
// only to the user, never to an agent: an agent's conversation goes to its
// model's maker under the user's account.
package anon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/filetx"
)

// The routes a compartment leaves by.
const (
	Mullvad = "mullvad" // a Mullvad relay's SOCKS5 proxy, inside the Mac's own Mullvad tunnel
	Tor     = "tor"     // Irie's own Tor, on a SocksPort of the compartment's own
)

// Routes are the routes, in the order the config names them.
var Routes = []string{Mullvad, Tor}

// SearchName is the compartment of Irie's SearXNG: the engines see its exit,
// never a research compartment's or the Mac's own.
const SearchName = "search"

// Compartment is one line of inquiry and its exit.
type Compartment struct {
	Name    string    `json:"name"`
	Route   string    `json:"route"`
	Relay   string    `json:"relay,omitempty"`    // a Mullvad compartment's relay, such as se-got-wg-001
	Country string    `json:"country,omitempty"`  // the relay's country
	City    string    `json:"city,omitempty"`     // and city
	TorPort int       `json:"tor_port,omitempty"` // a Tor compartment's own SocksPort on 127.0.0.1
	Created time.Time `json:"created"`
	// Moved is when the compartment last took another relay, because
	// Mullvad had taken its own out of service. Irie never moves one for
	// any other reason: not for a site that blocks it, nor one that slows
	// it down.
	Moved time.Time `json:"moved,omitzero"`
}

// Proxy is the compartment's exit as a proxy address, socks5h://host:port:
// the h has names looked up at the exit, never on the Mac.
func (c Compartment) Proxy() string {
	if c.Route == Tor {
		return "socks5h://127.0.0.1:" + strconv.Itoa(c.TorPort)
	}
	return "socks5h://" + SocksAddr(c.Relay)
}

// Where says where the compartment leaves from, in words that name no
// address or relay: what an agent may be told.
func (c Compartment) Where() string {
	if c.Route == Tor {
		return "Tor, on circuits of its own"
	}
	at := c.Country
	if c.City != "" && c.Country != "" {
		at = c.City + ", " + c.Country
	}
	if at == "" {
		return "a Mullvad exit of its own"
	}
	return "a Mullvad exit of its own in " + at
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// CheckName checks a compartment's name: lower-case letters, digits and
// hyphens, at most 40, starting with a letter or digit.
func CheckName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("%q is not a compartment's name: lower-case letters, digits and -, at most 40", name)
	}
	return nil
}

// Store keeps the compartments, in a file of the user's alone.
type Store struct {
	Path string // <data>/anon/compartments.json
}

type storeFile struct {
	Compartments map[string]Compartment `json:"compartments"`
}

// Load reads every compartment, by name; none when there is no file. A
// reader takes no lock: writes replace the file whole.
func (s Store) Load() (map[string]Compartment, error) {
	var f storeFile
	b, err := os.ReadFile(s.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", s.Path, err)
		}
	}
	if f.Compartments == nil {
		f.Compartments = map[string]Compartment{}
	}
	return f.Compartments, nil
}

// Update changes the compartments under the file's write lock, so the
// core and the user's own commands take turns.
func (s Store) Update(change func(map[string]Compartment) error) error {
	return filetx.Update(s.Path, 0o600, func(old []byte) ([]byte, error) {
		var f storeFile
		if len(old) > 0 {
			if err := json.Unmarshal(old, &f); err != nil {
				return nil, fmt.Errorf("%s: %w", s.Path, err)
			}
		}
		if f.Compartments == nil {
			f.Compartments = map[string]Compartment{}
		}
		if err := change(f.Compartments); err != nil {
			return nil, err
		}
		b, err := json.MarshalIndent(f, "", "  ")
		return append(b, '\n'), err
	})
}

// Options are how a new compartment gets its exit.
type Options struct {
	Route     string   // the route of a compartment made without one named
	Countries []string // Mullvad exits only in these countries, by code such as se; none = any
	Owned     bool     // Mullvad exits only on servers Mullvad owns, not rents
	TorPort   int      // the first of the Tor compartments' SocksPorts
}

// ErrRouteDiffers is Take's error when a compartment is asked for on
// another route than the one it has.
var ErrRouteDiffers = errors.New("the compartment leaves by another route")

// Taken is what Take did.
type Taken struct {
	Compartment
	Made  bool // the compartment is new
	Moved bool // it took another relay, Mullvad having taken its own out of service
}

// Take returns the compartment named name, on route ("" = its own, or
// o.Route for a new one). A new one gets an exit of its own: a relay from
// relays that no other compartment holds and that is not avoid, the relay
// the Mac's own tunnel leaves from; or the next free Tor port. A Mullvad
// compartment whose relay is no longer among relays takes another.
// relays may be nil when only Tor compartments are asked for.
func Take(s Store, name, route string, o Options, relays []Relay, avoid string, now time.Time) (Taken, error) {
	if err := CheckName(name); err != nil {
		return Taken{}, err
	}
	if route != "" && !slices.Contains(Routes, route) {
		return Taken{}, fmt.Errorf("route %q: must be mullvad or tor", route)
	}
	var out Taken
	err := s.Update(func(all map[string]Compartment) error {
		c, ok := all[name]
		if ok && route != "" && c.Route != route {
			return fmt.Errorf("%s leaves by %s: %w", name, c.Route, ErrRouteDiffers)
		}
		if !ok {
			c = Compartment{Name: name, Route: route, Created: now.UTC()}
			if c.Route == "" {
				c.Route = o.Route
			}
			if c.Route == "" {
				c.Route = Mullvad
			}
			out.Made = true
		}
		switch c.Route {
		case Tor:
			if c.TorPort == 0 {
				c.TorPort = freePort(all, o.TorPort)
			}
		case Mullvad:
			if c.Relay != "" && slices.ContainsFunc(relays, func(r Relay) bool { return r.Host == c.Relay }) {
				break
			}
			if c.Relay != "" && relays == nil {
				break // nothing to tell a relay gone by
			}
			held := map[string]bool{}
			for _, other := range all {
				if other.Name != name && other.Relay != "" {
					held[other.Relay] = true
				}
			}
			if avoid != "" {
				held[avoid] = true
			}
			r, err := Pick(relays, o.Countries, o.Owned, held)
			if err != nil {
				return err
			}
			if c.Relay != "" {
				c.Moved, out.Moved = now.UTC(), true
			}
			c.Relay, c.Country, c.City = r.Host, r.Country, r.City
		default:
			return fmt.Errorf("%s has route %q, which Irie does not know", name, c.Route)
		}
		all[name] = c
		out.Compartment = c
		return nil
	})
	if err != nil {
		return Taken{}, err
	}
	return out, nil
}

// freePort is the first port from first up that no Tor compartment holds.
func freePort(all map[string]Compartment, first int) int {
	used := map[int]bool{}
	for _, c := range all {
		used[c.TorPort] = true
	}
	p := first
	for used[p] {
		p++
	}
	return p
}

// Forget drops the compartment named name, which lets its exit go. It
// reports whether there was one.
func Forget(s Store, name string) (bool, error) {
	found := false
	err := s.Update(func(all map[string]Compartment) error {
		_, found = all[name]
		delete(all, name)
		return nil
	})
	return found, err
}

// Sorted is the compartments in order of their names.
func Sorted(all map[string]Compartment) []Compartment {
	var out []Compartment
	for _, k := range slices.Sorted(maps.Keys(all)) {
		out = append(out, all[k])
	}
	return out
}

// ProxyEnv are the variables that send a shell's programs through proxy:
// those curl, Go, Python's requests and most others read, in both cases,
// with the machine's own addresses left out so local tools still answer.
// A program that ignores them leaves from the Mac's own tunnel.
func ProxyEnv(proxy string) []string {
	var env []string
	for _, k := range []string{"ALL_PROXY", "HTTPS_PROXY", "HTTP_PROXY"} {
		env = append(env, k+"="+proxy)
	}
	for _, k := range []string{"all_proxy", "https_proxy", "http_proxy"} {
		env = append(env, k+"="+proxy)
	}
	return append(env, "NO_PROXY=localhost,127.0.0.1,::1", "no_proxy=localhost,127.0.0.1,::1")
}

// Exports are ProxyEnv as lines of a shell's export, for Claude Code's
// CLAUDE_ENV_FILE, which runs before each Bash command, with IRIE_ANON
// naming the compartment.
func Exports(c Compartment) string {
	var b []byte
	for _, kv := range append(ProxyEnv(c.Proxy()), "IRIE_ANON="+c.Name) {
		k, v, _ := strings.Cut(kv, "=")
		b = fmt.Appendf(b, "export %s=%s\n", k, shellQuote(v))
	}
	return string(b)
}

func shellQuote(s string) string {
	out := []byte{'\''}
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, `'\''`...)
			continue
		}
		out = append(out, s[i])
	}
	return string(append(out, '\''))
}
