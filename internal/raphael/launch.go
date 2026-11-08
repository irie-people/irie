package raphael

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// What starts at login or with the system, and how he weighs it. A launchd
// job is a plist in a LaunchAgents or LaunchDaemons folder (launchd.plist
// (5)); the kinds of persistence he looks for are those public write-ups of
// Mac malware name, such as MITRE ATT&CK's Launch Agent (T1543.001), Launch
// Daemon (T1543.004), Cron (T1053.003) and Login Hook (T1037.002). Every
// sign alone has good programs that show it too, so he adds them up, and
// says how sure he is: never certain.

// launchDir is one folder of launchd jobs.
type launchDir struct {
	path   string
	user   bool   // the person's own: in their gui domain, where he can turn one off
	starts string // when its jobs start, in words
}

// startupInput is what the look at startup items needs.
type startupInput struct {
	Home  string
	UID   int
	Known []string // what the person said is theirs: labels, plist paths or programs
	Now   time.Time
	// Roots, when set, stand in for / in tests.
	Root string
}

// onMac is path as the Mac names it: without the test's root, whether the
// path went through a link to it or not.
func (in startupInput) onMac(path string) string {
	if in.Root == "" {
		return path
	}
	roots := []string{in.Root}
	if r, err := filepath.EvalSymlinks(in.Root); err == nil {
		roots = append(roots, r)
	}
	for _, r := range roots {
		if rest, ok := strings.CutPrefix(path, r); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
			return rest
		}
	}
	return path
}

func (in startupInput) dirs() []launchDir {
	return []launchDir{
		{path: filepath.Join(in.Home, "Library", "LaunchAgents"), user: true, starts: "as you log in"},
		{path: filepath.Join(in.Root, "/Library/LaunchAgents"), starts: "as anyone logs in"},
		{path: filepath.Join(in.Root, "/Library/LaunchDaemons"), starts: "as the Mac starts, with the system's rights"},
	}
}

// job is one launchd job, as its plist says.
type job struct {
	plist   string
	dir     launchDir
	label   string
	program string   // as the plist names it
	args    []string // its arguments, the program's own first
	runAt   bool     // RunAtLoad
	keep    bool     // KeepAlive
	every   int      // StartInterval, seconds
	cal     bool     // StartCalendarInterval
	watch   bool     // WatchPaths or QueueDirectories
	demand  bool     // Sockets or MachServices: started when asked for
	empty   bool     // the plist says nothing
}

// startupFindings weighs every launchd job outside the system's own
// folders, the person's crontab, login hooks and old startup items.
func startupFindings(ctx context.Context, r Runner, in startupInput) (fs []Finding, fine []string) {
	if runtime.GOOS != "darwin" && in.Root == "" {
		return nil, nil
	}
	sigs := map[string]signature{}
	var looked, known int
	for _, d := range in.dirs() {
		ents, err := os.ReadDir(d.path)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !strings.HasSuffix(e.Name(), ".plist") {
				continue
			}
			looked++
			path := filepath.Join(d.path, e.Name())
			f, isKnown := weighJob(ctx, r, in, d, path, sigs)
			switch {
			case isKnown:
				known++
			case f.ID != "":
				fs = append(fs, f)
			}
		}
	}
	if looked > 0 {
		line := fmt.Sprintf("Startup: %d launch agents and daemons besides macOS's own", looked)
		if n := looked - len(fs) - known; n == looked {
			line += ", none with warning signs."
		} else {
			line += fmt.Sprintf("; %d with no warning signs", n)
			if known > 0 {
				line += fmt.Sprintf(", %d you know", known)
			}
			line += "."
		}
		fine = append(fine, line)
	}
	cron, cronFine := cronFindings(ctx, r, in)
	fs = append(fs, cron...)
	fine = append(fine, cronFine...)
	fs = append(fs, hookFindings(ctx, r, in)...)
	if ents, err := os.ReadDir(filepath.Join(in.Root, "/Library/StartupItems")); err == nil && len(ents) > 0 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		fs = append(fs, Finding{ID: "startupitems", Area: AreaStartup, Level: Note,
			Text:     "/Library/StartupItems holds an old kind of startup item, which macOS no longer runs.",
			Evidence: []string{"In it: " + strings.Join(names, ", ") + "."},
			Advice:   "It is likely left from an app long gone. If you don't know it, ask whoever looks after your Mac before removing it: it needs an administrator."})
	}
	return fs, fine
}

