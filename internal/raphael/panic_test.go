package raphael

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// panicReport writes a panic report as macOS does, a line of JSON about
// it and then the report, made up for the test: at is its time, swap the
// end of its compressor's line.
func panicReport(t *testing.T, dir, name string, at time.Time, why, swap string) string {
	t.Helper()
	head := fmt.Sprintf(`{"bug_type":"210","timestamp":%q,"os_version":"macOS 14.1","incident_id":"00000000-0000-0000-0000-000000000001"}`, at.Format(reportTimeLayout))
	procs := map[string]any{
		"1":   map[string]any{"pid": 1, "procname": "launchd", "residentMemoryBytes": 20 << 20},
		"300": map[string]any{"pid": 300, "procname": "2.1.400", "residentMemoryBytes": 6 << 30},
		"301": map[string]any{"pid": 301, "procname": "2.1.400", "residentMemoryBytes": 5 << 30},
		"302": map[string]any{"pid": 302, "procname": "claude", "residentMemoryBytes": 1 << 30},
		"400": map[string]any{"pid": 400, "procname": "llama-server", "residentMemoryBytes": 7 << 30},
		"500": map[string]any{"pid": 500, "procname": "irie", "residentMemoryBytes": 1 << 30},
		"501": map[string]any{"pid": 501, "procname": "irie", "residentMemoryBytes": 1 << 30},
		"600": map[string]any{"pid": 600, "procname": "Finder", "residentMemoryBytes": 100 << 20},
	}
	body, _ := json.Marshal(map[string]any{
		"panicString": "panic(cpu 3 caller 0xfffffe0012345678): " + why + "\nDebugger message: panic\nKernel version: Darwin Kernel Version 23.1.0\n" +
			"Compressor Info: 20% of compressed pages limit (OK) and 50% of segments limit (OK) with 7 swapfiles and " + swap + "\n" +
			"Panicked task 0xfffffe0000000000: 0 pages, 400 threads: pid 0: kernel_task\n",
		"processByPid": procs,
		"notes":        []string{"made up for a test, of alice's Mac at /home/alice"},
	})
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(head+"\n"+string(body)), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, at, at)
	return p
}

// resetReport writes a report that the Mac reset, as macOS does.
func resetReport(t *testing.T, dir, name string, at time.Time) string {
	t.Helper()
	head := fmt.Sprintf(`{"bug_type":"115","timestamp":%q,"os_version":"macOS 14.1","name":"ResetCounter"}`, at.Format(reportTimeLayout))
	p := filepath.Join(dir, name)
	os.WriteFile(p, []byte(head+"\nReset count: 1\nBoot failure count: 0\n"), 0o640)
	os.Chtimes(p, at, at)
	return p
}

func TestReadPanicSaysWhatHappenedInPlainWords(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.FixedZone("", -5*3600))
	p := panicReport(t, dir, "panic-full-2026-11-20-093000.0002.panic", at, "watchdog timeout: no checkins from watchdogd in 120 seconds (900 total checkins since monitoring last enabled)", "LOW swap space")
	got, err := ReadPanic(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.At.Equal(at) || !got.LowSwap || got.Swapfiles != 7 || !strings.HasPrefix(got.Reason, "watchdog timeout") {
		t.Errorf("read %+v", got)
	}
	if got.Words != "macOS stopped answering for 120 seconds, so its watchdog restarted it" {
		t.Errorf("words %q", got.Words)
	}
	// Claude Code's agents together, by the version its build is named by.
	if len(got.Top) != 3 || got.Top[0].Name != "Claude Code" || got.Top[0].Count != 3 || got.Top[0].Bytes != 12<<30 || got.Top[1].Name != "llama-server" || got.Top[2].Name != "irie" || got.Top[2].Count != 2 {
		t.Errorf("top %+v", got.Top)
	}
	text, lines := got.Tell()
	if !strings.Contains(text, "crashed and restarted") || len(lines) != 2 || !strings.Contains(lines[0], "swap had run out of disk") || lines[1] != "Most memory: Claude Code (3) 12 GB, llama-server 7 GB, irie (2) 2 GB." {
		t.Errorf("tell %q %q", text, lines)
	}
}

func TestAnotherPanicIsSaidAsItIs(t *testing.T) {
	dir := t.TempDir()
	p := panicReport(t, dir, "panic-full-x.panic", time.Now(), "a made-up fault at 0x0 (saved state: 0x1)", "OK swap space")
	got, err := ReadPanic(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.LowSwap || got.Words != "its kernel stopped: a made-up fault at 0x0" {
		t.Errorf("read %+v", got)
	}
	if _, lines := got.Tell(); len(lines) != 1 {
		t.Errorf("a swap line without low swap: %q", lines)
	}
}

func TestNewestRestartTakesTheNewestReport(t *testing.T) {
	dir := t.TempDir()
	if _, ok, _ := NewestRestart(dir); ok {
		t.Error("a restart with no report")
	}
	now := time.Now().Truncate(time.Second)
	panicReport(t, dir, "panic-full-old.panic", now.Add(-72*time.Hour), "an old fault", "OK swap space")
	newer := panicReport(t, dir, "panic-full-new.panic", now.Add(-time.Hour), "watchdog timeout: no checkins from watchdogd in 99 seconds", "LOW swap space")
	// The reset written with the panic is the panic's.
	resetReport(t, dir, "ResetCounter-new.diag", now.Add(-time.Hour+4*time.Second))
	got, ok, err := NewestRestart(dir)
	if err != nil || !ok || got.File != newer || !got.LowSwap {
		t.Fatalf("newest %+v %v %v", got, ok, err)
	}
	// A reset well after it, with no panic, is a restart of its own.
	reset := resetReport(t, dir, "ResetCounter-later.diag", now.Add(-10*time.Minute))
	got, ok, _ = NewestRestart(dir)
	if !ok || got.File != reset || got.Reason != "" || !strings.Contains(got.Words, "without a kernel panic report") {
		t.Errorf("a reset alone: %+v", got)
	}
	if text, _ := got.Tell(); !strings.HasPrefix(text, "Your Mac restarted at") {
		t.Errorf("tell %q", text)
	}
}

func TestPanicFindingNeedsCareForADayAndGoesAfterThree(t *testing.T) {
	p := Panic{At: time.Now().Add(-2 * time.Hour), File: "/x/panic-full-a.panic", Reason: "r", Words: "w", LowSwap: true}
	f, ok := PanicFinding(p, time.Now())
	if !ok || f.Level != Warn || f.Area != AreaPanic || f.ID != "panic:panic-full-a.panic" || !strings.Contains(f.Advice, "keeps room free") {
		t.Errorf("a fresh one: %+v", f)
	}
	if f, _ := PanicFinding(p, time.Now().Add(30*time.Hour)); f.Level != Note {
		t.Errorf("after a day: %s", f.Level)
	}
	if _, ok := PanicFinding(p, time.Now().Add(80*time.Hour)); ok {
		t.Error("after three days it is still listed")
	}
}
