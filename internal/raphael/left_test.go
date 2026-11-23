package raphael

import (
	"context"
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/proc"
)

func TestCandidatesAreThePersonsOwnThatLaunchdTook(t *testing.T) {
	ps := []Proc{
		{PID: 10, PPID: 1, UID: 501, Path: "/bin/sh"},
		{PID: 11, PPID: 1, UID: 501, Path: "/Users/alice/go/bin/devserver"},
		{PID: 12, PPID: 10, UID: 501, Path: "/usr/bin/python3"},                                           // not launchd's
		{PID: 13, PPID: 1, UID: 0, Path: "/usr/local/bin/helper"},                                         // another user's
		{PID: 14, PPID: 1, UID: 501, Path: "/usr/libexec/trustd"},                                         // macOS's own
		{PID: 15, PPID: 1, UID: 501, Path: "/Applications/Safari.app/Contents/MacOS/Safari"},              // an app
		{PID: 16, PPID: 1, UID: 501, Path: "/opt/homebrew/bin/tmux"},                                      // a tmux server: §2.9's
		{PID: 17, PPID: 1, UID: 501, Path: "/Users/alice/.local/share/irie/bin/irie"},                     // Irie's own
		{PID: 18, PPID: 1, UID: 501, Path: "/private/var/folders/xy/T/irie-bin-1/fakeagent"},              // a test's
		{PID: 19, PPID: 1, UID: 501, Path: "/System/Library/CoreServices/Finder.app/Contents/MacOS/Find"}, // both
	}
	var got []int
	for _, p := range Candidates(ps, 501, "/Users/alice/.local/share/irie/bin/irie") {
		got = append(got, p.PID)
	}
	if !slices.Equal(got, []int{10, 11, 18}) {
		t.Errorf("candidates = %v, want 10, 11 and 18", got)
	}
}

const (
	sock    = "/var/folders/xy/T/irie-501/core.sock"
	trees   = "/Users/alice/.local/share/irie/worktrees"
	convA   = "0b5e1c2a-1111-4a2b-9c3d-123456789abc" // the closed session's conversation
	convB   = "7d0f3e4b-2222-4b3c-8d4e-abcdef012345" // the open one's
	convOut = "9a8b7c6d-3333-4c4d-9e5f-0123456789ab" // a conversation outside Irie
)

func pad(conv string) string { return "/private/tmp/claude-501/-Users-alice-project/" + conv }

// sessions are an open session and a closed one, as the core lists them:
// the open first.
func sessions() []Session {
	return []Session{
		{ID: "s38open", Name: "fix-login", Conversations: []string{convB}, Worktree: trees + "/irie/fix-login", Cwd: trees + "/irie/fix-login", Open: true},
		{ID: "s38sub", Name: "docs-pass", Worktree: "", Cwd: trees + "/irie/fix-login/docs", Open: true},
		{ID: "s38gone", Name: "can-we-clean-up-the", Conversations: []string{convA}, Worktree: trees + "/irie/clean-up", Cwd: trees + "/irie/clean-up", Closed: time.Now().Add(-21 * time.Hour)},
		{ID: "s38main", Name: "in-the-checkout", Cwd: "/Users/alice/src/irie", Open: true},
	}
}

func here(gone ...string) Here {
	return Here{Socket: sock, Worktrees: trees, Gone: func(s string) bool { return slices.Contains(gone, s) }}
}

