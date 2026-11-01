package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/lid"
	"github.com/irie-people/irie/internal/paths"
)

// lidFake is a Mac whose tools answer by their base name and arguments;
// allowed says whether sudo takes the rule.
func lidFake(allowed bool, out map[string]string) lid.Mac {
	return lid.Mac{Run: func(name string, args ...string) (string, error) {
		line := strings.Join(append([]string{filepath.Base(name)}, args...), " ")
		if strings.HasPrefix(line, "sudo -n -l ") && !allowed {
			return "sudo: a password is required\n", errors.New("exit status 1")
		}
		return out[line], nil
	}}
}

func TestLidStatusSaysWhatToDoAndWhatIrieDid(t *testing.T) {
	if !lid.Supported() {
		t.Skip("a lid is kept open only on a Mac")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	p, err := paths.New(func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	facts := map[string]string{
		"ioreg -r -k AppleClamshellState -d 1":                "    \"AppleClamshellState\" = Yes\n",
		"pmset -g batt":                                       "Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t42%; discharging\n",
		"notifyutil -g com.apple.system.thermalpressurelevel": "com.apple.system.thermalpressurelevel 1\n",
		"pmset -g": " SleepDisabled\t\t1\n",
	}
	var b strings.Builder
	lidStatus(&b, config.Default().Lid, p, lidFake(false, facts), time.Now())
	for _, want := range []string{"lid.awake = work", "Not set up: run irie setup lid", "Irie has not kept it awake", "its lid is closed; on battery at 42%; heat moderate; sleep is off, but not by Irie."} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, b.String())
		}
	}

	now := time.Now()
	lid.Write(p.LidFile(), lid.State{Off: true, Since: now.Add(-time.Hour), Why: "sessions work"})
	os.MkdirAll(filepath.Dir(lid.AgentPath(home)), 0o755)
	os.WriteFile(lid.AgentPath(home), []byte("plist"), 0o644)
	b.Reset()
	lidStatus(&b, config.Default().Lid, p, lidFake(true, facts), now)
	for _, want := range []string{"Set up.", "Now: Irie keeps the Mac awake, since", "(sessions work)"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, b.String())
		}
	}
	if strings.Contains(b.String(), "not by Irie") {
		t.Errorf("sleep Irie turned off was called someone else's:\n%s", b.String())
	}
}

func TestLidCheckTurnsSleepBackOnForAStaleHold(t *testing.T) {
	file := filepath.Join(t.TempDir(), "lid.json")
	// A hold no core has touched for ten minutes, with sudo standing in
	// to fail: the check tries, and says why it could not.
	lid.Write(file, lid.State{Off: true, Since: time.Now().Add(-time.Hour), Why: "sessions work"})
	old := time.Now().Add(-10 * time.Minute)
	os.Chtimes(file, old, old)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\necho \"$*\" >> "+filepath.Join(bin, "sudo.log")+"\n"), 0o755)
	os.WriteFile(filepath.Join(bin, "ioreg"), []byte("#!/bin/sh\necho '\"AppleClamshellState\" = No'\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := lidCmd([]string{"check", "--file", file}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(bin, "sudo.log"))
	if strings.TrimSpace(string(b)) != "-n /usr/bin/pmset -a disablesleep 0" {
		t.Errorf("sudo ran %q", b)
	}
	if s, _, _ := lid.Read(file); s.Off {
		t.Errorf("lid.json = %+v, want sleep back on", s)
	}
}
