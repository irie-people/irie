//go:build darwin || linux

package sysload

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests hold tools/heavy.sh, which keeps test and build runs from
// stacking up on the machine and from starting on a disk too full for the
// swap to grow, and the scripts that call it, to the rules Room sets.

// heavy runs tools/heavy.sh, and the scripts that call it, on a machine of
// the test's making: a fake sysctl and df first on PATH, which say the
// RAM, the share of it free and the free disk, and Irie's data in a folder
// of its own, so that the real machine's runs are none of its business.
type heavy struct {
	t    *testing.T
	root string // the repository
	bin  string // the fakes
	home string
	env  []string // more of the environment, as IRIE_HEAVY_SLOTS=1
}

func newHeavy(t *testing.T, env ...string) *heavy {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	h := &heavy{t: t, root: root, bin: t.TempDir(), home: t.TempDir(), env: env}
	h.write("sysctl", `#!/bin/sh
# sysctl -n NAME: what the file of that name beside this one holds.
[ "$1" = -n ] && [ -f "$(dirname "$0")/$2" ] || exit 1
cat "$(dirname "$0")/$2"
`)
	h.write("df", `#!/bin/sh
# df -Pk VOLUME: df.free kB free, as POSIX's df -P writes it.
echo "$2" >"$(dirname "$0")/df.volume"
echo "Filesystem 1024-blocks Used Available Capacity Mounted on"
echo "/dev/fake 1073741824 0 $(cat "$(dirname "$0")/df.free") 1% $2"
`)
	return h
}

func (h *heavy) write(name, body string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.bin, name), []byte(body), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

// machine sets the RAM, the share of it free, in percent, and the free
// disk.
func (h *heavy) machine(ram, freePercent, disk int64) {
	h.write("hw.memsize", strconv.FormatInt(ram, 10))
	h.write("kern.memorystatus_level", strconv.FormatInt(freePercent, 10))
	h.write("df.free", strconv.FormatInt(disk/1024, 10))
}

func (h *heavy) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = h.root
	cmd.Env = append([]string{
		"PATH=" + h.bin + ":" + os.Getenv("PATH"),
		"HOME=" + h.home,
		"XDG_DATA_HOME=" + filepath.Join(h.home, "data"),
	}, h.env...)
	// Its own process group, so that a script cut short leaves nothing.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return cmd
}

// run runs tools/heavy.sh to its end, and gives what it said.
func (h *heavy) run(args ...string) (string, error) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := h.command(ctx, "sh", append([]string{"tools/heavy.sh"}, args...)...)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// started is a script that runs on while the test reads what it says.
type started struct {
	cmd   *exec.Cmd
	lines chan string
	done  chan error
}

// start starts NAME ARGS... and reads what it says, a line at a time.
func (h *heavy) start(name string, args ...string) *started {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := h.command(ctx, name, args...)
	r, w, err := os.Pipe()
	if err != nil {
		h.t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	w.Close()
	s := &started{cmd: cmd, lines: make(chan string, 100), done: make(chan error, 1)}
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		close(s.lines)
	}()
	go func() { s.done <- cmd.Wait() }()
	h.t.Cleanup(func() {
		cancel()
		<-s.done
		r.Close()
	})
	return s
}

// await waits for a line that holds want, and fails at what it said
// instead.
func (s *started) await(t *testing.T, want string) string {
	t.Helper()
	var said []string
	deadline := time.After(time.Minute)
	for {
		select {
		case l, ok := <-s.lines:
			if !ok {
				t.Fatalf("it ended without saying %q; it said:\n%s", want, strings.Join(said, "\n"))
			}
			if strings.Contains(l, want) {
				return l
			}
			said = append(said, l)
		case <-deadline:
			t.Fatalf("a minute without saying %q; it said:\n%s", want, strings.Join(said, "\n"))
		}
	}
}

