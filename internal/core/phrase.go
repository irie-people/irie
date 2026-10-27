package core

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Plain words for tool calls. The sidebar's rows and the Mac app's
// conversation say "Running the tests" where a terminal shows "Bash · go
// test ./...". The vocabulary is Claude Code's: its tool names (the tools
// reference) and their inputs (the Agent SDK's TypeScript reference).

// Phrase says what a tool call does while it runs: "Running the tests",
// "Editing main.rs", "Reading Store.swift", "Searching for “Theme”". A
// question says what it asks: "Asks: which colour should the button be?".
// tool is the tool's name and input its tool_input. An input that is
// missing or unexpected still gives words, such as "Editing a file".
func Phrase(tool string, input json.RawMessage) string { return wordsFor(tool, input).doing() }

// PhraseDone says what a tool call did once it has run: "Ran the tests",
// "Edited main.rs".
func PhraseDone(tool string, input json.RawMessage) string { return wordsFor(tool, input).done() }

// PhraseAsk says what a permission prompt for a tool call asks: "Asks: may
// it run rm -rf build?", "Asks: may it edit main.rs?".
func PhraseAsk(tool string, input json.RawMessage) string { return wordsFor(tool, input).ask() }

// words are a tool call in plain words: a verb in its base form and what it
// acts on, or a question for the user.
type words struct {
	verb, object string // "run", "the tests"; a verb may end in a particle, as "look for" does
	question     string // what the call asks the user
	command      string // the command a permission prompt names, shortened
}

func say(verb, object string) words { return words{verb: verb, object: object} }

func (w words) doing() string {
	if w.question != "" {
		return "Asks: " + w.question
	}
	return sentence(conjugate(w.verb, ing), w.object)
}

func (w words) done() string {
	if w.question != "" && w.verb == "" {
		return "Asked: " + w.question
	}
	return sentence(conjugate(w.verb, past), w.object)
}

// failed says what a call that failed tried: "Couldn't run the tests".
func (w words) failed() string {
	if w.question != "" && w.verb == "" {
		return "Asked: " + w.question
	}
	return "Couldn’t " + strings.TrimSpace(w.verb+" "+w.object)
}

func (w words) ask() string {
	switch {
	case w.question != "":
		return "Asks: " + w.question
	case w.command != "":
		return "Asks: may it run " + w.command + "?"
	}
	return "Asks: may it " + strings.TrimSpace(w.verb+" "+w.object) + "?"
}

// sentence starts a phrase with a capital and adds the object.
func sentence(verb, object string) string {
	r, n := utf8.DecodeRuneInString(verb)
	s := string(unicode.ToUpper(r)) + verb[n:]
	if object != "" {
		s += " " + object
	}
	return s
}

// conjugate puts the first word of a verb, such as "look" of "look for",
// in another form.
func conjugate(verb string, form func(string) string) string {
	if first, rest, ok := strings.Cut(verb, " "); ok {
		return form(first) + " " + rest
	}
	return form(verb)
}

// irregular holds the -ing and past forms that the rules of ing and past
// get wrong; an empty form follows the rules.
var irregular = map[string][2]string{
	"be": {"being", "was"}, "begin": {"", "began"}, "bind": {"", "bound"}, "bring": {"", "brought"},
	"build": {"", "built"}, "buy": {"", "bought"}, "catch": {"", "caught"}, "choose": {"", "chose"},
	"cut": {"", "cut"}, "deal": {"", "dealt"}, "dig": {"", "dug"}, "do": {"", "did"}, "draw": {"", "drew"},
	"feed": {"", "fed"}, "feel": {"", "felt"}, "find": {"", "found"}, "forget": {"", "forgot"},
	"freeze": {"", "froze"}, "get": {"", "got"}, "give": {"", "gave"}, "go": {"", "went"},
	"grow": {"", "grew"}, "hang": {"", "hung"}, "hide": {"", "hid"}, "hold": {"", "held"},
	"keep": {"", "kept"}, "know": {"", "knew"}, "lay": {"", "laid"}, "lead": {"", "led"},
	"leave": {"", "left"}, "let": {"", "let"}, "light": {"", "lit"}, "lose": {"", "lost"},
	"make": {"", "made"}, "mean": {"", "meant"}, "meet": {"", "met"}, "overwrite": {"", "overwrote"},
	"pay": {"", "paid"}, "put": {"", "put"}, "quit": {"quitting", "quit"}, "read": {"", "read"},
	"rebuild": {"", "rebuilt"}, "redo": {"", "redid"}, "reread": {"", "reread"}, "rerun": {"", "reran"},
	"reset": {"", "reset"}, "rewrite": {"", "rewrote"}, "run": {"", "ran"}, "say": {"", "said"},
	"see": {"", "saw"}, "seek": {"", "sought"}, "sell": {"", "sold"}, "send": {"", "sent"},
	"set": {"", "set"}, "shake": {"", "shook"}, "shoot": {"", "shot"}, "shut": {"", "shut"},
	"sit": {"", "sat"}, "sleep": {"", "slept"}, "slide": {"", "slid"}, "speak": {"", "spoke"},
	"spend": {"", "spent"}, "spin": {"", "spun"}, "split": {"", "split"}, "spread": {"", "spread"},
	"stand": {"", "stood"}, "steal": {"", "stole"}, "stick": {"", "stuck"}, "strike": {"", "struck"},
	"sweep": {"", "swept"}, "take": {"", "took"}, "teach": {"", "taught"}, "tear": {"", "tore"},
	"tell": {"", "told"}, "think": {"", "thought"}, "throw": {"", "threw"}, "understand": {"", "understood"},
	"undo": {"", "undid"}, "upset": {"", "upset"}, "wake": {"", "woke"}, "wear": {"", "wore"},
	"win": {"", "won"}, "write": {"", "wrote"},
}

// doubled are verbs of more than one syllable that double their last
// letter: committing, formatted.
var doubled = map[string]bool{
	"admit": true, "begin": true, "commit": true, "control": true, "debug": true, "emit": true,
	"equip": true, "forget": true, "format": true, "occur": true, "omit": true, "permit": true,
	"prefer": true, "program": true, "refer": true, "regret": true, "remap": true, "rerun": true,
	"submit": true, "transfer": true, "unpin": true, "unwrap": true, "unzip": true, "upset": true,
}

// ing is a verb's -ing form: running, making, copying.
func ing(v string) string {
	if f := irregular[v][0]; f != "" {
		return f
	}
	switch {
	case strings.HasSuffix(v, "ie"):
		return v[:len(v)-2] + "ying"
	case len(v) > 2 && strings.HasSuffix(v, "e") && !strings.HasSuffix(v, "ee") && !strings.HasSuffix(v, "ye") && !strings.HasSuffix(v, "oe"):
		return v[:len(v)-1] + "ing"
	case doubles(v):
		return v + v[len(v)-1:] + "ing"
	}
	return v + "ing"
}

// past is a verb's past form: ran, made, copied.
func past(v string) string {
	if f := irregular[v][1]; f != "" {
		return f
	}
	switch {
	case strings.HasSuffix(v, "e"):
		return v + "d"
	case len(v) > 1 && strings.HasSuffix(v, "y") && !strings.ContainsRune("aeiou", rune(v[len(v)-2])):
		return v[:len(v)-1] + "ied"
	case doubles(v):
		return v + v[len(v)-1:] + "ed"
	}
	return v + "ed"
}

