package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// linkFixture is a core with a session whose folder holds a few files, and
// a peer's session, with home a folder of its own.
func linkFixture(t *testing.T, cfg config.Config) (c *Core, id, folder, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	folder = t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		p := filepath.Join(folder, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/core/pane.go", []byte("package core\n"))
	write("docs/notes.md", []byte("# Notes\n"))
	write("odd:name.go", []byte("package odd\n"))
	write("pic.png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	write("latin1.txt", []byte("caf\xe9\n"))
	write("big.txt", []byte(strings.Repeat("a", ipc.CardFileMax+1)))
	if err := os.WriteFile(filepath.Join(home, "todo.txt"), []byte("milk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c = bareCore(t, cfg)
	x := &session{id: "s0000000000l1", name: "links", worktree: folder}
	c.mu.Lock()
	c.sessions[x.id] = x
	if c.peers == nil {
		c.peers = map[string]*peerState{}
	}
	c.peers["Asahi"] = &peerState{name: "Asahi", rows: map[string]ipc.Session{"s5": {ID: "s5", Name: "fix it"}}}
	c.mu.Unlock()
	return c, x.id, folder, home
}

// By default a link opens in the session's panes: a page on a browser
// card, a file of text on the code card at the line it names, and what no
// card shows, such as a mail address, a folder or a picture, in the Mac's
// own app.
func TestALinkOpensInTheSessionsPanes(t *testing.T) {
	c, id, folder, home := linkFixture(t, config.Default())
	pane := filepath.Join(folder, "internal/core/pane.go")
	page := func(u string) ipc.LinkResult { return ipc.LinkResult{In: ipc.LinkInCard, Kind: "browser", URL: u} }
	file := func(p string, line, col int) ipc.LinkResult {
		return ipc.LinkResult{In: ipc.LinkInCard, Kind: "code", Path: p, Line: line, Col: col}
	}
	system := func(u string) ipc.LinkResult { return ipc.LinkResult{In: ipc.LinkInSystem, URL: u} }
	for link, want := range map[string]ipc.LinkResult{
		"https://example.com/docs?q=1#top": page("https://example.com/docs?q=1#top"),
		"http://localhost:5173/":           page("http://localhost:5173/"),
		"  https://example.com/  ":         page("https://example.com/"),
		"github.com/alice/repo":            page("https://github.com/alice/repo"),
		"localhost:3000":                   page("http://localhost:3000"),
		"internal/core/pane.go":            file(pane, 0, 0),
		"internal/core/pane.go:12":         file(pane, 12, 0),
		"internal/core/pane.go:12:5":       file(pane, 12, 5),
		"internal/core/pane.go:12-20":      file(pane, 12, 0),
		"internal/core/pane.go#L7-L9":      file(pane, 7, 0),
		"internal/core/pane.go.":           file(pane, 0, 0),
		"internal/core/pane.go:3.":         file(pane, 3, 0),
		"./docs/notes.md":                  file(filepath.Join(folder, "docs/notes.md"), 0, 0),
		"docs/../docs/notes.md":            file(filepath.Join(folder, "docs/notes.md"), 0, 0),
		pane + ":4":                        file(pane, 4, 0),
		"file://" + pane:                   file(pane, 0, 0),
		"odd:name.go":                      file(filepath.Join(folder, "odd:name.go"), 0, 0),
		"~/todo.txt:2":                     file(filepath.Join(home, "todo.txt"), 2, 0),
		"$HOME/todo.txt":                   file(filepath.Join(home, "todo.txt"), 0, 0),
		"pic.png":                          system("file://" + filepath.Join(folder, "pic.png")),
		"latin1.txt":                       system("file://" + filepath.Join(folder, "latin1.txt")),
		"big.txt":                          system("file://" + filepath.Join(folder, "big.txt")),
		"docs/":                            system("file://" + filepath.Join(folder, "docs")),
		"mailto:alice@example.com":         system("mailto:alice@example.com"),
		"ssh://alice@example.com":          system("ssh://alice@example.com"),
		"file://example.com/share/a.txt":   system("file://example.com/share/a.txt"),
	} {
		got, err := c.linkOp(ipc.LinkArgs{Session: id, Link: link})
		if err != nil || got != want {
			t.Errorf("%q = %+v, %v; want %+v", link, got, err, want)
		}
	}
	for link, msg := range map[string]string{
		"internal/nope.go:3": "No file at internal/nope.go.",
		"~/nope.txt":         "No file at ~/nope.txt.",
		"notes.md":           "No file at notes.md.",
	} {
		if _, err := c.linkOp(ipc.LinkArgs{Session: id, Link: link}); code(err) != ipc.CodeNotFound || err.Error() != msg {
			t.Errorf("%q: %v, want %q", link, err, msg)
		}
	}
	for name, a := range map[string]ipc.LinkArgs{
		"no link":   {Session: id, Link: " "},
		"too long":  {Session: id, Link: "https://example.com/" + strings.Repeat("a", ipc.LinkMax)},
		"a peer's":  {Session: "Asahi/s5", Link: "internal/core/pane.go:12"},
		"a device":  {Session: id, Link: "/dev/null"},
		"not found": {Session: "s0000000000zz", Link: "https://example.com/"},
	} {
		if _, err := c.linkOp(a); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// A click that asks for the other place, and ui.terminal_links = "browser",
// open a link in the Mac's own apps; with no session there are no panes
// to open it in, and a peer's session's addresses still open here.
func TestALinkOpensWhereTheSettingSays(t *testing.T) {
	c, id, folder, _ := linkFixture(t, config.Default())
	pane := filepath.Join(folder, "internal/core/pane.go")
	for _, tc := range []struct {
		a    ipc.LinkArgs
		want ipc.LinkResult
	}{
		{ipc.LinkArgs{Session: id, Link: "https://example.com/", Other: true}, ipc.LinkResult{In: ipc.LinkInSystem, URL: "https://example.com/"}},
		{ipc.LinkArgs{Session: id, Link: "internal/core/pane.go:12", Other: true}, ipc.LinkResult{In: ipc.LinkInSystem, URL: "file://" + pane}},
		{ipc.LinkArgs{Link: "https://example.com/"}, ipc.LinkResult{In: ipc.LinkInSystem, URL: "https://example.com/"}},
		{ipc.LinkArgs{Session: "Asahi/s5", Link: "https://example.com/"}, ipc.LinkResult{In: ipc.LinkInCard, Kind: "browser", URL: "https://example.com/"}},
		{ipc.LinkArgs{Session: "Asahi/s5", Link: "github.com/alice/repo"}, ipc.LinkResult{In: ipc.LinkInCard, Kind: "browser", URL: "https://github.com/alice/repo"}},
		{ipc.LinkArgs{Session: "Asahi/s5", Link: "mailto:alice@example.com"}, ipc.LinkResult{In: ipc.LinkInSystem, URL: "mailto:alice@example.com"}},
	} {
		if got, err := c.linkOp(tc.a); err != nil || got != tc.want {
			t.Errorf("%+v = %+v, %v; want %+v", tc.a, got, err, tc.want)
		}
	}
	if _, err := c.linkOp(ipc.LinkArgs{Session: "Asahi/s5", Link: "internal/core/pane.go:12"}); err == nil || !strings.Contains(err.Error(), "on Asahi, where fix it runs") {
		t.Errorf("a peer's file: %v", err)
	}

	cfg := config.Default()
	cfg.UI.TerminalLinks = "browser"
	c, id, folder, _ = linkFixture(t, cfg)
	pane = filepath.Join(folder, "internal/core/pane.go")
	for _, tc := range []struct {
		a    ipc.LinkArgs
		want ipc.LinkResult
	}{
		{ipc.LinkArgs{Session: id, Link: "https://example.com/"}, ipc.LinkResult{In: ipc.LinkInSystem, URL: "https://example.com/"}},
		{ipc.LinkArgs{Session: id, Link: "internal/core/pane.go:12"}, ipc.LinkResult{In: ipc.LinkInSystem, URL: "file://" + pane}},
		{ipc.LinkArgs{Session: id, Link: "https://example.com/", Other: true}, ipc.LinkResult{In: ipc.LinkInCard, Kind: "browser", URL: "https://example.com/"}},
		{ipc.LinkArgs{Session: id, Link: "internal/core/pane.go:12", Other: true}, ipc.LinkResult{In: ipc.LinkInCard, Kind: "code", Path: pane, Line: 12}},
	} {
		if got, err := c.linkOp(tc.a); err != nil || got != tc.want {
			t.Errorf("browser: %+v = %+v, %v; want %+v", tc.a, got, err, tc.want)
		}
	}
}

// The header carries ui.terminal_links, and hello lists the link op, which
// answers through Call.
func TestTheHeaderSaysWhereLinksOpen(t *testing.T) {
	cfg := config.Default()
	c := bareCore(t, cfg)
	c.mu.Lock()
	h := c.headerNow()
	c.mu.Unlock()
	if h.TerminalLinks != "rolodex" {
		t.Errorf("header's terminal_links = %q, want rolodex", h.TerminalLinks)
	}
	cfg.UI.TerminalLinks = "browser"
	c.useConfig(cfg)
	c.mu.Lock()
	h = c.headerNow()
	c.mu.Unlock()
	if h.TerminalLinks != "browser" {
		t.Errorf("header's terminal_links = %q, want browser", h.TerminalLinks)
	}
	res, err := c.Call(ipc.OpHello, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hello, ok := res.(ipc.HelloResult); !ok || !slices.Contains(hello.Features, ipc.OpLink) {
		t.Errorf("hello = %+v, want %q among its features", res, ipc.OpLink)
	}
	raw, _ := json.Marshal(ipc.LinkArgs{Link: "https://example.com/"})
	if res, err := c.Call(ipc.OpLink, raw); err != nil || res != (ipc.LinkResult{In: ipc.LinkInSystem, URL: "https://example.com/"}) {
		t.Errorf("link = %+v, %v", res, err)
	}
}
