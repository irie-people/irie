package worktree

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/irie-hq/irie/internal/testutil"
)

const readme = "# website\n"

// fixture makes a repo, website, as a checkout looks after some work: its
// commit holds a .gitignore and a .worktreeinclude, and it has local files
// git does not track, some listed for copying and some not.
func fixture(t *testing.T) string {
	t.Helper()
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "website"), map[string]string{
		"README.md":        readme,
		"src/main.go":      "package main\n",
		".gitignore":       ".env\nbuild/\n*.log\n",
		".worktreeinclude": ".env\n",
	})
	testutil.WriteFiles(t, repo, map[string]string{
		".claude/settings.local.json": "{\"permissions\":{\"allow\":[\"Bash(go test:*)\"]}}\n",
		"CLAUDE.local.md":             "Use the staging API.\n",
		".env":                        "API_KEY=example\n",
		"build/out.bin":               "ignored, not listed\n",
		"debug.log":                   "ignored, not listed\n",
	})
	return repo
}

func options(t *testing.T) Options {
	return Options{
		Root:         filepath.Join(t.TempDir(), "worktrees"),
		Base:         "HEAD",
		BranchPrefix: "irie/",
		Copy:         []string{".claude/settings.local.json", "CLAUDE.local.md"},
		Submodules:   true,
	}
}

// same reports whether two paths name the same directory, whatever
// symlinks lead there (macOS's temp dirs live behind one).
func same(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func gone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s exists (%v)", path, err)
	}
}

func TestAWorktreeHoldsHEADAndTheLocalFiles(t *testing.T) {
	repo := fixture(t)
	o := options(t)
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, o)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(o.Root, "website", "fix-it"); res.Dir != want || res.Slug != "fix-it" || res.Branch != "irie/fix-it" {
		t.Errorf("result = %+v, want %s on irie/fix-it", res, want)
	}

	// The local files arrive, byte for byte; nothing else untracked does.
	for _, f := range []string{".claude/settings.local.json", "CLAUDE.local.md", ".env"} {
		want, _ := os.ReadFile(filepath.Join(repo, f))
		if got, err := os.ReadFile(filepath.Join(res.Dir, f)); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s in the worktree = %q, %v; want %q", f, got, err, want)
		}
	}
	gone(t, filepath.Join(res.Dir, "build"))
	gone(t, filepath.Join(res.Dir, "debug.log"))
	if want := []string{".claude/settings.local.json", ".env", "CLAUDE.local.md"}; !slices.Equal(res.Copied, want) {
		t.Errorf("copied %q, want %q", res.Copied, want)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings %q", res.Warnings)
	}

	// Its tracked files are HEAD's, on a branch of its own that tracks nothing.
	if out := testutil.Git(t, res.Dir, "status", "--porcelain", "--untracked-files=no"); out != "" {
		t.Errorf("tracked files differ from HEAD:\n%s", out)
	}
	if a, b := testutil.Git(t, res.Dir, "rev-parse", "HEAD"), testutil.Git(t, repo, "rev-parse", "HEAD"); a != b {
		t.Errorf("worktree HEAD %s, main checkout HEAD %s", a, b)
	}
	if b := testutil.Git(t, res.Dir, "rev-parse", "--abbrev-ref", "HEAD"); b != "irie/fix-it" {
		t.Errorf("worktree branch %q", b)
	}
	if out, err := exec.Command("git", "-C", res.Dir, "rev-parse", "--abbrev-ref", "@{upstream}").CombinedOutput(); err == nil {
		t.Errorf("the branch tracks %s", out)
	}
	// The main checkout stays on its own branch.
	if b := testutil.Git(t, repo, "rev-parse", "--abbrev-ref", "HEAD"); b != "main" {
		t.Errorf("main checkout moved to %q", b)
	}
	// Irie's own directories are private.
	for _, d := range []string{o.Root, filepath.Join(o.Root, "website")} {
		if fi, err := os.Stat(d); err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", d, fi.Mode().Perm(), err)
		}
	}
}

func TestTheSameSlugAgainGetsANumber(t *testing.T) {
	repo := fixture(t)
	o := options(t)
	create := func(r Request) Result {
		t.Helper()
		r.Repo, r.Name = repo, "website"
		res, err := Create(r, o)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	create(Request{Slug: "fix-it"})
	if res := create(Request{Slug: "fix-it"}); res.Slug != "fix-it-2" || res.Branch != "irie/fix-it-2" || res.Dir != filepath.Join(o.Root, "website", "fix-it-2") {
		t.Errorf("second = %+v", res)
	}
	if res := create(Request{Slug: "fix-it"}); res.Slug != "fix-it-3" {
		t.Errorf("third = %+v", res)
	}
	// A branch made by hand, a directory left behind and a name taken
	// elsewhere all count.
	testutil.Git(t, repo, "branch", "irie/docs")
	if res := create(Request{Slug: "docs"}); res.Slug != "docs-2" {
		t.Errorf("after a branch irie/docs: %+v", res)
	}
	if err := os.MkdirAll(filepath.Join(o.Root, "website", "stale"), 0o700); err != nil {
		t.Fatal(err)
	}
	if res := create(Request{Slug: "stale"}); res.Slug != "stale-2" {
		t.Errorf("after a stale directory: %+v", res)
	}
	if res := create(Request{Slug: "x", Taken: func(s string) bool { return s == "x" }}); res.Slug != "x-2" || res.Branch != "irie/x-2" {
		t.Errorf("with x taken: %+v", res)
	}
}

func TestADirtyCheckoutStaysBehind(t *testing.T) {
	repo := fixture(t)
	testutil.WriteFiles(t, repo, map[string]string{
		"README.md":  readme + "A half-done edit.\n",
		"src/new.go": "package main\n",
	})
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, options(t))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(res.Dir, "README.md")); string(b) != readme {
		t.Errorf("README.md in the worktree = %q, want HEAD's", b)
	}
	gone(t, filepath.Join(res.Dir, "src", "new.go"))
	w := strings.Join(res.Warnings, "\n")
	if !strings.Contains(w, "2 uncommitted changes") || !strings.Contains(w, "not carried over") {
		t.Errorf("warnings %q, want one about 2 uncommitted changes", res.Warnings)
	}
}

