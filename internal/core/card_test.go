package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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

// TestACodeCardCarriesItsFile: a code card names a file that exists, with
// where to put the cursor, and the app gets all three; a path the card
// can't open, or on a card that takes none, is refused.
func TestACodeCardCarriesItsFile(t *testing.T) {
	c := bareCore(t, config.Default())
	x := &session{id: "s0000000000c2", name: "editor"}
	c.mu.Lock()
	c.sessions[x.id] = x
	c.mu.Unlock()
	raw, _ := json.Marshal(ipc.SubscribeArgs{View: ipc.ViewApp})
	frames, cancel, err := c.Subscribe(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	dir := t.TempDir()
	good := filepath.Join(dir, "main.go")
	if err := os.WriteFile(good, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(ipc.CardFileMax + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	edge := filepath.Join(dir, "edge.bin")
	f, err = os.Create(edge)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(ipc.CardFileMax); err != nil {
		t.Fatal(err)
	}
	f.Close()
	link := filepath.Join(dir, "link.go")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}

	for name, a := range map[string]ipc.CardArgs{
		"a missing file":      {Kind: "code", Path: filepath.Join(dir, "nope.go")},
		"a folder":            {Kind: "code", Path: dir},
		"a relative path":     {Kind: "code", Path: "main.go"},
		"an unclean path":     {Kind: "code", Path: dir + "/sub/../main.go"},
		"a too big file":      {Kind: "code", Path: big},
		"a path on a browser": {Kind: "browser", Path: good},
		"a negative line":     {Kind: "code", Path: good, Line: -1},
		"a negative column":   {Kind: "code", Path: good, Line: 1, Col: -2},
	} {
		a.Session = x.id
		err := c.cardOp(a)
		if err == nil || code(err) != ipc.CodeBadArgs {
			t.Errorf("%s: %v", name, err)
		}
	}

	next := func() ipc.Frame {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case f := <-frames:
				if f.T == ipc.FrameCard {
					return f
				}
			case <-deadline:
				t.Fatal("the app got no card frame")
			}
		}
	}
	for _, p := range []string{good, link, edge} {
		if err := c.cardOp(ipc.CardArgs{Session: x.id, Kind: "code", Path: p, Line: 12, Col: 3}); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		fr := next()
		if fr.Kind != "code" || fr.Path != p || fr.Line != 12 || fr.Col != 3 {
			t.Errorf("card frame %+v", fr)
		}
	}
	if err := c.cardOp(ipc.CardArgs{Session: x.id, Kind: "code", Path: good, Line: 12, Col: 3}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(next())
	if want := `{"t":"card","id":"s0000000000c2","kind":"code","path":` + strconv.Quote(good) + `,"line":12,"col":3}`; string(b) != want {
		t.Errorf("on the wire: %s, want %s", b, want)
	}
	// A code card with no file is the session's code card.
	if err := c.cardOp(ipc.CardArgs{Session: x.id, Kind: "code"}); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(next())
	if string(b) != `{"t":"card","id":"s0000000000c2","kind":"code"}` {
		t.Errorf("on the wire: %s", b)
	}
}
