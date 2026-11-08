package raphael

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// cacheFloor is the smallest cache worth a line of its own.
const cacheFloor = 500 << 20

// mostCaches is the most caches he lists, the biggest first.
const mostCaches = 8

// cache is one folder of caches, as he weighs it.
type cache struct {
	name string // the folder's name
	path string
	size int64
}

// toolCaches are caches that a tool keeps and clears itself, which he
// names with advice but leaves to the tool: a program that runs as node or
// python can't be told from the rest, so he can't tell that none of them
// is in the middle of an install.
var toolCaches = map[string]string{
	"Yarn":     "Yarn's package cache: `yarn cache clean` clears it while no install runs.",
	"pip":      "pip's download cache: `pip3 cache purge` clears it while no install runs.",
	"node-gyp": "node-gyp's copies of Node's headers: it downloads them again when a native package builds.",
	"pnpm":     "pnpm's cache: `pnpm store prune` clears what no project uses.",
	"Cypress":  "Cypress's test browsers: `npx cypress cache prune` keeps only the one in use.",
}

// keepCaches are folders he never offers to clear: Irie's own, which
// Azrael looks after, browsers that tests run in, and the system's.
var keepCaches = []string{"irie", "ms-playwright", "CloudKit", "com.apple", "familycircled", "GeoServices", "FamilyCircle"}

// appCaches are folders that a known app keeps under a name of its own
// rather than its bundle id, by that name.
var appCaches = map[string]string{
	"Google":         "com.google.Chrome",
	"Firefox":        "org.mozilla.firefox",
	"Mozilla":        "org.mozilla.firefox",
	"Microsoft Edge": "com.microsoft.edgemac",
	"BraveSoftware":  "com.brave.Browser",
	"Spotify":        "com.spotify.client",
	"Slack":          "com.tinyspeck.slackmacgap",
	"discord":        "com.hnc.Discord",
}