// doubles reports whether a verb doubles its last letter before -ing and
// -ed: a verb of one syllable that ends in a consonant, a vowel and a
// consonant other than w, x or y (stopping, planned), or one in doubled.
func doubles(v string) bool {
	if doubled[v] {
		return true
	}
	n := len(v)
	if n < 3 || syllables(v) != 1 {
		return false
	}
	vowel := func(c byte) bool { return strings.IndexByte("aeiou", c) >= 0 }
	return !vowel(v[n-1]) && strings.IndexByte("wxy", v[n-1]) < 0 && vowel(v[n-2]) && !vowel(v[n-3])
}

// syllables counts a word's groups of vowels.
func syllables(v string) int {
	n, in := 0, false
	for i := 0; i < len(v); i++ {
		isVowel := strings.IndexByte("aeiouy", v[i]) >= 0 && (v[i] != 'y' || i > 0)
		if isVowel && !in {
			n++
		}
		in = isVowel
	}
	return n
}

// toolInput is a tool call's input, read leniently: a field of an
// unexpected type reads as empty.
type toolInput map[string]json.RawMessage

func (in toolInput) str(key string) string {
	var s string
	json.Unmarshal(in[key], &s)
	return s
}

func wordsFor(tool string, raw json.RawMessage) words {
	var in toolInput
	json.Unmarshal(raw, &in)
	w := toolWords(tool, in)
	if w.verb == "" && w.question == "" {
		w = say("work", "")
	}
	return w
}

// fixedWords are the words of the tools whose input adds nothing to them.
var fixedWords = map[string]words{
	"Artifact":             say("publish", "a page"),
	"BashOutput":           say("check", "a background command"),
	"CronCreate":           say("schedule", "a task"),
	"CronDelete":           say("cancel", "a scheduled task"),
	"CronList":             say("check", "its scheduled tasks"),
	"EndConversation":      say("end", "the conversation"),
	"EnterPlanMode":        say("make", "a plan"),
	"EnterWorktree":        say("move into", "a worktree"),
	"ExitWorktree":         say("leave", "the worktree"),
	"KillBash":             say("stop", "a background command"),
	"KillShell":            say("stop", "a background command"),
	"ListAgents":           say("check", "the other agents"),
	"ListMcpResourcesTool": say("list", "its resources"),
	"LSP":                  say("check", "the code"),
	"Monitor":              say("keep", "watch in the background"),
	"PushNotification":     say("send", "you a notification"),
	"ReadMcpResourceTool":  say("read", "a resource"),
	"RemoteTrigger":        say("schedule", "a cloud task"),
	"ReportFindings":       say("report", "its findings"),
	"ScheduleWakeup":       say("schedule", "a wake-up"),
	"SendFeedback":         say("draft", "feedback"),
	"SendUserFile":         say("send", "you a file"),
	"ShareOnboardingGuide": say("share", "the onboarding guide"),
	"SubagentHandback":     say("hand back", "its report"),
	"TaskCreate":           say("add", "a task"),
	"TaskGet":              say("check", "a task"),
	"TaskList":             say("check", "its tasks"),
	"TaskOutput":           say("check", "a task’s output"),
	"TaskStop":             say("stop", "a task"),
	"TaskUpdate":           say("update", "a task"),
	"TodoWrite":            say("update", "its to-do list"),
	"ToolSearch":           say("load", "more tools"),
	"WaitForMcpServers":    say("wait for", "its tools"),
	"Workflow":             say("run", "a workflow"),
}

func toolWords(tool string, in toolInput) words {
	file := func(key, otherwise string) string { return or(base(in.str(key)), otherwise) }
	switch tool {
	case "Bash", "PowerShell":
		return shellWords(in.str("command"), in.str("description"))
	case "Read":
		return say("read", file("file_path", "a file"))
	case "NotebookRead":
		return say("read", file("notebook_path", "a notebook"))
	case "Write":
		return say("write", file("file_path", "a file"))
	case "Edit", "MultiEdit":
		return say("edit", file("file_path", "a file"))
	case "NotebookEdit":
		return say("edit", file("notebook_path", "a notebook"))
	case "Grep":
		if p := in.str("pattern"); p != "" {
			return say("search for", quote(short(p, 40)))
		}
		return say("search", "the files")
	case "Glob":
		return globWords(in.str("pattern"))
	case "LS":
		return say("look in", file("path", "a folder"))
	case "WebFetch":
		return say("read", or(hostOf(in.str("url")), "a web page"))
	case "WebSearch":
		if q := in.str("query"); q != "" {
			return say("search", "the web for "+quote(short(q, 40)))
		}
		return say("search", "the web")
	case "Agent", "Task":
		if d := in.str("description"); d != "" {
			return say("ask", "a helper to "+lowerFirst(short(d, 60)))
		}
		return say("ask", "a helper")
	case "AskUserQuestion":
		var q struct {
			Questions []struct {
				Question string `json:"question"`
			} `json:"questions"`
		}
		if json.Unmarshal(in["questions"], &q.Questions) == nil && len(q.Questions) > 0 && strings.TrimSpace(q.Questions[0].Question) != "" {
			return words{question: lowerFirst(short(q.Questions[0].Question, 120))}
		}
		return say("ask", "you a question")
	case "ExitPlanMode":
		return words{verb: "share", object: "its plan", question: "may it go ahead with its plan?"}
	case "Skill":
		if s := in.str("skill"); s != "" {
			return say("use", "the "+short(s, 40)+" skill")
		}
		return say("use", "a skill")
	case "SendMessage":
		return say("message", or(short(in.str("to"), 40), "another agent"))
	}
	if w, ok := fixedWords[tool]; ok {
		return w
	}
	if rest, ok := strings.CutPrefix(tool, "mcp__"); ok {
		server, name, _ := strings.Cut(rest, "__")
		object := humanize(server)
		if name != "" {
			object += " (" + humanize(name) + ")"
		}
		return say("use", object)
	}
	if tool == "" {
		return words{}
	}
	return say("use", tool)
}

// globWords are the words of a file name pattern: "*.swift" is looking for
// .swift files.
func globWords(pattern string) words {
	if m := extGlob.FindStringSubmatch(pattern); m != nil {
		return say("look for", "."+m[1]+" files")
	}
	if pattern == "" {
		return say("look for", "files")
	}
	if !strings.ContainsAny(pattern, "*?[{") {
		return say("look for", base(pattern))
	}
	return say("look for", quote(short(pattern, 40)))
}

var extGlob = regexp.MustCompile(`^(?:.*/)?\*\.([A-Za-z0-9]+)$`)

// Ranks of the simple commands of a command line: the highest speaks for
// the line.
const (
	rankNone    = iota // cd or export: says nothing
	rankFilter         // echo, sleep, or a filter in a pipe
	rankUnknown        // a program without words of its own
	rankKnown          // a program with words
	rankMajor          // a build, a commit or an install
	rankTests          // a test run
)

// shellWords reads a shell command line. Its busiest simple command speaks
// for it, so a test run beats the cd before it. A command Irie has no words
// for is said with the description Claude Code's model gave it, when there
// is one ("Fetch the latest prices").
func shellWords(cmd, desc string) words {
	w, rank := commandWords(cmd)
	if rank <= rankUnknown {
		if d, ok := describedWords(desc); ok {
			w = d
		}
	}
	if w.verb == "" {
		w = say("run", "a command")
	}
	w.command = short(firstLine(cmd), 60)
	return w
}

func commandWords(line string) (words, int) {
	var w words
	rank := rankNone
	for _, s := range splitCommand(line) {
		if sw, r := simpleWords(s); r > rank {
			w, rank = sw, r
		}
	}
	return w, rank
}

