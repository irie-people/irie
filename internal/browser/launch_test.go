package browser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func writeExec(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindLooksWhereInstallersPutIt(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Applications", "Mullvad Browser.app", "Contents", "MacOS", "mullvadbrowser")
	onPath := filepath.Join(dir, "bin", "mullvad-browser")
	oldApps, oldLook, oldHome := appPaths, lookPath, homeDir
	t.Cleanup(func() { appPaths, lookPath, homeDir = oldApps, oldLook, oldHome })
	homeDir = func() (string, error) { return dir, nil }
	appPaths = map[string][]string{runtime.GOOS: {"~/Applications/Mullvad Browser.app/Contents/MacOS/mullvadbrowser"}}
	lookPath = func(name string) (string, error) {
		if p := filepath.Join(dir, "bin", name); executable(p) {
			return p, nil
		}
		return "", errors.New("not found")
	}

	if _, err := Find(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nothing installed: %v", err)
	}
	writeExec(t, onPath)
	if got, err := Find(""); err != nil || got != onPath {
		t.Fatalf("on PATH: %q, %v", got, err)
	}
	writeExec(t, app)
	if got, err := Find(""); err != nil || got != app {
		t.Fatalf("the app comes first: %q, %v", got, err)
	}
	other := filepath.Join(dir, "firefox")
	if _, err := Find(other); err == nil {
		t.Fatal("a browser.path that is not there was found")
	}
	writeExec(t, other)
	if got, err := Find(other); err != nil || got != other {
		t.Fatalf("browser.path: %q, %v", got, err)
	}
}

func TestParseProxy(t *testing.T) {
	for _, c := range []struct {
		in   string
		host string
		port int
		ok   bool
	}{
		{"socks5://10.64.0.1:1080", "10.64.0.1", 1080, true},
		{"socks5h://se-sto-wg-socks5-001.relays.mullvad.net:1080", "se-sto-wg-socks5-001.relays.mullvad.net", 1080, true},
		{"socks5://[::1]:1080", "::1", 1080, true},
		{"http://10.64.0.1:1080", "", 0, false},
		{"socks5://10.64.0.1", "", 0, false},
		{"socks5://user:pw@10.64.0.1:1080", "", 0, false},
		{"socks5://10.64.0.1:99999", "", 0, false},
		{`socks5://a");evil(":1080`, "", 0, false},
	} {
		host, port, err := ParseProxy(c.in)
		if (err == nil) != c.ok || host != c.host || port != c.port {
			t.Errorf("ParseProxy(%q) = %q, %d, %v", c.in, host, port, err)
		}
	}
}

func TestUserPrefsKeepTheBrowsersOwnSettings(t *testing.T) {
	plainPrefs := userPrefs(Options{})
	if plainPrefs != "user_pref(\"remote.prefs.recommended\", false);\n" {
		t.Fatalf("with no proxy: %q", plainPrefs)
	}
	p := userPrefs(Options{Proxy: "socks5://10.64.0.1:1080"})
	for _, want := range []string{
		`user_pref("remote.prefs.recommended", false);`,
		`user_pref("network.proxy.type", 1);`,
		`user_pref("network.proxy.socks", "10.64.0.1");`,
		`user_pref("network.proxy.socks_port", 1080);`,
		`user_pref("network.proxy.socks_remote_dns", true);`,
		// Behind a proxy, the ways round it are shut (DESIGN §4.16).
		`user_pref("network.proxy.failover_direct", false);`,
		`user_pref("network.proxy.allow_hijacking_localhost", true);`,
		`user_pref("network.dns.disabled", true);`,
		`user_pref("media.peerconnection.enabled", false);`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("user.js lacks %s:\n%s", want, p)
		}
	}
}

