// Package dms holds the dead man's switch guide's helpers
// (docs/proposals/extensions.md §2): the check-in's two checks at
// Healthchecks.io, the phone's one-tap check-in and the calendar file. The
// guide's agent runs them as `irie dms …`; they keep the Healthchecks key
// from the agent and get the exact parts exactly right.
package dms

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Calendar is the switch's iCalendar file (RFC 5545): a reminder to check in
// every so many days, and a review once a year. Times are floating, the same
// wall clock wherever the calendar is (RFC 5545 §3.3.5, form #1), so a
// reminder at ten stays at ten after a move or a change of clock.
type Calendar struct {
	First  time.Time // the first check-in reminder; only its date and clock time count
	Every  int       // days between check-ins
	Review time.Time // the first yearly review
	Stamp  time.Time // when the file was made, for DTSTAMP
	ID     string    // a token of the switch's own, so a file made again names the same events
	// What the reminder says under its title, such as how late a missed
	// check-in becomes a reminder and an alarm.
	Note string
}

// Rule is the recurrence of a check-in: weekly when Every is whole weeks,
// on the first reminder's weekday, else every so many days (RFC 5545
// §3.3.10).
func (c Calendar) Rule() string {
	if c.Every%7 == 0 {
		return fmt.Sprintf("FREQ=WEEKLY;INTERVAL=%d;BYDAY=%s", c.Every/7, weekday(c.First.Weekday()))
	}
	return fmt.Sprintf("FREQ=DAILY;INTERVAL=%d", c.Every)
}

func weekday(d time.Weekday) string { return [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}[d] }

// Bytes is the file: two events, each with an alarm when it starts.
func (c Calendar) Bytes() []byte {
	var b bytes.Buffer
	line := func(s string) { fold(&b, s) }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Irie//Dead man's switch//EN")
	line("CALSCALE:GREGORIAN")
	checkin := "Check in: tap I'm OK"
	note := "Tap I'm OK on your phone, so your dead man's switch knows you are well."
	if c.Note != "" {
		note += "\n\n" + c.Note
	}
	event(line, "checkin-"+c.ID+"@irie", c.Stamp, c.First, 15*time.Minute, c.Rule(), checkin, note)
	review := "Review your dead man's switch"
	reviewNote := strings.Join([]string{
		"Once a year, check that your switch still fits your life:",
		"- The people: still the right ones, with the right email addresses and the right access?",
		"- The letter: READ ME FIRST still true, and every account in it still there?",
		"- The emergency access in your password manager: every contact still confirmed?",
		"- The check-in: tap I'm OK, and see it arrive.",
		"- Google and Apple: the Inactive Account Manager and the Legacy Contact still set?",
		"Your switch.md says where each part is.",
	}, "\n")
	event(line, "review-"+c.ID+"@irie", c.Stamp, c.Review, 30*time.Minute, "FREQ=YEARLY", review, reviewNote)
	line("END:VCALENDAR")
	return b.Bytes()
}

func event(line func(string), uid string, stamp, start time.Time, length time.Duration, rule, summary, description string) {
	line("BEGIN:VEVENT")
	line("UID:" + uid)
	line("DTSTAMP:" + stamp.UTC().Format("20060102T150405Z"))
	line("DTSTART:" + start.Format("20060102T150405"))
	line(fmt.Sprintf("DURATION:PT%dM", int(length/time.Minute)))
	line("RRULE:" + rule)
	line("SUMMARY:" + text(summary))
	line("DESCRIPTION:" + text(description))
	line("BEGIN:VALARM")
	line("ACTION:DISPLAY")
	line("DESCRIPTION:" + text(summary))
	line("TRIGGER:PT0S")
	line("END:VALARM")
	line("END:VEVENT")
}

// text escapes a TEXT value (RFC 5545 §3.3.11).
func text(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`)
	return r.Replace(s)
}

// fold writes one content line, folded so that no line passes 75 octets,
// never inside a UTF-8 character, and ended with CRLF (RFC 5545 §3.1).
func fold(b *bytes.Buffer, s string) {
	limit := 75
	for len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		// Nor inside an escape such as \n, which some readers unfold badly.
		if n := backslashes(s[:cut]); n%2 == 1 {
			cut--
		}
		b.WriteString(s[:cut])
		b.WriteString("\r\n ")
		s = s[cut:]
		limit = 74 // the space that starts a folded line counts
	}
	b.WriteString(s)
	b.WriteString("\r\n")
}

// backslashes counts the backslashes that end s.
func backslashes(s string) int {
	n := 0
	for n < len(s) && s[len(s)-1-n] == '\\' {
		n++
	}
	return n
}

// NextWeekday is the first day after now that falls on d, at the clock time
// hh:mm, in now's location.
func NextWeekday(now time.Time, d time.Weekday, hh, mm int) time.Time {
	days := (int(d) - int(now.Weekday()) + 7) % 7
	if days == 0 {
		days = 7
	}
	t := now.AddDate(0, 0, days)
	return time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, now.Location())
}
