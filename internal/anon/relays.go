package anon

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"runtime"
	"slices"
	"strings"
)

// Relay is a Mullvad WireGuard relay, as Irie picks exits from.
type Relay struct {
	Host     string // such as se-got-wg-001
	CC       string // its country's code, such as se
	Country  string // such as Sweden
	City     string // such as Gothenburg
	Owned    bool   // on a server Mullvad owns, not one it rents
	Provider string // who hosts it
}

// SocksAddr is the SOCKS5 proxy of the relay named host, as host:port.
// Mullvad names each WireGuard relay's proxy after it, se-got-wg-001's
// se-got-wg-socks5-001.relays.mullvad.net on port 1080 (Mullvad's "SOCKS5
// proxy" guide), and its proxies answer only inside its tunnel: an app sent
// through one loses the network when the tunnel drops, rather than leave
// from the Mac's own address.
func SocksAddr(host string) string {
	return strings.Replace(host, "-wg-", "-wg-socks5-", 1) + ".relays.mullvad.net:1080"
}

// SocksName is the name a check of the relay's exit reports, which is its
// proxy's: se-got-wg-socks5-001.
func SocksName(host string) string {
	return strings.Replace(host, "-wg-", "-wg-socks5-", 1)
}

// RelayCaches are where the Mullvad daemon keeps its relay list, by
// GOOS. Reading it asks nothing of the network.
var RelayCaches = map[string]string{
	"darwin": "/Library/Caches/mullvad-vpn/relays.json",
	"linux":  "/var/cache/mullvad-vpn/relays.json",
}

// ErrNoRelays is ReadRelays' error when the daemon's list is not where it
// keeps it.
var ErrNoRelays = errors.New("Mullvad's relay list is not here: is Mullvad VPN installed and logged in?")

// ReadRelays reads the relay list the Mullvad daemon keeps, at path, or
// where it keeps it on this OS when path is "": its active WireGuard
// relays.
func ReadRelays(path string) ([]Relay, error) {
	if path == "" {
		path = RelayCaches[runtime.GOOS]
	}
	if path == "" {
		return nil, ErrNoRelays
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", path, ErrNoRelays)
	}
	if err != nil {
		return nil, err
	}
	rs, err := ParseRelays(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rs, nil
}

// ParseRelays reads the daemon's relay list: {"locations": {"se-got":
// {"city", "country"}}, "wireguard": {"relays": [{"hostname", "active",
// "owned", "location", "provider"}]}}. It keeps the active relays.
func ParseRelays(b []byte) ([]Relay, error) {
	var f struct {
		Locations map[string]struct {
			City    string `json:"city"`
			Country string `json:"country"`
		} `json:"locations"`
		WireGuard struct {
			Relays []struct {
				Hostname string `json:"hostname"`
				Active   bool   `json:"active"`
				Owned    bool   `json:"owned"`
				Location string `json:"location"`
				Provider string `json:"provider"`
			} `json:"relays"`
		} `json:"wireguard"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	var out []Relay
	for _, r := range f.WireGuard.Relays {
		if !r.Active || !strings.Contains(r.Hostname, "-wg-") || !hostRE.MatchString(r.Hostname) {
			continue
		}
		loc := f.Locations[r.Location]
		cc, _, _ := strings.Cut(r.Location, "-")
		out = append(out, Relay{Host: r.Hostname, CC: cc, Country: loc.Country, City: loc.City, Owned: r.Owned, Provider: r.Provider})
	}
	if len(out) == 0 {
		return nil, errors.New("it lists no active WireGuard relay")
	}
	return out, nil
}

var hostRE = nameRE // relay names are lower-case letters, digits and hyphens, as compartments' are

// ErrNoExit is Pick's error when no relay is left to pick.
var ErrNoExit = errors.New("no Mullvad relay is left to give it")

// Pick chooses a relay at random among relays: in countries when some are
// named, on Mullvad's own servers when owned, and none of held. The choice
// is crypto/rand's, so no one can tell which a compartment will get.
func Pick(relays []Relay, countries []string, owned bool, held map[string]bool) (Relay, error) {
	var ok []Relay
	for _, r := range relays {
		if held[r.Host] || (owned && !r.Owned) || (len(countries) > 0 && !slices.Contains(countries, r.CC)) {
			continue
		}
		ok = append(ok, r)
	}
	if len(ok) == 0 {
		why := ""
		if len(countries) > 0 {
			why = " in " + strings.Join(countries, ", ")
		}
		if owned {
			why += " on Mullvad's own servers"
		}
		return Relay{}, fmt.Errorf("%w%s; name more countries in anon.countries, or set anon.owned = false", ErrNoExit, why)
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(ok))))
	if err != nil {
		return Relay{}, err
	}
	return ok[n.Int64()], nil
}
