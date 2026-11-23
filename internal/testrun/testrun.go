// Package testrun reads tests from what an agent's harness hands its hooks:
// whether a shell command runs tests or checks, whether a run passed or
// failed by what it printed and its exit code, and whether an agent's
// message says its tests pass. The jot dog uses both to tell when an
// agent's report says more than its own record shows (DESIGN §4.8). It is
// plain code, so it costs no tokens.
package testrun

import (
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// compiled is a pattern compiled on its first use rather than as any irie
// starts: these took 318 KB in every process, each hook call among them.
func compiled(p string) func() *regexp.Regexp {
	return sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(p) })
}

// Outcomes of a test run.
const (
	Passed = "passed"
	Failed = "failed"
	// Ran is a run whose outcome cannot be read: it went to the background,
	// it was cut short, or what it printed shows neither a pass nor a
	// failure, as when a filter left nothing.
	Ran = "ran"
)

// runner is a command that runs tests, at the head of a segment of a shell
// command. Its exit code is its run's outcome, unless what follows it in
// the command hides that code.
var runner = compiled(`^(?:go\s+test|cargo\s+(?:test|nextest)|swift\s+test|xcodebuild\s.*\btest\b|(?:python3?\s+-m\s+)?pytest|python3?\s+-m\s+unittest|(?:npm|pnpm|yarn)\s+(?:run\s+)?test|bun\s+test|deno\s+test|node\s+--test|(?:npx\s+|bunx\s+)?(?:jest|vitest|mocha)|(?:bundle\s+exec\s+)?rspec|mix\s+test|(?:\./gradlew|gradle)\s+test|mvn\s+test|dotnet\s+test|zig\s+build\s+test|forge\s+test|ctest|tox|make\s+(?:test|check))\b`)

// wrapper is a script that runs tests among other work, such as a repo's
// own check or install script. It counts as a run, but its exit code says
// nothing sure of its tests: an install can fail for want of a lock.
var wrapper = compiled(`^(?:(?:sh|bash|zsh)\s+)?(?:\S*/)?(?:live|[\w.-]*(?:test|check|smoke)[\w.-]*)\.sh(?:\s|$)`)

// lead is what may come before a command at the head of a segment: a
// subshell or group, a negation, a shell keyword, timing or an environment.
var lead = compiled(`^(?:[({!]\s*|(?:if|then|else|elif|do|while|until)\s+|time\s+|timeout\s+\S+\s+|nice\s+(?:-n\s*\S+\s+)?|command\s+|exec\s+|env\s+|caffeinate\s+(?:-\w+\s+)*|[A-Za-z_][A-Za-z0-9_]*=(?:'[^']*'|"[^"]*"|\S*)\s+)+`)

// harmless is a command that cannot fail, which may follow a test runner
// without taking its exit code's place.
var harmless = compiled(`^(?:echo|printf|true|:)(?:\s|$)`)

// Runs reports whether a shell command runs tests or checks.
func Runs(command string) bool {
	for _, s := range segments(command) {
		if runner().MatchString(s.cmd) || wrapper().MatchString(s.cmd) {
			return true
		}
	}
	return false
}

// failMark is a line of output that shows tests failing: Go's, cargo's,
// pytest's, Jest's and Vitest's, Bun's, XCTest's, npm's and make's, and
// this repo's tools/check.sh's.
var failMark = compiled(`(?m)^(?:--- FAIL: .*|FAIL(?:\s.*)?|panic: .*|test result: FAILED.*|error: test failed.*|FAILED .*|=+ .*\b[1-9]\d* (?:failed|errors?)\b.*|\s*Tests?:?\s+[1-9]\d* failed.*|\s*Test Files\s+[1-9]\d* failed.*|\s*[1-9]\d* fail(?:ed)?\s*|.*Executed \d+ tests?, with [1-9]\d* failures?.*|npm (?:ERR!|error) .*[Tt]est.*|make(?:\[\d+\])?: \*\*\* .*Error \d+.*|check: gofmt would change.*)$`)

// passMark is a line that shows tests passing.
var passMark = compiled(`(?m)^(?:ok\s+\S+.*|PASS|test result: ok\..*|=+ .*\b\d+ passed\b.*|\s*Tests?:?\s+\d+ passed.*|\s*\d+ pass\s*|.*Executed \d+ tests?, with 0 failures.*)$`)

