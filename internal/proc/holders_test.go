package proc

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

func TestHoldingFindsWhatWorksInAFolderOrHasAFileOpenThere(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no look at what processes hold here")
	}
	base := t.TempDir()
	tree, other, elsewhere := filepath.Join(base, "tree"), filepath.Join(base, "other"), filepath.Join(base, "elsewhere")
	for _, d := range []string{filepath.Join(tree, "sub"), other, elsewhere} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(tree, "open.txt")
	if err := os.WriteFile(file, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// One works in a folder under tree; one works elsewhere with a file of
	// tree open.
	worker := exec.Command("sleep", "30")
	worker.Dir = filepath.Join(tree, "sub")
	opener := exec.Command("/bin/sh", "-c", "exec 3<\"$1\"; exec sleep 30", "sh", file)
	opener.Dir = elsewhere
	for _, c := range []*exec.Cmd{worker, opener} {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Process.Kill(); c.Wait() })
	}

	// The shell gives way to sleep with the file still open.
	var held map[string][]Holder
	deadline := time.Now().Add(10 * time.Second)
	for {
		var err error
		if held, err = Holding([]string{tree, other}); err != nil {
			t.Fatal(err)
		}
		if len(held[tree]) >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	has := func(pid int, what string) bool {
		return slices.ContainsFunc(held[tree], func(h Holder) bool { return h.PID == pid && (what == "" || h.What == what) })
	}
	if !has(worker.Process.Pid, "cwd") {
		t.Errorf("the process at work in it is not among %+v", held[tree])
	}
	if !has(opener.Process.Pid, "") {
		t.Errorf("the process with a file open in it is not among %+v", held[tree])
	}
	if len(held[other]) != 0 {
		t.Errorf("nothing runs in the other folder, but %+v", held[other])
	}
}

func TestHoldingMatchesWholeFoldersInEitherForm(t *testing.T) {
	real := t.TempDir()
	if err := os.Mkdir(filepath.Join(real, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	all := []Holder{
		{PID: 1, What: "cwd", Path: filepath.Join(resolved, "a")},
		{PID: 2, What: "3", Path: resolved + "-2/b"},
		{PID: 3, What: "txt", Path: filepath.Join(resolved, "a", "bin")},
	}
	got := holding(all, []string{link, filepath.Join(link, "a")}, fileKey)
	if pids := pidsOf(got[link]); !slices.Equal(pids, []int{1, 3}) {
		t.Errorf("held through the link: %v", pids)
	}
	// The inner folder is held by what is in it, and so is the outer.
	if pids := pidsOf(got[filepath.Join(link, "a")]); !slices.Equal(pids, []int{1, 3}) {
		t.Errorf("held inner: %v", pids)
	}
}

func TestHoldingKnowsAFolderByItsFileWhateverItsName(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(base, "caf\u00e9", "Tree")
	if err := os.MkdirAll(tree, 0o700); err != nil {
		t.Fatal(err)
	}
	// lsof under a locale that cannot print the letter escapes its bytes.
	escaped := filepath.Join(base, `caf\xc3\xa9`, "Tree", "x.txt")
	all := []Holder{{PID: 1, What: "cwd", Path: escaped}}
	// A disk that ignores case, as macOS's do by default, names it as it is
	// on disk while the dir may be given in another case.
	lower := filepath.Join(base, "caf\u00e9", "tree")
	folds := false
	if fi, err := os.Stat(lower); err == nil {
		want, _ := os.Stat(tree)
		folds = os.SameFile(fi, want)
	}
	if folds {
		all = append(all, Holder{PID: 2, What: "cwd", Path: filepath.Join(tree, "sub")})
	}
	dir := tree
	if folds {
		dir = lower
	}
	got := holding(all, []string{dir}, fileKey)
	want := []int{1}
	if folds {
		want = []int{1, 2}
	}
	if pids := pidsOf(got[dir]); !slices.Equal(pids, want) {
		t.Errorf("held %v, want %v (case folds: %v)", pids, want, folds)
	}
	if u := unescape(`a\x41\tb^@`); u != "aA\tb\x00" {
		t.Errorf("unescape = %q", u)
	}
}

func pidsOf(hs []Holder) []int {
	var out []int
	for _, h := range hs {
		out = append(out, h.PID)
	}
	return out
}
