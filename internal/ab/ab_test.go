package ab

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/proc"
)

// build writes a shell script that stands in for a build of a program.
func build(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func ab(t *testing.T, o Options) Report {
	t.Helper()
	if o.Runs == 0 {
		o.Runs = 2
	}
	r, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTheSameOutputIsTheSame(t *testing.T) {
	gold := build(t, "gold", `printf 'a\nb\n'`)
	nw := build(t, "new", `printf 'a\nb\n'`)
	r := ab(t, Options{Gold: gold, New: nw, Warmup: 1})
	if !r.Same || r.Diff != nil || r.Varies != "" {
		t.Fatalf("got %+v, want the same", r)
	}
	if r.GoldOut != (Output{Bytes: 4, Lines: 2}) || r.NewOut != r.GoldOut {
		t.Errorf("outputs %+v and %+v, want 4 bytes in 2 lines, exit 0", r.GoldOut, r.NewOut)
	}
}

func TestIgnoreBlanksWhatMayDiffer(t *testing.T) {
	gold := build(t, "gold", `echo "tool v1 (June)"; echo x`)
	nw := build(t, "new", `echo "tool v2 (November)"; echo x`)
	r := ab(t, Options{Gold: gold, New: nw})
	if r.Same || r.Diff == nil || *r.Diff != (Diff{Line: 1, Gold: "tool v1 (June)", New: "tool v2 (November)"}) {
		t.Fatalf("without --ignore: got same=%v diff=%+v, want line 1 named", r.Same, r.Diff)
	}
	r = ab(t, Options{Gold: gold, New: nw, Ignore: []*regexp.Regexp{regexp.MustCompile(`v\d+ \(\w+\)`)}})
	if !r.Same {
		t.Errorf("with the version ignored: got diff %+v, want the same", r.Diff)
	}
}

func TestTheFirstDifferenceIsNamed(t *testing.T) {
	gold := build(t, "gold", `printf 'a\nb\nc\nd\n'`)
	cases := []struct {
		body string
		want Diff
	}{
		{`printf 'a\nb\nC\nd\n'`, Diff{Line: 3, Gold: "c", New: "C"}},
		{`printf 'a\nb\nc\n'`, Diff{Line: 4, Gold: "d", New: "(end of output)"}},
		{`printf 'a\nb\nc\nd'`, Diff{Line: 4, Gold: "d", New: "d (no newline at the end)"}},
	}
	for _, c := range cases {
		r := ab(t, Options{Gold: gold, New: build(t, "new", c.body)})
		if r.Diff == nil || *r.Diff != c.want {
			t.Errorf("%s: got %+v, want %+v", c.body, r.Diff, c.want)
		}
	}
}

func TestAnExitStatusIsPartOfTheOutput(t *testing.T) {
	gold := build(t, "gold", `echo ok`)
	nw := build(t, "new", `echo ok; exit 3`)
	r := ab(t, Options{Gold: gold, New: nw})
	if r.Same || r.Diff == nil || *r.Diff != (Diff{Gold: "exit 0", New: "exit 3"}) {
		t.Fatalf("got same=%v diff=%+v, want the exits named", r.Same, r.Diff)
	}
}

func TestAWindowsLineEndingIsAPlainOne(t *testing.T) {
	gold := build(t, "gold", `printf 'a\r\nb\r\n'`)
	nw := build(t, "new", `printf 'a\nb\n'`)
	if r := ab(t, Options{Gold: gold, New: nw}); !r.Same {
		t.Errorf("got diff %+v, want the same", r.Diff)
	}
}

func TestAGoldThatVariesIsCalledOut(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	gold := build(t, "gold", `n=$(cat "`+count+`" 2>/dev/null || echo 0); n=$((n + 1)); echo $n >"`+count+`"; echo $n`)
	nw := build(t, "new", `echo 1`)
	r := ab(t, Options{Gold: gold, New: nw, Runs: 3})
	if r.Same || r.Varies != "gold" || r.VariesAt != 2 {
		t.Errorf("got same=%v varies=%q at %d, want gold from run 2", r.Same, r.Varies, r.VariesAt)
	}
}

func TestStdinReachesBoth(t *testing.T) {
	in := filepath.Join(t.TempDir(), "in")
	if err := os.WriteFile(in, []byte("hello\nthere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gold := build(t, "gold", `cat`)
	nw := build(t, "new", `cat`)
	r := ab(t, Options{Gold: gold, New: nw, Stdin: in})
	if !r.Same || r.GoldOut.Bytes != 12 || r.GoldOut.Lines != 2 {
		t.Errorf("got same=%v %+v, want both to read the 12 bytes", r.Same, r.GoldOut)
	}
}

func TestTheBuildsTakeTurns(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	gold := build(t, "gold", `printf g >>"`+log+`"`)
	nw := build(t, "new", `printf n >>"`+log+`"`)
	ab(t, Options{Gold: gold, New: nw, Runs: 2, Warmup: 1})
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "gnnggn" {
		t.Errorf("ran %q, want gold then new, then new then gold, then gold then new", got)
	}
}

func TestEachRunIsMeasured(t *testing.T) {
	gold := build(t, "gold", `i=0; while [ $i -lt 2000 ]; do i=$((i + 1)); done`)
	nw := build(t, "new", `true`)
	r := ab(t, Options{Gold: gold, New: nw, Runs: 3})
	if r.Time.Gold.Median <= 0 || r.Time.Gold.Min > r.Time.Gold.Median || r.Time.Gold.Max < r.Time.Gold.Median {
		t.Errorf("time %+v, want a median between a least and a most above 0", r.Time.Gold)
	}
	if r.CPU.Gold.Median <= 0 {
		t.Errorf("cpu %+v, want the loop's CPU", r.CPU.Gold)
	}
	if (runtime.GOOS == "darwin" || runtime.GOOS == "linux") && r.Memory.Gold.Median <= 0 {
		t.Errorf("memory %+v, want the peak resident memory", r.Memory.Gold)
	}
	if r.Time.Unit != "s" || r.Memory.Unit != "bytes" {
		t.Errorf("units %q and %q", r.Time.Unit, r.Memory.Unit)
	}
}

// A run in macOS's background state says so: its times measure the
// machine as much as the builds.
func TestARunHeldBackSaysSo(t *testing.T) {
	if !proc.CanBackground || proc.InBackground() {
		t.Skip("no background state here, or the test runs in it already")
	}
	if err := proc.SetBackground(os.Getpid(), true); err != nil {
		t.Fatal(err)
	}
	defer proc.SetBackground(os.Getpid(), false)
	gold := build(t, "gold", `true`)
	if r := ab(t, Options{Gold: gold, New: gold, Runs: 1}); !r.HeldBack {
		t.Error("a run in the background state did not say so")
	}
}

func TestABuildThatCannotRunIsAnError(t *testing.T) {
	gold := build(t, "gold", `true`)
	_, err := Run(context.Background(), Options{Gold: gold, New: filepath.Join(t.TempDir(), "missing"), Runs: 1})
	if !errors.Is(err, ErrRun) {
		t.Errorf("got %v, want ErrRun", err)
	}
	slow := build(t, "slow", `exec sleep 5`)
	start := time.Now()
	_, err = Run(context.Background(), Options{Gold: gold, New: slow, Runs: 1, Timeout: 200 * time.Millisecond})
	if !errors.Is(err, ErrRun) || time.Since(start) > 4*time.Second {
		t.Errorf("got %v after %s, want ErrRun at the timeout", err, time.Since(start))
	}
}
