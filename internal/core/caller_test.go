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
