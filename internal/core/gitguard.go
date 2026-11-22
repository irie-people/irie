package core

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
)

// gitAskNote is the reason Claude Code shows the user in its permission
// prompt when Mikael asks them first: what the call changes, and why it is
// theirs to say.
func gitAskNote(what string) string {
	return "[" + brand.Name + "] Mikael asks you first: this " + what + ". Your own git runs that as you, outside any sandbox, from your terminal, its prompt and your git apps. Allow it only if you asked for it."
}

// gitGuard says what to ask the user before a call an agent is about to
// make changes what their own git runs: a repository's config, hooks or
// attributes, or a setting git runs as a command (DESIGN §4.11). "" lets
// the call go on as it would. Mikael keeps it while he and his door are on.
// It holds no lock when it is called.
func (c *Core) gitGuard(tool string, input json.RawMessage, cwd string, evs []event.Event) string {
	m := c.cfg().Arch.Mikael
	if !m.On || !m.Door || !slices.ContainsFunc(evs, func(e event.Event) bool { return e.Kind == event.ToolStarted }) {
		return ""
	}
	var in struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Notebook string `json:"notebook_path"`
	}
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	home, _ := os.UserHomeDir()
	what := ""
	switch tool {
	case "Bash":
		what = changesGit(in.Command, cwd, home)
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		g := gitScan{home: home, dir: cwd, vars: map[string]string{}}
		if p := in.FilePath + in.Notebook; g.protected(p) {
			what = "writes " + g.show(p)
		}
	}
	if what == "" {
		return ""
	}
	return gitAskNote(what)
}

// changesGit says what a shell command changes of what the user's own git
// runs, "" for nothing: git config setting a key it runs as a command
// (runsCommand), or a write to a file gitFile names, by a redirect, cp, mv,
// rm, ln, install, rsync, ditto, tee, dd, sed -i, perl -i, chmod or chown.
// Like Gabriel's guard it reads the commands a person would see, following
// cd and the variables the command sets, and errs toward letting them run:
// a path in a folder it cannot know runs. Claude Code's permissions stay the
// hard limit.
func changesGit(cmd, cwd, home string) string {
	g := gitScan{home: home, dir: cwd, vars: map[string]string{}}
	return g.change(cmd)
}

// gitScan follows a shell command's commands as a shell runs them: the
// folder they run in ("" once a cd went somewhere unknown) and the
// variables they set.
type gitScan struct {
	home string
	dir  string
	vars map[string]string
}

func (g *gitScan) change(cmd string) string {
	for _, ws := range commands(withoutText(cmd)) {
		for i, w := range ws {
			m := redirect.FindString(w)
			if !strings.Contains(m, ">") {
				continue
			}
			target := w[len(m):]
			if target == "" && i+1 < len(ws) {
				target = ws[i+1]
			}
			if !strings.HasPrefix(target, "&") && g.protected(target) {
				return "writes " + g.show(target)
			}
		}
		words, _ := command(ws)
		if len(words) == 0 {
			g.set(ws) // NAME=VALUE alone: the shell keeps it
			continue
		}
		switch base := filepath.Base(words[0]); {
		case base == "cd" || base == "pushd":
			g.cd(words[1:])
		case base == "export" || base == "declare" || base == "typeset" || base == "local" || base == "readonly":
			g.set(words[1:])
		case slices.Contains(shells, base):
			for i, w := range words[1:] {
				if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.HasSuffix(w, "c") {
					if i+2 < len(words) {
						sub := gitScan{home: g.home, dir: g.dir, vars: maps.Clone(g.vars)}
						if what := sub.change(words[i+2]); what != "" {
							return what
						}
					}
					break
				}
			}
		case base == "eval":
			if what := g.change(strings.Join(words[1:], " ")); what != "" {
				return what
			}
		case base == "git":
			if what := g.gitConfig(words[1:]); what != "" {
				return what
			}
		default:
			verb, paths := writesTo(base, words[1:])
			for _, p := range paths {
				if g.protected(p) {
					return verb + " " + g.show(p)
				}
			}
		}
	}
	return ""
}