// bundleName is the shape of a bundle id, such as com.spotify.client.
var bundleName = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9_-]+){2,}$`)

// cachesInput is what the look at caches needs.
type cachesInput struct {
	Home     string
	Procs    []Proc
	Disk     Disk
	Thorough bool // ask Homebrew what it would clean up
}

// cacheFindings weighs the biggest caches: Go's build cache and
// Homebrew's, which their tools clear, and the caches of apps that are not
// running, which go to the Trash. It returns the total too.
func cacheFindings(ctx context.Context, r Runner, in cachesInput) ([]Finding, int64) {
	dir := cachesDir(in.Home)
	ents, _ := os.ReadDir(dir)
	var paths []string
	for _, e := range ents {
		if e.IsDir() {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	goBin := findTool("go")
	goCache := ""
	if goBin != "" {
		if out, _, err := r.Run(ctx, goBin, "env", "GOCACHE"); err == nil {
			goCache = strings.TrimSpace(string(out))
		}
	}
	brewBin := findTool("brew")
	brewCache := ""
	if brewBin != "" {
		if out, _, err := r.Run(ctx, brewBin, "--cache"); err == nil {
			brewCache = strings.TrimSpace(string(out))
		}
	}
	for _, p := range []string{goCache, brewCache} {
		if p != "" && filepath.IsAbs(p) && !slices.Contains(paths, p) {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				paths = append(paths, p)
			}
		}
	}
	sizes := duSizes(ctx, r, paths)
	var total int64
	var caches []cache
	for _, p := range paths {
		n := sizes[p]
		if strings.HasPrefix(p, dir+"/") {
			total += n
		}
		if n >= cacheFloor {
			caches = append(caches, cache{name: filepath.Base(p), path: p, size: n})
		}
	}
	slices.SortFunc(caches, func(a, b cache) int {
		switch {
		case a.size > b.size:
			return -1
		case a.size < b.size:
			return 1
		}
		return strings.Compare(a.path, b.path)
	})
	running := runningApps(ctx, r)
	var fs []Finding
	for _, c := range caches {
		var f Finding
		switch {
		case c.path == goCache:
			f = goCacheFinding(c, goBin, in.Home)
		case c.path == brewCache:
			f = brewFinding(ctx, r, c, brewBin, in.Home, in.Thorough)
		case toolCaches[c.name] != "":
			f = Finding{ID: "cache:" + c.name, Area: AreaCaches, Level: Note, Size: c.size,
				Text: fmt.Sprintf("%s takes %s.", tildePath(c.path, in.Home), Size(c.size)), Advice: toolCaches[c.name]}
		case keep(c.name):
			continue
		default:
			id := appCaches[c.name]
			if id == "" && bundleName.MatchString(c.name) {
				id = c.name
			}
			if id == "" {
				continue // he can't tell whose it is
			}
			f = appCacheFinding(ctx, r, c, id, running, in)
		}
		if in.Disk.Low() && f.Level == Note {
			f.Level = Warn
		}
		fs = append(fs, f)
		if len(fs) == mostCaches {
			break
		}
	}
	return fs, total
}

// keep reports whether he never offers to clear the folder called name.
func keep(name string) bool {
	return slices.ContainsFunc(keepCaches, func(k string) bool { return name == k || strings.HasPrefix(name, k+".") })
}

// cachesDir is where apps keep their caches.
func cachesDir(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Caches")
	}
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return x
	}
	return filepath.Join(home, ".cache")
}

// findTool finds a tool by name: on the PATH, or where Homebrew and Go put
// it.
func findTool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/local/go/bin", "/home/linuxbrew/.linuxbrew/bin"} {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// duSizes measures each folder with one run of du, by its path: what it
// takes on the disk, without following links or leaving its volume.
func duSizes(ctx context.Context, r Runner, paths []string) map[string]int64 {
	out := map[string]int64{}
	if len(paths) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	b, _, _ := r.Run(ctx, "/usr/bin/du", append([]string{"-sk", "-x"}, paths...)...)
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		kb, path, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(kb), 10, 64); err == nil {
			out[path] = n << 10
		}
	}
	return out
}

// goCacheFinding is Go's build cache, which `go clean -cache` clears and Go
// fills again as it builds.
func goCacheFinding(c cache, goBin, home string) Finding {
	return Finding{
		ID: "cache:go-build", Area: AreaCaches, Level: Note, Size: c.size,
		Text:   fmt.Sprintf("Go's build cache takes %s (%s).", Size(c.size), tildePath(c.path, home)),
		Advice: "It is safe to clear: Go builds what it needs again, so the next builds and tests take longer.",
		Fix: &Fix{
			Label: "Clear Go's build cache",
			Run:   []string{command(tildePath(goBin, home), "clean", "-cache")},
			About: fmt.Sprintf("Frees about %s. Raffi waits until no Go build or test runs, so none of them breaks.", Size(c.size)),
			kind:  fixTool, tool: goBin, path: c.path, busy: []string{"go"},
		},
	}
}

// brewFinding is Homebrew's cache: what `brew cleanup -n` says it would
// remove (old versions and downloads it no longer needs), which `brew
// cleanup` then removes.
func brewFinding(ctx context.Context, r Runner, c cache, brewBin, home string, thorough bool) Finding {
	f := Finding{
		ID: "cache:homebrew", Area: AreaCaches, Level: Note, Size: c.size,
		Text: fmt.Sprintf("Homebrew keeps %s of downloads (%s).", Size(c.size), tildePath(c.path, home)),
	}
	frees := int64(-1)
	// The dry run reads the whole Cellar and can take minutes, on a busy
	// Mac more than an hour, so only a look the person asked for has it.
	if thorough {
		dctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		if out, _, err := r.Run(dctx, brewBin, "cleanup", "-n"); err == nil {
			frees = brewFrees(string(out))
		}
	}
	switch {
	case frees == 0:
		f.Advice = "`brew cleanup -n` finds nothing it no longer needs."
		return f
	case frees > 0:
		f.Size = frees
		f.Text = fmt.Sprintf("Homebrew can free %s of old versions and downloads it no longer needs.", Size(frees))
		f.Advice = "`brew cleanup -n` lists them; nothing you installed goes."
	default:
		f.Advice = "`brew cleanup` removes the old versions and downloads it no longer needs; nothing you installed goes."
	}
	about := "Removes what `brew cleanup -n` listed: old versions of what you installed, and downloads it no longer needs."
	if frees > 0 {
		about = fmt.Sprintf("Frees about %s: old versions of what you installed, and downloads it no longer needs.", Size(frees))
	}
	f.Fix = &Fix{
		Label: "Clean up Homebrew",
		Run:   []string{command(tildePath(brewBin, home), "cleanup")},
		About: about,
		kind:  fixTool, tool: brewBin, path: c.path, busy: []string{"brew"},
	}
	return f
}

// brewWould is the line that ends `brew cleanup -n`.
var brewWould = regexp.MustCompile(`would free approximately ([0-9.]+)\s*([KMGT]?B) of disk space`)