func TestWhoseTellsTheSessionByItsEnvironmentConversationOrWorktree(t *testing.T) {
	root := Seen{PID: 100, Name: "sh", Args: []string{"sh", "scripts/test-listener.sh"}, Cwd: "/"}
	for _, tc := range []struct {
		name string
		o    Orphan
		want string // the session's id, "core" for another core's, "" for none
		how  string // what How says, in part
	}{
		{"its environment, with this core's socket", Orphan{Root: with(root, func(s *Seen) { s.Session, s.Socket = "s38gone", sock })}, "s38gone", "Its environment names the session: " + brand.Env("SESSION") + "=s38gone."},
		{"the environment of a process it runs", Orphan{Root: root, Kids: []Seen{{PID: 101, Name: "node", Session: "s38open", Socket: sock}}}, "s38open", "The environment of node (pid 101), which it runs"},
		{"an id this core no longer lists", Orphan{Root: with(root, func(s *Seen) { s.Session, s.Socket = "s38old", sock })}, "s38old", brand.Env("SESSION") + "=s38old"},
		{"another core that is gone", Orphan{Root: with(root, func(s *Seen) { s.Session, s.Socket = "s9", "/tmp/irie-t-1/run/irie/core.sock" })}, "core", "which is gone"},
		{"another core that runs, whatever its folder says", Orphan{Root: with(root, func(s *Seen) {
			s.Session, s.Socket, s.Cwd = "s9", "/tmp/irie-t-2/run/irie/core.sock", trees+"/irie/fix-login"
		})}, "", ""},
		{"its folder, a conversation's scratchpad", Orphan{Root: with(root, func(s *Seen) { s.Cwd = pad(convA) + "/scratchpad" })}, "s38gone", "Its folder is in the session's Claude Code folder: " + pad(convA) + "/scratchpad."},
		{"a child's script by its full path", Orphan{Root: root, Kids: []Seen{{PID: 101, Name: "python3", Args: []string{"python3", pad(convA) + "/scratchpad/listen.py"}, Cwd: "/"}}}, "s38gone", "The argument of python3 (pid 101), which it runs, is in the session's Claude Code folder"},
		{"its output, a background task's", Orphan{Root: with(root, func(s *Seen) { s.Out = []string{pad(convB) + "/tasks/b0pv6pkg8.output"} })}, "s38open", "Its output is in the session's Claude Code folder"},
		{"an argument of the form --flag=/path", Orphan{Root: with(root, func(s *Seen) { s.Args = []string{"server", "--config=" + trees + "/irie/clean-up/c.json"} })}, "s38gone", "Its argument is in the session's worktree"},
		{"its folder in a worktree, an open session's first", Orphan{Root: with(root, func(s *Seen) { s.Cwd = trees + "/irie/fix-login/web" })}, "s38open", "Its folder is in the session's worktree: " + trees + "/irie/fix-login/web."},
		{"its folder in a session's folder in another's worktree", Orphan{Root: with(root, func(s *Seen) { s.Cwd = trees + "/irie/fix-login/docs/site" })}, "s38sub", "worktree"},
		{"its folder in a closed session's worktree", Orphan{Root: with(root, func(s *Seen) { s.Cwd = trees + "/irie/clean-up" })}, "s38gone", "worktree"},
		{"a conversation outside Irie", Orphan{Root: with(root, func(s *Seen) { s.Cwd = pad(convOut) })}, "", ""},
		{"a session's folder outside the worktrees", Orphan{Root: with(root, func(s *Seen) { s.Cwd = "/Users/alice/src/irie" })}, "", ""},
		{"nothing at all", Orphan{Root: root}, "", ""},
	} {
		c, ok := Whose(tc.o, sessions(), here("/tmp/irie-t-1/run/irie/core.sock"))
		got := ""
		switch {
		case ok && c.Session != nil:
			got = c.Session.ID
		case ok:
			got = "core"
		}
		if got != tc.want || !strings.Contains(c.How, tc.how) {
			t.Errorf("%s: whose = %q (%q), want %q (%q)", tc.name, got, c.How, tc.want, tc.how)
		}
	}
}

// with is s changed by f.
func with(s Seen, f func(*Seen)) Seen {
	f(&s)
	return s
}

