package lid

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
)

// fake is a Mac whose tools answer from out, by the tool's base name and
// its arguments, and which records each run.
type fake struct {
	out  map[string]string
	fail map[string]bool
	ran  []string
}

func (f *fake) mac() Mac {
	return Mac{Run: func(name string, args ...string) (string, error) {
		line := strings.Join(append([]string{filepath.Base(name)}, args...), " ")
		f.ran = append(f.ran, line)
		if f.fail[line] {
			return f.out[line], errors.New("exit status 1")
		}
		return f.out[line], nil
	}}
}

func TestParsePowerReadsWhatTheMacRunsOn(t *testing.T) {
	for _, c := range []struct {
		out  string
		want Power
	}{
		{"Now drawing from 'Battery Power'\n -InternalBattery-0 (id=6488163)\t15%; discharging; 0:36 remaining present: true\n", Power{Battery: true, Percent: 15}},
		{"Now drawing from 'AC Power'\n -InternalBattery-0 (id=6488163)\t100%; charged; 0:00 remaining present: true\n", Power{Battery: false, Percent: 100}},
		{"Now drawing from 'AC Power'\n", Power{Battery: false, Percent: -1}},
		{"", Power{Percent: -1}},
	} {
		if got := ParsePower(c.out); got != c.want {
			t.Errorf("ParsePower(%q) = %+v, want %+v", c.out, got, c.want)
		}
	}
}

func TestParseIsOffReadsSleepDisabled(t *testing.T) {
	on := "System-wide power settings:\n SleepDisabled\t\t1\nCurrently in use:\n standby              1\n sleep                1\n"
	if !ParseIsOff(on) {
		t.Errorf("SleepDisabled 1 read as on")
	}
	for _, out := range []string{"System-wide power settings:\n SleepDisabled\t\t0\n", "Currently in use:\n sleep                1 (sleep prevented by caffeinate)\n", ""} {
		if ParseIsOff(out) {
			t.Errorf("ParseIsOff(%q) = true", out)
		}
	}
}

func TestParseHeatReadsThePressureLevel(t *testing.T) {
	if n, ok := ParseHeat("com.apple.system.thermalpressurelevel 2\n"); !ok || n != Heavy {
		t.Errorf("ParseHeat = %d, %v", n, ok)
	}
	for _, out := range []string{"", "com.apple.other 1", "com.apple.system.thermalpressurelevel x"} {
		if _, ok := ParseHeat(out); ok {
			t.Errorf("ParseHeat(%q) ok", out)
		}
	}
}

func TestParseClosedReadsTheClamshell(t *testing.T) {
	closed := "+-o IOPMrootDomain  <class IOPMrootDomain, id 0x100000201>\n    {\n      \"AppleClamshellCausesSleep\" = Yes\n      \"AppleClamshellState\" = Yes\n    }\n"
	if !ParseClosed(closed) {
		t.Error("a closed lid read as open")
	}
	if ParseClosed(strings.ReplaceAll(closed, "State\" = Yes", "State\" = No")) {
		t.Error("an open lid read as closed")
	}
	if ParseClosed("    | |   \"AppleClamshellCausesSleep\" = Yes\n") {
		t.Error("AppleClamshellCausesSleep read as the lid")
	}
	if !ParseClosed("    | |   \"AppleClamshellState\" = Yes\n") {
		t.Error("a line in ioreg's tree not read")
	}
	if ParseClosed("") {
		t.Error("a Mac with no lid read as closed")
	}
}

func TestParseExternalFindsADisplayBesideTheBuiltInOne(t *testing.T) {
	builtIn := `{"SPDisplaysDataType" : [{"_name" : "Apple M3 Pro", "spdisplays_ndrvs" : [{"_name" : "Color LCD", "spdisplays_connection_type" : "spdisplays_internal", "spdisplays_main" : "spdisplays_yes"}]}]}`
	if ext, err := ParseExternal(builtIn); err != nil || ext {
		t.Errorf("the built-in display alone: %v, %v", ext, err)
	}
	two := strings.Replace(builtIn, `}]}]}`, `}, {"_name" : "DELL U2723QE", "spdisplays_main" : "spdisplays_no"}]}]}`, 1)
	if ext, err := ParseExternal(two); err != nil || !ext {
		t.Errorf("a display beside it: %v, %v", ext, err)
	}
	if ext, err := ParseExternal(`{"SPDisplaysDataType" : [{"_name" : "Apple M3 Pro"}]}`); err != nil || ext {
		t.Errorf("no display at all (the lid closed): %v, %v", ext, err)
	}
	if _, err := ParseExternal("not json"); err == nil {
		t.Error("no error for output that is not JSON")
	}
}