// set keeps the variables that words set, NAME=VALUE. One whose value is
// unknown, such as $(mktemp -d), which the reader splits off, is forgotten.
func (g *gitScan) set(words []string) {
	for _, w := range words {
		if !assignment.MatchString(w) {
			continue
		}
		name, val, _ := strings.Cut(w, "=")
		if v, ok := g.expand(val); ok && val != "" {
			g.vars[name] = v
		} else {
			delete(g.vars, name)
		}
	}
}

// cd follows cd DIR: the folder is unknown after cd - or a DIR it can't read.
func (g *gitScan) cd(args []string) {
	if len(args) == 0 {
		g.dir = g.home
		return
	}
	if p, ok := g.path(args[len(args)-1]); ok && args[len(args)-1] != "-" {
		g.dir = p
	} else {
		g.dir = ""
	}
}

// expand puts the variables' values, $NAME and ${NAME}, and the home folder
// for ~ and $HOME, into word: ok is false while it names one unknown.
func (g *gitScan) expand(word string) (string, bool) {
	if g.home != "" && (word == "~" || strings.HasPrefix(word, "~/")) {
		word = g.home + word[1:]
	}
	var b strings.Builder
	for i := 0; i < len(word); i++ {
		if word[i] != '$' || i+1 == len(word) {
			b.WriteByte(word[i])
			continue
		}
		rest, braced := word[i+1:], word[i+1] == '{'
		if braced {
			rest = rest[1:]
		}
		n := 0
		for n < len(rest) && (rest[n] == '_' || rest[n] >= 'A' && rest[n] <= 'Z' || rest[n] >= 'a' && rest[n] <= 'z' || n > 0 && rest[n] >= '0' && rest[n] <= '9') {
			n++
		}
		if n == 0 || braced && (n == len(rest) || rest[n] != '}') {
			return "", false
		}
		v, ok := g.vars[rest[:n]]
		if rest[:n] == "HOME" && g.home != "" {
			v, ok = g.home, true
		}
		if !ok {
			return "", false
		}
		b.WriteString(v)
		i += n
		if braced {
			i += 2
		}
	}
	return b.String(), true
}

// path is word as a clean, absolute path; ok is false when it can't be
// known: an unknown variable, or a relative path after an unknown cd.
func (g *gitScan) path(word string) (string, bool) {
	p, ok := g.expand(word)
	if !ok || p == "" {
		return "", false
	}
	if !filepath.IsAbs(p) {
		if g.dir == "" {
			return "", false
		}
		p = filepath.Join(g.dir, p)
	}
	return filepath.Clean(p), true
}

// protected reports whether word is a path that gitFile names.
func (g *gitScan) protected(word string) bool {
	p, ok := g.path(word)
	return ok && gitFile(p, g.home)
}

// show is word as the user would know it: its path, the home folder as ~.
func (g *gitScan) show(word string) string {
	p, ok := g.path(word)
	if !ok {
		return word
	}
	if g.home != "" && (p == g.home || strings.HasPrefix(p, g.home+"/")) {
		return "~" + p[len(g.home):]
	}
	return p
}

