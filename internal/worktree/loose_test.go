package worktree

import (
	"fmt"
	"os"
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
