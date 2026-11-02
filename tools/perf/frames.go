package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// Frames is what one subscription heard: frames by type, with their sizes,
// which fields each upsert changed, and which parts of the header each
// header frame changed.
type Frames struct {
	Total int                  `json:"total"`
	Bytes int64                `json:"bytes"`
	Types map[string]FrameType `json:"types"`
	// Snapshots after the first, which subscribing brings: each one sent
	// every row again.
	LaterSnapshots int `json:"later_snapshots"`
	// SnapshotRows is how many rows those snapshots carried, and
	// SnapshotChanged how many of them had changed.
	SnapshotRows    int `json:"snapshot_rows"`
	SnapshotChanged int `json:"snapshot_changed"`
	// Upserts by field: how many upserts changed it.
	UpsertFields map[string]int `json:"upsert_fields"`
	// UpsertsSame is upserts that changed nothing the client had.
	UpsertsSame int `json:"upserts_same"`
	// HeaderFields by part, such as plan.five_hour: how many header frames
	// changed it. HeadersSame is header frames that changed nothing.
	HeaderFields map[string]int `json:"header_fields"`
	HeadersSame  int            `json:"headers_same"`
	// Rows is how many rows the client had at the end.
	Rows int `json:"rows"`
}

// FrameType is how many frames of one type came, and their bytes.
type FrameType struct {
	N     int   `json:"n"`
	Bytes int64 `json:"bytes"`
}

// counter keeps what the client knows, as a client would, to tell what each
// frame changed.
type counter struct {
	f      Frames
	rows   map[string]map[string]json.RawMessage
	header map[string]json.RawMessage
	seen   bool // the first snapshot came
}

func newCounter() *counter {
	return &counter{
		f:    Frames{Types: map[string]FrameType{}, UpsertFields: map[string]int{}, HeaderFields: map[string]int{}},
		rows: map[string]map[string]json.RawMessage{},
	}
}

// take counts one frame, a line as the core sent it.
func (c *counter) take(line []byte) {
	var f struct {
		T        string                       `json:"t"`
		Session  map[string]json.RawMessage   `json:"session"`
		Sessions []map[string]json.RawMessage `json:"sessions"`
		ID       string                       `json:"id"`
		Header   map[string]json.RawMessage   `json:"header"`
	}
	if json.Unmarshal(line, &f) != nil || f.T == "" {
		return
	}
	n := int64(len(line))
	c.f.Total++
	c.f.Bytes += n
	t := c.f.Types[f.T]
	t.N++
	t.Bytes += n
	c.f.Types[f.T] = t
	switch f.T {
	case ipc.FrameSnapshot:
		later := c.seen
		c.seen = true
		now := map[string]map[string]json.RawMessage{}
		for _, row := range f.Sessions {
			id := str(row["id"])
			if later {
				c.f.SnapshotRows++
				if len(changed(c.rows[id], row, false)) > 0 {
					c.f.SnapshotChanged++
				}
			}
			now[id] = row
		}
		c.rows = now
		if later {
			c.f.LaterSnapshots++
		}
		if f.Header != nil {
			c.header = f.Header
		}
	case ipc.FrameUpsert:
		if f.Session == nil {
			return
		}
		id := str(f.Session["id"])
		fields := changed(c.rows[id], f.Session, false)
		if len(fields) == 0 {
			c.f.UpsertsSame++
		}
		for _, k := range fields {
			c.f.UpsertFields[k]++
		}
		c.rows[id] = f.Session
	case ipc.FrameRemove:
		delete(c.rows, f.ID)
	case ipc.FrameHeader:
		parts := changed(c.header, f.Header, true)
		if len(parts) == 0 {
			c.f.HeadersSame++
		}
		for _, k := range parts {
			c.f.HeaderFields[k]++
		}
		c.header = f.Header
	}
}

// changed lists the fields of b that differ from a, a field that went
// missing included; deep names an object's own fields too, as
// plan.five_hour. A row the client did not have yet changed in every field.
func changed(a, b map[string]json.RawMessage, deep bool) []string {
	var out []string
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	for k := range keys {
		x, y := a[k], b[k]
		if bytes.Equal(x, y) {
			continue
		}
		if deep {
			var ox, oy map[string]json.RawMessage
			if json.Unmarshal(x, &ox) == nil && json.Unmarshal(y, &oy) == nil && (ox != nil || oy != nil) {
				for _, sub := range changed(ox, oy, false) {
					out = append(out, k+"."+sub)
				}
				continue
			}
		}
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func str(raw json.RawMessage) string {
	var s string
	json.Unmarshal(raw, &s)
	return s
}

// listen subscribes to the core at sock as a client that sees every
// session, as the tmux sidebar does, and counts what comes until stop
// closes. It returns what it heard, or why it could not subscribe.
func listen(sock string, stop <-chan struct{}) (Frames, error) {
	conn, err := net.DialTimeout("unix", sock, 5*time.Second)
	if err != nil {
		return Frames{}, err
	}
	defer conn.Close()
	b, _ := json.Marshal(ipc.SubscribeArgs{})
	req, _ := json.Marshal(ipc.Request{ID: 1, Op: ipc.OpSubscribe, Args: b})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return Frames{}, err
	}
	r := bufio.NewReaderSize(conn, 1<<20)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	first, err := r.ReadBytes('\n')
	if err != nil {
		return Frames{}, fmt.Errorf("subscribe: %w", err)
	}
	var rep ipc.Reply
	if err := json.Unmarshal(first, &rep); err != nil || !rep.OK {
		if rep.Error != nil {
			return Frames{}, rep.Error
		}
		return Frames{}, errors.New("subscribe: no reply")
	}
	conn.SetReadDeadline(time.Time{})
	go func() {
		<-stop
		conn.Close()
	}()
	c := newCounter()
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			c.take(line[:len(line)-1])
			c.f.Rows = len(c.rows)
		}
		if err != nil {
			select {
			case <-stop:
				return c.f, nil
			default:
			}
			if errors.Is(err, io.EOF) {
				return c.f, errors.New("the core closed the subscription")
			}
			return c.f, err
		}
	}
}