// gitFile reports whether writing path, clean and absolute, changes what
// the user's git runs: a repository's config, config.worktree, hooks or
// info/attributes, a bare one's too (NAME.git), a linked worktree's .git
// file, which names its repository, or the user's own git config. A
// repository in a temporary folder is the agent's own.
func gitFile(path, home string) bool {
	if !filepath.IsAbs(path) || scratchPath(path) {
		return false
	}
	if home != "" {
		for _, f := range []string{".gitconfig", ".config/git/config", ".config/git/attributes"} {
			if path == filepath.Join(home, f) {
				return true
			}
		}
	}
	seg := strings.Split(filepath.ToSlash(path), "/")
	for i, s := range seg {
		if s != ".git" && !(len(s) > len(".git") && strings.HasSuffix(s, ".git")) {
			continue
		}
		r := seg[i+1:]
		switch {
		case len(r) == 0:
			return s == ".git" // a worktree's .git file, or the folder itself
		case r[0] == "hooks", len(r) == 1 && (r[0] == "config" || r[0] == "config.worktree"),
			len(r) == 2 && r[0] == "info" && r[1] == "attributes":
			return true
		case r[0] == "worktrees" || r[0] == "modules":
			last := r[len(r)-1]
			if last == "config" || last == "config.worktree" || slices.Contains(r[1:], "hooks") || len(r) > 2 && r[len(r)-2] == "info" && last == "attributes" {
				return true
			}
		}
	}
	return false
}

// scratchPath reports whether path is in a temporary folder, where an agent
// makes repositories of its own.
func scratchPath(path string) bool {
	dirs := []string{"/tmp", "/private/tmp", "/var/folders", "/private/var/folders"}
	if t := strings.TrimRight(os.TempDir(), "/"); t != "" {
		dirs = append(dirs, t)
	}
	return slices.ContainsFunc(dirs, func(d string) bool { return strings.HasPrefix(path, d+"/") })
}

// writesTo is what the program base, run with args, writes, moves or
// removes, with the word for it: a copy's or link's destination; every
// path given to mv, rm, tee, chmod and chown; dd's of=; the files sed and
// perl edit in place.
func writesTo(base string, args []string) (verb string, paths []string) {
	switch strings.TrimPrefix(base, "g") {
	case "cp", "install", "ln", "rsync", "ditto", "scp":
		opts, rest := argv(args, "tSmogB", []string{"target-directory", "suffix", "mode", "owner", "group", "backup"})
		if t := opts["-t"] + opts["--target-directory"]; t != "" {
			return "writes into", []string{t}
		}
		if len(rest) < 2 {
			return "", nil
		}
		return "writes", rest[len(rest)-1:]
	case "mv":
		_, rest := argv(args, "", nil)
		return "moves", rest
	case "rm", "unlink", "trash":
		_, rest := argv(args, "", nil)
		return "removes", rest
	case "tee":
		_, rest := argv(args, "", nil)
		return "writes", rest
	case "chmod", "chown", "chgrp":
		_, rest := argv(args, "", nil)
		return "changes who may run", rest
	case "dd":
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "of="); ok {
				return "writes", []string{v}
			}
		}
	case "sed", "perl":
		inPlace := slices.ContainsFunc(args, func(a string) bool {
			return a == "--in-place" || strings.HasPrefix(a, "--in-place=") || len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(strings.SplitN(a[1:], "e", 2)[0], 'i')
		})
		if !inPlace {
			return "", nil
		}
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				paths = append(paths, a)
			}
		}
		return "edits", paths
	}
	return "", nil
}

// gitConfig says what git, run with args, changes of what the user's git
// runs: git config setting a key it runs as a command (runsCommand), or
// opening a config file to edit, in any config file but a temporary one.
// git -c KEY=VALUE sets a key for one command only, and runs.
func (g *gitScan) gitConfig(args []string) string {
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			break
		}
		if slices.Contains([]string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env"}, a) {
			i++ // its value
		}
	}
	if i >= len(args) || args[i] != "config" {
		return ""
	}
	opts, rest := argv(args[i+1:], "f", []string{"file", "blob", "type", "default", "comment", "value"})
	has := func(names ...string) bool {
		return slices.ContainsFunc(names, func(n string) bool { _, ok := opts[n]; return ok })
	}
	if f := opts["-f"] + opts["--file"]; f != "" {
		if p, ok := g.path(f); ok && scratchPath(p) {
			return ""
		}
	}
	switch {
	case has("-e", "--edit") || len(rest) > 0 && rest[0] == "edit":
		return "opens a git config file to edit"
	case len(rest) > 0 && rest[0] == "set":
		rest = rest[1:]
	case len(rest) > 0 && slices.Contains([]string{"get", "list", "unset", "rename-section", "remove-section"}, rest[0]):
		return ""
	case has("--get", "--get-all", "--get-regexp", "--get-urlmatch", "--get-color", "--get-colorbool", "-l", "--list",
		"--unset", "--unset-all", "--remove-section", "--rename-section"):
		return ""
	}
	if len(rest) < 2 || !runsCommand(rest[0], rest[1]) {
		return ""
	}
	return "sets git's " + rest[0] + " to " + gitValue(rest[1])
}