// firstMark names the failing test itself, in Go's and pytest's words,
// before a package's or a summary's line.
var firstMark = compiled(`(?m)^(?:--- FAIL: .*|panic: .*|FAILED .*)$`)

// lineMax is how much of the line that shows a failure is kept, in bytes.
const lineMax = 120

// Read is what a shell command's run says of its tests: "" when the command
// runs none; else Passed, Failed or Ran, and for a failure the line of its
// output that shows it. exit is the command's exit code, or -1 when it is
// not known. Output that shows a failure is a failure, wherever the command
// sends it; otherwise a non-zero exit is one only when it is surely a test
// runner's (exitsWith).
func Read(command, output string, exit int) (outcome, line string) {
	segs := segments(command)
	runs, sure := false, false
	for i, s := range segs {
		switch {
		case runner().MatchString(s.cmd):
			runs = true
			sure = sure || exitsWith(segs, i)
		case wrapper().MatchString(s.cmd):
			runs = true
		}
	}
	if !runs {
		return "", ""
	}
	if m := firstMark().FindString(output); m != "" {
		return Failed, cut(m)
	}
	if m := failMark().FindString(output); m != "" {
		return Failed, cut(m)
	}
	switch {
	case exit > 0 && sure:
		return Failed, ""
	case passMark().MatchString(output):
		return Passed, ""
	}
	return Ran, ""
}

// exitsWith reports whether the command's exit code is the runner's at
// segs[i] whenever that runner fails: what follows it runs only after it
// succeeds (&&) and cannot fail itself. A pipe, ; or || after it puts
// another command's code in its place, as `go test | tail` and
// `go test; echo done` do.
func exitsWith(segs []segment, i int) bool {
	for j := i; j < len(segs); j++ {
		if j > i && !harmless().MatchString(segs[j].cmd) {
			return false
		}
		if segs[j].then != "" && segs[j].then != "&&" {
			return false
		}
	}
	return true
}

func cut(line string) string {
	line = strings.TrimSpace(line)
	if len(line) <= lineMax {
		return line
	}
	n := lineMax
	for n > 0 && !utf8.RuneStart(line[n]) {
		n--
	}
	return line[:n] + "…"
}

// segment is one simple command of a shell command line, and the operator
// that follows it: "&&", "||", "|", ";", "&", or "" at the end.
type segment struct {
	cmd, then string
}

// segments splits a shell command at its operators and newlines, outside
// quotes, leaving out the bodies of here-documents, whose lines are text
// rather than commands. Each segment is trimmed of what may lead a command
// (lead).
func segments(command string) []segment {
	var out []segment
	var b strings.Builder
	var heredocs []string // delimiters whose bodies start at the next newline
	flush := func(then string) {
		s := strings.TrimSpace(b.String())
		b.Reset()
		if s == "" {
			if n := len(out); n > 0 && then != "" {
				out[n-1].then = then
			}
			return
		}
		out = append(out, segment{cmd: strings.TrimSpace(lead().ReplaceAllString(s, "")), then: then})
	}
	quote := byte(0)
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case quote != 0:
			b.WriteByte(c)
			if c == '\\' && quote == '"' && i+1 < len(command) {
				i++
				b.WriteByte(command[i])
			} else if c == quote {
				quote = 0
			}
		case c == '\\' && i+1 < len(command):
			b.WriteByte(c)
			i++
			b.WriteByte(command[i])
		case c == '\'' || c == '"':
			quote = c
			b.WriteByte(c)
		case c == '<' && strings.HasPrefix(command[i:], "<<") && !strings.HasPrefix(command[i:], "<<<"):
			if d, n := heredoc(command[i+2:]); d != "" {
				heredocs = append(heredocs, d)
				i += 1 + n
				continue
			}
			b.WriteByte(c)
		case c == '\n':
			flush(";")
			for _, d := range heredocs {
				i = skipBody(command, i+1, d)
			}
			heredocs = nil
		case c == ';':
			flush(";")
		case c == '&' || c == '|':
			op := string(c)
			if i+1 < len(command) && command[i+1] == c {
				op += string(c)
				i++
			} else if c == '&' && i+1 < len(command) && command[i+1] == '>' {
				// &> sends output to a file.
				b.WriteByte(c)
				continue
			} else if c == '|' && i+1 < len(command) && command[i+1] == '&' {
				i++ // |& pipes stderr too.
			} else if c == '&' && i > 0 && (command[i-1] == '>' || command[i-1] == '<') {
				// 2>&1 duplicates a descriptor.
				b.WriteByte(c)
				continue
			}
			flush(op)
		default:
			b.WriteByte(c)
		}
	}
	flush("")
	// A newline or ; at the very end runs nothing after the last command.
	if n := len(out); n > 0 && out[n-1].then == ";" {
		out[n-1].then = ""
	}
	return out
}

