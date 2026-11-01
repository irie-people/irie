package dms

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/filetx"
)

const day = 24 * 60 * 60 // seconds

// Rhythm is how the switch counts, in days: how often the user checks in,
// and how late each check goes down. A reminder emails the user; the alarm,
// later, emails the people they chose.
type Rhythm struct {
	Every  int `json:"every"`  // days between check-ins: both checks' period
	Remind int `json:"remind"` // days late before the reminder goes to the user
	Alarm  int `json:"alarm"`  // days late before the alarm goes to the people
}

// Suggested is the rhythm the guide offers: check in every 14 days; two
// days late, a reminder; sixteen days after that, the alarm.
var Suggested = Rhythm{Every: 14, Remind: 2, Alarm: 16}

// Check says what is wrong with r, in words for the user.
func (r Rhythm) Check() error {
	var errs []error
	for _, f := range []struct {
		name string
		days int
	}{{"--every", r.Every}, {"--remind", r.Remind}, {"--alarm", r.Alarm}} {
		if f.days < 1 || f.days*day > MaxSeconds {
			errs = append(errs, fmt.Errorf("%s %d: must be from 1 to 365 days", f.name, f.days))
		}
	}
	if r.Alarm <= r.Remind {
		errs = append(errs, fmt.Errorf("--alarm %d must be more days than --remind %d, so the reminder comes first", r.Alarm, r.Remind))
	}
	return errors.Join(errs...)
}

// Words is the rhythm in one sentence.
func (r Rhythm) Words() string {
	return fmt.Sprintf("Check in every %d days. A missed check-in reminds you %s later; %s after that, the people you chose are emailed (%d days after your last check-in).",
		r.Every, days(r.Remind), days(r.Alarm-r.Remind), r.Every+r.Alarm)
}

func days(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

// Text is a check's name and description. The alarm's name is what the
// people's email is about, so it says what to do; its description holds
// the steps.
type Text struct {
	Name string
	Desc string
}

func (t Text) check(what string) error {
	switch {
	case strings.TrimSpace(t.Name) == "":
		return fmt.Errorf("the %s has no name", what)
	case utf8.RuneCountInString(t.Name) > MaxName:
		return fmt.Errorf("the %s's name is %d characters; Healthchecks takes at most %d", what, utf8.RuneCountInString(t.Name), MaxName)
	case strings.ContainsFunc(t.Name, unicode.IsControl):
		return fmt.Errorf("the %s's name must be one line", what)
	case utf8.RuneCountInString(t.Desc) > MaxDesc:
		return fmt.Errorf("the %s's description is %d characters; Healthchecks takes at most %d", what, utf8.RuneCountInString(t.Desc), MaxDesc)
	}
	return nil
}

// DefaultReminder is the reminder check, which only the user hears from.
func DefaultReminder(r Rhythm) Text {
	return Text{
		Name: "Dead man's switch: tap I'm OK",
		Desc: fmt.Sprintf("You missed a check-in for your dead man's switch. Tap I'm OK on your phone now. If you don't, the people you chose will be emailed in %s.", days(r.Alarm-r.Remind)),
	}
}

// Ref is one check, as the helpers remember it. Its ping URL is a key of the
// switch: whoever has it can check in for the user, or set the alarm off at
// once by adding /fail, so it lives only here, in the phone's shortcut and
// in the password manager.
type Ref struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	PingURL string `json:"ping_url"`
}

// State is what the helpers keep between runs.
type State struct {
	API      string    `json:"api"`
	Rhythm   Rhythm    `json:"rhythm"`
	Reminder Ref       `json:"reminder"`
	Alarm    Ref       `json:"alarm"`
	Token    string    `json:"token"` // names the calendar's events, so a file made again has the same ones
	Made     time.Time `json:"made"`
}

// Store is the switch's folder in Irie's state, $XDG_STATE_HOME/irie/dms,
// which only the user can read: the Healthchecks key and the checks.
type Store struct{ Dir string }

const (
	keyFile   = "healthchecks.key"
	stateFile = "switch.json"
)

// KeyPath is where the key is kept, for messages.
func (s Store) KeyPath() string { return filepath.Join(s.Dir, keyFile) }

// SaveKey keeps a project's API key, 0600.
func (s Store) SaveKey(key string) error {
	key = strings.TrimSpace(key)
	switch {
	case key == "":
		return errors.New("no key: copy the API key from the project's settings page first")
	case len(key) > 200 || strings.ContainsFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }):
		return errors.New("that is not an API key: it is more than one word. Copy just the key")
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return filetx.WriteFile(s.KeyPath(), []byte(key+"\n"), 0o600)
}

// Key reads the kept key.
func (s Store) Key() (string, error) {
	b, err := os.ReadFile(s.KeyPath())
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("no Healthchecks API key yet: copy the project's API key, then run `irie dms key --paste`")
	}
	return strings.TrimSpace(string(b)), err
}

// Load reads the switch's state; none yet is no error.
func (s Store) Load() (State, bool, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, false, fmt.Errorf("%s: %v", filepath.Join(s.Dir, stateFile), err)
	}
	return st, true, nil
}

