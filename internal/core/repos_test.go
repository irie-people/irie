package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/testutil"
	"github.com/irie-hq/irie/internal/tmux"
)

// localFiles are a checkout's untracked local files that its worktrees get
// (DESIGN §8 M3): the two of worktree.copy's default, and one the repo's
// .worktreeinclude lists.
var localFiles = map[string]string{
	".claude/settings.local.json": "{\"permissions\":{\"allow\":[\"Bash(go test:*)\"]}}\n",
	"CLAUDE.local.md":             "Use the staging API.\n",
	".env":                        "API_KEY=example\n",
}

// website makes a git repo at dir with one commit, the local files and an
// ignored file that no list names. Call testutil.IsolateGit first.
func website(t *testing.T, dir string) string {
	t.Helper()
	repo := testutil.NewRepo(t, dir, map[string]string{
		"README.md":        "# website\n",
		".gitignore":       ".env\nbuild/\n",
		".worktreeinclude": ".env\n",
	})
	testutil.WriteFiles(t, repo, localFiles)
	testutil.WriteFiles(t, repo, map[string]string{"build/out.bin": "not copied\n"})
	return repo
}

// spawnArgs spawns a and fails the test when the spawn fails.
func (c *coreEnv) spawnArgs(a ipc.SpawnArgs) ipc.Session {
	c.t.Helper()
	var r ipc.SpawnResult
	if err := c.call("spawn", a, &r); err != nil {
		c.t.Fatalf("spawn %+v: %v", a, err)
	}
	return r.Session
}

// agentPanes counts the server's agent panes.
func (c *coreEnv) agentPanes() int {
	n := 0
	for _, p := range c.panes() {
		if p["@irie_role"] == tmux.RoleAgent {
			n++
		}
	}
	return n
}

// sameDir reports whether a and b name one directory, whatever symlinks
// lead there (macOS's temp dirs live behind one).
func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// restartCore kills the core under its supervisor and waits for the new one.
func (c *coreEnv) restartCore() {
	c.t.Helper()
	kill(c.t, c.supervised(tmux.AgentsSession+":core"))
	testutil.Eventually(c.t, 10*time.Second, "the core did not come back", func() bool {
		return c.call("hello", ipc.HelloArgs{Client: "test", API: ipc.API}, nil) == nil
	})
}

func TestAnAtRepoTaskRunsInANewWorktree(t *testing.T) {
	testutil.IsolateGit(t)
	repo := website(t, filepath.Join(t.TempDir(), "website"))
	c := setup(t)
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: repo})
	wt := filepath.Join(c.paths.Worktrees(), "website", "prompt")
	if s.Worktree != wt || s.Cwd != wt || s.Branch != "irie/prompt" || s.Name != "prompt" || !sameDir(s.Repo, repo) {
		t.Errorf("session = %+v, want the worktree %s on irie/prompt", s, wt)
	}
	if want := []string{".claude/settings.local.json", ".env", "CLAUDE.local.md"}; !slices.Equal(s.Copied, want) {
		t.Errorf("copied %q, want %q", s.Copied, want)
	}
	c.waitState(s.ID, ipc.StateWorking)

	// The worktree has the local files, and its tracked files are HEAD's.
	for f, body := range localFiles {
		if b, err := os.ReadFile(filepath.Join(wt, f)); err != nil || string(b) != body {
			t.Errorf("%s in the worktree = %q, %v", f, b, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(wt, "build")); err == nil {
		t.Error("an ignored file no list names was copied")
	}
	if out := testutil.Git(t, wt, "status", "--porcelain", "--untracked-files=no"); out != "" {
		t.Errorf("tracked files differ from HEAD:\n%s", out)
	}
	if a, b := testutil.Git(t, wt, "rev-parse", "HEAD"), testutil.Git(t, repo, "rev-parse", "HEAD"); a != b {
		t.Errorf("worktree HEAD %s, main checkout HEAD %s", a, b)
	}
	if b := testutil.Git(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); b != "irie/prompt" {
		t.Errorf("worktree branch %q", b)
	}

	// The agent works in the worktree.
	if log := c.agentLog(s.ID); len(log) == 0 || !sameDir(fmt.Sprint(log[0]["cwd"]), wt) {
		t.Errorf("agent log %v, want it started in %s", log, wt)
	}
	// The journal says where the task runs.
	for _, e := range c.events(s.ID) {
		if e.Kind == event.SpawnRequested && (e.Str("worktree") != wt || e.Str("branch") != "irie/prompt" || !sameDir(e.Str("repo"), repo)) {
			t.Errorf("spawn.requested = %+v", e.Data)
		}
	}

	// The same task in the same repo gets the next slug.
	again := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: repo})
	if again.Name != "prompt-2" || again.Branch != "irie/prompt-2" || again.Worktree != wt+"-2" {
		t.Errorf("second session = %+v", again)
	}

	// A restarted core knows each row's worktree.
	c.restartCore()
	if r, ok := c.row(s.ID); !ok || r.Worktree != wt || r.Branch != "irie/prompt" || !slices.Equal(r.Copied, s.Copied) || !sameDir(r.Repo, repo) {
		t.Errorf("row after a restart = %+v", r)
	}
}

