package worktree

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/testutil"
)

// hooked gives the fixture's repo a post-checkout hook, as a repo of its
// own would have, that notes each checkout in an ignored file.
func hooked(t *testing.T, repo string) {
	t.Helper()
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nmkdir -p build && echo ran >> build/hooked\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

// canGo fails the test unless CheckDrop finds the worktree can go.
func canGo(t *testing.T, dir, repo, branch, land string, copied []string) Landed {
	t.Helper()
	l, err := CheckDrop(dir, repo, branch, land, copied, wait)
	if err != nil || l.Keep != "" || l.Gone {
		t.Fatalf("CheckDrop = %+v, %v; want it can go", l, err)
	}
	return l
}

// keeps fails the test unless CheckDrop keeps the worktree, saying why.
func keeps(t *testing.T, dir, repo, branch, land string, copied []string, why string) {
	t.Helper()
	l, err := CheckDrop(dir, repo, branch, land, copied, wait)
	if err != nil || !strings.HasPrefix(l.Keep, why) {
		t.Errorf("CheckDrop = %+v, %v; want it kept: %s", l, err, why)
	}
}

func TestALandedWorktreeGoesAndComesBackFromItsBranch(t *testing.T) {
	repo := fixture(t)
	testutil.Git(t, repo, "branch", "live")
	hooked(t, repo)
	o := options(t)
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, o)
	if err != nil {
		t.Fatal(err)
	}
	dir := res.Dir
	if _, err := os.Stat(filepath.Join(dir, "build", "hooked")); err != nil {
		t.Fatalf("the hook did not run for the new worktree: %v", err)
	}
	// Build output git ignores does not count.
	testutil.WriteFiles(t, dir, map[string]string{"build/out.bin": "built\n", "debug.log": "log\n"})
	l := canGo(t, dir, repo, res.Branch, "live", res.Copied)
	if l.Branch != "irie/fix-it" || l.Commit != testutil.Git(t, repo, "rev-parse", "live") {
		t.Errorf("found %+v", l)
	}
	if n := DiskUsage(dir); n <= 0 {
		t.Errorf("disk usage %d", n)
	}

	if err := Drop(dir, repo, res.Copied, wait); err != nil {
		t.Fatal(err)
	}
	gone(t, dir)
	if b := testutil.Git(t, repo, "rev-parse", "--verify", "refs/heads/irie/fix-it"); b != l.Commit {
		t.Errorf("the branch is at %q, want %s", b, l.Commit)
	}
	if out := testutil.Git(t, repo, "worktree", "list", "--porcelain"); strings.Contains(out, "fix-it") {
		t.Errorf("git still lists it:\n%s", out)
	}
	// The main checkout's own local files are untouched.
	for _, f := range res.Copied {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Errorf("the repo's %s: %v", f, err)
		}
	}
	if l, err := CheckDrop(dir, repo, res.Branch, "live", res.Copied, wait); err != nil || !l.Gone {
		t.Errorf("CheckDrop of a dropped worktree = %+v, %v", l, err)
	}

	// It comes back at the same path, on its branch, with its hook run
	// and its local files copied in again.
	back, err := Restore(repo, dir, res.Branch, o)
	if err != nil {
		t.Fatal(err)
	}
	if back.Dir != dir || back.Branch != res.Branch || !slices.Equal(back.Copied, res.Copied) {
		t.Errorf("restored %+v, want %+v", back, res)
	}
	if b := testutil.Git(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); b != "irie/fix-it" {
		t.Errorf("restored on %q", b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "build", "hooked")); err != nil || string(b) != "ran\n" {
		t.Errorf("the hook's note in the restored worktree = %q, %v", b, err)
	}
	for _, f := range res.Copied {
		want, _ := os.ReadFile(filepath.Join(repo, f))
		if got, err := os.ReadFile(filepath.Join(dir, f)); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s restored as %q, %v", f, got, err)
		}
	}
	canGo(t, dir, repo, res.Branch, "live", res.Copied)
}

