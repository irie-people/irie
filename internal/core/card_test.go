package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// TestTheCardOp: a card is asked of every Mac app open, for a session the
// core has, of a kind the app has; a plugin's card waits for its plugin;
// and with no app open it says so.
func TestTheCardOp(t *testing.T) {
	c := bareCore(t, config.Default())
	x := &session{id: "s0000000000c1", name: "inbox"}
	c.mu.Lock()
	c.sessions[x.id] = x
	c.mu.Unlock()
	if err := c.cardOp(ipc.CardArgs{Session: x.id, Kind: "fax"}); err == nil || !strings.Contains(err.Error(), "mail") {
		t.Errorf("an unknown kind: %v", err)
	}
	if err := c.cardOp(ipc.CardArgs{Session: "s0000000000zz", Kind: "mail"}); err == nil || !strings.Contains(err.Error(), "No session") {
		t.Errorf("an unknown session: %v", err)
	}
	if err := c.cardOp(ipc.CardArgs{Session: x.id, Kind: "markets"}); err == nil || !strings.Contains(err.Error(), "plugins on markets") {
		t.Errorf("markets with its plugin off: %v", err)
	}
	if err := c.cardOp(ipc.CardArgs{Session: x.id, Kind: "mail"}); err == nil || !strings.Contains(err.Error(), "app is open") {
		t.Errorf("no app open: %v", err)
	}

	raw, _ := json.Marshal(ipc.SubscribeArgs{View: ipc.ViewApp})
	frames, cancel, err := c.Subscribe(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := c.cardOp(ipc.CardArgs{Session: x.id, Kind: "mail"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f := <-frames:
			if f.T != ipc.FrameCard {
				continue
			}
			if f.ID != x.id || f.Kind != "mail" {
				t.Fatalf("card frame %+v", f)
			}
			b, _ := json.Marshal(f)
			if string(b) != `{"t":"card","id":"s0000000000c1","kind":"mail"}` {
				t.Errorf("on the wire: %s", b)
			}
			return
		case <-deadline:
			t.Fatal("the app got no card frame")
		}
	}
}
