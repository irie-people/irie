package wsl

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/irie-people/irie/internal/brand"
)

func utf16le(s string, bom bool) []byte {
	var b []byte
	if bom {
		b = append(b, 0xff, 0xfe)
	}
	for _, c := range utf16.Encode([]rune(s)) {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}

const table = "  NAME                   STATE           VERSION\r\n* Ubuntu                 Running         2\r\n  docker-desktop         Stopped         2\r\n  Debian                 Stopped         1\r\n"

func TestDecodeReadsBothOfWSLsEncodings(t *testing.T) {
	for name, b := range map[string][]byte{
		"utf-16le":        utf16le(table, false),
		"utf-16le, a BOM": utf16le(table, true),
		"utf-8":           []byte(table),
	} {
		if got := Decode(b); got != table {
			t.Errorf("%s: Decode = %q", name, got)
		}
	}
}

func TestParseListReadsTheTable(t *testing.T) {
	want := []Distro{
		{Name: "Ubuntu", State: "Running", Version: 2, Default: true},
		{Name: "docker-desktop", State: "Stopped", Version: 2},
		{Name: "Debian", State: "Stopped", Version: 1},
	}
	if got := ParseList(table); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseList = %+v", got)
	}
	// A header in another language, and the note of a WSL with none.
	if got := ParseList("  NOM      ÉTAT      VERSION\n* Ubuntu-24.04    En cours d'exécution    2\n"); len(got) != 1 || got[0].Name != "Ubuntu-24.04" || !got[0].Default || got[0].Version != 2 {
		t.Errorf("French table: %+v", got)
	}
	if got := ParseList("Windows Subsystem for Linux has no installed distributions.\nYou can resolve this by installing a distribution.\n"); len(got) != 0 {
		t.Errorf("no distributions: %+v", got)
	}
}

func TestChoosePicksWhereIrieRuns(t *testing.T) {
	ds := ParseList(table)
	for _, tc := range []struct {
		name  string
		ds    []Distro
		named string
		want  string
		ok    bool
	}{
		{"Ubuntu first", ds, "", "Ubuntu", true},
		{"the one named", ds, "debian", "Debian", true},
		{"named but missing", ds, "Fedora", "Fedora", false},
		{"the default on WSL 2", []Distro{{Name: "Ubuntu-24.04", Version: 2, Default: true}}, "", "Ubuntu-24.04", true},
		{"not a default on WSL 1", []Distro{{Name: "Debian", Version: 1, Default: true}}, "", DefaultDistro, false},
		{"not Docker's", []Distro{{Name: "docker-desktop", Version: 2, Default: true}}, "", DefaultDistro, false},
		{"none", nil, "", DefaultDistro, false},
	} {
		d, ok := Choose(tc.ds, tc.named)
		if d.Name != tc.want || ok != tc.ok {
			t.Errorf("%s: Choose = %q, %v; want %q, %v", tc.name, d.Name, ok, tc.want, tc.ok)
		}
	}
}

func TestBuildReadsVer(t *testing.T) {
	if n, ok := Build("\r\nMicrosoft Windows [Version 10.0.19045.3803]\r\n"); !ok || n != 19045 {
		t.Errorf("Build = %d, %v", n, ok)
	}
	if n, ok := Build("Microsoft Windows [versión 10.0.26100.2314]"); !ok || n != 26100 {
		t.Errorf("Build in Spanish = %d, %v", n, ok)
	}
	if _, ok := Build("no version here"); ok {
		t.Error("Build read a version from nothing")
	}
}

