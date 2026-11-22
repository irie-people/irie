package ab

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/testrun"
)

// An agent's `irie ab` runs, as its hooks see them, and what its messages
// say of speed and output: the jot dog holds a jot's word to its own last
// run (DESIGN §4.8, §4.24). Plain code, so it costs no tokens.

// Verdicts of a run, as the journal keeps them (tool.finished "ab").
const (
	Faster  = "faster"  // the same output, and new clearly faster
	Slower  = "slower"  // the same output, and new clearly slower
	Even    = "even"    // the same output, and no clear difference in time
	Differs = "differs" // new's output differs from gold's, or a build's own runs disagree
)

// invoked is `irie ab` in a shell command: the binary by its name or a
// path to it, or the session's own build in $IRIE_BIN.
var invoked = regexp.MustCompile(`(?:^|[\s;&|(/"'])(?:` + regexp.QuoteMeta(brand.Slug) + `|\$\{?` + brand.Env("BIN") + `\}?)["']?\s+ab(?:\s|$)`)

// Runs reports whether a shell command runs `irie ab`.
func Runs(command string) bool { return invoked.MatchString(command) }

// verdictLine is the line a run's report ends with when the outputs are the
// same (printAB in cmd/irie), and differsLine the error a run that differs
// exits with.
var (
	verdictLine = regexp.MustCompile(`(?m)^new is (faster|slower|neither clearly faster nor slower), with the same output\s*$`)
	differsLine = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(brand.Slug) + `: ab: (?:new's output differs from gold's|gold's own output changes|new's own output changes)[^\n]*`)
)

// Read is the verdict of an `irie ab` run, and the line of its output that
// says it, from what its command printed, in words or as --json; "" when it
// shows none, as for a run cut short or a usage error.
func Read(output string) (verdict, line string) {
	if m := verdictLine.FindStringSubmatch(output); m != nil {
		line = strings.TrimSpace(m[0])
		switch m[1] {
		case "faster":
			return Faster, line
		case "slower":
			return Slower, line
		}
		return Even, line
	}
	if m := differsLine.FindString(output); m != "" {
		return Differs, strings.TrimSpace(m)
	}
	var r struct {
		Same *bool  `json:"same"`
		Time Metric `json:"time"`
	}
	i := strings.Index(output, "{")
	if i < 0 || json.NewDecoder(strings.NewReader(output[i:])).Decode(&r) != nil || r.Same == nil {
		return "", ""
	}
	switch {
	case !*r.Same:
		return Differs, `"same": false`
	case r.Time.Change == Lower:
		return Faster, `"time": {"change": "lower"}`
	case r.Time.Change == Higher:
		return Slower, `"time": {"change": "higher"}`
	}
	return Even, `"time": {"change": "same"}`
}

// fasterClaim says a change made something faster or lighter.
var fasterClaim = regexp.MustCompile(`(?i)\b(?:faster|quicker|speed(?:s|ed)?[ -]?ups?|sped up|less (?:cpu|memory|time)|lower (?:cpu|memory|latency))\b`)

// sameClaim says the output stayed as it was.
var sameClaim = regexp.MustCompile(`(?i)\b(?:output|results?)\b[^.;\n]{0,40}?\b(?:unchanged|identical|the same|byte[- ]for[- ]byte)\b|\bsame output\b`)

// SaysFaster reports whether a message says plainly that something is now
// faster or lighter, with no word that takes it back (testrun.Says).
func SaysFaster(msg string) bool { return testrun.Says(msg, fasterClaim) }

// SaysSame reports whether a message says plainly that the output is the
// same as before.
func SaysSame(msg string) bool { return testrun.Says(msg, sameClaim) }
