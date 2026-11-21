package habits

import (
	"path"
	"regexp"
	"strings"

	"github.com/irie-people/irie/internal/brand"
)

// Family names a shell command by its first program and, for a program
// with subcommands, the subcommand: git log, sed -n, irie ctl list,
// tools/live.sh. Assignments, cd, echo and the like before it are passed
// over, and so are the bodies of heredocs, which hold scripts and text,
// not commands.
func Family(cmd string) string {
	for _, seg := range segments(stripHeredocs(cmd)) {
		if f := segFamily(seg); f != "" {
			return f
		}
	}
	return "(none)"
}

// heredocRE finds the start of a heredoc and its delimiter: <<EOF,
// <<-'EOF' or <<"EOF", but not a here-string, <<<.
var heredocRE = regexp.MustCompile(`(?:^|[^<])<<-?\s*(?:'([^']+)'|"([^"]+)"|\\?([A-Za-z_][A-Za-z0-9_]*))`)

// stripHeredocs drops the bodies of a command's heredocs.
func stripHeredocs(s string) string {
	var out, pending []string
	for _, l := range strings.Split(s, "\n") {
		if len(pending) > 0 {
			if strings.TrimLeft(l, "\t") == pending[0] {
				pending = pending[1:]
			}
			continue
		}
		out = append(out, l)
		for _, m := range heredocRE.FindAllStringSubmatch(l, -1) {
			pending = append(pending, m[1]+m[2]+m[3])
		}
	}
	return strings.Join(out, "\n")
}

// segments splits a command into the words of each simple command, at
// the ;, &, |, newlines and parentheses that are not quoted. A command
// substitution, $(...), stays inside its word; a comment is dropped.
func segments(s string) [][]string {
	var segs [][]string
	var words []string
	var w strings.Builder
	in := false
	word := func() {
		if in {
			words = append(words, w.String())
			w.Reset()
			in = false
		}
	}
	seg := func() {
		word()
		if len(words) > 0 {
			segs = append(segs, words)
			words = nil
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				j = len(s) - i - 1
			}
			w.WriteString(s[i+1 : i+1+j])
			in = true
			i += j + 1
		case c == '"':
			j := i + 1
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			w.WriteString(strings.ReplaceAll(s[i+1:min(j, len(s))], `\"`, `"`))
			in = true
			i = j
		case c == '\\' && i+1 < len(s):
			if s[i+1] != '\n' {
				w.WriteByte(s[i+1])
				in = true
			}
			i++
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			depth, j := 0, i+1
			for ; j < len(s); j++ {
				if s[j] == '(' {
					depth++
				} else if s[j] == ')' {
					if depth--; depth == 0 {
						break
					}
				}
			}
			w.WriteString(s[i:min(j+1, len(s))])
			in = true
			i = j
		case c == '&' && (i > 0 && s[i-1] == '>' || i+1 < len(s) && s[i+1] == '>'):
			w.WriteByte(c) // a redirection: 2>&1, &>
			in = true
		case c == ' ' || c == '\t':
			word()
		case c == '\n' || c == ';' || c == '&' || c == '|' || c == '(' || c == ')':
			seg()
		case c == '#' && !in:
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				i = len(s)
			} else {
				i += j - 1
			}
		default:
			w.WriteByte(c)
			in = true
		}
	}
	seg()
	return segs
}

var (
	assignRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	durRE    = regexp.MustCompile(`^[0-9.]+[smhd]?$`)
	subRE    = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	toolRE   = regexp.MustCompile(`(?:^|/)((?:tools|mac|\.github|ios|site)/[\w./-]+)$`)
)

// noise is the commands that say nothing of what a call is for.
var noise = map[string]bool{"cd": true, "pushd": true, "export": true, "set": true, "true": true, "false": true, "echo": true, "printf": true, "local": true, "unset": true, ":": true, "trap": true, "then": true, "else": true, "elif": true, "fi": true, "do": true, "done": true, "{": true, "}": true, "source": true, ".": true, "esac": true}

// wrappers run the command after them, which is the one that counts.
var wrappers = map[string]bool{"if": true, "!": true, "timeout": true, "gtimeout": true, "nice": true, "env": true, "command": true, "exec": true, "time": true, "caffeinate": true, "nohup": true, "sudo": true}

// subs is how many subcommand words name a call of a program.
var subs = map[string]int{"git": 1, "go": 1, "gh": 2, "cargo": 1, "npm": 1, "pnpm": 1, "bun": 1, "uv": 1, "docker": 1, "brew": 1, "tmux": 1, "launchctl": 1, "ollama": 1, "swift": 1, "xcrun": 1, "defaults": 1, "kubectl": 1, "tools/code.sh": 1}

// segFamily names one simple command, or says nothing for noise.
func segFamily(ws []string) string {
	for len(ws) > 0 && (assignRE.MatchString(ws[0]) || wrappers[ws[0]]) {
		wrapper := wrappers[ws[0]]
		ws = ws[1:]
		for wrapper && len(ws) > 0 && (strings.HasPrefix(ws[0], "-") || durRE.MatchString(ws[0]) || assignRE.MatchString(ws[0])) {
			ws = ws[1:]
		}
	}
	if len(ws) == 0 || noise[ws[0]] {
		return ""
	}
	p := prog(ws[0])
	if p == "sh" || p == "bash" || p == "zsh" {
		for i, w := range ws[1:] {
			if !strings.HasPrefix(w, "-") {
				if strings.HasSuffix(w, ".sh") {
					p, ws = prog(w), ws[i+1:]
				}
				break
			}
		}
	}
	if p == "sed" {
		for _, w := range ws[1:min(3, len(ws))] {
			if strings.HasPrefix(w, "-i") {
				return "sed -i"
			}
		}
		for _, w := range ws[1:min(3, len(ws))] {
			if w == "-n" {
				return "sed -n"
			}
		}
		return "sed"
	}
	n := subs[p]
	if p == brand.Slug {
		n = 1
		if len(ws) > 1 && ws[1] == "ctl" {
			n = 2
		}
	}
	parts := []string{p}
	for i := 1; i < len(ws) && len(parts) <= n; i++ {
		w := ws[i]
		if strings.HasPrefix(w, "-") {
			if p == "git" && (w == "-C" || w == "-c") {
				i++
			}
			continue
		}
		if !subRE.MatchString(w) {
			break
		}
		parts = append(parts, w)
	}
	return strings.Join(parts, " ")
}

// prog names a program: irie however it is reached, a repository tool
// by its path in the repository, anything else by its base name.
func prog(w string) string {
	bin := brand.Env("BIN")
	if w == "$"+bin || w == "${"+bin+"}" || w == brand.Slug || strings.HasSuffix(w, "/bin/"+brand.Slug) {
		return brand.Slug
	}
	if m := toolRE.FindStringSubmatch(w); m != nil {
		return m[1]
	}
	if strings.Contains(w, "/") {
		return path.Base(w)
	}
	return w
}
