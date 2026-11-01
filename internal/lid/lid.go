// Package lid keeps a MacBook awake with its lid closed while Irie's work
// is under way (DESIGN §4.12).
//
// macOS sleeps a MacBook the moment its lid closes, whatever caffeinate or
// another power assertion holds. Only a display beside it, or the Mac's
// sleep turned off altogether (pmset's disablesleep, which needs root and
// lasts across restarts), keeps it running. `irie setup lid` installs a
// sudoers rule that lets the user's own account run exactly `pmset -a
// disablesleep 1` and `pmset -a disablesleep 0` without a password, and a
// launchd agent whose check turns sleep back on when no core keeps it off.
// The core turns sleep off while sessions work and back on once they are
// through, and then sleeps the Mac at once if its lid is closed with no
// other display.
//
// Each read runs one small macOS tool (pmset, notifyutil, ioreg or
// system_profiler), found on PATH first so that tests can stand in for it.
package lid

import (
	"context"
	"encoding/json"
	"errors"
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

	"github.com/irie-people/irie/internal/config"
)

// Rule is the sudoers file `irie setup lid` writes. sudo reads each file in
// /etc/sudoers.d whose name has no dot, so the file it is checked under
// first, Rule+".new", is never read while it is unchecked.
const Rule = "/private/etc/sudoers.d/irie-lid"

// Pmset is the tool the rule names: sudo matches a command by its path.
const Pmset = "/usr/bin/pmset"

// Label is the launchd agent's label, after the Mac app's bundle id.
const Label = "io.github.irie-hq.irie.lid"

// Every is how often the launchd agent runs its check.
const Every = 2 * time.Minute

// Stale is how long a hold may go without the core's word before the
// check turns the Mac's sleep back on.
const Stale = 3 * time.Minute

// Heavy is the thermal pressure from which the Mac may sleep:
// kOSThermalPressureLevelHeavy in macOS's <libkern/OSThermalNotification.h>,
// where 0 is nominal, 1 moderate, 3 trapping and 4 sleeping.
const Heavy = 2

// thermalKey is the notify(3) name macOS posts the thermal pressure level
// under (kOSThermalNotificationPressureLevelName).
const thermalKey = "com.apple.system.thermalpressurelevel"

// ErrNoRule is a hold sudo refused: `irie setup lid` has not been run, or
// its rule was removed.
var ErrNoRule = errors.New("the rule that lets Irie turn the Mac's sleep off is not installed: run irie setup lid")

// Supported reports whether this is a Mac, the one system a lid is kept
// open for.
func Supported() bool { return runtime.GOOS == "darwin" }

// timeout bounds each tool's run.
const timeout = 10 * time.Second

// tool is the path to run name by: from PATH, else fallback.
func tool(name, fallback string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return fallback
}

// Mac is the Mac, through its tools.
type Mac struct {
	// Run runs a tool and returns what it printed to stdout and stderr;
	// nil runs it, bounded by a timeout.
	Run func(name string, args ...string) (string, error)
}

func (m Mac) run(name string, args ...string) (string, error) {
	if m.Run != nil {
		return m.Run(name, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// SleepOff turns the Mac's sleep off (true) or back on (false), as `pmset
// -a disablesleep 1` or `0` does, through sudo -n and the rule. With sleep
// off the Mac stays awake with its lid closed, and on battery too.
func (m Mac) SleepOff(off bool) error {
	v := "0"
	if off {
		v = "1"
	}
	out, err := m.run(tool("sudo", "/usr/bin/sudo"), "-n", Pmset, "-a", "disablesleep", v)
	if err != nil {
		if strings.Contains(out, "password") {
			return ErrNoRule
		}
		return fmt.Errorf("sudo pmset -a disablesleep %s: %v: %s", v, err, strings.TrimSpace(out))
	}
	return nil
}

// Allowed reports whether the rule lets this account turn the Mac's sleep
// off and on, by `sudo -n -l`, which runs nothing.
func (m Mac) Allowed() bool {
	for _, v := range []string{"1", "0"} {
		if _, err := m.run(tool("sudo", "/usr/bin/sudo"), "-n", "-l", Pmset, "-a", "disablesleep", v); err != nil {
			return false
		}
	}
	return true
}

// IsOff reports whether the Mac's sleep is off, by `pmset -g`.
func (m Mac) IsOff() (bool, error) {
	out, err := m.run(tool("pmset", Pmset), "-g")
	if err != nil {
		return false, err
	}
	return ParseIsOff(out), nil
}

// ParseIsOff reads `pmset -g`, which lists "SleepDisabled 1" among its
// system-wide settings while sleep is off, and nothing of it otherwise.
func ParseIsOff(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "SleepDisabled" {
			return f[1] == "1"
		}
	}
	return false
}

// Dark reports whether the Mac is in a dark wake, awake for a moment on
// its own while it sleeps, by `pmset -g systemstate`.
func (m Mac) Dark() (bool, error) {
	out, err := m.run(tool("pmset", Pmset), "-g", "systemstate")
	if err != nil {
		return false, err
	}
	return ParseDark(out), nil
}

// ParseDark reads `pmset -g systemstate`, whose capabilities lack Graphics
// in a dark wake: "Current System Capabilities are: CPU Graphics Audio
// Network" in a full one.
func ParseDark(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if _, caps, ok := strings.Cut(line, "Capabilities are:"); ok {
			return !slices.Contains(strings.Fields(caps), "Graphics")
		}
	}
	return false
}

// Power is what the Mac runs on.
type Power struct {
	Battery bool // its battery, not a power adapter
	Percent int  // the battery's charge; -1 for a Mac with none
}

// Power reads what the Mac runs on, by `pmset -g batt`.
func (m Mac) Power() (Power, error) {
	out, err := m.run(tool("pmset", Pmset), "-g", "batt")
	if err != nil {
		return Power{Percent: -1}, err
	}
	return ParsePower(out), nil
}

var charge = regexp.MustCompile(`InternalBattery\S*\s.*?(\d+)%`)

// ParsePower reads `pmset -g batt`: "Now drawing from 'Battery Power'" or
// "'AC Power'", then a line for the internal battery, if there is one, with
// its charge, such as "-InternalBattery-0 (id=1234)	15%; discharging".
func ParsePower(out string) Power {
	p := Power{Percent: -1}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "drawing from") {
			p.Battery = strings.Contains(line, "'Battery Power'")
		}
		if m := charge.FindStringSubmatch(line); m != nil {
			p.Percent, _ = strconv.Atoi(m[1])
		}
	}
	return p
}