// imperative are the regular verbs a command's description may start with.
var imperative = map[string]bool{}

func init() {
	for _, v := range strings.Fields(`add analyze append apply archive audit benchmark bump call cancel
		capture change check clean clear clone close collect commit compare compile compress configure
		confirm connect convert copy count create debug decode delete deploy detect diff disable display
		download dump edit enable encode ensure examine execute explore export extract fetch filter fix
		format generate grep import initialize inspect install kill launch link lint list load locate log
		look measure merge migrate modify monitor mount move open parse patch ping poll preview print
		probe profile prune publish pull push query rebase record refresh register reload remove rename
		render replace request resolve restart restore retry revert review save scan search select serve
		show sign simulate skip sort start stash stop strip submit summarize switch sync tag tail test
		tidy touch trace trigger trim try unpack unstage unzip update upgrade upload use validate verify
		view wait watch zip`) {
		imperative[v] = true
	}
}

// describedWords reads the description of a command, which Claude Code's
// model writes in the imperative, as words.
func describedWords(desc string) (words, bool) {
	desc = strings.TrimRight(strings.Join(strings.Fields(desc), " "), ".")
	first, rest, _ := strings.Cut(desc, " ")
	v := strings.ToLower(first)
	if _, ok := irregular[v]; !ok && !imperative[v] {
		return words{}, false
	}
	return say(v, short(rest, 60)), true
}

// token is one word of a command line without its quotes. bare says its
// first character was unquoted, as an operator's or a redirection's is.
type token struct {
	s    string
	bare bool
}

// simple is one simple command of a command line: its words without
// redirections, and the file its output goes to.
type simple struct {
	args []string
	out  string
}

// splitCommand splits a command line into simple commands at unquoted &&,
// ||, ;, |, &, parentheses and newlines, and drops comments and heredoc
// bodies. It reads a command for its words, not to run it.
func splitCommand(line string) []simple {
	var (
		cmds    []simple
		toks    []token
		cur     strings.Builder
		inWord  bool
		bare    bool
		heredoc []string // the delimiters of bodies that follow this line
		delim   bool     // the next word is a heredoc's delimiter
	)
	begin := func(unquoted bool) {
		if !inWord {
			inWord, bare = true, unquoted
		}
	}
	endWord := func() {
		if !inWord {
			return
		}
		s := cur.String()
		switch {
		case delim:
			heredoc, delim = append(heredoc, s), false
		case bare && strings.HasPrefix(s, "<<") && !strings.HasPrefix(s, "<<<"):
			if d := strings.TrimPrefix(s[2:], "-"); d != "" {
				heredoc = append(heredoc, d)
			} else {
				delim = true
			}
		}
		toks = append(toks, token{s, bare})
		cur.Reset()
		inWord = false
	}
	endCmd := func() {
		endWord()
		if len(toks) > 0 {
			cmds = append(cmds, simpleOf(toks))
		}
		toks, delim = nil, false
	}
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == '\\':
			if i+1 < len(rs) {
				i++
				if rs[i] != '\n' {
					begin(false)
					cur.WriteRune(rs[i])
				}
			}
		case c == '\'':
			begin(false)
			for i++; i < len(rs) && rs[i] != '\''; i++ {
				cur.WriteRune(rs[i])
			}
		case c == '"':
			begin(false)
			for i++; i < len(rs) && rs[i] != '"'; i++ {
				if rs[i] == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]) {
					i++
				}
				cur.WriteRune(rs[i])
			}
		case c == '`' || c == '$' && i+1 < len(rs) && rs[i+1] == '(':
			begin(true)
			end := substitutionEnd(rs, i)
			cur.WriteString(string(rs[i : end+1]))
			i = end
		case c == '#' && !inWord:
			for i+1 < len(rs) && rs[i+1] != '\n' {
				i++
			}
		case c == '\n':
			endCmd()
			for _, d := range heredoc {
				i = heredocEnd(rs, i, d)
			}
			heredoc = nil
		case c == '&' && inWord && bare && (strings.HasSuffix(cur.String(), ">") || strings.HasSuffix(cur.String(), "<")),
			c == '&' && i+1 < len(rs) && rs[i+1] == '>',
			c == '|' && inWord && bare && strings.HasSuffix(cur.String(), ">"):
			// 2>&1, &>file and >|file are redirections, not operators.
			begin(true)
			cur.WriteRune(c)
		case strings.ContainsRune(";&|()", c):
			endCmd()
		case unicode.IsSpace(c):
			endWord()
		default:
			begin(true)
			cur.WriteRune(c)
		}
	}
	endCmd()
	return cmds
}

