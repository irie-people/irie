package council

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Markdown is the tally as a council reads it: a section for each seat,
// each number with the stretch before's beside it in brackets, and the
// earlier councils' plans at the end.
func Markdown(t Tally) string { return render(t, 1).b.String() }

// Line is the line of the tally t that watch names, a plan's watch line,
// without its "- "; "" when it names none. A line is named by its label,
// the words before its colon, and its section by the words of its heading:
// of the lines whose label watch holds as words, the longest label wins,
// then the section with the most of its words in watch, then the first.
func Line(t Tally, watch string) string {
	w := strings.ToLower(watch)
	best, score := "", 0
	for _, l := range render(t, 1).lines {
		label := strings.ToLower(l.label)
		if utf8.RuneCountInString(label) < 3 || !hasWords(w, label) {
			continue
		}
		n := 100 * len(label)
		for _, word := range strings.FieldsFunc(strings.ToLower(l.section), func(r rune) bool { return !isWord(r) }) {
			if utf8.RuneCountInString(word) >= 4 && hasWords(w, word) {
				n++
			}
		}
		if n > score {
			best, score = l.label+": "+l.text, n
		}
	}
	return best
}

// hasWords reports whether s holds words as a whole, not inside a word.
func hasWords(s, words string) bool {
	if words == "" {
		return false
	}
	for i := 0; ; {
		j := strings.Index(s[i:], words)
		if j < 0 {
			return false
		}
		j += i
		before, _ := utf8.DecodeLastRuneInString(s[:j])
		after, _ := utf8.DecodeRuneInString(s[j+len(words):])
		if !isWord(before) && !isWord(after) {
			return true
		}
		i = j + 1
	}
}

