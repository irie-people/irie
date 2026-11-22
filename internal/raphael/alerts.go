package raphael

// macOS's own alerts, and the access it asks for in the app's name (DESIGN
// §4.11). macOS shows two kinds of its own notices as alerts, which stay
// on screen until they are closed, and hides both from System Settings ›
// Notifications (UNHideSettings in each one's bundle): Login Items'
// "Background Items Added", which comes each time a program is set to
// start by itself, and Privacy & Security's, such as an app kept from
// changing other apps. Their style is two bits of each one's flags in
// com.apple.ncprefs, as NCutil reads them: banners (1<<3), which go by
// themselves, and alerts (1<<4).
//
// Every program an agent runs runs in the app's name: macOS holds the app
// that started a process "responsible" for it, so a tool that opens a
// folder macOS guards asks the person in the app's name. macOS keeps each
// answer for the code's signature: an app signed ad hoc is a new app at
// each build, so it asks again. The look reads what macOS wrote in its log
// lately, which keeps these lines for an hour or two, so it sees what asked
// just now, not all day.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
)

// Notifier is one of macOS's own senders of notices, whose style a person
// may want to set and System Settings does not let them.
type Notifier struct {
	ID   string // its bundle id in com.apple.ncprefs
	Name string // as System Settings would name it
	What string // what it says, in a few words
}

// Notifiers are those Raffi knows, in the order he reports them.
var Notifiers = []Notifier{
	{ID: "com.apple.BTMNotificationAgent", Name: "Login Items", What: "“Background Items Added”, each time a program is set to start by itself"},
	{ID: "com.apple.tccd", Name: "Privacy & Security", What: "an app kept from changing other apps, and other privacy notices"},
}

// The styles macOS shows a sender's notices in.
const (
	StyleNone    = "none"    // not at all
	StyleBanners = "banners" // a while, then into Notification Center
	StyleAlerts  = "alerts"  // until they are closed
)

// The bits of a sender's flags in com.apple.ncprefs that are its style.
const (
	bannerBit int64 = 1 << 3
	alertBit  int64 = 1 << 4
)

// StyleOf is the style a sender's flags say.
func StyleOf(flags int64) string {
	switch {
	case flags&alertBit != 0:
		return StyleAlerts
	case flags&bannerBit != 0:
		return StyleBanners
	}
	return StyleNone
}

// WithStyle is flags with its style changed to style, its other bits
// left as they are.
func WithStyle(flags int64, style string) int64 {
	flags &^= bannerBit | alertBit
	switch style {
	case StyleBanners:
		flags |= bannerBit
	case StyleAlerts:
		flags |= alertBit
	}
	return flags
}

// ncEntry is a sender's entry in com.apple.ncprefs: where it is in its
// apps array, and its flags.
type ncEntry struct {
	index int
	flags int64
}

// readNC reads macOS's notification settings as `defaults export` writes
// them, XML, with each sender's entry by its bundle id.
func readNC(ctx context.Context, r Runner) ([]byte, map[string]ncEntry, error) {
	out, errb, err := r.Run(ctx, "/usr/bin/defaults", "export", "com.apple.ncprefs", "-")
	if err != nil {
		return nil, nil, fmt.Errorf("defaults export com.apple.ncprefs: %v: %s", err, strings.TrimSpace(string(errb)))
	}
	v, err := parsePlist(out)
	if err != nil {
		return nil, nil, fmt.Errorf("com.apple.ncprefs: %w", err)
	}
	m, _ := v.(map[string]any)
	apps, _ := m["apps"].([]any)
	entries := map[string]ncEntry{}
	for i, a := range apps {
		app, _ := a.(map[string]any)
		id := plistString(app, "bundle-id")
		if f, ok := app["flags"].(int64); ok && id != "" {
			entries[id] = ncEntry{index: i, flags: f}
		}
	}
	return out, entries, nil
}

// notifierName is how a person reads the sender id.
func notifierName(id string) string {
	for _, n := range Notifiers {
		if n.ID == id {
			return n.Name
		}
	}
	return id
}

// ncBackups is how many copies of the settings as they were SetStyle keeps.
const ncBackups = 5

