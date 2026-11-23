package raphael

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/sysload"
)

// blob writes a file of 64 KiB at path, made the given age. Its bytes are
// random, so that no file system stores it in fewer blocks.
func blob(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 64<<10)
	rand.Read(b)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// aged makes a folder the given age.
func aged(t *testing.T, path string, age time.Duration) {
	t.Helper()
	at := time.Now().Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// shortBy is a machine whose free disk is short of the buffer by short
// bytes, and gains what goes from under root, as Keep reads its room.
func shortBy(root string, short int64) func() (sysload.Room, error) {
	start := treeSize(root)
	return func() (sysload.Room, error) {
		return sysload.Room{RAM: 1 << 30, DiskTotal: 1 << 40, DiskFree: sysload.MinBuffer - short + (start - treeSize(root))}, nil
	}
}

// keepMac is a home with Go's build cache, whose entries are as old as
// ages says, and a runner whose ps lists ps.
func keepMac(t *testing.T, ages map[string]time.Duration, ps string) (home string, places Places, r *fakeRunner) {
	t.Helper()
	home = t.TempDir()
	dir := filepath.Join(home, "Library", "Caches", "go-build")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "README"), []byte(goReadme+"\nRun \"go clean -cache\" if the directory is getting too large.\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "trim.txt"), []byte("1\n"), 0o600)
	for name, age := range ages {
		p := filepath.Join(dir, name[:2], name)
		if strings.HasSuffix(name, "/") {
			// An executable entry is a folder.
			p = filepath.Join(dir, name[:2], strings.TrimSuffix(name, "/"))
			blob(t, filepath.Join(p, "prog"), age)
			aged(t, p, age)
			continue
		}
		blob(t, p, age)
	}
	r = &fakeRunner{out: map[string]string{"/bin/ps " + strings.Join(psArgs, " "): ps}}
	return home, Places{GoCache: dir}, r
}

// startsAt has procStarts say when each pid started, for the test.
func startsAt(t *testing.T, starts map[int]time.Time) {
	t.Helper()
	old := procStarts
	procStarts = func() (map[int]time.Time, error) { return starts, nil }
	t.Cleanup(func() { procStarts = old })
}

func goEntry(places Places, name string) string {
	return filepath.Join(places.GoCache, name[:2], strings.TrimSuffix(name, "/"))
}

func TestKeepTrimsGoBuildOldestFirstUntilTheBufferIsBack(t *testing.T) {
	ages := map[string]time.Duration{
		"00aa-d": 30 * time.Hour, "05ff-d/": 6 * time.Hour, "01bb-a": 5 * time.Hour,
		"02cc-d": 3 * time.Hour, "03dd-d": 90 * time.Minute, "04ee-a": 20 * time.Minute,
	}
	home, places, r := keepMac(t, ages, "")
	startsAt(t, map[int]time.Time{})
	// Short by a little more than one entry: the two oldest go, the rest stay.
	res, err := Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, 100<<10)})
	if err != nil {
		t.Fatal(err)
	}
	for name, gone := range map[string]bool{"00aa-d": true, "05ff-d/": true, "01bb-a": false, "02cc-d": false, "03dd-d": false, "04ee-a": false} {
		if exists(goEntry(places, name)) == gone {
			t.Errorf("%s gone = %v, want %v", name, !gone, gone)
		}
	}
	if !exists(filepath.Join(places.GoCache, "README")) || !exists(filepath.Join(places.GoCache, "trim.txt")) {
		t.Error("removed what is not an entry")
	}
	if len(res.Cleared) != 1 || res.Cleared[0].ID != "go-build" || res.Cleared[0].Size < 128<<10 || !strings.Contains(res.Cleared[0].What, "2 oldest entries") || res.Cleared[0].Freed < 128<<10 {
		t.Errorf("cleared %+v", res.Cleared)
	}
	if res.Room.Short() {
		t.Errorf("still short: %s", res.Room.Words())
	}
	if !strings.HasPrefix(res.Cleared[0].Path, "~/") {
		t.Errorf("path %q is not written from ~", res.Cleared[0].Path)
	}
}

