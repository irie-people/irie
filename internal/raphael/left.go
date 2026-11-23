package raphael

// Left running (DESIGN §4.11). An agent starts servers, watchers and loops
// in the background and moves on, and some outlive the work that needed
// them: the shell that started one ends, and launchd takes it as its own.
// Raphael flags each such process whose session is closed, or whose open
// session has been quiet for leftQuiet, and offers to stop it and what it
// runs, on the person's OK, never by himself (after Blitz's spec 052,
// PR-20).
//
// Whose a process is, its environment says best: IRIE_SESSION, with
// IRIE_SOCKET naming this core. macOS hides the environment of its own
// programs, such as /bin/sh and /bin/zsh, but shows other programs' to
// their user, Homebrew's and Go's among them (measured on macOS 14). So its
// files tell too: a Claude Code conversation's scratchpad, or the output
// file of one of its background tasks, names the conversation, and an Irie
// worktree its session. What one process of a tree tells counts for the
// tree's root, the process launchd took: a loop's child often names its
// script by its full path where the loop names it by a relative one.

import (
	"bufio"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
)

const (
	// leftQuiet is how long an open session must be quiet before what it
	// left running is flagged: a server it started for the person may be
	// wanted while they work with it (Blitz's left_after).
	leftQuiet = 2 * time.Hour
	// leftGrace is how long a process must have run, and a session have
	// been closed, before it is flagged: one younger may be on its way out
	// with its session.
	leftGrace = 10 * time.Minute
	// leftShown is how many of what a root runs its finding names.
	leftShown = 4
)

// Seen is a process of a candidate's tree, with what may say whose it is.
type Seen struct {
	PID     int
	Start   int64     // when it started, as the process table says, to tell a pid used again
	Started time.Time // the same by the clock; zero when the system cannot say
	Name    string    // its command's name, cut short by the system
	Args    []string  // its arguments; nil when unread
	Cwd     string    // its working folder; "" when unread
	Out     []string  // the files its output and errors go to
	// Session and Socket are IRIE_SESSION and IRIE_SOCKET from the
	// environment it started with: "" when it had none, or the system
	// hides it.
	Session, Socket string
}

// Orphan is a candidate (Candidates): a process launchd took as its own,
// with what it runs.
type Orphan struct {
	Root Seen
	Path string // the root's program, as ps names it
	Kids []Seen // what it runs, each after its parent
}

// Session is one of Irie's sessions, open or closed, as far as Raphael
// needs it to tell whose a process is.
type Session struct {
	ID, Name string
	// Conversations are its agent's Claude Code conversations, the
	// current one first: each names a scratchpad,
	// /tmp/claude-<uid>/<project>/<conversation>.
	Conversations []string
	Worktree      string // the worktree Irie made for it; "" when none
	Cwd           string // the folder it runs in
	Open          bool
	Closed        time.Time // when it closed; zero when open, or when no one knows
	// Quiet is since when its agent and the person have been quiet: its
	// agent's last report, the person's last word to it, its start. Zero
	// when no one knows, and then it is never taken for quiet.
	Quiet   time.Time
	Working bool // its agent is in a turn
}

// Here is what tells this core's sessions from another's.
type Here struct {
	Socket    string // this core's socket, which its sessions' IRIE_SOCKET names
	Worktrees string // the folder Irie makes its worktrees in
	// Gone reports whether another core is gone, by its socket: its
	// sessions are then over. Nil when nothing counts as gone.
	Gone func(socket string) bool
}

// Claim is whose a process is, and how Raphael knows.
type Claim struct {
	Session *Session // the session that left it; nil for another core's
	Core    string   // the socket of the core that is gone, when Session is nil
	By      Seen     // the process of the tree that told
	How     string   // what told, in a line
}

// Left is a process an agent left running.
type Left struct {
	Orphan
	Claim
	For   time.Duration // how long it has run
	Quiet time.Duration // how long its open session has been quiet; 0 when it is closed
}

