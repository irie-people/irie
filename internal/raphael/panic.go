package raphael

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
)

// A kernel panic (DESIGN §4.11). When macOS's kernel panics, the Mac
// restarts, and after the boot macOS writes a report in PanicDir:
// panic-*.panic, a line of JSON about the report, then the report as JSON,
// with the panic string and a stackshot of every process; and
// ResetCounter-*.diag, which says the Mac reset. Raffi reads the newest,
// and says in plain words what happened: the panic string, whether the
// memory compressor said the swap was out of disk ("LOW swap space"), and
// what took the most memory, Claude Code's agents together.

// PanicDir is where macOS keeps its panic and reset reports.
const PanicDir = "/Library/Logs/DiagnosticReports"

// Panic is what the newest restart report says.
type Panic struct {
	At        time.Time   `json:"at"`
	File      string      `json:"file"`                // the report, by its path
	Reason    string      `json:"reason,omitempty"`    // the panic string's first line, after where it panicked; "" for a reset without a panic report
	Words     string      `json:"words"`               // what happened, in plain words
	LowSwap   bool        `json:"low_swap,omitempty"`  // the compressor said the swap was low on disk
	Swapfiles int         `json:"swapfiles,omitempty"` // how many swap files there were
	Top       []PanicUser `json:"top,omitempty"`       // what took the most memory, the most first
}

// PanicUser is a program that took memory as the kernel panicked.
type PanicUser struct {
	Name  string `json:"name"`
	Count int    `json:"count"` // its processes
	Bytes int64  `json:"bytes"` // their resident memory, all told
}

// NewestRestart reads the newest restart report in dir: the newest panic
// report, or a reset with none near it. ok is false when there is none.
func NewestRestart(dir string) (p Panic, ok bool, err error) {
	return ReadRestart(RestartReports(dir))
}

// RestartReports finds the newest panic report and the newest reset
// report in dir, without reading them; "" for none.
func RestartReports(dir string) (panicFile, reset string) {
	return newest(dir, "panic-*.panic"), newest(dir, "ResetCounter-*.diag")
}

// ReadRestart reads the restart that the newest panic report and the
// newest reset report tell of: the panic, or a reset with no panic near
// it. ok is false when there is neither.
func ReadRestart(panicFile, reset string) (p Panic, ok bool, err error) {
	if panicFile != "" {
		if p, err = ReadPanic(panicFile); err != nil {
			return Panic{}, false, err
		}
	}
	if reset != "" {
		at, err := reportTime(reset)
		// A reset after a panic is that panic's; one well after, its own.
		if err == nil && (panicFile == "" || at.Sub(p.At) > 5*time.Minute) {
			return Panic{At: at, File: reset, Words: "it reset without a kernel panic report, as a power cut, a hang the watchdog ended or a fault of its hardware does"}, true, nil
		}
	}
	return p, panicFile != "", nil
}

// newest is the newest file in dir whose name matches pattern, by its
// time; "" for none.
func newest(dir, pattern string) string {
	paths, _ := filepath.Glob(filepath.Join(dir, pattern))
	best, bestAt := "", time.Time{}
	for _, p := range paths {
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if fi.ModTime().After(bestAt) || fi.ModTime().Equal(bestAt) && p > best {
			best, bestAt = p, fi.ModTime()
		}
	}
	return best
}

// reportHead is the line of JSON a report of macOS's starts with.
type reportHead struct {
	BugType   string `json:"bug_type"`
	Timestamp string `json:"timestamp"`
}

// reportTimeLayout is how a report's head writes its time.
const reportTimeLayout = "2006-01-02 15:04:05.00 -0700"

// reportTime is when a report says it was written, or its file's time.
func reportTime(path string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()
	line, _ := bufio.NewReader(f).ReadString('\n')
	var h reportHead
	if json.Unmarshal([]byte(line), &h) == nil {
		if t, err := time.Parse(reportTimeLayout, h.Timestamp); err == nil {
			return t, nil
		}
	}
	fi, err := f.Stat()
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}

