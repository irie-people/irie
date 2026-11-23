package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/filetx"
)

// Version is the way the ledger prices calls. A session's totals written
// another way are priced again from its transcripts.
const Version = 1

// Row is one of a Sum's tallies as a file keeps it.
type Row struct {
	Key
	Tally
}

// Totals are a session's calls added up, as its costs.json keeps them.
type Totals struct {
	V    int   `json:"v"`
	Rows []Row `json:"rows"`
}

// Totals are s as a file keeps it, its rows in order.
func (s Sum) Totals() Totals {
	t := Totals{V: Version, Rows: []Row{}}
	for k, v := range s {
		t.Rows = append(t.Rows, Row{Key: k, Tally: *v})
	}
	slices.SortFunc(t.Rows, func(a, b Row) int {
		return strings.Compare(a.Day+"\x00"+a.Model+"\x00"+a.Purpose, b.Day+"\x00"+b.Model+"\x00"+b.Purpose)
	})
	return t
}

// Sum is t's calls added up again.
func (t Totals) Sum() Sum {
	s := Sum{}
	for _, r := range t.Rows {
		v := r.Tally
		s[r.Key] = &v
	}
	return s
}

// ReadTotals reads a session's totals, and false when it has none, or
// none priced the way this ledger prices.
func ReadTotals(path string) (Sum, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var t Totals
	if json.Unmarshal(b, &t) != nil || t.V != Version {
		return nil, false
	}
	return t.Sum(), true
}

// WriteTotals writes a session's totals, as a transaction.
func WriteTotals(path string, s Sum) error {
	b, err := json.Marshal(s.Totals())
	if err != nil {
		return err
	}
	return filetx.WriteFile(path, append(b, '\n'), 0o600)
}

// Entry is one of Irie's own calls, as ledger.jsonl keeps it: the call,
// where it ran, how long it took, and what it cost, which a call with no
// price leaves out.
type Entry struct {
	Call
	Server string   `json:"server,omitempty"` // the host of the server it went to
	Millis int64    `json:"ms,omitempty"`
	USD    *float64 `json:"usd,omitempty"`
}

// NewEntry is c as ledger.jsonl keeps it, priced.
func NewEntry(c Call, server string, took time.Duration) Entry {
	e := Entry{Call: c, Server: server, Millis: took.Milliseconds()}
	if usd, ok := c.Cost(); ok {
		e.USD = &usd
	}
	return e
}

// Append adds e to the ledger at path, a line of its own, written at once:
// the file is only ever appended to.
func Append(path string, e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return errors.Join(err, f.Close())
}

// ReadEntries reads the ledger at path from from on. A line a crash cut
// short is skipped.
func ReadEntries(path string, from time.Time) ([]Entry, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.At.Before(from) {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// Trim keeps the ledger at path from from on, as a transaction, when it
// holds anything older; it changes nothing otherwise. An Append while it
// runs could be lost, so the core trims only as it starts, before it
// makes any call.
func Trim(path string, from time.Time) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var kept bytes.Buffer
	cut := false
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e) != nil || e.At.Before(from) {
			cut = true
			continue
		}
		kept.Write(line)
		kept.WriteByte('\n')
	}
	if !cut {
		return nil
	}
	return filetx.WriteFile(path, kept.Bytes(), 0o600)
}

// SumOf adds entries up.
func SumOf(entries []Entry) Sum {
	s := Sum{}
	for _, e := range entries {
		s.Add(e.Call)
	}
	return s
}