// Heat reads the Mac's thermal pressure level, by notifyutil: 0, nominal,
// up to 4.
func (m Mac) Heat() (int, error) {
	out, err := m.run(tool("notifyutil", "/usr/bin/notifyutil"), "-g", thermalKey)
	if err != nil {
		return 0, err
	}
	n, ok := ParseHeat(out)
	if !ok {
		return 0, fmt.Errorf("notifyutil -g %s: %q", thermalKey, strings.TrimSpace(out))
	}
	return n, nil
}

// ParseHeat reads `notifyutil -g com.apple.system.thermalpressurelevel`,
// which prints the name and its level, such as
// "com.apple.system.thermalpressurelevel 0".
func ParseHeat(out string) (int, bool) {
	f := strings.Fields(out)
	if len(f) != 2 || f[0] != thermalKey {
		return 0, false
	}
	n, err := strconv.Atoi(f[1])
	return n, err == nil
}

// Closed reports whether the lid is closed, by ioreg's AppleClamshellState.
// A Mac with no lid has none, and its lid is never closed.
func (m Mac) Closed() (bool, error) {
	out, err := m.run(tool("ioreg", "/usr/sbin/ioreg"), "-r", "-k", "AppleClamshellState", "-d", "1")
	if err != nil {
		return false, err
	}
	return ParseClosed(out), nil
}

// ParseClosed reads `ioreg -r -k AppleClamshellState -d 1`, whose line
// `"AppleClamshellState" = Yes` says the lid is closed.
func ParseClosed(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimLeft(line, " \t|"), "="); ok && strings.TrimSpace(k) == `"AppleClamshellState"` {
			return strings.TrimSpace(v) == "Yes"
		}
	}
	return false
}

// External reports whether a display other than the built-in one is
// there, by system_profiler: with one, a closed lid does not sleep the Mac.
func (m Mac) External() (bool, error) {
	out, err := m.run(tool("system_profiler", "/usr/sbin/system_profiler"), "-json", "SPDisplaysDataType")
	if err != nil {
		return false, err
	}
	return ParseExternal(out)
}

