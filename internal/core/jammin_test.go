//go:build !nojammin

package core

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/jammin"
	"github.com/irie-people/irie/internal/tmux"
)

// noScripts is a Runner that fails the test if any script runs: with the
// Music app not running, none may.
type noScripts struct{ t *testing.T }

func (n noScripts) Run(_ context.Context, _, script string, _ ...string) (string, error) {
	n.t.Errorf("a script ran: %.40q", script)
	return "", nil
}

// jamminCore is a core with Jammin on, whose music asks nothing of anyone:
// no Spotify app yet, and a Music app that isn't running.
func jamminCore(t *testing.T, cfg config.Config) *Core {
	t.Helper()
	c := bareCore(t, cfg)
	c.jammin.once.Do(func() {
		c.jammin.sv = &jammin.Service{
			Dir:      t.TempDir(),
			Store:    jammin.Files{Dir: t.TempDir()},
			Config:   func() config.Jammin { return c.cfg().Jammin },
			Script:   noScripts{t},
			Running:  func(string) bool { return false },
			API:      "http://127.0.0.1:9/v1",
			Accounts: "http://127.0.0.1:9",
		}
	})
	return c
}

// TestTheJamminOp: a look says how each source stands, :jammin answers it
// in words, and the plugins op shows the sources' states from the core's
// last look.
func TestTheJamminOp(t *testing.T) {
	c := jamminCore(t, config.Default())
	r, err := c.jamminOp(0, ipc.JamminArgs{})
	if err != nil || len(r.Sources) != 2 || r.Sources[0].Name != ipc.JamSpotify || r.Sources[0].State != ipc.JamNeeds || r.Sources[1].Name != ipc.JamMusic || r.Sources[1].State != ipc.JamNeeds {
		t.Fatalf("look: %+v, %v", r, err)
	}
	if !strings.Contains(r.Text, "No Spotify app yet") || !strings.Contains(r.Text, "Apple Music: The Music app isn't open") {
		t.Errorf("text = %q", r.Text)
	}
	in, err := c.input(ipc.InputArgs{Text: ":jammin"}, 0)
	if err != nil || in.Did != ipc.InputAnswered || in.Op != ipc.OpJammin || !strings.Contains(in.Text, "Spotify:") {
		t.Errorf(":jammin = %+v, %v", in, err)
	}
	if _, err := c.input(ipc.InputArgs{Text: ":jammin spotify next"}, 0); err == nil || !strings.Contains(err.Error(), "No Spotify app yet") {
		t.Errorf(":jammin spotify next with no app: %v", err)
	}
	p, err := c.pluginsOp(ipc.PluginsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	for _, pl := range p.Plugins {
		if pl.Name != "jammin" {
			continue
		}
		if !pl.On || len(pl.Parts) != 2 || pl.Parts[0].State != "needs" || !strings.Contains(pl.Parts[0].Detail, "developer.spotify.com") {
			t.Errorf("jammin plugin = %+v", pl)
		}
		return
	}
	t.Error("no jammin plugin")
}

// TestAnAgentCannotJam: what a person listens to is theirs, so every call
// from inside an agent is refused, a look as much as a play, typed words
// too; and the plugin off refuses with what turns it on.
func TestAnAgentCannotJam(t *testing.T) {
	c := jamminCore(t, config.Default())
	// This test's own parent stands for an agent's host.
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	for _, a := range []ipc.JamminArgs{{}, {Do: ipc.JamPlay}, {Do: ipc.JamSearch, Query: "x"}, {Do: ipc.JamSignIn, ClientID: "0123456789abcdef0123456789abcdef"}} {
		if _, err := c.jamminOp(os.Getpid(), a); err == nil || !strings.Contains(err.Error(), "an agent can't see or play their music") {
			t.Errorf("%+v from an agent: %v", a, err)
		}
	}
	if _, err := c.CallFrom(os.Getpid(), ipc.OpJammin, []byte(`{"do":"pause"}`)); err == nil {
		t.Error("the op from an agent went through")
	}
	if _, err := c.input(ipc.InputArgs{Text: ":jammin pause"}, os.Getpid()); err == nil {
		t.Error(":jammin from an agent went through")
	}
	if c.cfg().Jammin.Spotify.ClientID != "" {
		t.Error("an agent's sign-in kept a Client ID")
	}
	if _, err := c.jamminOp(0, ipc.JamminArgs{}); err != nil {
		t.Errorf("the person's own call: %v", err)
	}

	off := config.Default()
	off.Plugins.Jammin = false
	c = jamminCore(t, off)
	if _, err := c.jamminOp(0, ipc.JamminArgs{}); err == nil || !strings.Contains(err.Error(), "plugins on jammin") {
		t.Errorf("plugin off: %v", err)
	}
	if _, err := c.input(ipc.InputArgs{Text: ":jammin"}, 0); err == nil || !strings.Contains(err.Error(), "plugins on jammin") {
		t.Errorf(":jammin with the plugin off: %v", err)
	}
}
