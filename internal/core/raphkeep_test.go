package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/raphael"
	"github.com/irie-people/irie/internal/sysload"
	"github.com/irie-people/irie/internal/tmux"
)

// lockedLog is a log the core's goroutines may write at once.
type lockedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// sizeOf is what root takes on the disk, files' blocks all told.
func sizeOf(root string) int64 {
	var n int64
	filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// keepCore is a core whose Raffi keeps room on a made-up machine: Go's
// build cache in a folder of the test's, with two entries two days old
// and one of a minute, and a disk short of the buffer by short bytes,
// which gains what goes from the cache. No restart report is there.
func keepCore(t *testing.T, short int64) (c *Core, cache string, log *lockedLog) {
	t.Helper()
	c, _ = raphCore(t)
	log = &lockedLog{}
	c.log = slog.New(slog.NewTextHandler(log, nil))
	cache = filepath.Join(t.TempDir(), "go-build")
	os.MkdirAll(cache, 0o700)
	os.WriteFile(filepath.Join(cache, "README"), []byte("This directory holds cached build artifacts from the Go build system.\n"), 0o600)
	for name, age := range map[string]time.Duration{"00aa-d": 48 * time.Hour, "01bb-a": 47 * time.Hour, "02cc-d": time.Minute} {
		p := filepath.Join(cache, name[:2], name)
		os.MkdirAll(filepath.Dir(p), 0o700)
		b := make([]byte, 64<<10)
		rand.Read(b)
		os.WriteFile(p, b, 0o600)
		at := time.Now().Add(-age)
		os.Chtimes(p, at, at)
	}
	start := sizeOf(cache)
	k := &c.raph.keep
	k.readRoom = func() (sysload.Room, error) {
		return sysload.Room{RAM: 1 << 30, DiskTotal: 1 << 40, DiskFree: sysload.MinBuffer - short + start - sizeOf(cache)}, nil
	}
	k.places = &raphael.Places{GoCache: cache}
	k.panicDir = t.TempDir()
	k.weighOwn = func(context.Context, raphael.Runner, raphael.OwnInput) []raphael.Finding {
		return []raphael.Finding{{ID: "own:trash", Area: raphael.AreaDisk, Level: raphael.Note, Text: "The Trash holds 3.2 GB.", Size: 3_200_000_000,
			Fix: &raphael.Fix{Label: "Empty the Trash", Run: []string{"rm -rf ~/.Trash/*"}, About: "Deletes what is in the Trash for good."}}}
	}
	return c, cache, log
}

// waitKeep waits for the run keepSweep started to end.
func waitKeep(t *testing.T, c *Core) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		c.raph.keep.mu.Lock()
		ran := !c.raph.keep.last.IsZero()
		c.raph.keep.mu.Unlock()
		if ran && !c.raph.keep.running.Load() {
			return
		}
	}
	t.Fatal("the run did not end")
}

// tellsInHeader are the tells the header carries now.
func tellsInHeader(c *Core) []ipc.Tell {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.headerNow().Tells
}

func TestRaffiClearsCachesBelowTheBufferByHimself(t *testing.T) {
	// Short by less than the old entries take: they go, and he tells
	// nothing, the room being back.
	c, cache, log := keepCore(t, 100<<10)
	c.keepSweep()
	waitKeep(t, c)
	if exists(filepath.Join(cache, "00", "00aa-d")) || exists(filepath.Join(cache, "01", "01bb-a")) || !exists(filepath.Join(cache, "02", "02cc-d")) {
		t.Error("the old entries stay, or the new one went")
	}
	if !strings.Contains(log.String(), "cleared by himself, to keep room for swap") || !strings.Contains(log.String(), "Go's build cache") {
		t.Errorf("core.log says no clear:\n%s", log)
	}
	rep := c.raphaelReport()
	if len(rep.Cleared) != 1 || rep.Cleared[0].ID != "go-build" || rep.Cleared[0].Size < 128<<10 || !strings.Contains(rep.Room, "keeps 26.8 GB free for swap") {
		t.Errorf("his report: %+v %q", rep.Cleared, rep.Room)
	}
	if now := c.raphaelNow(); !strings.Contains(now.status, "of caches by himself, to keep room for swap") {
		t.Errorf("his row: %q", now.status)
	}
	if tells := tellsInHeader(c); len(tells) != 0 {
		t.Errorf("told %+v with the room back", tells)
	}
	b, err := os.ReadFile(c.keepFile())
	if err != nil || !strings.Contains(string(b), "go-build") {
		t.Errorf("kept %s, %v", b, err)
	}
}

