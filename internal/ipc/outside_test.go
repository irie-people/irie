package ipc

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestOutsideFrameAlwaysListsTheSessions(t *testing.T) {
	b, err := json.Marshal(Frame{T: FrameOutside})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"t":"outside","outside":[]}` {
		t.Errorf("empty outside frame = %s", b)
	}
	b, err = json.Marshal(Frame{T: FrameOutside, Problem: "claude: command not found"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"t":"outside","outside":[],"problem":"claude: command not found"}` {
		t.Errorf("outside frame with a problem = %s", b)
	}
}

func TestOutsideFrameRoundTrips(t *testing.T) {
	o := OutsideSession{
		VendorID: "6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f", Name: "auth refactor", Kind: KindInteractive,
		Status: StatusIdle, PID: 4242, Cwd: "/home/alice/code/website",
		Started: time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC), Movable: true,
	}
	b, err := json.Marshal(Frame{T: FrameOutside, Outside: []OutsideSession{o}})
	if err != nil {
		t.Fatal(err)
	}
	var f Frame
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if f.T != FrameOutside || len(f.Outside) != 1 || !reflect.DeepEqual(f.Outside[0], o) || f.Sessions != nil {
		t.Errorf("round trip of %s = %+v", b, f)
	}
}

func TestOutsideKeys(t *testing.T) {
	started := time.UnixMilli(1761487200000)
	for _, c := range []struct {
		o    OutsideSession
		want string
	}{
		{OutsideSession{VendorID: "6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f", ID: "7c5dcf5d", PID: 7}, "6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f"},
		{OutsideSession{ID: "7c5dcf5d", PID: 7}, "id:7c5dcf5d"},
		{OutsideSession{PID: 7}, "pid:7"},
		{OutsideSession{Cwd: "/home/alice", Started: started}, "/home/alice@1761487200000"},
	} {
		if got := c.o.Key(); got != c.want {
			t.Errorf("Key(%+v) = %q, want %q", c.o, got, c.want)
		}
	}
}