// gitValue quotes a value for the user to read, cut short when long.
func gitValue(v string) string {
	if len(v) > 120 {
		v = v[:120] + "…"
	}
	return "“" + v + "”"
}

// gitCommandKeys are git settings whose value git runs as a command, or
// that point git at hooks or at more settings. A * stands for a name, which
// may hold dots (credential.https://example.com.helper).
var gitCommandKeys = []string{
	"core.fsmonitor", "core.hookspath", "core.sshcommand", "core.pager", "core.editor", "core.askpass", "core.gitproxy",
	"sequence.editor", "diff.external", "credential.helper", "credential.*.helper", "gpg.program", "gpg.*.program",
	"include.path", "includeif.*.path", "init.templatedir", "interactive.difffilter", "uploadpack.packobjectshook",
	"diff.*.textconv", "diff.*.command", "filter.*.clean", "filter.*.smudge", "filter.*.process", "merge.*.driver",
	"remote.*.uploadpack", "remote.*.receivepack", "difftool.*.cmd", "mergetool.*.cmd", "browser.*.cmd", "man.*.cmd",
	"web.browser", "sendemail.smtpserver", "sendemail.sendmailcmd", "sendemail.tocmd", "sendemail.cccmd",
	"pager.*", "alias.*",
}

// runsCommand reports whether setting key to value has git run a command:
// one of gitCommandKeys, short of an empty value, an alias to a git command
// (only ! runs a shell), a yes or no where git takes one (core.fsmonitor,
// pager.*) and plain ssh for core.sshCommand (plainSSH).
func runsCommand(key, value string) bool {
	key = strings.ToLower(key)
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	for _, k := range gitCommandKeys {
		pre, suf, wild := strings.Cut(k, "*")
		switch {
		case !wild && key != k:
			continue
		case wild && !(strings.HasPrefix(key, pre) && strings.HasSuffix(key, suf) && len(key) > len(pre)+len(suf)):
			continue
		case k == "alias.*":
			return strings.HasPrefix(v, "!")
		case k == "core.fsmonitor" || k == "pager.*":
			return !slices.Contains([]string{"true", "false", "yes", "no", "on", "off", "1", "0"}, strings.ToLower(v))
		case k == "core.sshcommand":
			return !plainSSH(v)
		}
		return true
	}
	return false
}

// plainSSH reports whether value runs ssh itself with options that run
// nothing else: no shell syntax, no quotes, no other config file (-F), and
// no ProxyCommand, LocalCommand, KnownHostsCommand, Match or Include.
func plainSSH(value string) bool {
	if strings.ContainsAny(value, ";&|`$()<>\\'\"\n") {
		return false
	}
	f := strings.Fields(value)
	if len(f) == 0 || filepath.Base(f[0]) != "ssh" {
		return false
	}
	for _, w := range f[1:] {
		lw := strings.ToLower(w)
		if strings.HasPrefix(w, "-F") || slices.ContainsFunc([]string{"proxycommand", "localcommand", "knownhostscommand", "match", "include"}, func(s string) bool { return strings.Contains(lw, s) }) {
			return false
		}
	}
	return true
}
