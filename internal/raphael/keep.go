package raphael

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/sysload"
)

// Keeping room for swap (DESIGN §4.11, §2.9). macOS adds a swap file when
// the memory it compresses runs out of room, on the startup disk's VM
// volume, which shares its free space with the person's files. On
// 2026-11-23 a disk 97% full left the swap no room to grow, and the kernel
// panicked. So while the free disk is under sysload.Room's buffer, Raffi
// clears by himself, with no press, caches that their tools make again,
// oldest and safest first, reading the room again after each, until the
// buffer is back: Go's build cache by age, Homebrew's downloads, the Yarn,
// pip, uv and npm caches, Xcode's DerivedData, the copies of itself that
// Chrome left behind, and a browser's cache once that browser is closed.
// Right before each he checks again that it is safe: the folder is still
// that cache, its tool idle, its app closed. The person's own files are
// never his to clear: they get findings whose fixes ask first (own.go).

// Cleared is what one clear did.
type Cleared struct {
	At    time.Time
	ID    string // which cache, such as go-build
	What  string // in words
	Path  string // where, ~ for the home folder
	Size  int64  // what it took on the disk
	Freed int64  // what the free disk gained, which other programs move too
	Free  int64  // the free disk after it
	Error string // why it stopped short, when it did
}

// KeepResult is what keeping room did.
type KeepResult struct {
	Cleared []Cleared
	Left    []string     // what he left be, and why, a line each: a tool at work, an app open
	Room    sysload.Room // the room after
}

// Places are where the caches he clears are; "" for one this machine
// doesn't have.
type Places struct {
	GoCache     string // go env GOCACHE
	BrewCache   string // brew --cache
	Yarn        string // Yarn's (1.x) cache
	Pip         string // pip's cache
	UV          string // uv's cache
	UVBin       string // uv, which alone may clear its cache
	NPM         string // npm's content cache, <cache>/_cacache
	DerivedData string // Xcode's
	Clones      string // where Chrome keeps copies of itself as it runs
	Caches      string // where apps keep their caches, ~/Library/Caches on a Mac
}

// FindPlaces finds where the caches are on this machine: where each tool
// says, or keeps its cache by default, as its documentation says.
func FindPlaces(ctx context.Context, r Runner, home string) Places {
	p := Places{Caches: cachesDir(home)}
	if goBin := findTool("go"); goBin != "" {
		if out, _, err := r.Run(ctx, goBin, "env", "GOCACHE"); err == nil {
			p.GoCache = strings.TrimSpace(string(out))
		}
	}
	if brewBin := findTool("brew"); brewBin != "" {
		if out, _, err := r.Run(ctx, brewBin, "--cache"); err == nil {
			p.BrewCache = strings.TrimSpace(string(out))
		}
	}
	xdg := os.Getenv("XDG_CACHE_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".cache")
	}
	mac := runtime.GOOS == "darwin"
	envOr := func(key, mac, other string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if runtime.GOOS == "darwin" {
			return mac
		}
		return other
	}
	p.Yarn = envOr("YARN_CACHE_FOLDER", filepath.Join(home, "Library", "Caches", "Yarn"), filepath.Join(xdg, "yarn"))
	p.Pip = envOr("PIP_CACHE_DIR", filepath.Join(home, "Library", "Caches", "pip"), filepath.Join(xdg, "pip"))
	p.UV = envOr("UV_CACHE_DIR", filepath.Join(xdg, "uv"), filepath.Join(xdg, "uv"))
	p.UVBin = findToolAt("uv", filepath.Join(home, ".local", "bin"), filepath.Join(home, ".cargo", "bin"))
	p.NPM = filepath.Join(envOr("npm_config_cache", filepath.Join(home, ".npm"), filepath.Join(home, ".npm")), "_cacache")
	if mac {
		p.DerivedData = filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData")
		if out, _, err := r.Run(ctx, "/usr/bin/getconf", "DARWIN_USER_TEMP_DIR"); err == nil {
			if t := strings.TrimSpace(string(out)); filepath.IsAbs(t) {
				p.Clones = filepath.Join(filepath.Dir(filepath.Clean(t)), "X", chromeClones)
			}
		}
	}
	return p
}

