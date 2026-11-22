package lock

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSpecReadsNpxAndUvx(t *testing.T) {
	for _, c := range []struct {
		cmd     string
		args    []string
		want    *ipc.LockPackage
		written string
	}{
		{"npx", []string{"-y", "@example/fake-mcp@1.2.3", "--token", "x"}, &ipc.LockPackage{Ecosystem: NPM, Name: "@example/fake-mcp", Version: "1.2.3"}, "@example/fake-mcp@1.2.3"},
		{"/usr/local/bin/npx", []string{"--yes", "fake-server@0.6.2"}, &ipc.LockPackage{Ecosystem: NPM, Name: "fake-server", Version: "0.6.2"}, "fake-server@0.6.2"},
		{"npx", []string{"--package=fake-server@2.0.0-rc.1", "--", "fake"}, &ipc.LockPackage{Ecosystem: NPM, Name: "fake-server", Version: "2.0.0-rc.1"}, "fake-server@2.0.0-rc.1"},
		{"npx", []string{"-p", "fake-server@1.0.0", "fake"}, &ipc.LockPackage{Ecosystem: NPM, Name: "fake-server", Version: "1.0.0"}, "fake-server@1.0.0"},
		{"npm", []string{"exec", "-y", "--", "fake-server@1.0.1"}, &ipc.LockPackage{Ecosystem: NPM, Name: "fake-server", Version: "1.0.1"}, "fake-server@1.0.1"},
		// Unpinned: npx runs the newest, so no version is known.
		{"npx", []string{"-y", "@example/fake-mcp"}, nil, "@example/fake-mcp"},
		{"npx", []string{"-y", "fake-server@latest"}, nil, "fake-server@latest"},
		{"npx", []string{"fake-server@^1.2.0"}, nil, "fake-server@^1.2.0"},
		{"npx", []string{"fake-server@1"}, nil, "fake-server@1"},
		{"uvx", []string{"fake-tools==0.4"}, &ipc.LockPackage{Ecosystem: PyPI, Name: "fake-tools", Version: "0.4"}, "fake-tools==0.4"},
		{"uvx", []string{"fake-tools@0.4.1", "serve"}, &ipc.LockPackage{Ecosystem: PyPI, Name: "fake-tools", Version: "0.4.1"}, "fake-tools@0.4.1"},
		{"uvx", []string{"--from", "fake-tools[extra]==1.2rc1", "fake"}, &ipc.LockPackage{Ecosystem: PyPI, Name: "fake-tools", Version: "1.2rc1"}, "fake-tools[extra]==1.2rc1"},
		{"uv", []string{"tool", "run", "--python", "3.12", "fake-tools==2"}, &ipc.LockPackage{Ecosystem: PyPI, Name: "fake-tools", Version: "2"}, "fake-tools==2"},
		{"uvx", []string{"--from", "fake-tools>0.2,<0.3", "fake"}, nil, "fake-tools>0.2,<0.3"},
		{"uvx", []string{"--from", "git+https://example.com/fake@1.0", "fake"}, nil, ""},
		{"node", []string{"/home/alice/server.js"}, nil, ""},
		{"docker", []string{"run", "-i", "example/fake:1.0"}, nil, ""},
	} {
		got, written := Spec(c.cmd, c.args)
		if !reflect.DeepEqual(got, c.want) || written != c.written {
			t.Errorf("Spec(%s %v) = %+v, %q; want %+v, %q", c.cmd, c.args, got, written, c.want, c.written)
		}
	}
}

func TestCompareOrdersReleases(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"2.1.250", "2.1.251", -1},
		{"2.1.251", "2.1.250", 1},
		{"2.1.84", "2.1.163", -1},
		{"2.1.0", "2.1", 0},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.2rc1", "1.2", -1},
		{"1.2.dev1", "1.2a1", -1},
		{"1.2.post1", "1.2", 1},
		{"v3.0.0", "3.0.0", 0},
		{"1.0.0+build.5", "1.0.0", 0},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// A lock written for a fake harness and a fake MCP server: the harness's
