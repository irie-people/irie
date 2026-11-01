package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// ParseTask turns command bar input into spawn args (DESIGN §4.2). Only the
// first line is read for tokens: @name or @path runs the task in a new
// worktree of that git repo, or in place in a directory git does not know;
// @name! runs it in the repo's own checkout; +profile picks the profile;
// +explain or +teach picks a tutor mode. The rest of the input is the task,
// verbatim.
func ParseTask(input, home string) (ipc.SpawnArgs, error) {
	var a ipc.SpawnArgs
	first, rest, multi := strings.Cut(input, "\n")
	if strings.HasPrefix(strings.TrimSpace(first), ":") {
		cmd, _, _ := strings.Cut(strings.TrimSpace(first), " ")
		return a, fmt.Errorf("unknown command %s", cmd)
	}

	var words []string
	tokens := false
	for _, w := range strings.Fields(first) {
		if !isToken(w) {
			words = append(words, w)
			continue
		}
		tokens = true
		if err := token(&a, w, home); err != nil {
			return a, err
		}
	}

	// Without a token the first line is kept as typed, inner spacing and all.
	line := strings.TrimSpace(first)
	if tokens {
		line = strings.Join(words, " ")
	}
	switch {
	case !multi:
		a.Task = line
	case line == "":
		a.Task = rest
	default:
		a.Task = line + "\n" + rest
	}
	if strings.TrimSpace(a.Task) == "" {
		return a, errors.New("empty task")
	}
	return a, nil
}

// SpawnTokens applies the @ and + tokens among the first of words, as `irie
// ctl spawn` takes them, and returns the words that follow.
func SpawnTokens(a *ipc.SpawnArgs, words []string, home string) ([]string, error) {
	for i, w := range words {
		if !isToken(w) || strings.ContainsAny(w, " \t\n") {
			return words[i:], nil
		}
		if err := token(a, w, home); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func isToken(w string) bool { return strings.HasPrefix(w, "@") || strings.HasPrefix(w, "+") }

// token applies one @ or + word to a. Paths are made absolute here; names
// are the core's to resolve.
func token(a *ipc.SpawnArgs, w, home string) error {
	v := w[1:]
	if w[0] == '+' {
		// The tutor words set how the task works with you, beside its
		// profile (docs/proposals/learning.md).
		if v == config.TutorExplain || v == config.TutorTeach {
			if a.Tutor != "" {
				return errors.New("one of +explain and +teach per task")
			}
			a.Tutor = v
			return nil
		}
		switch {
		case a.Profile != "":
			return errors.New("only one +profile per task")
		case v == "":
			return errors.New("+ needs a profile name")
		}
		a.Profile = v
		return nil
	}
	if a.Repo != "" {
		return errors.New("only one @ target per task")
	}
	inPlace := strings.HasSuffix(v, "!")
	v = strings.TrimSuffix(v, "!")
	switch {
	case v == "":
		return errors.New("@ needs a repo name or a path")
	case strings.Contains(v, "!"):
		return fmt.Errorf("%s: one ! at the end runs the task in the repo's own checkout", w)
	case v == "~":
		v = home
	case strings.HasPrefix(v, "~/"):
		v = filepath.Join(home, v[2:])
	case filepath.IsAbs(v):
		v = filepath.Clean(v)
	case strings.HasPrefix(v, "~") || v == "." || v == ".." || strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../"):
		return fmt.Errorf("%s: give an absolute path, a ~/ path or a repo name", w)
	}
	a.Repo, a.InPlace = v, inPlace
	return nil
}

// chipMin is the size above which a paste becomes a chip.
const chipMin = 8 << 10

// chip is a paste kept out of the textarea, which would cap it and turn its
// tabs into spaces: the bar shows its label, and submitting puts the paste
// back whole in its place.
type chip struct{ label, content string }

func newChip(content string) chip {
	// Lines end in LF, whatever the terminal sent; nothing else changes.
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return chip{label: chipLabel(content), content: content}
}

// chipLabel describes a paste: [paste 2.1 MB · 31,204 lines].
func chipLabel(s string) string {
	lines := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		lines++
	}
	unit := "lines"
	if lines == 1 {
		unit = "line"
	}
	return fmt.Sprintf("[paste %s · %s %s]", size(len(s)), thousands(lines), unit)
}

func size(n int) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1f KB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d B", n)
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// expand puts each chip's paste back in place of its label. Labels match
// chips in the order they were made; a chip whose label was deleted is
// dropped, and a label typed by hand stays text.
func expand(task string, chips []chip) string {
	used := make([]bool, len(chips))
	var b strings.Builder
	for {
		at, which := -1, -1
		for i, c := range chips {
			if used[i] {
				continue
			}
			if j := strings.Index(task, c.label); j >= 0 && (at < 0 || j < at) {
				at, which = j, i
			}
		}
		if which < 0 {
			break
		}
		b.WriteString(task[:at])
		b.WriteString(chips[which].content)
		task = task[at+len(chips[which].label):]
		used[which] = true
	}
	b.WriteString(task)
	return b.String()
}
