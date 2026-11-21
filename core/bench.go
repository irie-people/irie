package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/irie-people/irie/internal/brand"
)

// BenchTask reads a bench's words (DESIGN §6.6), as :bench and `irie ctl
// bench` take them: line holds them, and text, when it runs on over more
// lines, the rest of the task. A ^HOST word runs the bench's sessions on
// that machine; the @ and + words say where they run and on what profile,
// as for a task. The rest is the task.
func BenchTask(line, text, home string) (BenchArgs, error) {
	return View{Home: home}.benchTask(line, text)
}

// benchTask is BenchTask, with the + words read against v.Profiles; but a
// bench on another machine runs on that machine's profiles, which may not
// be this one's, so there every + name is taken for a profile's, and that
// machine says when it has none.
func (v View) benchTask(line, text string) (BenchArgs, error) {
	var a BenchArgs
	var words []string
	for _, w := range strings.Fields(line) {
		if h, ok := strings.CutPrefix(w, "^"); ok && h != "" && a.Host == "" {
			a.Host = h
			continue
		}
		words = append(words, w)
	}
	task := strings.Join(words, " ")
	if _, more, ok := strings.Cut(text, "\n"); ok {
		task += "\n" + more
	}
	if a.Host != "" {
		v.Profiles = nil
	}
	s, err := v.parseTask(task)
	switch {
	case err != nil:
		return a, err
	case s.Anon != "":
		return a, errors.New("a bench runs no anonymous research")
	case s.InPlace:
		return a, errors.New("a bench's runs each need a worktree of their own: drop the ! after the repo")
	}
	a.Task, a.Profile, a.Repo = s.Task, s.Profile, s.Repo
	return a, nil
}

// BenchStarted says what a bench that started does.
func BenchStarted(b *Bench) string {
	if b == nil {
		return "Started a bench."
	}
	where := ""
	if b.Host != "" {
		where = " on " + b.Host
	}
	return fmt.Sprintf("Started bench %s: %d runs of +%s%s on “%s”. Once they are through, +%s judges them, makes the one result and rewrites +%s's playbook.",
		b.ID, len(b.Runs), b.Profile, where, firstLine(b.Task), b.Judge, b.Profile)
}

// BenchLine is one bench in a line: its id and state, its runs' scores,
// and the task.
func BenchLine(b Bench) string {
	var scores []string
	for _, r := range b.Runs {
		switch {
		case r.Score != nil:
			scores = append(scores, fmt.Sprint(*r.Score))
		default:
			scores = append(scores, "-")
		}
	}
	where := "+" + b.Profile
	if b.Host != "" {
		where += " on " + b.Host
	}
	line := fmt.Sprintf("%s  %-7s  %s  %s  %s  %s", b.ID, b.State, where, b.Class, strings.Join(scores, "/"), firstLine(b.Task))
	if b.Problem != "" {
		line += "  (" + b.Problem + ")"
	}
	return line
}

// firstLine is a task's first line, cut short.
func firstLine(task string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	if r := []rune(first); len(r) > 60 {
		first = string(r[:59]) + "…"
	}
	return first
}

// BenchUsage is how `irie ctl bench` is used.
const BenchUsage = "usage: " + brand.Slug + " ctl bench [--runs N] [--judge PROFILE] [--from REPO] [--wait] [^HOST] [+PROFILE] [@REPO] TASK...\n       " + brand.Slug + " ctl bench [--json]   lists the benches\n       " + brand.Slug + " ctl bench [--wait] [--json] ID   shows one, or waits for its verdict"