// weighJob weighs one launchd job. isKnown is true when the person said it
// is theirs; a zero Finding means nothing to say.
func weighJob(ctx context.Context, r Runner, in startupInput, d launchDir, path string, sigs map[string]signature) (Finding, bool) {
	base := filepath.Base(path)
	f := Finding{ID: "launch:" + tildePath(path, in.Home), Area: AreaStartup}
	m, err := readPlistFile(ctx, r, path)
	if err != nil {
		if errorsIsPermission(err) {
			// A daemon's plist only an administrator can read: odd, but
			// some good apps install theirs so.
			f.Level, f.Text = Note, fmt.Sprintf("%s is a launch daemon only an administrator can read, so Raffi can't tell what it starts.", tildePath(path, in.Home))
			f.Advice = "Its name says whose it may be. If you don't know it, ask whoever looks after your Mac."
			f.Key = strings.TrimSuffix(base, ".plist")
			return f, knownTo(in.Known, f.Key, path, "")
		}
		f.Level, f.Text = Note, fmt.Sprintf("%s does not read as a launchd plist: %v.", tildePath(path, in.Home), err)
		f.Advice = "launchd skips a file it can't read, so it starts nothing."
		return f, false
	}
	j := parseJob(m, path, d)
	f.Key = j.label
	if f.Key == "" {
		f.Key = strings.TrimSuffix(base, ".plist")
	}
	if knownTo(in.Known, j.label, path, j.program) {
		return Finding{}, true
	}
	if j.empty {
		f.Level = Note
		f.Text = fmt.Sprintf("%s is empty, so it starts nothing.", tildePath(path, in.Home))
		f.Advice = "It does no harm. It may be left from an app you removed, or emptied on purpose so that a program cannot put it back and start again: leave it unless you know which."
		return f, false
	}
	prog := resolveProgram(j.program, in.Root)
	what := describeJob(j, in.Home)
	if prog == "" || !exists(prog) {
		f.Level = Note
		f.Text = fmt.Sprintf("%s starts %s, which is no longer here.", jobName(j), tildePath(j.program, in.Home))
		f.Advice = "It is likely left from an app you removed: it does nothing, and can go."
		if d.user {
			f.Fix = launchOffFix(j, in)
		} else {
			f.Advice += " Removing it needs an administrator."
		}
		return f, false
	}
	signs := jobSigns(ctx, r, j, prog, in, sigs)
	score := 0
	for _, s := range signs {
		score += s.weight
	}
	switch {
	case score >= suspiciousScore:
		f.Level = Warn
		f.Text = fmt.Sprintf("%s looks suspicious: it starts %s %s.", jobName(j), what, d.starts)
		f.Sure = "Fairly sure something is wrong with it, though not certain: these are signs, and a good program can show them too."
	case score >= lookScore:
		f.Level = Note
		f.Text = fmt.Sprintf("%s is worth a look: it starts %s %s.", jobName(j), what, d.starts)
		f.Sure = "Not sure: many good programs show these signs too."
	default:
		return Finding{}, false
	}
	for _, s := range signs {
		f.Evidence = append(f.Evidence, s.text)
	}
	if d.user {
		f.Advice = "If you know it, mark it as yours and Raffi no longer flags it. If you don't, Raffi can turn it off: nothing is deleted, and it can be turned back on."
		f.Fix = launchOffFix(j, in)
	} else {
		f.Advice = "If you know it, mark it as yours. If you don't, ask whoever looks after your Mac: turning it off needs an administrator. System Settings > General > Login Items lists it too."
	}
	return f, false
}

// The scores that make a job worth a look, and suspicious.
const (
	lookScore       = 3
	suspiciousScore = 5
)