func TestLeftRunningFlagsWhatAClosedOrQuietSessionLeft(t *testing.T) {
	now := time.Now()
	orphan := func(pid int, ran time.Duration, f func(*Seen)) Orphan {
		s := Seen{PID: pid, Start: int64(pid), Started: now.Add(-ran), Name: "sh"}
		f(&s)
		return Orphan{Root: s, Path: "/bin/sh"}
	}
	in := func(id string) func(*Seen) { return func(s *Seen) { s.Session, s.Socket = id, sock } }
	ss := []Session{
		{ID: "quiet", Name: "quiet", Open: true, Quiet: now.Add(-3 * time.Hour)},
		{ID: "busy", Name: "busy", Open: true, Quiet: now.Add(-3 * time.Hour), Working: true},
		{ID: "lately", Name: "lately", Open: true, Quiet: now.Add(-time.Hour)},
		{ID: "unknown", Name: "unknown", Open: true},
		{ID: "closed", Name: "closed", Closed: now.Add(-time.Hour)},
		{ID: "just", Name: "just", Closed: now.Add(-time.Minute)},
	}
	orphans := []Orphan{
		orphan(1, 5*time.Hour, in("quiet")),
		orphan(2, 5*time.Hour, in("busy")),     // its agent is in a turn
		orphan(3, 5*time.Hour, in("lately")),   // quiet an hour only
		orphan(4, 5*time.Hour, in("unknown")),  // no one knows since when it is quiet
		orphan(5, 5*time.Hour, in("closed")),   //
		orphan(6, 5*time.Hour, in("just")),     // closed a minute ago: on its way out
		orphan(7, time.Minute, in("closed")),   // started a minute ago
		orphan(8, time.Hour, func(s *Seen) {}), // nobody's
		orphan(9, time.Hour, func(s *Seen) { s.Session, s.Socket = "s1", "/tmp/irie-t-9/run/irie/core.sock" }),
	}
	got := map[int]Left{}
	for _, l := range LeftRunning(orphans, ss, here("/tmp/irie-t-9/run/irie/core.sock"), now) {
		got[l.Root.PID] = l
	}
	if len(got) != 3 || got[1].Quiet != 3*time.Hour || got[5].Quiet != 0 || got[5].For != 5*time.Hour || got[9].Core == "" {
		t.Errorf("left running = %+v; want 1 (quiet 3 hours), 5 (closed) and 9 (another core, gone)", got)
	}
}