// Save keeps the switch's state, 0600.
func (s Store) Save(st State) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return filetx.WriteFile(filepath.Join(s.Dir, stateFile), append(b, '\n'), 0o600)
}

// Make creates the two checks, or brings them up to date when they are there
// (by name), and returns the state to keep. It assigns no integrations.
func Make(c Client, r Rhythm, reminder, alarm Text, old State) (State, []string, error) {
	if err := r.Check(); err != nil {
		return State{}, nil, err
	}
	if err := errors.Join(reminder.check("reminder"), alarm.check("alarm")); err != nil {
		return State{}, nil, err
	}
	if reminder.Name == alarm.Name {
		return State{}, nil, errors.New("the reminder and the alarm need names of their own")
	}
	st := old
	st.API, st.Rhythm, st.Made = c.API, r, time.Now().UTC().Truncate(time.Second)
	if st.Token == "" {
		st.Token = token()
	}
	var did []string
	for _, x := range []struct {
		text  Text
		grace int
		ref   *Ref
		what  string
	}{{reminder, r.Remind, &st.Reminder, "reminder"}, {alarm, r.Alarm, &st.Alarm, "alarm"}} {
		ch, created, err := c.Upsert(NewCheck{Name: x.text.Name, Desc: x.text.Desc, Timeout: r.Every * day, Grace: x.grace * day})
		if err != nil {
			return State{}, did, err
		}
		if ch.UUID == "" || ch.PingURL == "" {
			return State{}, did, errors.New("Healthchecks sent no ping URL: this is the read-only API key; the helpers need the project's full one")
		}
		*x.ref = Ref{Name: ch.Name, UUID: ch.UUID, PingURL: ch.PingURL}
		verb := "Updated"
		if created {
			verb = "Made"
		}
		did = append(did, fmt.Sprintf("%s the %s, “%s”: period %s, grace %s.", verb, x.what, ch.Name, days(r.Every), days(x.grace)))
	}
	return st, did, nil
}

func token() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Report is what `irie dms status` says: each check's state and the
// integrations it notifies, then what is left to do.
func Report(st State, checks []Check, channels []Channel, now time.Time) []string {
	name := map[string]Channel{}
	for _, ch := range channels {
		name[ch.ID] = ch
	}
	var out, todo []string
	for _, x := range []struct {
		ref  Ref
		what string
	}{{st.Reminder, "reminder"}, {st.Alarm, "alarm"}} {
		i := slices.IndexFunc(checks, func(c Check) bool { return c.UUID == x.ref.UUID })
		if i < 0 {
			out = append(out, fmt.Sprintf("The %s, “%s”, is gone from Healthchecks.", x.what, x.ref.Name))
			todo = append(todo, "Make the checks again: `irie dms checks`.")
			continue
		}
		c := checks[i]
		var tell []string
		for _, id := range strings.Split(c.Channels, ",") {
			if ch, ok := name[strings.TrimSpace(id)]; ok {
				tell = append(tell, fmt.Sprintf("%s (%s)", cmpOr(ch.Name, "unnamed"), ch.Kind))
			}
		}
		line := fmt.Sprintf("The %s, “%s”: %s", x.what, c.Name, state(c, now))
		if len(tell) > 0 {
			line += " It notifies " + strings.Join(tell, ", ") + "."
			if x.what == "reminder" && len(tell) > 1 {
				todo = append(todo, fmt.Sprintf("“%s” emails %d addresses, and should email only you: in Healthchecks, open it and untick everyone but you. An integration added in the web page is attached to every check.", c.Name, len(tell)))
			}
		} else {
			line += " It notifies nobody yet."
			who := "your own email address"
			if x.what == "alarm" {
				who = "an email integration for each person you chose"
			}
			todo = append(todo, fmt.Sprintf("In Healthchecks, open Integrations, add %s, and tick it for “%s”.", who, c.Name))
		}
		out = append(out, line)
		switch c.Status {
		case "new":
			todo = append(todo, fmt.Sprintf("Tap I'm OK once: “%s” waits for its first check-in, and until then it never goes down.", c.Name))
		case "paused":
			todo = append(todo, fmt.Sprintf("“%s” is paused: the next check-in starts it again.", c.Name))
		}
	}
	if len(todo) == 0 {
		return append(out, "Nothing left to do: both checks are running and notify someone.")
	}
	out = append(out, "Left to do:")
	for _, t := range dedupe(todo) {
		out = append(out, "- "+t)
	}
	return out
}

func state(c Check, now time.Time) string {
	switch c.Status {
	case "new":
		return "not started, it waits for its first check-in."
	case "paused":
		return "paused."
	case "down":
		return "DOWN: it has sent its email."
	case "grace":
		return "late: it emails at the end of its grace time."
	}
	if c.NextPing != nil {
		return fmt.Sprintf("up; the next check-in is due by %s.", c.NextPing.In(now.Location()).Format("Mon 2 Jan 15:04"))
	}
	return "up."
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func dedupe(xs []string) []string {
	var out []string
	for _, x := range xs {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}