// substitutionEnd is the index that closes the command substitution that
// starts at i, $( or a backquote, or the last index when nothing does.
func substitutionEnd(rs []rune, i int) int {
	if rs[i] == '`' {
		for j := i + 1; j < len(rs); j++ {
			if rs[j] == '\\' {
				j++
			} else if rs[j] == '`' {
				return j
			}
		}
		return len(rs) - 1
	}
	depth := 0
	for j := i + 1; j < len(rs); j++ {
		switch rs[j] {
		case '\\':
			j++
		case '\'':
			for j++; j < len(rs) && rs[j] != '\''; j++ {
			}
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return len(rs) - 1
}

// heredocEnd skips the body of a heredoc that starts after the newline at
// i, and returns the index of the newline that ends its delimiter line.
func heredocEnd(rs []rune, i int, delim string) int {
	for i < len(rs)-1 {
		start := i + 1
		end := start
		for end < len(rs) && rs[end] != '\n' {
			end++
		}
		if strings.TrimSpace(string(rs[start:end])) == delim {
			return end
		}
		i = end
	}
	return len(rs) - 1
}

var redirection = regexp.MustCompile(`^(\d*|&)(>>|>\||>|<)(&\d*-?)?(.*)$`)

func simpleOf(toks []token) simple {
	var s simple
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.bare && strings.HasPrefix(t.s, "<<") {
			if t.s == "<<" || t.s == "<<-" || t.s == "<<<" {
				i++ // the delimiter, or the string
			}
			continue
		}
		if m := redirection.FindStringSubmatch(t.s); t.bare && m != nil {
			target := m[4]
			if target == "" && m[3] == "" && i+1 < len(toks) {
				i++
				target = toks[i].s
			}
			if m[2] != "<" && (m[1] == "" || m[1] == "1" || m[1] == "&") && m[3] == "" && target != "/dev/null" {
				s.out = target
			}
			continue
		}
		s.args = append(s.args, t.s)
	}
	return s
}

var envAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// wrappers run the command that follows them, as do the shell's keywords
// before a command. Each lists its flags that take a value.
var wrappers = map[string]string{
	"sudo": "ugCDhpr", "doas": "u", "env": "uSC", "time": "", "nohup": "", "exec": "a", "nice": "n",
	"timeout": "sk", "xargs": "IiLnPsdEa", "caffeinate": "tw", "stdbuf": "ioe", "command": "", "builtin": "",
	"if": "", "then": "", "elif": "", "else": "", "while": "", "until": "", "do": "", "!": "",
}

// unwrap drops a command's variable assignments and wrappers.
func unwrap(args []string) []string {
	for len(args) > 0 {
		a := args[0]
		if envAssignment.MatchString(a) {
			args = args[1:]
			continue
		}
		valued, ok := wrappers[a]
		if !ok || a == "command" && len(args) > 1 && (args[1] == "-v" || args[1] == "-V") {
			return args
		}
		args = args[1:]
		for len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "-" {
			f := args[0]
			args = args[1:]
			if len(f) == 2 && strings.IndexByte(valued, f[1]) >= 0 && len(args) > 0 {
				args = args[1:]
			}
		}
		if a == "timeout" && len(args) > 0 {
			args = args[1:] // the duration
		}
	}
	return args
}

func simpleWords(s simple) (words, int) {
	args := unwrap(s.args)
	if len(args) == 0 {
		return words{}, rankNone
	}
	prog, rest := path.Base(args[0]), args[1:]
	if slices.Contains(rest, "--help") {
		return say("read", "the help for "+prog), rankKnown
	}
	if len(rest) == 1 && rest[0] == "--version" {
		return say("check", "the version of "+prog), rankKnown
	}
	if w, r, ok := programWords(prog, rest, s.out); ok {
		return w, r
	}
	if strings.HasPrefix(prog, "python") {
		w, r, _ := interpreterWords("python", rest)
		return w, r
	}
	if strings.Contains(args[0], "/") {
		return scriptWords(prog)
	}
	return say("run", prog), rankUnknown
}

// The words of tests, builds, checks and formatting, whatever runs them.
var (
	testWords   = say("run", "the tests")
	buildWords  = say("build", "the project")
	checkWords  = say("check", "the code")
	formatWords = say("format", "the code")
)

// programWords has words for the programs agents run most.
func programWords(prog string, args []string, out string) (words, int, bool) {
	ops := operands(args, "")
	first := ""
	if len(ops) > 0 {
		first = ops[0]
	}
	switch prog {
	case "cd", "pushd", "popd", "export", "set", "unset", "source", ".", "true", "false", ":", "alias",
		"shopt", "trap", "local", "declare", "typeset", "for", "case", "esac", "fi", "done", "{", "}",
		"clear", "wait", "history", "umask", "ulimit", "read", "shift", "return", "exit", "break", "continue", "eval":
		return words{}, rankNone, true
	case "echo", "printf":
		if out != "" {
			return say("write", base(out)), rankKnown, true
		}
		return say("print", "a message"), rankFilter, true
	case "sleep":
		return say("wait", seconds(first)), rankFilter, true
	case "test", "[", "[[":
		return say("check", "a condition"), rankFilter, true
	case "pwd":
		return say("check", "where it is"), rankFilter, true
	case "which", "type", "whereis", "command":
		return say("check for", or(last(ops), "a program")), rankFilter, true
	case "date":
		return say("check", "the time"), rankFilter, true
	case "uname", "sw_vers", "hostname", "arch", "system_profiler":
		return say("check", "the system"), rankFilter, true
	case "printenv":
		return say("check", "the environment"), rankFilter, true
	case "whoami", "id", "groups":
		return say("check", "the user"), rankFilter, true
	case "df", "du":
		return say("check", "disk space"), rankKnown, true
	case "ls", "eza", "exa", "lsd", "tree":
		if first != "" {
			return say("look in", base(first)), rankKnown, true
		}
		return say("look at", "the files"), rankKnown, true
	case "find", "bfs":
		return findWords(args), rankKnown, true
	case "fd", "fdfind":
		if first != "" {
			return say("look for", quote(short(first, 32))), rankKnown, true
		}
		return say("look for", "files"), rankKnown, true
	case "cat", "bat", "less", "more", "nl", "xxd", "hexdump", "od", "strings", "jq", "yq":
		if prog == "cat" && out != "" {
			return say("write", base(out)), rankKnown, true
		}
		if prog == "jq" || prog == "yq" {
			ops = operands(args, "f", "--arg", "--argjson") // the first operand is the filter
			if len(ops) > 0 {
				ops = ops[1:]
			}
		}
		if len(ops) > 0 {
			return say("read", several(ops)), rankKnown, true
		}
		return say("read", "the output"), rankFilter, true
	case "head", "tail":
		ops = operands(args, "nc")
		follow := slices.Contains(args, "-f") || slices.Contains(args, "-F")
		switch {
		case len(ops) > 0 && prog == "tail" && follow:
			return say("watch", base(ops[0])), rankKnown, true
		case len(ops) > 0:
			return say("read", several(ops)), rankKnown, true
		}
		return say("read", "the output"), rankFilter, true
	case "sed", "gsed", "awk", "gawk", "perl":
		return editorWords(prog, args)
	case "grep", "egrep", "fgrep", "rg", "ag", "ack", "ugrep":
		return searchWords(args), rankKnown, true
	case "wc":
		if first != "" {
			return say("count", "the lines in "+several(ops)), rankKnown, true
		}
		return say("count", "the lines"), rankFilter, true
	case "sort", "uniq", "cut", "tr", "column", "rev", "paste", "comm", "fold", "fmt", "expand", "base64":
		return say("read", "the output"), rankFilter, true
	case "tee":
		if first != "" {
			return say("write", several(ops)), rankFilter, true
		}
		return say("read", "the output"), rankFilter, true
	case "diff", "cmp", "colordiff", "difft":
		if len(ops) >= 2 {
			return say("compare", base(ops[0])+" and "+base(ops[1])), rankKnown, true
		}
		return say("compare", "files"), rankKnown, true
	case "mkdir":
		ops = operands(args, "m")
		if len(ops) == 1 {
			return say("make", "the "+base(ops[0])+" folder"), rankKnown, true
		}
		return say("make", "folders"), rankKnown, true
	case "rm", "rmdir", "unlink", "trash", "srm":
		return say("delete", several(ops)), rankKnown, true
	case "mv":
		return say("move", several(allButLast(operands(args, "tS")))), rankKnown, true
	case "cp", "ditto":
		return say("copy", several(allButLast(operands(args, "tS")))), rankKnown, true
	case "ln":
		return say("link", several(allButLast(operands(args, "tS")))), rankKnown, true
	case "touch":
		return say("create", several(operands(args, "dtr"))), rankKnown, true
	case "chmod":
		if len(ops) >= 2 && strings.Contains(ops[0], "+x") {
			return say("make", several(ops[1:])+" runnable"), rankKnown, true
		}
		return say("change", "permissions"), rankKnown, true
	case "chown", "chgrp":
		return say("change", "who owns files"), rankKnown, true
	case "tar", "bsdtar":
		return tarWords(args), rankKnown, true
	case "unzip", "gunzip", "bunzip2", "unxz", "7z":
		return say("unpack", or(base(first), "an archive")), rankKnown, true
	case "zip", "gzip", "bzip2", "xz":
		return say("pack", or(base(first), "files")), rankKnown, true
	case "open", "xdg-open":
		return say("open", or(hostOf(first), base(first), "a file")), rankKnown, true
	case "curl", "wget", "http", "https", "xh":
		return fetchWords(prog, args, out), rankKnown, true
	case "ssh", "mosh":
		host := strings.SplitN(first, ":", 2)[0]
		if _, h, ok := strings.Cut(host, "@"); ok {
			host = h
		}
		return say("connect to", or(host, "a server")), rankKnown, true
	case "scp", "sftp":
		return say("copy", "files over the network"), rankKnown, true
	case "rsync":
		return say("sync", "files"), rankKnown, true
	case "ps", "pgrep", "lsof", "top", "htop", "btop", "pstree", "pidof":
		return say("check", "what’s running"), rankKnown, true
	case "kill", "pkill", "killall":
		if n := operands(args, "sgu"); prog != "kill" && len(n) > 0 {
			return say("stop", n[0]), rankKnown, true
		}
		return say("stop", "a process"), rankKnown, true
	case "git":
		return gitWords(args)
	case "gh":
		return ghWords(ops)
	case "go":
		return goWords(ops)
	case "cargo":
		return cargoWords(first)
	case "swift":
		switch first {
		case "test":
			return testWords, rankTests, true
		case "build":
			return buildWords, rankMajor, true
		case "run":
			return say("run", "the program"), rankKnown, true
		case "package":
			return say("update", "the packages"), rankKnown, true
		case "":
			return say("start", "Swift"), rankUnknown, true
		}
		w, r := scriptWords(base(first))
		return w, r, true
	case "swiftc", "clang", "clang++", "gcc", "g++", "cc", "c++", "javac", "rustc", "ninja", "msbuild", "go-build":
		return buildWords, rankMajor, true
	case "xcodebuild":
		if slices.Contains(args, "test") {
			return testWords, rankTests, true
		}
		return buildWords, rankMajor, true
	case "xcrun":
		w, r := simpleWords(simple{args: args})
		return w, r, true
	case "make", "gmake":
		return makeWords(args)
	case "cmake":
		if slices.Contains(args, "--build") {
			return buildWords, rankMajor, true
		}
		return say("set up", "the build"), rankMajor, true
	case "gradle", "gradlew", "mvn", "mvnw", "dotnet", "zig", "bazel", "bazelisk", "sbt", "mix", "rake", "lein":
		return taskWords(prog, first)
	case "npm", "pnpm", "yarn", "bun":
		return npmWords(prog, args)
	case "deno":
		switch first {
		case "test":
			return testWords, rankTests, true
		case "fmt":
			return formatWords, rankMajor, true
		case "lint", "check":
			return checkWords, rankMajor, true
		case "task":
			return scriptRunWords(prog, second(ops))
		}
		w, r := scriptWords(base(second(ops)))
		return w, r, true
	case "npx", "bunx", "pnpx", "uvx":
		return runnerWords(args)
	case "pip", "pip3", "pipx", "uv", "poetry", "pipenv", "conda", "mamba", "gem", "bundle", "brew", "apt", "apt-get",
		"dnf", "yum", "pacman", "apk", "port", "cabal", "stack", "composer", "opam", "hatch", "pdm", "rye":
		return packageWords(prog, args)
	case "pytest", "py.test", "jest", "vitest", "mocha", "rspec", "phpunit", "ctest", "tox", "nox", "ava", "karma",
		"bats", "ginkgo", "gotestsum":
		return testWords, rankTests, true
	case "playwright", "cypress":
		if first == "test" || first == "run" {
			return testWords, rankTests, true
		}
		return say("run", prog), rankKnown, true
	case "ruff", "biome":
		if first == "format" {
			return formatWords, rankMajor, true
		}
		return checkWords, rankMajor, true
	case "tsc":
		if slices.Contains(args, "--noEmit") {
			return checkWords, rankMajor, true
		}
		return buildWords, rankMajor, true
	case "eslint", "golangci-lint", "staticcheck", "flake8", "pylint", "mypy", "pyright", "swiftlint", "shellcheck",
		"hadolint", "markdownlint", "vale", "revive", "tflint", "stylelint", "rubocop", "oxlint":
		return checkWords, rankMajor, true
	case "prettier", "black", "gofmt", "goimports", "gofumpt", "rustfmt", "swift-format", "swiftformat",
		"clang-format", "shfmt", "isort", "autopep8", "yapf", "dprint":
		return formatWords, rankMajor, true
	case "node", "ruby", "php", "bash", "sh", "zsh", "fish", "dash", "ts-node", "tsx", "lua", "Rscript", "julia":
		return interpreterWords(prog, args)
	case "osascript":
		return say("run", "a script"), rankUnknown, true
	case "docker", "podman", "docker-compose":
		return dockerWords(prog, ops)
	case "kubectl", "helm":
		return say("use", prog), rankKnown, true
	case "tmux", "screen":
		return say("use", prog), rankKnown, true
	case "defaults":
		if first == "write" || first == "delete" {
			return say("change", "a setting"), rankKnown, true
		}
		return say("check", "a setting"), rankKnown, true
	case "plutil", "stat", "file", "md5", "md5sum", "shasum", "sha1sum", "sha256sum", "otool", "nm":
		return say("check", or(base(last(ops)), "a file")), rankKnown, true
	case "codesign":
		return say("sign", or(base(last(ops)), "the app")), rankKnown, true
	case "launchctl":
		return say("check", "background services"), rankKnown, true
	case "claude":
		return say("run", "Claude Code"), rankKnown, true
	case "irie":
		return say("run", "Irie"), rankKnown, true
	case "code", "cursor", "vim", "nvim", "vi", "nano", "emacs", "subl", "zed":
		return say("open", or(base(first), "an editor")), rankKnown, true
	case "man", "tldr":
		return say("read", "the manual for "+or(first, "a command")), rankKnown, true
	case "ollama":
		switch first {
		case "run":
			return say("run", "a local model"), rankKnown, true
		case "pull":
			return say("download", "a model"), rankKnown, true
		case "serve":
			return say("start", "Ollama"), rankKnown, true
		}
		return say("check", "the local models"), rankKnown, true
	case "sqlite3", "psql", "mysql", "redis-cli", "mongosh":
		return say("query", "the database"), rankKnown, true
	}
	return words{}, 0, false
}

// operands are the arguments that are not flags. A short flag in valued,
// such as n for head, takes the next argument as its value, as does a long
// flag in long.
func operands(args []string, valued string, long ...string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(out, args[i+1:]...)
		case strings.HasPrefix(a, "--"):
			if !strings.Contains(a, "=") && slices.Contains(long, a) {
				i++
			}
		case len(a) > 1 && a[0] == '-':
			if len(a) == 2 && strings.IndexByte(valued, a[1]) >= 0 {
				i++
			}
		default:
			out = append(out, a)
		}
	}
	return out
}