func TestADirtyMainCheckoutStaysBehind(t *testing.T) {
	testutil.IsolateGit(t)
	repo := website(t, filepath.Join(t.TempDir(), "website"))
	testutil.WriteFiles(t, repo, map[string]string{"README.md": "# website\nA half-done edit.\n"})
	c := setup(t)
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: repo})
	if !slices.ContainsFunc(s.Warnings, func(w string) bool { return strings.Contains(w, "uncommitted") }) {
		t.Errorf("warnings %q, want one about the uncommitted change", s.Warnings)
	}
	if b, _ := os.ReadFile(filepath.Join(s.Worktree, "README.md")); string(b) != "# website\n" {
		t.Errorf("README.md in the worktree = %q, want HEAD's", b)
	}
	// The row keeps the warning, which the status strip shows.
	if r := c.waitState(s.ID, ipc.StateWorking); !slices.Equal(r.Warnings, s.Warnings) {
		t.Errorf("row warnings %q, spawn warnings %q", r.Warnings, s.Warnings)
	}
}

func TestNamesAndTargets(t *testing.T) {
	testutil.IsolateGit(t)
	tmp := t.TempDir()
	code := filepath.Join(tmp, "code")
	site := website(t, filepath.Join(code, "website"))
	testutil.NewRepo(t, filepath.Join(code, "a", "api"), map[string]string{"go.mod": "module example.com/a/api\n"})
	apiB := testutil.NewRepo(t, filepath.Join(code, "b", "api"), map[string]string{"go.mod": "module example.com/b/api\n"})
	plain := filepath.Join(tmp, "plain")
	if err := os.MkdirAll(plain, 0o700); err != nil {
		t.Fatal(err)
	}
	c := setupWith(t, agentConfig{extra: fmt.Sprintf("[repos]\nroots = [%q]\naliases = { web = %q }\n", code, site)})

	// @name! runs in the main checkout itself.
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: "web", InPlace: true})
	if s.Cwd != site || s.Worktree != "" || s.Branch != "" || !sameDir(s.Repo, site) {
		t.Errorf("@web! = %+v, want the main checkout %s", s, site)
	}
	if out := testutil.Git(t, site, "branch", "--list", "irie/*"); out != "" {
		t.Errorf("@web! made a branch: %q", out)
	}
	// A path git does not know runs in place.
	if s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: plain}); s.Cwd != plain || s.Worktree != "" || s.Repo != "" {
		t.Errorf("@%s = %+v, want it in place", plain, s)
	}
	// A repo found under repos.roots gets a worktree; its path under the
	// root picks one of two repos of the same name.
	if s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: "website"}); s.Worktree != filepath.Join(c.paths.Worktrees(), "website", s.Name) {
		t.Errorf("@website = %+v", s)
	}
	if s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: "b/api"}); !sameDir(s.Repo, apiB) || s.Worktree != filepath.Join(c.paths.Worktrees(), "api", s.Name) {
		t.Errorf("@b/api = %+v", s)
	}

	// An ambiguous name lists the candidates and spawns nothing.
	rows, panes := len(c.list()), c.agentPanes()
	err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Repo: "api"}, nil)
	var ie *ipc.Error
	if !errors.As(err, &ie) || ie.Code != ipc.CodeAmbiguous {
		t.Errorf("@api: %v, want an ambiguous error", err)
	}
	for _, want := range []string{filepath.Join(code, "a", "api"), filepath.Join(code, "b", "api")} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("@api: %v, want it to list %s", err, want)
		}
	}
	// A name that is no repo lists the closest names and spawns nothing.
	err = c.call("spawn", ipc.SpawnArgs{Task: "prompt", Repo: "websit"}, nil)
	if !errors.As(err, &ie) || ie.Code != ipc.CodeNotFound || !strings.Contains(err.Error(), "@website") {
		t.Errorf("@websit: %v, want it to suggest @website", err)
	}
	if len(c.list()) != rows || c.agentPanes() != panes {
		t.Errorf("failed spawns left rows or panes: %d → %d rows, %d → %d panes", rows, len(c.list()), panes, c.agentPanes())
	}
	// A path that does not exist spawns nothing.
	if err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Repo: filepath.Join(tmp, "missing")}, nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("@missing: %v", err)
	}
	// cwd runs in place, so it cannot go with a repo.
	if err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Repo: "web", Cwd: plain}, nil); err == nil {
		t.Error("spawn with both a cwd and a repo succeeded")
	}
}

