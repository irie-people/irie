package ctxgit

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A synthetic transcript in Claude Code's shape (DESIGN §4.7, PROVENANCE):
// never a recorded one.
type tx struct {
	t     *testing.T
	path  string
	clock time.Time
	last  string
	n     int
}

func newTx(t *testing.T, dir, session string) *tx {
	t.Helper()
	return &tx{t: t, path: filepath.Join(dir, session+".jsonl"), clock: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
}

func (x *tx) add(v map[string]any) string {
	x.t.Helper()
	x.n++
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", x.n)
	x.clock = x.clock.Add(time.Second)
	v["uuid"], v["sessionId"], v["timestamp"], v["cwd"] = id, "sess-a", x.clock.Format(time.RFC3339Nano), "/home/alice/app"
	if _, ok := v["parentUuid"]; !ok {
		if x.last == "" {
			v["parentUuid"] = nil
		} else {
			v["parentUuid"] = x.last
		}
	}
	b, _ := json.Marshal(v)
	f, err := os.OpenFile(x.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		x.t.Fatal(err)
	}
	f.Write(append(b, '\n'))
	f.Close()
	x.last = id
	return id
}

func (x *tx) prompt(text string) string {
	return x.add(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
}

func (x *tx) reply(text string) string {
	return x.add(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}})
}