// SetStyle has macOS show the sender id's notices in style: it changes
// that sender's entry in com.apple.ncprefs alone, with PlistBuddy, then
// has the two processes that show notices read it again (usernoted and
// NotificationCenter), which closes any on screen; they stay in
// Notification Center. A copy of the settings as they were goes in dir
// first, the last few kept. It reports whether it changed anything, and
// fails when macOS kept its own style.
func SetStyle(ctx context.Context, r Runner, id, style, dir string) (bool, error) {
	if style != StyleBanners && style != StyleAlerts && style != StyleNone {
		return false, fmt.Errorf("no style %q: banners, alerts or none", style)
	}
	data, entries, err := readNC(ctx, r)
	if err != nil {
		return false, err
	}
	e, ok := entries[id]
	if !ok {
		return false, fmt.Errorf("macOS’s notification settings have no entry for %s yet: it makes one with its first notice", notifierName(id))
	}
	if StyleOf(e.flags) == style {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	stamp := time.Now().UTC().Format("20060102T150405.000Z")
	backup := filepath.Join(dir, "ncprefs-"+stamp+".plist")
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return false, err
	}
	edit := filepath.Join(dir, "edit-"+stamp+".plist")
	if err := os.WriteFile(edit, data, 0o600); err != nil {
		return false, err
	}
	defer os.Remove(edit)
	want := WithStyle(e.flags, style)
	if _, errb, err := r.Run(ctx, "/usr/libexec/PlistBuddy", "-c", fmt.Sprintf("Set :apps:%d:flags %d", e.index, want), edit); err != nil {
		return false, fmt.Errorf("PlistBuddy: %v: %s", err, strings.TrimSpace(string(errb)))
	}
	if err := checkEdit(edit, id, want, len(entries)); err != nil {
		return false, err
	}
	if _, errb, err := r.Run(ctx, "/usr/bin/defaults", "import", "com.apple.ncprefs", edit); err != nil {
		return false, fmt.Errorf("defaults import com.apple.ncprefs: %v: %s", err, strings.TrimSpace(string(errb)))
	}
	// Each reads the settings as it starts; launchd starts both again.
	r.Run(ctx, "/usr/bin/killall", "usernoted")
	r.Run(ctx, "/usr/bin/killall", "NotificationCenter")
	pruneNCBackups(dir)
	_, after, err := readNC(ctx, r)
	if err != nil {
		return true, err
	}
	if got := StyleOf(after[id].flags); got != style {
		return true, fmt.Errorf("macOS kept %s’s notices as %s", notifierName(id), got)
	}
	return true, nil
}

// checkEdit makes sure PlistBuddy changed what it should and kept the rest:
// the same senders, and id's flags as wanted.
func checkEdit(path, id string, want int64, senders int) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	v, err := parsePlist(b)
	if err != nil {
		return fmt.Errorf("the edited settings don't read: %w", err)
	}
	m, _ := v.(map[string]any)
	apps, _ := m["apps"].([]any)
	n, got := 0, int64(-1)
	for _, a := range apps {
		app, _ := a.(map[string]any)
		if f, ok := app["flags"].(int64); ok && plistString(app, "bundle-id") != "" {
			n++
			if plistString(app, "bundle-id") == id {
				got = f
			}
		}
	}
	if n != senders || got != want {
		return errors.New("the edited settings are not as they should be, so Raffi left macOS's as they were")
	}
	return nil
}

// pruneNCBackups keeps the newest ncBackups copies in dir.
func pruneNCBackups(dir string) {
	ents, _ := filepath.Glob(filepath.Join(dir, "ncprefs-*.plist"))
	slices.Sort(ents) // their names start with the time
	for len(ents) > ncBackups {
		os.Remove(ents[0])
		ents = ents[1:]
	}
}

// alertsInput is what the look at macOS's alerts needs.
type alertsInput struct {
	Home string
	// Want is the style the person chose for each sender (alerts.*), by
	// its id; a sender with none chosen is missing.
	Want map[string]string
	// Signed are the app's programs to weigh the signatures of: the app
	// wherever it is installed, and the command.
	Signed []string
	Now    time.Time
	// Log stands in for macOS's log in tests: what `log show` printed.
	Log *string
}

// logWindow is how far back the look reads macOS's log: tccd's lines
// last about that long there.
const logWindow = 2 * time.Hour

// logPredicate picks the lines the look reads from macOS's log: each
// "Background Items Added" alert, and tccd's records of the access asked
// for.
const logPredicate = `(subsystem == "com.apple.backgroundtaskmanagement" AND eventMessage BEGINSWITH "Posting advisory") OR (subsystem == "com.apple.TCC" AND eventMessage BEGINSWITH "AUTHREQ_")`