// heredoc reads a here-document's delimiter after "<<": an optional "-",
// then a word, perhaps quoted. It returns the delimiter and how many bytes
// it took, or "" when there is none.
func heredoc(s string) (string, int) {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i < len(s) && (s[i] == '\'' || s[i] == '"') {
		q := s[i]
		end := strings.IndexByte(s[i+1:], q)
		if end <= 0 {
			return "", 0
		}
		return s[i+1 : i+1+end], i + 2 + end
	}
	start := i
	for i < len(s) && (s[i] == '_' || s[i] == '-' || 'a' <= s[i] && s[i] <= 'z' || 'A' <= s[i] && s[i] <= 'Z' || '0' <= s[i] && s[i] <= '9') {
		i++
	}
	if i == start {
		return "", 0
	}
	return s[start:i], i
}

// skipBody returns the index of the newline that ends a here-document's
// closing line, which starts its body at from; or the command's end when the
// body never closes.
func skipBody(command string, from int, delim string) int {
	for from < len(command) {
		end := strings.IndexByte(command[from:], '\n')
		line := command[from:]
		if end >= 0 {
			line = command[from : from+end]
		}
		if strings.TrimLeft(line, "\t") == delim {
			if end < 0 {
				return len(command)
			}
			return from + end
		}
		if end < 0 {
			return len(command)
		}
		from += end + 1
	}
	return len(command)
}

// passClaim is a sentence that says tests or checks pass.
var passClaim = compiled(`(?i)\b(?:tests?|test suite|suites?|checks?|specs?)\b[^.;\n]{0,40}?\b(?:pass|passes|passed|passing|green|succeed|succeeds|succeeded)\b|\bpass(?:es|ed|ing)?\s+(?:all\s+)?(?:of\s+)?(?:the\s+|its\s+|their\s+|my\s+|every\s+)?(?:\w+\s+){0,2}?(?:tests?|checks?)\b|\b(?:check\.sh|go test|cargo test|pytest|npm test|bun test|swift test)\b[^.;\n]{0,40}?\b(?:pass|passes|passed|passing|green|clean|ok)\b`)

// hedge is a word that takes a sentence's claim back, makes it partial or
// puts it off: one that names a failure, a negation, a condition or the
// future, an exception, or a count of some but not all.
var hedge = compiled(`(?i)\b(?:not|no|never|none|nothing|without|cannot|fail\w*|skip\w*|flak\w*|broke\w*|until|unless|once|if|when|whether|would|will|should|could|might|may|before|except|but|apart|besides|other than|yet|expected to|hope\w*)\b|n't\b|\d+\s+(?:of|out of)\s+\d+`)

// sentence splits a message into its sentences and lines.
var sentence = compiled(`[.!?](?:\s|$)|[;\n]`)

// SaysPassed reports whether a message says, plainly and in full, that tests
// or checks pass: a sentence that claims it with no word that takes it back.
func SaysPassed(msg string) bool { return Says(msg, passClaim()) }

// Says reports whether a message says, plainly and in full, what claim
// matches: a sentence that claims it with no word that takes it back, such
// as a negation, a condition, the future or an exception (hedge).
func Says(msg string, claim *regexp.Regexp) bool {
	msg = strings.ReplaceAll(msg, "’", "'")
	for _, s := range sentence().Split(msg, -1) {
		if claim.MatchString(s) && !hedge().MatchString(s) {
			return true
		}
	}
	return false
}