// ends waits for the script's end, and fails if it doesn't end in a minute
// or ends in an error.
func (s *started) ends(t *testing.T) {
	t.Helper()
	select {
	case err := <-s.done:
		s.done <- err
		if err != nil {
			t.Fatalf("it failed: %v", err)
		}
	case <-time.After(time.Minute):
		t.Fatal("it was still waiting a minute later")
	}
}

// running says whether the script is still going.
func (s *started) running() bool {
	select {
	case err := <-s.done:
		s.done <- err
		return false
	default:
		return true
	}
}

// sleeper starts a process to count as a run, which the test ends.
func (h *heavy) sleeper() *exec.Cmd {
	h.t.Helper()
	cmd := exec.Command("sleep", "600")
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	return cmd
}

func pid(cmd *exec.Cmd) string { return strconv.Itoa(cmd.Process.Pid) }

// runs lists the runs' files, by pid.
func (h *heavy) runs() []string {
	h.t.Helper()
	files, err := filepath.Glob(filepath.Join(h.home, "data", "*", "heavy", "*"))
	if err != nil {
		h.t.Fatal(err)
	}
	var pids []string
	for _, f := range files {
		pids = append(pids, filepath.Base(f))
	}
	slices.Sort(pids)
	return pids
}

func sorted(s ...string) []string {
	slices.Sort(s)
	return s
}

func TestTheScriptsFloorIsRooms(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ ram, free int64 }{
		{18 * GiB, 8 * GiB}, // the Mac that panicked on 2026-11-23: under 10 GiB
		{18 * GiB, 10*GiB + 512<<20},
		{64 * GiB, 20 * GiB}, // over 10 GiB but under half the RAM
		{64 * GiB, 33 * GiB},
	} {
		h := newHeavy(t)
		h.machine(c.ram, 50, c.free)
		out, err := h.run("--room", "live")
		r := Room{RAM: c.ram, DiskFree: c.free, DiskTotal: 1 << 40}
		if r.Starved() != (err != nil) {
			t.Errorf("%d GiB of RAM, %d MiB free: the script's floor is not Room.Floor's %s (refused: %v)\n%s", c.ram/GiB, c.free>>20, Size(r.Floor()), err, out)
		}
		want := "live: only " + Size(c.free) + " of the disk is free, under the " + Size(r.Floor()) + " floor that leaves the swap room to grow, so no test or build starts; clear " + Size(r.Floor()-c.free) + " or more"
		if r.Starved() && !strings.Contains(out, want) {
			t.Errorf("%d GiB of RAM, %d GiB free: it said\n%s\nwant %q", c.ram/GiB, c.free/GiB, out, want)
		}
		if !r.Starved() && out != "" {
			t.Errorf("%d GiB of RAM, %d MiB free, above the floor: it said %q", c.ram/GiB, c.free>>20, out)
		}
		// The free disk where the swap grows: the VM volume on macOS.
		volume, _ := os.ReadFile(filepath.Join(h.bin, "df.volume"))
		want = "/"
		if runtime.GOOS == "darwin" {
			want = "/System/Volumes/VM"
		}
		if got := strings.TrimSpace(string(volume)); got != want {
			t.Errorf("it read the free disk of %q, want %q", got, want)
		}
	}
}

