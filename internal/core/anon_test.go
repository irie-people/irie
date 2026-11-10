package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/anon"
	"github.com/irie-people/irie/internal/browser"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/ipc"
)

func TestAnonymousResearchIsOffUntilSetUp(t *testing.T) {
	c := bareCore(t, config.Default())
	if _, _, err := c.prepareAnon("acme", ""); err == nil || !strings.Contains(err.Error(), "irie setup anon") {
		t.Errorf("prepareAnon while off: %v", err)
	}
	if _, err := c.anonOp(ipc.AnonArgs{}); err == nil || !strings.Contains(err.Error(), "irie setup anon") {
		t.Errorf("anon op while off: %v", err)
	}
	cfg := config.Default()
	cfg.Anon.On = true
	c = bareCore(t, cfg)
	if _, _, err := c.prepareAnon(anon.SearchName, ""); err == nil || !strings.Contains(err.Error(), "SearXNG alone") {
		t.Errorf("a task in the search compartment: %v", err)
	}
}

func TestATaskOfItsOwnGetsACompartmentNamedForIt(t *testing.T) {
	c := bareCore(t, config.Default())
	for base, want := range map[string]string{
		"who-runs-example-com":  "who-runs-example-com",
		"Big Scam!":             "big-scam",
		"":                      "research",
		"search":                "research",
		strings.Repeat("x", 50): strings.Repeat("x", 34),
	} {
		if got, err := c.anonName(ipc.AnonNew, base); err != nil || got != want {
			t.Errorf("anonName(%q) = %q, %v; want %q", base, got, err, want)
		}
	}
	now := time.Now()
	if _, err := anon.Take(c.anonStore(), "big-scam", anon.Tor, c.anonOptions(), nil, "", now); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.anonName(ipc.AnonNew, "big-scam"); got != "big-scam-2" {
		t.Errorf("a name taken: %q", got)
	}
	if got, err := c.anonName("acme", "anything"); err != nil || got != "acme" {
		t.Errorf("a named compartment: %q, %v", got, err)
	}
	if _, err := c.anonName("Not OK", ""); err == nil {
		t.Error("a bad name was taken")
	}
}

