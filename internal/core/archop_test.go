package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/arch"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
)

func TestArchListShowsEveryArchAgent(t *testing.T) {
	t.Setenv("IRIE_GABRIEL", "")
	cfg := config.Default()
	cfg.Arch.Mikael.Good = []string{"Give credit where it is due."}
	c := &Core{sessions: map[string]*session{}}
	c.useConfig(cfg)
	r := c.archList()
	if len(r.Agents) != len(arch.All()) {
		t.Fatalf("agents = %d, want every one of %v", len(r.Agents), arch.Names())
	}
	m, g := r.Agents[0], r.Agents[1]
	if m.Name != "mikael" || m.Icon == "" || m.Title == "" || m.About == "" || !m.On || m.Status == "" {
		t.Errorf("mikael = %+v", m)
	}
	if len(m.Actions) != 1 || m.Actions[0].Op != ipc.OpSpawn || !strings.Contains(string(m.Actions[0].Args), `"profile":"mikael"`) {
		t.Errorf("mikael's actions = %+v", m.Actions)
	}
	// Settings come with their values, as the config op has them.
	values := map[string]string{}
	for _, s := range append(m.Settings, g.Settings...) {
		if s.Kind == "" || s.Label == "" || !strings.HasPrefix(s.Key, "arch.") {
			t.Errorf("setting %+v", s)
		}
		values[s.Key] = string(s.Value)
	}
	for key, want := range map[string]string{
		"arch.mikael.on":     "true",
		"arch.mikael.good":   `["Give credit where it is due."]`,
		"arch.gabriel.away":  `"5m"`,
		"arch.gabriel.boost": `"1h"`,
		"arch.gabriel.heavy": "50",
	} {
		if values[key] != want {
			t.Errorf("%s = %s, want %s", key, values[key], want)
		}
	}
	if g.Name != "gabriel" || g.Icon == "" {
		t.Errorf("gabriel = %+v", g)
	}
	if proc.CanBackground {
		if len(g.Actions) != 1 || g.Actions[0].Label != "Boost for 1 hour" || g.Actions[0].Op != ipc.OpGabriel {
			t.Errorf("gabriel's actions = %+v", g.Actions)
		}
		var args ipc.GabrielArgs
		if err := json.Unmarshal(g.Actions[0].Args, &args); err != nil || !args.Boost {
			t.Errorf("boost args = %s, %v", g.Actions[0].Args, err)
		}
		// A boost on offers its end instead.
		c.gab.boostUntil = time.Now().Add(time.Hour)
		if g := c.archList().Agents[1]; len(g.Actions) != 1 || g.Actions[0].Label != "Stop the boost" || !strings.Contains(g.Status, "Boosting until") {
			t.Errorf("boosting: %+v", g)
		}
	}
	// Gabriel off is said so, with nothing to do.
	t.Setenv("IRIE_GABRIEL", "off")
	if g := c.archList().Agents[1]; g.On || len(g.Actions) != 0 || !strings.HasPrefix(g.Status, "Off") {
		t.Errorf("Gabriel off: %+v", g)
	}
}

func TestSpokenFor(t *testing.T) {
	for d, want := range map[time.Duration]string{time.Hour: "1 hour", 2 * time.Hour: "2 hours", 30 * time.Minute: "30 minutes", 90 * time.Minute: "1 hour 30 minutes", time.Minute: "1 minute"} {
		if got := spokenFor(d); got != want {
			t.Errorf("spokenFor(%v) = %q, want %q", d, got, want)
		}
	}
}
