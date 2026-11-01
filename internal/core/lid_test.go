package core

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/lid"
	"github.com/irie-people/irie/internal/paths"
)

// lidMac stands in for the Mac's tools on PATH: sudo, which runs pmset
// -a disablesleep by the rule and logs what it ran; pmset, which says
// whether sleep is off, reads the battery and logs sleepnow; notifyutil,
// for the heat; ioreg, for the lid; and system_profiler, for the displays.
type lidMac struct {
	dir string
}

func newLidMac(t *testing.T) *lidMac {
	t.Helper()
	if !lid.Supported() {
		t.Skip("the lid is kept open only on a Mac")
	}
	m := &lidMac{dir: t.TempDir()}
	bin := filepath.Join(m.dir, "bin")
	os.MkdirAll(bin, 0o755)
	m.set("off", "0")
	m.set("batt", "Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t80%; charging; present: true\n")
	m.set("heat", "0")
	m.set("lid", "No")
	m.set("systemstate", "Current System Capabilities are: CPU Graphics Audio Network \nCurrent Power State: 4\n")
	fakeTool(t, bin, "sudo", `echo "$*" >> `+m.path("sudo.log")+`
[ "$2 $3 $4" = "/usr/bin/pmset -a disablesleep" ] || exit 1
echo "$5" > `+m.path("off")+"\n")
	fakeTool(t, bin, "pmset", `case "$*" in
"-g") [ "$(cat `+m.path("off")+`)" = 1 ] && printf ' SleepDisabled\t\t1\n'; echo ' sleep 1' ;;
"-g batt") cat `+m.path("batt")+` ;;
"-g systemstate") cat `+m.path("systemstate")+` ;;
sleepnow) echo sleepnow >> `+m.path("pmset.log")+` ;;
*) exit 1 ;;
esac
`)
	fakeTool(t, bin, "notifyutil", `echo "com.apple.system.thermalpressurelevel $(cat `+m.path("heat")+`)"`+"\n")
	fakeTool(t, bin, "ioreg", `echo "    \"AppleClamshellState\" = $(cat `+m.path("lid")+`)"`+"\n")
	fakeTool(t, bin, "system_profiler", `echo '{"SPDisplaysDataType" : [{"spdisplays_ndrvs" : [{"spdisplays_connection_type" : "spdisplays_internal"}]}]}'`+"\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("IRIE_LID", "")
	rule := m.path("irie-lid")
	m.set("irie-lid", "the rule")
	old := lidRule
	lidRule = rule
	t.Cleanup(func() { lidRule = old })
	return m
}

func (m *lidMac) path(name string) string { return filepath.Join(m.dir, name) }

func (m *lidMac) set(name, value string) {
	os.WriteFile(m.path(name), []byte(value), 0o644)
}

func (m *lidMac) lines(name string) []string {
	b, _ := os.ReadFile(m.path(name))
	return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(b)), "\n", "|"))
}