func TestLinuxPathIsWhereWSLMountsTheDrive(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\alice\Helper`: "/mnt/c/Users/alice/Helper",
		`D:/work/site`:          "/mnt/d/work/site",
		`@C:\Users\alice\site`:  "@/mnt/c/Users/alice/site",
		"~/Helper":              "~/Helper",
		"ctl":                   "ctl",
		`C:relative`:            `C:relative`,
	} {
		if got, _ := LinuxPath(in); got != want {
			t.Errorf("LinuxPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCommandRunsIrieThroughALoginShell(t *testing.T) {
	got := Command("Ubuntu", []string{"ctl", "spawn", "--cwd", `C:\Users\alice\site`, "fix the menu"})
	want := []string{"-d", "Ubuntu", "--cd", "~", "-e", "sh", "-lc",
		dataDir + `exec "$d/` + brand.Slug + `/bin/` + brand.Slug + `" "$@"`, brand.Slug,
		"ctl", "spawn", "--cwd", "/mnt/c/Users/alice/site", "fix the menu"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Command =\n%q\nwant\n%q", got, want)
	}
}

func TestKeepRunningEditsOnlyItsKey(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"no file", "", "[general]\ninstanceIdleTimeout=-1\n"},
		{"other sections", "[wsl2]\nmemory=8GB", "[wsl2]\nmemory=8GB\n\n[general]\ninstanceIdleTimeout=-1\n"},
		{"its section", "[General]\ndistributionInstallPath=D:\\\\wsl\n", "[General]\ninstanceIdleTimeout=-1\ndistributionInstallPath=D:\\\\wsl\n"},
		{"its key", "[wsl2]\nmemory=8GB\n[general]\ninstanceIdleTimeout = 15000 # the default\n", "[wsl2]\nmemory=8GB\n[general]\ninstanceIdleTimeout=-1\n"},
		{"Windows' line endings", "[wsl2]\r\nmemory=8GB\r\n", "[wsl2]\r\nmemory=8GB\r\n\r\n[general]\r\ninstanceIdleTimeout=-1\r\n"},
		{"the key in another section", "[wsl2]\ninstanceIdleTimeout=-1\n", "[wsl2]\ninstanceIdleTimeout=-1\n\n[general]\ninstanceIdleTimeout=-1\n"},
	} {
		got := KeepRunning(tc.in)
		if got != tc.want {
			t.Errorf("%s: KeepRunning =\n%q\nwant\n%q", tc.name, got, tc.want)
		}
		if !KeepsRunning(got) {
			t.Errorf("%s: KeepsRunning(KeepRunning) is false", tc.name)
		}
		if KeepsRunning(tc.in) {
			t.Errorf("%s: KeepsRunning before the edit", tc.name)
		}
	}
}

func TestEncodeIsWhatEncodedCommandTakes(t *testing.T) {
	got, err := base64.StdEncoding.DecodeString(Encode("Start-Process 'wsl.exe'"))
	if err != nil || Decode(got) != "Start-Process 'wsl.exe'" || !slices.Equal(got[:4], []byte{'S', 0, 't', 0}) {
		t.Errorf("Encode gave %q, %v", got, err)
	}
}

func TestShortcutScriptQuotesEveryPath(t *testing.T) {
	s := ShortcutScript(`C:\Users\o'brien\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Startup\X.lnk`, `C:\Users\o'brien\AppData\Local\x\x.exe`, "up", "Starts it", true)
	for _, want := range []string{
		`$l = 'C:\Users\o''brien\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Startup\X.lnk';`,
		`CreateShortcut($l)`,
		`-Path (Split-Path -Parent $l)`,
		`$s.TargetPath = 'C:\Users\o''brien\AppData\Local\x\x.exe'`,
		`$s.Arguments = 'up'`,
		`$s.WindowStyle = 7`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"`) {
		t.Errorf("script has a double quote, which PowerShell expands: %s", s)
	}
}

func TestFragmentAddsAProfile(t *testing.T) {
	b, err := Fragment(`C:\Users\alice\AppData\Local\x\bin\x.exe`)
	var f struct {
		Profiles []map[string]string `json:"profiles"`
	}
	if err != nil || json.Unmarshal(b, &f) != nil || len(f.Profiles) != 1 {
		t.Fatalf("Fragment = %s, %v", b, err)
	}
	if p := f.Profiles[0]; p["name"] != brand.Name || p["commandline"] != `"C:\Users\alice\AppData\Local\x\bin\x.exe"` {
		t.Errorf("profile = %v", p)
	}
}

func ids(steps []Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return out
}

func TestPlanOffersWhatIsMissingInOrder(t *testing.T) {
	ubuntu := Distro{Name: "Ubuntu", Version: 2}
	done := State{Build: 26100, WSL: true, Distro: ubuntu, HasDistro: true, Installed: true, KeepRunning: true, Shortcut: true, Startup: true}
	for _, tc := range []struct {
		name  string
		edit  func(*State)
		want  []string
		ready bool
	}{
		{"all done", func(*State) {}, nil, true},
		{"old Windows", func(s *State) { s.Build = 18363 }, []string{"windows"}, false},
		{"no WSL", func(s *State) { s.WSL, s.HasDistro, s.Installed = false, false, false }, []string{"wsl"}, false},
		{"no distribution", func(s *State) { s.HasDistro, s.Installed = false, false }, []string{"distro", "install"}, false},
		{"WSL 1", func(s *State) { s.Distro.Version, s.Installed = 1, false }, []string{"version", "install"}, false},
		{"only the extras", func(s *State) { s.KeepRunning, s.Shortcut, s.Startup = false, false, false }, []string{"keep", "shortcut", "startup"}, true},
	} {
		st := done
		tc.edit(&st)
		if got := ids(Plan(st)); !slices.Equal(got, tc.want) || Ready(st) != tc.ready {
			t.Errorf("%s: Plan = %v, Ready = %v; want %v, %v", tc.name, got, Ready(st), tc.want, tc.ready)
		}
	}
}