// KeepInput is what keeping room needs.
type KeepInput struct {
	Home   string
	Now    time.Time
	Places Places
	// Room reads the machine's room: before the first clear, and again
	// after each.
	Room func() (sysload.Room, error)
	// DryRun measures what he would clear while the disk is short, and
	// clears nothing.
	DryRun bool
}

// Keep clears caches, oldest and safest first, while the free disk is
// under the buffer, and stops once it is back. With DryRun it measures all
// he would clear, short or not.
func Keep(ctx context.Context, r Runner, in KeepInput) (KeepResult, error) {
	room, err := in.Room()
	res := KeepResult{Room: room}
	if err != nil || !room.Known() {
		return res, err
	}
	if !in.DryRun && !room.Short() {
		return res, nil
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	k := &keeper{r: r, in: in, room: room}
	for _, clear := range []func(context.Context) []Cleared{k.goBuild, k.brew, k.tools, k.derivedData, k.clones, k.browsers} {
		if ctx.Err() != nil || !in.DryRun && !k.room.Short() {
			break
		}
		res.Cleared = append(res.Cleared, clear(ctx)...)
	}
	res.Left, res.Room = k.left, k.room
	return res, ctx.Err()
}

// keeper is one run of Keep.
type keeper struct {
	r    Runner
	in   KeepInput
	room sysload.Room
	left []string
}

// need is what the buffer lacks now; in a dry run, all there is.
func (k *keeper) need() int64 {
	if k.in.DryRun {
		return 1 << 62
	}
	return max(0, k.room.Buffer()-k.room.DiskFree)
}

// done reads the room again after a clear, and fills in what it freed.
func (k *keeper) done(c Cleared) Cleared {
	c.At, c.Path = time.Now(), tildePath(c.Path, k.in.Home)
	before := k.room.DiskFree
	if !k.in.DryRun {
		if room, err := k.in.Room(); err == nil && room.Known() {
			k.room = room
		}
	}
	c.Freed, c.Free = max(0, k.room.DiskFree-before), k.room.DiskFree
	return c
}

// leave says what he left be, and why.
func (k *keeper) leave(format string, args ...any) {
	k.left = append(k.left, fmt.Sprintf(format, args...))
}

// procs reads the processes right before a clear, with their start times
// where the system says.
func (k *keeper) procs(ctx context.Context) ([]Proc, map[int]time.Time, error) {
	ps, err := Procs(ctx, k.r)
	if err != nil {
		return nil, nil, err
	}
	starts, err := procStarts()
	if err != nil {
		return nil, nil, err
	}
	return ps, starts, nil
}

// procStarts reads when each process started, by its pid, where the
// system says; a test replaces it.
var procStarts = func() (map[int]time.Time, error) {
	t, err := readTable()
	if err != nil {
		return nil, err
	}
	starts := map[int]time.Time{}
	for _, p := range t.All() {
		if at, ok := p.Started(); ok {
			starts[p.PID] = at
		}
	}
	return starts, nil
}

// realDir reports whether path is a folder itself, not a link to one, at
// an absolute path other than the home folder or the root.
func realDir(path, home string) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) == filepath.Clean(home) || filepath.Clean(path) == "/" {
		return false
	}
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir()
}

// goReadme is the first line of the README Go writes in its build cache
// (cmd/go/internal/cache), by which he knows the folder is Go's.
const goReadme = "This directory holds cached build artifacts from the Go build system."

// goUseLag is how much older than its last use a Go cache entry's time
// may be: Go sets an entry's time as it uses it only when that is an hour
// old or more (cmd/go/internal/cache's mtimeInterval).
const goUseLag = time.Hour

