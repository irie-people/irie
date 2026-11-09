package anon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// Irie's own Tor and SearXNG live in <data>/anon: tor/ holds Tor's config
// and data, searxng/ SearXNG's checkout, its Python and its settings.

// Dirs are the folders of anon's own files under dir, <data>/anon.
type Dirs struct{ Root string }

func (d Dirs) Compartments() string { return filepath.Join(d.Root, "compartments.json") }
func (d Dirs) Tor() string          { return filepath.Join(d.Root, "tor") }
func (d Dirs) Torrc() string        { return filepath.Join(d.Root, "tor", "torrc") }
func (d Dirs) TorPid() string       { return filepath.Join(d.Root, "tor", "tor.pid") }
func (d Dirs) Searx() string        { return filepath.Join(d.Root, "searxng") }
func (d Dirs) SearxSrc() string     { return filepath.Join(d.Root, "searxng", "src") }
func (d Dirs) SearxPython() string  { return filepath.Join(d.Root, "searxng", "venv", "bin", "python") }
func (d Dirs) SearxSettings() string {
	return filepath.Join(d.Root, "searxng", "settings.yml")
}
func (d Dirs) SearxSecret() string { return filepath.Join(d.Root, "searxng", "secret") }

// Torrc is the config of Irie's own Tor: a client only, on 127.0.0.1, with
// a SocksPort for each port in ports, one per Tor compartment. Tor never
// puts streams that came in on different SocksPorts on one circuit, so two
// compartments never share a Tor exit at once (tor(1), "SocksPort").
func Torrc(d Dirs, ports []int) string {
	var b strings.Builder
	b.WriteString("# Irie's own Tor (DESIGN §4.16). Irie writes this file again whenever a Tor\n# compartment is made: edits are lost.\n")
	fmt.Fprintf(&b, "DataDirectory %s\n", filepath.Join(d.Tor(), "data"))
	fmt.Fprintf(&b, "PidFile %s\n", d.TorPid())
	ports = slices.Clone(ports)
	slices.Sort(ports)
	for _, p := range slices.Compact(ports) {
		fmt.Fprintf(&b, "SocksPort 127.0.0.1:%d\n", p)
	}
	b.WriteString("ClientOnly 1\nAvoidDiskWrites 1\nSafeLogging 1\nLog notice stdout\n")
	return b.String()
}

// TorPorts are the SocksPorts of the Tor compartments in all.
func TorPorts(all map[string]Compartment) []int {
	var ports []int
	for _, c := range all {
		if c.Route == Tor && c.TorPort > 0 {
			ports = append(ports, c.TorPort)
		}
	}
	slices.Sort(ports)
	return ports
}

// WriteTorrc writes Tor's config for the Tor compartments in all, and
// reports whether it changed.
func WriteTorrc(d Dirs, all map[string]Compartment) (bool, error) {
	text := Torrc(d, TorPorts(all))
	if old, err := os.ReadFile(d.Torrc()); err == nil && string(old) == text {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Join(d.Tor(), "data"), 0o700); err != nil {
		return false, err
	}
	return true, os.WriteFile(d.Torrc(), []byte(text), 0o600)
}

// ReloadTor asks a running Tor to read its config again, which opens the
// SocksPorts of new compartments and keeps every circuit it has. It
// reports false when no Tor of Irie's runs.
func ReloadTor(d Dirs) bool {
	b, err := os.ReadFile(d.TorPid())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return false
	}
	return syscall.Kill(pid, syscall.SIGHUP) == nil
}

// torPlaces are where Homebrew and Linux packages put tor.
var torPlaces = []string{"/opt/homebrew/bin/tor", "/usr/local/bin/tor", "/usr/bin/tor", "/usr/sbin/tor"}

// ErrNoTor is FindTor's error when tor is nowhere to be found.
var ErrNoTor = errors.New("tor is not installed (irie setup anon installs it)")

// FindTor finds tor: configured, else on the PATH, else where Homebrew and
// Linux packages put it.
func FindTor(configured string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("anon.tor = %q: %w", configured, err)
		}
		return configured, nil
	}
	if p, err := exec.LookPath("tor"); err == nil {
		return p, nil
	}
	for _, p := range torPlaces {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", ErrNoTor
}