// brewFrees reads what `brew cleanup -n` says it would free; 0 for
// nothing, -1 when it does not say. Homebrew counts in powers of 1000
// (Formatter.disk_usage_readable).
func brewFrees(out string) int64 {
	m := brewWould.FindStringSubmatch(out)
	if m == nil {
		if strings.Contains(out, "Would remove") {
			return -1
		}
		return 0
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return -1
	}
	unit := map[string]float64{"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12}[m[2]]
	return int64(n * unit)
}

// appCacheFinding is the cache of the app with bundle id id: offered to the
// Trash while the app is not running.
func appCacheFinding(ctx context.Context, r Runner, c cache, id string, running map[string]string, in cachesInput) Finding {
	app := appPath(ctx, r, id)
	name := c.name
	if n, ok := runningAs(id, running); ok && n != "" {
		name = n
	} else if app != "" {
		name = appName(app)
	}
	f := Finding{
		ID: "cache:" + c.name, Area: AreaCaches, Level: Note, Size: c.size,
		Text: fmt.Sprintf("%s's cache takes %s (%s).", name, Size(c.size), tildePath(c.path, in.Home)),
	}
	if appRunning(id, app, running, in.Procs) {
		f.Advice = fmt.Sprintf("%s is open now. Quit it, and Raffi can put its cache in the Trash.", name)
		return f
	}
	if app == "" {
		f.Text = fmt.Sprintf("%s takes %s, the cache of an app that seems no longer here.", tildePath(c.path, in.Home), Size(c.size))
	}
	f.Advice = "An app makes its cache again as it needs: clearing it frees room, and the app may start slower once."
	trash := filepath.Join(in.Home, ".Trash")
	f.Fix = &Fix{
		Label: "Put " + name + "'s cache in the Trash",
		Run:   []string{command("mv", tildePath(c.path, in.Home), tildePath(trash, in.Home)+"/")},
		About: fmt.Sprintf("Moves the folder to the Trash, which frees %s once you empty it. Until then you can put it back.", Size(c.size)),
		Undo:  "Drag it from the Trash back to " + tildePath(filepath.Dir(c.path), in.Home) + ".",
		kind:  fixTrash, path: c.path, ident: id,
	}
	return f
}

// appPath finds the app with bundle id id with Spotlight; "" when it is
// not here, or Spotlight does not know.
func appPath(ctx context.Context, r Runner, id string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	out, _, err := r.Run(ctx, "/usr/bin/mdfind", fmt.Sprintf("kMDItemCFBundleIdentifier == %q", id))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); strings.HasSuffix(line, ".app") {
			return line
		}
	}
	return ""
}

// runningApps are the apps that run now, by `lsappinfo list`: their names
// by their bundle ids.
func runningApps(ctx context.Context, r Runner) map[string]string {
	if runtime.GOOS != "darwin" {
		return map[string]string{}
	}
	out, _, err := r.Run(ctx, "/usr/bin/lsappinfo", "list")
	if err != nil {
		return map[string]string{}
	}
	return parseApps(string(out))
}

var (
	appLine       = regexp.MustCompile(`^\s*\d+\) "(.*)" ASN:`)
	bundleIDField = regexp.MustCompile(`^\s*bundleID="([^"]+)"`)
)

// parseApps reads `lsappinfo list`: each app's name on its first line, and
// its bundle id on a line after.
func parseApps(out string) map[string]string {
	apps := map[string]string{}
	name := ""
	for _, line := range strings.Split(out, "\n") {
		if m := appLine.FindStringSubmatch(line); m != nil {
			name = m[1]
			continue
		}
		if m := bundleIDField.FindStringSubmatch(line); m != nil {
			apps[m[1]] = name
		}
	}
	return apps
}

// runningAs finds the running app that the bundle id id is, or is part of
// (com.x.app for com.x.app.ShipIt), and returns its name; ok is false when
// none runs.
func runningAs(id string, running map[string]string) (name string, ok bool) {
	// The app itself, or the one it is part of, before any of its helpers,
	// and the helper nearest it after.
	if n, ok := running[id]; ok {
		return n, true
	}
	best := ""
	for r := range running {
		switch {
		case strings.HasPrefix(id, r+"."):
			if !strings.HasPrefix(best, id) && len(r) > len(best) || strings.HasPrefix(best, id+".") {
				best = r
			}
		case strings.HasPrefix(r, id+"."):
			if best == "" || strings.HasPrefix(best, id+".") && len(r) < len(best) {
				best = r
			}
		}
	}
	if best == "" {
		return "", false
	}
	return running[best], true
}

// appRunning reports whether the app with bundle id id runs: it, or an app
// it is part of, is among the running apps, or a process runs from inside
// its bundle.
func appRunning(id, app string, running map[string]string, ps []Proc) bool {
	if _, ok := runningAs(id, running); ok {
		return true
	}
	if app != "" {
		for _, p := range ps {
			if strings.HasPrefix(p.Path, app+"/") {
				return true
			}
		}
	}
	return false
}
