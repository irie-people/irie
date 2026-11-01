package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ParseTask turns what was typed into spawn args (DESIGN §4.2). Only the
// first line is read for words: @name or @path runs the task in a new
// worktree of that git repo, or in place in a directory git does not know;
// @name! runs it in the repo's own checkout; +profile picks the profile.
// The rest of the input is the task, verbatim. home is what ~ means.
func ParseTask(input, home string) (SpawnArgs, error) {
	var a SpawnArgs
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
		if err := applyToken(&a, w, home); err != nil {
			return a, err
		}
	}

	// Without a word the first line is kept as typed, inner spacing and all.
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
		if tokens {
			return a, errors.New("empty task: type what you want done after the @ or + words")
		}
		return a, errors.New("empty task: type what you want done")
	}
	return a, nil
}

// SpawnTokens applies the @ and + words among the first of words, as `irie
// ctl spawn` takes them, and returns the words that follow.
func SpawnTokens(a *SpawnArgs, words []string, home string) ([]string, error) {
	for i, w := range words {
		if !isToken(w) || strings.ContainsAny(w, " \t\n") {
			return words[i:], nil
		}
		if err := applyToken(a, w, home); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func isToken(w string) bool { return strings.HasPrefix(w, "@") || strings.HasPrefix(w, "+") }

// applyToken applies one @ or + word to a. Paths are made absolute here; names
// are the core's to resolve.
func applyToken(a *SpawnArgs, w, home string) error {
	v := w[1:]
	if w[0] == '+' {
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

// Unpaste puts each paste back whole in place of its label. Labels match
// pastes in the order they were made; a paste whose label was deleted is
// dropped, and a label typed by hand, with no paste left for it, stays text.
func Unpaste(task string, pastes []Paste) string {
	used := make([]bool, len(pastes))
	var b strings.Builder
	for {
		at, which := -1, -1
		for i, p := range pastes {
			if used[i] || p.Label == "" {
				continue
			}
			if j := strings.Index(task, p.Label); j >= 0 && (at < 0 || j < at) {
				at, which = j, i
			}
		}
		if which < 0 {
			break
		}
		b.WriteString(task[:at])
		b.WriteString(pastes[which].Text)
		task = task[at+len(pastes[which].Label):]
		used[which] = true
	}
	b.WriteString(task)
	return b.String()
}
