package raphael

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Proc is one process, as ps sees it.
type Proc struct {
	PID, PPID, UID int
	CPU            time.Duration // the CPU time it has used, all told
	RSS            int64         // its resident memory, in bytes
	Path           string        // its executable, by its full path when ps knows it
}

// psArgs is what ps prints for Procs: four numbers, the CPU time, then the
// executable, which may hold spaces.
var psArgs = []string{"-Ao", "pid=,ppid=,uid=,rss=,time=,comm="}

// Procs reads every process with ps.
func Procs(ctx context.Context, r Runner) ([]Proc, error) {
	out, _, err := r.Run(ctx, "/bin/ps", psArgs...)
	if err != nil {
		return nil, err
	}
	return ParsePS(string(out)), nil
}

// ParsePS reads the output of ps with psArgs.
func ParsePS(out string) []Proc {
	var ps []Proc
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		uid, e3 := strconv.Atoi(f[2])
		kb, e4 := strconv.ParseInt(f[3], 10, 64)
		cpu, ok := parseCPUTime(f[4])
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || !ok {
			continue
		}
		// The executable is the rest of the line, spaces and all.
		rest := line
		for i := 0; i < 5; i++ {
			rest = strings.TrimLeft(rest, " \t")
			rest = rest[strings.IndexAny(rest, " \t"):]
		}
		ps = append(ps, Proc{PID: pid, PPID: ppid, UID: uid, CPU: cpu, RSS: kb << 10, Path: strings.TrimSpace(rest)})
	}
	return ps
}

// parseCPUTime reads ps's CPU time: [dd-][[hh:]mm:]ss[.ss], where macOS
// lets the minutes run past 59 (497:24.45).
func parseCPUTime(s string) (time.Duration, bool) {
	var d time.Duration
	if days, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, false
		}
		d, s = time.Duration(n)*24*time.Hour, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, false
	}
	sec, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, false
	}
	d += time.Duration(math.Round(sec * float64(time.Second)))
	unit := time.Minute
	for i := len(parts) - 2; i >= 0; i-- {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return 0, false
		}
		d += time.Duration(n) * unit
		unit = time.Hour
	}
	return d, true
}

// The sampler's thresholds: a process runs hot while it uses hotShare of a
// CPU or more, and is a runaway once it has done so for hotFor.
const (
	hotShare = 0.8
	hotFor   = 10 * time.Minute
	// sampleGap is the longest gap between two samples that still tells
	// how busy a process was between them: a longer one, such as the
	// machine asleep, starts its count again.
	sampleGap = 5 * time.Minute
)

// Sampler keeps each process's CPU time from one sample to the next, so
// that a process that runs hot for a long time can be told from one that
// is busy for a moment. The core samples once a minute.
type Sampler struct {
	seen map[int]track
}

// track is one process from sample to sample.
type track struct {
	start    int64         // when it started, as the process table says, to tell a pid used again
	cpu      time.Duration // its CPU time at the last sample
	at       time.Time     // when that was
	hotSince time.Time     // since when it has run hot; zero while it does not
	hotCPU   time.Duration // its CPU time then
}

// Hot is a process that has run hot for a long time.
type Hot struct {
	Proc
	Start int64         // when it started, as the process table says
	Share float64       // the share of a CPU it used on average while hot, such as 0.97
	For   time.Duration // how long it has run hot
}

// Add takes a sample of every process at now. starts gives each pid's
// start time from the process table, so a pid used again counts afresh;
// a pid it lacks has started too lately to say.
func (s *Sampler) Add(now time.Time, ps []Proc, starts map[int]int64) {
	if s.seen == nil {
		s.seen = map[int]track{}
	}
	next := make(map[int]track, len(ps))
	for _, p := range ps {
		start, ok := starts[p.PID]
		if !ok {
			continue
		}
		t, was := s.seen[p.PID]
		if !was || t.start != start || now.Sub(t.at) > sampleGap || now.Sub(t.at) <= 0 || p.CPU < t.cpu {
			next[p.PID] = track{start: start, cpu: p.CPU, at: now}
			continue
		}
		share := float64(p.CPU-t.cpu) / float64(now.Sub(t.at))
		if share >= hotShare {
			if t.hotSince.IsZero() {
				t.hotSince, t.hotCPU = t.at, t.cpu
			}
		} else {
			t.hotSince, t.hotCPU = time.Time{}, 0
		}
		t.cpu, t.at = p.CPU, now
		next[p.PID] = t
	}
	s.seen = next
}

// Hot returns the processes of ps that have run hot for hotFor or longer,
// by the samples so far, the hottest first.
func (s *Sampler) Hot(ps []Proc) []Hot {
	var out []Hot
	for _, p := range ps {
		t, ok := s.seen[p.PID]
		if !ok || t.hotSince.IsZero() {
			continue
		}
		long := t.at.Sub(t.hotSince)
		if long < hotFor {
			continue
		}
		out = append(out, Hot{Proc: p, Start: t.start, Share: float64(t.cpu-t.hotCPU) / float64(long), For: long})
	}
	slices.SortFunc(out, func(a, b Hot) int {
		switch {
		case a.Share > b.Share:
			return -1
		case a.Share < b.Share:
			return 1
		}
		return a.PID - b.PID
	})
	return out
}

// Owner says whose a process is, for advice on it: a session of Irie's,
// one of Irie's own programs, or nobody's in particular.
type Owner struct {
	Session map[int]string // the processes of each session's tree, by pid, and the session's name
	Irie    map[int]bool   // Irie's own processes: the core, the sidebars, the app
}