func TestKeepLeavesWhatAGoBuildAtWorkMayRead(t *testing.T) {
	ages := map[string]time.Duration{"00aa-d": 30 * time.Hour, "01bb-a": 5 * time.Hour, "02cc-d": 3 * time.Hour, "03dd-d": 90 * time.Minute}
	// A go command that started two and a half hours ago may read what it
	// last touched an hour before it started.
	home, places, r := keepMac(t, ages, "  42 1 501 100 0:01.00 /usr/local/go/bin/go\n  43 42 501 100 0:01.00 /tmp/go-build1/b001/core.test\n")
	startsAt(t, map[int]time.Time{42: time.Now().Add(-150 * time.Minute), 43: time.Now().Add(-48 * time.Hour)})
	res, err := Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, 10<<20)})
	if err != nil {
		t.Fatal(err)
	}
	for name, gone := range map[string]bool{"00aa-d": true, "01bb-a": true, "02cc-d": false, "03dd-d": false} {
		if exists(goEntry(places, name)) == gone {
			t.Errorf("%s gone = %v, want %v", name, !gone, gone)
		}
	}
	if !res.Room.Short() || len(res.Cleared) != 1 {
		t.Errorf("room %s, cleared %+v", res.Room.Words(), res.Cleared)
	}
}

func TestKeepLeavesAFolderThatIsNotGosCache(t *testing.T) {
	home, places, r := keepMac(t, map[string]time.Duration{"00aa-d": 30 * time.Hour}, "")
	startsAt(t, map[int]time.Time{})
	os.Remove(filepath.Join(places.GoCache, "README"))
	res, _ := Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, 10<<20)})
	if !exists(goEntry(places, "00aa-d")) || len(res.Cleared) != 0 || len(res.Left) != 1 || !strings.Contains(res.Left[0], "does not look like Go's") {
		t.Errorf("cleared %+v, left %v", res.Cleared, res.Left)
	}
	// Nor the home folder, whatever GOCACHE says.
	places.GoCache = home
	os.WriteFile(filepath.Join(home, "README"), []byte(goReadme+"\n"), 0o600)
	blob(t, filepath.Join(home, "00", "x-d"), 30*time.Hour)
	if res, _ := Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, 10<<20)}); len(res.Cleared) != 0 || !exists(filepath.Join(home, "00", "x-d")) {
		t.Errorf("cleared the home folder: %+v", res.Cleared)
	}
}

func TestKeepDoesNothingWithTheBufferFreeAndMeasuresInADryRun(t *testing.T) {
	home, places, r := keepMac(t, map[string]time.Duration{"00aa-d": 30 * time.Hour, "01bb-a": 5 * time.Hour}, "")
	startsAt(t, map[int]time.Time{})
	res, _ := Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, -1<<30)})
	if len(res.Cleared) != 0 || !exists(goEntry(places, "00aa-d")) {
		t.Errorf("cleared with room to spare: %+v", res.Cleared)
	}
	res, _ = Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, -1<<30), DryRun: true})
	if len(res.Cleared) != 1 || res.Cleared[0].Size < 128<<10 || !exists(goEntry(places, "00aa-d")) || !exists(goEntry(places, "01bb-a")) {
		t.Errorf("dry run: %+v", res.Cleared)
	}
}

func TestKeepClearsHomebrewsDownloadsWhileBrewIsIdle(t *testing.T) {
	home := t.TempDir()
	brew := filepath.Join(home, "Library", "Caches", "Homebrew")
	blob(t, filepath.Join(brew, "downloads", "abc--tool--1.0.bottle.tar.gz"), time.Hour)
	blob(t, filepath.Join(brew, "api", "formula.jws.json"), time.Hour)
	os.Symlink(filepath.Join(brew, "downloads", "abc--tool--1.0.bottle.tar.gz"), filepath.Join(brew, "tool--1.0.bottle.tar.gz"))
	ps := "/bin/ps " + strings.Join(psArgs, " ")
	r := &fakeRunner{out: map[string]string{ps: "  7 1 501 100 0:01.00 /opt/homebrew/bin/brew\n"}}
	startsAt(t, map[int]time.Time{})
	places := Places{BrewCache: brew}
	res, _ := Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, 10<<20)})
	if len(res.Cleared) != 0 || len(res.Left) != 1 || !strings.Contains(res.Left[0], "Homebrew is at work") {
		t.Errorf("while brew runs: %+v %v", res.Cleared, res.Left)
	}
	r.out[ps] = ""
	res, _ = Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, 10<<20)})
	if len(res.Cleared) != 1 || res.Cleared[0].ID != "homebrew" || exists(filepath.Join(brew, "downloads", "abc--tool--1.0.bottle.tar.gz")) {
		t.Errorf("idle: %+v", res.Cleared)
	}
	if _, err := os.Lstat(filepath.Join(brew, "tool--1.0.bottle.tar.gz")); err == nil {
		t.Error("the link to the download stays")
	}
	if !exists(filepath.Join(brew, "api", "formula.jws.json")) || !exists(filepath.Join(brew, "downloads")) {
		t.Error("cleared more than the downloads")
	}
}

