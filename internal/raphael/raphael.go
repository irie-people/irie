// Package raphael is Raphael's health check (DESIGN §4.11): he looks over
// the computer Irie runs on and says, in plain words with advice, what
// slows it down, fills it up or may have got in. He finds processes that
// run hot for a long time, memory hogs and swap, a disk nearly full and the
// biggest caches that are safe to clear, login items that are unsigned, not
// notarized or look like known kinds of suspicious persistence, and the
// state of macOS's own protections.
//
// He only looks. A Finding may carry a Fix, which shows first what it will
// run; the core runs one only on the person's OK, one at a time (Do). He
// never deletes the person's own files: an app's cache goes to the Trash,
// where it can be put back, and a tool's cache is cleared by the tool. For
// what may be malware he gives his evidence and how sure he is, never a
// certainty.
//
// Each look runs one small tool (ps, du, codesign, spctl, plutil and the
// like) through a Runner, which a test replaces, and parses what it prints.
package raphael

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/proc"
)

// The levels of a finding.
const (
	OK   = "ok"   // well
	Note = "note" // worth a look, with advice
	Warn = "warn" // needs care
)

// The areas he looks at, in the order he reports them.
const (
	AreaProcesses  = "processes"
	AreaMemory     = "memory"
	AreaDisk       = "disk"
	AreaCaches     = "caches"
	AreaStartup    = "startup"
	AreaProtection = "protection"
)

// areaOrder is the order areas are reported in.
var areaOrder = []string{AreaDisk, AreaMemory, AreaProcesses, AreaProtection, AreaStartup, AreaCaches}

// Finding is one thing Raphael found.
type Finding struct {
	ID       string   `json:"id"`                 // the same from one look to the next, such as cache:go-build
	Area     string   `json:"area"`               // one of the Area constants
	Level    string   `json:"level"`              // OK, Note or Warn
	Text     string   `json:"text"`               // what he found, in a line
	Advice   string   `json:"advice,omitempty"`   // what to do about it
	Evidence []string `json:"evidence,omitempty"` // what he saw, a line each, for something that may have got in
	Sure     string   `json:"sure,omitempty"`     // how sure he is of that, in words; never certain
	Size     int64    `json:"size,omitempty"`     // the bytes it takes, or a fix would free
	Fix      *Fix     `json:"fix,omitempty"`      // what he can do about it, on the person's OK
	// Key is what the person can mark as theirs (arch.raphael.known), so
	// that he no longer flags it: a startup item's label. "" for none.
	Key string `json:"key,omitempty"`
}

// Fix is what Raphael can do about a finding, once the person says OK.
type Fix struct {
	Label string   `json:"label"`          // the button, such as "Clear Go's build cache"
	Run   []string `json:"run"`            // what he will run, a command a line, as the person reads it
	About string   `json:"about"`          // what it does and what it frees, in a line or two
	Undo  string   `json:"undo,omitempty"` // how to undo it; "" when there is nothing to undo

	kind  string // the fix's kind, one of the fix constants (fix.go)
	path  string // the folder to put in the Trash, or the plist of a login item
	pid   int    // the process to stop
	start int64  // when it started, so that a pid used again is never touched
	ident string // the launch agent's label, or the bundle id of the app whose cache it is
	tool  string // the tool that clears its own cache, by its full path
	busy  []string
}

// fixJSON is a Fix as the core keeps it between its runs (raphael.json),
// with what it acts on: a later core checks it all again before it runs.
type fixJSON struct {
	Label string   `json:"label"`
	Run   []string `json:"run"`
	About string   `json:"about"`
	Undo  string   `json:"undo,omitempty"`
	Kind  string   `json:"kind"`
	Path  string   `json:"path,omitempty"`
	PID   int      `json:"pid,omitempty"`
	Start int64    `json:"start,omitempty"`
	Ident string   `json:"ident,omitempty"`
	Tool  string   `json:"tool,omitempty"`
	Busy  []string `json:"busy,omitempty"`
}