func TestLeftFindingsNameTheSessionAndStopTheRootFirst(t *testing.T) {
	ss := sessions()
	now := time.Now()
	loop := Left{
		Orphan: Orphan{Path: "/bin/sh",
			Root: Seen{PID: 100, Start: 7, Started: now.Add(-21 * time.Hour), Name: "sh", Args: []string{"sh", "scripts/test-listener.sh"}, Cwd: pad(convA) + "/scratchpad"},
			Kids: []Seen{{PID: 101, Start: 8, Name: "python3", Args: []string{"/usr/bin/python3", "/Users/alice/Documents/listen.py"}}}},
		Claim: Claim{Session: &ss[2], How: "Its folder is in the session's Claude Code folder: " + pad(convA) + "/scratchpad."},
		For:   21 * time.Hour,
	}
	server := Left{
		Orphan: Orphan{Path: "/Users/alice/go/bin/devserver", Root: Seen{PID: 200, Start: 9, Name: "devserver", Args: []string{"/Users/alice/go/bin/devserver", "-port", "8080"}}},
		Claim:  Claim{Session: &ss[0], How: "Its folder is in the session's worktree."}, For: 5 * time.Hour, Quiet: 3 * time.Hour,
	}
	test := Left{
		Orphan: Orphan{Path: "/private/var/folders/xy/T/irie-bin-1/fakeagent", Root: Seen{PID: 300, Start: 10, Name: "fakeagent"}},
		Claim:  Claim{Core: "/tmp/" + brand.Slug + "-t-1/run/" + brand.Slug + "/core.sock", How: "gone"}, For: 15 * time.Hour,
	}
	hot := []Hot{{Proc: Proc{PID: 101, UID: 501, Path: "/usr/bin/python3"}, Start: 8, Share: 0.97, For: 3 * time.Hour}}
	fs, _ := Quick(Input{Home: "/Users/alice", UID: 501, Left: []Left{loop, server, test}, Hot: hot}, Memory{}, Disk{})
	byID := map[string]Finding{}
	for _, f := range fs {
		byID[f.ID] = f
	}
	if _, ok := byID["proc:101:8"]; ok {
		t.Error("what was left running is also listed as running hot")
	}
	l := byID["left:100:7"]
	if l.Text != "sh scripts/test-listener.sh was left running by the session “can-we-clean-up-the”, closed now (pid 100, for 21 hours, with the process it runs)." || l.Level != Warn {
		t.Errorf("the loop: %q, %s", l.Text, l.Level)
	}
	if !slices.Contains(l.Evidence, "python3 (pid 101) has used 97% of a CPU for 3 hours.") || !slices.Contains(l.Evidence, "It runs python3 ~/Documents/listen.py (pid 101).") || l.Evidence[0] != loop.How {
		t.Errorf("the loop's evidence: %q", l.Evidence)
	}
	if l.Fix == nil || l.Fix.kind != fixStopTree || l.Fix.Label != "Stop sh and what it runs" || !slices.Equal(l.Fix.Run, []string{"kill -TERM 100", "kill -TERM 101"}) || l.Fix.pid != 100 || l.Fix.start != 7 || !slices.Equal(l.Fix.tree, []procRef{{101, 8}}) {
		t.Errorf("the loop's fix: %+v", l.Fix)
	}
	if s := byID["left:200:9"]; s.Text != "devserver -port 8080 was left running by the session “fix-login”, quiet for 3 hours (pid 200, for 5 hours)." || s.Level != Note || !slices.Equal(s.Fix.Run, []string{"kill -TERM 200"}) || s.Fix.Label != "Stop devserver" {
		t.Errorf("the server: %q, %+v", s.Text, s.Fix)
	}
	if f := byID["left:300:10"]; f.Text != "fakeagent was left running by a test's "+brand.Name+", gone now (pid 300, for 15 hours)." {
		t.Errorf("the test's: %q", f.Text)
	}
	// A core after this one runs the fix as it was found.
	b, _ := json.Marshal(l.Fix)
	var back Fix
	if err := json.Unmarshal(b, &back); err != nil || back.kind != fixStopTree || !slices.Equal(back.tree, l.Fix.tree) || back.path != "sh" {
		t.Errorf("the fix after JSON: %+v, %v", back, err)
	}
}

func TestParseFiles(t *testing.T) {
	out := "p100\nfcwd\nn/private/tmp/claude-501/p/" + convA + "/scratchpad\nf1\nn/private/tmp/claude-501/p/" + convA + "/tasks/b1.output\nf2\nn/private/tmp/claude-501/p/" + convA + "/tasks/b1.output\n" +
		"p101\nfcwd\nn/\nf1\nnpipe\nf2\nn/dev/null\n"
	fs := parseFiles(out)
	if a := fs[100]; a.Cwd != "/private/tmp/claude-501/p/"+convA+"/scratchpad" || !slices.Equal(a.Out, []string{"/private/tmp/claude-501/p/" + convA + "/tasks/b1.output"}) {
		t.Errorf("100: %+v", a)
	}
	if b, ok := fs[101]; !ok || b.Cwd != "/" || len(b.Out) != 0 {
		t.Errorf("101: %+v, %v; want / and no files for a pipe or /dev/null", b, ok)
	}
}