func TestBaseNamesTheStart(t *testing.T) {
	repo := fixture(t)
	first := testutil.Git(t, repo, "rev-parse", "HEAD")
	testutil.WriteFiles(t, repo, map[string]string{"CHANGES.md": "One change.\n"})
	testutil.Git(t, repo, "add", "CHANGES.md")
	testutil.Git(t, repo, "commit", "-q", "-m", "Second commit")
	o := options(t)
	o.Base = first
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "old"}, o)
	if err != nil {
		t.Fatal(err)
	}
	if got := testutil.Git(t, res.Dir, "rev-parse", "HEAD"); got != first {
		t.Errorf("worktree HEAD %s, want %s", got, first)
	}
	gone(t, filepath.Join(res.Dir, "CHANGES.md"))
}

func TestARepoWithNoCommitsFailsClearly(t *testing.T) {
	testutil.IsolateGit(t)
	testutil.NeedGit(t)
	dir := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, dir, "init", "-q", "-b", "main")
	o := options(t)
	_, err := Create(Request{Repo: dir, Name: "empty", Slug: "x"}, o)
	if err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Fatalf("err = %v, want one saying the repo has no commits", err)
	}
	gone(t, filepath.Join(o.Root, "empty", "x"))
	if out := testutil.Git(t, dir, "branch", "--list", "irie/*"); out != "" {
		t.Errorf("branches left behind: %q", out)
	}
}

func TestSubmodulesAreInitialised(t *testing.T) {
	testutil.IsolateGit(t)
	// Local submodules travel over git's file transport, which git allows
	// only when asked.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	tmp := t.TempDir()
	lib := testutil.NewRepo(t, filepath.Join(tmp, "lib"), map[string]string{"lib.go": "package lib\n"})
	app := testutil.NewRepo(t, filepath.Join(tmp, "app"), map[string]string{"main.go": "package main\n"})
	testutil.Git(t, app, "submodule", "add", "-q", lib, "lib")
	testutil.Git(t, app, "commit", "-q", "-m", "Add lib")

	o := options(t)
	res, err := Create(Request{Repo: app, Name: "app", Slug: "with"}, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "lib", "lib.go")); err != nil {
		t.Errorf("the submodule was not checked out: %v (warnings %q)", err, res.Warnings)
	}
	o.Submodules = false
	res, err = Create(Request{Repo: app, Name: "app", Slug: "without"}, o)
	if err != nil {
		t.Fatal(err)
	}
	gone(t, filepath.Join(res.Dir, "lib", "lib.go"))
}

func TestCopiesNeverFollowASymlinkOutOfTheWorktree(t *testing.T) {
	testutil.IsolateGit(t)
	tmp := t.TempDir()
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "web")
	testutil.NewRepo(t, repo, map[string]string{".gitignore": "conf/secret.json\n", ".worktreeinclude": "conf/secret.json\n"})
	// HEAD holds conf as a link to a directory outside; the main checkout
	// has swapped the link for a directory with a local file in it.
	if err := os.Symlink(outside, filepath.Join(repo, "conf")); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "add", "conf")
	testutil.Git(t, repo, "commit", "-q", "-m", "Link conf")
	if err := os.Remove(filepath.Join(repo, "conf")); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFiles(t, repo, map[string]string{"conf/secret.json": "{}\n"})

	res, err := Create(Request{Repo: repo, Name: "web", Slug: "x"}, options(t))
	if err != nil {
		t.Fatal(err)
	}
	gone(t, filepath.Join(outside, "secret.json"))
	if slices.Contains(res.Copied, "conf/secret.json") {
		t.Errorf("copied %q", res.Copied)
	}
	if w := strings.Join(res.Warnings, "\n"); !strings.Contains(w, "conf/secret.json") {
		t.Errorf("warnings %q, want one naming conf/secret.json", res.Warnings)
	}
}

func TestFindReportsTheCheckout(t *testing.T) {
	repo := fixture(t)
	r, ok, err := Find(filepath.Join(repo, "src"))
	if err != nil || !ok || r.Top != repo || r.Main != repo || r.Sub != "src" {
		t.Errorf("Find(src) = %+v, %v, %v", r, ok, err)
	}
	if r, ok, err := Find(t.TempDir()); ok || err != nil {
		t.Errorf("Find outside git = %+v, %v, %v", r, ok, err)
	}
	if _, _, err := Find("relative/dir"); err == nil {
		t.Error("Find of a relative path did not fail")
	}

	// A linked worktree knows the main checkout it came from.
	res, err := Create(Request{Repo: repo, Name: "website", Slug: "fix-it"}, options(t))
	if err != nil {
		t.Fatal(err)
	}
	r, ok, err = Find(res.Dir)
	if err != nil || !ok || r.Top != res.Dir || !same(r.Main, repo) || r.Sub != "" {
		t.Errorf("Find(worktree) = %+v, %v, %v", r, ok, err)
	}

	// A path through a symlink keeps the form it was given.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	r, ok, err = Find(filepath.Join(link, "src"))
	if err != nil || !ok || r.Top != link || r.Sub != "src" || !same(r.Main, repo) {
		t.Errorf("Find(link/src) = %+v, %v, %v", r, ok, err)
	}
}
