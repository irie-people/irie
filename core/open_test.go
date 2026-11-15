package core

import (
	"reflect"
	"strings"
	"testing"
)

// TestSplitFilePos: a trailing :LINE or :LINE:COL comes off a file name
// unless the name as given exists.
func TestSplitFilePos(t *testing.T) {
	exists := func(p string) bool { return p == "/home/alice/a:7.txt" || p == "/home/alice/b:2:3" }
	for _, c := range []struct {
		in        string
		file      string
		line, col int
	}{
		{"/home/alice/main.go", "/home/alice/main.go", 0, 0},
		{"/home/alice/main.go:12", "/home/alice/main.go", 12, 0},
		{"/home/alice/main.go:12:5", "/home/alice/main.go", 12, 5},
		{"/home/alice/a:7.txt", "/home/alice/a:7.txt", 0, 0},
		{"/home/alice/b:2:3", "/home/alice/b:2:3", 0, 0},
		{"/home/alice/b:2:3:9", "/home/alice/b:2", 3, 9},
		{"/home/alice/a:7.txt:4", "/home/alice/a:7.txt", 4, 0},
		{"/home/alice/main.go:x", "/home/alice/main.go:x", 0, 0},
		{"/home/alice/main.go:12:x", "/home/alice/main.go:12:x", 0, 0},
		{"/home/alice/main.go:0", "/home/alice/main.go:0", 0, 0},
		{"/home/alice/main.go:-3", "/home/alice/main.go:-3", 0, 0},
		{"/home/alice/main.go:", "/home/alice/main.go:", 0, 0},
	} {
		file, line, col := SplitFilePos(c.in, exists)
		if file != c.file || line != c.line || col != c.col {
			t.Errorf("%q = %q, %d, %d; want %q, %d, %d", c.in, file, line, col, c.file, c.line, c.col)
		}
	}
}

// :open FILE[:LINE[:COL]] opens a file on the selected session's code
// card: a relative name is the session's folder's, ~/ the home folder's,
// and a trailing line splits off unless the name as given is a file.
func TestOpenShowsAFileBesideTheSelectedSession(t *testing.T) {
	v := view()
	v.Sessions[0].Worktree = "/home/alice/code/web-login"
	v.Sessions[1].Cwd = "/home/alice/code/parser"
	v.Sessions = append(v.Sessions, Session{ID: "s5", Name: "far", State: StateWorking, Host: "asahi", Cwd: "/home/alice/far"})
	v.Exists = func(p string) bool { return p == "/home/alice/code/web-login/odd:3" }
	for _, c := range []struct {
		text, session string
		want          CardArgs
	}{
		{":open main.go", "s1", CardArgs{Session: "s1", Kind: "code", Path: "/home/alice/code/web-login/main.go"}},
		{":open internal/core/pane.go:12", "s1", CardArgs{Session: "s1", Kind: "code", Path: "/home/alice/code/web-login/internal/core/pane.go", Line: 12}},
		{":open ./a/../b.go:3:9", "s1", CardArgs{Session: "s1", Kind: "code", Path: "/home/alice/code/web-login/b.go", Line: 3, Col: 9}},
		{":open odd:3", "s1", CardArgs{Session: "s1", Kind: "code", Path: "/home/alice/code/web-login/odd:3"}},
		{":open ~/notes.md", "s1", CardArgs{Session: "s1", Kind: "code", Path: "/home/alice/notes.md"}},
		{":open /etc/hosts:2", "s2", CardArgs{Session: "s2", Kind: "code", Path: "/etc/hosts", Line: 2}},
		// A session without a folder reads from home.
		{":open todo.txt", "s3", CardArgs{Session: "s3", Kind: "code", Path: "/home/alice/todo.txt"}},
	} {
		st, err := Read(InputArgs{Text: c.text, Session: c.session}, v)
		if err != nil {
			t.Errorf("%q: %v", c.text, err)
			continue
		}
		if st.Op != OpCard || !reflect.DeepEqual(st.Args, c.want) {
			t.Errorf("%q: %s %+v, want card %+v", c.text, st.Op, st.Args, c.want)
		}
	}
	for _, c := range []struct{ text, session, says string }{
		{":open main.go", "", ":open acts on the selected session"},
		{":open main.go", "s5", "runs on asahi"},
	} {
		_, err := Read(InputArgs{Text: c.text, Session: c.session}, v)
		if code(err) != CodeBadArgs || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%q: %v, want bad args saying %q", c.text, err, c.says)
		}
	}
	// Bare :open isn't a command of its own.
	if _, err := Read(InputArgs{Text: ":open", Session: "s1"}, v); code(err) != CodeBadArgs {
		t.Errorf(":open alone: %v, want bad args", err)
	}
	r, err := Said(Step{Op: OpCard, Args: CardArgs{Path: "/home/alice/a/pane.go", Line: 12}, Session: &v.Sessions[0]}, nil, nil)
	if err != nil || r.Text != "Opened pane.go at line 12 beside fix login." {
		t.Errorf("said %q, %v", r.Text, err)
	}
}
