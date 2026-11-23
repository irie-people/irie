package core

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
)

func TestAnAgentCannotPressKeys(t *testing.T) {
	c := bareCore(t, config.Default())
	// This test's own parent stands for an agent's host.
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	raw, err := json.Marshal(ipc.KeysArgs{Session: "s000000000000", Keys: []string{"1", "Enter"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CallFrom(os.Getpid(), ipc.OpKeys, raw); err == nil || !strings.Contains(err.Error(), "An agent can't press keys") {
		t.Errorf("keys from an agent: %v", err)
	}
	// A person's client, such as the phone's gateway, gets as far as the
	// session, which this core doesn't have.
	var e *ipc.Error
	if _, err := c.CallFrom(0, ipc.OpKeys, raw); !errors.As(err, &e) || e.Code != ipc.CodeNotFound {
		t.Errorf("keys from a person's client: %v", err)
	}
}

// TestAnAgentCannotTurnAPluginOn: a plugin of yours can run your programs
// and give every agent its tools, so neither the plugins op nor a setting
// turns one on for an agent; turning one off, and the person, can.
func TestAnAgentCannotTurnAPluginOn(t *testing.T) {
	c := bareCore(t, config.Default())
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	plugin := func(peer int, name string, on bool) error {
		raw, err := json.Marshal(ipc.PluginsArgs{Plugin: name, On: &on})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.CallFrom(peer, ipc.OpPlugins, raw)
		return err
	}
	// One of the plugins this build has: markets where it is built, which
	// starts off, or else another that drives nothing outside, so that a
	// copy that leaves plugins out checks the same. Never the VPN.
	isOn := func(name string) (on, found bool) {
		t.Helper()
		r, err := c.pluginsOp(ipc.PluginsArgs{})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range r.Plugins {
			if p.Name == name {
				return p.On, true
			}
		}
		return false, false
	}
	name := ""
	for _, n := range []string{"markets", "obsidian", "chat", "bitwarden"} {
		if _, ok := isOn(n); ok {
			name = n
			break
		}
	}
	if name != "" {
		if err := plugin(0, name, false); err != nil {
			t.Fatalf("%s off, from the person: %v", name, err)
		}
		if err := plugin(os.Getpid(), name, true); err == nil || !strings.Contains(err.Error(), "An agent can't turn a plugin on") {
			t.Errorf("%s on, from an agent: %v", name, err)
		}
		if on, _ := isOn(name); on {
			t.Errorf("the agent turned %s on", name)
		}
		if err := plugin(0, name, true); err != nil {
			t.Errorf("%s on, from the person: %v", name, err)
		} else if on, _ := isOn(name); !on {
			t.Errorf("the person turned %s on, and it is off", name)
		}
		if err := plugin(os.Getpid(), name, false); err != nil {
			t.Errorf("%s off, from an agent: %v", name, err)
		} else if on, _ := isOn(name); on {
			t.Errorf("an agent turned %s off, and it is on", name)
		}
	}
	set := func(peer int, key string, value bool) error {
		v, _ := json.Marshal(value)
		_, err := c.settingsFrom(peer, ipc.ConfigArgs{Set: []ipc.ConfigSet{{Key: key, Value: v}}})
		return err
	}
	if err := set(os.Getpid(), "plugins.manifests.listener", true); err == nil || !strings.Contains(err.Error(), "plugins.manifests.listener") {
		t.Errorf("a manifest's switch on, from an agent: %v", err)
	}
	if err := set(0, "plugins.manifests.listener", true); err != nil || !c.cfg().Plugins.Manifests["listener"] {
		t.Errorf("a manifest's switch on, from the person: %v", err)
	}
	if err := set(os.Getpid(), "plugins.manifests.listener", false); err != nil || c.cfg().Plugins.Manifests["listener"] {
		t.Errorf("a manifest's switch off, from an agent: %v", err)
	}
}