func parseJob(m map[string]any, path string, d launchDir) job {
	j := job{plist: path, dir: d, label: plistString(m, "Label"), program: plistString(m, "Program"), args: plistStrings(m, "ProgramArguments")}
	if j.program == "" && len(j.args) > 0 {
		j.program = j.args[0]
	}
	j.runAt = plistBool(m, "RunAtLoad")
	j.keep = plistBool(m, "KeepAlive")
	switch n := m["StartInterval"].(type) {
	case int64:
		j.every = int(n)
	case float64:
		j.every = int(n)
	}
	_, j.cal = m["StartCalendarInterval"]
	_, wp := m["WatchPaths"]
	_, qd := m["QueueDirectories"]
	j.watch = wp || qd
	_, socks := m["Sockets"]
	_, mach := m["MachServices"]
	j.demand = socks || mach
	j.empty = j.label == "" && j.program == ""
	return j
}

// jobName is how a job is called: by its label, or its file's name.
func jobName(j job) string {
	if j.label != "" {
		return j.label
	}
	return filepath.Base(j.plist)
}

// describeJob is what a job starts, and how often, in words.
func describeJob(j job, home string) string {
	what := tildePath(j.program, home)
	if len(j.args) > 1 {
		args := j.args[1:]
		if len(args) > 3 {
			args = append(slices.Clone(args[:3]), "…")
		}
		what += " " + strings.Join(args, " ")
	}
	var how []string
	switch {
	case j.keep:
		how = append(how, "keeps it running")
	case j.every > 0:
		how = append(how, "runs it every "+For(time.Duration(j.every)*time.Second))
	case j.cal:
		how = append(how, "runs it on a schedule")
	case j.watch:
		how = append(how, "runs it when files change")
	case j.demand:
		how = append(how, "runs it when asked for")
	}
	if len(how) > 0 {
		return what + " (and " + strings.Join(how, ", ") + ")"
	}
	return what
}

// sign is one warning sign, with its weight.
type sign struct {
	weight int
	text   string
}

// interpreters run a script or code given to them.
var interpreters = []string{"sh", "bash", "zsh", "dash", "ksh", "csh", "tcsh", "fish", "python", "python2", "python3", "perl", "ruby", "osascript", "node", "php", "tclsh", "pwsh", "bun", "deno"}

// fetches are signs that a command fetches code and runs it, or hides what
// it runs.
var fetches = regexp.MustCompile(`(?i)\b(curl|wget|ncat|socat)\b|\bnc\s+-|base64\s+(-d|-D|--decode)|/dev/tcp/|\beval\b|\bxxd\s+-r`)

// commonDots are hidden folders in the home folder that well-known tools
// install programs in.
var commonDots = []string{".local", ".cargo", ".rustup", ".nvm", ".volta", ".bun", ".deno", ".pyenv", ".rbenv", ".asdf", ".orbstack", ".docker", ".colima", ".sdkman", ".dotnet", ".ghcup", ".nix-profile", ".npm-global", ".gem", ".jenv", ".fnm"}

// tempDirs hold what may be gone tomorrow, or what any user can write:
// no place for a program that starts at login.
var tempDirs = []string{"/tmp/", "/private/tmp/", "/var/tmp/", "/private/var/tmp/", "/var/folders/", "/private/var/folders/", "/Users/Shared/"}

