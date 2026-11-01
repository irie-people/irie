package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/dms"
)

const dmsUsage = `usage: irie dms <command>

The dead man's switch guide's helpers (:setup switch starts the guide). They
keep the Healthchecks API key and the checks' ping URLs in Irie's state
folder, where only you can read them, and never print either.

  irie dms key --paste    keep the Healthchecks project's API key you just
                          copied, then empty the clipboard; without --paste,
                          type or pipe it in
  irie dms checks --alarm-name NAME [--alarm-desc-file FILE]
                  [--every DAYS] [--remind DAYS] [--alarm DAYS]
                  [--reminder-name NAME] [--reminder-desc-file FILE] [--api URL]
                          make the reminder and the alarm checks, or bring them
                          up to date; 14, 2 and 16 days unless you say
  irie dms status         each check's state, who it emails, and what is left
  irie dms ping           check in from this computer, as the phone's tap does
  irie dms pause          pause both checks, so nobody is emailed; the next
                          check-in starts them again
  irie dms resume         start them again now
  irie dms calendar --out FILE [--first YYYY-MM-DD] [--at HH:MM]
                  [--review YYYY-MM-DD]
                          a calendar file: a reminder to check in, first on
                          the next Sunday at 10:00, and a review once a year
  irie dms shortcut --out FILE [--unsigned]
                          the iPhone's one-tap check-in, "I'm OK", signed for
                          your own phone with macOS's shortcuts tool
  irie dms contact --out FILE [--domain DOMAIN]
                          the check-in for any phone: an "I'm OK" contact card
                          with a link that emails both checks
`

func dmsCmd(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(dmsUsage)
		return nil
	}
	_, p, err := load()
	if err != nil {
		return err
	}
	store := dms.Store{Dir: filepath.Join(p.State, "dms")}
	sub, args := args[0], args[1:]
	switch sub {
	case "key":
		fs := flag.NewFlagSet("dms key", flag.ContinueOnError)
		paste := fs.Bool("paste", false, "read the key from the clipboard, then empty it")
		api := fs.String("api", "", "the Management API, for your own Healthchecks; "+dms.DefaultAPI+" by default")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return dmsKey(store, *paste, *api)
	case "checks":
		return dmsChecks(store, args)
	case "status":
		st, c, err := dmsState(store)
		if err != nil {
			return err
		}
		checks, err := c.Checks()
		if err != nil {
			return err
		}
		chans, err := c.Channels()
		if err != nil {
			return err
		}
		fmt.Println(st.Rhythm.Words())
		for _, l := range dms.Report(st, checks, chans, time.Now()) {
			fmt.Println(l)
		}
		return nil
	case "ping":
		st, _, err := dmsState(store)
		if err != nil {
			return err
		}
		for _, r := range []dms.Ref{st.Reminder, st.Alarm} {
			if err := dms.Ping(nil, r.PingURL); err != nil {
				return fmt.Errorf("“%s”: %v", r.Name, err)
			}
		}
		fmt.Println("Checked in: both checks start counting again from now.")
		return nil
	case "pause", "resume":
		st, c, err := dmsState(store)
		if err != nil {
			return err
		}
		act := c.Pause
		if sub == "resume" {
			act = c.Resume
		}
		for _, r := range []dms.Ref{st.Reminder, st.Alarm} {
			if err := act(r.UUID); err != nil {
				return fmt.Errorf("“%s”: %v", r.Name, err)
			}
		}
		if sub == "pause" {
			fmt.Println("Paused both checks: nobody is emailed until you check in again, which starts them.")
		} else {
			fmt.Println("Both checks are running again.")
		}
		return nil
	case "calendar":
		return dmsCalendar(store, args)
	case "shortcut":
		return dmsShortcut(store, args)
	case "contact":
		return dmsContact(store, args)
	}
	return fmt.Errorf("unknown dms command %q\n\n%s", sub, dmsUsage)
}

// dmsState is the switch's state and a client for its API.
func dmsState(store dms.Store) (dms.State, dms.Client, error) {
	st, ok, err := store.Load()
	if err != nil {
		return dms.State{}, dms.Client{}, err
	}
	if !ok {
		return dms.State{}, dms.Client{}, errors.New("no checks yet: `irie dms checks` makes them")
	}
	key, err := store.Key()
	if err != nil {
		return dms.State{}, dms.Client{}, err
	}
	return st, dms.Client{API: st.API, Key: key}, nil
}

