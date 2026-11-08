package raphael

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// xprotectStale is how long XProtect may go without an update before he
// says so: Apple sends new rules every week or two.
const xprotectStale = 60 * 24 * time.Hour

// protectInput is what the look at macOS's protections needs.
type protectInput struct {
	Now  time.Time
	Root string // stands in for / in tests
}

// protectFindings reads the state of macOS's own protections: System
// Integrity Protection, Gatekeeper, XProtect and its updates, FileVault and
// the firewall. Each is advice only: turning one on is for the person, in
// System Settings, often with an administrator's password.
func protectFindings(ctx context.Context, r Runner, in protectInput) (fs []Finding, fine []string) {
	if runtime.GOOS != "darwin" && in.Root == "" {
		return nil, nil
	}
	var on []string
	if out, _, err := r.Run(ctx, "/usr/bin/csrutil", "status"); err == nil {
		switch sipOn(string(out)) {
		case 1:
			on = append(on, "System Integrity Protection")
		case 0:
			fs = append(fs, Finding{ID: "sip", Area: AreaProtection, Level: Warn,
				Text:   "System Integrity Protection is off, so nothing keeps programs from changing macOS itself.",
				Advice: "Turn it back on: start up in Recovery, open Terminal there, run `csrutil enable` and restart. Leave it off only if you turned it off on purpose."})
		}
	}
	if out, errb, err := r.Run(ctx, "/usr/sbin/spctl", "--status"); err == nil || len(out)+len(errb) > 0 {
		switch s := string(out) + string(errb); {
		case strings.Contains(s, "assessments enabled"):
			on = append(on, "Gatekeeper")
		case strings.Contains(s, "assessments disabled"):
			fs = append(fs, Finding{ID: "gatekeeper", Area: AreaProtection, Level: Warn,
				Text:   "Gatekeeper is off, so apps open without macOS checking who made them.",
				Advice: "Turn it on in System Settings > Privacy & Security: allow apps from the App Store and identified developers."})
		}
	}
	if f, ok := xprotectFinding(ctx, r, in); ok {
		if f.Level == OK {
			fine = append(fine, f.Text)
		} else {
			fs = append(fs, f)
		}
	}
	fs = append(fs, updateFindings(ctx, r, in)...)
	if out, _, err := r.Run(ctx, "/usr/bin/fdesetup", "status"); err == nil {
		switch s := string(out); {
		case strings.Contains(s, "FileVault is On"):
			on = append(on, "FileVault")
		case strings.Contains(s, "FileVault is Off"):
			fs = append(fs, Finding{ID: "filevault", Area: AreaProtection, Level: Note,
				Text:   "FileVault is off, so anyone who has your Mac can read its disk.",
				Advice: "Turn it on in System Settings > Privacy & Security > FileVault, and keep the recovery key somewhere safe."})
		}
	}
	if out, _, err := r.Run(ctx, "/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate"); err == nil {
		switch s := string(out); {
		case strings.Contains(s, "enabled"):
			on = append(on, "the firewall")
		case strings.Contains(s, "disabled"):
			fs = append(fs, Finding{ID: "firewall", Area: AreaProtection, Level: Note,
				Text:   "The firewall is off: macOS ships it so, and on a network you trust that is fine.",
				Advice: "On networks you don't trust, such as a café's, turn it on in System Settings > Network > Firewall."})
		}
	}
	if len(on) > 0 {
		fine = append(fine, "On: "+joinWords(on)+".")
	}
	return fs, fine
}

// sipOn reads `csrutil status`: 1 on, 0 off, -1 unknown.
func sipOn(out string) int {
	switch {
	case strings.Contains(out, "status: enabled"):
		return 1
	case strings.Contains(out, "status: disabled"):
		return 0
	}
	return -1
}

// xprotectFinding says which XProtect rules the Mac has and when they came:
// its version and its rules' date, from the bundle macOS keeps them in.
func xprotectFinding(ctx context.Context, r Runner, in protectInput) (Finding, bool) {
	for _, b := range []string{"/Library/Apple/System/Library/CoreServices/XProtect.bundle", "/System/Library/CoreServices/XProtect.bundle"} {
		b = in.Root + b
		fi, err := os.Stat(b + "/Contents/Resources/XProtect.meta.plist")
		if err != nil {
			fi, err = os.Stat(b)
		}
		if err != nil {
			continue
		}
		version := ""
		if m, err := readPlistFile(ctx, r, b+"/Contents/Info.plist"); err == nil {
			version = plistString(m, "CFBundleShortVersionString")
		}
		age := in.Now.Sub(fi.ModTime())
		f := Finding{ID: "xprotect", Area: AreaProtection, Level: OK,
			Text: fmt.Sprintf("XProtect, macOS's malware check, has rules %s from %s.", version, fi.ModTime().Format("2 Jan 2006"))}
		if age > xprotectStale {
			f.Level = Warn
			f.Text = fmt.Sprintf("XProtect, macOS's malware check, has not had new rules for %s (version %s, %s).", For(age), version, fi.ModTime().Format("2 Jan 2006"))
			f.Advice = "Turn on System Settings > General > Software Update > Automatic Updates > Install Security Responses and system files, and check for updates."
		}
		return f, true
	}
	return Finding{}, false
}

// updateFindings reads Software Update's settings: whether the Mac installs
// its security updates by itself, and the updates waiting.
func updateFindings(ctx context.Context, r Runner, in protectInput) []Finding {
	m, err := readPlistFile(ctx, r, in.Root+"/Library/Preferences/com.apple.SoftwareUpdate.plist")
	if err != nil {
		return nil
	}
	var fs []Finding
	// A setting not there is macOS's default: on.
	var off []string
	for _, k := range [][2]string{{"CriticalUpdateInstall", "security updates"}, {"ConfigDataInstall", "security responses and system files"}} {
		if v, ok := m[k[0]].(bool); ok && !v {
			off = append(off, k[1])
		} else if n, ok := m[k[0]].(int64); ok && n == 0 {
			off = append(off, k[1])
		}
	}
	if len(off) > 0 {
		fs = append(fs, Finding{ID: "autoupdate", Area: AreaProtection, Level: Warn,
			Text:   "macOS does not install " + joinWords(off) + " by itself.",
			Advice: "Turn on System Settings > General > Software Update > Automatic Updates > Install Security Responses and system files."})
	}
	if list, ok := m["RecommendedUpdates"].([]any); ok && len(list) > 0 {
		var names []string
		for _, u := range list {
			if d, ok := u.(map[string]any); ok {
				// macOS's own name its version already: "macOS Sonoma
				// 14.8.9", version "14.8.9".
				name := plistString(d, "Display Name")
				if v := plistString(d, "Display Version"); v != "" && !strings.Contains(name, v) {
					name += " " + v
				}
				if name != "" {
					names = append(names, name)
				}
			}
		}
		if len(names) > 0 {
			fs = append(fs, Finding{ID: "updates", Area: AreaProtection, Level: Note,
				Text:   "Updates are waiting: " + joinWords(names) + ".",
				Advice: "Install them from System Settings > General > Software Update, when a restart suits you."})
		}
	}
	return fs
}

// joinWords is a list in words: "a", "a and b", "a, b and c".
func joinWords(ws []string) string {
	switch len(ws) {
	case 0:
		return ""
	case 1:
		return ws[0]
	}
	return strings.Join(ws[:len(ws)-1], ", ") + " and " + ws[len(ws)-1]
}