func TestCopyProfileKeepsTimesAndLeavesRunFiles(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	files := map[string]string{
		"prefs.js":                           "user_pref(\"a\", 1);",
		"extensions/uBlock0@raymondhill.net": "xpi",
		"WebDriverBiDiServer.json":           "{}",
		"parent.lock":                        "",
		pidFile:                              "123",
		templateMark:                         "{}",
	}
	for name, body := range files {
		p := filepath.Join(src, name)
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte(body), 0o600)
		os.Chtimes(p, old, old)
	}
	os.Symlink("/nowhere", filepath.Join(src, "lock"))
	if err := copyProfile(src, dst); err != nil {
		t.Fatal(err)
	}
	var got []string
	filepath.WalkDir(dst, func(p string, d os.DirEntry, err error) error {
		if !d.IsDir() {
			rel, _ := filepath.Rel(dst, p)
			got = append(got, rel)
		}
		return nil
	})
	slices.Sort(got)
	want := []string{"extensions/uBlock0@raymondhill.net", "prefs.js"}
	if !slices.Equal(got, want) {
		t.Fatalf("copied %v, want %v", got, want)
	}
	fi, _ := os.Stat(filepath.Join(dst, "extensions/uBlock0@raymondhill.net"))
	if !fi.ModTime().Equal(old) {
		t.Fatalf("the copy's time is %v, want %v", fi.ModTime(), old)
	}
}

func TestTemplateReadyFollowsTheBrowsersBuild(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "mullvadbrowser")
	writeExec(t, exe)
	tmpl := filepath.Join(dir, "template")
	if TemplateReady(tmpl, exe) {
		t.Fatal("no template is ready")
	}
	info, _ := infoOf(exe)
	mark, _ := json.Marshal(info)
	os.MkdirAll(tmpl, 0o700)
	os.WriteFile(filepath.Join(tmpl, templateMark), mark, 0o600)
	if !TemplateReady(tmpl, exe) {
		t.Fatal("the template made by this build is not ready")
	}
	os.WriteFile(exe, []byte("#!/bin/sh\n# a new build\n"), 0o755)
	if TemplateReady(tmpl, exe) {
		t.Fatal("a template from another build is still ready")
	}
}

func TestServerFileAndListeningLine(t *testing.T) {
	dir := t.TempDir()
	if serverFile(dir) != "" {
		t.Fatal("an address with no file")
	}
	os.WriteFile(filepath.Join(dir, "WebDriverBiDiServer.json"), []byte(`{"ws_host": "127.0.0.1", "ws_port": 51900}`), 0o600)
	if got := serverFile(dir); got != "127.0.0.1:51900" {
		t.Fatalf("serverFile = %q", got)
	}
	var lines []string
	w := &lineWriter{line: func(l string) { lines = append(lines, l) }}
	w.Write([]byte("*** You are running in headless mode.\nWebDriver BiDi listen"))
	w.Write([]byte("ing on ws://127.0.0.1:51900\r\n"))
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	if m := listening.FindStringSubmatch(lines[1]); m == nil || m[1] != "127.0.0.1:51900" {
		t.Fatalf("the listening line: %q", lines[1])
	}
}

func TestStartReportsABrowserThatExits(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "notabrowser")
	os.WriteFile(exe, []byte("#!/bin/sh\necho 'cannot open display' >&2\nexit 1\n"), 0o755)
	_, err := start(context.Background(), Options{Headless: true}, exe, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "cannot open display") {
		t.Fatalf("start = %v, want the browser's own last words", err)
	}
}