func TestARecentRepoAnswersToItsName(t *testing.T) {
	testutil.IsolateGit(t)
	repo := website(t, filepath.Join(t.TempDir(), "elsewhere", "tools"))
	c := setup(t)
	// ctl spawn reads the same @ and + tokens as the command bar. A path
	// makes the repo a recent one...
	out, err := exec.Command(c.bin, "ctl", "spawn", "@"+repo, "+fake", "prompt").CombinedOutput()
	if err != nil {
		t.Fatalf("ctl spawn: %v\n%s", err, out)
	}
	if first, ok := c.row(strings.TrimSpace(string(out))); !ok || first.Worktree == "" || first.Profile != "fake" || !sameDir(first.Repo, repo) {
		t.Errorf("ctl spawn @%s +fake = %+v", repo, first)
	}
	// ...so its name resolves now, and after a restart, from the journal.
	if s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: "tools"}); !sameDir(s.Repo, repo) || s.Worktree == "" {
		t.Errorf("@tools = %+v", s)
	}
	c.restartCore()
	if s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: "tools"}); !sameDir(s.Repo, repo) {
		t.Errorf("@tools after a restart = %+v", s)
	}
	var r ipc.CompleteResult
	if err := c.call("complete", ipc.CompleteArgs{Text: "@to"}, &r); err != nil || !slices.Equal(r.Items, []string{"@tools"}) {
		t.Errorf("complete @to = %q, %v", r.Items, err)
	}
}

const genericProfiles = `
[profiles.cat]
harness = "generic"
command = "/bin/cat"
task = "paste"

[profiles.echo]
harness = "generic"
command = "/bin/sh"
args = ["-c", "printf 'task: %s\\n' \"$1\"; exec sleep 600", "sh"]
`

func TestAGenericProfileGetsItsTask(t *testing.T) {
	c := setupWith(t, agentConfig{readyTimeout: "3s", extra: genericProfiles})
	const task = "hello from a pasted task"
	cat := c.spawnArgs(ipc.SpawnArgs{Task: task, Profile: "cat"})
	if cat.Profile != "cat" {
		t.Errorf("profile %q", cat.Profile)
	}
	echo := c.spawnArgs(ipc.SpawnArgs{Task: "fix the flaky test", Profile: "echo"})
	screen := func(s ipc.Session) string {
		out, _ := c.TryTmux("capture-pane", "-p", "-t", s.Pane)
		return out
	}
	testutil.Eventually(t, 10*time.Second, "cat never showed the pasted task", func() bool {
		return strings.Contains(screen(cat), task)
	})
	testutil.Eventually(t, 10*time.Second, "the task was not the last argument", func() bool {
		return strings.Contains(screen(echo), "task: fix the flaky test")
	})

	// With the task in, a generic session counts as working. It has no
	// hooks, so it never turns stalled.
	c.waitState(cat.ID, ipc.StateWorking)
	c.waitState(echo.ID, ipc.StateWorking)
	time.Sleep(4 * time.Second)
	for _, s := range []ipc.Session{cat, echo} {
		if r, _ := c.row(s.ID); r.State != ipc.StateWorking {
			t.Errorf("%s is %s (%s) past the ready timeout", s.Name, r.State, r.Detail)
		}
		evs := c.events(s.ID)
		if !slices.ContainsFunc(evs, func(e event.Event) bool { return e.Kind == event.PromptSubmitted && e.Source == event.SourceIrie }) {
			t.Errorf("%s: journal kinds %v, want Irie's prompt.submitted", s.Name, kinds(evs))
		}
	}
	// The task went in once: the terminal echoes it and cat prints it back.
	if n := strings.Count(screen(cat), task); n > 2 {
		t.Errorf("the task shows %d times:\n%s", n, screen(cat))
	}

	// An unknown profile spawns nothing and names the profiles there are.
	err := c.call("spawn", ipc.SpawnArgs{Task: "x", Profile: "nope"}, nil)
	if err == nil || !strings.Contains(err.Error(), "nope") || !strings.Contains(err.Error(), "cat, claude, echo, fake, haiku, local, sonnet, +auto") {
		t.Errorf("+nope: %v", err)
	}
	var r ipc.CompleteResult
	if err := c.call("complete", ipc.CompleteArgs{Text: "+"}, &r); err != nil || !slices.Equal(r.Items, []string{"+auto", "+cat", "+claude", "+echo", "+fake", "+haiku", "+local", "+sonnet"}) {
		t.Errorf("complete + = %q, %v", r.Items, err)
	}
	// Each profile says what it runs and costs, a profile with live sessions
	// what one takes in memory too, and auto where it sends each class.
	if len(r.Notes) != len(r.Items) || !strings.Contains(r.Notes[0], "hard→claude") || !strings.HasPrefix(r.Notes[1], "/bin/cat") ||
		!strings.Contains(r.Notes[2], "opus-5-5 max") || !strings.Contains(r.Notes[6], "local, $0") || !strings.Contains(r.Notes[7], "sonnet-5-5 medium") {
		t.Errorf("notes = %q", r.Notes)
	}
}