// goBuild trims Go's build cache by age, the oldest entries first, as Go's
// own trim does after five days: only entries no build at work can be
// reading, untouched for an hour more than the oldest Go command running
// has run, and for an hour at least.
func (k *keeper) goBuild(ctx context.Context) []Cleared {
	dir := k.in.Places.GoCache
	if !realDir(dir, k.in.Home) {
		return nil
	}
	if b, err := os.ReadFile(filepath.Join(dir, "README")); err != nil || !strings.HasPrefix(string(b), goReadme) {
		k.leave("%s does not look like Go's build cache, so %s left it be", tildePath(dir, k.in.Home), brand.He)
		return nil
	}
	ps, starts, err := k.procs(ctx)
	if err != nil {
		return nil
	}
	cutoff := k.in.Now.Add(-goUseLag)
	for _, p := range ps {
		if goCommand(p) {
			if at, ok := starts[p.PID]; ok && at.Add(-goUseLag).Before(cutoff) {
				cutoff = at.Add(-goUseLag)
			}
		}
	}
	type entry struct {
		path string
		mod  time.Time
		size int64
		dir  bool
	}
	var old []entry
	for i := 0; i < 256; i++ {
		sub := filepath.Join(dir, fmt.Sprintf("%02x", i))
		names, _ := readNames(sub)
		for _, name := range names {
			if !strings.HasSuffix(name, "-a") && !strings.HasSuffix(name, "-d") {
				continue
			}
			p := filepath.Join(sub, name)
			fi, err := os.Lstat(p)
			if err != nil || !fi.ModTime().Before(cutoff) || fi.Mode()&fs.ModeSymlink != 0 {
				continue
			}
			size := allocated(fi)
			if fi.IsDir() {
				size = treeSize(p)
			}
			old = append(old, entry{p, fi.ModTime(), size, fi.IsDir()})
		}
	}
	slices.SortFunc(old, func(a, b entry) int { return a.mod.Compare(b.mod) })
	need := k.need()
	var size int64
	var n int
	var youngest time.Time
	for _, e := range old {
		if size >= need {
			break
		}
		if !k.in.DryRun {
			// Checked again right before: a build may have used it since.
			fi, err := os.Lstat(e.path)
			if err != nil || !fi.ModTime().Before(cutoff) {
				continue
			}
			if e.dir {
				err = os.RemoveAll(e.path)
			} else {
				err = os.Remove(e.path)
			}
			if err != nil {
				continue
			}
		}
		size += e.size
		n++
		youngest = e.mod
	}
	if n == 0 {
		if len(old) == 0 {
			k.leave("Go's build cache had nothing untouched for %s", For(k.in.Now.Sub(cutoff)))
		}
		return nil
	}
	what := fmt.Sprintf("Go's build cache: the %d oldest entries, untouched for %s or more", n, For(k.in.Now.Sub(youngest)))
	return []Cleared{k.done(Cleared{ID: "go-build", What: what, Path: dir, Size: size})}
}

