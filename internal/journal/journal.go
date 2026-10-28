// Package journal is the core's append-only record of events (DESIGN §5.4):
// one NDJSON file of envelopes per UTC day, YYYY-MM-DD.ndjson, with files
// 0600 in a 0700 directory, kept journal.keep_days days. Derived state is
// computed from it, so a restarted core reads it back.
package journal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/event"
)

// Options configure a journal.
type Options struct {
	KeepDays int              // days of files kept, today included
	Now      func() time.Time // the clock; time.Now when nil
}

// Journal appends events. It is safe for concurrent use.
type Journal struct {
	dir  string
	keep int
	now  func() time.Time

	mu  sync.Mutex
	seq uint64
	day string
	f   *os.File
}

const dayLayout = "2006-01-02"

var dayFile = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.ndjson$`)

// Open opens the journal in dir, creating dir when needed, removes days
// older than KeepDays, and carries on from the last seq it holds.
func Open(dir string, o Options) (*Journal, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	j := &Journal{dir: dir, keep: max(o.KeepDays, 1), now: o.Now}
	j.prune(j.now().UTC().Format(dayLayout))
	files, err := days(dir)
	if err != nil {
		return nil, err
	}
	for i := len(files) - 1; i >= 0 && j.seq == 0; i-- {
		evs, _ := readFile(filepath.Join(dir, files[i]), nil)
		for _, e := range evs {
			j.seq = max(j.seq, e.Seq)
		}
	}
	return j, nil
}

// Append stamps e with the envelope version, the next seq and, unless it has
// one, the current time, caps its data strings and writes it. An event that
// is not a valid envelope is refused and uses up no seq.
func (j *Journal) Append(e event.Event) (event.Event, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := j.now()
	e.V = event.Version
	if e.TS == "" {
		e.TS = event.Stamp(now)
	}
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	event.CapAll(e.Data)
	e.Seq = j.seq + 1
	if err := event.Validate(e); err != nil {
		return e, err
	}
	j.seq = e.Seq
	line, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	if err := j.open(now.UTC().Format(dayLayout)); err != nil {
		return e, err
	}
	_, err = j.f.Write(append(line, '\n'))
	return e, err
}

// open makes the file of day the one appended to.
func (j *Journal) open(day string) error {
	if j.f != nil && j.day == day {
		return nil
	}
	if j.f != nil {
		j.f.Close()
		j.f = nil
	}
	j.prune(day)
	path := filepath.Join(j.dir, day+".ndjson")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	// A line cut short by a crash is ended, so the next one stands alone.
	if r, err := os.Open(path); err == nil {
		if fi, err := r.Stat(); err == nil && fi.Size() > 0 {
			last := make([]byte, 1)
			if _, err := r.ReadAt(last, fi.Size()-1); err == nil && last[0] != '\n' {
				f.Write([]byte{'\n'})
			}
		}
		r.Close()
	}
	j.f, j.day = f, day
	return nil
}

// prune removes the day files that fall out of the window ending on today.
func (j *Journal) prune(today string) {
	t, err := time.Parse(dayLayout, today)
	if err != nil {
		return
	}
	cut := t.AddDate(0, 0, -j.keep).Format(dayLayout) + ".ndjson"
	files, _ := days(j.dir)
	for _, f := range files {
		if f <= cut {
			os.Remove(filepath.Join(j.dir, f))
		}
	}
}

// Close closes the journal's file.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return nil
	}
	err := j.f.Close()
	j.f = nil
	return err
}

// days lists the day files in dir, oldest first.
func days(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && dayFile.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// Read returns the events of the days from since's UTC day on, oldest first.
// Lines that are not valid envelopes, such as one a crash cut short, are
// skipped. A missing journal has no events.
func Read(dir string, since time.Time) ([]event.Event, error) {
	return read(dir, since, nil)
}

// ReadKind is Read for the events of one kind. It skips the other lines
// before decoding them, so reading a long journal stays cheap.
func ReadKind(dir string, since time.Time, kind string) ([]event.Event, error) {
	return ReadKinds(dir, since, kind)
}

// ReadKinds is ReadKind for the events of any of several kinds.
func ReadKinds(dir string, since time.Time, kinds ...string) ([]event.Event, error) {
	needles := make([][]byte, len(kinds))
	for i, k := range kinds {
		needles[i] = []byte(`"kind":"` + k + `"`)
	}
	return read(dir, since, func(line []byte) bool {
		for _, n := range needles {
			if bytes.Contains(line, n) {
				return true
			}
		}
		return false
	})
}

// read reads the day files from since's day on, decoding the lines that
// keep accepts, or every line when keep is nil.
func read(dir string, since time.Time, keep func([]byte) bool) ([]event.Event, error) {
	files, err := days(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	from := ""
	if !since.IsZero() {
		from = since.UTC().Format(dayLayout) + ".ndjson"
	}
	var out []event.Event
	for _, f := range files {
		if f < from {
			continue
		}
		evs, err := readFile(filepath.Join(dir, f), keep)
		if err != nil {
			return out, err
		}
		out = append(out, evs...)
	}
	return out, nil
}

func readFile(path string, keep func([]byte) bool) ([]event.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []event.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		if keep != nil && !keep(sc.Bytes()) {
			continue
		}
		var e event.Event
		if json.Unmarshal(sc.Bytes(), &e) == nil && event.Validate(e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}