// uvRunner is fakeRunner, and uv, which clears its own cache.
type uvRunner struct {
	fakeRunner
	uv    string // uv's cache
	clean int    // the times uv was asked to clear it
}

func (u *uvRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	if name == "/home/alice/.local/bin/uv" && strings.Join(args, " ") == "cache clean" {
		u.clean++
		names, _ := readNames(u.uv)
		for _, n := range names {
			os.RemoveAll(filepath.Join(u.uv, n))
		}
		return nil, nil, nil
	}
	return u.fakeRunner.Run(ctx, name, args...)
}

func TestKeepClearsEachToolsCacheWhileTheToolIsIdle(t *testing.T) {
	home := t.TempDir()
	places := Places{Yarn: filepath.Join(home, "Library", "Caches", "Yarn"), Pip: filepath.Join(home, "Library", "Caches", "pip"), UV: filepath.Join(home, ".cache", "uv"), NPM: filepath.Join(home, ".npm", "_cacache"), UVBin: "/home/alice/.local/bin/uv"}
	for _, d := range []string{places.Yarn, places.Pip, places.UV, places.NPM} {
		blob(t, filepath.Join(d, "v6", "pkg"), time.Hour)
	}
	// npx's own folder, beside npm's cache, is never touched: what it
	// installed may be running.
	blob(t, filepath.Join(home, ".npm", "_npx", "abc", "server.js"), time.Hour)
	r := &uvRunner{fakeRunner: fakeRunner{out: map[string]string{"/bin/ps " + strings.Join(psArgs, " "): "  9 1 501 100 0:01.00 /opt/homebrew/bin/node\n 10 1 501 100 0:01.00 /opt/homebrew/bin/node\n"}}, uv: places.UV}
	startsAt(t, map[int]time.Time{})
	old := procArgs
	procArgs = func(pid int) []string {
		if pid == 9 {
			return []string{"node", "/opt/homebrew/bin/yarn", "install"}
		}
		return []string{"node", "/home/alice/.npm/_npx/abc/node_modules/.bin/mcp-server"}
	}
	t.Cleanup(func() { procArgs = old })
	res, _ := Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, 10<<20)})
	got := map[string]bool{}
	for _, c := range res.Cleared {
		got[c.ID] = true
	}
	if got["yarn"] || !got["pip"] || !got["uv"] || !got["npm"] {
		t.Errorf("cleared %+v, left %v", res.Cleared, res.Left)
	}
	if !exists(filepath.Join(places.Yarn, "v6", "pkg")) || exists(filepath.Join(places.Pip, "v6", "pkg")) || !exists(places.Pip) {
		t.Error("yarn's cache went while yarn ran, or pip's folder went with its cache")
	}
	if !exists(filepath.Join(home, ".npm", "_npx", "abc", "server.js")) {
		t.Error("npx's folder went")
	}
	if len(res.Left) != 1 || !strings.Contains(res.Left[0], "yarn is at work (node, pid 9)") {
		t.Errorf("left %v", res.Left)
	}
	// uv's cache is cleared by uv alone, which keeps its folder.
	if r.clean != 1 || !exists(places.UV) {
		t.Errorf("uv asked %d times; its folder there %v", r.clean, exists(places.UV))
	}
	// Without uv here, its cache stays: no one else may change it.
	blob(t, filepath.Join(places.UV, "wheels", "w"), time.Hour)
	places.UVBin = ""
	res, _ = Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, 10<<20)})
	if !exists(filepath.Join(places.UV, "wheels", "w")) || !strings.Contains(strings.Join(res.Left, "\n"), "uv is not here to clear uv's cache") {
		t.Errorf("uv's cache without uv: left %v", res.Left)
	}
}