// SearXNG, a metasearch engine (AGPL-3.0-or-later, github.com/searxng/
// searxng), is installed from its own repository at a commit pinned here,
// at least two weeks old when it was picked, into a Python of its own. It
// has no releases: its repository is the release.
const (
	SearxRepo   = "https://github.com/searxng/searxng"
	SearxCommit = "629368e40c2aa4edffb05636a8bbc23d2eaa5923" // 2026-10-23
)

// SearxInstalled reports whether SearXNG's checkout and Python are there.
func SearxInstalled(d Dirs) bool {
	if _, err := os.Stat(filepath.Join(d.SearxSrc(), "searx", "webapp.py")); err != nil {
		return false
	}
	_, err := os.Stat(d.SearxPython())
	return err == nil
}

// SearxSteps are the commands that install SearXNG into d: a checkout at
// SearxCommit, a Python of its own made by uv when there is one (which
// fetches a Python 3.12 if need be), else by python3, which must be 3.10 or
// later, and SearXNG's pinned requirements in it.
func SearxSteps(d Dirs, git, uv, python3 string) [][]string {
	src, venv := d.SearxSrc(), filepath.Dir(filepath.Dir(d.SearxPython()))
	steps := [][]string{
		{git, "clone", "--filter=blob:none", "--no-checkout", SearxRepo, src},
		{git, "-C", src, "-c", "advice.detachedHead=false", "checkout", "-q", SearxCommit},
	}
	reqs := filepath.Join(src, "requirements.txt")
	if uv != "" {
		return append(steps,
			[]string{uv, "venv", "--python", "3.12", venv},
			[]string{uv, "pip", "install", "--python", d.SearxPython(), "-r", reqs})
	}
	return append(steps,
		[]string{python3, "-m", "venv", venv},
		[]string{d.SearxPython(), "-m", "pip", "install", "-r", reqs})
}

// SearxSecret is SearXNG's secret key, made once and kept beside its
// settings.
func SearxSecret(d Dirs) (string, error) {
	if b, err := os.ReadFile(d.SearxSecret()); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s := hex.EncodeToString(buf)
	if err := os.MkdirAll(d.Searx(), 0o700); err != nil {
		return "", err
	}
	return s, os.WriteFile(d.SearxSecret(), []byte(s+"\n"), 0o600)
}

// SearxOptions are what SearXNG's settings say.
type SearxOptions struct {
	Port   int
	Secret string
	Proxy  string // where the engines are asked from: the search compartment's exit
	Tor    bool   // the exit is Tor's: SearXNG checks so as it starts, and its onion engines work
}

// SearxSettings are SearXNG's settings over its own defaults (SearXNG's
// settings docs): on 127.0.0.1 only, JSON results on for the agents' search
// tool, no metrics, no autocomplete, images through SearXNG, and every
// engine asked through the search compartment's exit, names looked up
// there. One exit, not a pool: SearXNG would spread requests over several
// proxies to get round the engines' limits, which Irie doesn't do.
func SearxSettings(o SearxOptions) string {
	q := func(s string) string { return strconv.Quote(s) }
	var b strings.Builder
	b.WriteString("# Irie's own SearXNG (DESIGN §4.16). Irie writes this file as it starts SearXNG:\n# edits are lost.\n")
	b.WriteString("use_default_settings: true\n")
	b.WriteString("general:\n  instance_name: \"Irie\"\n  enable_metrics: false\n")
	b.WriteString("search:\n  safe_search: 0\n  autocomplete: \"\"\n  formats:\n    - html\n    - json\n")
	fmt.Fprintf(&b, "server:\n  bind_address: \"127.0.0.1\"\n  port: %d\n  secret_key: %s\n  limiter: false\n  public_instance: false\n  image_proxy: true\n  method: \"POST\"\n", o.Port, q(o.Secret))
	b.WriteString("outgoing:\n  request_timeout: 6.0\n  max_request_timeout: 20.0\n")
	if o.Proxy != "" {
		fmt.Fprintf(&b, "  proxies:\n    all://:\n      - %s\n", q(o.Proxy))
	}
	if o.Tor {
		b.WriteString("  using_tor_proxy: true\n  extra_proxy_timeout: 10.0\n")
	}
	return b.String()
}

// SearxEnv is what SearXNG's environment adds: where its settings are.
func SearxEnv(d Dirs) []string {
	return []string{"SEARXNG_SETTINGS_PATH=" + d.SearxSettings()}
}
