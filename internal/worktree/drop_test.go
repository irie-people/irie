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

// look is the drop's look at a worktree Create made, landing on live.
func look(res Result, repo string, o Options) Look {
	return Look{Dir: res.Dir, Repo: repo, Branch: res.Branch, Land: "live", Copied: res.Copied, Copy: o.Copy, Scan: true}
}

// canGo fails the test unless CheckDrop finds the worktree can go.
func canGo(t *testing.T, k Look) Landed {
	t.Helper()
	l, err := CheckDrop(k, wait)
	if err != nil || l.Keep != "" || l.Gone {
		t.Fatalf("CheckDrop = %+v, %v; want it can go", l, err)
	}
	return l
}

// keeps fails the test unless CheckDrop keeps the worktree, saying why.
func keeps(t *testing.T, k Look, why string) {
	t.Helper()
	l, err := CheckDrop(k, wait)
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
	l := canGo(t, look(res, repo, o))
	if l.Branch != "irie/fix-it" || l.Commit != testutil.Git(t, repo, "rev-parse", "live") || l.Bytes <= 0 {
		t.Errorf("found %+v", l)
	}
	for _, f := range res.Copied {
		if !slices.Contains(l.Local, f) {
			t.Errorf("its local files %q lack %s", l.Local, f)
		}
	}

	if partial, err := Drop(dir, repo, l.Local, wait); err != nil || partial {
		t.Fatal(partial, err)
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
	if l, err := CheckDrop(look(res, repo, o), wait); err != nil || !l.Gone {
		t.Errorf("CheckDrop of a dropped worktree = %+v, %v", l, err)
	}

	// It comes back at the same path, on its branch, with its hook run
	// and its local files copied in again.
	back, err := Restore(repo, dir, res.Branch, l.Commit, o)
	if err != nil {
		t.Fatal(err)
	}
	if back.Dir != dir || back.Branch != res.Branch || !slices.Equal(back.Copied, res.Copied) || len(back.Warnings) != 0 {
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
	canGo(t, look(res, repo, o))
}

func TestWorkNotLandedKeepsTheWorktree(t *testing.T) {
	repo := fixture(t)
	testutil.Git(t, repo, "branch", "live")
	o := options(t)
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, o)
	if err != nil {
		t.Fatal(err)
	}
	dir, branch := res.Dir, res.Branch
	k := look(res, repo, o)
	canGo(t, k)

	// Changes and untracked files.
	testutil.WriteFiles(t, dir, map[string]string{"README.md": "# website, fixed\n"})
	keeps(t, k, "1 file is not committed: README.md")
	testutil.Git(t, dir, "checkout", "--", "README.md")
	testutil.WriteFiles(t, dir, map[string]string{"notes.md": "a\n", "b.md": "b\n", "c.md": "c\n", "d.md": "d\n"})
	keeps(t, k, "4 files are not committed: b.md, c.md, d.md and more")
	for _, f := range []string{"notes.md", "b.md", "c.md", "d.md"} {
		os.Remove(filepath.Join(dir, f))
	}
	// A local copy changed since Irie copied it in.
	testutil.WriteFiles(t, dir, map[string]string{"CLAUDE.local.md": "Use the live API.\n"})
	keeps(t, k, "its local file CLAUDE.local.md differs from the repo's")
	// Drop refuses it too, and the copies it took out go back.
	if _, err := Drop(dir, repo, res.Copied, wait); err == nil {
		t.Fatal("Drop removed a worktree whose copy changed")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md")); string(b) != "Use the live API.\n" {
		t.Errorf("the changed copy = %q", b)
	}
	// A link to the repo's own copy, as a repo's hook may make, holds
	// nothing of its own.
	os.Remove(filepath.Join(dir, "CLAUDE.local.md"))
	if err := os.Symlink(filepath.Join(repo, "CLAUDE.local.md"), filepath.Join(dir, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}
	canGo(t, k)
	// A local file the session made where the repo has none would not
	// come back, ignored or not.
	k.Copy = append(slices.Clone(o.Copy), "notes.local.md")
	testutil.WriteFiles(t, dir, map[string]string{"notes.local.md": "mine\n"})
	keeps(t, k, "its local file notes.local.md is not in the repo")
	os.Remove(filepath.Join(dir, "notes.local.md"))
	testutil.WriteFiles(t, dir, map[string]string{".env.local": "KEY=mine\n"})
	testutil.WriteFiles(t, dir, map[string]string{".worktreeinclude": ".env\n.env.local\n", ".gitignore": ".env\n.env.local\nbuild/\n*.log\n"})
	testutil.Git(t, dir, "commit", "-q", "-am", "Keep .env.local local")
	testutil.Git(t, repo, "branch", "-f", "live", branch)
	keeps(t, k, "its local file .env.local is not in the repo")
	os.Remove(filepath.Join(dir, ".env.local"))
	canGo(t, k)

	// A commit not in live, until it lands.
	testutil.WriteFiles(t, dir, map[string]string{"fix.md": "fixed\n"})
	testutil.Git(t, dir, "add", "fix.md")
	testutil.Git(t, dir, "commit", "-q", "-m", "Fix it")
	keeps(t, k, "1 commit is not in live")
	k2 := k
	k2.Land = ""
	keeps(t, k2, "its repo has no land branch (dog.land)")
	k2.Land = "release"
	keeps(t, k2, "its repo has no branch release")
	testutil.Git(t, repo, "branch", "-f", "live", branch)
	canGo(t, k)

	// Changes git is told not to see.
	testutil.Git(t, dir, "update-index", "--assume-unchanged", "README.md")
	keeps(t, k, "1 file is hidden from git's status (assume-unchanged or skip-worktree): README.md")
	testutil.Git(t, dir, "update-index", "--no-assume-unchanged", "README.md")
	testutil.Git(t, dir, "update-index", "--skip-worktree", "fix.md")
	keeps(t, k, "1 file is hidden from git's status")
	testutil.Git(t, dir, "update-index", "--no-skip-worktree", "fix.md")

	// What is checked out, and git's lock.
	testutil.Git(t, dir, "checkout", "-q", "--detach")
	keeps(t, k, "its HEAD is detached, at ")
	testutil.Git(t, dir, "checkout", "-q", "-b", "other")
	keeps(t, k, "it has other checked out, not irie/fix-it")
	testutil.Git(t, dir, "checkout", "-q", branch)
	testutil.Git(t, repo, "worktree", "lock", dir)
	keeps(t, k, "git has it locked")
	testutil.Git(t, repo, "worktree", "unlock", dir)
	canGo(t, k)

	// Another checkout inside it, in a folder git ignores, as Claude Code
	// keeps its worktrees: git worktree remove would delete what it holds.
	inner := filepath.Join(dir, "build", "trees", "inner")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "inner", inner)
	testutil.WriteFiles(t, inner, map[string]string{"wip.md": "not committed\n"})
	keeps(t, k, "another worktree is inside it: ")
	testutil.Git(t, repo, "worktree", "remove", "--force", inner)
	clone := filepath.Join(dir, "build", "clone")
	if err := os.MkdirAll(clone, 0o700); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "init", "-q")
	keeps(t, k, "another checkout is inside it, at build/clone")
	os.RemoveAll(clone)
	// A read-only folder, such as a module cache's, which git could not
	// empty.
	ro := filepath.Join(dir, "build", "mod")
	testutil.WriteFiles(t, ro, map[string]string{"x.go": "package x\n"})
	os.Chmod(ro, 0o555)
	t.Cleanup(func() { os.Chmod(ro, 0o755) })
	keeps(t, k, "a folder in it is read-only, so git could not remove it: build/mod")
	os.Chmod(ro, 0o755)
	canGo(t, k)

	// The repo's own checkout never goes.
	keeps(t, Look{Dir: repo, Repo: repo, Branch: "main", Land: "live"}, "it is the repo's own checkout")
	// Nor does a folder git does not list.
	other := filepath.Join(t.TempDir(), "other")
	os.MkdirAll(other, 0o700)
	keeps(t, Look{Dir: other, Repo: repo, Branch: branch, Land: "live"}, "git does not list it")

	// Untracked work that appears after the look stops git.
	testutil.WriteFiles(t, dir, map[string]string{"late.md": "late\n"})
	if partial, err := Drop(dir, repo, res.Copied, wait); err == nil || partial {
		t.Fatalf("Drop of a worktree with untracked work: partial %v, %v", partial, err)
	}
	for _, f := range append([]string{"late.md"}, res.Copied...) {
		if _, err := os.Lstat(filepath.Join(dir, f)); err != nil {
			t.Errorf("after the refused drop, %s: %v", f, err)
		}
	}
}