func TestWorkNotLandedKeepsTheWorktree(t *testing.T) {
	repo := fixture(t)
	testutil.Git(t, repo, "branch", "live")
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, options(t))
	if err != nil {
		t.Fatal(err)
	}
	dir, branch, copied := res.Dir, res.Branch, res.Copied
	canGo(t, dir, repo, branch, "live", copied)

	// Changes and untracked files.
	testutil.WriteFiles(t, dir, map[string]string{"README.md": "# website, fixed\n"})
	keeps(t, dir, repo, branch, "live", copied, "1 file is not committed: README.md")
	testutil.Git(t, dir, "checkout", "--", "README.md")
	testutil.WriteFiles(t, dir, map[string]string{"notes.md": "a\n", "b.md": "b\n", "c.md": "c\n", "d.md": "d\n"})
	keeps(t, dir, repo, branch, "live", copied, "4 files are not committed: b.md, c.md, d.md and more")
	for _, f := range []string{"notes.md", "b.md", "c.md", "d.md"} {
		os.Remove(filepath.Join(dir, f))
	}
	// A local copy changed since Irie copied it in.
	testutil.WriteFiles(t, dir, map[string]string{"CLAUDE.local.md": "Use the live API.\n"})
	keeps(t, dir, repo, branch, "live", copied, "its copy of CLAUDE.local.md is not the repo's")
	// git itself refuses it too, and the copies Drop took out go back.
	if err := Drop(dir, repo, copied, wait); err == nil {
		t.Fatal("Drop removed a worktree whose copy changed")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md")); string(b) != "Use the live API.\n" {
		t.Errorf("the changed copy = %q", b)
	}
	orig, _ := os.ReadFile(filepath.Join(repo, "CLAUDE.local.md"))
	testutil.WriteFiles(t, dir, map[string]string{"CLAUDE.local.md": string(orig)})
	canGo(t, dir, repo, branch, "live", copied)

	// A commit not in live, until it lands.
	testutil.WriteFiles(t, dir, map[string]string{"fix.md": "fixed\n"})
	testutil.Git(t, dir, "add", "fix.md")
	testutil.Git(t, dir, "commit", "-q", "-m", "Fix it")
	keeps(t, dir, repo, branch, "live", copied, "1 commit is not in live")
	keeps(t, dir, repo, branch, "", copied, "its repo has no land branch (dog.land)")
	keeps(t, dir, repo, branch, "release", copied, "its repo has no branch release")
	testutil.Git(t, repo, "branch", "-f", "live", branch)
	canGo(t, dir, repo, branch, "live", copied)

	// What is checked out, and git's lock.
	testutil.Git(t, dir, "checkout", "-q", "--detach")
	keeps(t, dir, repo, branch, "live", copied, "its HEAD is detached, at ")
	testutil.Git(t, dir, "checkout", "-q", "-b", "other")
	keeps(t, dir, repo, branch, "live", copied, "it has other checked out, not irie/fix-it")
	testutil.Git(t, dir, "checkout", "-q", branch)
	testutil.Git(t, repo, "worktree", "lock", dir)
	keeps(t, dir, repo, branch, "live", copied, "git has it locked")
	testutil.Git(t, repo, "worktree", "unlock", dir)
	canGo(t, dir, repo, branch, "live", copied)

	// The repo's own checkout never goes.
	keeps(t, repo, repo, "main", "live", nil, "it is the repo's own checkout")
	// Nor does a folder git does not list.
	other := filepath.Join(t.TempDir(), "other")
	os.MkdirAll(other, 0o700)
	keeps(t, other, repo, branch, "live", nil, "git does not list it")

	// Untracked work that appears after the look stops git.
	testutil.WriteFiles(t, dir, map[string]string{"late.md": "late\n"})
	if err := Drop(dir, repo, copied, wait); err == nil {
		t.Fatal("Drop removed a worktree with untracked work")
	}
	for _, f := range append([]string{"late.md"}, copied...) {
		if _, err := os.Lstat(filepath.Join(dir, f)); err != nil {
			t.Errorf("after the refused drop, %s: %v", f, err)
		}
	}
}

func TestRestoreNeedsTheBranchAndAFreePath(t *testing.T) {
	repo := fixture(t)
	testutil.Git(t, repo, "branch", "live")
	o := options(t)
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(repo, res.Dir, res.Branch, o); err == nil || !strings.Contains(err.Error(), "is there already") {
		t.Errorf("Restore over a worktree that is there: %v", err)
	}
	// A worktree removed by hand, which git still lists, comes back.
	if err := os.RemoveAll(res.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(repo, res.Dir, res.Branch, o); err != nil {
		t.Fatalf("Restore of a worktree removed by hand: %v", err)
	}
	if err := Drop(res.Dir, repo, res.Copied, wait); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "branch", "-D", res.Branch)
	if _, err := Restore(repo, res.Dir, res.Branch, o); err == nil || !strings.Contains(err.Error(), "its branch irie/fix-it is gone") {
		t.Errorf("Restore with the branch gone: %v", err)
	}
	gone(t, res.Dir)
}

func TestReadBranchSaysWhatADroppedWorktreeLeftUnlanded(t *testing.T) {
	repo := fixture(t)
	testutil.Git(t, repo, "branch", "live")
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, options(t))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	testutil.WriteFiles(t, res.Dir, map[string]string{"fix.md": "fixed\n"})
	commitAt(t, res.Dir, "Fix it", start.Add(time.Minute))
	testutil.Git(t, repo, "branch", "-f", "live", res.Branch)
	if err := Drop(res.Dir, repo, res.Copied, wait); err != nil {
		t.Fatal(err)
	}
	l, err := ReadBranch(repo, res.Branch, "live", start, wait)
	if err != nil || l.Left() || l.Branch != res.Branch {
		t.Errorf("landed: %+v, %v", l, err)
	}
	// The land branch moves back: its commit is loose again.
	testutil.Git(t, repo, "branch", "-f", "live", "main")
	l, err = ReadBranch(repo, res.Branch, "live", start, wait)
	if err != nil || l.Ahead != 1 || len(l.Commits) != 1 || !strings.HasSuffix(l.Commits[0], " Fix it") {
		t.Errorf("after live moved back: %+v, %v", l, err)
	}
	if _, err := ReadBranch(repo, "irie/nope", "live", start, wait); err == nil {
		t.Error("ReadBranch of a branch that is not there")
	}
}