func TestArgsRoundTrip(t *testing.T) {
	o := Options{Path: "/opt/mb/mullvadbrowser", Profile: "/s/browser", Template: "/d/template", Headless: false, Idle: 3 * time.Minute, Proxy: "socks5://10.64.0.1:1080", Search: "https://example.com/?q=%s"}
	args := Args(o)
	if args[0] != "browser" {
		t.Fatalf("Args = %q", args)
	}
	got, check, err := ParseArgs(args[1:], os.Stderr)
	if err != nil || check != "" || got != o {
		t.Fatalf("ParseArgs(Args(o)) = %+v, %q, %v", got, check, err)
	}
	d, _, _ := ParseArgs(nil, os.Stderr)
	if !d.Headless || d.Idle != DefaultIdle || d.Search != DefaultSearch {
		t.Fatalf("defaults = %+v", d)
	}
	if !slices.Contains(Args(d), "--headless=true") || slices.Contains(Args(d), "--search") {
		t.Fatalf("Args(defaults) = %q", Args(d))
	}
	if _, _, err := ParseArgs([]string{"--proxy", "http://x:1"}, os.Stderr); err == nil {
		t.Fatal("a bad proxy was taken")
	}
}

func TestNormURL(t *testing.T) {
	for in, want := range map[string]string{
		"example.com":                 "https://example.com",
		" https://example.com/a?b=c ": "https://example.com/a?b=c",
		"http://localhost:8080/":      "http://localhost:8080/",
		"about:blank":                 "about:blank",
		"file:///etc/passwd":          "",
		"javascript:alert(1)":         "",
		"":                            "",
		"https://":                    "",
	} {
		got, err := normURL(in)
		if (err == nil) != (want != "") || got != want {
			t.Errorf("normURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestNum(t *testing.T) {
	for n, want := range map[int]string{0: "0", 917: "917", 10000: "10,000", 1234567: "1,234,567"} {
		if got := num(n); got != want {
			t.Errorf("num(%d) = %q", n, got)
		}
	}
}

func TestHowToInstall(t *testing.T) {
	brew := func(string) (string, error) { return "/opt/homebrew/bin/brew", nil }
	none := func(string) (string, error) { return "", errors.New("no") }
	if i := HowToInstall("darwin", brew); !slices.Equal(i.Argv, []string{"/opt/homebrew/bin/brew", "install", "--cask", "mullvad-browser"}) {
		t.Fatalf("macOS with brew: %+v", i)
	}
	if i := HowToInstall("darwin", none); i.Argv != nil || !strings.Contains(i.Hint, "mullvad.net/browser") {
		t.Fatalf("macOS without brew: %+v", i)
	}
	if i := HowToInstall("linux", brew); i.Argv != nil {
		t.Fatalf("Linux: %+v", i)
	}
}

// TestAProxysNameIsLookedUpBeforeTheBrowserStarts: behind a proxy the
// browser does without DNS of its own, so a proxy named by its host name
// goes to it as an address (DESIGN §4.16).
func TestAProxysNameIsLookedUpBeforeTheBrowserStarts(t *testing.T) {
	defer func(f func(context.Context, string) ([]string, error)) { lookupHost = f }(lookupHost)
	lookupHost = func(_ context.Context, host string) ([]string, error) {
		if host == "se-got-wg-socks5-001.relays.mullvad.net" {
			return []string{"fc00:bbbb::1", "10.124.0.7"}, nil
		}
		return nil, errors.New("no such host")
	}
	for in, want := range map[string]string{
		"socks5://se-got-wg-socks5-001.relays.mullvad.net:1080":  "socks5://10.124.0.7:1080",
		"socks5h://se-got-wg-socks5-001.relays.mullvad.net:1080": "socks5://10.124.0.7:1080",
		"socks5h://127.0.0.1:9150":                               "socks5h://127.0.0.1:9150",
	} {
		if got, err := resolveProxy(context.Background(), in); err != nil || got != want {
			t.Errorf("resolveProxy(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := resolveProxy(context.Background(), "socks5://nowhere.example:1080"); err == nil || !strings.Contains(err.Error(), "Mullvad VPN") {
		t.Errorf("a name that doesn't resolve: %v", err)
	}
	if p := userPrefs(Options{Proxy: "socks5://[fc00:bbbb::1]:1080"}); !strings.Contains(p, `user_pref("network.proxy.socks", "fc00:bbbb::1");`) {
		t.Errorf("an IPv6 proxy:\n%s", p)
	}
}