func TestRestoreNeedsAFreePathAndMakesAGoneBranchAgain(t *testing.T) {
	repo := fixture(t)
	testutil.Git(t, repo, "branch", "live")
	o := options(t)
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, o)
	if err != nil {
		t.Fatal(err)
	}
	tip := testutil.Git(t, res.Dir, "rev-parse", "HEAD")
	if _, err := Restore(repo, res.Dir, res.Branch, tip, o); err == nil || !strings.Contains(err.Error(), "is there already") {
		t.Errorf("Restore over a worktree that is there: %v", err)
	}
	// A worktree removed by hand, which git still lists, comes back.
	if err := os.RemoveAll(res.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(repo, res.Dir, res.Branch, tip, o); err != nil {
		t.Fatalf("Restore of a worktree removed by hand: %v", err)
	}
	l := canGo(t, look(res, repo, o))
	if _, err := Drop(res.Dir, repo, l.Local, wait); err != nil {
		t.Fatal(err)
	}
	// Its branch, merged, is deleted: it is made again at its commit.
	testutil.Git(t, repo, "branch", "-D", res.Branch)
	back, err := Restore(repo, res.Dir, res.Branch, tip, o)
	if err != nil {
		t.Fatalf("Restore with the branch gone: %v", err)
	}
	if b := testutil.Git(t, res.Dir, "rev-parse", "HEAD"); b != tip || len(back.Warnings) != 1 || !strings.Contains(back.Warnings[0], "was gone, so it starts again at ") {
		t.Errorf("made again at %s, warnings %q", b, back.Warnings)
	}
	l = canGo(t, look(res, repo, o))
	if _, err := Drop(res.Dir, repo, l.Local, wait); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "branch", "-D", res.Branch)
	if _, err := Restore(repo, res.Dir, res.Branch, "", o); err == nil || !strings.Contains(err.Error(), "its branch irie/fix-it is gone") {
		t.Errorf("Restore with the branch and its commit unknown: %v", err)
	}
	gone(t, res.Dir)
}