// alertFindings weighs macOS's own alerts: the senders whose notices stay
// until closed, the background items and access it asked about lately in
// the log, the crashes of System Settings' Login Items, and whether the
// app is signed so that macOS keeps what the person allowed.
func alertFindings(ctx context.Context, r Runner, in alertsInput) (fs []Finding, fine []string) {
	if runtime.GOOS != "darwin" && in.Log == nil {
		return nil, nil
	}
	sfs, sfine := styleFindings(ctx, r, in)
	fs, fine = append(fs, sfs...), append(fine, sfine...)
	adhoc, sigs := signingFinding(ctx, r, in)
	if adhoc.ID != "" {
		fs = append(fs, adhoc)
	}
	fine = append(fine, sigs...)
	// What macOS said lately, when its log reads: a log that does not
	// says nothing of what it asked.
	text, read := "", false
	if in.Log != nil {
		text, read = *in.Log, true
	} else if out, _, err := r.Run(ctx, "/usr/bin/log", "show", "--last", fmt.Sprintf("%dm", int(logWindow/time.Minute)), "--style", "compact", "--predicate", logPredicate); err == nil {
		text, read = string(out), true
	}
	if read {
		if f, line := backgroundFinding(Advisories(text)); f.ID != "" {
			fs = append(fs, f)
		} else if line != "" {
			fine = append(fine, line)
		}
		if f, line := asksFinding(Asks(text, brand.BundleID), adhoc.ID != ""); f.ID != "" {
			fs = append(fs, f)
		} else if line != "" {
			fine = append(fine, line)
		}
	}
	if f := loginItemsCrashes(in.Home, in.Now); f.ID != "" {
		fs = append(fs, f)
	}
	return fs, fine
}

// styleFindings says which of macOS's senders show their notices until
// they are closed, with a fix that has them go by themselves; one whose
// style the person chose is well as it is.
func styleFindings(ctx context.Context, r Runner, in alertsInput) (fs []Finding, fine []string) {
	_, entries, err := readNC(ctx, r)
	if err != nil {
		return nil, nil
	}
	for _, n := range Notifiers {
		e, ok := entries[n.ID]
		if !ok {
			continue
		}
		style := StyleOf(e.flags)
		if want := in.Want[n.ID]; style != StyleAlerts || want == StyleAlerts {
			fine = append(fine, fmt.Sprintf("macOS shows %s’s notices (%s) as %s.", n.Name, n.What, style))
			continue
		}
		f := Finding{ID: "alerts:style:" + n.ID, Area: AreaAlerts, Level: Note,
			Text:   fmt.Sprintf("macOS’s %s notices (%s) stay on screen until you close each one.", n.Name, n.What),
			Advice: "Raffi can have them go by themselves after a few seconds, as banners do; they still wait in Notification Center. System Settings › Notifications doesn’t list them, so this is the way to change it, and " + brand.Name + "’s Settings › Alerts keeps your choice.",
		}
		f.Fix = &Fix{Label: "Let them go by themselves",
			Run: []string{
				command("defaults", "export", "com.apple.ncprefs", "ncprefs.plist"),
				command("/usr/libexec/PlistBuddy", "-c", fmt.Sprintf("Set :apps:%d:flags %d", e.index, WithStyle(e.flags, StyleBanners)), "ncprefs.plist"),
				command("defaults", "import", "com.apple.ncprefs", "ncprefs.plist"),
				command("killall", "usernoted", "NotificationCenter"),
			},
			About: fmt.Sprintf("Changes how macOS shows %s’s notices, and nothing else, from alerts to banners. A copy of your notification settings as they were is kept in %s’s state folder first. Notices on screen close as macOS starts its notices again; they stay in Notification Center.", n.Name, brand.Name),
			Undo:  "Settings › Alerts in " + brand.Name + ": “Stay until I close them”.",
			kind:  fixNCStyle, ident: n.ID, path: StyleBanners}
		fs = append(fs, f)
	}
	return fs, fine
}

// advisory is how macOS's log says it showed a "Background Items Added"
// alert: the item's name, in curly quotes, starts its text.
var advisory = regexp.MustCompile(`Posting advisory notification request: identifier=\S+, body='“([^”]+)”`)

// Advisories counts the "Background Items Added" alerts in what `log
// show` printed, by the item they name.
func Advisories(log string) map[string]int {
	n := map[string]int{}
	for _, m := range advisory.FindAllStringSubmatch(log, -1) {
		n[m[1]]++
	}
	return n
}

// ownItems are the names macOS gives the app's own background items: the
// lid's launcher, and the command an older lid agent ran.
var ownItems = []string{brand.Slug + "-lid", brand.Slug}