// dmsKey keeps the key from the clipboard, a terminal or a pipe, and checks
// that it works. It prints only the key's last four characters.
func dmsKey(store dms.Store, paste bool, api string) error {
	var key string
	var err error
	switch {
	case paste:
		key, err = readClipboard()
	default:
		key, err = readSecret("Paste the Healthchecks API key (it will not show): ")
	}
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if err := store.SaveKey(key); err != nil {
		return err
	}
	if paste {
		clearClipboard()
	}
	tail := key
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	fmt.Printf("Kept the Healthchecks API key (…%s) in %s, which only you can read.", tail, store.KeyPath())
	if paste {
		fmt.Print(" The clipboard is empty again.")
	}
	fmt.Println()
	// The checks remember their API; a key given before them names it.
	st, ok, err := store.Load()
	if err != nil {
		return err
	}
	if api == "" {
		api = st.API
	}
	if api == "" {
		api = dms.DefaultAPI
	}
	if !ok {
		st.API = api
		if err := store.Save(st); err != nil {
			return err
		}
	}
	checks, err := dms.Client{API: api, Key: key}.Checks()
	if err != nil {
		return fmt.Errorf("the key is kept, but it doesn't work yet: %v", err)
	}
	for _, c := range checks {
		if c.UUID == "" {
			return errors.New("this is the project's read-only API key; copy the full one instead, and run this again")
		}
	}
	fmt.Printf("It works: the project has %d checks.\n", len(checks))
	return nil
}

func dmsChecks(store dms.Store, args []string) error {
	fs := flag.NewFlagSet("dms checks", flag.ContinueOnError)
	every := fs.Int("every", dms.Suggested.Every, "days between check-ins")
	remind := fs.Int("remind", dms.Suggested.Remind, "days late before the reminder emails you")
	alarm := fs.Int("alarm", dms.Suggested.Alarm, "days late before the alarm emails the people you chose")
	alarmName := fs.String("alarm-name", "", "the alarm's name, which the people's email is about: say what to do")
	alarmDesc := fs.String("alarm-desc-file", "", "a file with the alarm's description: the steps for whoever reads it")
	remName := fs.String("reminder-name", "", "the reminder's name")
	remDesc := fs.String("reminder-desc-file", "", "a file with the reminder's description")
	api := fs.String("api", "", "the Management API, for your own Healthchecks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r := dms.Rhythm{Every: *every, Remind: *remind, Alarm: *alarm}
	if err := r.Check(); err != nil {
		return err
	}
	reminder := dms.DefaultReminder(r)
	if *remName != "" {
		reminder.Name = *remName
	}
	if *remDesc != "" {
		b, err := os.ReadFile(*remDesc)
		if err != nil {
			return err
		}
		reminder.Desc = strings.TrimSpace(string(b))
	}
	if *alarmName == "" {
		return errors.New("--alarm-name is needed: what the people's email is about, such as “Alice has not checked in for 30 days: read this”")
	}
	a := dms.Text{Name: *alarmName}
	if *alarmDesc != "" {
		b, err := os.ReadFile(*alarmDesc)
		if err != nil {
			return err
		}
		a.Desc = strings.TrimSpace(string(b))
	}
	old, _, err := store.Load()
	if err != nil {
		return err
	}
	key, err := store.Key()
	if err != nil {
		return err
	}
	c := dms.Client{API: firstOf(*api, old.API, dms.DefaultAPI), Key: key}
	st, did, err := dms.Make(c, r, reminder, a, old)
	for _, l := range did {
		fmt.Println(l)
	}
	if err != nil {
		return err
	}
	if err := store.Save(st); err != nil {
		return err
	}
	fmt.Println(r.Words())
	fmt.Println("Their ping URLs are kept in Irie's state folder, for the phone's tap and the calendar; they are not shown here.")
	fmt.Println("Next, in Healthchecks: open Integrations, add an email integration for yourself and tick it for the reminder only; add one for each person you chose and tick those for the alarm only. Then `irie dms status`.")
	return nil
}

func dmsCalendar(store dms.Store, args []string) error {
	fs := flag.NewFlagSet("dms calendar", flag.ContinueOnError)
	out := fs.String("out", "", "the .ics file to write")
	first := fs.String("first", "", "the first reminder's date, YYYY-MM-DD; the next Sunday by default")
	at := fs.String("at", "10:00", "the reminders' time of day, HH:MM")
	review := fs.String("review", "", "the first yearly review's date, YYYY-MM-DD; about two months from now by default")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("--out is needed: the calendar file to write, such as “~/Documents/Dead Man's Switch/Dead man's switch.ics”")
	}
	st, ok, err := store.Load()
	if err != nil {
		return err
	}
	r := dms.Suggested
	if ok && st.Rhythm.Every > 0 {
		r = st.Rhythm
	}
	if st.Token == "" {
		st.Token = fmt.Sprintf("%x", time.Now().UnixNano())
	}
	clock, err := time.Parse("15:04", *at)
	if err != nil {
		return fmt.Errorf("--at %q: want HH:MM, such as 10:00", *at)
	}
	now := time.Now()
	start := dms.NextWeekday(now, time.Sunday, clock.Hour(), clock.Minute())
	if *first != "" {
		d, err := time.ParseInLocation("2006-01-02", *first, now.Location())
		if err != nil {
			return fmt.Errorf("--first %q: want YYYY-MM-DD", *first)
		}
		start = time.Date(d.Year(), d.Month(), d.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	}
	rev := dms.NextWeekday(now.AddDate(0, 2, 0), time.Sunday, clock.Hour(), clock.Minute())
	if *review != "" {
		d, err := time.ParseInLocation("2006-01-02", *review, now.Location())
		if err != nil {
			return fmt.Errorf("--review %q: want YYYY-MM-DD", *review)
		}
		rev = time.Date(d.Year(), d.Month(), d.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	}
	cal := dms.Calendar{
		First: start, Every: r.Every, Review: rev, Stamp: now, ID: st.Token,
		Note: fmt.Sprintf("Missed one? You get an email %d days later; the people you chose, %d days after that.", r.Remind, r.Alarm-r.Remind),
	}
	path := expandHome(*out)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := writePrivate(path, cal.Bytes()); err != nil {
		return err
	}
	if ok {
		if err := store.Save(st); err != nil {
			return err
		}
	}
	fmt.Printf("Wrote %s: “Check in: tap I'm OK” every %d days from %s, and a yearly review from %s.\n", path, r.Every, start.Format("Mon 2 Jan 2006 15:04"), rev.Format("2 Jan 2006"))
	fmt.Println("Open it on the phone (AirDrop or Messages it over) or on this computer, and add both events to the calendar you look at.")
	return nil
}

func firstOf(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
		}
	}
	return p
}