func isWord(r rune) bool { return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

// doc is a tally being written: its Markdown, and its lines with the
// section and the label each is under, for Line.
type doc struct {
	b       strings.Builder
	base    int // the level of its title's heading
	section string
	lines   []docLine
}

type docLine struct{ section, label, text string }

func (d *doc) heading(level int, s string) {
	if d.b.Len() > 0 {
		d.b.WriteString("\n")
	}
	fmt.Fprintf(&d.b, "%s %s\n\n", strings.Repeat("#", d.base+level), s)
	d.section = s
}

func (d *doc) para(s string) { d.b.WriteString(s + "\n") }

func (d *doc) item(label, text string) {
	d.lines = append(d.lines, docLine{d.section, label, text})
	fmt.Fprintf(&d.b, "- %s: %s\n", label, text)
}

// cols are the numbers a list's rows show.
type cols struct {
	n        string // what N counts, such as "calls"; "" to leave it out
	of       bool   // N of Of, with its share
	sessions bool
	minutes  bool
	most     bool // the longest one
	usd      bool
	asks     bool // N of Of as answered of asked
}

// numbers is row a's numbers, each with b's beside it in brackets.
func (c cols) numbers(a, b Row) string {
	var parts []string
	add := func(now, before string) { parts = append(parts, now+" ("+before+")") }
	switch {
	case c.asks:
		add(num(a.Of)+" asked", num(b.Of))
		add(num(a.N)+" answered", num(b.N))
	case c.of:
		add(num(a.N)+" of "+num(a.Of)+", "+share(a), num(b.N)+" of "+num(b.Of)+", "+share(b))
	case c.n != "":
		add(units(a.N, c.n), num(b.N))
	}
	if c.sessions {
		add(units(a.Sessions, "sessions"), num(b.Sessions))
	}
	if c.minutes {
		add(mins(a.Minutes)+" min"+inAll(c.asks), mins(b.Minutes))
	}
	if c.most {
		add("the longest "+mins(a.Most)+" min", mins(b.Most))
	}
	if c.usd {
		add(usd(a.USD), usd(b.USD))
	}
	s := strings.Join(parts, ", ")
	if len(a.Examples) > 0 {
		var ex []string
		for _, x := range a.Examples {
			ex = append(ex, code(x))
		}
		s += ". E.g. " + strings.Join(ex, " · ")
	}
	if a.Note != "" {
		s += ". " + a.Note
	}
	return s
}

func inAll(asks bool) string {
	if asks {
		return " waited in all"
	}
	return ""
}

// list writes the rows of now, each beside the row of before by its name,
// then the rows only before had, up to as many as now's list may hold.
func (d *doc) list(now, before []Row, top int, c cols) {
	if len(now) == 0 && len(before) == 0 {
		d.para("None.")
		return
	}
	had := map[string]Row{}
	for _, r := range before {
		had[r.Name] = r
	}
	seen := map[string]bool{}
	n := 0
	for _, r := range now {
		d.item(r.Name, c.numbers(r, had[r.Name]))
		seen[r.Name] = true
		n++
	}
	for _, r := range before {
		if !seen[r.Name] && n < top {
			d.item(r.Name, c.numbers(Row{Name: r.Name}, r))
			n++
		}
	}
}

// render writes the tally with its title's heading at level base.
func render(t Tally, base int) *doc {
	d := &doc{base: base - 1}
	a, b := t.Now, t.Before
	d.heading(1, "Raguel's tally, "+day(a.From)+" to "+day(a.To))
	d.para(fmt.Sprintf("%s, each number beside the %s before it (%s to %s) in brackets. Calls, waits and failures count in the stretch they came in; a session's cost, Claude Code's own from its start at list price, counts in the stretch of its last event.",
		counted(t.Days, "day"), counted(t.Days, "day"), day(b.From), day(b.To)))
	d.b.WriteString("\n")
	d.item("Sessions", num(a.Sessions)+" ("+num(b.Sessions)+")")
	d.item("Tool calls", num(a.Calls)+" ("+num(b.Calls)+")")

	d.heading(2, Seat("raguel")+": cost")
	d.item("What the sessions cost", usd(a.USD)+" ("+usd(b.USD)+")")
	d.heading(3, "By repository")
	d.list(a.ByRepo, b.ByRepo, topList, cols{n: "sessions", usd: true})
	d.heading(3, "By profile")
	d.list(a.ByProfile, b.ByProfile, topList, cols{n: "sessions", usd: true})
	d.heading(3, "The costliest sessions")
	if len(a.Costliest) == 0 {
		d.para("None.")
	}
	for _, r := range a.Costliest {
		d.item(r.Name, usd(r.USD)+", "+r.Note)
	}
	d.heading(3, "Prompt-cache rebuilds, by what woke the session")
	d.item(a.Rebuilds.Name, cols{n: "rebuilds", usd: true}.numbers(a.Rebuilds, b.Rebuilds))
	d.list(a.RebuiltBy, b.RebuiltBy, topList, cols{n: "rebuilds", usd: true})
	d.heading(3, "Calls by tool")
	d.list(a.Tools, b.Tools, topTools, cols{n: "calls", sessions: true, minutes: true})
	d.heading(3, "Commands by kind")
	d.list(a.Commands, b.Commands, topCommands, cols{n: "calls", sessions: true, minutes: true})
	d.heading(3, "Searches, then a read of what they found within two calls")
	d.list(a.Finds, b.Finds, topList, cols{of: true})
	d.heading(3, "The same command three or more times in a row")
	d.list(a.Repeats, b.Repeats, topList, cols{n: "calls", sessions: true})
	d.heading(3, "Tasks handed on")
	d.item(a.Delegated.Name, cols{n: "tasks", usd: true}.numbers(a.Delegated, b.Delegated))

	d.heading(2, Seat("gabriel")+": time")
	d.heading(3, "Hand-written waits")
	d.item(a.Waits.Name, cols{n: "calls", minutes: true}.numbers(a.Waits, b.Waits))
	d.heading(3, "The kinds of call that took the longest")
	d.list(a.Longest, b.Longest, topList, cols{minutes: true, n: "calls"})
	d.heading(3, "Your asks, and how long they waited for an answer")
	d.list(a.Asks, b.Asks, topList, cols{asks: true, minutes: true, most: true})

	d.heading(2, Seat("raphael")+": what breaks")
	d.heading(3, "Calls that failed, by kind")
	d.list(a.ToolFailures, b.ToolFailures, topList, cols{of: true})
	d.heading(3, "Turns that failed, by error")
	d.list(a.TurnFailures, b.TurnFailures, topList, cols{n: "turns"})
	d.heading(3, "Runs of tests and checks that failed")
	d.item(a.Tests.Name, cols{of: true}.numbers(a.Tests, b.Tests))
	d.list(a.TestsBy, b.TestsBy, topList, cols{of: true})
	d.heading(3, "Turns the jot dog sent back, by what was left")
	d.list(a.Nudges, b.Nudges, topList, cols{n: "turns"})
	d.heading(3, "Usage limits")
	d.item("Tasks carried on by another session", num(a.Failovers)+" ("+num(b.Failovers)+")")
	d.item("Sessions switched to their fallback model", num(a.Fallbacks)+" ("+num(b.Fallbacks)+")")

	d.heading(2, Seat("mikael")+": what must not be traded")
	d.para("No numbers: he weighs every plan.")

	d.heading(2, "Earlier councils' plans")
	if len(t.Earlier) == 0 {
		d.para("None yet.")
	}
	for _, e := range t.Earlier {
		fmt.Fprintf(&d.b, "- %s, plan %d, %s: %s. %s Watch: %s.", e.Council, e.N, Seat(e.Seat), stop(e.Title), said(e), stop(e.Watch))
		if e.Then != "" {
			fmt.Fprintf(&d.b, " Then: %s.", stop(e.Then))
		}
		if e.Now != "" {
			fmt.Fprintf(&d.b, " Now: %s.", stop(e.Now))
		}
		d.b.WriteString("\n")
	}
	return d
}

// said is what the user said to an earlier plan, in words.
func said(e Earlier) string {
	switch e.Said {
	case Yes:
		s := "Yes"
		if e.Jot != "" {
			s += "; its jot " + e.Jot
			if e.State != "" {
				s += " is " + e.State
			}
		}
		return s + "."
	case No:
		if e.Why != "" {
			return "No: " + stop(e.Why) + "."
		}
		return "No."
	case Lapsed:
		return "Lapsed with no answer."
	}
	return "Waiting for an answer."
}

// stop is s without the full stop it may end with, for a sentence that
// puts its own.
func stop(s string) string { return strings.TrimRight(strings.TrimSpace(s), ".") }

// code is a command as Markdown's code.
func code(s string) string {
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

func day(t time.Time) string { return t.Format("2006-01-02") }

// num is n with its thousands apart: 47,093.
func num(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

// mins is a number of minutes: to the tenth below 10, else whole.
func mins(m float64) string {
	if m < 10 {
		return strconv.FormatFloat(math.Round(m*10)/10, 'f', -1, 64)
	}
	return num(int(math.Round(m)))
}

// usd is dollars and cents, or whole dollars from a thousand.
func usd(v float64) string {
	if v >= 1000 {
		return "$" + num(int(math.Round(v)))
	}
	return fmt.Sprintf("$%.2f", v)
}

// share is N's share of Of, in percent.
func share(r Row) string {
	if r.Of == 0 {
		return "0%"
	}
	return strconv.Itoa(int(math.Round(100*float64(r.N)/float64(r.Of)))) + "%"
}

// units is n of what many counts, such as "calls": "1 call", "2 calls".
func units(n int, many string) string {
	if n == 1 {
		return "1 " + strings.TrimSuffix(many, "s")
	}
	return num(n) + " " + many
}

// counted is n things in words: "1 day", "7 days".
func counted(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return num(n) + " " + thing + "s"
}