// backgroundFinding says what background items macOS announced lately: a
// finding when it said so more than once, else a line, or nothing.
func backgroundFinding(n map[string]int) (Finding, string) {
	total := 0
	for _, c := range n {
		total += c
	}
	if total == 0 {
		return Finding{}, ""
	}
	names := slices.Sorted(maps.Keys(n))
	slices.SortStableFunc(names, func(a, b string) int { return n[b] - n[a] })
	var parts []string
	own, others := false, false
	for _, k := range names {
		parts = append(parts, fmt.Sprintf("%s (%s)", k, plural(n[k], "once", "%d times")))
		if slices.Contains(ownItems, k) {
			own = true
		} else {
			others = true
		}
	}
	window := For(logWindow)
	if total == 1 {
		return Finding{}, fmt.Sprintf("macOS announced one background item in the last %s: %s.", window, names[0])
	}
	f := Finding{ID: "alerts:background", Area: AreaAlerts, Level: Note,
		Text: fmt.Sprintf("macOS said %d times in the last %s that background items were added: %s.", total, window, strings.Join(parts, ", ")),
	}
	var advice []string
	if own {
		advice = append(advice, fmt.Sprintf("“%s” is %s’s lid check. Its launchd agent ran the %s command itself, which each install replaces, so macOS took every install for a new item. It runs a launcher now that no install replaces: macOS announces it once more, as “%s”, and then no more.", brand.Slug, brand.Name, brand.Slug, brand.Slug+"-lid"))
	}
	if others {
		advice = append(advice, "Each of the others is a program set to start by itself: Raffi’s look at what starts by itself weighs each one, and System Settings › General › Login Items lists them.")
	}
	f.Advice = strings.Join(advice, " ")
	return f, ""
}

// Ask is one time macOS asked the person for access in the app's name, or
// gave up waiting for them to answer.
type Ask struct {
	Service string // what it asked for, in words
	Program string // the program that wanted it
	Allowed bool
	Waited  bool // nobody answered, so macOS took it as No
}

// tccLine is one of tccd's records of a request, in `log show
// --style compact`.
var tccLine = regexp.MustCompile(`tccd\[(\d+):[^\]]*\] \[com\.apple\.TCC:access\] (AUTHREQ_\w+): msgID=([\d.]+), (.*)$`)

// tccProcess is a process as tccd names one: its identifier and program.
var tccProcess = regexp.MustCompile(`(responsible|accessing|requesting)=\{TCCDProcess: identifier=([^,]*),[^}]*binary_path=([^,}]*)`)

// The fields of tccd's records the look reads.
var (
	tccService = regexp.MustCompile(`service=(\w+)`)
	tccSubject = regexp.MustCompile(`subject=([^,]*)`)
	tccResult  = regexp.MustCompile(`authValue=(\d+), authReason=(\d+)`)
)

// Asks are the times macOS asked the person for access for the app
// bundleID, or for a program it is responsible for, such as a tool an
// agent runs, in what `log show` printed. tccd writes several lines for
// each request, by msgID; the answer's reason says whether a person was
// asked: User Consent (2) when they answered, Prompt Timeout (9) when
// nobody did, as public write-ups of tccd read its values.
func Asks(log, bundleID string) []Ask {
	type req struct {
		service, subject, responsible, program string
		value, reason                          int
		done                                   bool
	}
	reqs := map[string]*req{}
	var order []string
	for _, line := range strings.Split(log, "\n") {
		m := tccLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1] + "/" + m[3]
		q := reqs[key]
		if q == nil {
			q = &req{}
			reqs[key] = q
			order = append(order, key)
		}
		rest := m[4]
		switch m[2] {
		case "AUTHREQ_CTX":
			if s := tccService.FindStringSubmatch(rest); s != nil {
				q.service = s[1]
			}
		case "AUTHREQ_SUBJECT":
			if s := tccSubject.FindStringSubmatch(rest); s != nil {
				q.subject = s[1]
			}
		case "AUTHREQ_ATTRIBUTION":
			for _, p := range tccProcess.FindAllStringSubmatch(rest, -1) {
				switch p[1] {
				case "responsible":
					q.responsible = p[2]
				case "accessing":
					q.program = p[3]
				case "requesting":
					if q.program == "" {
						q.program = p[3]
					}
				}
			}
		case "AUTHREQ_RESULT":
			if s := tccResult.FindStringSubmatch(rest); s != nil {
				q.value, _ = strconv.Atoi(s[1])
				q.reason, _ = strconv.Atoi(s[2])
				q.done = true
			}
		}
	}
	var out []Ask
	for _, k := range order {
		q := reqs[k]
		if !q.done || (q.reason != 2 && q.reason != 9) || (q.subject != bundleID && q.responsible != bundleID) {
			continue
		}
		out = append(out, Ask{Service: serviceName(q.service), Program: filepath.Base(q.program), Allowed: q.value == 2, Waited: q.reason == 9})
	}
	return out
}