// ParseExternal reads `system_profiler -json SPDisplaysDataType`: a list
// of graphics processors, each with the displays on it, the built-in one
// with the connection type spdisplays_internal.
func ParseExternal(out string) (bool, error) {
	var v struct {
		GPUs []struct {
			Displays []struct {
				Connection string `json:"spdisplays_connection_type"`
			} `json:"spdisplays_ndrvs"`
		} `json:"SPDisplaysDataType"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return false, fmt.Errorf("system_profiler SPDisplaysDataType: %w", err)
	}
	for _, g := range v.GPUs {
		for _, d := range g.Displays {
			if d.Connection != "spdisplays_internal" {
				return true, nil
			}
		}
	}
	return false, nil
}

// SleepNow sleeps the Mac, as `pmset sleepnow` does; it needs no root.
func (m Mac) SleepNow() error {
	if out, err := m.run(tool("pmset", Pmset), "sleepnow"); err != nil {
		return fmt.Errorf("pmset sleepnow: %v: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// SleepIfClosed sleeps the Mac if its lid is closed with no other display,
// as closing the lid would have; it reports whether it did.
func (m Mac) SleepIfClosed() (bool, error) {
	closed, err := m.Closed()
	if err != nil || !closed {
		return false, err
	}
	if ext, err := m.External(); err != nil || ext {
		return false, err
	}
	return true, m.SleepNow()
}

// Facts are what the hold depends on.
type Facts struct {
	Work  bool // a session works, or did within lid.grace
	Power Power
	Heat  int // the thermal pressure level
}

// Want reports whether the Mac's sleep should be off, and why, in a few
// words for the log and `irie lid`.
func Want(c config.Lid, f Facts) (bool, string) {
	switch {
	case c.Awake == config.LidOff:
		return false, "lid.awake is off"
	case c.Awake == config.LidWork && !f.Work:
		return false, "no session works"
	case f.Power.Battery && f.Power.Percent >= 0 && f.Power.Percent < c.Battery:
		return false, fmt.Sprintf("the battery is at %d%%, under lid.battery's %d%%", f.Power.Percent, c.Battery)
	case f.Heat >= Heavy:
		return false, "the Mac is hot"
	case c.Awake == config.LidAlways:
		return true, "lid.awake is always"
	}
	return true, "sessions work"
}

// State is what the core last did to the Mac's sleep, in the state dir's
// lid.json. While sleep is off the core touches the file at each look, so
// its modification time is the core's last word.
type State struct {
	Off   bool      `json:"off"`   // the core turned sleep off and has not turned it back on
	Since time.Time `json:"since"` // when that last changed
	Why   string    `json:"why"`
}

// Read reads the state at path and when the core last touched it; with no
// file, the state is that Irie has never turned sleep off.
func Read(path string) (State, time.Time, error) {
	var s State
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, time.Time{}, nil
	}
	if err != nil {
		return s, time.Time{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s, time.Time{}, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, time.Time{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, fi.ModTime(), nil
}

// Write writes s to path whole, through a file beside it.
func Write(path string, s State) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Touch is the core's word that it still keeps sleep off.
func Touch(path string, now time.Time) error { return os.Chtimes(path, now, now) }

// Check is the launchd agent's look: when the state says sleep is off but
// no core has touched it for Stale, as after a crash or a restart, it
// turns sleep back on and sleeps the Mac if its lid is closed. It returns
// what it did, or "" when it did nothing.
func Check(m Mac, path string, now time.Time) (string, error) {
	s, at, err := Read(path)
	if err != nil || !s.Off || now.Sub(at) < Stale {
		return "", err
	}
	if err := m.SleepOff(false); err != nil {
		return "", err
	}
	why := fmt.Sprintf("no core had kept it awake for %v", now.Sub(at).Round(time.Minute))
	if err := Write(path, State{Off: false, Since: now, Why: why}); err != nil {
		return "", err
	}
	if slept, err := m.SleepIfClosed(); slept && err == nil {
		why += "; its lid was closed, so it slept"
	}
	return why, nil
}

var userName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

// RuleText is the sudoers rule for user: exactly the two pmset commands,
// as root, without a password.
func RuleText(user string) (string, error) {
	if !userName.MatchString(user) {
		return "", fmt.Errorf("lid: %q is not a user name a sudoers rule can name", user)
	}
	return fmt.Sprintf("# Irie (irie setup lid): lets %s turn sleep off and on, so a closed lid does not stop agents at work.\n%s ALL = (root) NOPASSWD: %s -a disablesleep 1, %s -a disablesleep 0\n", user, user, Pmset, Pmset), nil
}

// InstallScript is the shell script, run as root, that installs text as
// the rule: written beside it under a name sudo skips, checked by visudo,
// then moved into place, so a rule visudo refuses never reaches sudo.
func InstallScript(text string) (string, error) {
	if strings.Contains(text, "'") {
		return "", errors.New("lid: the rule may not hold a single quote")
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return `set -e; f=` + Rule + `; /bin/mkdir -p "$(/usr/bin/dirname "$f")"; /usr/bin/printf '%s\n' '` + strings.Join(lines, `' '`) + `' > "$f.new"; /usr/sbin/chown root:wheel "$f.new"; /bin/chmod 0440 "$f.new"; if /usr/sbin/visudo -cqf "$f.new"; then /bin/mv -f "$f.new" "$f"; else /bin/rm -f "$f.new"; exit 1; fi`, nil
}

// RemoveScript is the shell script, run as root, that removes the rule.
func RemoveScript() string { return `/bin/rm -f ` + Rule + ` ` + Rule + `.new` }

// AgentPath is where the launchd agent goes for the user whose home is
// home.
func AgentPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

// AgentPlist is the launchd agent that runs `bin lid check --file state` as
// the user logs in, and every Every after.
func AgentPlist(bin, state string) string {
	esc := func(s string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + esc(bin) + `</string>
		<string>lid</string>
		<string>check</string>
		<string>--file</string>
		<string>` + esc(state) + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>StartInterval</key>
	<integer>` + strconv.Itoa(int(Every/time.Second)) + `</integer>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
`
}