// readClipboard reads the clipboard: pbpaste on macOS, wl-paste or xclip on
// Linux.
func readClipboard() (string, error) {
	var tries [][]string
	if runtime.GOOS == "darwin" {
		tries = [][]string{{"pbpaste"}}
	} else {
		tries = [][]string{{"wl-paste", "--no-newline"}, {"xclip", "-o", "-selection", "clipboard"}, {"xsel", "--clipboard", "--output"}}
	}
	for _, t := range tries {
		if _, err := exec.LookPath(t[0]); err != nil {
			continue
		}
		out, err := exec.Command(t[0], t[1:]...).Output()
		if err != nil {
			return "", fmt.Errorf("could not read the clipboard with %s: %v", t[0], err)
		}
		return string(out), nil
	}
	return "", errors.New("no clipboard tool here (pbpaste, wl-paste, xclip or xsel); run `irie dms key` in a terminal and paste the key there")
}

// clearClipboard empties the clipboard, so the key doesn't linger in it.
func clearClipboard() {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "darwin":
		cmd = exec.Command("pbcopy")
	case haveTool("wl-copy"):
		cmd = exec.Command("wl-copy", "--clear")
	case haveTool("xclip"):
		cmd = exec.Command("xclip", "-selection", "clipboard")
	case haveTool("xsel"):
		cmd = exec.Command("xsel", "--clipboard", "--clear")
	default:
		return
	}
	cmd.Stdin = strings.NewReader("")
	cmd.Run()
}

func haveTool(name string) bool { _, err := exec.LookPath(name); return err == nil }