func TestKeepClearsTheCopiesChromeLeftBehind(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(t.TempDir(), "X", chromeClones)
	clone := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		blob(t, filepath.Join(p, "Google Chrome.app.bundle", "Contents", "Info.plist"), age)
		aged(t, p, age)
		return p
	}
	stale := clone("code_sign_clone.aaa111", 48*time.Hour)
	current := clone("code_sign_clone.bbb222", time.Hour)
	running := clone("code_sign_clone.ccc333", 72*time.Hour)
	ps := "/bin/ps " + strings.Join(psArgs, " ")
	r := &fakeRunner{out: map[string]string{ps: "  5 1 501 100 0:01.00 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome\n" +
		"  6 5 501 100 0:01.00 " + running + "/Google Chrome.app.bundle/Contents/MacOS/helper\n"}}
	startsAt(t, map[int]time.Time{5: time.Now().Add(-61 * time.Minute)})
	res, _ := Keep(context.Background(), r, KeepInput{Home: home, Places: Places{Clones: dir}, Room: shortBy(filepath.Dir(dir), 10<<20)})
	if exists(stale) || !exists(current) || !exists(running) {
		t.Errorf("stale gone %v, the running Chrome's kept %v, one a program runs from kept %v", !exists(stale), exists(current), exists(running))
	}
	if len(res.Cleared) != 1 || res.Cleared[0].ID != "chrome-clones" || !strings.Contains(res.Cleared[0].What, "a copy Chrome made") {
		t.Errorf("cleared %+v", res.Cleared)
	}
	// With no Chrome running, a copy younger than ten minutes may be one
	// starting: it stays.
	young := clone("code_sign_clone.ddd444", 5*time.Minute)
	r.out[ps] = ""
	Keep(context.Background(), r, KeepInput{Home: home, Places: Places{Clones: dir}, Room: shortBy(filepath.Dir(dir), 10<<20)})
	if !exists(young) || exists(current) {
		t.Errorf("young kept %v, current gone %v", exists(young), !exists(current))
	}
}

func TestKeepClearsABrowsersCacheOnlyWhileItIsClosed(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the browsers' caches are a Mac's")
	}
	home := t.TempDir()
	caches := filepath.Join(home, "Library", "Caches")
	blob(t, filepath.Join(caches, "Google", "Chrome", "Default", "Cache", "data_1"), time.Hour)
	blob(t, filepath.Join(caches, "com.duckduckgo.macos.browser", "WebKit", "data"), time.Hour)
	r := &fakeRunner{out: map[string]string{
		"/bin/ps " + strings.Join(psArgs, " "): "",
		"/usr/bin/lsappinfo list": ` 1) "DuckDuckGo" ASN:0x0-0x1:
    bundleID="com.duckduckgo.macos.browser"
`,
	}}
	startsAt(t, map[int]time.Time{})
	res, _ := Keep(context.Background(), r, KeepInput{Home: home, Places: Places{Caches: caches}, Room: shortBy(home, 10<<20)})
	if exists(filepath.Join(caches, "Google", "Chrome", "Default")) || !exists(filepath.Join(caches, "Google", "Chrome")) {
		t.Error("a closed Chrome's cache stays, or its folder went")
	}
	if !exists(filepath.Join(caches, "com.duckduckgo.macos.browser", "WebKit", "data")) {
		t.Error("an open browser's cache went")
	}
	if len(res.Cleared) != 1 || res.Cleared[0].ID != "browser:com.google.Chrome" || len(res.Left) != 1 || !strings.Contains(res.Left[0], "DuckDuckGo is open") {
		t.Errorf("cleared %+v, left %v", res.Cleared, res.Left)
	}
}

func TestKeepGoesOldestAndSafestFirstAndStopsOnceTheBufferIsBack(t *testing.T) {
	home, places, r := keepMac(t, map[string]time.Duration{"00aa-d": 30 * time.Hour, "01bb-a": 20 * time.Hour}, "")
	startsAt(t, map[int]time.Time{})
	places.Pip = filepath.Join(home, "Library", "Caches", "pip")
	blob(t, filepath.Join(places.Pip, "http", "x"), time.Hour)
	// Go's build cache alone gives back what is short: pip's stays.
	res, _ := Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, 32<<10)})
	if len(res.Cleared) != 1 || res.Cleared[0].ID != "go-build" || !exists(filepath.Join(places.Pip, "http", "x")) || !exists(goEntry(places, "01bb-a")) {
		t.Errorf("cleared %+v", res.Cleared)
	}
	// Short by more, Go's build cache goes first, then pip's.
	res, _ = Keep(context.Background(), r, KeepInput{Home: home, Places: places, Room: shortBy(home, 10<<20)})
	if len(res.Cleared) != 2 || res.Cleared[0].ID != "go-build" || res.Cleared[1].ID != "pip" {
		t.Errorf("cleared %+v", res.Cleared)
	}
}