func TestWantKeepsTheMacAwakeOnlyForWorkAndWhileItIsSafe(t *testing.T) {
	work := config.Lid{Awake: config.LidWork, Battery: 10}
	plugged := Power{Percent: 50}
	for _, c := range []struct {
		name string
		c    config.Lid
		f    Facts
		want bool
		why  string
	}{
		{"a session works", work, Facts{Work: true, Power: plugged}, true, "sessions work"},
		{"nothing works", work, Facts{Power: plugged}, false, "no session works"},
		{"off", config.Lid{Awake: config.LidOff}, Facts{Work: true}, false, "lid.awake is off"},
		{"always, with nothing at work", config.Lid{Awake: config.LidAlways, Battery: 10}, Facts{Power: plugged}, true, "lid.awake is always"},
		{"on battery above the floor", work, Facts{Work: true, Power: Power{Battery: true, Percent: 10}}, true, "sessions work"},
		{"on battery under the floor", work, Facts{Work: true, Power: Power{Battery: true, Percent: 9}}, false, "the battery is at 9%, under lid.battery's 10%"},
		{"plugged in with a low battery", work, Facts{Work: true, Power: Power{Battery: false, Percent: 3}}, true, "sessions work"},
		{"on battery, down to empty", config.Lid{Awake: config.LidWork}, Facts{Work: true, Power: Power{Battery: true, Percent: 1}}, true, "sessions work"},
		{"a Mac with no battery", work, Facts{Work: true, Power: Power{Percent: -1}}, true, "sessions work"},
		{"moderate heat", work, Facts{Work: true, Power: plugged, Heat: 1}, true, "sessions work"},
		{"heavy heat", work, Facts{Work: true, Power: plugged, Heat: Heavy}, false, "the Mac is hot"},
		{"heavy heat, always", config.Lid{Awake: config.LidAlways}, Facts{Power: plugged, Heat: 3}, false, "the Mac is hot"},
	} {
		got, why := Want(c.c, c.f)
		if got != c.want || why != c.why {
			t.Errorf("%s: Want = %v, %q; want %v, %q", c.name, got, why, c.want, c.why)
		}
	}
}

func TestSleepOffRunsPmsetThroughSudo(t *testing.T) {
	f := &fake{}
	if err := f.mac().SleepOff(true); err != nil {
		t.Fatal(err)
	}
	if err := f.mac().SleepOff(false); err != nil {
		t.Fatal(err)
	}
	want := []string{"sudo -n /usr/bin/pmset -a disablesleep 1", "sudo -n /usr/bin/pmset -a disablesleep 0"}
	if !slices.Equal(f.ran, want) {
		t.Errorf("ran %q, want %q", f.ran, want)
	}
}

func TestSleepOffWithoutTheRuleSaysToSetItUp(t *testing.T) {
	line := "sudo -n /usr/bin/pmset -a disablesleep 1"
	f := &fake{out: map[string]string{line: "sudo: a password is required\n"}, fail: map[string]bool{line: true}}
	if err := f.mac().SleepOff(true); !errors.Is(err, ErrNoRule) {
		t.Errorf("err = %v, want ErrNoRule", err)
	}
	other := &fake{out: map[string]string{line: "pmset: something else\n"}, fail: map[string]bool{line: true}}
	if err := other.mac().SleepOff(true); err == nil || errors.Is(err, ErrNoRule) || !strings.Contains(err.Error(), "something else") {
		t.Errorf("err = %v, want pmset's own error", err)
	}
}

func TestSleepIfClosedSleepsOnlyALidClosedWithNoOtherDisplay(t *testing.T) {
	const (
		ioreg = "ioreg -r -k AppleClamshellState -d 1"
		sp    = "system_profiler -json SPDisplaysDataType"
		sleep = "pmset sleepnow"
	)
	builtIn := `{"SPDisplaysDataType" : [{"spdisplays_ndrvs" : [{"spdisplays_connection_type" : "spdisplays_internal"}]}]}`
	external := `{"SPDisplaysDataType" : [{"spdisplays_ndrvs" : [{"spdisplays_connection_type" : "spdisplays_internal"}, {}]}]}`
	for _, c := range []struct {
		name, lid, displays string
		slept               bool
	}{
		{"closed", `"AppleClamshellState" = Yes`, builtIn, true},
		{"open", `"AppleClamshellState" = No`, builtIn, false},
		{"closed with a display beside it", `"AppleClamshellState" = Yes`, external, false},
	} {
		f := &fake{out: map[string]string{ioreg: c.lid, sp: c.displays}}
		slept, err := f.mac().SleepIfClosed()
		if err != nil || slept != c.slept || slices.Contains(f.ran, sleep) != c.slept {
			t.Errorf("%s: slept %v, %v; ran %q", c.name, slept, err, f.ran)
		}
	}
}

