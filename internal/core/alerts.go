package core

// macOS's own alerts (DESIGN §4.11, Raphael). Two kinds of macOS's own
// notices stay on screen until they are closed, and System Settings ›
// Notifications does not list them: alerts.background and alerts.privacy
// are the person's choice of style for them, which the core keeps, as it
// starts and as the setting changes. Raffi's look says which stay, and his
// fix, on the person's OK, makes the same change and keeps it as their
// choice.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/raphael"
)

// alertsKeys are the settings of macOS's senders of notices, by each one's
// id in its notification settings.
var alertsKeys = map[string]string{
	"com.apple.BTMNotificationAgent": "alerts.background",
	"com.apple.tccd":                 "alerts.privacy",
}

// alertsWant is the style the person chose for each sender, by its id; a
// sender left as macOS has it is missing.
func alertsWant(a config.Alerts) map[string]string {
	w := map[string]string{}
	if a.Background != config.AlertsAsMacOS {
		w["com.apple.BTMNotificationAgent"] = a.Background
	}
	if a.Privacy != config.AlertsAsMacOS {
		w["com.apple.tccd"] = a.Privacy
	}
	return w
}

// alertsDelay is how long after it starts the core first keeps the alert
// styles: macOS's notices may still be starting at a login.
var alertsDelay = 30 * time.Second

// alertsStart keeps the alert styles once the core has been up a while.
func (c *Core) alertsStart() {
	if runtime.GOOS != "darwin" || raphaelOff() {
		return
	}
	time.Sleep(alertsDelay)
	c.alertsKeep()
}

// alertsKeep puts the style the person chose back on each sender whose
// style differs, one keep at a time. Each change keeps a copy of macOS's
// notification settings as they were, in the state folder.
func (c *Core) alertsKeep() {
	if runtime.GOOS != "darwin" || raphaelOff() {
		return
	}
	c.alertsMu.Lock()
	defer c.alertsMu.Unlock()
	want := alertsWant(c.cfg().Alerts)
	for _, n := range raphael.Notifiers {
		style, ok := want[n.ID]
		if !ok {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		changed, err := raphael.SetStyle(ctx, c.raph.run(), n.ID, style, raphael.NCBackupDir(c.paths.State))
		cancel()
		switch {
		case err != nil:
			c.log.Warn("alerts: macOS's style for "+n.Name, "want", style, "err", err)
		case changed:
			c.log.Info("alerts: macOS shows "+n.Name+"'s notices as you chose", "style", style)
		}
	}
}

// raphaelSigned are the programs whose signatures say whether macOS keeps
// what the person allows them: the app wherever it is installed, and the
// command.
func (c *Core) raphaelSigned() []string {
	home, _ := os.UserHomeDir()
	out := []string{filepath.Join(home, "Applications", brand.App), filepath.Join("/Applications", brand.App)}
	if c.bin != "" {
		out = append(out, c.bin)
	}
	return out
}

// keepRaphaelStyle keeps the style a fix of Raffi's set as the person's
// choice (alerts.*), so that the core puts it back should macOS change it.
func (c *Core) keepRaphaelStyle(f raphael.Fix) {
	id, style, ok := f.Style()
	key := alertsKeys[id]
	if !ok || key == "" {
		return
	}
	v, _ := json.Marshal(style)
	if err := c.configure(c.paths.ConfigFile(), []ipc.ConfigSet{{Key: key, Value: v}}); err != nil {
		c.log.Warn("alerts: keep Raffi's style as the choice", "key", key, "err", err)
	}
}