func TestStopTreeFixLeavesAPidUsedAgainAndAStuckRootsChildrenBe(t *testing.T) {
	defer func(rt func() (proc.Table, error), s func(int, syscall.Signal) error, g time.Duration) {
		readTable, signal, stopGrace = rt, s, g
	}(readTable, signal, stopGrace)
	stopGrace = 50 * time.Millisecond
	table := []proc.Proc{{PID: 10, PPID: 1, Start: 1}, {PID: 11, PPID: 10, Start: 2}}
	readTable = func() (proc.Table, error) { return proc.NewTable(table), nil }
	var sent []int
	signal = func(pid int, s syscall.Signal) error {
		sent = append(sent, pid)
		return nil
	}
	f := Fix{kind: fixStopTree, pid: 10, start: 5, path: "sh", tree: []procRef{{11, 2}}}
	if _, err := Do(context.Background(), nil, f, DoInput{}); err == nil || len(sent) != 0 {
		t.Errorf("a pid used again: %v, sent %v", err, sent)
	}
	// One that stays after it is asked to stop may be stuck: what it runs
	// would only be started again.
	f.start = 1
	said, err := Do(context.Background(), nil, f, DoInput{})
	if err != nil || !slices.Equal(sent, []int{10}) || !strings.Contains(said, "it may be stuck, so "+brand.Raffi+" left what it runs be") {
		t.Errorf("a stuck root: %q, %v, sent %v", said, err, sent)
	}
}

// TestStopTreeFixStopsARealLoopAndWhatItRuns stops a loop that starts its
// child again as soon as it ends, as the listener's was: stopping the child
// first would only have it started again.
func TestStopTreeFixStopsARealLoopAndWhatItRuns(t *testing.T) {
	defer func(s func(int, syscall.Signal) error) { signal = s }(signal)
	var order []int
	signal = func(pid int, s syscall.Signal) error {
		if s == syscall.SIGTERM {
			order = append(order, pid)
		}
		return syscall.Kill(pid, s)
	}
	loop := exec.Command("/bin/sh", "-c", "while :; do sleep 300; done")
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() { loop.Wait(); close(reaped) }()
	var kids []proc.Proc
	defer func() {
		loop.Process.Kill()
		for _, k := range kids {
			syscall.Kill(k.PID, syscall.SIGKILL)
		}
	}()
	var root proc.Proc
	for deadline := time.Now().Add(5 * time.Second); len(kids) == 0; {
		tb, err := proc.Read()
		if err != nil {
			t.Fatal(err)
		}
		root, _ = tb.Get(loop.Process.Pid)
		kids = tb.Tree(loop.Process.Pid)
		if time.Now().After(deadline) {
			t.Fatal("the loop started no sleep")
		}
		time.Sleep(20 * time.Millisecond)
	}
	l := Left{Orphan: Orphan{Path: "/bin/sh", Root: Seen{PID: root.PID, Start: root.Start, Name: "sh", Args: []string{"/bin/sh", "-c", "while :; do sleep 300; done"}}}}
	for _, k := range kids {
		l.Kids = append(l.Kids, Seen{PID: k.PID, Start: k.Start, Name: k.Name})
	}
	said, err := Do(context.Background(), nil, *stopTreeFix(l), DoInput{})
	if err != nil || !strings.HasPrefix(said, "Stopped sh (pid ") || !strings.Contains(said, "the process it ran") {
		t.Fatalf("said %q, %v", said, err)
	}
	if len(order) < 2 || order[0] != root.PID || order[1] != kids[0].PID {
		t.Errorf("asked to stop in the order %v; want the loop %d first, then its sleep %d", order, root.PID, kids[0].PID)
	}
	tb, err := proc.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range append([]proc.Proc{root}, kids...) {
		if tb.Alive(p) {
			t.Errorf("%s (pid %d) still runs", p.Name, p.PID)
		}
	}
	if left := tb.Tree(root.PID); len(left) > 0 {
		t.Errorf("the loop started %d again", len(left))
	}
	select {
	case <-reaped:
	case <-time.After(5 * time.Second):
		t.Error("the loop never ended")
	}
}
