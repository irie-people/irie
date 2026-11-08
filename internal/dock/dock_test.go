package dock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmuxExample is the example file of cmux's Dock docs, which must read as is.
const cmuxExample = `{
  "controls": [
    {"id": "git", "title": "Git", "command": "lazygit", "height": 300},
    {"id": "logs", "title": "Logs", "command": "tail -f ./logs/development.log", "cwd": "."},
    {"id": "feed", "title": "Feed", "command": "cmux feed tui --opentui", "height": 320},
    {"id": "dashboard", "title": "Dashboard", "type": "browser", "url": "http://127.0.0.1:8877/sidebar", "chrome": false}
  ]
}`

func TestParseReadsCmuxExample(t *testing.T) {
	got, err := Parse([]byte(cmuxExample))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d controls, want 4", len(got))
	}
	if got[0].Type != Terminal || got[0].Scope != ScopeSession || got[0].Height != 300 {
		t.Errorf("git = %+v, want a terminal control of the session, 300 high", got[0])
	}
	d := got[3]
	if d.Type != Browser || d.URL != "http://127.0.0.1:8877/sidebar" || d.Chrome == nil || *d.Chrome {
		t.Errorf("dashboard = %+v, want a browser control without chrome", d)
	}
}

func TestParseDefaultsAndUnknownFields(t *testing.T) {
	got, err := Parse([]byte(`{"controls":[{"id":"tests","command":"make test","newer":"field"}], "version": 2}`))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Title != "tests" || got[0].Type != Terminal || got[0].Scope != ScopeSession {
		t.Errorf("got %+v, want the id as title, a terminal, the session's scope", got[0])
	}
}

func TestParseRefuses(t *testing.T) {
	for name, data := range map[string]string{
		"not json":        `{"controls": [`,
		"no id":           `{"controls":[{"command":"ls"}]}`,
		"id with a dot":   `{"controls":[{"id":"a.b","command":"ls"}]}`,
		"id twice":        `{"controls":[{"id":"a","command":"ls"},{"id":"a","command":"ls"}]}`,
		"no command":      `{"controls":[{"id":"a"}]}`,
		"browser, no url": `{"controls":[{"id":"a","type":"browser"}]}`,
		"browser, file":   `{"controls":[{"id":"a","type":"browser","url":"file:///etc/passwd"}]}`,
		"unknown type":    `{"controls":[{"id":"a","type":"window","command":"ls"}]}`,
		"unknown scope":   `{"controls":[{"id":"a","command":"ls","scope":"world"}]}`,
		"bad env name":    `{"controls":[{"id":"a","command":"ls","env":{"A=B":"c"}}]}`,
		"too many":        `{"controls":[` + strings.Repeat(`{"id":"x","command":"ls"},`, MaxControls) + `{"id":"y","command":"ls"}]}`,
	} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindPrefersIrieAndTheNearest(t *testing.T) {
	top := t.TempDir()
	sub := filepath.Join(top, "app", "web")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if p, _ := Find(sub, top); p != "" {
		t.Fatalf("found %q in a repo with no Dock file", p)
	}
	write(t, filepath.Join(top, ".cmux", "dock.json"), cmuxExample)
	if p, src := Find(sub, top); p != filepath.Join(top, ".cmux", "dock.json") || src != SourceCmux {
		t.Fatalf("got %q %q, want the top's cmux file", p, src)
	}
	write(t, filepath.Join(top, ".irie", "dock.json"), cmuxExample)
	if p, src := Find(sub, top); p != filepath.Join(top, ".irie", "dock.json") || src != SourceRepo {
		t.Fatalf("got %q %q, want the top's Irie file over its cmux one", p, src)
	}
	write(t, filepath.Join(top, "app", ".cmux", "dock.json"), cmuxExample)
	if p, _ := Find(sub, top); p != filepath.Join(top, "app", ".cmux", "dock.json") {
		t.Fatalf("got %q, want the nearest folder's file", p)
	}
	// Outside a repo only the folder itself counts.
	if p, _ := Find(sub, ""); p != "" {
		t.Fatalf("got %q, want none: outside a repo the folders above don't count", p)
	}
}

func TestFindMine(t *testing.T) {
	home := t.TempDir()
	mine := filepath.Join(home, ".config", "irie", "dock.json")
	// cmux's own personal file is never read.
	write(t, filepath.Join(home, ".config", "cmux", "dock.json"), cmuxExample)
	if p, _ := FindMine(mine); p != "" {
		t.Fatalf("got %q, want none", p)
	}
	write(t, mine, cmuxExample)
	if p, src := FindMine(mine); p != mine || src != SourceMine {
		t.Fatalf("got %q %q, want Irie's own file", p, src)
	}
}

func TestReadHashesAndReportsProblems(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	write(t, good, cmuxExample)
	f := Read(good, SourceRepo)
	if f.Problem != "" || len(f.Controls) != 4 || len(f.Hash) != 64 {
		t.Fatalf("got %+v, want 4 controls and a SHA-256", f)
	}
	write(t, good, cmuxExample+"\n")
	if g := Read(good, SourceRepo); g.Hash == f.Hash {
		t.Fatal("a changed file kept its hash, so its trust would carry over")
	}
	bad := filepath.Join(dir, "bad.json")
	write(t, bad, `{"controls":[{"id":"a"}]}`)
	if b := Read(bad, SourceRepo); b.Problem == "" || len(b.Controls) != 0 {
		t.Fatalf("got %+v, want a problem and no controls", b)
	}
	if m := Read(filepath.Join(dir, "none.json"), SourceMine); m.Problem == "" {
		t.Fatal("a missing file read without a problem")
	}
}

func TestInstanceAndKey(t *testing.T) {
	c := Control{ID: "git", Scope: ScopeSession}
	if a, b := Instance(c, SourceRepo, "s1", "r"), Instance(c, SourceMine, "s1", "r"); a == b {
		t.Errorf("a repo control and the user's own with one id share %q", a)
	}
	if a, b := Instance(c, SourceRepo, "s1", "r"), Instance(c, SourceRepo, "s2", "r"); a == b {
		t.Errorf("two sessions share the copy %q of a session control", a)
	}
	c.Scope = ScopeRepo
	if a, b := Instance(c, SourceRepo, "s1", "r"), Instance(c, SourceRepo, "s2", "r"); a != b {
		t.Errorf("a repo control has copies %q and %q, want one", a, b)
	}
	for _, name := range []string{Instance(c, SourceCmux, "s1", "abc"), Instance(Control{ID: "x", Scope: ScopeGlobal}, SourceMine, "s1", "")} {
		if strings.ContainsAny(name, ".:") {
			t.Errorf("%q holds a character tmux would change", name)
		}
	}
	if Key(SourceCmux, "git") != Key(SourceRepo, "git") || Key(SourceMine, "git") == Key(SourceRepo, "git") {
		t.Error("keys must tell the user's own controls from the repo's, and only that")
	}
}

func TestCwd(t *testing.T) {
	for _, c := range []struct{ cwd, want string }{
		{"", "/w/s1"},
		{".", "/rel"},
		{"web", "/rel/web"},
		{"~", "/home/alice"},
		{"~/src", "/home/alice/src"},
		{"/var/log", "/var/log"},
	} {
		if got := Cwd(Control{Cwd: c.cwd}, "/w/s1", "/rel", "/home/alice"); got != c.want {
			t.Errorf("cwd %q: got %q, want %q", c.cwd, got, c.want)
		}
	}
}