// TestAnAnonymousSessionGetsItsServersAndNothingElse: the session's MCP
// servers are Irie's alone, WebFetch and WebSearch are refused, its shell
// runs in a sandbox whose only way out is its forwarder, it may not read
// the compartments' exits or the user's sign-ins, and it has no agent
// teams (DESIGN §4.16).
func TestAnAnonymousSessionGetsItsServersAndNothingElse(t *testing.T) {
	cfg := config.Default()
	cfg.Anon.On = true
	cfg.Browser.Path = "/nonexistent/mullvadbrowser" // no browser here
	c := bareCore(t, cfg)
	comp := anon.Compartment{Name: "acme", Route: anon.Mullvad, Relay: "se-got-wg-001"}
	dir := t.TempDir()
	x := &session{id: "s0000000000aa", cwd: "/home/alice/work"}
	s := harness.Spawn{Dir: dir, Teams: true, MCP: map[string]harness.MCPServer{browser.ServerName: {Command: "x"}},
		Environ: []string{"PATH=/usr/bin", "GH_TOKEN=x", "ACME_API_KEY=y", "GIT_AUTHOR_NAME=alice"}}
	if err := c.anonLaunch(&s, x, comp); err != nil {
		t.Fatal(err)
	}
	if len(s.MCP) != 1 || s.MCP[anon.ServerName].Command != c.bin || !s.StrictMCP || s.Teams || x.anon != "acme" {
		t.Fatalf("spawn = %+v, session anon %q", s, x.anon)
	}
	sb := s.Sandbox
	if sb == nil || sb.SOCKSPort < anon.ForwardPort || sb.HTTPPort < anon.ForwardPort || sb.SOCKSPort == sb.HTTPPort || x.anonPorts != [2]int{sb.SOCKSPort, sb.HTTPPort} {
		t.Fatalf("sandbox = %+v, ports %v", sb, x.anonPorts)
	}
	args := strings.Join(s.MCP[anon.ServerName].Args, " ")
	if !strings.Contains(args, "anon mcp --dir "+c.paths.Anon()+" --compartment acme") || !strings.Contains(args, "--evidence /home/alice/work/evidence") || strings.Contains(args, "--search") ||
		!strings.Contains(args, fmt.Sprintf("--socks %d --http %d", sb.SOCKSPort, sb.HTTPPort)) {
		t.Errorf("server args = %q (SearXNG is not installed, so no --search)", args)
	}
	deny := strings.Join(s.Deny, " ")
	if !strings.HasPrefix(deny, "WebFetch WebSearch ") || !strings.Contains(deny, "Read(/"+c.paths.Anon()+"/**)") || !strings.Contains(deny, "Read(~/.ssh/**)") {
		t.Errorf("deny = %v", s.Deny)
	}
	if !slices.Contains(sb.DenyRead, c.paths.Anon()) || !slices.Contains(sb.DenyRead, "~/.ssh") {
		t.Errorf("the sandbox reads %v", sb.DenyRead)
	}
	if !slices.Contains(sb.DenyEnv, "GH_TOKEN") || !slices.Contains(sb.DenyEnv, "ACME_API_KEY") || slices.Contains(sb.DenyEnv, "PATH") || slices.Contains(sb.DenyEnv, "GIT_AUTHOR_NAME") {
		t.Errorf("the sandbox keeps from the shell %v", sb.DenyEnv)
	}
	b, err := os.ReadFile(filepath.Join(dir, harness.AnonEnvFile))
	if err != nil || !strings.Contains(string(b), fmt.Sprintf("export ALL_PROXY='socks5h://127.0.0.1:%d'", sb.SOCKSPort)) ||
		!strings.Contains(string(b), fmt.Sprintf("export HTTPS_PROXY='http://127.0.0.1:%d'", sb.HTTPPort)) || strings.Contains(string(b), "mullvad") || strings.Contains(string(b), "se-got") {
		t.Errorf("%s = %q, %v", harness.AnonEnvFile, b, err)
	}
	// Another session gets ports of its own.
	y := &session{id: "s0000000000ab", cwd: "/home/alice/work"}
	c.sessions = map[string]*session{x.id: x}
	s2 := harness.Spawn{Dir: t.TempDir()}
	if err := c.anonLaunch(&s2, y, comp); err != nil || s2.Sandbox.SOCKSPort == sb.SOCKSPort || s2.Sandbox.HTTPPort == sb.HTTPPort || s2.Sandbox.SOCKSPort == sb.HTTPPort {
		t.Errorf("a second session's ports: %+v, %v", s2.Sandbox, err)
	}
	note := anonNote(x)
	for _, want := range []string{"compartment acme", "WebFetch and WebSearch are off", "sandbox", "Never look up your exit's address", "never act", "identify them", "untrusted"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note lacks %q:\n%s", want, note)
		}
	}
	if anonNote(&session{}) != "" {
		t.Error("a session with no compartment is told of one")
	}
	if w := x.wire(); w.Anon != "acme" {
		t.Errorf("wire = %+v", w)
	}
	raw, _ := json.Marshal(x.wire())
	if strings.Contains(string(raw), "se-got") || strings.Contains(string(raw), strconv.Itoa(sb.SOCKSPort)) {
		t.Errorf("the session's wire names its exit or ports: %s", raw)
	}
}

// TestMoreForAnAnonymousJotStaysInItsCompartment: a jot that names no
// compartment goes on in the last one's, and one that asks for anonymous
// research the last did not, or for another compartment, is a task of its
// own (DESIGN §4.16).
func TestMoreForAnAnonymousJotStaysInItsCompartment(t *testing.T) {
	plain, acme := ipc.SpawnArgs{Task: "x"}, ipc.SpawnArgs{Task: "x", Anon: "acme"}
	for _, c := range []struct {
		name string
		a, b ipc.SpawnArgs
		same bool
	}{
		{"plain after plain", plain, plain, true},
		{"plain after acme", acme, plain, true},
		{"acme after acme", acme, acme, true},
		{"acme after plain", plain, acme, false},
		{"leaks after acme", acme, ipc.SpawnArgs{Task: "x", Anon: "leaks"}, false},
		{"a new one after a new one", ipc.SpawnArgs{Anon: ipc.AnonNew}, ipc.SpawnArgs{Anon: ipc.AnonNew}, false},
		{"acme on tor after acme", acme, ipc.SpawnArgs{Task: "x", Anon: "acme", AnonRoute: "tor"}, false},
	} {
		if got := samePlace(c.a, c.b); got != c.same {
			t.Errorf("%s: samePlace = %v, want %v", c.name, got, c.same)
		}
	}
}

// An anonymous session isn't told to delegate: its sandboxed shell can't
// reach Irie's ctl (DESIGN §4.16).
func TestAnAnonymousSessionIsNotToldToDelegate(t *testing.T) {
	cfg := config.Default()
	cfg.Route.Delegate = true
	c := bareCore(t, cfg)
	x := &session{harness: "claude-code", profile: "claude", out: &outcome{}}
	if c.delegateHint(x) == "" {
		t.Skip("no cheaper profile to delegate to in the defaults")
	}
	x.anon = "acme"
	if h := c.delegateHint(x); h != "" {
		t.Errorf("an anonymous session is told %q", h)
	}
}
