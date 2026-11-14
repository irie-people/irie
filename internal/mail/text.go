package mail

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// Summary is how the accounts stand, in words, as :mail answers in a task
// bar: the unread count of each, then the newest unread messages.
func Summary(r ipc.MailResult, now time.Time) string {
	if len(r.Accounts) == 0 {
		return "Mail: no accounts yet. Add one in " + brand.Name + "'s Mail (Add Account…), or with `" + brand.Slug + " mail add gmail you@gmail.com`."
	}
	total := 0
	var lines []string
	for _, a := range r.Accounts {
		total += a.Unread
		line := fmt.Sprintf("  %s (%s): ", a.Name, a.Provider)
		switch a.State {
		case ipc.MailNeeds:
			line += "needs its password"
		case ipc.MailError:
			line += "couldn't be read: " + a.Problem
		case ipc.MailChecking:
			line += "not read yet"
		default:
			line += fmt.Sprintf("%d unread", a.Unread)
		}
		lines = append(lines, line)
	}
	head := fmt.Sprintf("Mail: %d unread in %s.", total, count(len(r.Accounts), "account"))
	var unread []string
	for _, m := range r.Messages {
		if m.Unread && len(unread) < 5 {
			unread = append(unread, "  "+Line(m, now))
		}
	}
	out := head + "\n" + strings.Join(lines, "\n")
	if len(unread) > 0 {
		out += "\nNewest unread:\n" + strings.Join(unread, "\n")
	}
	return out
}

// Table is a list of messages as irie mail prints it: a line each, the
// account first, with ● for unread and ★ for starred.
func Table(r ipc.MailResult, now time.Time) string {
	var b strings.Builder
	for _, a := range r.Accounts {
		if a.State == ipc.MailError || a.State == ipc.MailNeeds {
			fmt.Fprintf(&b, "! %s (%s): %s\n", a.Name, a.Provider, a.Problem)
		}
	}
	if len(r.Messages) == 0 {
		b.WriteString("No messages.\n")
		return b.String()
	}
	for _, m := range r.Messages {
		b.WriteString(Line(m, now))
		b.WriteString("\n")
	}
	return b.String()
}

// Line is one message on a line.
func Line(m ipc.MailMessage, now time.Time) string {
	mark := " "
	switch {
	case m.Unread:
		mark = "●"
	case m.Starred:
		mark = "★"
	}
	subject := m.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	return fmt.Sprintf("%s %-10s %-22s %-44s %s", mark, cut(m.Account, 10), cut(m.From, 22), cut(subject, 44), When(m.Date, now))
}

// When is a message's time as a list shows it: the time today, the day
// this week, else the date.
func When(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	switch {
	case t.IsZero():
		return ""
	case sameDay(t, now):
		return t.Format("15:04")
	case now.Sub(t) < 6*24*time.Hour && t.Before(now):
		return t.Format("Mon")
	case t.Year() == now.Year():
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func count(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}