func (x *tx) tool(name, command string) string {
	use := x.add(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "tool_use", "id": "toolu_" + fmt.Sprint(x.n), "name": name, "input": map[string]any{"command": command}}}}})
	x.add(map[string]any{"type": "user", "toolUseResult": map[string]any{"stdout": "ok"}, "message": map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": "toolu_" + fmt.Sprint(x.n-1), "content": "ok"}}}})
	return use
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir=" + dir}, args...)...)
	cmd.Env = Env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func open(t *testing.T) *Store {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	st, err := Open(filepath.Join(t.TempDir(), "context.git"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestObjectsAreGits(t *testing.T) {
	st := open(t)
	id, err := st.write("blob", []byte("hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "ce013625030ba8dba906f756967f9e9ca394464a"; id != want {
		t.Fatalf("blob id %s, want git's %s", id, want)
	}
	if got := gitIn(t, st.Dir(), "cat-file", "-p", id); got != "hello" {
		t.Fatalf("git reads %q", got)
	}
	tree, err := st.writeTree([]treeEntry{{name: "b.md", id: id}, {name: "a", id: id}, {name: "a-dir", id: mustTree(t, st, id), dir: true}})
	if err != nil {
		t.Fatal(err)
	}
	ls := gitIn(t, st.Dir(), "ls-tree", tree)
	if !strings.Contains(ls, "a-dir") || !strings.Contains(ls, "b.md") {
		t.Fatalf("ls-tree: %s", ls)
	}
	c, err := st.writeCommit(tree, nil, time.Unix(1700000000, 0), "subject\n\nbody")
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, st.Dir(), "fsck", "--strict", c)
}

func mustTree(t *testing.T, st *Store, blob string) string {
	t.Helper()
	id, err := st.writeTree([]treeEntry{{name: "x", id: blob}})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func session(x *tx) Session {
	return Session{ID: "s0test", Name: "app", Conversation: "sess-a", Model: "claude-opus-5-5", Transcripts: []string{x.path}}
}

func TestSyncStoresATurnACommit(t *testing.T) {
	st := open(t)
	x := newTx(t, t.TempDir(), "sess-a")
	x.prompt("add a readme")
	x.tool("Bash", "ls /home/alice/app")
	x.reply("Added README.md.")
	x.prompt("now a licence")
	x.reply("Added LICENSE.")
	head, err := st.Sync(session(x))
	if err != nil {
		t.Fatal(err)
	}
	log, err := st.Log("s0test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].Prompt != "now a licence" || log[1].Prompt != "add a readme" || log[0].Commit != head {
		t.Fatalf("log: %+v", log)
	}
	if log[0].Turn != 2 || log[0].Said != "Added LICENSE." || log[0].Session != "s0test" || log[0].Model != "claude-opus-5-5" {
		t.Fatalf("turn: %+v", log[0])
	}
	if got := gitIn(t, st.Dir(), "ls-tree", "--name-only", "-r", head); got != "turns/0001.jsonl\nturns/0001.md\nturns/0002.jsonl\nturns/0002.md" {
		t.Fatalf("tree:\n%s", got)
	}
	md := gitIn(t, st.Dir(), "show", head+":turns/0001.md")
	if !strings.Contains(md, "## You\n\nadd a readme") || !strings.Contains(md, "- **Bash** `ls /home/alice/app`") || !strings.Contains(md, "Added README.md.") {
		t.Fatalf("md:\n%s", md)
	}
	// git reads the trailers too.
	if got := gitIn(t, st.Dir(), "log", "-1", "--format=%(trailers:key=Irie-Turn,valueonly)", head); got != "2" {
		t.Fatalf("trailer: %q", got)
	}
	gitIn(t, st.Dir(), "fsck", "--strict")

	// Nothing new: no commit.
	again, err := st.Sync(session(x))
	if err != nil || again != head {
		t.Fatalf("sync again: %s %v", again, err)
	}
	// One more turn: one more commit, on the last.
	x.prompt("and a changelog")
	x.reply("Added CHANGELOG.md.")
	next, err := st.Sync(session(x))
	if err != nil {
		t.Fatal(err)
	}
	if parent := gitIn(t, st.Dir(), "rev-parse", next+"^"); parent != head {
		t.Fatalf("parent %s, want %s", parent, head)
	}
	// A new store, as after a restart, reads the branch back.
	st2, _ := Open(st.Dir())
	if same, err := st2.Sync(session(x)); err != nil || same != next {
		t.Fatalf("restart sync: %s %v", same, err)
	}
}

func TestATurnInProgressGrows(t *testing.T) {
	st := open(t)
	x := newTx(t, t.TempDir(), "sess-a")
	x.prompt("fix the tests")
	x.tool("Bash", "go test ./...")
	s := session(x)
	s.Working = true
	partial, err := st.Sync(s)
	if err != nil {
		t.Fatal(err)
	}
	x.reply("Fixed.")
	s.Working = false
	done, err := st.Sync(s)
	if err != nil {
		t.Fatal(err)
	}
	log, _ := st.Log("s0test", 0)
	if len(log) != 2 || !log[1].Partial || log[0].Partial || log[0].Turn != 1 || log[1].Turn != 1 || log[0].Commit != done || log[1].Commit != partial {
		t.Fatalf("log: %+v", log)
	}
	c, cut, err := st.Resolve("s0test", "1")
	if err != nil || c != done || cut != "" {
		t.Fatalf("resolve 1: %s %s %v", c, cut, err)
	}
}

func TestARewindGoesOnFromTheOlderTurn(t *testing.T) {
	st := open(t)
	x := newTx(t, t.TempDir(), "sess-a")
	x.prompt("one")
	endOne := x.reply("did one")
	x.prompt("two")
	x.reply("did two")
	if _, err := st.Sync(session(x)); err != nil {
		t.Fatal(err)
	}
	log, _ := st.Log("s0test", 0)
	turnOne, turnTwo := log[1].Commit, log[0].Commit
	// /rewind to before "two": the next prompt follows "did one".
	x.add(map[string]any{"type": "user", "parentUuid": endOne, "message": map[string]any{"role": "user", "content": "two, but better"}})
	x.reply("did two better")
	head, err := st.Sync(session(x))
	if err != nil {
		t.Fatal(err)
	}
	if parent := gitIn(t, st.Dir(), "rev-parse", head+"^"); parent != turnOne {
		t.Fatalf("the rewound turn's parent is %s, want turn one %s", parent, turnOne)
	}
	if kept := gitIn(t, st.Dir(), "for-each-ref", "--format=%(objectname)", "refs/irie/rewound/"); kept != turnTwo {
		t.Fatalf("the old head was not kept: %q", kept)
	}
	lines, err := st.Transcript(head, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(lines), `"two"`) || !strings.Contains(string(lines), "two, but better") {
		t.Fatalf("transcript holds the wrong line:\n%s", lines)
	}
}

func TestANewConversationStartsItsOwnTurns(t *testing.T) {
	st := open(t)
	dir := t.TempDir()
	a := newTx(t, dir, "sess-a")
	a.prompt("first")
	a.reply("ok")
	s := session(a)
	tip, err := st.Sync(s)
	if err != nil {
		t.Fatal(err)
	}
	b := newTx(t, dir, "sess-b")
	b.n = 100 // its own uuids
	b.prompt("after /clear")
	b.reply("fresh")
	s.Transcripts = append(s.Transcripts, b.path)
	head, err := st.Sync(s)
	if err != nil {
		t.Fatal(err)
	}
	if parent := gitIn(t, st.Dir(), "rev-parse", head+"^"); parent != tip {
		t.Fatalf("parent %s, want %s", parent, tip)
	}
	if got := gitIn(t, st.Dir(), "ls-tree", "--name-only", "-r", head); got != "turns/0001.jsonl\nturns/0001.md" {
		t.Fatalf("a new conversation's tree holds only its own turns:\n%s", got)
	}
}

func TestResolveAndCutAndFork(t *testing.T) {
	st := open(t)
	x := newTx(t, t.TempDir(), "sess-a")
	x.prompt("one")
	x.reply("did one")
	x.prompt("two")
	mid := x.tool("Bash", "make")
	x.reply("did two")
	x.prompt("three")
	x.reply("did three")
	head, err := st.Sync(session(x))
	if err != nil {
		t.Fatal(err)
	}
	log, _ := st.Log("s0test", 0)
	for _, c := range []struct{ at, want string }{{"", head}, {"3", head}, {"2", log[1].Commit}, {"-2", log[2].Commit}, {log[1].Leaf, log[1].Commit}} {
		got, cut, err := st.Resolve("s0test", c.at)
		if err != nil || got != c.want || cut != "" {
			t.Fatalf("resolve %q: %s %q %v, want %s", c.at, got, cut, err, c.want)
		}
	}
	if err := st.Tag("good-plan", log[1].Commit); err != nil {
		t.Fatal(err)
	}
	if got, _, err := st.Resolve("s0test", "good-plan"); err != nil || got != log[1].Commit {
		t.Fatalf("tag: %s %v", got, err)
	}
	if _, _, err := st.Resolve("s0test", "7"); err == nil {
		t.Fatal("turn 7 resolved")
	}
	// An entry in the middle of turn two: its commit, and a cut.
	got, cut, err := st.Resolve("s0test", mid)
	if err != nil || got != log[1].Commit || cut != mid {
		t.Fatalf("resolve the middle: %s %q %v", got, cut, err)
	}
	cutCommit, err := st.Cut(got, cut)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := st.Transcript(cutCommit, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(lines), "did two") || !strings.Contains(string(lines), `"make"`) {
		t.Fatalf("the cut holds:\n%s", lines)
	}
	if strings.Count(string(lines), "\n") != 4 {
		t.Fatalf("the cut holds %d lines, want 4:\n%s", strings.Count(string(lines), "\n"), lines)
	}

	// A fork's transcript: its own id on every line, the folder moved.
	fork := ForkLines(lines, "fork-id", "/home/alice/app", "/home/alice/forks/app-2")
	if strings.Contains(string(fork), `"sess-a"`) || strings.Count(string(fork), `"sessionId":"fork-id"`) != 4 {
		t.Fatalf("session ids:\n%s", fork)
	}
	if strings.Contains(string(fork), `"/home/alice/app"`) || !strings.Contains(string(fork), `"cwd":"/home/alice/forks/app-2"`) {
		t.Fatalf("paths:\n%s", fork)
	}

	// The fork's branch starts at the cut and goes on from it.
	if err := st.StartBranch("s0fork", cutCommit); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fork-id.jsonl")
	os.WriteFile(path, fork, 0o600)
	f := &tx{t: t, path: path, clock: x.clock, last: mid, n: 500}
	f.prompt("try it another way")
	f.reply("done another way")
	forkHead, err := st.Sync(Session{ID: "s0fork", Conversation: "fork-id", Transcripts: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	if base := gitIn(t, st.Dir(), "merge-base", forkHead, head); base != log[1].Commit {
		t.Fatalf("the fork and its source part at %s, want turn two %s", base, log[1].Commit)
	}
	forkLog, _ := st.Log("s0fork", 0)
	if len(forkLog) != 4 || forkLog[0].Prompt != "try it another way" || forkLog[0].Turn != 3 || forkLog[1].Cut != mid {
		t.Fatalf("fork log: %+v", forkLog)
	}
}

func TestReplacePathStopsAtLongerNames(t *testing.T) {
	in := `{"a":"/x/app","b":"/x/app/main.go","c":"/x/app-2/main.go","d":"cd /x/app && ls","e":"/x/apps"}`
	got := string(replacePath([]byte(in), []byte("/x/app"), []byte("/y/fork")))
	want := `{"a":"/y/fork","b":"/y/fork/main.go","c":"/x/app-2/main.go","d":"cd /y/fork && ls","e":"/x/apps"}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestPrompts(t *testing.T) {
	for _, c := range []struct {
		line string
		want string
	}{
		{`{"type":"user","message":{"content":"hello"}}`, "hello"},
		{`{"type":"user","message":{"content":[{"type":"text","text":"hi"}]}}`, "hi"},
		{`{"type":"user","message":{"content":[{"type":"tool_result","content":"x"}]}}`, ""},
		{`{"type":"user","isMeta":true,"message":{"content":"Caveat"}}`, ""},
		{`{"type":"user","message":{"content":"<command-name>/model</command-name>\n<command-args>opus</command-args>"}}`, "/model opus"},
		{`{"type":"user","message":{"content":"<local-command-stdout>Set model</local-command-stdout>"}}`, ""},
		{`{"type":"user","message":{"content":"<bash-input>ls</bash-input>"}}`, "! ls"},
		{`{"type":"assistant","message":{"content":"hello"}}`, ""},
	} {
		var l line
		json.Unmarshal([]byte(c.line), &l)
		if got := l.promptText(); got != c.want {
			t.Errorf("%s: %q, want %q", c.line, got, c.want)
		}
	}
}
