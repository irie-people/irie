package remote

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/irie-people/irie/internal/ipc"
)

// hostKey marks a row of one of the core's peers, another machine's
// session that the core shows in its list (docs/proposals/multi-host.md
// §4.5).
var hostKey = []byte(`"host":"`)

// hidePeers leaves the core's peers' rows out of a frame or a list for a
// phone. A phone pairs with each machine and shows each one's own
// sessions, so a row of another machine's would show twice; and a core
// paired with this machine through its gateway must hear only this
// machine's sessions, never its peers' again. Most frames name no host
// and pass as they are.
func hidePeers(b []byte) []byte {
	if !bytes.Contains(b, hostKey) && !bytes.HasPrefix(b, []byte(`{"t":"remove"`)) {
		return b
	}
	var f ipc.Frame
	if json.Unmarshal(b, &f) != nil {
		return b
	}
	switch f.T {
	case ipc.FrameSnapshot:
		if !bytes.Contains(b, hostKey) {
			return b
		}
		f.Sessions = ownRows(f.Sessions)
	case ipc.FrameUpsert:
		if f.Session == nil || f.Session.Host == "" {
			return b
		}
		return nil
	case ipc.FrameRemove:
		if !strings.Contains(f.ID, "/") {
			return b
		}
		return nil
	default:
		return b
	}
	out, err := json.Marshal(f)
	if err != nil {
		return b
	}
	return out
}

// hideInList leaves the peers' rows out of a list result.
func hideInList(raw json.RawMessage) json.RawMessage {
	if !bytes.Contains(raw, hostKey) {
		return raw
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	var rows []ipc.Session
	if json.Unmarshal(m["sessions"], &rows) != nil {
		return raw
	}
	b, err := json.Marshal(ownRows(rows))
	if err != nil {
		return raw
	}
	m["sessions"] = b
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

func ownRows(rows []ipc.Session) []ipc.Session {
	out := make([]ipc.Session, 0, len(rows))
	for _, s := range rows {
		if s.Host == "" {
			out = append(out, s)
		}
	}
	return out
}
