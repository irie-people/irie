package raphael

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
)

// ncApp is one sender in the stand-in's notification settings.
type ncApp struct {
	id    string
	flags int64
}

// fakeNC stands in for macOS's notification settings and the tools that
// change them: defaults export and import, PlistBuddy and killall.
type fakeNC struct {
	apps []ncApp
	keep bool // macOS keeps its own: an import changes nothing
	ran  []string
}

const btm, tccd = "com.apple.BTMNotificationAgent", "com.apple.tccd"

// The flags macOS gave both on a Mac: alerts, with other bits set.
const alertFlags, bannerFlags int64 = 327680150, 327680142

func (f *fakeNC) xml() string { return ncXML(f.apps) }

func ncXML(apps []ncApp) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>apps</key>
	<array>
`)
	for _, a := range apps {
		fmt.Fprintf(&b, "\t\t<dict>\n\t\t\t<key>bundle-id</key>\n\t\t\t<string>%s</string>\n\t\t\t<key>flags</key>\n\t\t\t<integer>%d</integer>\n\t\t\t<key>src</key>\n\t\t\t<array>\n\t\t\t\t<dict>\n\t\t\t\t\t<key>req</key>\n\t\t\t\t\t<data>+t4MAAAAAEQ=</data>\n\t\t\t\t</dict>\n\t\t\t</array>\n\t\t</dict>\n", a.id, a.flags)
	}
	b.WriteString("\t</array>\n\t<key>version</key>\n\t<integer>2</integer>\n</dict>\n</plist>\n")
	return b.String()
}

func readNCApps(t *testing.T, path string) []ncApp {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := parsePlist(b)
	if err != nil {
		t.Fatal(err)
	}
	var out []ncApp
	for _, a := range v.(map[string]any)["apps"].([]any) {
		m := a.(map[string]any)
		out = append(out, ncApp{plistString(m, "bundle-id"), m["flags"].(int64)})
	}
	return out
}

func (f *fakeNC) runner(t *testing.T) Runner {
	return runFunc(func(name string, args ...string) ([]byte, []byte, error) {
		f.ran = append(f.ran, strings.Join(append([]string{filepath.Base(name)}, args...), " "))
		switch {
		case name == "/usr/bin/defaults" && slices.Equal(args, []string{"export", "com.apple.ncprefs", "-"}):
			return []byte(f.xml()), nil, nil
		case name == "/usr/libexec/PlistBuddy" && len(args) == 3 && args[0] == "-c":
			var i int
			var v int64
			if n, _ := fmt.Sscanf(args[1], "Set :apps:%d:flags %d", &i, &v); n != 2 || i >= len(f.apps) {
				return nil, []byte("Set: Entry, \":apps\", Does Not Exist"), errors.New("exit status 1")
			}
			apps := readNCApps(t, args[2])
			apps[i].flags = v
			return nil, nil, os.WriteFile(args[2], []byte(ncXML(apps)), 0o600)
		case name == "/usr/bin/defaults" && len(args) == 3 && args[0] == "import":
			if !f.keep {
				f.apps = readNCApps(t, args[2])
			}
			return nil, nil, nil
		case name == "/usr/bin/killall":
			return nil, nil, nil
		}
		return nil, nil, errors.New("not here")
	})
}

// runFunc is a Runner made of a function.
type runFunc func(name string, args ...string) ([]byte, []byte, error)

func (f runFunc) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	return f(name, args...)
}

func TestTheStyleIsTwoBitsOfTheFlags(t *testing.T) {
	if StyleOf(alertFlags) != StyleAlerts || StyleOf(bannerFlags) != StyleBanners || StyleOf(2) != StyleNone {
		t.Errorf("styles: %s %s %s", StyleOf(alertFlags), StyleOf(bannerFlags), StyleOf(2))
	}
	if got := WithStyle(alertFlags, StyleBanners); got != bannerFlags {
		t.Errorf("banners: %d, want %d", got, bannerFlags)
	}
	if got := WithStyle(bannerFlags, StyleAlerts); got != alertFlags {
		t.Errorf("alerts: %d, want %d", got, alertFlags)
	}
	if got := WithStyle(alertFlags, StyleNone); got != alertFlags-16 {
		t.Errorf("none: %d", got)
	}
}

func TestSetStyleChangesOneSenderAndKeepsACopy(t *testing.T) {
	f := &fakeNC{apps: []ncApp{{"com.apple.Safari", 8396814}, {btm, alertFlags}, {tccd, alertFlags}}}
	before := f.xml()
	dir := filepath.Join(t.TempDir(), "ncprefs")
	changed, err := SetStyle(context.Background(), f.runner(t), btm, StyleBanners, dir)
	if err != nil || !changed {
		t.Fatalf("changed %v, %v; ran %q", changed, err, f.ran)
	}
	if want := []ncApp{{"com.apple.Safari", 8396814}, {btm, bannerFlags}, {tccd, alertFlags}}; !slices.Equal(f.apps, want) {
		t.Errorf("settings now %v, want %v", f.apps, want)
	}
	if !slices.Contains(f.ran, "killall usernoted") || !slices.Contains(f.ran, "killall NotificationCenter") {
		t.Errorf("macOS's notices were not started again: %q", f.ran)
	}
	copies, _ := filepath.Glob(filepath.Join(dir, "ncprefs-*.plist"))
	if len(copies) != 1 {
		t.Fatalf("copies %q", copies)
	}
	if b, _ := os.ReadFile(copies[0]); string(b) != before {
		t.Errorf("the copy is not the settings as they were")
	}
	if edits, _ := filepath.Glob(filepath.Join(dir, "edit-*")); len(edits) != 0 {
		t.Errorf("the edit was left: %q", edits)
	}
	// Once it is so, nothing more runs.
	f.ran = nil
	if changed, err := SetStyle(context.Background(), f.runner(t), btm, StyleBanners, dir); err != nil || changed || slices.Contains(f.ran, "killall usernoted") {
		t.Errorf("again: changed %v, %v; ran %q", changed, err, f.ran)
	}
}

func TestSetStyleSaysWhenMacOSKeepsItsOwnOrHasNoSuchSender(t *testing.T) {
	f := &fakeNC{apps: []ncApp{{btm, alertFlags}}, keep: true}
	if _, err := SetStyle(context.Background(), f.runner(t), btm, StyleBanners, t.TempDir()); err == nil || !strings.Contains(err.Error(), "kept Login Items’s notices as alerts") {
		t.Errorf("kept: %v", err)
	}
	f = &fakeNC{apps: []ncApp{{"com.apple.Safari", 8}}}
	if _, err := SetStyle(context.Background(), f.runner(t), btm, StyleBanners, t.TempDir()); err == nil || !strings.Contains(err.Error(), "no entry for Login Items") {
		t.Errorf("no sender: %v", err)
	}
	if _, err := SetStyle(context.Background(), f.runner(t), btm, "loud", t.TempDir()); err == nil {
		t.Errorf("a style that isn't one was taken")
	}
}

func TestOnlyTheLastFewCopiesAreKept(t *testing.T) {
	dir := t.TempDir()
	for i := range 8 {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("ncprefs-20261122T0%d0000.000Z.plist", i)), nil, 0o600)
	}
	pruneNCBackups(dir)
	left, _ := filepath.Glob(filepath.Join(dir, "ncprefs-*.plist"))
	if len(left) != ncBackups || !strings.Contains(left[0], "T030000") {
		t.Errorf("left %q", left)
	}
}

func TestStyleFindingsOfferBannersForAlertsTheyDidNotChoose(t *testing.T) {
	f := &fakeNC{apps: []ncApp{{btm, alertFlags}, {tccd, bannerFlags}}}
	fs, fine := styleFindings(context.Background(), f.runner(t), alertsInput{})
	if len(fs) != 1 || fs[0].ID != "alerts:style:"+btm || fs[0].Area != AreaAlerts || fs[0].Fix == nil {
		t.Fatalf("findings %+v", fs)
	}
	if id, style, ok := fs[0].Fix.Style(); !ok || id != btm || style != StyleBanners {
		t.Errorf("the fix sets %s %s %v", id, style, ok)
	}
	if len(fine) != 1 || !strings.Contains(fine[0], "Privacy & Security") || !strings.Contains(fine[0], "banners") {
		t.Errorf("fine %q", fine)
	}
	// Alerts the person chose are well as they are.
	fs, _ = styleFindings(context.Background(), f.runner(t), alertsInput{Want: map[string]string{btm: StyleAlerts}})
	if len(fs) != 0 {
		t.Errorf("a style they chose was flagged: %+v", fs)
	}
}

func TestTheFixKeepsWhatItChangesAcrossCores(t *testing.T) {
	fix := Fix{Label: "Let them go by themselves", Run: []string{"x"}, kind: fixNCStyle, ident: btm, path: StyleBanners}
	b, err := json.Marshal(fix)
	if err != nil {
		t.Fatal(err)
	}
	var back Fix
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if id, style, ok := back.Style(); !ok || id != btm || style != StyleBanners {
		t.Errorf("after a round trip: %s %s %v", id, style, ok)
	}
	if _, _, ok := (Fix{kind: fixTrash}).Style(); ok {
		t.Errorf("another fix says it sets a style")
	}
	f := &fakeNC{apps: []ncApp{{btm, alertFlags}}}
	state := t.TempDir()
	said, err := Do(context.Background(), f.runner(t), back, DoInput{Home: "/Users/alice", State: state})
	if err != nil || !strings.Contains(said, "as banners now") || StyleOf(f.apps[0].flags) != StyleBanners {
		t.Errorf("Do: %q, %v", said, err)
	}
	if copies, _ := filepath.Glob(filepath.Join(NCBackupDir(state), "ncprefs-*")); len(copies) != 1 {
		t.Errorf("copies in the state folder: %q", copies)
	}
}

// The lines macOS's log holds, in `log show --style compact`, as on a Mac.
const btmLog = `Timestamp               Ty Process[PID:TID]
2026-11-22 08:17:18.876 Df BackgroundTaskManagementAgent[5570:257e1a8] [com.apple.backgroundtaskmanagement:main] Posting advisory notification request: identifier=30BA9EC9-E707-4D56-8850-FC0D731B68A3, body='“irie” is an item that can run in the backgro…'
2026-11-22 08:51:20.820 Df BackgroundTaskManagementAgent[5570:265097f] [com.apple.backgroundtaskmanagement:main] Posting advisory notification request: identifier=D6406C3C-C8A2-4CDB-8076-2E7DEEF96E57, body='“irie” is an item that can run in the backgro…'
2026-11-22 09:09:24.995 Df BackgroundTaskManagementAgent[71002:27026c4] [com.apple.backgroundtaskmanagement:main] Posting advisory notification request: identifier=7A4ECB4D-7D8F-4C1E-B4D0-575E1D4DA156, body='“Example Updater” is an item that can run in…'
`

func TestBackgroundItemsAnnouncedLately(t *testing.T) {
	n := Advisories(btmLog)
	if n["irie"] != 2 || n["Example Updater"] != 1 || len(n) != 2 {
		t.Fatalf("advisories %v", n)
	}
	f, line := backgroundFinding(n)
	if f.ID != "alerts:background" || line != "" || !strings.Contains(f.Text, "3 times") || !strings.Contains(f.Text, "irie (2 times), Example Updater (once)") {
		t.Errorf("finding %+v, line %q", f, line)
	}
	if !strings.Contains(f.Advice, "launcher") {
		t.Errorf("the advice doesn't say why the app's own item came again: %q", f.Advice)
	}
	if f, line := backgroundFinding(map[string]int{"Example Updater": 1}); f.ID != "" || !strings.Contains(line, "one background item") {
		t.Errorf("one: %+v, %q", f, line)
	}
	if f, line := backgroundFinding(nil); f.ID != "" || line != "" {
		t.Errorf("none: %+v, %q", f, line)
	}
}

// tccLog is tccd's records of four requests: a person allowed rg into
// Documents and nobody answered for osascript's Automation, both in the
// app's name; a preflight no one was asked about; and another app's ask.
var tccLog = strings.NewReplacer("BUNDLE", brand.BundleID).Replace(`2026-11-22 09:05:21.100 Df tccd[155:26d4a1d] [com.apple.TCC:access] AUTHREQ_CTX: msgID=801.1, function=TCCAccessRequest, service=kTCCServiceSystemPolicyDocumentsFolder, preflight=no, query=1, client_dict=(null), daemon_dict=<private>
2026-11-22 09:05:21.101 Df tccd[155:26d4a1d] [com.apple.TCC:access] AUTHREQ_ATTRIBUTION: msgID=801.1, attribution={responsible={TCCDProcess: identifier=BUNDLE, pid=10, auid=501, euid=501, binary_path=/Users/alice/Applications/App.app/Contents/MacOS/App}, accessing={TCCDProcess: identifier=rg, pid=11, auid=501, euid=501, binary_path=/opt/homebrew/bin/rg}, },
2026-11-22 09:05:21.102 Df tccd[155:26d4a1d] [com.apple.TCC:access] AUTHREQ_SUBJECT: msgID=801.1, subject=BUNDLE,
2026-11-22 09:05:29.000 Df tccd[155:26d4a1d] [com.apple.TCC:access] AUTHREQ_RESULT: msgID=801.1, authValue=2, authReason=2, authVersion=1, error=(null),
2026-11-22 09:06:00.000 Df tccd[155:26d4a1d] [com.apple.TCC:access] AUTHREQ_CTX: msgID=802.1, function=TCCAccessRequest, service=kTCCServiceAppleEvents, preflight=no, query=1, client_dict=(null), daemon_dict=<private>
2026-11-22 09:06:00.001 Df tccd[155:26d4a1d] [com.apple.TCC:access] AUTHREQ_ATTRIBUTION: msgID=802.1, attribution={responsible={TCCDProcess: identifier=BUNDLE, pid=10, auid=501, euid=501, binary_path=/Users/alice/Applications/App.app/Contents/MacOS/App}, accessing={TCCDProcess: identifier=com.apple.osascript, pid=12, auid=501, euid=501, binary_path=/usr/bin/osascript}, },
2026-11-22 09:07:00.000 Df tccd[155:26d4a1d] [com.apple.TCC:access] AUTHREQ_RESULT: msgID=802.1, authValue=0, authReason=9, authVersion=1, error=(null),
2026-11-22 09:08:00.000 Df tccd[155:26d4a1d] [com.apple.TCC:access] AUTHREQ_CTX: msgID=803.1, function=<private>, service=kTCCServiceScreenCapture, preflight=yes, query=1, client_dict=(null), daemon_dict=<private>
2026-11-22 09:08:00.001 Df tccd[155:26d4a1d] [com.apple.TCC:access] AUTHREQ_SUBJECT: msgID=803.1, subject=BUNDLE,
2026-11-22 09:08:00.002 Df tccd[155:26d4a1d] [com.apple.TCC:access] AUTHREQ_RESULT: msgID=803.1, authValue=1, authReason=5, authVersion=1, error=(null),
2026-11-22 09:09:00.000 Df tccd[50106:26d4a1d] [com.apple.TCC:access] AUTHREQ_CTX: msgID=801.1, function=TCCAccessRequest, service=kTCCServiceCamera, preflight=no, query=1, client_dict=(null), daemon_dict=<private>
2026-11-22 09:09:00.001 Df tccd[50106:26d4a1d] [com.apple.TCC:access] AUTHREQ_SUBJECT: msgID=801.1, subject=com.example.other,
2026-11-22 09:09:09.000 Df tccd[50106:26d4a1d] [com.apple.TCC:access] AUTHREQ_RESULT: msgID=801.1, authValue=2, authReason=2, authVersion=1, error=(null),
`)

func TestAsksAreThePromptsInTheAppsName(t *testing.T) {
	asks := Asks(tccLog, brand.BundleID)
	want := []Ask{
		{Service: "your Documents folder", Program: "rg", Allowed: true},
		{Service: "Automation (controlling another app)", Program: "osascript", Waited: true},
	}
	if !slices.Equal(asks, want) {
		t.Fatalf("asks %+v, want %+v", asks, want)
	}
	f, line := asksFinding(asks, true)
	text := f.Text
	if f.ID == "" {
		text = line
	}
	for _, w := range []string{"2 times", "your Documents folder (once, for rg)", "Automation (controlling another app) (once, for osascript)"} {
		if !strings.Contains(text, w) {
			t.Errorf("text lacks %q: %s", w, text)
		}
	}
	if f.ID != "" {
		for _, w := range []string{"Full Disk Access", "unanswered", "signed ad hoc"} {
			if !strings.Contains(f.Advice, w) {
				t.Errorf("advice lacks %q: %s", w, f.Advice)
			}
		}
	}
	if f, line := asksFinding(nil, false); f.ID != "" || !strings.Contains(line, "no access") {
		t.Errorf("none: %+v, %q", f, line)
	}
}

func TestSigningAdHocMeansMacOSAsksAgain(t *testing.T) {
	dir := t.TempDir()
	app, cli := filepath.Join(dir, "App.app"), filepath.Join(dir, "app")
	os.MkdirAll(app, 0o755)
	os.WriteFile(cli, nil, 0o755)
	r := &fakeRunner{errs: map[string]string{
		"/usr/bin/codesign -dv --verbose=2 " + app: "Executable=" + app + "/Contents/MacOS/App\nIdentifier=com.example.app\nSignature=adhoc\nTeamIdentifier=not set\n",
		"/usr/bin/codesign -dv --verbose=2 " + cli: "Executable=" + cli + "\nIdentifier=app\nAuthority=Example local code signing\nTeamIdentifier=not set\n",
	}}
	f, fine := signingFinding(context.Background(), r, alertsInput{Home: dir, Signed: []string{app, cli, filepath.Join(dir, "gone")}})
	if f.ID != "alerts:adhoc" || !strings.Contains(f.Text, "~/App.app is signed ad hoc") || !strings.Contains(f.Advice, "tools/sign-identity.sh") {
		t.Errorf("finding %+v", f)
	}
	if len(fine) != 1 || !strings.Contains(fine[0], "“Example local code signing”") {
		t.Errorf("fine %q", fine)
	}
}

func TestLoginItemsCrashesThisWeek(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "Library", "Logs", "DiagnosticReports")
	os.MkdirAll(dir, 0o755)
	now := time.Date(2026, 11, 22, 9, 30, 0, 0, time.Local)
	for name, age := range map[string]time.Duration{"LoginItems-2026-11-22-092000.ips": 10 * time.Minute, "LoginItems-2026-11-20-101500.ips": 2 * 24 * time.Hour, "LoginItems-2026-11-01-080000.ips": 21 * 24 * time.Hour, "Example-2026-11-22-090000.ips": time.Hour} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, nil, 0o600)
		os.Chtimes(p, now.Add(-age), now.Add(-age))
	}
	f := loginItemsCrashes(home, now)
	if f.ID != "alerts:loginitems" || !strings.Contains(f.Text, "2 times this week") || !strings.Contains(f.Text, "22 Nov at 09:20") {
		t.Errorf("finding %+v", f)
	}
	if f := loginItemsCrashes(t.TempDir(), now); f.ID != "" {
		t.Errorf("no reports: %+v", f)
	}
}

func TestALookWithoutTheLogSaysNothingOfWhatMacOSAsked(t *testing.T) {
	f := &fakeNC{apps: []ncApp{{btm, alertFlags}}}
	fs, fine := alertFindings(context.Background(), f.runner(t), alertsInput{Home: t.TempDir(), Now: time.Now()})
	for _, line := range fine {
		if strings.Contains(line, "asked you") || strings.Contains(line, "background item") {
			t.Errorf("a log that didn't read said %q", line)
		}
	}
	if len(fs) != 1 || fs[0].ID != "alerts:style:"+btm {
		t.Errorf("findings %+v", fs)
	}
	log := btmLog + tccLog
	fs, _ = alertFindings(context.Background(), f.runner(t), alertsInput{Home: t.TempDir(), Now: time.Now(), Log: &log})
	var ids []string
	for _, x := range fs {
		ids = append(ids, x.ID)
	}
	if !slices.Contains(ids, "alerts:background") {
		t.Errorf("with the log: %q", ids)
	}
}
