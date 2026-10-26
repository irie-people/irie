package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
)

const session = "s0k3j9x2m4a7q"

func ev(kind string) event.Event {
	return event.Event{Session: session, Source: event.SourceIrie, Harness: "claude-code", Kind: kind}
}

// clock is a settable time source.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func open(t *testing.T, dir string, c *clock, keep int) *Journal {
	t.Helper()
	j, err := Open(dir, Options{KeepDays: keep, Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

func appendKinds(t *testing.T, j *Journal, kinds ...string) []event.Event {
	t.Helper()
	var out []event.Event
	for _, k := range kinds {
		e, err := j.Append(ev(k))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func lines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func TestAppendStampsAndWritesPrivateDailyFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "journal")
	c := &clock{time.Date(2026, 10, 26, 14, 2, 11, 408_000_000, time.UTC)}
	j := open(t, dir, c, 30)
	got := appendKinds(t, j, event.SpawnRequested, event.SessionStarted)
	if got[0].Seq != 1 || got[1].Seq != 2 {
		t.Errorf("seqs %d %d, want 1 2", got[0].Seq, got[1].Seq)
	}
	if got[0].TS != "2026-10-26T14:02:11.408Z" || got[0].V != 1 || got[0].Data == nil {
		t.Errorf("stamped %+v", got[0])
	}

	file := filepath.Join(dir, "2026-10-26.ndjson")
	for path, want := range map[string]os.FileMode{dir: 0o700, file: 0o600} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s mode %v, want %v", path, fi.Mode().Perm(), want)
		}
	}
	for _, l := range lines(t, file) {
		dec := json.NewDecoder(strings.NewReader(l))
		dec.DisallowUnknownFields()
		var e event.Event
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("%q: %v", l, err)
		}
		if err := event.Validate(e); err != nil {
			t.Errorf("%q: %v", l, err)
		}
	}

	// A reopened journal carries on counting.
	j.Close()
	j = open(t, dir, c, 30)
	if e := appendKinds(t, j, event.PromptSubmitted); e[0].Seq != 3 {
		t.Errorf("seq after reopening = %d, want 3", e[0].Seq)
	}
}

func TestAppendCapsDataStrings(t *testing.T) {
	c := &clock{time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)}
	j := open(t, t.TempDir(), c, 30)
	e := ev(event.ToolStarted)
	e.Data = map[string]any{"tool": "Bash", "summary": strings.Repeat("x", 10*event.MaxString)}
	got, err := j.Append(e)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.Str("summary")); n > event.MaxString {
		t.Errorf("summary is %d bytes", n)
	}
}

func TestAppendRefusesAnInvalidEvent(t *testing.T) {
	c := &clock{time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	j := open(t, dir, c, 30)
	bad := ev(event.ToolStarted)
	bad.Session = "not-a-session"
	if _, err := j.Append(bad); err == nil {
		t.Error("an invalid event was written")
	}
	if e := appendKinds(t, j, event.ToolStarted); e[0].Seq != 1 {
		t.Errorf("a refused event used up a seq: next is %d", e[0].Seq)
	}
}

func TestATornLastLineIsSkipped(t *testing.T) {
	dir := t.TempDir()
	good := `{"v":1,"seq":7,"ts":"2026-10-26T13:00:00.000Z","session":"` + session + `","agent":"","source":"irie","harness":"claude-code","kind":"spawn.requested","data":{}}`
	if err := os.WriteFile(filepath.Join(dir, "2026-10-26.ndjson"), []byte(good+"\n"+`{"v":1,"seq":8,"ts":"2026`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &clock{time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)}
	j := open(t, dir, c, 30)
	if e := appendKinds(t, j, event.SessionStarted); e[0].Seq != 8 {
		t.Errorf("seq = %d, want 8", e[0].Seq)
	}
	evs, err := Read(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var seqs []uint64
	for _, e := range evs {
		seqs = append(seqs, e.Seq)
	}
	if !slices.Equal(seqs, []uint64{7, 8}) {
		t.Errorf("read seqs %v, want [7 8]", seqs)
	}
}

func TestRotatesAtUTCMidnight(t *testing.T) {
	dir := t.TempDir()
	c := &clock{time.Date(2026, 10, 26, 23, 59, 59, 999_000_000, time.UTC)}
	j := open(t, dir, c, 30)
	appendKinds(t, j, event.ToolStarted)
	c.t = c.t.Add(2 * time.Millisecond)
	appendKinds(t, j, event.ToolFinished)
	for _, f := range []string{"2026-10-26.ndjson", "2026-10-27.ndjson"} {
		if n := len(lines(t, filepath.Join(dir, f))); n != 1 {
			t.Errorf("%s has %d lines, want 1", f, n)
		}
	}
	evs, err := Read(dir, time.Date(2026, 10, 27, 5, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != event.ToolFinished || evs[0].Seq != 2 {
		t.Errorf("read from the 27th: %+v", evs)
	}
}

func TestKeepsOnlyKeepDays(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"2026-09-26.ndjson", "2026-09-27.ndjson", "2026-10-25.ndjson", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(f string) bool {
		_, err := os.Stat(filepath.Join(dir, f))
		return err == nil
	}
	c := &clock{time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	j := open(t, dir, c, 30)
	if exists("2026-09-26.ndjson") {
		t.Error("a file 30 days old was kept")
	}
	if !exists("2026-09-27.ndjson") || !exists("2026-10-25.ndjson") || !exists("notes.txt") {
		t.Error("a recent day or a file that is not a day was removed")
	}
	// Crossing midnight moves the window on.
	c.t = c.t.Add(24 * time.Hour)
	appendKinds(t, j, event.ToolStarted)
	if exists("2026-09-27.ndjson") || !exists("2026-10-27.ndjson") {
		t.Error("the window did not move at midnight")
	}
}

func TestReadSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	c := &clock{time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)}
	j := open(t, dir, c, 30)
	appendKinds(t, j, event.SpawnRequested)
	f, err := os.OpenFile(filepath.Join(dir, "2026-10-26.ndjson"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriter(f)
	w.WriteString("not json\n{\"v\":1}\n")
	w.Flush()
	f.Close()
	appendKinds(t, j, event.SessionStarted)
	evs, err := Read(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Kind != event.SpawnRequested || evs[1].Kind != event.SessionStarted {
		t.Errorf("read %+v", evs)
	}
	if evs, err := Read(filepath.Join(dir, "missing"), time.Time{}); err != nil || len(evs) != 0 {
		t.Errorf("a missing journal: %v, %v", evs, err)
	}
}