// findToolAt finds a tool as findTool does, or else in one of dirs, where
// an installer for the person's own tools puts it.
func findToolAt(name string, dirs ...string) string {
	if p := findTool(name); p != "" {
		return p
	}
	for _, dir := range dirs {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// goCommand reports whether p is the go command or a tool of Go's
// toolchain, which read the build cache. A test binary runs from its own
// folder, not the cache.
func goCommand(p Proc) bool {
	return filepath.Base(p.Path) == "go" || strings.Contains(p.Path, "/pkg/tool/")
}

// readNames lists a folder's names.
func readNames(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Readdirnames(-1)
}

// brew clears Homebrew's downloads, as `brew cleanup --prune=all` does,
// while brew does not run: the files in its cache's downloads folder, and
// the links to them beside it. It leaves the rest of Homebrew's cache be.
func (k *keeper) brew(ctx context.Context) []Cleared {
	dir := k.in.Places.BrewCache
	if !realDir(dir, k.in.Home) || !realDir(filepath.Join(dir, "downloads"), k.in.Home) {
		return nil
	}
	ps, _, err := k.procs(ctx)
	if err != nil {
		return nil
	}
	if who := busyWith(ps, []string{"brew"}); who != "" {
		k.leave("Homebrew is at work (%s), so %s left its downloads be", who, brand.He)
		return nil
	}
	var size int64
	var errs []string
	downloads := filepath.Join(dir, "downloads")
	names, _ := readNames(downloads)
	for _, name := range names {
		n, err := k.remove(filepath.Join(downloads, name))
		size += n
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	// The links to them, beside the downloads folder and in Cask.
	for _, d := range []string{dir, filepath.Join(dir, "Cask")} {
		names, _ := readNames(d)
		for _, name := range names {
			p := filepath.Join(d, name)
			if fi, err := os.Lstat(p); err == nil && fi.Mode()&fs.ModeSymlink != 0 && !k.in.DryRun {
				os.Remove(p)
			}
		}
	}
	if size == 0 {
		return nil
	}
	return []Cleared{k.done(Cleared{ID: "homebrew", What: "Homebrew's downloads", Path: downloads, Size: size, Error: firstErr(errs)})}
}

// toolCache is a cache a tool makes again as it needs, and which programs
// tell it is at work.
type toolCache struct {
	id, what string
	path     func(Places) string
	busy     func(Proc, []string) bool // the process, with its arguments where it is node or python
	// clean is the command with which the tool clears its cache, for one
	// whose cache only it may change; nil to empty the folder.
	clean func(Places) []string
}

// keptToolCaches are the tools' caches he clears, in the order he clears
// them.
var keptToolCaches = []toolCache{
	{"yarn", "Yarn's package cache", func(p Places) string { return p.Yarn }, func(p Proc, args []string) bool {
		return progIs(p, "yarn", "yarnpkg") || runs(p, args, "node", "yarn")
	}, nil},
	{"pip", "pip's download cache", func(p Places) string { return p.Pip }, func(p Proc, args []string) bool {
		return strings.HasPrefix(filepath.Base(p.Path), "pip") || runs(p, args, "python", "pip")
	}, nil},
	// uv's cache is uv's alone to change, under its own lock: `uv cache
	// clean` clears it.
	{"uv", "uv's cache", func(p Places) string { return p.UV }, func(p Proc, _ []string) bool { return progIs(p, "uv", "uvx") }, func(p Places) []string {
		if p.UVBin == "" {
			return []string{}
		}
		return []string{p.UVBin, "cache", "clean"}
	}},
	{"npm", "npm's package cache", func(p Places) string { return p.NPM }, func(p Proc, args []string) bool {
		return progIs(p, "npm", "npx") || runs(p, args, "node", "npm") || runs(p, args, "node", "npx")
	}, nil},
}

// procArgs reads a process's arguments; a test replaces it.
var procArgs = func(pid int) []string {
	argv, _, err := proc.Args(pid)
	if err != nil {
		return nil
	}
	return argv
}

// progIs reports whether p's program is one of names.
func progIs(p Proc, names ...string) bool { return slices.Contains(names, filepath.Base(p.Path)) }

// runs reports whether p is the interpreter whose name starts with interp,
// running a script or module whose name holds tool.
func runs(p Proc, args []string, interp, tool string) bool {
	if !strings.HasPrefix(filepath.Base(p.Path), interp) {
		return false
	}
	for _, a := range args[min(1, len(args)):] {
		if strings.Contains(filepath.Base(a), tool) || a == tool {
			return true
		}
	}
	return false
}

// tools clears the Yarn, pip, uv and npm caches, each while its tool does
// not run, keeping each cache's folder.
func (k *keeper) tools(ctx context.Context) []Cleared {
	var out []Cleared
	for _, t := range keptToolCaches {
		if ctx.Err() != nil || !k.in.DryRun && !k.room.Short() {
			break
		}
		dir := t.path(k.in.Places)
		if !realDir(dir, k.in.Home) || !strings.HasPrefix(dir, k.in.Home+"/") {
			continue
		}
		if !cacheLike(dir, t.id) {
			k.leave("%s is where %s says its cache is, but its name is no cache's, so %s left it be", tildePath(dir, k.in.Home), t.id, brand.He)
			continue
		}
		ps, _, err := k.procs(ctx)
		if err != nil {
			return out
		}
		at := ""
		for _, p := range ps {
			var args []string
			if b := filepath.Base(p.Path); strings.HasPrefix(b, "node") || strings.HasPrefix(b, "python") {
				args = procArgs(p.PID)
			}
			if t.busy(p, args) {
				at = fmt.Sprintf("%s, pid %d", filepath.Base(p.Path), p.PID)
				break
			}
		}
		if at != "" {
			k.leave("%s is at work (%s), so %s left %s be", t.id, at, brand.He, t.what)
			continue
		}
		var size int64
		if t.clean == nil {
			size, err = k.emptyDir(dir)
		} else if size, err = k.toolClean(ctx, dir, t.clean(k.in.Places)); err == errNoTool {
			k.leave("%s is not here to clear %s, which only it may change", t.id, t.what)
			continue
		}
		if size == 0 {
			continue
		}
		out = append(out, k.done(Cleared{ID: t.id, What: t.what, Path: dir, Size: size, Error: errText(err)}))
	}
	return out
}

// cacheLike reports whether a tool's cache folder is named as a cache is:
// the tool's name, as Yarn's and pip's are by default, or a name that says
// cache, as npm's _cacache does. A folder a setting names otherwise, such
// as ~/Documents, is never emptied.
func cacheLike(dir, tool string) bool {
	name := strings.ToLower(filepath.Base(dir))
	return name == tool || strings.Contains(name, "cache")
}

// errNoTool says the tool that clears its own cache is not here.
var errNoTool = errors.New("the tool is not here")

// toolClean has a tool clear its own cache with cmd, and says what that
// freed of dir; in a dry run it only measures.
func (k *keeper) toolClean(ctx context.Context, dir string, cmd []string) (int64, error) {
	if len(cmd) == 0 {
		return 0, errNoTool
	}
	before := treeSize(dir)
	if k.in.DryRun || before == 0 {
		return before, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	_, errb, err := k.r.Run(cctx, cmd[0], cmd[1:]...)
	freed := max(0, before-treeSize(dir))
	if err != nil {
		return freed, fmt.Errorf("%s: %v: %s", command(cmd...), err, cut(strings.TrimSpace(string(errb)), 200))
	}
	return freed, nil
}

// derivedData clears Xcode's DerivedData, the oldest project's first,
// while Xcode and its builds do not run.
func (k *keeper) derivedData(ctx context.Context) []Cleared {
	dir := k.in.Places.DerivedData
	if !realDir(dir, k.in.Home) {
		return nil
	}
	ps, _, err := k.procs(ctx)
	if err != nil {
		return nil
	}
	running := runningApps(ctx, k.r)
	if _, ok := running["com.apple.dt.Xcode"]; ok {
		k.leave("Xcode is open, so %s left its DerivedData be", brand.He)
		return nil
	}
	for _, p := range ps {
		if strings.Contains(p.Path, "/Xcode.app/") || progIs(p, "xcodebuild") {
			k.leave("Xcode is at work (%s, pid %d), so %s left its DerivedData be", filepath.Base(p.Path), p.PID, brand.He)
			return nil
		}
	}
	return k.oldestFirst(dir, "derived-data", func(n int) string {
		return fmt.Sprintf("Xcode's DerivedData: the %s", plural(n, "oldest project's build", "%d oldest projects' builds"))
	}, func(string, fs.FileInfo) bool { return true })
}

// chromeClones is the folder Chrome keeps copies of itself in as it runs,
// in the per-user folder beside the temporary one, so that an update
// doesn't change the program under a running Chrome.
const chromeClones = "com.google.Chrome.code_sign_clone"

// clones clears the copies of itself that Chrome left behind: each running
// Chrome makes one as it starts, so one older than every Chrome running,
// or than ten minutes when none runs, is no running Chrome's. Copies are
// clones on APFS, so the disk gains less than they seem to take.
func (k *keeper) clones(ctx context.Context) []Cleared {
	dir := k.in.Places.Clones
	if !realDir(dir, k.in.Home) || filepath.Base(dir) != chromeClones {
		return nil
	}
	ps, starts, err := k.procs(ctx)
	if err != nil {
		return nil
	}
	before := k.in.Now.Add(-10 * time.Minute)
	for _, p := range ps {
		if strings.Contains(p.Path, "/Google Chrome.app/") {
			if at, ok := starts[p.PID]; ok && at.Add(-time.Minute).Before(before) {
				before = at.Add(-time.Minute)
			} else if !ok {
				before = time.Time{} // a Chrome whose start can't be read: none is surely stale
			}
		}
	}
	inUse := func(path string) bool {
		return slices.ContainsFunc(ps, func(p Proc) bool { return strings.HasPrefix(p.Path, path+"/") })
	}
	return k.oldestFirst(dir, "chrome-clones", func(n int) string {
		return plural(n, "a copy Chrome made of itself as it ran, and left behind", "%d copies Chrome made of itself as it ran, and left behind")
	}, func(path string, fi fs.FileInfo) bool {
		return strings.HasPrefix(filepath.Base(path), "code_sign_clone.") && fi.ModTime().Before(before) && !inUse(path)
	})
}

// browserCache is a browser's cache, in the caches folder.
type browserCache struct {
	dir  string // under the caches folder
	id   string // the browser's bundle id
	name string
	app  string // its app bundle's name
}

// browserCaches are the browsers whose caches he clears while they are
// closed: Chromium's browsers keep theirs under the caches folder as their
// data folder is named under Application Support, Firefox its profiles'.
var browserCaches = []browserCache{
	{"Google/Chrome", "com.google.Chrome", "Chrome", "Google Chrome.app"},
	{"BraveSoftware/Brave-Browser", "com.brave.Browser", "Brave", "Brave Browser.app"},
	{"Microsoft Edge", "com.microsoft.edgemac", "Edge", "Microsoft Edge.app"},
	{"Firefox", "org.mozilla.firefox", "Firefox", "Firefox.app"},
	{"com.duckduckgo.macos.browser", "com.duckduckgo.macos.browser", "DuckDuckGo", "DuckDuckGo.app"},
}

// browsers clears each browser's cache while that browser is closed,
// keeping its folder: the browser makes it again as it browses.
func (k *keeper) browsers(ctx context.Context) []Cleared {
	if runtime.GOOS != "darwin" || k.in.Places.Caches == "" {
		return nil
	}
	var out []Cleared
	for _, b := range browserCaches {
		if ctx.Err() != nil || !k.in.DryRun && !k.room.Short() {
			break
		}
		dir := filepath.Join(k.in.Places.Caches, b.dir)
		if !realDir(dir, k.in.Home) {
			continue
		}
		ps, _, err := k.procs(ctx)
		if err != nil {
			return out
		}
		open := appRunning(b.id, appPath(ctx, k.r, b.id), runningApps(ctx, k.r), ps) ||
			slices.ContainsFunc(ps, func(p Proc) bool { return strings.Contains(p.Path, "/"+b.app+"/") })
		if open {
			k.leave("%s is open, so %s left its cache be", b.name, brand.He)
			continue
		}
		size, err := k.emptyDir(dir)
		if size == 0 {
			continue
		}
		out = append(out, k.done(Cleared{ID: "browser:" + b.id, What: b.name + "'s cache, while " + b.name + " was closed", Path: dir, Size: size, Error: errText(err)}))
	}
	return out
}

// oldestFirst removes the entries of dir that ok picks, the oldest first,
// until the buffer is back.
func (k *keeper) oldestFirst(dir, id string, what func(n int) string, ok func(string, fs.FileInfo) bool) []Cleared {
	names, _ := readNames(dir)
	type entry struct {
		path string
		fi   fs.FileInfo
	}
	var es []entry
	for _, name := range names {
		p := filepath.Join(dir, name)
		if fi, err := os.Lstat(p); err == nil && ok(p, fi) {
			es = append(es, entry{p, fi})
		}
	}
	slices.SortFunc(es, func(a, b entry) int { return a.fi.ModTime().Compare(b.fi.ModTime()) })
	need := k.need()
	var size int64
	var n int
	var errs []string
	for _, e := range es {
		if size >= need {
			break
		}
		got, err := k.remove(e.path)
		size += got
		if got > 0 {
			n++
		}
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	if size == 0 {
		return nil
	}
	return []Cleared{k.done(Cleared{ID: id, What: what(n), Path: dir, Size: size, Error: firstErr(errs)})}
}

// emptyDir removes what dir holds, keeping dir, and says what that took.
func (k *keeper) emptyDir(dir string) (int64, error) {
	names, err := readNames(dir)
	if err != nil {
		return 0, err
	}
	var size int64
	var errs []string
	for _, name := range names {
		n, err := k.remove(filepath.Join(dir, name))
		size += n
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return size, errors.New(firstErr(errs))
	}
	return size, nil
}

// remove removes path, a file or a folder with all it holds, and says
// what that took on the disk; in a dry run it only measures.
func (k *keeper) remove(path string) (int64, error) {
	size := treeSize(path)
	if k.in.DryRun || size == 0 {
		return size, nil
	}
	if err := os.RemoveAll(path); err != nil {
		return size - treeSize(path), err
	}
	return size, nil
}

// treeSize is what path takes on the disk, a folder with all it holds,
// not following links.
func treeSize(path string) int64 {
	var n int64
	filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			n += allocated(fi)
		}
		return nil
	})
	return n
}

// firstErr is the first of errs, with how many more there were.
func firstErr(errs []string) string {
	switch len(errs) {
	case 0:
		return ""
	case 1:
		return errs[0]
	}
	return fmt.Sprintf("%s (and %d more)", errs[0], len(errs)-1)
}

// errText is err's text, or "" for none.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