func TestUnderTheFloorARunWaitsForRoomThenStops(t *testing.T) {
	t.Parallel()
	low := int64(2 * GiB)
	r := Room{RAM: 18 * GiB, DiskFree: low, DiskTotal: 1 << 40}

	// It waits no longer than IRIE_HEAVY_WAIT, then stops with the
	// reason, and counts as no run.
	h := newHeavy(t, "IRIE_HEAVY_WAIT=0")
	h.machine(18*GiB, 50, low)
	out, err := h.run(pid(h.sleeper()), "test.sh")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("under the floor: %v, want exit status 1\n%s", err, out)
	}
	if want := "test.sh: only " + Size(low) + " of the disk is free, under the " + Size(r.Floor()) + " floor"; !strings.Contains(out, want) || !strings.Contains(out, "go clean -cache") {
		t.Errorf("it said\n%s\nwant %q and what to clear", out, want)
	}
	if runs := h.runs(); len(runs) != 0 {
		t.Errorf("a run that stopped left its file: %v", runs)
	}

	// A CI runner says so and goes on.
	h = newHeavy(t, "IRIE_HEAVY_WAIT=0", "CI=true")
	h.machine(18*GiB, 50, low)
	if out, err := h.run(pid(h.sleeper()), "check"); err != nil || !strings.Contains(out, "going on, as a CI runner") {
		t.Errorf("on a CI runner: %v\n%s", err, out)
	}

	// A run that waits says so in a line, and goes once there is room.
	h = newHeavy(t)
	h.machine(18*GiB, 50, low)
	s := h.start("sh", "tools/heavy.sh", pid(h.sleeper()), "check")
	s.await(t, "check: only "+Size(low)+" of the disk is free, under the "+Size(r.Floor())+" floor that leaves the swap room to grow; waiting up to 10m for room")
	h.machine(18*GiB, 50, 30*GiB)
	s.ends(t)
}