// jobSigns are the warning signs of a job whose program is prog.
func jobSigns(ctx context.Context, r Runner, j job, prog string, in startupInput, sigs map[string]signature) []sign {
	var signs []sign
	code := prog // the file that holds the code: the script, for an interpreter that runs one
	script, inline := false, ""
	if slices.Contains(interpreters, filepath.Base(prog)) && len(j.args) > 1 {
		for i, a := range j.args[1:] {
			if a == "-c" || a == "-e" {
				inline = strings.Join(j.args[i+2:], " ")
				break
			}
			if !strings.HasPrefix(a, "-") {
				code, script = resolveProgram(a, in.Root), true
				break
			}
		}
		switch {
		case inline != "":
			signs = append(signs, sign{2, fmt.Sprintf("It runs code written into its plist, with %s: %s", filepath.Base(prog), cut(inline, 120))})
		case script:
			signs = append(signs, sign{1, fmt.Sprintf("It runs a script (%s), which can't be signed.", tildePath(code, in.Home))})
		}
	}
	if fetches.MatchString(strings.Join(j.args, " ")) {
		signs = append(signs, sign{3, "Its command fetches something from elsewhere, or decodes what it runs."})
	}
	real := code
	if p, err := filepath.EvalSymlinks(code); err == nil {
		real = p
	}
	// Where it runs from, as on the Mac: a test's root is left out.
	at, home := in.onMac(real), in.onMac(in.Home)
	for _, d := range tempDirs {
		if strings.HasPrefix(at, d) {
			signs = append(signs, sign{3, fmt.Sprintf("It runs from %s, a folder for passing files, not for programs that start by themselves.", strings.TrimSuffix(d, "/"))})
			break
		}
	}
	for _, d := range []string{"Downloads", "Library/Caches"} {
		if strings.HasPrefix(at, filepath.Join(home, d)+"/") {
			signs = append(signs, sign{3, fmt.Sprintf("It runs from ~/%s, not a place for programs that start by themselves.", d)})
		}
	}
	if h := hiddenPart(at, home); h != "" {
		w := 2
		if strings.HasPrefix(at, filepath.Join(home, "Library")+"/") || filepath.Base(at) == h {
			w = 3
		}
		signs = append(signs, sign{w, fmt.Sprintf("It runs from a hidden place (%s).", tildePath(at, home))})
	}
	// A program's own signature says who made it; a script or code in the
	// plist has none, which its sign above says.
	apple := false
	if !script && inline == "" {
		s, ok := sigs[real]
		if !ok {
			s = assess(ctx, r, real)
			sigs[real] = s
		}
		signs = append(signs, s.signs()...)
		apple = s.apple
	}
	if strings.HasPrefix(j.label, "com.apple.") && !apple {
		signs = append(signs, sign{3, "Its label says it is Apple's, but what it runs is not signed by Apple."})
	}
	if j.label != "" && strings.TrimSuffix(filepath.Base(j.plist), ".plist") != j.label {
		signs = append(signs, sign{1, fmt.Sprintf("Its file is named %s, but it calls itself %s.", filepath.Base(j.plist), j.label)})
	}
	if writableByAll(j.plist) || writableByAll(real) {
		signs = append(signs, sign{2, "Anyone on this Mac can change it, or what it runs."})
	}
	if fi, err := os.Stat(j.plist); err == nil && in.Now.Sub(fi.ModTime()) < 7*24*time.Hour && len(signs) > 0 {
		signs = append(signs, sign{1, "It was added or changed in the last week, on " + fi.ModTime().Format("2 Jan") + "."})
	}
	return signs
}

// hiddenPart is the first hidden folder or file in path that no
// well-known tool uses; "" for none.
func hiddenPart(path, home string) string {
	rel := path
	if strings.HasPrefix(path, home+"/") {
		rel = strings.TrimPrefix(path, home+"/")
		if first, _, _ := strings.Cut(rel, "/"); slices.Contains(commonDots, first) {
			return ""
		}
	}
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return part
		}
	}
	return ""
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// resolveProgram finds a job's program: a path as it is, or a bare name in
// the folders launchd looks in.
func resolveProgram(p, root string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Join(root, p)
	}
	for _, d := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		if c := filepath.Join(root, d, p); exists(c) {
			return c
		}
	}
	return p
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func writableByAll(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().Perm()&0o002 != 0
}

func errorsIsPermission(err error) bool {
	return err != nil && (os.IsPermission(err) || strings.Contains(err.Error(), "permission denied"))
}

// knownTo reports whether the person said a job is theirs: its label, its
// plist or its program is on their list.
func knownTo(known []string, label, plist, program string) bool {
	for _, k := range known {
		k = strings.TrimSpace(k)
		if k != "" && (k == label || k == plist || k == program) {
			return true
		}
	}
	return false
}