// readSecret reads one line: from a terminal without echo, else from
// standard input as it comes.
func readSecret(prompt string) (string, error) {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	tty := fi.Mode()&os.ModeCharDevice != 0
	if tty {
		fmt.Fprint(os.Stderr, prompt)
		stty := func(arg string) error {
			cmd := exec.Command("stty", arg)
			cmd.Stdin = os.Stdin
			return cmd.Run()
		}
		if err := stty("-echo"); err != nil {
			return "", fmt.Errorf("could not hide what you type: %v", err)
		}
		defer func() {
			stty("echo")
			fmt.Fprintln(os.Stderr)
		}()
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		if errors.Is(err, io.EOF) {
			return "", errors.New("no key given: copy it and run `irie dms key --paste`, or type it into a terminal")
		}
		return "", err
	}
	return line, nil
}

// dmsShortcut writes the iPhone's shortcut. It holds the ping URLs, so it is
// written 0600, and the user deletes it once the phone has it.
func dmsShortcut(store dms.Store, args []string) error {
	fs := flag.NewFlagSet("dms shortcut", flag.ContinueOnError)
	out := fs.String("out", "", "the .shortcut file to write")
	unsigned := fs.Bool("unsigned", false, "leave it unsigned; an iPhone imports only a signed one")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("--out is needed: the shortcut to write, such as “~/Documents/Dead Man's Switch/I'm OK.shortcut”")
	}
	st, ok, err := store.Load()
	if err != nil {
		return err
	}
	if !ok || st.Reminder.PingURL == "" || st.Alarm.PingURL == "" {
		return errors.New("no checks yet: `irie dms checks` makes them")
	}
	b, err := dms.Shortcut([]string{st.Reminder.PingURL, st.Alarm.PingURL})
	if err != nil {
		return err
	}
	path := expandHome(*out)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	_, lookErr := exec.LookPath("shortcuts")
	if *unsigned || lookErr != nil {
		if err := writePrivate(path, b); err != nil {
			return err
		}
		fmt.Printf("Wrote %s, unsigned. An iPhone imports only a signed shortcut: sign it on a Mac with `shortcuts sign --input FILE --output FILE`.\n", path)
		return nil
	}
	// shortcuts sign reads a file by its extension, so the unsigned one is
	// a .shortcut too, in a folder of its own.
	tmp, err := os.MkdirTemp(filepath.Dir(path), ".irie-shortcut-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	plain := filepath.Join(tmp, "unsigned.shortcut")
	if err := os.WriteFile(plain, b, 0o600); err != nil {
		return err
	}
	cmd := exec.Command("shortcuts", "sign", "--mode", "people-who-know-me", "--input", plain, "--output", path)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("shortcuts sign failed: %v: %s", err, strings.TrimSpace(string(msg)))
	}
	os.Chmod(path, 0o600)
	fmt.Printf("Wrote %s, signed for people who have you in their contacts, which your own iPhone does.\n", path)
	fmt.Println("Send it to the phone with AirDrop or Messages, open it, and tap Add Shortcut; Settings › Shortcuts › Private Sharing must be on. Then add it to the Home Screen, tap it once, and delete the file: it holds the checks' ping URLs.")
	return nil
}

// dmsContact writes the check-in for any phone.
func dmsContact(store dms.Store, args []string) error {
	fs := flag.NewFlagSet("dms contact", flag.ContinueOnError)
	out := fs.String("out", "", "the .vcf file to write")
	domain := fs.String("domain", "", "the checks' email domain, for your own Healthchecks; the ping URLs' by default")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("--out is needed: the contact card to write, such as “~/Documents/Dead Man's Switch/I'm OK.vcf”")
	}
	st, ok, err := store.Load()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no checks yet: `irie dms checks` makes them")
	}
	var addrs []string
	for _, r := range []dms.Ref{st.Reminder, st.Alarm} {
		a, err := dms.CheckAddress(r, *domain)
		if err != nil {
			return err
		}
		addrs = append(addrs, a)
	}
	b, err := dms.Contact(addrs)
	if err != nil {
		return err
	}
	path := expandHome(*out)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := writePrivate(path, b); err != nil {
		return err
	}
	fmt.Printf("Wrote %s: a contact called “I'm OK”, with both checks' email addresses and a link that writes to both.\n", path)
	fmt.Println("Open it on the phone and add the contact. To check in, open the contact's link and send the email. Then `irie dms status` should show both checks up; if only one is, write to both addresses instead.")
	return nil
}

// writePrivate writes a file of the user's own, readable by them alone,
// through a temporary file renamed into place, with no lock file beside it.
func writePrivate(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".irie-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