// knownBusy is advice on macOS's own programs that can run hot for a long
// time with good reason, by name.
var knownBusy = map[string]string{
	"mds":                         "Spotlight is indexing your disk. It settles by itself once it is through.",
	"mds_stores":                  "Spotlight is indexing your disk. It settles by itself once it is through.",
	"mdworker":                    "Spotlight is reading new or changed files. It settles by itself.",
	"mdworker_shared":             "Spotlight is reading new or changed files. It settles by itself.",
	"kernel_task":                 "macOS slows the Mac down this way when it runs hot. Give it air, and close what works it hard.",
	"WindowServer":                "It draws every window. Many windows, displays or animations keep it busy; closing some helps.",
	"backupd":                     "Time Machine is backing up. It settles once the backup is through.",
	"photoanalysisd":              "Photos is looking through your library. It settles by itself.",
	"photolibraryd":               "Photos is working on your library. It settles by itself.",
	"softwareupdated":             "macOS is getting or installing an update.",
	"XProtect":                    "macOS's malware scan is running. It settles by itself.",
	"XprotectService":             "macOS's malware scan is running. It settles by itself.",
	"syspolicyd":                  "macOS is checking the apps you open.",
	"bird":                        "iCloud Drive is syncing.",
	"cloudd":                      "iCloud is syncing.",
	"fileproviderd":               "A cloud drive is syncing files.",
	"corespotlightd":              "Spotlight is indexing your apps' data. It settles by itself.",
	"mediaanalysisd":              "macOS is looking through your photos and videos. It settles by itself.",
	"com.apple.WebKit.WebContent": "A web page in Safari or another app works hard. Closing busy tabs helps.",
}

// systemProc reports whether a process belongs to the system: another
// user's, or a program in one of macOS's own folders.
func systemProc(p Proc, uid int) bool {
	if p.UID != uid {
		return true
	}
	for _, d := range []string{"/System/", "/usr/libexec/", "/usr/sbin/", "/sbin/", "/Library/Apple/"} {
		if strings.HasPrefix(p.Path, d) {
			return true
		}
	}
	return false
}

// appOf is the app a program belongs to, by the outermost .app in its
// path: such as /Applications/Safari.app for a helper inside it. "" for a
// program outside an app.
func appOf(path string) string {
	if i := strings.Index(path, ".app/"); i >= 0 {
		return path[:i+len(".app")]
	}
	return ""
}

// appName is the name an app goes by, such as Safari.
func appName(app string) string {
	return strings.TrimSuffix(filepath.Base(app), ".app")
}

// procName is a process in words: its app's name, or its program's.
func procName(p Proc) string {
	if app := appOf(p.Path); app != "" {
		name := appName(app)
		if base := filepath.Base(p.Path); base != name {
			return fmt.Sprintf("%s (%s)", name, base)
		}
		return name
	}
	return progName(p.Path)
}

// versioned is the name of a program kept by its version, such as 2.1.316.
var versioned = regexp.MustCompile(`^v?[0-9]+(\.[0-9]+)+$`)

// progName is a program's name: its file's, or for one kept in a folder of
// versions (~/.local/share/claude/versions/2.1.316), that folder's owner.
func progName(path string) string {
	base := filepath.Base(path)
	if !versioned.MatchString(base) {
		return base
	}
	dir := filepath.Dir(path)
	if filepath.Base(dir) == "versions" {
		dir = filepath.Dir(dir)
	}
	return filepath.Base(dir)
}

// hotFindings says what runs hot for a long time: a program of the
// person's own, which he can stop on their OK when it is not an app, one
// of an Irie session's, and one of macOS's own, with advice.
func hotFindings(hot []Hot, o Owner, uid int, home string) []Finding {
	var fs []Finding
	for _, h := range hot {
		name := procName(h.Proc)
		f := Finding{
			ID: fmt.Sprintf("proc:%d:%d", h.PID, h.Start), Area: AreaProcesses, Level: Note,
			Text: fmt.Sprintf("%s has used %.0f%% of a CPU for %s (pid %d).", name, h.Share*100, For(h.For), h.PID),
		}
		app := appOf(h.Path)
		switch {
		case o.Irie[h.PID]:
			f.Advice = "It is Irie's own. Restarting it is safe: your agents keep running."
		case o.Session[h.PID] != "":
			// An agent's own work: Gabriel holds it back while you work.
			continue
		case systemProc(h.Proc, uid):
			f.Advice = knownBusy[filepath.Base(h.Path)]
			if f.Advice == "" {
				f.Advice = "It is part of macOS or another user's. If it stays this busy for hours, a restart of the Mac usually settles it."
			}
		case app != "":
			f.Advice = fmt.Sprintf("Quit %s and open it again: quit it yourself, from its menu, so that it can save your work first.", appName(app))
		default:
			// A program of the person's own outside an app, such as a test
			// left running: hot for an hour, it is likely stuck.
			if h.For >= time.Hour {
				f.Level = Warn
			}
			f.Advice = "It may be stuck. If you don't know it or no longer need it, Raffi can stop it."
			f.Fix = &Fix{
				Label: "Stop " + filepath.Base(h.Path),
				Run:   []string{command("kill", "-TERM", strconv.Itoa(h.PID))},
				About: fmt.Sprintf("Asks %s (%s) to stop, as quitting it would: it may not save what it was doing.", filepath.Base(h.Path), tildePath(h.Path, home)),
				kind:  fixStop, pid: h.PID, start: h.Start, path: h.Path,
			}
		}
		fs = append(fs, f)
	}
	return fs
}