// machine stands in for Windows: what each command answers, and what the
// commands that change things do.
type machine struct {
	t       *testing.T
	e       Env
	build   string
	wsl     bool
	list    string
	irie    bool
	failRun string // a command, by its first words, that exits 1
	ran     []string
}

func (m *machine) Output(name string, args ...string) (string, error) {
	switch cmd := strings.Join(append([]string{name}, args...), " "); {
	case cmd == "cmd.exe /c ver":
		return m.build, nil
	case strings.HasPrefix(cmd, "wsl.exe") && !m.wsl:
		m.t.Errorf("asked wsl.exe %q with no WSL installed", args)
		return "", errors.New("exit status 1")
	case cmd == "wsl.exe --list --verbose":
		if ParseList(m.list) == nil {
			return "Windows Subsystem for Linux has no installed distributions.", errors.New("exit status 1")
		}
		return m.list, nil
	case slices.Equal(args, Installed("Ubuntu")):
		if m.irie {
			return "", nil
		}
		return "", errors.New("exit status 1")
	}
	m.t.Errorf("unexpected Output %s %q", name, args)
	return "", errors.New("unexpected")
}

func (m *machine) Run(name string, args ...string) (int, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	m.ran = append(m.ran, cmd)
	if m.failRun != "" && strings.HasPrefix(cmd, m.failRun) {
		return 1, nil
	}
	switch {
	case cmd == "wsl.exe --install -d Ubuntu":
		m.list = "  NAME    STATE    VERSION\n* Ubuntu  Running  2\n"
	case slices.Equal(args, Install("Ubuntu")):
		m.irie = true
	case name == "powershell.exe":
		b, _ := base64.StdEncoding.DecodeString(args[len(args)-1])
		script := Decode(b)
		for _, lnk := range []string{m.e.Shortcut(), m.e.Startup()} {
			if strings.Contains(script, quote(lnk)) {
				if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
					m.t.Fatal(err)
				}
				if err := os.WriteFile(lnk, nil, 0o644); err != nil {
					m.t.Fatal(err)
				}
			}
		}
	}
	return 0, nil
}

func newMachine(t *testing.T) *machine {
	root := t.TempDir()
	e := Env{Exe: filepath.Join(root, "Local", brand.Slug, "bin", brand.Slug+".exe"), Profile: filepath.Join(root, "alice"),
		AppData: filepath.Join(root, "Roaming"), LocalAppData: filepath.Join(root, "Local")}
	if err := os.MkdirAll(e.Profile, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &machine{t: t, e: e, build: "Microsoft Windows [Version 10.0.26100.2314]"}
	m.setWSL(true)
	return m
}

// setWSL puts the Store's WSL in, or takes it out.
func (m *machine) setWSL(on bool) {
	m.wsl = on
	m.e.ProgramFiles = filepath.Join(filepath.Dir(m.e.Profile), "Program Files")
	exe := filepath.Join(m.e.ProgramFiles, "WSL", "wsl.exe")
	if !on {
		os.Remove(exe)
		return
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		m.t.Fatal(err)
	}
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		m.t.Fatal(err)
	}
}

func TestSetupWalksEveryStepAfterAYes(t *testing.T) {
	m := newMachine(t)
	var out strings.Builder
	code := Setup(m, m.e, strings.NewReader("y\n\ny\nyes\nY\n"), &out, false, false)
	if code != 0 {
		t.Fatalf("Setup = %d:\n%s", code, out.String())
	}
	// The distribution, Irie in it, then the two shortcuts; .wslconfig is
	// written, not run.
	if len(m.ran) != 4 || m.ran[0] != "wsl.exe --install -d Ubuntu" || !strings.Contains(m.ran[1], "install.sh | sh") ||
		!strings.HasPrefix(m.ran[2], "powershell.exe") || !strings.HasPrefix(m.ran[3], "powershell.exe") {
		t.Errorf("ran %q", m.ran)
	}
	if b, err := os.ReadFile(filepath.Join(m.e.Profile, ".wslconfig")); err != nil || !KeepsRunning(string(b)) {
		t.Errorf(".wslconfig = %q, %v", b, err)
	}
	if _, err := os.Stat(m.e.Fragment()); err != nil {
		t.Errorf("no fragment: %v", err)
	}
	if !strings.Contains(out.String(), "is set up in Ubuntu") {
		t.Errorf("output:\n%s", out.String())
	}
	// Run again, it finds everything done and asks nothing.
	m.ran = nil
	if code := Setup(m, m.e, strings.NewReader(""), &out, false, false); code != 0 || len(m.ran) != 0 {
		t.Errorf("second Setup = %d, ran %q", code, m.ran)
	}
}