func gitWords(args []string) (words, int, bool) {
	// git's own options come before its command.
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if (args[0] == "-C" || args[0] == "-c") && len(args) > 1 {
			args = args[1:]
		}
		args = args[1:]
	}
	if len(args) == 0 {
		return say("run", "git"), rankKnown, true
	}
	sub, rest := args[0], args[1:]
	ops := operands(rest, "")
	first := ""
	if len(ops) > 0 {
		first = ops[0]
	}
	switch sub {
	case "status":
		return say("check", "what changed"), rankKnown, true
	case "diff", "show", "difftool":
		return say("look at", "the changes"), rankKnown, true
	case "log", "shortlog", "reflog", "blame", "whatchanged":
		return say("read", "the history"), rankKnown, true
	case "add":
		return say("stage", "the changes"), rankKnown, true
	case "commit":
		return say("commit", "the changes"), rankMajor, true
	case "push":
		return say("push", "the changes"), rankMajor, true
	case "pull":
		return say("pull", "the latest changes"), rankMajor, true
	case "fetch":
		return say("fetch", "the latest changes"), rankKnown, true
	case "merge":
		if slices.Contains(rest, "--abort") {
			return say("stop", "the merge"), rankMajor, true
		}
		if r := operands(rest, "msX"); len(r) > 0 {
			return say("merge", r[0]), rankMajor, true
		}
		return say("merge", "the branches"), rankMajor, true
	case "rebase":
		return say("rebase", "the branch"), rankMajor, true
	case "checkout", "switch":
		if b := valueOf(rest, "-b", "-B", "-c", "-C", "--orphan"); b != "" {
			return say("make", "the branch "+b), rankKnown, true
		}
		if i := slices.Index(rest, "--"); i >= 0 && i+1 < len(rest) {
			return say("restore", several(rest[i+1:])), rankKnown, true
		}
		if first != "" {
			return say("switch to", first), rankKnown, true
		}
		return say("switch", "branches"), rankKnown, true
	case "branch":
		if b := valueOf(rest, "-d", "-D", "--delete"); b != "" {
			return say("delete", "the branch "+b), rankKnown, true
		}
		if first != "" && len(ops) == len(operands(rest, "")) && !hasAny(rest, "-l", "--list", "-a", "-r", "-v", "-vv", "--show-current", "--contains", "--merged") {
			return say("make", "the branch "+first), rankKnown, true
		}
		return say("list", "the branches"), rankKnown, true
	case "stash":
		switch first {
		case "pop", "apply":
			return say("bring", "back the changes it set aside"), rankKnown, true
		case "list", "show":
			return say("check", "the changes it set aside"), rankKnown, true
		}
		return say("set", "the changes aside"), rankKnown, true
	case "clone":
		return say("clone", or(repoName(first), "a repository")), rankMajor, true
	case "worktree":
		switch first {
		case "add":
			return say("make", "a worktree"), rankKnown, true
		case "remove", "prune":
			return say("remove", "a worktree"), rankKnown, true
		}
		return say("list", "the worktrees"), rankKnown, true
	case "reset":
		switch {
		case slices.Contains(rest, "--hard"):
			return say("throw", "away the changes"), rankMajor, true
		case slices.Contains(rest, "--soft"):
			return say("undo", "the last commit"), rankMajor, true
		}
		return say("unstage", "the changes"), rankKnown, true
	case "restore":
		return say("restore", or(several(operands(rest, "s")), "the files")), rankKnown, true
	case "revert":
		return say("revert", "a commit"), rankMajor, true
	case "cherry-pick":
		return say("cherry-pick", "a commit"), rankMajor, true
	case "tag":
		return say("tag", "a release"), rankKnown, true
	case "init":
		return say("make", "a repository"), rankKnown, true
	case "rm":
		return say("delete", several(ops)), rankKnown, true
	case "mv":
		return say("move", several(allButLast(ops))), rankKnown, true
	case "grep":
		return searchWords(rest), rankKnown, true
	case "submodule":
		return say("update", "the submodules"), rankKnown, true
	case "config", "rev-parse", "ls-files", "ls-tree", "cat-file", "describe", "for-each-ref", "symbolic-ref",
		"merge-base", "show-ref", "count-objects", "remote", "rev-list", "name-rev", "check-ignore", "var", "version":
		return say("check", "the repository"), rankKnown, true
	}
	return say("run", "git "+sub), rankKnown, true
}