// Candidates are the processes an agent may have left running: the
// person's own whose parent is launchd, outside an app and the system's
// folders, not a tmux server (§2.9's leftovers are those) and not Irie's
// own program, irie.
func Candidates(ps []Proc, uid int, irie string) []Proc {
	var out []Proc
	for _, p := range ps {
		if p.PPID != 1 || systemProc(p, uid) || appOf(p.Path) != "" || filepath.Base(p.Path) == "tmux" || (irie != "" && p.Path == irie) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// scratchpad finds a Claude Code conversation in a path: its scratchpad
// and its tasks' output are in /tmp/claude-<uid>/<project>/<conversation>.
var scratchpad = regexp.MustCompile(`/claude-[0-9]+/[^/]+/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:/|$)`)

// pathsOf are the absolute paths of what s shows: its folder, its output's
// files and its arguments, such as /x/listen.py and --config=/x/c.json.
func pathsOf(s Seen) []string {
	out := append([]string{}, s.Cwd)
	out = append(out, s.Out...)
	for _, a := range s.Args {
		switch i := strings.Index(a, "=/"); {
		case strings.HasPrefix(a, "/"):
			out = append(out, a)
		case i > 0:
			out = append(out, a[i+1:])
		}
	}
	return slices.DeleteFunc(out, func(p string) bool { return p == "" })
}

// within reports whether path is dir or in it.
func within(path, dir string) bool {
	dir = strings.TrimSuffix(dir, "/")
	return dir != "" && (path == dir || strings.HasPrefix(path, dir+"/"))
}

// Whose says whose o is, by what its root and then what it runs show: each
// process's environment first, then its files. An environment that names
// a core ends the search, as it is sure: a process of another core's
// session that still runs is that core's to judge. ss lists the open
// sessions first, then the closed, the last closed first.
func Whose(o Orphan, ss []Session, h Here) (Claim, bool) {
	tree := append([]Seen{o.Root}, o.Kids...)
	for _, s := range tree {
		if s.Session == "" || s.Socket == "" {
			continue
		}
		how := fmt.Sprintf("Its environment names the session: %s=%s.", brand.Env("SESSION"), s.Session)
		if s.PID != o.Root.PID {
			how = fmt.Sprintf("The environment of %s (pid %d), which it runs, names the session: %s=%s.", s.Name, s.PID, brand.Env("SESSION"), s.Session)
		}
		if s.Socket != h.Socket {
			if h.Gone == nil || !h.Gone(s.Socket) {
				return Claim{}, false
			}
			how = fmt.Sprintf("Its environment names another %s, by its socket %s, which is gone.", brand.Name, s.Socket)
			if s.PID != o.Root.PID {
				how = fmt.Sprintf("The environment of %s (pid %d), which it runs, names another %s, by its socket %s, which is gone.", s.Name, s.PID, brand.Name, s.Socket)
			}
			return Claim{Core: s.Socket, By: s, How: how}, true
		}
		for i := range ss {
			if ss[i].ID == s.Session {
				return Claim{Session: &ss[i], By: s, How: how}, true
			}
		}
		// One this core no longer lists, closed long ago.
		return Claim{Session: &Session{ID: s.Session}, By: s, How: how}, true
	}
	for _, s := range tree {
		for _, p := range pathsOf(s) {
			if c, ok := claimPath(p, s, o.Root.PID, ss, h); ok {
				return c, true
			}
		}
	}
	return Claim{}, false
}

// claimPath is whose a path of s says it is: a session's conversation, by
// its scratchpad or a task's output, or its worktree.
func claimPath(p string, s Seen, root int, ss []Session, h Here) (Claim, bool) {
	said := func(where string) string {
		kind := "file"
		switch {
		case p == s.Cwd:
			kind = "folder"
		case slices.Contains(s.Out, p):
			kind = "output"
		case slices.ContainsFunc(s.Args, func(a string) bool { return a == p || strings.HasSuffix(a, "="+p) }):
			kind = "argument"
		}
		if s.PID == root {
			return fmt.Sprintf("Its %s is in the session's %s: %s.", kind, where, p)
		}
		return fmt.Sprintf("The %s of %s (pid %d), which it runs, is in the session's %s: %s.", kind, s.Name, s.PID, where, p)
	}
	if m := scratchpad.FindStringSubmatch(p); m != nil {
		for i := range ss {
			if slices.Contains(ss[i].Conversations, m[1]) {
				return Claim{Session: &ss[i], By: s, How: said("Claude Code folder")}, true
			}
		}
	}
	if h.Worktrees == "" || !within(p, h.Worktrees) {
		return Claim{}, false
	}
	// The session whose folder holds it closest: one that runs in a
	// folder of another's worktree is the one; the open before the closed.
	best, deep := -1, 0
	for i := range ss {
		for _, dir := range []string{ss[i].Worktree, ss[i].Cwd} {
			if within(dir, h.Worktrees) && within(p, dir) && len(dir) > deep {
				best, deep = i, len(dir)
			}
		}
	}
	if best < 0 {
		return Claim{}, false
	}
	return Claim{Session: &ss[best], By: s, How: said("worktree")}, true
}

// LeftRunning are the orphans an agent left running: one whose session is
// closed, and one whose open session has been quiet for leftQuiet, as
// Blitz's spec 052 has it (PR-20); one of another core that is gone, such
// as a test's, too. A process younger than leftGrace, or one whose session
// closed less than leftGrace ago, may still be on its way out, and waits.
func LeftRunning(orphans []Orphan, ss []Session, h Here, now time.Time) []Left {
	var out []Left
	for _, o := range orphans {
		if o.Root.Started.IsZero() || now.Sub(o.Root.Started) < leftGrace {
			continue
		}
		c, ok := Whose(o, ss, h)
		if !ok {
			continue
		}
		l := Left{Orphan: o, Claim: c, For: now.Sub(o.Root.Started)}
		switch s := c.Session; {
		case s == nil:
		case !s.Open:
			if !s.Closed.IsZero() && now.Sub(s.Closed) < leftGrace {
				continue
			}
		case s.Working || s.Quiet.IsZero() || now.Sub(s.Quiet) < leftQuiet:
			continue
		default:
			l.Quiet = now.Sub(s.Quiet)
		}
		out = append(out, l)
	}
	return out
}

// leftPIDs are the processes of what was left running, roots and what
// they run, which the hot findings leave to theirs.
func leftPIDs(ls []Left) map[int]bool {
	out := map[int]bool{}
	for _, l := range ls {
		out[l.Root.PID] = true
		for _, k := range l.Kids {
			out[k.PID] = true
		}
	}
	return out
}

// commandLine is what a process runs in words: its arguments, with the
// home folder written ~, cut short; its program's name when they are
// unread.
func commandLine(s Seen, path, home string) string {
	if len(s.Args) == 0 {
		if path != "" {
			return progName(path)
		}
		return s.Name
	}
	args := slices.Clone(s.Args)
	args[0] = progName(args[0])
	for i := range args {
		args[i] = tildePath(args[i], home)
	}
	return cut(strings.Join(args, " "), 60)
}

// sessionName is a session as a finding names it.
func sessionName(s *Session) string {
	if s.Name != "" {
		return "“" + s.Name + "”"
	}
	return s.ID
}

// leftFindings says what agents left running, a finding each, with the
// stop of it and what it runs, on the person's OK. One that runs hot needs
// care; the hot findings leave it out.
func leftFindings(ls []Left, hot []Hot, home string) []Finding {
	hotBy := map[int]Hot{} // what runs hot, by pid
	for _, h := range hot {
		hotBy[h.PID] = h
	}
	var fs []Finding
	for _, l := range ls {
		name := commandLine(l.Root, l.Path, home)
		runs := ""
		switch n := len(l.Kids); n {
		case 0:
		case 1:
			runs = ", with the process it runs"
		default:
			runs = fmt.Sprintf(", with the %d processes it runs", n)
		}
		f := Finding{ID: fmt.Sprintf("left:%d:%d", l.Root.PID, l.Root.Start), Area: AreaProcesses, Level: Note}
		switch s := l.Session; {
		case s == nil:
			whose := "another " + brand.Name
			if strings.Contains(l.Core, "/"+brand.Slug+"-t-") {
				whose = "a test's " + brand.Name
			}
			f.Text = fmt.Sprintf("%s was left running by %s, gone now (pid %d, for %s%s).", name, whose, l.Root.PID, For(l.For), runs)
			f.Advice = "The " + brand.Name + " that started it no longer runs, so nothing needs it. Raffi can stop it and what it runs."
		case !s.Open:
			f.Text = fmt.Sprintf("%s was left running by the session %s, closed now (pid %d, for %s%s).", name, sessionName(s), l.Root.PID, For(l.For), runs)
			f.Advice = "Its session is closed, so its agent no longer needs it. If you don't need it either, Raffi can stop it and what it runs."
		default:
			f.Text = fmt.Sprintf("%s was left running by the session %s, quiet for %s (pid %d, for %s%s).", name, sessionName(s), For(l.Quiet), l.Root.PID, For(l.For), runs)
			f.Advice = "Its agent started it and has been quiet since: it may be a server you still use. If you don't need it, Raffi can stop it and what it runs."
		}
		f.Evidence = append(f.Evidence, l.How)
		tree := append([]Seen{l.Root}, l.Kids...)
		for _, s := range tree {
			if h, ok := hotBy[s.PID]; ok && h.Start == s.Start {
				f.Level = Warn
				f.Evidence = append(f.Evidence, fmt.Sprintf("%s (pid %d) has used %.0f%% of a CPU for %s.", s.Name, s.PID, h.Share*100, For(h.For)))
			}
		}
		for i, k := range l.Kids {
			if i == leftShown {
				f.Evidence = append(f.Evidence, fmt.Sprintf("It runs %d more.", len(l.Kids)-leftShown))
				break
			}
			f.Evidence = append(f.Evidence, fmt.Sprintf("It runs %s (pid %d).", commandLine(k, "", home), k.PID))
		}
		f.Fix = stopTreeFix(l)
		fs = append(fs, f)
	}
	return fs
}

// stopTreeFix stops what was left running: its root first, so that a loop
// can't start what it runs again, then what it runs.
func stopTreeFix(l Left) *Fix {
	prog := progName(l.Path)
	if len(l.Root.Args) > 0 {
		prog = progName(l.Root.Args[0])
	}
	fix := &Fix{
		Label: "Stop " + prog,
		Run:   []string{command("kill", "-TERM", strconv.Itoa(l.Root.PID))},
		About: fmt.Sprintf("Asks %s (pid %d) to stop, as quitting it would: it may not save what it was doing.", prog, l.Root.PID),
		kind:  fixStopTree, pid: l.Root.PID, start: l.Root.Start, path: prog,
	}
	if len(l.Kids) == 0 {
		return fix
	}
	fix.Label += " and what it runs"
	line := []string{"kill", "-TERM"}
	for _, k := range l.Kids {
		fix.tree = append(fix.tree, procRef{PID: k.PID, Start: k.Start})
		line = append(line, strconv.Itoa(k.PID))
	}
	fix.Run = append(fix.Run, command(line...))
	fix.About = fmt.Sprintf("Asks %s (pid %d) to stop first, so that it can't start them again, then what it runs (%s now), as quitting them would: they may not save what they were doing.",
		prog, l.Root.PID, plural(len(l.Kids), "1 process", "%d processes"))
	return fix
}

// Files is what lsof shows of a process: its working folder, and the
// files its output and its errors go to.
type Files struct {
	Cwd string
	Out []string
}

// ReadFiles reads the working folder and the output's files of each of
// pids with one lsof (-a ANDs -p with -d: only those descriptors of only
// those processes), which answers at once for a few, where a listing of
// every process's files is slow. A pid gone meanwhile is left out.
func ReadFiles(ctx context.Context, r Runner, pids []int) (map[int]Files, error) {
	if len(pids) == 0 {
		return map[int]Files{}, nil
	}
	list := make([]string, len(pids))
	for i, p := range pids {
		list[i] = strconv.Itoa(p)
	}
	out, errb, err := r.Run(ctx, lsofPath(), "-n", "-P", "-w", "-a", "-p", strings.Join(list, ","), "-d", "cwd,1,2", "-Fpfn")
	// lsof fails when one of the pids is gone, and still lists the rest.
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("lsof: %v: %s", err, cut(strings.TrimSpace(string(errb)), 200))
	}
	return parseFiles(string(out)), nil
}

// lsofPath is where macOS keeps lsof, and where Linux does.
func lsofPath() string {
	if exists("/usr/sbin/lsof") {
		return "/usr/sbin/lsof"
	}
	return "/usr/bin/lsof"
}

// parseFiles reads lsof's -F pfn output: p the pid, f the descriptor (cwd,
// or 1 and 2), n the name. Only paths count: a pipe or a terminal does not.
func parseFiles(out string) map[int]Files {
	fs := map[int]Files{}
	pid, fd := 0, ""
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		v := line[1:]
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(v)
			fd = ""
			if pid > 0 {
				fs[pid] = fs[pid]
			}
		case 'f':
			fd = v
		case 'n':
			if pid <= 0 || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "/dev/") {
				continue
			}
			f := fs[pid]
			switch fd {
			case "cwd":
				f.Cwd = v
			case "1", "2":
				if !slices.Contains(f.Out, v) {
					f.Out = append(f.Out, v)
				}
			}
			fs[pid] = f
		}
	}
	return fs
}