// services are tccd's services in words, as System Settings › Privacy &
// Security names them.
var services = map[string]string{
	"kTCCServiceSystemPolicyDocumentsFolder":  "your Documents folder",
	"kTCCServiceSystemPolicyDesktopFolder":    "your Desktop folder",
	"kTCCServiceSystemPolicyDownloadsFolder":  "your Downloads folder",
	"kTCCServiceSystemPolicyRemovableVolumes": "removable volumes",
	"kTCCServiceSystemPolicyNetworkVolumes":   "network volumes",
	"kTCCServiceSystemPolicyAppData":          "other apps’ data",
	"kTCCServiceSystemPolicyAppBundles":       "App Management",
	"kTCCServiceSystemPolicyAllFiles":         "Full Disk Access",
	"kTCCServiceFileProviderDomain":           "files in iCloud Drive or another file provider",
	"kTCCServiceAppleEvents":                  "Automation (controlling another app)",
	"kTCCServiceAccessibility":                "Accessibility",
	"kTCCServicePostEvent":                    "Accessibility (sending keys)",
	"kTCCServiceListenEvent":                  "Input Monitoring",
	"kTCCServiceScreenCapture":                "Screen Recording",
	"kTCCServiceMicrophone":                   "the microphone",
	"kTCCServiceCamera":                       "the camera",
	"kTCCServiceAddressBook":                  "Contacts",
	"kTCCServiceCalendar":                     "Calendars",
	"kTCCServiceReminders":                    "Reminders",
	"kTCCServicePhotos":                       "Photos",
	"kTCCServiceSpeechRecognition":            "Speech Recognition",
	"kTCCServiceBluetoothAlways":              "Bluetooth",
	"kTCCServiceDeveloperTool":                "Developer Tools",
}

// folders are the services Full Disk Access covers, for which it is the
// one answer that stops macOS asking.
var folders = []string{"your Documents folder", "your Desktop folder", "your Downloads folder", "removable volumes", "network volumes", "other apps’ data", "files in iCloud Drive or another file provider"}

func serviceName(s string) string {
	if n, ok := services[s]; ok {
		return n
	}
	if s == "" {
		return "something"
	}
	return strings.TrimPrefix(s, "kTCCService")
}

// asksFinding says what macOS asked the person for, in the app's name,
// lately: by what it asked for, with the programs that wanted it.
func asksFinding(asks []Ask, adhoc bool) (Finding, string) {
	window := For(logWindow)
	if len(asks) == 0 {
		return Finding{}, fmt.Sprintf("macOS asked you for no access in %s’s name in the last %s.", brand.Name, window)
	}
	type group struct {
		n        int
		programs []string
	}
	by := map[string]*group{}
	var order []string
	waited, onFolders := 0, false
	for _, a := range asks {
		g := by[a.Service]
		if g == nil {
			g = &group{}
			by[a.Service] = g
			order = append(order, a.Service)
		}
		g.n++
		if a.Program != "" && a.Program != "." && !slices.Contains(g.programs, a.Program) {
			g.programs = append(g.programs, a.Program)
		}
		if a.Waited {
			waited++
		}
		onFolders = onFolders || slices.Contains(folders, a.Service)
	}
	slices.SortStableFunc(order, func(a, b string) int { return by[b].n - by[a].n })
	var parts []string
	for _, s := range order {
		g := by[s]
		part := fmt.Sprintf("%s (%s", s, plural(g.n, "once", "%d times"))
		if len(g.programs) > 0 {
			part += ", for " + joinWords(g.programs[:min(len(g.programs), 3)])
		}
		parts = append(parts, part+")")
	}
	f := Finding{ID: "alerts:asks", Area: AreaAlerts,
		Level: asksLevel(len(asks), waited, adhoc),
		Text:  fmt.Sprintf("macOS asked you %s in the last %s for access in %s’s name: %s.", plural(len(asks), "once", "%d times"), window, brand.Name, strings.Join(parts, ", ")),
	}
	advice := []string{fmt.Sprintf("Agents run their tools in %s’s name, so macOS asks you, for %s, whenever one reaches for something it guards.", brand.Name, brand.Name)}
	if onFolders {
		advice = append(advice, fmt.Sprintf("For folders, allowing Full Disk Access for %s once (System Settings › Privacy & Security › Full Disk Access) answers them all, and macOS stops asking; or answer each once.", brand.Name))
	}
	if waited > 0 {
		advice = append(advice, plural(waited, "One went unanswered", "%d went unanswered")+", which macOS takes as No: the tool that asked failed quietly.")
	}
	if adhoc {
		advice = append(advice, "While "+brand.Name+" is signed ad hoc, macOS forgets each answer at the next build: sign it with an identity that stays the same, below.")
	}
	f.Advice = strings.Join(advice, " ")
	if f.Level == OK {
		return Finding{}, f.Text
	}
	return f, ""
}