func TestSetupWithoutWSLStopsForARestart(t *testing.T) {
	m := newMachine(t)
	m.setWSL(false)
	var out strings.Builder
	if code := Setup(m, m.e, strings.NewReader("y\n"), &out, false, false); code != 1 {
		t.Errorf("Setup = %d", code)
	}
	if len(m.ran) != 1 || !strings.HasPrefix(m.ran[0], "powershell.exe") || !strings.Contains(out.String(), "Restart Windows") {
		t.Errorf("ran %q:\n%s", m.ran, out.String())
	}
}

func TestSetupStopsAtANoToWhatBlocks(t *testing.T) {
	m := newMachine(t)
	var out strings.Builder
	if code := Setup(m, m.e, strings.NewReader("n\n"), &out, false, false); code != 1 || len(m.ran) != 0 {
		t.Errorf("Setup = %d, ran %q", code, m.ran)
	}
	if !strings.Contains(out.String(), "can't run until it is done") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestSetupSkipsANoToAnExtra(t *testing.T) {
	m := newMachine(t)
	m.list, m.irie = "  NAME    STATE    VERSION\n* Ubuntu  Running  2\n", true
	var out strings.Builder
	// No to keeping it running, yes to the shortcut, no to sign-in.
	if code := Setup(m, m.e, strings.NewReader("n\ny\nn\n"), &out, false, false); code != 0 {
		t.Fatalf("Setup = %d:\n%s", code, out.String())
	}
	if len(m.ran) != 1 || exists(filepath.Join(m.e.Profile, ".wslconfig")) || !exists(m.e.Shortcut()) || exists(m.e.Startup()) {
		t.Errorf("ran %q", m.ran)
	}
}

func TestSetupStopsWhenAFixLeavesItsStepUndone(t *testing.T) {
	m := newMachine(t)
	m.list = "  NAME    STATE    VERSION\n* Ubuntu  Running  2\n"
	var out strings.Builder
	// The installer exits 0, yet Irie is not there afterwards.
	m.irie = false
	stub := &stubbornInstall{m}
	if code := Setup(stub, m.e, strings.NewReader("y\n"), &out, false, false); code != 1 || !strings.Contains(out.String(), "Not done yet") {
		t.Errorf("Setup = %d:\n%s", code, out.String())
	}
}

// stubbornInstall runs the installer, which leaves Irie uninstalled.
type stubbornInstall struct{ *machine }

func (s *stubbornInstall) Run(name string, args ...string) (int, error) {
	s.ran = append(s.ran, name)
	return 0, nil
}

func TestSetupCheckChangesNothing(t *testing.T) {
	m := newMachine(t)
	var out strings.Builder
	if code := Setup(m, m.e, strings.NewReader("y\ny\n"), &out, true, false); code != 1 || len(m.ran) != 0 {
		t.Errorf("Setup --check = %d, ran %q", code, m.ran)
	}
	for _, want := range []string{"Windows build 26100", "WSL            yes", "none (Ubuntu to install)", "- WSL has no Ubuntu"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
}

func TestSetupWithYesTakesEveryOffer(t *testing.T) {
	m := newMachine(t)
	var out strings.Builder
	if code := Setup(m, m.e, strings.NewReader(""), &out, false, true); code != 0 || len(m.ran) != 4 {
		t.Errorf("Setup --yes = %d, ran %q:\n%s", code, m.ran, out.String())
	}
}

func TestHasWSLLooksForEitherWSL(t *testing.T) {
	root := t.TempDir()
	e := Env{ProgramFiles: filepath.Join(root, "Program Files"), SystemRoot: filepath.Join(root, "Windows")}
	if e.HasWSL() {
		t.Error("HasWSL with neither")
	}
	inbox := filepath.Join(e.SystemRoot, "System32", "lxss", "LxssManager.dll")
	if err := os.MkdirAll(filepath.Dir(inbox), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inbox, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !e.HasWSL() {
		t.Error("HasWSL missed Windows' own WSL")
	}
	if (Env{}).HasWSL() {
		t.Error("HasWSL with no folders")
	}
}

func TestSaveDistroIsReadBack(t *testing.T) {
	m := newMachine(t)
	if err := SaveDistro(m.e, "Debian"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", m.e.LocalAppData)
	t.Setenv(brand.Env("WSL_DISTRO"), "")
	if e := EnvFromOS(); e.Distro != "Debian" {
		t.Errorf("Distro = %q", e.Distro)
	}
	t.Setenv(brand.Env("WSL_DISTRO"), "Ubuntu-24.04")
	if e := EnvFromOS(); e.Distro != "Ubuntu-24.04" {
		t.Errorf("Distro = %q with the variable set", e.Distro)
	}
}