func ghWords(ops []string) (words, int, bool) {
	sub, act := "", ""
	if len(ops) > 0 {
		sub = ops[0]
	}
	if len(ops) > 1 {
		act = ops[1]
	}
	switch sub {
	case "pr":
		switch act {
		case "create", "new":
			return say("open", "a pull request"), rankMajor, true
		case "merge":
			return say("merge", "the pull request"), rankMajor, true
		case "comment", "review":
			return say("comment on", "the pull request"), rankKnown, true
		case "checkout", "co":
			return say("check out", "a pull request"), rankKnown, true
		case "close":
			return say("close", "the pull request"), rankKnown, true
		case "edit":
			return say("edit", "the pull request"), rankKnown, true
		}
		return say("check", "the pull requests"), rankKnown, true
	case "issue":
		switch act {
		case "create", "new":
			return say("open", "an issue"), rankMajor, true
		case "comment":
			return say("comment on", "an issue"), rankKnown, true
		case "close":
			return say("close", "an issue"), rankKnown, true
		}
		return say("read", "the issues"), rankKnown, true
	case "run", "workflow":
		return say("check", "the CI runs"), rankKnown, true
	case "release":
		if act == "create" {
			return say("make", "a release"), rankMajor, true
		}
		return say("check", "the releases"), rankKnown, true
	case "repo":
		switch act {
		case "clone":
			return say("clone", or(repoName(third(ops)), "a repository")), rankMajor, true
		case "create":
			return say("make", "a repository"), rankMajor, true
		}
		return say("look at", "the repository"), rankKnown, true
	case "api":
		return say("ask", "GitHub"), rankKnown, true
	case "auth":
		return say("check", "the GitHub login"), rankKnown, true
	case "search":
		return say("search", "GitHub"), rankKnown, true
	}
	return say("use", "GitHub"), rankKnown, true
}

func goWords(ops []string) (words, int, bool) {
	sub := ""
	if len(ops) > 0 {
		sub = ops[0]
	}
	switch sub {
	case "test":
		return testWords, rankTests, true
	case "build", "install":
		return buildWords, rankMajor, true
	case "vet":
		return checkWords, rankMajor, true
	case "fmt":
		return formatWords, rankMajor, true
	case "run":
		target := second(ops)
		if target == "" || target == "." {
			return say("run", "the program"), rankKnown, true
		}
		return say("run", base(target)), rankKnown, true
	case "get":
		return say("install", "packages"), rankMajor, true
	case "mod":
		switch second(ops) {
		case "tidy":
			return say("tidy", "the modules"), rankKnown, true
		case "download":
			return say("download", "the modules"), rankKnown, true
		}
		return say("check", "the modules"), rankKnown, true
	case "generate":
		return say("generate", "code"), rankKnown, true
	case "doc":
		return say("read", "the docs"), rankKnown, true
	case "clean":
		return say("clean", "up"), rankKnown, true
	case "list":
		return say("list", "the packages"), rankKnown, true
	case "version", "env":
		return say("check", "Go"), rankKnown, true
	}
	return say("run", strings.TrimSpace("go "+sub)), rankKnown, true
}

func cargoWords(sub string) (words, int, bool) {
	switch sub {
	case "test", "t", "nextest":
		return testWords, rankTests, true
	case "build", "b":
		return buildWords, rankMajor, true
	case "check", "c", "clippy":
		return checkWords, rankMajor, true
	case "fmt":
		return formatWords, rankMajor, true
	case "run", "r":
		return say("run", "the program"), rankKnown, true
	case "add", "install":
		return say("install", "packages"), rankMajor, true
	case "doc":
		return say("build", "the docs"), rankKnown, true
	case "bench":
		return say("run", "the benchmarks"), rankKnown, true
	case "update":
		return say("update", "the packages"), rankKnown, true
	case "clean":
		return say("clean", "up"), rankKnown, true
	}
	return say("run", strings.TrimSpace("cargo "+sub)), rankKnown, true
}

func makeWords(args []string) (words, int, bool) {
	target := ""
	for _, o := range operands(args, "CfjlIoW") {
		if !strings.Contains(o, "=") {
			target = o
			break
		}
	}
	switch {
	case target == "" || target == "all" || strings.HasPrefix(target, "build"):
		return buildWords, rankMajor, true
	case target == "check" || strings.HasPrefix(target, "test"):
		return testWords, rankTests, true
	case target == "lint" || target == "vet":
		return checkWords, rankMajor, true
	case target == "fmt" || target == "format":
		return formatWords, rankMajor, true
	case target == "install":
		return say("install", "the build"), rankMajor, true
	case target == "clean":
		return say("clean", "up"), rankKnown, true
	}
	return say("run", "make "+target), rankKnown, true
}