// ReadPanic reads a panic report: its head, then the report.
func ReadPanic(path string) (Panic, error) {
	f, err := os.Open(path)
	if err != nil {
		return Panic{}, err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 1<<16)
	line, err := br.ReadString('\n')
	if err != nil {
		return Panic{}, fmt.Errorf("%s: no report after its first line", path)
	}
	var h reportHead
	if err := json.Unmarshal([]byte(line), &h); err != nil {
		return Panic{}, fmt.Errorf("%s: its first line: %v", path, err)
	}
	var body struct {
		PanicString  string `json:"panicString"`
		ProcessByPid map[string]struct {
			Procname string `json:"procname"`
			Resident int64  `json:"residentMemoryBytes"`
		} `json:"processByPid"`
	}
	if err := json.NewDecoder(br).Decode(&body); err != nil {
		return Panic{}, fmt.Errorf("%s: %v", path, err)
	}
	p := Panic{File: path}
	if t, err := time.Parse(reportTimeLayout, h.Timestamp); err == nil {
		p.At = t
	} else if fi, err := f.Stat(); err == nil {
		p.At = fi.ModTime()
	}
	p.Reason = panicReason(body.PanicString)
	for _, l := range strings.Split(body.PanicString, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "Compressor Info:") {
			p.LowSwap = strings.Contains(l, "LOW swap space")
			if m := swapfilesRe.FindStringSubmatch(l); m != nil {
				p.Swapfiles, _ = strconv.Atoi(m[1])
			}
		}
	}
	p.Words = panicWords(p.Reason)
	users := map[string]*PanicUser{}
	for _, q := range body.ProcessByPid {
		name := q.Procname
		if claudeCode(name) {
			name = "Claude Code"
		}
		if name == "" {
			continue
		}
		u := users[name]
		if u == nil {
			u = &PanicUser{Name: name}
			users[name] = u
		}
		u.Count++
		u.Bytes += q.Resident
	}
	for _, u := range users {
		p.Top = append(p.Top, *u)
	}
	slices.SortFunc(p.Top, func(a, b PanicUser) int {
		switch {
		case a.Bytes > b.Bytes:
			return -1
		case a.Bytes < b.Bytes:
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	p.Top = p.Top[:min(len(p.Top), 3)]
	return p, nil
}

var (
	// panicHead is where a panic string says it panicked: panic(cpu N
	// caller 0x…): before its reason.
	panicHead   = regexp.MustCompile(`^panic\([^)]*\):\s*`)
	swapfilesRe = regexp.MustCompile(`with (\d+) swapfiles`)
	watchdogRe  = regexp.MustCompile(`watchdog timeout: no checkins from watchdogd in (\d+) seconds`)
	versionName = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

// panicReason is the panic string's first line, after where it panicked.
func panicReason(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return cut(strings.TrimSpace(panicHead.ReplaceAllString(line, "")), 200)
}

// panicWords says a panic's reason in plain words.
func panicWords(reason string) string {
	if m := watchdogRe.FindStringSubmatch(reason); m != nil {
		return fmt.Sprintf("macOS stopped answering for %s seconds, so its watchdog restarted it", m[1])
	}
	if reason == "" {
		return "its kernel panicked"
	}
	r, _, _ := strings.Cut(reason, " (")
	return "its kernel stopped: " + cut(r, 120)
}

// claudeCode reports whether a stackshot's process name is Claude Code's:
// its native build runs from a file named by its version, such as 2.1.330.
func claudeCode(name string) bool { return name == "claude" || versionName.MatchString(name) }

// Tell is the restart in a sentence, then its lines: what happened and
// what took the most memory.
func (p Panic) Tell() (text string, lines []string) {
	when := p.At.Local().Format("15:04 on 2 Jan")
	if p.Reason == "" {
		return fmt.Sprintf("Your Mac restarted at %s: %s.", when, p.Words), nil
	}
	text = fmt.Sprintf("Your Mac crashed and restarted at %s: %s.", when, p.Words)
	if p.LowSwap {
		lines = append(lines, "The swap had run out of disk: the disk was too full for it to grow.")
	}
	if len(p.Top) > 0 {
		lines = append(lines, "Most memory: "+p.TopWords()+".")
	}
	return text, lines
}

// TopWords names what took the most memory: "Claude Code (57) 22 GB,
// llama-server 7.1 GB".
func (p Panic) TopWords() string {
	var parts []string
	for _, u := range p.Top {
		name := u.Name
		if u.Count > 1 {
			name += fmt.Sprintf(" (%d)", u.Count)
		}
		parts = append(parts, name+" "+MemSize(u.Bytes))
	}
	return strings.Join(parts, ", ")
}

// panicKeep is how long a restart stays among his findings.
const panicKeep = 3 * 24 * time.Hour

// PanicFinding is the restart as a finding, for three days after it: one
// that needs care for the first day.
func PanicFinding(p Panic, now time.Time) (Finding, bool) {
	age := now.Sub(p.At)
	if p.File == "" || age > panicKeep || age < 0 {
		return Finding{}, false
	}
	text, lines := p.Tell()
	f := Finding{ID: "panic:" + filepath.Base(p.File), Area: AreaPanic, Level: Note, Text: text}
	if age < 24*time.Hour {
		f.Level = Warn
	}
	f.Evidence = append(lines, "The report: "+p.File)
	if p.Reason != "" {
		f.Evidence = append(f.Evidence, "The panic: "+p.Reason)
	}
	switch {
	case p.LowSwap:
		f.Advice = "Memory ran short and the swap couldn't grow: " + brand.Raffi + " now keeps room free on the disk for it, clearing caches by " + brand.Him + "self, and rests idle cards while memory is short. Fewer agents and local models at once help too."
	case p.Reason == "":
		f.Advice = "If it happens again, note what ran: " + brand.Raffi + " says so here after each restart."
	default:
		f.Advice = "A panic that comes again with the same words is worth reporting to Apple, with the report."
	}
	return f, true
}