// MarshalJSON writes the fix with what it acts on.
func (f Fix) MarshalJSON() ([]byte, error) {
	return json.Marshal(fixJSON{Label: f.Label, Run: f.Run, About: f.About, Undo: f.Undo, Kind: f.kind, Path: f.path, PID: f.pid, Start: f.start, Ident: f.ident, Tool: f.tool, Busy: f.busy})
}

// UnmarshalJSON reads a fix MarshalJSON wrote.
func (f *Fix) UnmarshalJSON(b []byte) error {
	var j fixJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*f = Fix{Label: j.Label, Run: j.Run, About: j.About, Undo: j.Undo, kind: j.Kind, path: j.Path, pid: j.PID, start: j.Start, ident: j.Ident, tool: j.Tool, busy: j.Busy}
	return nil
}

// Report is what one look found.
type Report struct {
	At       time.Time     `json:"at"`
	Took     time.Duration `json:"took"`
	Findings []Finding     `json:"findings"` // what needs care first, then what is worth a look
	Fine     []string      `json:"fine"`     // what is well, a line each
}

// Runner runs the tools Raphael reads, in the background state where the
// system has one, so that a look never takes the person's focus's CPU or
// disk (Gabriel's work, DESIGN §4.11).
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// Exec is the Runner that runs the tools for real.
type Exec struct{}

// Run runs name with args, and puts its process in the background state
// as it starts, which its children inherit.
func (Exec) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(cmd.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ANALYTICS=1", "HOMEBREW_NO_ENV_HINTS=1", "HOMEBREW_NO_INSTALL_CLEANUP=1", "NO_COLOR=1")
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	if proc.CanBackground {
		proc.SetBackground(cmd.Process.Pid, true)
	}
	err := cmd.Wait()
	return []byte(out.String()), []byte(errb.String()), err
}

// sortFindings puts what needs care first, then what is worth a look, and
// within each level the areas in their order, then the biggest first.
func sortFindings(fs []Finding) {
	rank := map[string]int{Warn: 0, Note: 1, OK: 2}
	slices.SortStableFunc(fs, func(a, b Finding) int {
		if d := rank[a.Level] - rank[b.Level]; d != 0 {
			return d
		}
		if d := slices.Index(areaOrder, a.Area) - slices.Index(areaOrder, b.Area); d != 0 {
			return d
		}
		switch {
		case a.Size > b.Size:
			return -1
		case a.Size < b.Size:
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// Size is n bytes in words, as Finder counts them: 1.2 GB, 640 MB, 12 KB.
func Size(n int64) string {
	const k = 1000
	switch {
	case n >= k*k*k*k:
		return trim(float64(n)/(k*k*k*k)) + " TB"
	case n >= k*k*k:
		return trim(float64(n)/(k*k*k)) + " GB"
	case n >= k*k:
		return fmt.Sprintf("%.0f MB", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.0f KB", float64(n)/k)
	}
	return fmt.Sprintf("%d bytes", n)
}

// MemSize is n bytes of memory in words, as macOS counts memory, in powers
// of 1024: 18 GB of RAM, 512 MB.
func MemSize(n int64) string {
	switch {
	case n >= 1<<30:
		return trim(float64(n)/(1<<30)) + " GB"
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
}

// trim is f with one decimal, or none when it is whole or large.
func trim(f float64) string {
	if f >= 100 {
		return fmt.Sprintf("%.0f", f)
	}
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(s, ".0")
}

// For is a duration in words, such as "3 hours" or "25 minutes".
func For(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	case d >= time.Hour:
		return "an hour"
	case d >= 2*time.Minute:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	return "a minute"
}

// quoteArg is a shell word as the person would type it: a path under the
// home folder keeps its ~/ outside the quotes, so that it still names it.
func quoteArg(s string) string {
	if rest, ok := strings.CutPrefix(s, "~/"); ok {
		if rest == "" {
			return s
		}
		return "~/" + quoteArg(rest)
	}
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// command is a command line as the person would type it.
func command(args ...string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = quoteArg(a)
	}
	return strings.Join(q, " ")
}

// tildePath is path with the home folder written ~.
func tildePath(path, home string) string {
	if home != "" && (path == home || strings.HasPrefix(path, home+"/")) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
