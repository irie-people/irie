package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/testutil"
)

const wait = 10 * time.Second

// commitAt commits everything in dir with the given committer date.
func commitAt(t *testing.T, dir, msg string, at time.Time) {
	t.Helper()
	t.Setenv("GIT_COMMITTER_DATE", fmt.Sprintf("@%d +0000", at.Unix()))
	testutil.Git(t, dir, "add", "-A")
	testutil.Git(t, dir, "commit", "-q", "-m", msg)
	os.Unsetenv("GIT_COMMITTER_DATE")
}

func TestReadLooseNamesWhatHasNotLanded(t *testing.T) {
	repo := fixture(t)
	testutil.Git(t, repo, "branch", "live")
	r, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-typo"}, options(t))
	if err != nil {
		t.Fatal(err)
	}
	// The branch starts from a commit that is not in live yet, made before
	// the jot started: it is not the jot's own.
	start := time.Now()
	testutil.WriteFiles(t, r.Dir, map[string]string{"base.md": "from before\n"})
	commitAt(t, r.Dir, "Work from before the jot", start.Add(-time.Hour))

	l, err := ReadLoose(r.Dir, "live", start, r.Copied, wait)
	if err != nil {
		t.Fatal(err)
	}
	// The local files Irie copied in are not the jot's work either.
	if want := (Loose{Branch: "irie/fix-typo"}); !reflect.DeepEqual(l, want) || l.Left() {
		t.Errorf("a fresh worktree: %+v, want %+v", l, want)
	}

	testutil.WriteFiles(t, r.Dir, map[string]string{"README.md": "# website, fixed\n", "notes/todo.md": "- ship it\n", "has space.md": "x\n"})
	l, err = ReadLoose(r.Dir, "live", start, r.Copied, wait)
	if err != nil {
		t.Fatal(err)
	}
	if l.Dirty != 3 || !reflect.DeepEqual(l.Files, []string{"README.md", "has space.md", "notes/todo.md"}) || l.Ahead != 0 || !l.Left() {
		t.Errorf("uncommitted work: %+v", l)
	}

	commitAt(t, r.Dir, "Fix the typo", start.Add(time.Minute))
	l, err = ReadLoose(r.Dir, "live", start, r.Copied, wait)
	if err != nil {
		t.Fatal(err)
	}
	if l.Dirty != 0 || l.Ahead != 1 || len(l.Commits) != 1 || !strings.HasSuffix(l.Commits[0], " Fix the typo") {
		t.Errorf("a commit not in live: %+v", l)
	}
	// Without a land branch, a commit is as far as it can go.
	if l, err := ReadLoose(r.Dir, "", start, r.Copied, wait); err != nil || l.Left() {
		t.Errorf("no land branch: %+v, %v", l, err)
	}

	// It lands: live takes the branch.
	testutil.Git(t, repo, "branch", "-f", "live", "irie/fix-typo")
	if l, err := ReadLoose(r.Dir, "live", start, r.Copied, wait); err != nil || l.Left() || l.Ahead != 0 {
		t.Errorf("landed: %+v, %v", l, err)
	}

	// A land branch the repo lacks is said, not taken for landed work.
	if l, err := ReadLoose(r.Dir, "release", start, r.Copied, wait); err != nil || !l.NoLand {
		t.Errorf("a missing land branch: %+v, %v", l, err)
	}
}

// landing is a worktree whose branch, irie/spaces, starts from live, and a
// worktree of live beside it, which takes copies of its commits.
type landing struct {
	t      *testing.T
	dir    string // the branch's worktree
	live   string // live's
	start  time.Time
	copied []string
}

// newLanding makes the two worktrees, and on the branch a commit for each
// name, authored a minute apart from start, an hour ago.
func newLanding(t *testing.T, names ...string) landing {
	repo := fixture(t)
	testutil.Git(t, repo, "branch", "live")
	r, err := Create(Request{Repo: repo, Name: "website", Slug: "spaces"}, options(t))
	if err != nil {
		t.Fatal(err)
	}
	l := landing{t: t, dir: r.Dir, live: filepath.Join(t.TempDir(), "live"), start: time.Now().Add(-time.Hour), copied: r.Copied}
	testutil.Git(t, repo, "worktree", "add", "-q", l.live, "live")
	for i, name := range names {
		l.commit(l.dir, name, name+"\n", l.start.Add(time.Duration(i+1)*time.Minute))
	}
	return l
}

// commit writes name.md in dir and commits it as "Add name", authored and
// committed at at.
func (l landing) commit(dir, name, text string, at time.Time) {
	l.t.Helper()
	testutil.WriteFiles(l.t, dir, map[string]string{name + ".md": text})
	l.t.Setenv("GIT_AUTHOR_DATE", fmt.Sprintf("@%d +0000", at.Unix()))
	commitAt(l.t, dir, "Add "+name, at)
	os.Unsetenv("GIT_AUTHOR_DATE")
}

// rev is the commit rev names on the branch.
func (l landing) rev(rev string) string { return testutil.Git(l.t, l.dir, "rev-parse", rev) }

// adapt copies commit into live with more than its patch, as a cherry-pick
// that had to be adapted does: -C keeps its author, author time and message.
func (l landing) adapt(commit string) {
	l.t.Helper()
	testutil.Git(l.t, l.live, "cherry-pick", "-n", commit)
	testutil.WriteFiles(l.t, l.live, map[string]string{"notes-" + commit[:7] + ".md": "adapted for live\n"})
	testutil.Git(l.t, l.live, "add", "-A")
	testutil.Git(l.t, l.live, "commit", "-q", "-C", commit)
}