// version and package, the profile, model and permission mode, each server
// with its package or Irie's version, Irie's plugins and skills with
// hashes, and the hooks; none of the servers' secrets.
func TestGatherLocksAFakeHarnessAndServer(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "src", "app")
	claudeDir := filepath.Join(home, ".claude")
	bin := filepath.Join(home, "bin", "claude")
	write(t, bin, "#!/bin/sh\necho 2.1.250\n")
	irie := filepath.Join(home, "bin", brand.Slug)
	write(t, irie, "irie\n")
	// Claude Code's config: a server for every folder, with a secret in
	// its arguments, and one for the session's folder alone.
	write(t, filepath.Join(home, ".claude.json"), `{
  "numStartups": 3,
  "mcpServers": {
    "fake": {"command": "npx", "args": ["-y", "@example/fake-mcp@1.2.3", "--token", "SECRET-ARG"], "env": {"FAKE_KEY": "SECRET-ENV"}},
    "loose": {"command": "npx", "args": ["-y", "fake-server"]}
  },
  "projects": {`+jsonString(cwd)+`: {"mcpServers": {"tools": {"command": "uvx", "args": ["fake-tools==0.4"]}}}}
}`)
	// The project's own servers: one of a name the user's own config has
	// too, which the project's takes, and an address with a secret.
	write(t, filepath.Join(cwd, ".mcp.json"), `{"mcpServers": {
  "loose": {"command": "npx", "args": ["fake-server@0.6.2"]},
  "remote": {"type": "http", "url": "https://user:SECRET-PASS@mcp.example.com/v1?key=SECRET-QUERY"}
}}`)
	write(t, filepath.Join(cwd, ".git", "HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(claudeDir, "settings.json"), `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "GUARD=1 $HOME/bin/guard.sh --key SECRET-HOOK"}]}]}}`)
	plugin := filepath.Join(home, "skills", "plugins", "0123")
	write(t, filepath.Join(plugin, "skills", "present", "SKILL.md"), "---\nname: present\n---\nPresent.\n")
	settings := `{"hooks": {"PreToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": "'` + irie + `' hook"}]}], "Stop": [{"hooks": [{"type": "command", "command": "'` + irie + `' hook"}]}]}}`

	in := Input{
		Session: "s0123456789ab", At: ipc.LockSpawn, Now: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC),
		Irie: "1.5.0+abcdef012345", IrieBin: irie,
		Harness: "claude-code", Binary: bin, Version: "2.1.250", Profile: "fake", Model: "claude-opus-5-5",
		Argv:     []string{"claude", "--model", "claude-opus-5-5", "--permission-mode", "acceptEdits", "--plugin-dir", plugin, "--settings", "x"},
		Settings: []byte(settings),
		MCP:      []Server{{Name: "irie-browser", From: ipc.LockFromIrie, Command: irie, Args: []string{"browser", "mcp"}}},
		Plugins:  []Plugin{{Name: "writing"}, {Name: "crm", Manifest: filepath.Join(home, "plugins", "crm.toml")}},
		Home:     home, ClaudeDir: claudeDir, Cwd: cwd,
	}
	write(t, in.Plugins[1].Manifest, "title = \"CRM\"\n")
	l := Gather(in)

	// Its binary is where any link leads: a native install's version.
	real, _ := filepath.EvalSymlinks(bin)
	if l.Harness.Name != "claude-code" || l.Harness.Version != "2.1.250" || l.Harness.Command != real ||
		!reflect.DeepEqual(l.Harness.Package, &ipc.LockPackage{Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.250"}) {
		t.Errorf("harness = %+v", l.Harness)
	}
	if l.Profile != "fake" || l.Model != "claude-opus-5-5" || l.PermissionMode != "acceptEdits" || l.At != ipc.LockSpawn {
		t.Errorf("profile, model, mode, at = %q, %q, %q, %q", l.Profile, l.Model, l.PermissionMode, l.At)
	}
	irieHash, _ := FileHash(irie)
	if l.Irie.Version != in.Irie || l.Irie.Hash != irieHash || !strings.HasPrefix(irieHash, "sha256:") {
		t.Errorf("irie = %+v", l.Irie)
	}
	servers := map[string]ipc.LockPart{}
	var names []string
	for _, s := range l.MCP {
		servers[s.Name] = s
		names = append(names, s.Name)
	}
	if want := []string{"irie-browser", "tools", "loose", "remote", "fake"}; !reflect.DeepEqual(names, want) {
		t.Errorf("servers %v, want %v: Irie's first, then the folder's, the project's and the user's", names, want)
	}
	if s := servers["irie-browser"]; s.From != ipc.LockFromIrie || s.Version != in.Irie || s.Command != brand.Slug+" browser mcp" || s.Package != nil {
		t.Errorf("Irie's server = %+v", s)
	}
	if s := servers["fake"]; s.From != ipc.LockFromUser || s.Command != "npx @example/fake-mcp@1.2.3" || s.Version != "1.2.3" ||
		!reflect.DeepEqual(s.Package, &ipc.LockPackage{Ecosystem: NPM, Name: "@example/fake-mcp", Version: "1.2.3"}) || !strings.HasPrefix(s.Hash, "sha256:") {
		t.Errorf("the fake server = %+v", s)
	}
	if s := servers["tools"]; s.From != ipc.LockFromLocal || s.Package == nil || s.Package.Ecosystem != PyPI || s.Package.Version != "0.4" {
		t.Errorf("the folder's server = %+v", s)
	}
	if s := servers["loose"]; s.From != ipc.LockFromProject || s.Version != "0.6.2" {
		t.Errorf("the project's server, which takes the name from the user's = %+v", s)
	}
	if s := servers["remote"]; s.Command != "http https://mcp.example.com" || s.Package != nil {
		t.Errorf("the address = %+v", s)
	}
	if len(l.Plugins) != 2 || !reflect.DeepEqual(l.Plugins[0], ipc.LockPart{Name: "writing", From: ipc.LockFromIrie, Version: in.Irie, Hash: irieHash}) ||
		l.Plugins[1].From != ipc.LockFromUser || !strings.HasPrefix(l.Plugins[1].Hash, "sha256:") || l.Plugins[1].Hash == irieHash {
		t.Errorf("plugins = %+v", l.Plugins)
	}
	if len(l.Skills) != 1 || l.Skills[0].Name != brand.Slug+":present" || l.Skills[0].From != ipc.LockFromIrie || !strings.HasPrefix(l.Skills[0].Hash, "sha256:") {
		t.Errorf("skills = %+v", l.Skills)
	}
	if len(l.Hooks) != 2 || l.Hooks[0].From != ipc.LockFromIrie || !reflect.DeepEqual(l.Hooks[0].Events, []string{"PreToolUse", "Stop"}) ||
		l.Hooks[1].Name != "PreToolUse Bash" || l.Hooks[1].From != ipc.LockFromUser || l.Hooks[1].Command != "guard.sh" {
		t.Errorf("hooks = %+v", l.Hooks)
	}
	if len(l.Notes) > 0 {
		t.Errorf("notes: %v", l.Notes)
	}

	// The file holds none of the secrets, and reads back the same.
	dir := filepath.Join(home, "state", "sessions", in.Session)
	if err := Write(dir, l); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SECRET-ARG", "SECRET-ENV", "SECRET-PASS", "SECRET-QUERY", "SECRET-HOOK", "FAKE_KEY"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("lock.json holds %s:\n%s", secret, b)
		}
	}
	back, err := Read(dir)
	if err != nil || !reflect.DeepEqual(back, l) {
		t.Errorf("read back %+v, %v", back, err)
	}
	if _, err := Read(t.TempDir()); err != ErrNone {
		t.Errorf("a folder with no lock: %v, want ErrNone", err)
	}

	// A check asks about the harness and the servers that name a version.
	want := []ipc.LockPackage{
		{Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.250", Part: "Claude Code"},
		{Ecosystem: PyPI, Name: "fake-tools", Version: "0.4", Part: "MCP server tools"},
		{Ecosystem: NPM, Name: "fake-server", Version: "0.6.2", Part: "MCP server loose"},
		{Ecosystem: NPM, Name: "@example/fake-mcp", Version: "1.2.3", Part: "MCP server fake"},
	}
	if got := Packages(l); !reflect.DeepEqual(got, want) {
		t.Errorf("packages = %+v\nwant %+v", got, want)
	}

	// An anonymous session loads Irie's servers alone; another harness
	// reads none of Claude Code's config.
	in.Strict = true
	if l := Gather(in); len(l.MCP) != 1 || l.MCP[0].Name != "irie-browser" {
		t.Errorf("strict: servers %+v", l.MCP)
	}
	in.Strict, in.Harness, in.Version = false, "generic", ""
	if l := Gather(in); len(l.MCP) != 1 || l.Hooks != nil || l.Skills != nil || l.Harness.Package != nil {
		t.Errorf("generic: %+v", l)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// osvStandIn is OSV.dev's API as a check uses it: an advisory for
// Claude Code 2.1.250, fixed in 2.1.251, and none for 2.1.251. It keeps
// each query it was sent, and counts the advisories it was asked for.
type osvStandIn struct {
	*httptest.Server
	queries [][]byte
	reads   atomic.Int32
}

func newOSV(t *testing.T) *osvStandIn {
	o := &osvStandIn{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/querybatch", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		o.queries = append(o.queries, b)
		var q struct {
			Queries []struct {
				Package struct{ Name, Ecosystem string } `json:"package"`
				Version string                           `json:"version"`
			} `json:"queries"`
		}
		if json.Unmarshal(b, &q) != nil {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		type result struct {
			Vulns []map[string]string `json:"vulns,omitempty"`
		}
		var out struct {
			Results []result `json:"results"`
		}
		for _, x := range q.Queries {
			var r result
			if x.Package.Ecosystem == "npm" && x.Package.Name == ClaudeCode && Compare(x.Version, "2.1.251") < 0 {
				r.Vulns = []map[string]string{{"id": "GHSA-test-0001", "modified": "2026-11-09T23:00:03.851745Z"}}
			}
			out.Results = append(out.Results, r)
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /v1/vulns/{id}", func(w http.ResponseWriter, r *http.Request) {
		o.reads.Add(1)
		if r.PathValue("id") != "GHSA-test-0001" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"id": "GHSA-test-0001", "summary": "Claude Code: the Edit tool can write outside the workspace",
  "modified": "2026-11-09T23:00:03.851745Z",
  "affected": [{"package": {"ecosystem": "npm", "name": "@anthropic-ai/claude-code"},
    "ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}, {"fixed": "2.1.251"}]}]}]}`)
	})
	o.Server = httptest.NewServer(mux)
	t.Cleanup(o.Close)
	return o
}

// --check against a stand-in for OSV: an advisory for one version and none
// for the next, with only names and versions sent.
func TestCheckFindsAnAdvisoryForOneVersionAndNoneForTheNext(t *testing.T) {
	o := newOSV(t)
	k := Checker{Base: o.URL, Client: o.Client(), Agent: "irie/test"}
	pkgs := []ipc.LockPackage{
		{Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.250", Part: "Claude Code"},
		{Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.251", Part: "Claude Code"},
	}
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	found, err := k.Check(context.Background(), pkgs, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("found %d, want 2", len(found))
	}
	want := ipc.LockAdvisory{ID: "GHSA-test-0001", Summary: "Claude Code: the Edit tool can write outside the workspace", Fixed: "2.1.251",
		Modified: time.Date(2026, 11, 9, 23, 0, 3, 851745000, time.UTC)}
	if f := found[0]; f.Version != "2.1.250" || !f.Checked.Equal(now) || len(f.Advisories) != 1 || f.Advisories[0] != want {
		t.Errorf("2.1.250: %+v", f)
	}
	if f := found[1]; f.Version != "2.1.251" || len(f.Advisories) != 0 || !f.Checked.Equal(now) {
		t.Errorf("2.1.251: %+v", f)
	}
	// Only the packages' names and versions went out.
	var sent map[string][]map[string]any
	if err := json.Unmarshal(o.queries[0], &sent); err != nil {
		t.Fatal(err)
	}
	for _, q := range sent["queries"] {
		keys := slices.Sorted(func(yield func(string) bool) {
			for k := range q {
				if !yield(k) {
					return
				}
			}
		})
		pkg, _ := q["package"].(map[string]any)
		if !reflect.DeepEqual(keys, []string{"package", "version"}) || len(pkg) != 2 || pkg["name"] == nil || pkg["ecosystem"] == nil {
			t.Errorf("a query sent %v", q)
		}
	}

	// Marked, the versions say so in words for the card.
	marked := Mark(pkgs, Results{}.Merge(found, o.URL, now).By())
	if r := Row(marked); r.Advisories != 1 || r.Words != "Claude Code 2.1.250: 1 advisory, fixed in 2.1.251" {
		t.Errorf("row %+v", r)
	}
	if got := Found(marked[1]); got != "no known advisory" {
		t.Errorf("2.1.251 is %q", got)
	}

	// A check again reads no advisory it knows as it is.
	reads := o.reads.Load()
	again, err := k.Check(context.Background(), pkgs, Results{Packages: found}.By(), now.Add(time.Hour))
	if err != nil || len(again[0].Advisories) != 1 || again[0].Advisories[0] != want {
		t.Fatalf("again: %+v, %v", again, err)
	}
	if o.reads.Load() != reads {
		t.Errorf("read the advisory again: %d reads, then %d", reads, o.reads.Load())
	}

	// OSV.dev not answering is an error that says so.
	o.Close()
	if _, err := k.Check(context.Background(), pkgs, nil, now); err == nil || !strings.Contains(err.Error(), "OSV.dev didn't answer") {
		t.Errorf("with OSV.dev gone: %v", err)
	}
}

func TestWordsSayWhatFixesIt(t *testing.T) {
	for _, c := range []struct {
		p    ipc.LockPackage
		want string
	}{
		{ipc.LockPackage{Name: ClaudeCode, Version: "2.1.250", Part: "Claude Code", Checked: true, Advisories: 1, Fixed: "2.1.251"}, "Claude Code 2.1.250: 1 advisory, fixed in 2.1.251"},
		{ipc.LockPackage{Name: "fake-server", Version: "0.6.2", Part: "MCP server loose", Checked: true, Advisories: 2, Unfixed: 2}, "MCP server loose 0.6.2: 2 advisories, no fix yet"},
		{ipc.LockPackage{Name: "fake-server", Version: "0.6.2", Checked: true, Advisories: 3, Unfixed: 1, Fixed: "0.7.0"}, "fake-server 0.6.2: 3 advisories, 2 fixed in 0.7.0"},
		{ipc.LockPackage{Name: "fake-server", Version: "0.6.2", Checked: true}, ""},
	} {
		if got := Words(c.p); got != c.want {
			t.Errorf("Words(%+v) = %q, want %q", c.p, got, c.want)
		}
	}
	// The fix of them all is the latest of their fixes; a package no
	// check knows keeps what its row said, as a peer's does.
	by := map[string]ipc.LockFound{Key(ipc.LockPackage{Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.80"}): {
		Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.80", Checked: time.Now(),
		Advisories: []ipc.LockAdvisory{{ID: "a", Fixed: "2.1.84"}, {ID: "b", Fixed: "2.1.163"}, {ID: "c", Fixed: "2.1.100"}},
	}}
	peer := ipc.LockPackage{Ecosystem: NPM, Name: "fake-server", Version: "0.6.2", Part: "MCP server loose", Checked: true, Advisories: 1, Unfixed: 1}
	got := Mark([]ipc.LockPackage{{Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.80", Part: "Claude Code"}, peer}, by)
	if r := Row(got); r.Advisories != 4 || r.Words != "Claude Code 2.1.80: 3 advisories, fixed in 2.1.163 · MCP server loose 0.6.2: 1 advisory, no fix yet" {
		t.Errorf("row %+v", r)
	}
}

func TestResultsKeepWhatNoCheckAsksAgainForAMonth(t *testing.T) {
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	old := Results{Packages: []ipc.LockFound{
		{Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.200", Checked: now.Add(-24 * time.Hour)},
		{Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.100", Checked: now.Add(-31 * 24 * time.Hour)},
		{Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.250", Checked: now.Add(-time.Hour)},
	}}
	r := old.Merge([]ipc.LockFound{{Ecosystem: NPM, Name: ClaudeCode, Version: "2.1.250", Checked: now, Advisories: []ipc.LockAdvisory{{ID: "x"}}, Sessions: []string{"a"}}}, OSV, now)
	var versions []string
	for _, f := range r.Packages {
		versions = append(versions, f.Version)
	}
	if !reflect.DeepEqual(versions, []string{"2.1.250", "2.1.200"}) || len(r.Packages[0].Advisories) != 1 || r.Packages[0].Sessions != nil || !r.Checked.Equal(now) {
		t.Errorf("merged %+v", r)
	}
	path := filepath.Join(t.TempDir(), ResultsFile)
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadResults(path)
	if err != nil || !reflect.DeepEqual(back.By(), r.By()) {
		t.Errorf("loaded %+v, %v", back, err)
	}
	if none, err := LoadResults(filepath.Join(t.TempDir(), "none.json")); err != nil || len(none.Packages) != 0 {
		t.Errorf("no file: %+v, %v", none, err)
	}
}
