package lessons

import (
	"fmt"
	"io"
	"strings"
)

// byShort says who stopped a kind's calls, in a word or two.
var byShort = map[string]string{
	ByCommand:    "its command",
	ByTool:       "the tool",
	ByIrie:       "Irie's guard",
	ByPermission: "a permission check",
	ByUser:       "the user",
	ByHook:       "a hook of yours",
}

// kindLine is a kind on one line: its calls and sessions, what it is, and
// what worked next.
func kindLine(k Kind) string {
	s := fmt.Sprintf("%d calls in %d sessions, stopped by %s: %s", k.Calls, k.Sessions, byShort[k.By], k.Kind)
	if k.Then != "" {
		s += " → then " + k.Then
	}
	return s
}

// Evidenced is a drafted lesson's evidence, a line for each kind it cites.
func Evidenced(l Lesson) string {
	var lines []string
	for _, k := range l.Kinds {
		lines = append(lines, kindLine(k))
	}
	if l.Days > 0 {
		lines = append(lines, fmt.Sprintf("Over %d days; drafted by %s.", l.Days, l.By))
	}
	return strings.Join(lines, "\n")
}

// CameOf says what came of a kept lesson: its kinds' failed calls a day
// before it was kept, by its evidence, and since, by the latest.
func CameOf(l Lesson) string {
	s := "Kept " + l.At.Local().Format("2 Jan 2006") + "."
	before := ""
	if l.Days > 0 {
		before = fmt.Sprintf(" Before: %s failed calls of its kinds a day.", perDay(l.Calls(), float64(l.Days)))
	}
	switch {
	case l.Since == nil:
		return s + before + " What came of it shows once lessons are drafted again."
	case l.SinceDays < 1:
		return s + before + fmt.Sprintf(" Since: %d in less than a day.", *l.Since)
	}
	days := fmt.Sprintf("%.0f days", l.SinceDays)
	if days == "1 days" {
		days = "1 day"
	}
	return s + before + fmt.Sprintf(" Since: %s a day, over %s.", perDay(*l.Since, l.SinceDays), days)
}

func perDay(n int, days float64) string {
	if days <= 0 {
		return "?"
	}
	return fmt.Sprintf("%.1f", float64(n)/days)
}

// Summary says in a line what the evidence read and what it left out.
func Summary(ev Evidence) string {
	pct := 0.0
	if ev.Calls > 0 {
		pct = 100 * float64(ev.Failed) / float64(ev.Calls)
	}
	s := fmt.Sprintf("%s to %s: %d of %d tool calls failed (%.1f%%), in %d sessions", ev.From.Local().Format("2 Jan"), ev.To.Local().Format("2 Jan"), ev.Failed, ev.Calls, pct, ev.Sessions)
	if ev.Transcripts > 0 {
		s += fmt.Sprintf(", read from the journal and %d transcripts", ev.Transcripts)
	} else {
		s += ", read from the journal alone"
	}
	s += "."
	if ev.YoursCalls > 0 {
		share := 100 * float64(ev.YoursCalls) / float64(max(ev.Failed, 1))
		s += fmt.Sprintf(" Hooks of your own refused %d (%.0f%% of what failed): their share is theirs to weigh, and no lesson is drafted from it.", ev.YoursCalls, share)
	}
	if ev.Unanswered > 0 {
		s += fmt.Sprintf(" Claude Code's auto mode gave no verdict %d times, no one's mistake.", ev.Unanswered)
	}
	return s
}

// PrintEvidence writes what failed, for a person or an agent to read at a
// glance: the kinds worth a lesson, numbered as a draft cites them, and the
// share of hooks of the user's own.
func PrintEvidence(w io.Writer, ev Evidence) {
	fmt.Fprintln(w, "What agents' calls failed at, "+Summary(ev))
	if len(ev.Kinds) == 0 {
		fmt.Fprintln(w, "\nNo kind of failure is shared by two sessions or more: nothing to learn from yet.")
	} else {
		fmt.Fprintln(w, "\nThe kinds two sessions or more share, or the user turned down:")
		for _, k := range ev.Kinds {
			fmt.Fprintf(w, "%3d. %s\n", k.N, kindLine(k))
		}
	}
	if len(ev.Yours) > 0 {
		fmt.Fprintln(w, "\nRefused by hooks of your own:")
		for _, k := range ev.Yours {
			fmt.Fprintf(w, "     %d calls in %d sessions: %s\n", k.Calls, k.Sessions, k.Kind)
		}
	}
}

// Print writes the lessons: those every session hears, and the latest
// draft's waiting for the user's word, each with its evidence.
func Print(w io.Writer, b Book, drafting bool) {
	if len(b.Kept) == 0 {
		fmt.Fprintln(w, "Every session hears no lessons yet.")
	} else {
		fmt.Fprintf(w, "Every session hears %d lessons you kept, as it starts:\n", len(b.Kept))
		for _, l := range b.Kept {
			fmt.Fprintf(w, "%3d. %s\n     %s\n", l.N, l.Text, CameOf(l))
		}
	}
	waiting := b.Waiting()
	switch {
	case drafting:
		fmt.Fprintln(w, "\nA draft is under way: its lessons show here when it is done.")
	case !b.Drafted.IsZero() && len(waiting) > 0:
		fmt.Fprintf(w, "\nDrafted %s by %s, waiting for your word (keep N, no N):\n", b.Drafted.Local().Format("2 Jan 15:04"), b.By)
		for _, l := range waiting {
			fmt.Fprintf(w, "%3d. %s\n", l.N, l.Text)
			for _, line := range strings.Split(Evidenced(l), "\n") {
				fmt.Fprintf(w, "     %s\n", line)
			}
		}
	case !b.Drafted.IsZero():
		fmt.Fprintf(w, "\nThe latest draft, of %s, has none waiting for your word.\n", b.Drafted.Local().Format("2 Jan 15:04"))
	}
	if b.Problem != "" && !drafting {
		fmt.Fprintf(w, "\nThe last draft brought no lessons: %s\n", b.Problem)
	}
	if b.Evidence != nil && !drafting {
		fmt.Fprintln(w, "\nIts evidence, "+Summary(*b.Evidence))
	}
}