// taskWords are the words of a build tool's task, such as gradle test.
func taskWords(prog, task string) (words, int, bool) {
	t := strings.ToLower(task)
	switch {
	case strings.Contains(t, "test") || t == "check" || t == "verify":
		return testWords, rankTests, true
	case t == "" || strings.Contains(t, "build") || strings.Contains(t, "assemble") || strings.Contains(t, "compile") || t == "package" || t == "install":
		return buildWords, rankMajor, true
	case t == "run":
		return say("run", "the program"), rankKnown, true
	case t == "clean":
		return say("clean", "up"), rankKnown, true
	case strings.Contains(t, "fmt") || strings.Contains(t, "format") || strings.Contains(t, "spotless"):
		return formatWords, rankMajor, true
	case strings.Contains(t, "lint"):
		return checkWords, rankMajor, true
	}
	return say("run", prog+" "+task), rankKnown, true
}

func npmWords(prog string, args []string) (words, int, bool) {
	ops := operands(args, "wC", "--workspace", "--filter", "--prefix", "--cwd")
	sub := ""
	if len(ops) > 0 {
		sub = ops[0]
	}
	switch sub {
	case "":
		if prog == "yarn" {
			return say("install", "packages"), rankMajor, true
		}
		return say("run", prog), rankUnknown, true
	case "test", "t", "tst":
		return testWords, rankTests, true
	case "run", "run-script":
		return scriptRunWords(prog, second(ops))
	case "install", "i", "ci", "add":
		return say("install", "packages"), rankMajor, true
	case "uninstall", "remove", "rm", "un":
		return say("remove", "packages"), rankKnown, true
	case "exec", "x", "dlx":
		return runnerWords(ops)
	case "publish":
		return say("publish", "the package"), rankMajor, true
	case "outdated", "ls", "list", "view", "info", "why", "audit":
		return say("check", "the packages"), rankKnown, true
	case "start":
		return scriptRunWords(prog, "start")
	}
	if prog == "npm" {
		return say("run", "npm "+sub), rankKnown, true
	}
	return scriptRunWords(prog, sub) // yarn build, pnpm lint
}

// scriptRunWords are the words of a package.json script.
func scriptRunWords(prog, script string) (words, int, bool) {
	switch {
	case script == "":
		return say("run", prog), rankUnknown, true
	case strings.HasPrefix(script, "test"):
		return testWords, rankTests, true
	case strings.HasPrefix(script, "build"):
		return buildWords, rankMajor, true
	case strings.HasPrefix(script, "lint") || script == "typecheck" || script == "check":
		return checkWords, rankMajor, true
	case strings.HasPrefix(script, "format") || script == "fmt":
		return formatWords, rankMajor, true
	case script == "dev" || script == "start" || script == "serve":
		return say("start", "the dev server"), rankKnown, true
	}
	return say("run", script), rankKnown, true
}

// runnerWords are the words of the program that npx, uvx and the like run.
func runnerWords(args []string) (words, int, bool) {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if (args[0] == "-p" || args[0] == "--package") && len(args) > 1 {
			args = args[1:]
		}
		args = args[1:]
	}
	if len(args) > 0 && (args[0] == "exec" || args[0] == "x" || args[0] == "dlx") {
		args = args[1:]
	}
	w, r := simpleWords(simple{args: args})
	return w, r, true
}

func packageWords(prog string, args []string) (words, int, bool) {
	ops := operands(args, "")
	sub := ""
	if len(ops) > 0 {
		sub = ops[0]
	}
	switch {
	case sub == "run" && slices.Contains([]string{"uv", "poetry", "pipenv", "pipx", "hatch", "pdm", "rye"}, prog),
		sub == "exec" && prog == "bundle":
		i := slices.Index(args, sub)
		return runnerWords(args[i+1:])
	case sub == "pip" && prog == "uv":
		i := slices.Index(args, sub)
		return packageWords("pip", args[i+1:])
	case sub == "venv" && prog == "uv":
		return say("make", "a virtual environment"), rankKnown, true
	}
	switch sub {
	case "install", "add", "i", "sync", "upgrade", "update", "reinstall", "tap", "create", "lock":
		return say("install", "packages"), rankMajor, true
	case "uninstall", "remove", "rm", "purge", "erase":
		return say("remove", "packages"), rankKnown, true
	case "":
		if prog == "bundle" {
			return say("install", "packages"), rankMajor, true
		}
	}
	return say("check", "the packages"), rankKnown, true
}

// interpreterWords are the words of a script that an interpreter runs, or
// of the command line a shell's -c runs.
func interpreterWords(prog string, args []string) (words, int, bool) {
	shell := slices.Contains([]string{"bash", "sh", "zsh", "fish", "dash"}, prog)
	lang := map[string]string{"python": "Python", "node": "JavaScript", "ruby": "Ruby", "php": "PHP",
		"ts-node": "TypeScript", "tsx": "TypeScript", "lua": "Lua", "Rscript": "R", "julia": "Julia"}[prog]
	if shell {
		lang = "shell"
	}
	for i, a := range args {
		switch {
		case a == "-c" && shell && i+1 < len(args):
			if w, r := commandWords(args[i+1]); r > rankNone {
				return w, r, true
			}
			return say("run", "a command"), rankUnknown, true
		case a == "-m" && prog == "python" && i+1 < len(args):
			switch module := args[i+1]; module {
			case "pytest", "unittest", "nose2":
				return testWords, rankTests, true
			case "pip":
				return packageWords("pip", args[i+2:])
			case "http.server":
				return say("start", "a web server"), rankKnown, true
			case "venv":
				return say("make", "a virtual environment"), rankKnown, true
			case "black", "isort":
				return formatWords, rankMajor, true
			case "mypy", "ruff", "flake8", "pylint":
				return checkWords, rankMajor, true
			default:
				return say("run", module), rankKnown, true
			}
		case a == "-c" || a == "-e" || a == "--eval":
			return say("run", "some "+lang), rankUnknown, true
		}
	}
	ops := operands(args, "")
	if len(ops) == 0 {
		return say("start", or(lang, prog)), rankUnknown, true
	}
	if prog == "python" && base(ops[0]) == "manage.py" && second(ops) == "test" {
		return testWords, rankTests, true
	}
	w, r := scriptWords(base(ops[0]))
	return w, r, true
}

// scriptWords are the words of a script run by its name: one named for
// tests, a build or a linter does that; any other runs.
func scriptWords(name string) (words, int) {
	l := strings.ToLower(name)
	switch {
	case strings.Contains(l, "test"):
		return testWords, rankTests
	case strings.Contains(l, "build"):
		return buildWords, rankMajor
	case strings.Contains(l, "lint"):
		return checkWords, rankMajor
	}
	return say("run", name), rankUnknown
}

func editorWords(prog string, args []string) (words, int, bool) {
	// BSD sed takes the suffix of -i as the next argument, often ''.
	if i := slices.Index(args, "-i"); (prog == "sed" || prog == "gsed") && i >= 0 && i+1 < len(args) &&
		(args[i+1] == "" || strings.HasPrefix(args[i+1], ".") && !strings.Contains(args[i+1], "/")) {
		args = slices.Delete(slices.Clone(args), i+1, i+2)
	}
	inPlace := false
	for i, a := range args {
		switch {
		case a == "--in-place" || strings.HasPrefix(a, "--in-place="):
			inPlace = true
		case (prog == "sed" || prog == "gsed") && strings.HasPrefix(a, "-i"):
			inPlace = true
		case prog == "perl" && len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a, 'i'):
			inPlace = true
		case (prog == "awk" || prog == "gawk") && a == "-i" && i+1 < len(args) && args[i+1] == "inplace":
			inPlace = true
		}
	}
	scripted := hasAny(args, "-e", "-E", "-f", "--expression", "--file")
	files := operands(args, "efFv", "--expression", "--file")
	if prog == "perl" && !inPlace && !scripted {
		if len(files) == 0 {
			return say("start", "Perl"), rankUnknown, true
		}
		w, r := scriptWords(base(files[0]))
		return w, r, true
	}
	if !scripted && len(files) > 0 {
		files = files[1:] // the first operand is the script
	}
	switch {
	case inPlace:
		return say("edit", or(several(files), "a file")), rankKnown, true
	case len(files) > 0:
		return say("read", several(files)), rankKnown, true
	case prog == "perl":
		return say("run", "some Perl"), rankUnknown, true
	}
	return say("read", "the output"), rankFilter, true
}