// launchOffFix turns a job of the person's own off: launchctl bootout
// stops it now, and disable keeps it from starting again. Nothing is
// deleted.
func launchOffFix(j job, in startupInput) *Fix {
	label := j.label
	if label == "" {
		return nil
	}
	target := fmt.Sprintf("gui/%d/%s", in.UID, label)
	return &Fix{
		Label: "Turn off " + label,
		Run:   []string{command("launchctl", "bootout", target), command("launchctl", "disable", target)},
		About: "Stops it now and keeps it from starting again. Its file stays where it is.",
		Undo:  command("launchctl", "enable", target) + " && " + command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", in.UID), tildePath(j.plist, in.Home)),
		kind:  fixLaunchOff, path: j.plist, ident: label,
	}
}

// signature is what codesign and Gatekeeper say of a program.
type signature struct {
	unknown   bool   // only an administrator can read it, so it can't be told
	signed    bool   // it carries a signature
	adhoc     bool   // an ad-hoc one, which names no one
	broken    string // why the signature does not check out; "" when it does
	authority string // who signed it, such as Developer ID Application: Example (TEAM)
	apple     bool   // Apple's own code
	checked   bool   // Gatekeeper assessed the app it is part of
	source    string // what Gatekeeper calls that app: Notarized Developer ID, Apple System, Mac App Store, Unnotarized Developer ID…
	accepted  bool   // Gatekeeper lets that app open
}

// assess asks codesign who signed the program at path and whether the
// signature checks out, and, for a program inside an app, Gatekeeper
// (spctl -a -t exec) whether the app is notarized. A program outside an
// app is judged by its signature alone: whether it is notarized can only
// be told by asking Apple, which he does not.
func assess(ctx context.Context, r Runner, path string) signature {
	var s signature
	_, errb, _ := r.Run(ctx, "/usr/bin/codesign", "-dv", "--verbose=2", path)
	if unreadable(string(errb)) {
		s.unknown = true
		return s
	}
	s.signed, s.adhoc, s.authority = parseCodesign(string(errb))
	if !s.signed {
		return s
	}
	if _, errb, err := r.Run(ctx, "/usr/bin/codesign", "--verify", path); err != nil {
		if unreadable(string(errb)) {
			s.unknown = true
			return s
		}
		s.broken = strings.TrimSpace(strings.TrimPrefix(firstLine(string(errb)), path+":"))
		if s.broken == "" {
			s.broken = "it does not check out"
		}
	}
	s.apple = s.authority == "Software Signing"
	if app := appOf(path); app != "" {
		out, errb, _ := r.Run(ctx, "/usr/sbin/spctl", "-a", "-vv", "-t", "exec", app)
		s.checked = true
		s.accepted, s.source = parseSpctl(string(out) + string(errb))
		s.apple = s.apple || s.source == "Apple System"
	}
	return s
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// parseCodesign reads what `codesign -dv --verbose=2` writes to stderr.
func parseCodesign(out string) (signed, adhoc bool, authority string) {
	if strings.Contains(out, "not signed at all") || strings.TrimSpace(out) == "" {
		return false, false, ""
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "Signature=adhoc":
			adhoc = true
		case strings.HasPrefix(line, "Authority=") && authority == "":
			authority = strings.TrimPrefix(line, "Authority=")
		}
	}
	return true, adhoc, authority
}

// parseSpctl reads what `spctl -a -vv` writes: accepted or rejected, then
// source= and origin=.
func parseSpctl(out string) (accepted bool, source string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasSuffix(line, ": accepted"):
			accepted = true
		case strings.HasPrefix(line, "source="):
			source = strings.TrimPrefix(line, "source=")
		}
	}
	return accepted, source
}

// unreadable reports whether codesign could not read a program, which
// says nothing of its signature.
func unreadable(out string) bool {
	return strings.Contains(out, "Permission denied") || strings.Contains(out, "Operation not permitted")
}