func TestRaffiTellsOnceWhatIsYoursWhenTheCachesAreNotEnough(t *testing.T) {
	c, _, _ := keepCore(t, 10<<20)
	c.keepSweep()
	waitKeep(t, c)
	tells := tellsInHeader(c)
	if len(tells) != 1 || !strings.HasPrefix(tells[0].ID, "room:") || tells[0].Level != raphael.Warn || tells[0].From != "raphael" ||
		!strings.Contains(tells[0].Text, "short of the room Raffi keeps for swap") || len(tells[0].Lines) != 3 ||
		!strings.Contains(tells[0].Lines[0], "of caches by himself") || tells[0].Lines[1] != "Biggest of yours: the Trash 3.2 GB." {
		t.Fatalf("told %+v", tells)
	}
	// His report lists the person's own, with its fix, which asks first.
	if f, ok := c.raphaelFinding("own:trash"); !ok || f.Fix == nil {
		t.Errorf("no finding for the Trash: %+v", f)
	}
	var found bool
	for _, f := range c.raphaelReport().Findings {
		found = found || f.ID == "own:trash" && f.Fix != nil
	}
	if !found {
		t.Error("the Trash is not in his report")
	}
	if it := c.raphaelNow().items; !strings.Contains(fmt.Sprint(it), "Empty the Trash") {
		t.Errorf("his row has no item for it: %+v", it)
	}
	// Still short, he neither runs again at once nor tells again.
	c.keepSweep()
	time.Sleep(50 * time.Millisecond)
	c.raph.keep.mu.Lock()
	c.raph.keep.last = time.Now().Add(-keepPause)
	c.raph.keep.mu.Unlock()
	c.keepSweep()
	waitKeep(t, c)
	if tells := tellsInHeader(c); len(tells) != 1 {
		t.Errorf("told again: %+v", tells)
	}
	// Under the floor, he tells once more.
	c.raph.keep.readRoom = func() (sysload.Room, error) {
		return sysload.Room{RAM: 1 << 30, DiskTotal: 1 << 40, DiskFree: 5 << 30}, nil
	}
	c.raph.keep.mu.Lock()
	c.raph.keep.last = time.Time{}
	c.raph.keep.mu.Unlock()
	c.keepSweep()
	waitKeep(t, c)
	if tells := tellsInHeader(c); len(tells) != 2 || !strings.Contains(tells[1].Text, "under the 10.7 GB floor, so new heavy work waits") {
		t.Errorf("under the floor: %+v", tells)
	}
}

func TestATellStandsUntilTheOwnerSawIt(t *testing.T) {
	c, _, _ := keepCore(t, 0)
	next := frames(t, c)
	if f, ok := next(5 * time.Second); !ok || f.T != ipc.FrameSnapshot {
		t.Fatalf("first frame %+v", f)
	}
	c.tell(ipc.Tell{ID: "t1", From: "raphael", At: time.Now(), Level: raphael.Warn, Text: "Something happened."})
	got := false
	for end := time.Now().Add(5 * time.Second); !got && time.Now().Before(end); {
		f, ok := next(time.Second)
		got = ok && f.T == ipc.FrameHeader && f.Header != nil && len(f.Header.Tells) == 1 && f.Header.Tells[0].ID == "t1"
	}
	if !got {
		t.Fatal("no client heard the tell")
	}
	c.tell(ipc.Tell{ID: "t1", From: "raphael", At: time.Now(), Text: "Again."})
	if tells := tellsInHeader(c); len(tells) != 1 || tells[0].Text != "Something happened." {
		t.Errorf("the same tell twice: %+v", tells)
	}
	// An agent can't say the person saw it.
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	if _, err := c.raphaelOp(os.Getpid(), ipc.RaphaelArgs{Told: "t1"}); err == nil || !strings.Contains(err.Error(), "An agent can't") {
		t.Errorf("from an agent: %v", err)
	}
	c.snap = tmux.Snapshot{}
	if len(tellsInHeader(c)) != 1 {
		t.Error("an agent's word took the tell away")
	}
	if _, err := c.raphaelOp(0, ipc.RaphaelArgs{Told: "t1"}); err != nil {
		t.Fatal(err)
	}
	if tells := tellsInHeader(c); len(tells) != 0 {
		t.Errorf("still carried once shown: %+v", tells)
	}
	if rep := c.raphaelReport(); len(rep.Told) != 1 || rep.Told[0].ID != "t1" {
		t.Errorf("his report forgot what he told: %+v", rep.Told)
	}
	// One no client showed goes from the header after a day.
	c.tell(ipc.Tell{ID: "t2", From: "raphael", At: time.Now().Add(-tellKeep - time.Minute), Text: "Old news."})
	if tells := tellsInHeader(c); len(tells) != 0 {
		t.Errorf("a day-old tell is carried: %+v", tells)
	}
}

