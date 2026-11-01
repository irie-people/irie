package filetx

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestMain lets the test binary be another process that writes: one that
// counts in a file, or one that holds a file's lock until it is killed or
// its test ends, which closes its stdin.
func TestMain(m *testing.M) {
	path := os.Getenv("FILETX_PATH")
	switch os.Getenv("FILETX_DO") {
	case "count":
		n, _ := strconv.Atoi(os.Getenv("FILETX_TIMES"))
		for range n {
			if err := Update(path, 0o600, increment); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		os.Exit(0)
	case "hold":
		if _, err := Lock(LockPath(path), time.Second); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("held")
		io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// increment adds one to the count in a file, which starts at none.
func increment(old []byte) ([]byte, error) {
	n := 0
	if len(old) > 0 {
		var err error
		if n, err = strconv.Atoi(string(bytes.TrimSpace(old))); err != nil {
			return nil, fmt.Errorf("the count is %q: %w", old, err)
		}
	}
	return []byte(strconv.Itoa(n+1) + "\n"), nil
}

// writer starts the test binary as another process that writes path.
func writer(t *testing.T, do, path string, env ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), append([]string{"FILETX_DO=" + do, "FILETX_PATH=" + path}, env...)...)
	cmd.Stderr = os.Stderr
	return cmd
}

// Writers in other processes and in this one each add to one count, and
// no addition is lost.
func TestUpdateTakesTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "count")
	const procs, goroutines, times = 3, 3, 40
	var cmds []*exec.Cmd
	for range procs {
		cmd := writer(t, "count", path, "FILETX_TIMES="+strconv.Itoa(times))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*times)
	for range goroutines {
		wg.Go(func() {
			for range times {
				if err := Update(path, 0o600, increment); err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Errorf("a writer process: %v", err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%d\n", (procs+goroutines)*times); string(b) != want {
		t.Fatalf("the count is %q, want %q: a writer's change was lost", b, want)
	}
}

// A writer holding the lock keeps the others out, and its death, by
// SIGKILL even, lets them in.
func TestTheLockGoesWithItsHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	cmd := writer(t, "hold", path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	defer cmd.Process.Kill()
	if line, err := bufio.NewReader(out).ReadString('\n'); line != "held\n" {
		t.Fatalf("the holder said %q: %v", line, err)
	}

	defer func(w time.Duration) { wait = w }(wait)
	wait = 100 * time.Millisecond
	start := time.Now()
	if err := WriteFile(path, []byte("{}\n"), 0o600); !errors.Is(err, ErrBusy) {
		t.Fatalf("WriteFile while another process holds the lock: %v, want ErrBusy", err)
	}
	if d := time.Since(start); d < wait {
		t.Errorf("WriteFile gave up after %v, before its wait of %v", d, wait)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a write that never had its turn made the file: %v", err)
	}

	cmd.Process.Kill()
	cmd.Wait()
	if err := WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile once the holder died: %v", err)
	}
}

// A reader, which takes no lock, sees the old file or the new one and
// never part of either.
func TestReadersSeeWholeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a, b := bytes.Repeat([]byte("a"), 1<<20), bytes.Repeat([]byte("b"), 1<<20)
	if err := WriteFile(path, a, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(got, a) && !bytes.Equal(got, b) {
				t.Errorf("read %d bytes that are neither file", len(got))
				return
			}
		}
	})
	for i := range 20 {
		data := a
		if i%2 == 0 {
			data = b
		}
		if err := WriteFile(path, data, 0o600); err != nil {
			t.Error(err)
			break
		}
	}
	close(done)
	wg.Wait()
}

// A new file gets perm and a file replaced keeps its mode. A link stays a
// link, to the file that changed, and its lock is beside the link.
func TestWriteFileKeepsModesAndLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkFile(t, path, "one\n", 0o600)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkFile(t, path, "two\n", 0o644)

	dotfiles := t.TempDir()
	real := filepath.Join(dotfiles, "irie.toml")
	if err := os.WriteFile(real, []byte("mine\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.toml")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(link, []byte("three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("the link is gone: %v %v", fi, err)
	}
	checkFile(t, real, "three\n", 0o640)
	checkDir(t, dotfiles, "irie.toml")
	checkDir(t, dir, ".config.toml.lock", ".linked.toml.lock", "config.toml", "linked.toml")
}

// A change that fails, or that changes nothing, leaves the file as it was,
// and a change of a file that is not there yet starts from nil.
func TestUpdateLeavesTheFileAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := WriteFile(path, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("refused")
	if err := Update(path, 0o600, func([]byte) ([]byte, error) { return []byte("lost\n"), refused }); err != refused {
		t.Fatalf("Update: %v, want the change's own error", err)
	}
	if err := Update(path, 0o600, func(old []byte) ([]byte, error) { return old, nil }); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("a change that changed nothing rewrote the file")
	}
	checkFile(t, path, "keep\n", 0o600)

	fresh := filepath.Join(dir, "new", "local.json")
	err = Update(fresh, 0o600, func(old []byte) ([]byte, error) {
		if old != nil {
			t.Errorf("a missing file read as %q", old)
		}
		return []byte("{}\n"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checkFile(t, fresh, "{}\n", 0o600)
}

// A write that fails leaves no temporary file behind.
func TestAFailedWriteLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("{}\n"), 0o600); err == nil {
		t.Fatal("WriteFile replaced a directory")
	}
	checkDir(t, dir, ".state.json.lock", "state.json")
}

func checkFile(t *testing.T, path, want string, mode fs.FileMode) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != want {
		t.Errorf("%s holds %q, want %q", filepath.Base(path), b, want)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != mode {
		t.Errorf("%s has mode %o, want %o", filepath.Base(path), got, mode)
	}
}

func checkDir(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s holds %q, want %q", dir, got, want)
	}
}