// asksLevel is how much the times macOS asked for access lately matter:
// OK, a line among what is well; Note, worth a look; Warn, needs care.
// asks counts them, waited those nobody answered, and adhoc is true while
// the app is signed ad hoc, so that each answer lasts until the next
// build.
func asksLevel(asks, waited int, adhoc bool) string {
	// Any ask lately is worth a look: it says what to allow once.
	return Note
}

// signingFinding weighs the signature of each of the app's programs: one
// signed ad hoc, or not at all, is a new program to macOS at each build,
// so it forgets what the person allowed it and asks again.
func signingFinding(ctx context.Context, r Runner, in alertsInput) (Finding, []string) {
	var adhoc, fine []string
	for _, p := range in.Signed {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		_, errb, _ := r.Run(ctx, "/usr/bin/codesign", "-dv", "--verbose=2", p)
		if unreadable(string(errb)) {
			continue
		}
		signed, isAdhoc, authority := parseCodesign(string(errb))
		switch {
		case !signed || isAdhoc:
			adhoc = append(adhoc, tildePath(p, in.Home))
		case authority != "":
			fine = append(fine, fmt.Sprintf("%s is signed by “%s”, so macOS keeps what you allow it from one build to the next.", tildePath(p, in.Home), authority))
		}
	}
	if len(adhoc) == 0 {
		return Finding{}, fine
	}
	return Finding{ID: "alerts:adhoc", Area: AreaAlerts, Level: Note,
		Text:     fmt.Sprintf("%s is signed ad hoc, so macOS takes each new build for a new program: it forgets the access you allowed and asks again.", joinWords(adhoc)),
		Advice:   "Sign each build with an identity that stays the same: in the checkout you install from, run tools/sign-identity.sh, which makes one on this Mac and has tools/live.sh sign every build with it; it takes an Apple Development or Developer ID identity you have, by name, too. After the next build, allow each access once more, and macOS keeps it.",
		Evidence: []string{"codesign -dv says Signature=adhoc, or not signed at all."},
	}, fine
}

// loginItemsCrashes says how often System Settings' Login Items crashed
// this week, by the reports macOS kept of it.
func loginItemsCrashes(home string, now time.Time) Finding {
	ents, err := os.ReadDir(filepath.Join(home, "Library", "Logs", "DiagnosticReports"))
	if err != nil {
		return Finding{}
	}
	n, last := 0, time.Time{}
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, "LoginItems-") || !(strings.HasSuffix(name, ".ips") || strings.HasSuffix(name, ".crash")) {
			continue
		}
		fi, err := e.Info()
		if err != nil || now.Sub(fi.ModTime()) > 7*24*time.Hour {
			continue
		}
		n++
		if fi.ModTime().After(last) {
			last = fi.ModTime()
		}
	}
	if n == 0 {
		return Finding{}
	}
	return Finding{ID: "alerts:loginitems", Area: AreaAlerts, Level: Note,
		Text:   fmt.Sprintf("System Settings’ Login Items crashed %s this week, the last on %s, so it showed an error where its list should be.", plural(n, "once", "%d times"), last.Local().Format("2 Jan at 15:04")),
		Advice: "It is macOS’s own fault, in the code that reads its list of background items: it crashes when that list changes as it opens, as it does when you click a “Background Items Added” alert. Close the alert instead, then open System Settings › General › Login Items yourself, or with Open Login Items in " + brand.Name + "’s Settings › Alerts. A macOS update may mend it.",
		Evidence: []string{
			"~/Library/Logs/DiagnosticReports/LoginItems-*.ips: the crash reports.",
		},
	}
}

// plural is one when n is 1, else many with n in it.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return fmt.Sprintf(many, n)
}
