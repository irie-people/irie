package core

import (
	"encoding/json"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/raphael"
)

func TestAlertsWantNamesTheSendersChosen(t *testing.T) {
	if w := alertsWant(config.Alerts{}); len(w) != 0 {
		t.Errorf("none chosen: %v", w)
	}
	w := alertsWant(config.Alerts{Background: config.AlertsBanners, Privacy: config.AlertsAlerts})
	if w["com.apple.BTMNotificationAgent"] != "banners" || w["com.apple.tccd"] != "alerts" || len(w) != 2 {
		t.Errorf("chosen: %v", w)
	}
	for _, n := range raphael.Notifiers {
		if alertsKeys[n.ID] == "" {
			t.Errorf("%s has no setting", n.ID)
		}
	}
}

func TestRaffisStyleFixIsKeptAsTheChoice(t *testing.T) {
	c, f := raphCore(t)
	var fix raphael.Fix
	if err := json.Unmarshal([]byte(`{"label":"Let them go by themselves","run":[],"about":"","kind":"ncstyle","ident":"com.apple.BTMNotificationAgent","path":"banners"}`), &fix); err != nil {
		t.Fatal(err)
	}
	c.keepRaphaelStyle(fix)
	if got := c.cfg().Alerts.Background; got != config.AlertsBanners {
		t.Errorf("alerts.background = %q", got)
	}
	if b, _ := os.ReadFile(c.paths.ConfigFile()); !strings.Contains(string(b), `background = "banners"`) {
		t.Errorf("config.toml =\n%s", b)
	}
	// The change has the core keep the style, through Raffi's runner, on
	// a Mac.
	deadline := time.Now().Add(5 * time.Second)
	for runtime.GOOS == "darwin" && !slices.ContainsFunc(f.lines(), func(l string) bool { return strings.HasPrefix(l, "/usr/bin/defaults export com.apple.ncprefs") }) {
		if time.Now().After(deadline) {
			t.Fatalf("the core didn't keep the style it was set to: ran %q", f.lines())
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.alertsMu.Lock() // the keep under way ends
	c.alertsMu.Unlock()
	// Another fix keeps no choice.
	var other raphael.Fix
	json.Unmarshal([]byte(`{"label":"Clear Go's build cache","run":[],"about":"","kind":"tool"}`), &other)
	c.keepRaphaelStyle(other)
	if got := c.cfg().Alerts; got != (config.Alerts{Background: config.AlertsBanners}) {
		t.Errorf("alerts = %+v", got)
	}
}