// expect checks that ahead of the branch's commits have not landed, the
// oldest of them "Add oldest", both as a close reads them and as the jot
// dog does.
func (l landing) expect(what string, ahead int, oldest string) {
	l.t.Helper()
	got, err := ReadCommits(l.dir, "live", l.start, wait)
	if err != nil || got.Branch != "irie/spaces" || got.Ahead != ahead || got.Dirty != 0 || len(got.Commits) != ahead ||
		ahead > 0 && !strings.HasSuffix(got.Commits[ahead-1], " Add "+oldest) {
		l.t.Errorf("%s: %+v, %v; want %d not landed, the oldest %q", what, got, err, ahead, oldest)
	}
	if got, err := ReadLoose(l.dir, "live", l.start, l.copied, wait); err != nil || got.Ahead != ahead {
		l.t.Errorf("%s: ReadLoose has %+v, %v; want %d not landed", what, got, err, ahead)
	}
}

// A commit has landed once its work is in the land branch, in whatever
// commit: a cherry-pick has the same patch, and one that had to be adapted
// keeps the author, the author time and the subject.
func TestACommitWhoseWorkIsInTheLandBranchHasLanded(t *testing.T) {
	l := newLanding(t, "a", "b", "c")
	l.expect("none landed", 3, "a")

	// a lands as a cherry-pick given another author time: only its patch
	// is the same.
	testutil.Git(t, l.live, "cherry-pick", l.rev("HEAD~2"))
	testutil.Git(t, l.live, "commit", "-q", "--amend", "--no-edit", fmt.Sprintf("--date=@%d +0000", l.start.Add(time.Hour).Unix()))
	l.expect("a cherry-picked", 2, "b")
	l.adapt(l.rev("HEAD~1"))
	l.expect("b adapted", 1, "c")
	// Another commit of c's subject, made at another time, is other work.
	l.commit(l.live, "c", "another c\n", l.start.Add(2*time.Hour))
	l.expect("a namesake of c", 1, "c")

	// Without a land branch, a commit is as far as it can go.
	if got, err := ReadCommits(l.dir, "", l.start, wait); err != nil || got.Ahead != 0 || got.NoLand {
		t.Errorf("no land branch: %+v, %v", got, err)
	}
	if got, err := ReadCommits(l.dir, "release", l.start, wait); err != nil || !got.NoLand {
		t.Errorf("a land branch the repo lacks: %+v, %v", got, err)
	}
	// A detached HEAD has no branch, and its commits count the same.
	testutil.Git(t, l.dir, "checkout", "-q", "--detach")
	if got, err := ReadCommits(l.dir, "live", l.start, wait); err != nil || got.Branch != "" || got.Ahead != 1 {
		t.Errorf("detached: %+v, %v", got, err)
	}
}

// A merge brings in what is on a branch already, so it is no work of its
// own; and a merge of the land branch, as tools/live.sh makes, does not hide
// the copies the land branch took before it.
func TestAMergeIsNoWorkOfItsOwn(t *testing.T) {
	l := newLanding(t, "a", "b")
	testutil.Git(t, l.live, "cherry-pick", l.rev("HEAD~1"))
	l.adapt(l.rev("HEAD"))
	l.commit(l.live, "d", "d\n", l.start.Add(10*time.Minute))
	l.expect("a and b copied", 0, "")
	testutil.Git(t, l.dir, "merge", "-q", "--no-edit", "-X", "theirs", "live")
	l.expect("live merged in", 0, "")
	l.commit(l.dir, "c", "c\n", l.start.Add(20*time.Minute))
	l.expect("c after the merge", 1, "c")

	// It lands: live takes the branch.
	testutil.Git(t, l.live, "merge", "-q", "--ff-only", "irie/spaces")
	l.expect("landed", 0, "")
}

func TestReadLooseCountsAllButNamesAFew(t *testing.T) {
	repo := fixture(t)
	files := map[string]string{}
	for i := range 8 {
		files[fmt.Sprintf("new/%d.md", i)] = "x\n"
	}
	testutil.WriteFiles(t, repo, files)
	l, err := ReadLoose(repo, "", time.Now(), []string{".claude/settings.local.json", "CLAUDE.local.md"}, wait)
	if err != nil {
		t.Fatal(err)
	}
	if l.Dirty != 8 || len(l.Files) != named || l.Branch != "main" {
		t.Errorf("%+v", l)
	}
	// A renamed file is one change.
	testutil.Git(t, repo, "add", "-A")
	testutil.Git(t, repo, "mv", "README.md", "READ ME.md")
	if l, err = ReadLoose(repo, "", time.Now(), []string{".claude/settings.local.json", "CLAUDE.local.md"}, wait); err != nil || l.Dirty != 9 {
		t.Errorf("after a rename: %+v, %v", l, err)
	}
}

func TestTreesListsTheWorktreesThatAreThere(t *testing.T) {
	repo := fixture(t)
	a, err := Create(Request{Repo: repo, Name: "website", Slug: "a"}, options(t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Create(Request{Repo: repo, Name: "website", Slug: "b"}, options(t))
	if err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, b.Dir, "checkout", "-q", "--detach")
	gone := t.TempDir()
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "gone", gone+"/c")
	os.RemoveAll(gone + "/c")

	trees, err := Trees(repo, wait)
	if err != nil {
		t.Fatal(err)
	}
	if len(trees) != 3 || !same(trees[0].Path, repo) || trees[0].Branch != "main" ||
		!same(trees[1].Path, a.Dir) || trees[1].Branch != "irie/a" || !same(trees[2].Path, b.Dir) || trees[2].Branch != "" {
		t.Errorf("trees = %+v", trees)
	}
}
