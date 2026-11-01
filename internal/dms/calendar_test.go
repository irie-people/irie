package dms

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
	}
}

func testCalendar() Calendar {
	return Calendar{
		First:  time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC),
		Every:  14,
		Review: time.Date(2027, 1, 10, 10, 0, 0, 0, time.UTC),
		Stamp:  time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
		ID:     "7f3a9c1e",
		Note:   "Missed it? You get an email 2 days later; the people you chose, 16 days after that.",
	}
}

func TestCalendarGolden(t *testing.T) {
	golden(t, "switch.ics", testCalendar().Bytes())
}

// unfold undoes RFC 5545 §3.1's folding.
func unfold(b []byte) []string {
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n ", ""), "\r\n"), "\r\n")
}

func TestCalendarFollowsRFC5545(t *testing.T) {
	c := testCalendar()
	c.Note = strings.Repeat("Ünïcödé, semicolons; and back\\slashes. ", 6)
	b := c.Bytes()
	if !bytes.HasSuffix(b, []byte("\r\n")) || bytes.Contains(bytes.ReplaceAll(b, []byte("\r\n"), nil), []byte("\n")) {
		t.Error("every line ends with CRLF, and no bare LF")
	}
	for _, l := range strings.Split(string(b), "\r\n") {
		if len(l) > 75 {
			t.Errorf("line of %d octets: %q", len(l), l)
		}
		if !utf8.ValidString(l) {
			t.Errorf("line splits a character: %q", l)
		}
		if backslashes(l)%2 == 1 {
			t.Errorf("line splits an escape: %q", l)
		}
	}
	lines := unfold(b)
	var desc string
	for _, l := range lines {
		if strings.HasPrefix(l, "DESCRIPTION:Tap I'm OK") {
			desc = l
		}
	}
	if !strings.Contains(desc, `Ünïcödé\, semicolons\; and back\\slashes.`) || !strings.Contains(desc, `well.\n\n`) {
		t.Errorf("escaped description: %q", desc)
	}
	for _, want := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=SU", "RRULE:FREQ=YEARLY", "TRIGGER:PT0S", "DTSTART:20261108T100000", "END:VCALENDAR"} {
		found := false
		for _, l := range lines {
			found = found || l == want
		}
		if !found {
			t.Errorf("no line %q", want)
		}
	}
}

func TestRule(t *testing.T) {
	c := testCalendar()
	for every, want := range map[int]string{
		7:  "FREQ=WEEKLY;INTERVAL=1;BYDAY=SU",
		14: "FREQ=WEEKLY;INTERVAL=2;BYDAY=SU",
		10: "FREQ=DAILY;INTERVAL=10",
	} {
		c.Every = every
		if got := c.Rule(); got != want {
			t.Errorf("every %d: %s, want %s", every, got, want)
		}
	}
}

func TestNextWeekday(t *testing.T) {
	sat := time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC)
	if got := NextWeekday(sat, time.Sunday, 10, 0); !got.Equal(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("from a Saturday: %v", got)
	}
	sun := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	if got := NextWeekday(sun, time.Sunday, 10, 0); !got.Equal(time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("from a Sunday: a week on, %v", got)
	}
}