// sudo is what sudo ran, one command a line.
func (m *lidMac) sudo() []string {
	b, _ := os.ReadFile(m.path("sudo.log"))
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func (m *lidMac) slept() bool {
	b, _ := os.ReadFile(m.path("pmset.log"))
	return strings.Contains(string(b), "sleepnow")
}

// lidCore is a core, with no tmux, whose one session works or not.
func lidCore(t *testing.T, file string) (*Core, *session) {
	t.Helper()
	cfg, err := config.Parse([]byte(file), "test.toml")
	if err != nil {
		t.Fatal(err)
	}
	p, err := paths.New(func(k string) string {
		if k == "HOME" {
			return t.TempDir()
		}
		return ""
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	x := stopped("s1", time.Now())
	x.observed = true
	c := &Core{sessions: map[string]*session{"s1": x}, paths: p, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.useConfig(cfg)
	return c, x
}

const (
	sleepOff = "-n /usr/bin/pmset -a disablesleep 1"
	sleepOn  = "-n /usr/bin/pmset -a disablesleep 0"
)

func TestTheLidKeepsTheMacAwakeWhileASessionWorks(t *testing.T) {
	m := newLidMac(t)
	c, x := lidCore(t, "[lid]\ngrace = \"0s\"\n")
	c.lidSweep()
	if got := m.sudo(); len(got) != 1 || got[0] != sleepOff {
		t.Fatalf("sudo ran %q, want sleep off while the session works", got)
	}
	if s, _, err := lid.Read(c.paths.LidFile()); err != nil || !s.Off || s.Why != "sessions work" {
		t.Errorf("lid.json = %+v, %v", s, err)
	}
	// Another look while it works only touches lid.json.
	c.lidSweep()
	if got := m.sudo(); len(got) != 1 {
		t.Errorf("sudo ran %q at the second look", got)
	}
	// The turn ends, the lid is open: sleep is back on, and the Mac awake.
	x.track.apply(event.Event{Kind: event.TurnEnded, Agent: event.Main, TS: event.Stamp(time.Now())})
	c.lidSweep()
	if got := m.sudo(); len(got) != 2 || got[1] != sleepOn {
		t.Fatalf("sudo ran %q, want sleep back on once the work is through", got)
	}
	if m.slept() {
		t.Error("the Mac was slept with its lid open")
	}
	if s, _, _ := lid.Read(c.paths.LidFile()); s.Off || s.Why != "no session works" {
		t.Errorf("lid.json = %+v", s)
	}
}

func TestTheLidSleepsTheMacOnceTheWorkIsThroughWithItClosed(t *testing.T) {
	m := newLidMac(t)
	c, x := lidCore(t, "[lid]\ngrace = \"0s\"\n")
	c.lidSweep()
	m.set("lid", "Yes")
	x.track.apply(event.Event{Kind: event.TurnEnded, Agent: event.Main, TS: event.Stamp(time.Now())})
	c.lidSweep()
	if got := m.sudo(); len(got) != 2 || got[1] != sleepOn {
		t.Fatalf("sudo ran %q", got)
	}
	if !m.slept() {
		t.Error("the work is through and the lid is closed, but the Mac was not slept")
	}
}

func TestTheLidWaitsOutTheGraceAndBackgroundWork(t *testing.T) {
	m := newLidMac(t)
	c, x := lidCore(t, "[lid]\ngrace = \"1h\"\n")
	c.lidSweep()
	x.track.apply(event.Event{Kind: event.TurnEnded, Agent: event.Main, TS: event.Stamp(time.Now())})
	c.lidSweep()
	if got := m.sudo(); len(got) != 1 {
		t.Errorf("sudo ran %q within the grace", got)
	}

	m2 := newLidMac(t)
	c2, x2 := lidCore(t, "[lid]\ngrace = \"0s\"\n")
	x2.track.apply(event.Event{Kind: event.TurnEnded, Agent: event.Main, TS: event.Stamp(time.Now()), Data: map[string]any{"waiting": 1}})
	c2.lidSweep()
	if got := m2.sudo(); len(got) != 1 || got[0] != sleepOff {
		t.Errorf("sudo ran %q, want sleep off while background work will wake the session", got)
	}
}

func TestTheLidLetsTheMacSleepOnALowBatteryOrWhenHot(t *testing.T) {
	m := newLidMac(t)
	m.set("batt", "Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t9%; discharging; present: true\n")
	c, _ := lidCore(t, "[lid]\nbattery = 10\n")
	c.lidSweep()
	if got := m.sudo(); len(got) != 0 {
		t.Errorf("sudo ran %q on 9%% of battery", got)
	}

	m = newLidMac(t)
	c, _ = lidCore(t, "")
	c.lidSweep()
	m.set("heat", "2")
	m.set("lid", "Yes")
	c.lidSweep()
	if got := m.sudo(); len(got) != 2 || got[1] != sleepOn || !m.slept() {
		t.Errorf("sudo ran %q, slept %v: the Mac grew hot with its lid closed", got, m.slept())
	}
}

func TestTheLidLeavesSleepAloneWhenItIsNotIries(t *testing.T) {
	m := newLidMac(t)
	m.set("off", "1")
	c, x := lidCore(t, "[lid]\ngrace = \"0s\"\n")
	c.lidSweep()
	x.track.apply(event.Event{Kind: event.TurnEnded, Agent: event.Main, TS: event.Stamp(time.Now())})
	c.lidSweep()
	if got := m.sudo(); len(got) != 0 {
		t.Errorf("sudo ran %q for sleep the user turned off", got)
	}

	m = newLidMac(t)
	os.Remove(lidRule)
	c, _ = lidCore(t, "")
	c.lidSweep()
	if got := m.sudo(); len(got) != 0 {
		t.Errorf("sudo ran %q with no rule installed", got)
	}

	m = newLidMac(t)
	c, _ = lidCore(t, "[lid]\nawake = \"off\"\n")
	c.lidSweep()
	if got := m.sudo(); len(got) != 0 {
		t.Errorf("sudo ran %q with lid.awake off", got)
	}
}

func TestTheLidAlwaysKeepsTheMacAwakeWhileTheCoreRuns(t *testing.T) {
	m := newLidMac(t)
	c, x := lidCore(t, "[lid]\nawake = \"always\"\ngrace = \"0s\"\n")
	x.track.apply(event.Event{Kind: event.TurnEnded, Agent: event.Main, TS: event.Stamp(time.Now())})
	c.lidSweep()
	if got := m.sudo(); len(got) != 1 || got[0] != sleepOff {
		t.Errorf("sudo ran %q, want sleep off with nothing at work", got)
	}
}

func TestTheLidLetsGoAsTheCoreStopsAndTheNextCoreTakesItOn(t *testing.T) {
	m := newLidMac(t)
	c, _ := lidCore(t, "")
	c.lidSweep()
	m.set("lid", "Yes")
	c.lidLetGo()
	if got := m.sudo(); len(got) != 2 || got[1] != sleepOn {
		t.Fatalf("sudo ran %q, want sleep back on as the core stops", got)
	}
	if m.slept() {
		t.Error("the Mac was slept as the core stopped, as for a restart")
	}

	// A core that crashed left sleep off: the next one takes the hold on,
	// and lets go once there is no work.
	lid.Write(c.paths.LidFile(), lid.State{Off: true, Since: time.Now().Add(-time.Hour), Why: "sessions work"})
	m.set("off", "1")
	next, x := lidCore(t, "[lid]\ngrace = \"0s\"\n")
	next.paths = c.paths
	next.lidAdopt()
	if !next.lid.off {
		t.Fatal("the hold the last core left was not taken on")
	}
	x.track.apply(event.Event{Kind: event.TurnEnded, Agent: event.Main, TS: event.Stamp(time.Now())})
	next.lid.worked = time.Now().Add(-time.Minute)
	next.lidSweep()
	if got := m.sudo(); len(got) != 3 || got[2] != sleepOn {
		t.Errorf("sudo ran %q, want the adopted hold let go", got)
	}
}

func TestTheLidHoldsAgainWhenSomethingElseTurnedSleepBackOn(t *testing.T) {
	m := newLidMac(t)
	c, _ := lidCore(t, "")
	c.lidSweep()
	m.set("off", "0") // as the launchd agent's check would
	c.lid.verified = time.Time{}
	c.lidSweep()
	c.lidSweep()
	if got := m.sudo(); len(got) != 2 || got[0] != sleepOff || got[1] != sleepOff {
		t.Errorf("sudo ran %q, want sleep turned off again", got)
	}
}

func TestTheLidHoldsNothingInADarkWake(t *testing.T) {
	m := newLidMac(t)
	// A sleeping Mac up for a moment on its own: its session still looks
	// as it did when the Mac slept.
	m.set("systemstate", "Current System Capabilities are: CPU Network \nCurrent Power State: 4\n")
	c, _ := lidCore(t, "")
	c.lidSweep()
	if got := m.sudo(); len(got) != 0 {
		t.Errorf("sudo ran %q in a dark wake", got)
	}
	// Once it is fully awake, the work holds it.
	m.set("systemstate", "Current System Capabilities are: CPU Graphics Audio Network \n")
	c.lidSweep()
	if got := m.sudo(); len(got) != 1 || got[0] != sleepOff {
		t.Errorf("sudo ran %q in a full wake", got)
	}
}

func TestNoLookHoldsOnceTheCoreLetGo(t *testing.T) {
	m := newLidMac(t)
	c, _ := lidCore(t, "")
	c.lidLetGo()
	c.lidSweep()
	if got := m.sudo(); len(got) != 0 {
		t.Errorf("sudo ran %q after the core let go", got)
	}
}

func TestTheLookAfterSetupHoldsAtOnce(t *testing.T) {
	m := newLidMac(t)
	rule := lidRule
	os.Remove(rule)
	c, _ := lidCore(t, "")
	c.lidSweep()
	m.set("irie-lid", "the rule") // irie setup lid
	c.lidSweep()
	if got := m.sudo(); len(got) != 1 || got[0] != sleepOff {
		t.Errorf("sudo ran %q, want sleep off at the first look after setup", got)
	}
}