func TestA5MiBTaskArrivesWhole(t *testing.T) {
	c := setup(t)
	task := strings.Repeat("A line of a very long task, with ünïcödé.\n", (5<<20)/45+1)
	if len(task) < 5<<20 {
		t.Fatalf("test setup: the task is %d bytes", len(task))
	}
	s := c.spawnArgs(ipc.SpawnArgs{Task: task})
	if b, err := os.ReadFile(c.paths.TaskFile(s.ID)); err != nil || !bytes.Equal(b, []byte(task)) {
		t.Fatalf("task.md is not the task (%d bytes, %v)", len(b), err)
	}
	c.waitState(s.ID, ipc.StateWorking)
	log := c.agentLog(s.ID)
	if len(log) == 0 {
		t.Fatal("no agent log")
	}
	argv, _ := log[0]["argv"].([]any)
	if len(argv) == 0 || argv[len(argv)-1] != "Your full task is in "+c.paths.TaskFile(s.ID)+". Read all of it before doing anything else." {
		t.Errorf("the agent's last argument is not the pointer to task.md")
	}
}

// TestTheCommandBarCompletesAndPastes drives the real sidebar: Tab completes
// a repo name through the core, and a paste over 8 KiB becomes a chip that
// the task gets whole.
func TestTheCommandBarCompletesAndPastes(t *testing.T) {
	testutil.IsolateGit(t)
	repo := website(t, filepath.Join(t.TempDir(), "website"))
	c := setupWith(t, agentConfig{extra: fmt.Sprintf("[ui]\nsidebar_width = 60\n\n[repos]\naliases = { website = %q }\n", repo)})
	screen := func() string {
		out, _ := c.TryTmux("capture-pane", "-p", "-t", tmux.Sidebar)
		return out
	}
	testutil.Eventually(t, 5*time.Second, "the sidebar did not draw", func() bool { return strings.Contains(screen(), "☀ Irie") })

	c.Tmux(t, "send-keys", "-t", tmux.Sidebar, "-l", "@webs")
	c.Tmux(t, "send-keys", "-t", tmux.Sidebar, "Tab")
	testutil.Eventually(t, 5*time.Second, "Tab did not complete @webs", func() bool { return strings.Contains(screen(), "@website") })

	// tmux pastes newlines as carriage returns, as a terminal does.
	body := strings.Repeat("prompt\n", 2000)
	file := filepath.Join(t.TempDir(), "paste.txt")
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Tmux(t, "load-buffer", "-b", "big", file)
	c.Tmux(t, "paste-buffer", "-p", "-d", "-b", "big", "-t", tmux.Sidebar)
	testutil.Eventually(t, 5*time.Second, "the paste did not become a chip", func() bool {
		return strings.Contains(screen(), "@website [paste 14.0 KB · 2,000 lines]")
	})
	c.Tmux(t, "send-keys", "-t", tmux.Sidebar, "Enter")
	var s ipc.Session
	testutil.Eventually(t, 15*time.Second, "Enter did not spawn the task", func() bool {
		if ls := c.list(); len(ls) == 1 {
			s = ls[0]
		}
		return s.ID != ""
	})
	if !sameDir(s.Repo, repo) || s.Worktree == "" {
		t.Errorf("session = %+v, want a worktree of %s", s, repo)
	}
	if b, err := os.ReadFile(c.paths.TaskFile(s.ID)); err != nil || string(b) != body {
		t.Errorf("task.md holds %d bytes (%v), want the %d-byte paste", len(b), err, len(body))
	}
}
