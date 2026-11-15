package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/lock"
	"github.com/irie-people/irie/internal/testutil"
)

// lockOSV is OSV.dev's API as a check uses it: one advisory for each
// Claude Code before fixed, which fixes it, and none from fixed on. It
// keeps the queries it was sent.
type lockOSV struct {
	*httptest.Server
	mu      sync.Mutex
	queries []string
}

func newLockOSV(t *testing.T, fixed string) *lockOSV {
	t.Helper()
	o := &lockOSV{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/querybatch", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		o.mu.Lock()
		o.queries = append(o.queries, string(b))
		o.mu.Unlock()
		var q struct {
			Queries []struct {
				Package struct{ Name, Ecosystem string } `json:"package"`
				Version string                           `json:"version"`
			} `json:"queries"`
		}
		json.Unmarshal(b, &q)
		var out struct {
			Results []map[string]any `json:"results"`
		}
		for _, x := range q.Queries {
			r := map[string]any{}
			if x.Package.Name == lock.ClaudeCode && lock.Compare(x.Version, fixed) < 0 {
				r["vulns"] = []map[string]string{{"id": "GHSA-test-0001", "modified": "2026-11-09T23:00:03Z"}}
			}
			out.Results = append(out.Results, r)
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /v1/vulns/GHSA-test-0001", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"id": "GHSA-test-0001", "summary": "Claude Code: the Edit tool can write outside the workspace", "modified": "2026-11-09T23:00:03Z",
  "affected": [{"package": {"ecosystem": "npm", "name": %q}, "ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}, {"fixed": %q}]}]}]}`, lock.ClaudeCode, fixed)
	})
	o.Server = httptest.NewServer(mux)
	t.Cleanup(o.Close)
	return o
}

func (o *lockOSV) asked() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.queries...)
}