// restartReport writes a panic report as macOS does, made up for a test,
// whose compressor said the swap was low on disk.
func restartReport(t *testing.T, dir string, at time.Time) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"panicString":  "panic(cpu 1 caller 0x1): watchdog timeout: no checkins from watchdogd in 100 seconds\nCompressor Info: 10% of compressed pages limit (OK) and 20% of segments limit (OK) with 6 swapfiles and LOW swap space\n",
		"processByPid": map[string]any{"10": map[string]any{"procname": "2.1.400", "residentMemoryBytes": 9 << 30}, "11": map[string]any{"procname": "llama-server", "residentMemoryBytes": 5 << 30}},
	})
	p := filepath.Join(dir, "panic-full-"+at.Format("2006-01-02-150405")+".0002.panic")
	head := fmt.Sprintf(`{"bug_type":"210","timestamp":%q}`, at.Format("2006-01-02 15:04:05.00 -0700"))
	if err := os.WriteFile(p, []byte(head+"\n"+string(body)), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, at, at)
	return p
}

func TestRaffiTellsOnceOfARestartAfterABoot(t *testing.T) {
	c, _, _ := keepCore(t, 0)
	dir := c.raph.keep.panicDir
	restartReport(t, dir, time.Now().Add(-90*time.Minute))
	c.notePanic(time.Now())
	tells := tellsInHeader(c)
	if len(tells) != 1 || !strings.HasPrefix(tells[0].ID, "panic:") || tells[0].Level != raphael.Warn || !strings.Contains(tells[0].Text, "crashed and restarted") {
		t.Fatalf("told %+v", tells)
	}
	if l := strings.Join(tells[0].Lines, "\n"); !strings.Contains(l, "swap had run out of disk") || !strings.Contains(l, "Claude Code 9 GB, llama-server 5 GB") || !strings.Contains(l, "Raffi now keeps 26.8 GB free for swap") {
		t.Errorf("lines %q", tells[0].Lines)
	}
	var found bool
	for _, f := range c.raphaelReport().Findings {
		found = found || f.Area == raphael.AreaPanic && f.Level == raphael.Warn
	}
	if !found {
		t.Error("his report has no finding of the restart")
	}
	// Not twice: not later, and not from the next core.
	c.notePanic(time.Now().Add(2 * panicEvery))
	c2, _, _ := keepCore(t, 0)
	c2.paths = c.paths
	c2.raph.keep.panicDir = dir
	c2.loadKeep()
	c2.notePanic(time.Now())
	if n := len(c.raph.keep.tells) + len(c2.raph.keep.tells); n != 2 || len(tellsInHeader(c2)) != 1 {
		t.Errorf("told %d times in two cores", n-1)
	}
	// A restart older than three days is not news.
	c3, _, _ := keepCore(t, 0)
	restartReport(t, c3.raph.keep.panicDir, time.Now().Add(-80*time.Hour))
	c3.notePanic(time.Now())
	if tells := tellsInHeader(c3); len(tells) != 0 {
		t.Errorf("told of an old restart: %+v", tells)
	}
}
