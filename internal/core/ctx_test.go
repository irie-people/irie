package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ctxgit"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// transcriptLines writes Claude-shaped transcript lines (DESIGN §4.7) for a
// conversation in cwd: a turn a prompt and a reply that names a file under
// cwd. Synthetic, never recorded.
type transcriptLines struct {
	path, session, cwd, last string
	n                        int
	at                       time.Time
}

func (l *transcriptLines) turn(t *testing.T, prompt, reply string) {
	t.Helper()
	for _, m := range []map[string]any{
		{"type": "user", "message": map[string]any{"role": "user", "content": prompt}},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": reply + " See " + l.cwd + "/README.md."}}}},
	} {
		l.n++
		l.at = l.at.Add(time.Second)
		id := fmt.Sprintf("10000000-0000-4000-8000-%012d", l.n)
		m["uuid"], m["sessionId"], m["cwd"], m["timestamp"] = id, l.session, l.cwd, l.at.Format(time.RFC3339Nano)
		if l.last != "" {
			m["parentUuid"] = l.last
		}
		l.last = id
		b, _ := json.Marshal(m)
		appendFile(t, l.path, string(b)+"\n")
	}
}

func (c *coreEnv) context(a ipc.ContextArgs) ipc.ContextResult {
	c.t.Helper()
	var r ipc.ContextResult
	if err := c.call(ipc.OpContext, a, &r); err != nil {
		c.t.Fatalf("context %s: %v", a.Action, err)
	}
	return r
}

func storeGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir=" + repo}, args...)...)
	cmd.Env = ctxgit.Env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestContextsAreStoredAndForkedAtATurnsCode(t *testing.T) {
	testutil.IsolateGit(t)
	repo := website(t, filepath.Join(t.TempDir(), "website"))
	claudeDir := t.TempDir()
	c := setupWith(t, agentConfig{
		env:   map[string]string{"CLAUDE_CONFIG_DIR": claudeDir},
		extra: "[repos.aliases]\nweb = " + strconv.Quote(repo) + "\n",
	})
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Repo: repo})
	row := c.waitState(s.ID, ipc.StateDone)
	tx := &transcriptLines{
		path:    filepath.Join(c.paths.Session(s.ID), "transcripts", row.VendorID+".jsonl"),
		session: row.VendorID, cwd: s.Worktree, at: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
	}
	os.MkdirAll(filepath.Dir(tx.path), 0o700)

	// Turn one on the worktree's first commit; turn two after a commit.
	first := testutil.Git(t, s.Worktree, "rev-parse", "HEAD")
	tx.turn(t, "write the about page", "Wrote it.")
	one := c.context(ipc.ContextArgs{Action: ipc.ContextSync, Session: s.ID}).Commit
	testutil.WriteFiles(t, s.Worktree, map[string]string{"about.md": "# about\n"})
	testutil.Git(t, s.Worktree, "add", "about.md")
	testutil.Git(t, s.Worktree, "commit", "-q", "-m", "about")
	second := testutil.Git(t, s.Worktree, "rev-parse", "HEAD")
	tx.turn(t, "now the contact page", "Wrote that too.")
	log := c.context(ipc.ContextArgs{Action: ipc.ContextLog, Session: s.Name})
	if len(log.Turns) != 2 || log.Turns[0].Prompt != "now the contact page" || log.Turns[1].Commit != one ||
		log.Turns[1].Code != first || log.Turns[0].Code != second || log.Session != s.ID {
		t.Fatalf("log: %+v", log)
	}
	if got := storeGit(t, log.Repo, "log", "--format=%s", ctxgit.Branch(s.ID)); got != "now the contact page\nwrite the about page" {
		t.Fatalf("git log of the branch:\n%s", got)
	}

	// A tag names a point; a point resolves.
	tag := c.context(ipc.ContextArgs{Action: ipc.ContextTag, Session: s.ID, At: "1", Name: "about-done"})
	if tag.Commit != one {
		t.Fatalf("tag at %s, want %s", tag.Commit, one)
	}

	// A fork from turn one: a worktree at turn one's code, a transcript of
	// its own where Claude Code looks, its paths moved there, and a branch
	// that shares turn one.
	var f ipc.ForkResult
	if err := c.call(ipc.OpFork, ipc.ForkArgs{Session: s.ID, At: "about-done", Task: "try a gallery instead"}, &f); err != nil {
		t.Fatal(err)
	}
	if f.From != one || f.Turn != 1 || f.Session.Worktree == "" || f.Session.Worktree == s.Worktree || !strings.HasSuffix(f.Session.Name, "· fork") {
		t.Fatalf("fork: %+v", f)
	}
	if head := testutil.Git(t, f.Session.Worktree, "rev-parse", "HEAD"); head != first {
		t.Fatalf("the fork's worktree is at %s, want turn one's code %s", head, first)
	}
	if _, err := os.Stat(filepath.Join(f.Session.Worktree, "about.md")); err == nil {
		t.Fatal("the fork has a file from after turn one")
	}
	file := filepath.Join(claudeProjects(configProfile(claudeDir), f.Session.Cwd), f.Session.VendorID+".jsonl")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the fork's transcript: %v", err)
	}
	lines := string(b)
	if strings.Count(lines, "\n") != 2 || strings.Contains(lines, "contact page") || strings.Contains(lines, s.Worktree+"/") || strings.Contains(lines, s.Worktree+`"`) ||
		strings.Count(lines, `"sessionId":"`+f.Session.VendorID+`"`) != 2 || !strings.Contains(lines, f.Session.Worktree+"/README.md") {
		t.Fatalf("the fork's transcript:\n%s", lines)
	}
	if got := storeGit(t, log.Repo, "rev-parse", ctxgit.Branch(f.Session.ID)); got != one {
		t.Fatalf("the fork's branch is at %s, want %s", got, one)
	}

	// The agent carries the conversation on with the task as its prompt.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var argv []any
		for _, l := range c.agentLog(f.Session.ID) {
			if l["t"] == "start" {
				argv, _ = l["argv"].([]any)
			}
		}
		if len(argv) > 2 {
			last, _ := argv[len(argv)-1].(string)
			resumes := false
			for i := range argv[:len(argv)-1] {
				resumes = resumes || argv[i] == "--resume" && argv[i+1] == f.Session.VendorID
			}
			if !resumes {
				t.Fatalf("argv: %v", argv)
			}
			if !strings.HasPrefix(last, "[Irie] This conversation is a fork of") || !strings.HasSuffix(last, "try a gallery instead") {
				t.Fatalf("the fork's prompt: %q", last)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fork's agent never started")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The diff of the two shows where they parted.
	if base := storeGit(t, log.Repo, "merge-base", ctxgit.Branch(s.ID), ctxgit.Branch(f.Session.ID)); base != one {
		t.Fatalf("merge-base %s, want %s", base, one)
	}
}

// configProfile is a profile whose Claude Code keeps its files in dir.
func configProfile(dir string) config.Profile {
	return config.Profile{Env: map[string]string{"CLAUDE_CONFIG_DIR": dir}}
}
