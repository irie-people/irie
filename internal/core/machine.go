package core

// What this core says of its machine to the cores it is paired with
// (docs/proposals/multi-host.md §4.2, §4.4): whether Claude Code is signed
// in, the profiles, the user's own Claude Code hooks, and the free disk.
// Another core starts a task here only when this machine runs the same
// hooks as that one, with every program they start, so no task ever runs
// without a hook, such as a repo's guard, that it would have had there.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/ready"
)

// machineEvery is how often, in ticks, the core checks its machine: the
// sign-in check starts Claude Code.
const machineEvery = 60

// checkMachine reads what the core says of its machine, and tells
// subscribers when it changed. The caller holds no lock.
func (c *Core) checkMachine() {
	home, _ := os.UserHomeDir()
	m := ipc.Machine{Name: c.machineNameLocked(), OS: runtime.GOOS}
	r := c.ready()
	for _, it := range r.Items {
		if it.ID == ready.SignInID {
			m.Claude = string(it.Status)
		}
	}
	m.Hooks, m.HooksMissing = userHooks(home, os.Getenv("PATH"))
	for name := range c.cfg().Profiles {
		m.Profiles = append(m.Profiles, name)
	}
	slices.Sort(m.Profiles)
	if d, err := localmodel.FreeDisk(c.paths.Data); err == nil {
		m.Disk = d
	}
	c.mu.Lock()
	changed := !reflect.DeepEqual(c.mach, &m)
	c.mach, c.machAt = &m, time.Now()
	c.mu.Unlock()
	if changed {
		c.publishHeader()
	}
}

// machineNameLocked is machineName for a caller that holds no lock.
func (c *Core) machineNameLocked() string { return c.machineName() }

// userHooks reads the user's own Claude Code hooks, ~/.claude/settings.json
// under home: each as the SHA-256 of its event, its matcher and its
// command, and the programs they start that are not there.
func userHooks(home, path string) (sums, missing []string) {
	b, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return nil, nil
	}
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(b, &s) != nil {
		return nil, nil
	}
	for event, groups := range s.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if h.Type != "command" || strings.TrimSpace(h.Command) == "" {
					continue
				}
				prog, rest := splitHook(h.Command)
				sum := sha256.Sum256([]byte(event + "\x00" + g.Matcher + "\x00" + filepath.Base(prog) + rest))
				sums = append(sums, hex.EncodeToString(sum[:]))
				if !hookRunnable(expandHome(prog, home), path) && !slices.Contains(missing, prog) {
					missing = append(missing, prog)
				}
			}
		}
	}
	slices.Sort(sums)
	sums = slices.Compact(sums)
	slices.Sort(missing)
	return sums, missing
}

// splitHook splits a hook's command, as sh would run it, into its
// program and the rest, after any VAR=value words. A program in quotes
// keeps its spaces.
func splitHook(cmd string) (prog, rest string) {
	cmd = strings.TrimSpace(cmd)
	for {
		word, after := firstWord(cmd)
		if k, _, ok := strings.Cut(word, "="); ok && k != "" && !strings.ContainsAny(k, `/"'$`) {
			cmd = strings.TrimSpace(after)
			continue
		}
		return word, after
	}
}

func firstWord(s string) (word, rest string) {
	if s == "" {
		return "", ""
	}
	if q := s[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(s[1:], q); end >= 0 {
			return s[1 : end+1], s[end+2:]
		}
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

func expandHome(p, home string) string {
	switch {
	case p == "~" || p == "$HOME":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	case strings.HasPrefix(p, "$HOME/"):
		return filepath.Join(home, p[6:])
	}
	return p
}

// hookRunnable reports whether a hook's program is here: a path that can
// run, or a name in a folder of path. A program named by a variable can't
// be told, and counts as there.
func hookRunnable(prog, path string) bool {
	if strings.Contains(prog, "$") {
		return true
	}
	can := func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
	}
	if strings.Contains(prog, "/") {
		return can(prog)
	}
	for _, dir := range filepath.SplitList(path) {
		if dir != "" && can(filepath.Join(dir, prog)) {
			return true
		}
	}
	return false
}