func TestStateRoundTripsAndAMissingFileIsNeverOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "lid.json")
	if s, at, err := Read(path); err != nil || s.Off || !at.IsZero() {
		t.Fatalf("missing: %+v, %v, %v", s, at, err)
	}
	since := time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)
	if err := Write(path, State{Off: true, Since: since, Why: "sessions work"}); err != nil {
		t.Fatal(err)
	}
	later := since.Add(time.Hour)
	if err := Touch(path, later); err != nil {
		t.Fatal(err)
	}
	s, at, err := Read(path)
	if err != nil || !s.Off || !s.Since.Equal(since) || s.Why != "sessions work" || !at.Equal(later) {
		t.Errorf("read %+v at %v, %v", s, at, err)
	}
}

func TestCheckTurnsSleepBackOnOnlyWhenNoCoreKeepsItOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lid.json")
	now := time.Now()
	f := &fake{}
	if why, err := Check(f.mac(), path, now); why != "" || err != nil || len(f.ran) > 0 {
		t.Errorf("no state: %q, %v, ran %q", why, err, f.ran)
	}
	if err := Write(path, State{Off: true, Since: now.Add(-time.Hour), Why: "sessions work"}); err != nil {
		t.Fatal(err)
	}
	// The core touched it a minute ago: it is on it.
	Touch(path, now.Add(-time.Minute))
	if why, err := Check(f.mac(), path, now); why != "" || err != nil || len(f.ran) > 0 {
		t.Errorf("a fresh hold: %q, %v, ran %q", why, err, f.ran)
	}
	// Ten minutes without a word: no core keeps it.
	Touch(path, now.Add(-10*time.Minute))
	why, err := Check(f.mac(), path, now)
	if err != nil || why != "no core had kept it awake for 10m0s" {
		t.Errorf("a stale hold: %q, %v", why, err)
	}
	if len(f.ran) == 0 || f.ran[0] != "sudo -n /usr/bin/pmset -a disablesleep 0" {
		t.Errorf("ran %q", f.ran)
	}
	if s, _, _ := Read(path); s.Off || s.Why != why {
		t.Errorf("state after the check = %+v", s)
	}
}

func TestRuleTextNamesOnlyTheTwoCommands(t *testing.T) {
	text, err := RuleText("alice")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "#") || lines[1] != "alice ALL = (root) NOPASSWD: /usr/bin/pmset -a disablesleep 1, /usr/bin/pmset -a disablesleep 0" {
		t.Errorf("rule = %q", text)
	}
	for _, bad := range []string{"", "alice ALL=(ALL) ALL", "al,ice", "%admin", "-alice", "al ice", "alice\n"} {
		if _, err := RuleText(bad); err == nil {
			t.Errorf("RuleText(%q) took it", bad)
		}
	}
}

func TestInstallScriptChecksTheRuleBeforeSudoReadsIt(t *testing.T) {
	text, _ := RuleText("alice")
	script, err := InstallScript(text)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"$f.new"`, "/usr/sbin/visudo -cqf", `/bin/mv -f "$f.new" "$f"`, "/bin/chmod 0440", "chown root:wheel"} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q: %s", want, script)
		}
	}
	if strings.Index(script, "visudo") > strings.Index(script, "/bin/mv") {
		t.Error("the rule is moved into place before visudo checks it")
	}
	if _, err := InstallScript("it's"); err == nil {
		t.Error("a quote in the rule was taken")
	}
	// The script writes the rule as given, line for line.
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	local := strings.Replace(script, "f="+Rule, "f="+filepath.Join(dir, "irie-lid"), 1)
	local = strings.NewReplacer("/usr/sbin/chown root:wheel", "true", "/usr/sbin/visudo -cqf", "test -s").Replace(local)
	if out, err := exec.Command(sh, "-c", local).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "irie-lid"))
	if err != nil || string(b) != text {
		t.Errorf("wrote %q, %v; want %q", b, err, text)
	}
	if _, err := os.Stat(filepath.Join(dir, "irie-lid.new")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the checked copy was left: %v", err)
	}
}

func TestAgentPlistRunsTheCheck(t *testing.T) {
	p := AgentPlist("/home/alice/.local/share/irie/bin/irie", "/home/alice/.local/state/irie/lid.json")
	for _, want := range []string{"<string>" + Label + "</string>", "<string>/home/alice/.local/share/irie/bin/irie</string>", "<string>lid</string>", "<string>check</string>", "<string>/home/alice/.local/state/irie/lid.json</string>", "<key>RunAtLoad</key>", "<integer>120</integer>"} {
		if !strings.Contains(p, want) {
			t.Errorf("plist lacks %q", want)
		}
	}
	if got := AgentPath("/home/alice"); got != "/home/alice/Library/LaunchAgents/io.github.irie-hq.irie.lid.plist" {
		t.Errorf("AgentPath = %q", got)
	}
}