// A spawn locks its session: the fake harness's version and package, its
// profile, model and permission mode, Irie's hooks and a fake MCP server
// of the user's own config with its package. A check against a stand-in
// for OSV marks the session whose version has an advisory, and not the
// one that runs the version that fixes it.
func TestLockIsWrittenAtSpawnAndACheckMarksItsRow(t *testing.T) {
	fake := testutil.BuildFakeAgent(t)
	claudeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(claudeDir, ".claude.json"), []byte(`{"mcpServers": {"fake": {"command": "npx", "args": ["-y", "@example/fake-mcp@1.2.3"], "env": {"FAKE_KEY": "secret"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	osv := newLockOSV(t, "2.1.301")
	c := setupWith(t, agentConfig{
		env: map[string]string{"CLAUDE_CONFIG_DIR": claudeDir},
		extra: fmt.Sprintf("[profiles.newer]\ncommand = %q\nmodel = \"claude-sonnet-5-5\"\nenv = { CLAUDE_CONFIG_DIR = %q, IRIE_FAKE_VERSION = \"2.1.301\" }\n\n[lock]\nosv = %q\n",
			fake, claudeDir, osv.URL),
	})
	older := c.spawn("look at the parser")
	var r ipc.SpawnResult
	if err := c.call("spawn", ipc.SpawnArgs{Task: "look at the lexer", Profile: "newer"}, &r); err != nil {
		t.Fatal(err)
	}
	newer := r.Session
	read := func(id string) ipc.Lock {
		var l ipc.Lock
		testutil.Eventually(t, 15*time.Second, "no lock.json for "+id, func() bool {
			var err error
			l, err = lock.Read(c.paths.Session(id))
			return err == nil
		})
		return l
	}
	l := read(older.ID)
	if l.At != ipc.LockSpawn || l.Profile != "fake" || l.Model != "claude-opus-5-5" || l.PermissionMode != "auto" {
		t.Errorf("lock: at %q, profile %q, model %q, mode %q", l.At, l.Profile, l.Model, l.PermissionMode)
	}
	if h := l.Harness; h.Name != "claude-code" || h.Version != "2.1.300" || h.Package == nil || h.Package.Name != lock.ClaudeCode || h.Command == "" {
		t.Errorf("harness = %+v", h)
	}
	if len(l.MCP) != 1 || l.MCP[0].Name != "fake" || l.MCP[0].From != ipc.LockFromUser || l.MCP[0].Package == nil || l.MCP[0].Package.Version != "1.2.3" {
		t.Errorf("servers = %+v", l.MCP)
	}
	if len(l.Hooks) == 0 || l.Hooks[0].From != ipc.LockFromIrie || len(l.Hooks[0].Events) == 0 || !strings.HasPrefix(l.Hooks[0].Hash, "sha256:") {
		t.Errorf("hooks = %+v", l.Hooks)
	}
	if !strings.HasPrefix(l.Irie.Hash, "sha256:") {
		t.Errorf("irie = %+v", l.Irie)
	}
	if b, _ := os.ReadFile(lock.Path(c.paths.Session(older.ID))); strings.Contains(string(b), "secret") {
		t.Errorf("the lock holds the server's secret:\n%s", b)
	}
	if l := read(newer.ID); l.Harness.Version != "2.1.301" || l.Model != "claude-sonnet-5-5" {
		t.Errorf("the newer session's lock: %+v", l.Harness)
	}
	row := func(id string) *ipc.LockRow {
		for _, s := range c.list() {
			if s.ID == id {
				return s.Lock
			}
		}
		return nil
	}
	testutil.Eventually(t, 5*time.Second, "the rows never named their locks' packages", func() bool {
		return row(older.ID) != nil && row(newer.ID) != nil
	})
	if l := row(older.ID); len(l.Packages) != 2 || l.Packages[0].Checked || l.Words != "" {
		t.Errorf("before a check, the row's lock = %+v", l)
	}
	if n := len(osv.asked()); n != 0 {
		t.Fatalf("OSV.dev was asked %d times before a check", n)
	}

	var res ipc.LockResult
	if err := c.call(ipc.OpLock, ipc.LockArgs{Check: true}, &res); err != nil {
		t.Fatal(err)
	}
	if !res.Asked || res.Checked.IsZero() || res.OSV != osv.URL || len(res.Sessions) != 2 {
		t.Errorf("result: %+v", res)
	}
	hits := map[string]int{}
	for _, f := range res.Packages {
		hits[f.Name+"@"+f.Version] = len(f.Advisories)
	}
	if want := map[string]int{lock.ClaudeCode + "@2.1.300": 1, lock.ClaudeCode + "@2.1.301": 0, "@example/fake-mcp@1.2.3": 0}; fmt.Sprint(hits) != fmt.Sprint(want) {
		t.Errorf("found %v, want %v", hits, want)
	}
	if q := osv.asked(); len(q) != 1 || strings.Contains(q[0], older.ID) || strings.Contains(q[0], "parser") || strings.Contains(q[0], claudeDir) {
		t.Errorf("OSV.dev was sent %q", q)
	}
	if l := row(older.ID); l == nil || l.Advisories != 1 || l.Words != "Claude Code 2.1.300: 1 advisory, fixed in 2.1.301" {
		t.Errorf("the older session's row: %+v", l)
	}
	if l := row(newer.ID); l == nil || l.Advisories != 0 || l.Words != "" || !l.Packages[0].Checked {
		t.Errorf("the newer session's row: %+v", l)
	}
	if _, err := os.Stat(filepath.Join(c.paths.State, lock.ResultsFile)); err != nil {
		t.Errorf("what the check found was not kept: %v", err)
	}
	// One session's lock, by its name.
	if err := c.call(ipc.OpLock, ipc.LockArgs{Session: newer.Name}, &res); err != nil || len(res.Sessions) != 1 || res.Sessions[0].ID != newer.ID || res.Sessions[0].Lock == nil {
		t.Errorf("one session: %+v, %v", res, err)
	}
	if err := c.call(ipc.OpLock, ipc.LockArgs{Session: "no such session"}, &res); err == nil {
		t.Error("a session that is not there has a lock")
	}
}

// What a check found outlasts a restart, and marks a peer's rows too: the
// versions this core's check knows by what it found, the others by what
// the peer's own check found.
func TestLockMarksOutlastARestartAndReachAPeersRows(t *testing.T) {
	c := newPeerCore(t)
	now := time.Now()
	found := []ipc.LockFound{{Ecosystem: lock.NPM, Name: lock.ClaudeCode, Version: "2.1.300", Checked: now,
		Advisories: []ipc.LockAdvisory{{ID: "GHSA-test-0001", Fixed: "2.1.301"}}}}
	if err := (lock.Results{}).Merge(found, lock.OSV, now).Save(c.lockResultsFile()); err != nil {
		t.Fatal(err)
	}
	c.loadLocks()
	claudeAt := func(v string) ipc.LockPackage {
		return ipc.LockPackage{Ecosystem: lock.NPM, Name: lock.ClaudeCode, Version: v, Part: "Claude Code"}
	}
	x := &session{id: "s0000000000aa", name: "parser", harness: "claude-code", hue: 1, lockPkgs: []ipc.LockPackage{claudeAt("2.1.300")}}
	c.mu.Lock()
	c.sessions[x.id] = x
	s := c.row(x, time.Time{})
	c.mu.Unlock()
	if s.Lock == nil || s.Lock.Words != "Claude Code 2.1.300: 1 advisory, fixed in 2.1.301" {
		t.Errorf("row after a restart: %+v", s.Lock)
	}

	theirs := ipc.LockPackage{Ecosystem: lock.NPM, Name: "fake-server", Version: "0.6.2", Part: "MCP server loose", Checked: true, Advisories: 1, Unfixed: 1}
	p := &peerState{name: "Asahi", up: true, stop: func() {}, rows: map[string]ipc.Session{
		"s1": {ID: "s1", Name: "night work", Lock: &ipc.LockRow{Packages: []ipc.LockPackage{claudeAt("2.1.300"), theirs}}},
		"s2": {ID: "s2", Name: "day work", Order: 1, Lock: &ipc.LockRow{Packages: []ipc.LockPackage{claudeAt("2.1.301")}}},
	}}
	c.mu.Lock()
	c.peers = map[string]*peerState{"Asahi": p}
	rows := c.peerRows(0)
	pkgs, names := c.lockPackages()
	c.mu.Unlock()
	words := map[string]string{}
	for _, r := range rows {
		if r.Lock != nil {
			words[r.ID] = r.Lock.Words
		}
	}
	if want := "Claude Code 2.1.300: 1 advisory, fixed in 2.1.301 · MCP server loose 0.6.2: 1 advisory, no fix yet"; words["Asahi/s1"] != want {
		t.Errorf("the peer's row says %q, want %q", words["Asahi/s1"], want)
	}
	if words["Asahi/s2"] != "" {
		t.Errorf("the peer's row on the fixed version says %q", words["Asahi/s2"])
	}
	// A check asks about the peer's versions as well as this machine's.
	var keys []string
	for _, p := range pkgs {
		keys = append(keys, p.Name+"@"+p.Version)
	}
	if want := []string{lock.ClaudeCode + "@2.1.300", "fake-server@0.6.2", lock.ClaudeCode + "@2.1.301"}; fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Errorf("a check asks about %v, want %v", keys, want)
	}
	if got := names[lock.Key(claudeAt("2.1.300"))]; fmt.Sprint(got) != "[parser night work]" {
		t.Errorf("2.1.300 runs in %v", got)
	}
}

// With lock.check = "daily" the core asks OSV.dev once a day, and not at
// all while it is off.
func TestLockChecksOnceADayWhenAskedTo(t *testing.T) {
	osv := newLockOSV(t, "2.1.301")
	c := newPeerCore(t)
	c.cfg().Lock.OSV = osv.URL
	x := &session{id: "s0000000000aa", name: "parser", harness: "claude-code", hue: 1,
		lockPkgs: []ipc.LockPackage{{Ecosystem: lock.NPM, Name: lock.ClaudeCode, Version: "2.1.300", Part: "Claude Code"}}}
	c.sessions[x.id] = x
	c.lockSweep()
	if n := len(osv.asked()); n != 0 {
		t.Fatalf("lock.check off asked OSV.dev %d times", n)
	}
	c.cfg().Lock.Check = config.LockCheckDaily
	c.lockSweep()
	c.lockSweep()
	if n := len(osv.asked()); n != 1 {
		t.Fatalf("asked OSV.dev %d times, want once", n)
	}
	c.mu.Lock()
	s := c.row(x, time.Time{})
	c.locks.results.Checked = time.Now().Add(-25 * time.Hour)
	c.mu.Unlock()
	if s.Lock == nil || s.Lock.Advisories != 1 {
		t.Errorf("row %+v", s.Lock)
	}
	c.lockSweep()
	if n := len(osv.asked()); n != 2 {
		t.Errorf("a day later, asked OSV.dev %d times in all, want twice", n)
	}
}