// searchWords are the words of grep, ripgrep and the like: the pattern they
// search for.
func searchWords(args []string) words {
	pattern := valueOf(args, "-e", "--regexp")
	if pattern == "" {
		if ops := operands(args, "fABCmdDgtTjM", "--glob", "--type", "--type-not", "--max-count", "--context",
			"--after-context", "--before-context", "--file", "--max-depth", "--threads"); len(ops) > 0 {
			pattern = ops[0]
		}
	}
	if pattern == "" {
		return say("search", "the files")
	}
	return say("search for", quote(short(pattern, 32)))
}

func findWords(args []string) words {
	if slices.Contains(args, "-delete") {
		return say("delete", "files")
	}
	if i := slices.Index(args, "-exec"); i >= 0 && i+1 < len(args) && path.Base(args[i+1]) == "rm" {
		return say("delete", "files")
	}
	if name := valueOf(args, "-name", "-iname"); name != "" {
		return globWords(name)
	}
	return say("look for", "files")
}

func tarWords(args []string) words {
	mode := ""
	if len(args) > 0 {
		mode = args[0]
	}
	archive := ""
	for _, a := range operands(args, "fC") {
		if strings.Contains(a, ".tar") || strings.HasSuffix(a, ".tgz") || strings.HasSuffix(a, ".zip") {
			archive = base(a)
			break
		}
	}
	if f := valueOf(args, "-f"); f != "" {
		archive = base(f)
	}
	switch {
	case strings.HasPrefix(mode, "--extract") || !strings.HasPrefix(mode, "--") && strings.ContainsRune(mode, 'x'):
		return say("unpack", or(archive, "an archive"))
	case strings.HasPrefix(mode, "--list") || !strings.HasPrefix(mode, "--") && strings.ContainsRune(mode, 't'):
		return say("look in", or(archive, "an archive"))
	}
	return say("pack", or(archive, "files"))
}

func fetchWords(prog string, args []string, out string) words {
	host := ""
	for _, a := range args {
		if strings.Contains(a, "://") {
			host = hostOf(a)
			break
		}
	}
	if host == "" {
		for _, a := range operands(args, "XHdoAeuFmrTwO", "--data", "--header", "--request", "--output", "--user-agent") {
			if h := hostOf(a); h != "" && strings.Contains(h, ".") {
				host = h
				break
			}
		}
	}
	download := prog == "wget" || out != "" || hasAny(args, "-o", "-O", "--output", "--remote-name")
	switch {
	case download:
		return say("download", or(host, base(out), "a file"))
	case host != "":
		return say("fetch", host)
	}
	return say("fetch", "a page")
}

func dockerWords(prog string, ops []string) (words, int, bool) {
	sub := ""
	if len(ops) > 0 {
		sub = ops[0]
	}
	if sub == "compose" || prog == "docker-compose" {
		rest := ops
		if prog != "docker-compose" {
			rest = rest[1:]
		}
		sub = "compose"
		if len(rest) > 0 {
			sub += " " + rest[0]
		}
	}
	switch sub {
	case "build":
		return say("build", "an image"), rankMajor, true
	case "run":
		return say("run", "a container"), rankKnown, true
	case "compose up":
		return say("start", "the containers"), rankKnown, true
	case "compose down", "stop":
		return say("stop", "the containers"), rankKnown, true
	case "logs", "compose logs":
		return say("read", "the logs"), rankKnown, true
	case "ps", "images", "compose ps":
		return say("check", "the containers"), rankKnown, true
	case "exec", "compose exec":
		return say("run", "a command in a container"), rankKnown, true
	case "pull":
		return say("download", "an image"), rankKnown, true
	case "push":
		return say("push", "an image"), rankKnown, true
	}
	return say("use", "Docker"), rankKnown, true
}

// valueOf is the value of the first of flags in args, given as the next
// argument or after an =.
func valueOf(args []string, flags ...string) string {
	for i, a := range args {
		for _, f := range flags {
			if a == f && i+1 < len(args) {
				return args[i+1]
			}
			if v, ok := strings.CutPrefix(a, f+"="); ok && strings.HasPrefix(f, "--") {
				return v
			}
		}
	}
	return ""
}

func hasAny(args []string, flags ...string) bool {
	return slices.ContainsFunc(args, func(a string) bool { return slices.Contains(flags, a) })
}

// several names one or two files by their base names, and more as the
// first and how many others.
func several(paths []string) string {
	switch len(paths) {
	case 0:
		return "files"
	case 1:
		return base(paths[0])
	case 2:
		return base(paths[0]) + " and " + base(paths[1])
	}
	return fmt.Sprintf("%s and %d more", base(paths[0]), len(paths)-1)
}

func allButLast(s []string) []string {
	if len(s) < 2 {
		return s
	}
	return s[:len(s)-1]
}

func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

func second(s []string) string {
	if len(s) < 2 {
		return ""
	}
	return s[1]
}

func third(s []string) string {
	if len(s) < 3 {
		return ""
	}
	return s[2]
}

// seconds says how long a sleep lasts.
func seconds(arg string) string {
	switch {
	case arg == "":
		return ""
	case arg == "1":
		return "1 second"
	case strings.Trim(arg, "0123456789") == "":
		return arg + " seconds"
	}
	return "a moment"
}

// base is the last element of a path: Store.swift of Sources/Irie/Store.swift.
func base(p string) string {
	p = strings.TrimRight(strings.TrimSpace(p), "/")
	if p == "" {
		return ""
	}
	return path.Base(p)
}

// repoName is the name of a repository from its URL or path.
func repoName(u string) string {
	return strings.TrimSuffix(base(u), ".git")
}

// hostOf is the host of a URL, without www.; "" when it has none.
func hostOf(u string) string {
	if u == "" || strings.ContainsAny(u, " \t") {
		return ""
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(p.Hostname(), "www.")
}

// quote puts curly quotes round a pattern or a search.
func quote(s string) string { return "“" + s + "”" }

// short puts s on one line and cuts it to n characters at most.
func short(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// firstLine is the first line of a command, with an ellipsis when more
// follows.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if line, rest, ok := strings.Cut(s, "\n"); ok && strings.TrimSpace(rest) != "" {
		return strings.TrimSpace(line) + " …"
	}
	return s
}

// lowerFirst lowers the first letter of a sentence unless its first word
// is a name or an acronym, such as I, API or README.
func lowerFirst(s string) string {
	word, _, _ := strings.Cut(s, " ")
	r, n := utf8.DecodeRuneInString(word)
	if n == len(word) || !unicode.IsUpper(r) {
		return s
	}
	if next, _ := utf8.DecodeRuneInString(word[n:]); unicode.IsUpper(next) {
		return s
	}
	return string(unicode.ToLower(r)) + s[n:]
}

// humanize turns a tool or server name such as create_issue into words.
func humanize(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(s)), " ")
}

// or is the first non-empty string.
func or(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