func TestADropGitCouldNotFinishIsMovedAsideOnRestore(t *testing.T) {
	repo := fixture(t)
	testutil.Git(t, repo, "branch", "live")
	o := options(t)
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, o)
	if err != nil {
		t.Fatal(err)
	}
	l := canGo(t, look(res, repo, o))
	// A read-only folder that appears after the look: git forgets the
	// worktree but cannot delete it all.
	ro := filepath.Join(res.Dir, "build", "mod")
	testutil.WriteFiles(t, ro, map[string]string{"x.go": "package x\n"})
	os.Chmod(ro, 0o555)
	t.Cleanup(func() {
		for _, d := range []string{ro, filepath.Join(res.Dir+".left", "build", "mod")} {
			os.Chmod(d, 0o755)
		}
	})
	partial, err := Drop(res.Dir, repo, l.Local, wait)
	if err == nil || !partial {
		t.Fatalf("Drop with a read-only folder: partial %v, %v", partial, err)
	}
	back, err := Restore(repo, res.Dir, res.Branch, l.Commit, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Warnings) != 1 || !strings.Contains(back.Warnings[0], res.Dir+".left") {
		t.Errorf("warnings %q", back.Warnings)
	}
	if _, err := os.Stat(filepath.Join(res.Dir+".left", "build", "mod", "x.go")); err != nil {
		t.Errorf("what was left is not aside: %v", err)
	}
	if b := testutil.Git(t, res.Dir, "rev-parse", "--abbrev-ref", "HEAD"); b != res.Branch {
		t.Errorf("restored on %q", b)
	}
}

func TestReadBranchSaysWhatADroppedWorktreeLeftUnlanded(t *testing.T) {
	repo := fixture(t)
	testutil.Git(t, repo, "branch", "live")
	o := options(t)
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, o)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	testutil.WriteFiles(t, res.Dir, map[string]string{"fix.md": "fixed\n"})
	commitAt(t, res.Dir, "Fix it", start.Add(time.Minute))
	testutil.Git(t, repo, "branch", "-f", "live", res.Branch)
	l := canGo(t, look(res, repo, o))
	if _, err := Drop(res.Dir, repo, l.Local, wait); err != nil {
		t.Fatal(err)
	}
	got, err := ReadBranch(repo, res.Branch, "live", start, wait)
	if err != nil || got.Left() || got.Branch != res.Branch {
		t.Errorf("landed: %+v, %v", got, err)
	}
	// The land branch moves back: its commit is loose again.
	testutil.Git(t, repo, "branch", "-f", "live", "main")
	got, err = ReadBranch(repo, res.Branch, "live", start, wait)
	if err != nil || got.Ahead != 1 || len(got.Commits) != 1 || !strings.HasSuffix(got.Commits[0], " Fix it") {
		t.Errorf("after live moved back: %+v, %v", got, err)
	}
	if _, err := ReadBranch(repo, "irie/nope", "live", start, wait); err == nil {
		t.Error("ReadBranch of a branch that is not there")
	}
}