func TestRunsTakeTurnsByTheFreeMemory(t *testing.T) {
	t.Parallel()
	h := newHeavy(t)
	// 16 GiB of RAM, half of it free: room for two runs, at 4 GB a run.
	h.machine(16*GiB, 50, 100*GiB)
	a, b, c := h.sleeper(), h.sleeper(), h.sleeper()
	for _, p := range []*exec.Cmd{a, b} {
		if out, err := h.run(pid(p), "test.sh"); err != nil || out != "" {
			t.Fatalf("a run with room to spare: %v %q", err, out)
		}
	}
	// The third waits, and says why in a line.
	s := h.start("sh", "tools/heavy.sh", pid(c), "test.sh")
	line := s.await(t, "test.sh: waiting for a turn: 2 test or build runs are going (pids ")
	if !strings.Contains(line, pid(a)) || !strings.Contains(line, pid(b)) || !strings.HasSuffix(line, "and "+Size(16*GiB/100*50)+" of free memory makes room for 2, at 4 GB a run") {
		t.Errorf("it said %q", line)
	}
	// It goes when one of them ends, and the file of the run that ended
	// goes.
	a.Process.Kill()
	a.Wait()
	s.ends(t)
	if got, want := h.runs(), sorted(pid(b), pid(c)); !slices.Equal(got, want) {
		t.Errorf("runs %v, want %v", got, want)
	}

	// A stopped run, as in a frozen session, counts as none, since the
	// free memory leaves out what it holds.
	if err := syscall.Kill(b.Process.Pid, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(b.Process.Pid, syscall.SIGCONT)
	if out, err := h.run(pid(h.sleeper()), "test.sh"); err != nil || out != "" {
		t.Errorf("a run beside one going and one stopped: %v %q", err, out)
	}

	// With little memory free one run goes, and only one.
	h = newHeavy(t)
	h.machine(16*GiB, 5, 100*GiB)
	if out, err := h.run(pid(h.sleeper()), "test.sh"); err != nil || out != "" {
		t.Errorf("the one run with too little memory free: %v %q", err, out)
	}
	s = h.start("sh", "tools/heavy.sh", pid(h.sleeper()), "check")
	s.await(t, "check: waiting for a turn: 1 test or build run is going (pid ")
}

func TestARunInsideAnotherOrGoneTakesNoTurn(t *testing.T) {
	t.Parallel()
	// A run whose process ended, and one whose pid another process took,
	// count as none, and their files go.
	h := newHeavy(t, "IRIE_HEAVY_SLOTS=1")
	h.machine(16*GiB, 50, 100*GiB)
	ended := h.sleeper()
	if out, err := h.run(pid(ended), "test.sh"); err != nil || out != "" {
		t.Fatalf("the first run: %v %q", err, out)
	}
	ended.Process.Kill()
	ended.Wait()
	dirs, _ := filepath.Glob(filepath.Join(h.home, "data", "*", "heavy"))
	if len(dirs) != 1 {
		t.Fatalf("the runs' folders: %v", dirs)
	}
	if err := os.WriteFile(filepath.Join(dirs[0], strconv.Itoa(os.Getpid())), []byte("Thu Jan 1 00:00:00 1970\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	x := h.sleeper()
	if out, err := h.run(pid(x), "test.sh"); err != nil || out != "" {
		t.Errorf("beside runs that are gone: %v %q", err, out)
	}
	if got, want := h.runs(), []string{pid(x)}; !slices.Equal(got, want) {
		t.Errorf("runs %v, want %v", got, want)
	}

	// A run inside another, as check.sh's tests are, takes no turn of its
	// own, though the only one is its forebear's.
	h = newHeavy(t, "IRIE_HEAVY_SLOTS=1")
	h.machine(16*GiB, 50, 100*GiB)
	s := h.start("sh", "-c", `sh tools/heavy.sh $$ check || exit 9; sh -c 'sh tools/heavy.sh $$ test.sh' && echo the inner run went`)
	s.await(t, "the inner run went")
	s.ends(t)
}

func TestTheTestAndCheckScriptsTakeATurnFirst(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ script, name string }{{"test.sh", "test.sh"}, {"check.sh", "check"}} {
		t.Run(c.script, func(t *testing.T) {
			t.Parallel()
			// No cargo, and a go and gofmt that only say they ran, should the
			// script not wait.
			h := newHeavy(t, "IRIE_HEAVY_SLOTS=1", "CARGO_HOME="+t.TempDir())
			h.machine(16*GiB, 50, 100*GiB)
			for _, tool := range []string{"go", "gofmt"} {
				h.write(tool, "#!/bin/sh\necho "+tool+" \"$@\" >>\"$(dirname \"$0\")/ran\"\n")
			}
			holder := h.sleeper()
			if out, err := h.run(pid(holder), "live"); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			s := h.start("sh", "tools/"+c.script, "./internal/sysload")
			s.await(t, c.name+": waiting for a turn: 1 test or build run is going (pid "+pid(holder)+")")
			if ran, err := os.ReadFile(filepath.Join(h.bin, "ran")); err == nil {
				t.Errorf("%s ran these before its turn:\n%s", c.script, ran)
			}
			if !s.running() {
				t.Errorf("%s ended rather than wait", c.script)
			}
		})
	}
}

func TestAnInstallAsksForRoomBeforeItTestsOrBuilds(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "tools", "live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(src), "\n")
	// at is the first line, not a comment, that matches re.
	at := func(re string) int {
		m := regexp.MustCompile(re)
		for i, l := range lines {
			if !strings.HasPrefix(strings.TrimSpace(l), "#") && m.MatchString(l) {
				return i
			}
		}
		t.Fatalf("tools/live.sh has no line like %s", re)
		return -1
	}
	room := at(`^\s*sh tools/heavy\.sh --room live$`)
	turn := at(`^\s*sh tools/heavy\.sh \$\$ live$`)
	done := at(`^\s*sh tools/heavy\.sh --done \$\$$`)
	for _, c := range []struct{ what, re string }{
		{"takes a place to test", `^\s*take_slot$`},
		{"builds the app", `mac/build\.sh`},
		{"runs the checks", `^\s*sh tools/check\.sh`},
		{"builds irie", `^\s*go build `},
	} {
		n := at(c.re)
		if n < room {
			t.Errorf("tools/live.sh %s (line %d) before it asks for room on the disk (line %d)", c.what, n+1, room+1)
		}
		if c.what != "takes a place to test" && n < turn {
			t.Errorf("tools/live.sh %s (line %d) before its turn among the machine's runs (line %d)", c.what, n+1, turn+1)
		}
	}
	if lock := at(`^\s*take_lock "\$\{IRIE_LIVE_WAIT`); done > lock {
		t.Errorf("tools/live.sh holds its turn (line %d) while it waits for the lock (line %d)", done+1, lock+1)
	}
}
