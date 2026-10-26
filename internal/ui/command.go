package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/irie-hq/irie/internal/ipc"
)

// ParseTask turns command bar input into spawn args (DESIGN §4.2). Only the
// first line is read for tokens: an @/abs/path or @~/path token runs the task
// in that directory, and the rest of the input is the task, verbatim.
func ParseTask(input, home string) (ipc.SpawnArgs, error) {
	var a ipc.SpawnArgs
	first, rest, multi := strings.Cut(input, "\n")
	if strings.HasPrefix(strings.TrimSpace(first), ":") {
		cmd, _, _ := strings.Cut(strings.TrimSpace(first), " ")
		return a, fmt.Errorf("unknown command %s", cmd)
	}

	var words []string
	for _, w := range strings.Fields(first) {
		if !strings.HasPrefix(w, "@") {
			words = append(words, w)
			continue
		}
		if a.Cwd != "" {
			return a, errors.New("only one @ target per task")
		}
		dir, err := target(w[1:], home)
		if err != nil {
			return a, err
		}
		a.Cwd = dir
	}

	// Without a token the first line is kept as typed, inner spacing and all.
	line := strings.TrimSpace(first)
	if a.Cwd != "" {
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

// target resolves the path of an @ token. Repository names are not
// supported yet, only absolute and ~ paths, which run in place.
func target(p, home string) (string, error) {
	switch {
	case p == "~":
		return home, nil
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:]), nil
	case filepath.IsAbs(p):
		return filepath.Clean(p), nil
	}
	return "", fmt.Errorf("@%s: give an absolute path or a ~/ path", p)
}
