package remote

import (
	"encoding/json"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

func TestAPhoneHearsOnlyThisMachinesSessions(t *testing.T) {
	frame := func(f ipc.Frame) []byte {
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	own := ipc.Session{ID: "s1", Name: "here", State: ipc.StateWorking}
	theirs := ipc.Session{ID: "Asahi/s9", Name: "there", State: ipc.StateIdle, Host: "Asahi"}

	snap := hidePeers(frame(ipc.Frame{T: ipc.FrameSnapshot, Sessions: []ipc.Session{own, theirs}, Header: &ipc.Header{Version: "v"}}))
	var f ipc.Frame
	if err := json.Unmarshal(snap, &f); err != nil || len(f.Sessions) != 1 || f.Sessions[0].ID != "s1" || f.Header == nil {
		t.Errorf("snapshot = %s, %v", snap, err)
	}
	if b := hidePeers(frame(ipc.Frame{T: ipc.FrameUpsert, Session: &theirs})); b != nil {
		t.Errorf("a peer's upsert passed: %s", b)
	}
	if b := hidePeers(frame(ipc.Frame{T: ipc.FrameRemove, ID: "Asahi/s9"})); b != nil {
		t.Errorf("a peer's remove passed: %s", b)
	}
	for _, f := range []ipc.Frame{{T: ipc.FrameUpsert, Session: &own}, {T: ipc.FrameRemove, ID: "s1"}, {T: ipc.FrameHeader, Header: &ipc.Header{}}} {
		in := frame(f)
		if out := hidePeers(in); string(out) != string(in) {
			t.Errorf("%s became %s", in, out)
		}
	}

	list, _ := json.Marshal(ipc.ListResult{Sessions: []ipc.Session{own, theirs}})
	var l ipc.ListResult
	if err := json.Unmarshal(hideInList(list), &l); err != nil || len(l.Sessions) != 1 || l.Sessions[0].ID != "s1" {
		t.Errorf("list = %+v, %v", l, err)
	}
}