// signs are the warning signs in a signature.
func (s signature) signs() []sign {
	switch {
	case s.unknown:
		return nil
	case !s.signed:
		return []sign{{2, "Its program is not signed, so nothing says who made it."}}
	case s.broken != "":
		return []sign{{3, "Its program's signature does not check out (" + s.broken + "): it may have been changed since it was signed."}}
	case s.adhoc:
		return []sign{{1, "Its program is signed by no one in particular (an ad-hoc signature), as programs built on this Mac and Homebrew's are."}}
	case s.checked && s.source == "Unnotarized Developer ID":
		return []sign{{1, fmt.Sprintf("Its app is signed (%s) but not notarized by Apple.", s.authority)}}
	case s.checked && !s.accepted && !s.apple && s.source != "Mac App Store":
		return []sign{{1, fmt.Sprintf("Gatekeeper would not let its app open (%s).", strings.TrimSpace(s.authority+" "+s.source))}}
	}
	return nil
}

// cronFindings weighs the person's crontab: each job with warning signs.
func cronFindings(ctx context.Context, r Runner, in startupInput) (fs []Finding, fine []string) {
	if in.Root != "" {
		return nil, nil
	}
	out, _, err := r.Run(ctx, "/usr/bin/crontab", "-l")
	if err != nil {
		return nil, nil // none
	}
	jobs := cronJobs(string(out))
	n := 0
	for _, line := range jobs {
		var ev []string
		if fetches.MatchString(line) {
			ev = append(ev, "It fetches something from elsewhere, or decodes what it runs.")
		}
		for _, d := range tempDirs {
			if strings.Contains(line, d) {
				ev = append(ev, "It runs something in "+strings.TrimSuffix(d, "/")+", a folder for passing files.")
				break
			}
		}
		if len(ev) == 0 {
			n++
			continue
		}
		fs = append(fs, Finding{ID: "cron:" + strconv.Itoa(len(fs)), Area: AreaStartup, Level: Warn,
			Text: "A job in your crontab looks suspicious: " + cut(line, 160), Evidence: ev,
			Sure:   "Not sure: a crontab of your own may do this on purpose.",
			Advice: "If you didn't add it, remove it with `crontab -e`, and look at what it runs.", Key: line})
	}
	if len(jobs) > 0 && n > 0 {
		fine = append(fine, fmt.Sprintf("Your crontab runs %d jobs with no warning signs.", n))
	}
	return fs, fine
}

// cronJobs are the jobs of a crontab: its lines but blanks, comments and
// settings.
func cronJobs(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, _, ok := strings.Cut(line, "="); ok && !strings.ContainsAny(k, " \t*") {
			continue // a setting, such as PATH=…
		}
		out = append(out, line)
	}
	return out
}

// hookFindings names login and logout hooks: scripts loginwindow runs as
// root at each login or logout, an old way to start things that malware
// has used too.
func hookFindings(ctx context.Context, r Runner, in startupInput) []Finding {
	var fs []Finding
	for _, p := range []string{filepath.Join(in.Home, "Library/Preferences/com.apple.loginwindow.plist"), filepath.Join(in.Root, "/Library/Preferences/com.apple.loginwindow.plist")} {
		m, err := readPlistFile(ctx, r, p)
		if err != nil {
			continue
		}
		for _, k := range []string{"LoginHook", "LogoutHook"} {
			script := plistString(m, k)
			if script == "" {
				continue
			}
			f := Finding{ID: "hook:" + k + ":" + script, Area: AreaStartup, Level: Note, Key: script,
				Text:   fmt.Sprintf("A %s runs %s with the system's rights.", strings.ToLower(strings.TrimSuffix(k, "Hook"))+" hook", tildePath(script, in.Home)),
				Advice: "Login hooks are an old way to start things, which macOS keeps only for old software, and which malware has used. If you don't know it, ask whoever looks after your Mac."}
			for _, d := range tempDirs {
				if strings.HasPrefix(script, d) {
					f.Level = Warn
					f.Evidence = append(f.Evidence, "It runs from "+strings.TrimSuffix(d, "/")+", a folder for passing files.")
				}
			}
			if hiddenPart(script, in.Home) != "" {
				f.Level = Warn
				f.Evidence = append(f.Evidence, "It runs from a hidden place.")
			}
			if f.Level == Warn {
				f.Sure = "Not sure, but these are signs of something that does not want to be seen."
			}
			fs = append(fs, f)
		}
	}
	return fs
}
