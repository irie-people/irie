// Package ab runs two builds of a program, gold and new, on the same
// arguments and input, and compares them: whether new's output is the same
// as gold's, and how its time, CPU and peak memory compare (DESIGN §4.24).
// "Faster" and "nothing changed" are claims an agent makes after it
// rewrites code; this is how one is checked rather than believed.
//
// The builds take turns, gold then new, then new then gold, so neither
// always runs on a cooler machine. Each run's standard output goes straight
// to a file, so that reading it never slows the build being timed, and is
// read once the run is over.
package ab

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/sysload"
)

// Options says what to run and how often.
type Options struct {
	Gold, New string   // the two builds: paths, or names on PATH
	Args      []string // arguments both builds get
	Stdin     string   // a file both builds read as standard input; empty is none
	Runs      int      // timed runs of each build
	Warmup    int      // runs of each build before the timed ones, compared but not timed
	Ignore    []*regexp.Regexp
	Timeout   time.Duration // for one run; 0 is none
}

// Report is what a comparison found.
type Report struct {
	Gold   string   `json:"gold"`
	New    string   `json:"new"`
	Args   []string `json:"args,omitempty"`
	Runs   int      `json:"runs"`
	Warmup int      `json:"warmup"`
	// Same is whether every run of new gave gold's output and exit status,
	// after Ignore.
	Same bool `json:"same"`
	// Varies names the build whose own runs disagree, "gold" or "new", and
	// VariesAt the first run (counting warm-ups, from 1) that differed from
	// its first. When gold varies, a difference from new means nothing:
	// what changes from run to run needs an Ignore.
	Varies   string `json:"varies,omitempty"`
	VariesAt int    `json:"varies_at,omitempty"`
	// Diff is the first line where new's output parts from gold's.
	Diff    *Diff  `json:"diff,omitempty"`
	GoldOut Output `json:"gold_output"`
	NewOut  Output `json:"new_output"`
	Time    Metric `json:"time"`
	CPU     Metric `json:"cpu"`
	Memory  Metric `json:"memory"`
	// What makes the times vary more than the builds do: a run in macOS's
	// background state (HeldBack), as when Gabriel holds its session back,
	// or more work waiting to run than CPUs as the runs ended (RunQueue,
	// the last minute's average, set only when it is over CPUs).
	HeldBack bool    `json:"held_back,omitempty"`
	RunQueue float64 `json:"run_queue,omitempty"`
	CPUs     int     `json:"cpus,omitempty"`
}

// Output is one build's output on its first run.
type Output struct {
	Bytes int64 `json:"bytes"`
	Lines int64 `json:"lines"`
	Exit  int   `json:"exit"`
}

// Diff is where two outputs part: a line number from 1, and each build's
// line there, or the exit statuses when the lines agree.
type Diff struct {
	Line int64  `json:"line"`
	Gold string `json:"gold"`
	New  string `json:"new"`
}

// Metric is one measure of both builds' timed runs: seconds, or bytes of
// peak memory. Percent is how far new's median is from gold's, as a
// percentage of gold's; Change says whether that is more than noise.
type Metric struct {
	Unit    string  `json:"unit"`
	Gold    Stats   `json:"gold"`
	New     Stats   `json:"new"`
	Percent float64 `json:"percent"`
	Change  Change  `json:"change"`
}

// Stats sums up a measure's samples.
type Stats struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

// ErrRun is a build that could not run, or ran out of time.
var ErrRun = errors.New("a build could not run")

// sample is one run's measures and its output's digest.
type sample struct {
	wall, cpu float64 // seconds
	rss       float64 // bytes; 0 where the system does not say
	exit      int
	sum       [sha256.Size]byte
	out       Output
}

// side is one build and what its runs gave.
type side struct {
	name, path string
	first      *sample
	firstOut   string // the first run's output file, kept for the diff
	timed      []sample
	variesAt   int
}

// Run runs both builds Warmup+Runs times each, taking turns, and compares
// them.
func Run(ctx context.Context, o Options) (Report, error) {
	if o.Runs < 1 {
		return Report{}, errors.New("want at least one run")
	}
	dir, err := os.MkdirTemp("", brand.Slug+"-ab-")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(dir)
	gold, nw := &side{name: "gold"}, &side{name: "new"}
	for _, s := range []*side{gold, nw} {
		bin := o.Gold
		if s == nw {
			bin = o.New
		}
		if s.path, err = exec.LookPath(bin); err != nil {
			return Report{}, fmt.Errorf("%w: %s: %v", ErrRun, s.name, err)
		}
	}
	scratch := filepath.Join(dir, "run.out")
	held := proc.InBackground()
	for i := 0; i < o.Warmup+o.Runs; i++ {
		order := []*side{gold, nw}
		if i%2 == 1 {
			order = []*side{nw, gold}
		}
		for _, s := range order {
			out := scratch
			if s.first == nil {
				out = filepath.Join(dir, s.name+".out")
			}
			r, err := runOnce(ctx, s.path, o, out, filepath.Join(dir, "run.err"))
			if err != nil {
				return Report{}, fmt.Errorf("%w: %s, run %d: %v", ErrRun, s.name, i+1, err)
			}
			if r.sum, r.out, err = digest(out, o.Ignore); err != nil {
				return Report{}, err
			}
			r.out.Exit = r.exit
			if s.first == nil {
				first := r
				s.first, s.firstOut = &first, out
			} else if s.variesAt == 0 && (r.sum != s.first.sum || r.exit != s.first.exit) {
				s.variesAt = i + 1
			}
			if i >= o.Warmup {
				s.timed = append(s.timed, r)
			}
		}
	}
	rep := Report{
		Gold: o.Gold, New: o.New, Args: o.Args, Runs: o.Runs, Warmup: o.Warmup,
		GoldOut: gold.first.out, NewOut: nw.first.out,
		HeldBack: held || proc.InBackground(),
	}
	if l, err := sysload.Read(); err == nil && l.RunQueue > float64(l.CPUs) {
		rep.RunQueue, rep.CPUs = l.RunQueue, l.CPUs
	}
	switch {
	case gold.variesAt > 0:
		rep.Varies, rep.VariesAt = "gold", gold.variesAt
	case nw.variesAt > 0:
		rep.Varies, rep.VariesAt = "new", nw.variesAt
	case gold.first.sum != nw.first.sum || gold.first.exit != nw.first.exit:
		d, err := firstDiff(gold.firstOut, nw.firstOut, o.Ignore)
		if err != nil {
			return Report{}, err
		}
		if d == nil {
			d = &Diff{Gold: fmt.Sprintf("exit %d", gold.first.exit), New: fmt.Sprintf("exit %d", nw.first.exit)}
		}
		rep.Diff = d
	default:
		rep.Same = true
	}
	rep.Time = metric("s", gold.timed, nw.timed, func(s sample) float64 { return s.wall })
	rep.CPU = metric("s", gold.timed, nw.timed, func(s sample) float64 { return s.cpu })
	rep.Memory = metric("bytes", gold.timed, nw.timed, func(s sample) float64 { return s.rss })
	return rep, nil
}

// runOnce runs one build with its output in out, and measures it.
func runOnce(ctx context.Context, path string, o Options, out, errOut string) (sample, error) {
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}
	stdout, err := os.Create(out)
	if err != nil {
		return sample{}, err
	}
	defer stdout.Close()
	stderr, err := os.Create(errOut)
	if err != nil {
		return sample{}, err
	}
	defer stderr.Close()
	cmd := exec.CommandContext(ctx, path, o.Args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if o.Stdin != "" {
		in, err := os.Open(o.Stdin)
		if err != nil {
			return sample{}, err
		}
		defer in.Close()
		cmd.Stdin = in
	}
	start := time.Now()
	err = cmd.Run()
	wall := time.Since(start)
	var exit *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return sample{}, fmt.Errorf("no answer within %s", o.Timeout)
	case ctx.Err() != nil:
		return sample{}, ctx.Err()
	case err != nil && !errors.As(err, &exit):
		return sample{}, err
	}
	ps := cmd.ProcessState
	return sample{
		wall: wall.Seconds(),
		cpu:  (ps.UserTime() + ps.SystemTime()).Seconds(),
		rss:  float64(maxRSS(ps)),
		exit: ps.ExitCode(),
	}, nil
}

// normalize is one line, as read with its newline, the way it is compared:
// a Windows line ending is a plain one, and what an Ignore matches is
// blanked out. A last line without a newline stays without one.
func normalize(line string, ignore []*regexp.Regexp) string {
	body, nl := strings.CutSuffix(line, "\n")
	body = strings.TrimSuffix(body, "\r")
	for _, re := range ignore {
		body = re.ReplaceAllLiteralString(body, "\x00")
	}
	if nl {
		return body + "\n"
	}
	return body
}

// digest reads an output file once: its size and lines, and a hash of its
// lines as they are compared.
func digest(path string, ignore []*regexp.Regexp) ([sha256.Size]byte, Output, error) {
	var sum [sha256.Size]byte
	f, err := os.Open(path)
	if err != nil {
		return sum, Output{}, err
	}
	defer f.Close()
	h := sha256.New()
	var o Output
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			o.Bytes += int64(len(line))
			o.Lines++
			io.WriteString(h, normalize(line, ignore))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return sum, Output{}, err
		}
	}
	copy(sum[:], h.Sum(nil))
	return sum, o, nil
}

// firstDiff is the first line where two output files part, as compared, or
// nil when they agree.
func firstDiff(a, b string, ignore []*regexp.Regexp) (*Diff, error) {
	fa, err := os.Open(a)
	if err != nil {
		return nil, err
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return nil, err
	}
	defer fb.Close()
	ra, rb := bufio.NewReaderSize(fa, 1<<16), bufio.NewReaderSize(fb, 1<<16)
	for n := int64(1); ; n++ {
		la, ea := ra.ReadString('\n')
		lb, eb := rb.ReadString('\n')
		if la == "" && lb == "" {
			return nil, nil
		}
		if normalize(la, ignore) != normalize(lb, ignore) {
			d := &Diff{Line: n, Gold: shown(la), New: shown(lb)}
			if d.Gold == d.New {
				d.Gold, d.New = unended(la, d.Gold), unended(lb, d.New)
			}
			if d.Gold == d.New {
				d.New += " (parts from gold's after the first 160 characters)"
			}
			return d, nil
		}
		if ea != nil && ea != io.EOF {
			return nil, ea
		}
		if eb != nil && eb != io.EOF {
			return nil, eb
		}
	}
}

// shown is a line as a report shows it: one line, cut at 160 characters.
func shown(line string) string {
	if line == "" {
		return "(end of output)"
	}
	line = strings.TrimRight(line, "\r\n")
	if utf8.RuneCountInString(line) > 160 {
		r := []rune(line)
		line = string(r[:160]) + "…"
	}
	return line
}

// unended marks a shown line that lacks the newline the other build's has,
// the one difference a shown line cannot show.
func unended(line, s string) string {
	if line != "" && !strings.HasSuffix(line, "\n") {
		return s + " (no newline at the end)"
	}
	return s
}

// metric sums up one measure of both builds' timed runs.
func metric(unit string, gold, nw []sample, of func(sample) float64) Metric {
	g, n := values(gold, of), values(nw, of)
	m := Metric{Unit: unit, Gold: summarize(g), New: summarize(n), Change: Same}
	if m.Gold.Median > 0 && m.New.Median > 0 {
		m.Percent = (m.New.Median - m.Gold.Median) / m.Gold.Median * 100
		m.Change = Compare(g, n)
	}
	return m
}

func values(s []sample, of func(sample) float64) []float64 {
	v := make([]float64, len(s))
	for i := range s {
		v[i] = of(s[i])
	}
	return v
}

// summarize is the median, least and most of some samples.
func summarize(v []float64) Stats {
	if len(v) == 0 {
		return Stats{}
	}
	s := slices.Clone(v)
	slices.Sort(s)
	med := s[len(s)/2]
	if len(s)%2 == 0 {
		med = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return Stats{Median: med, Min: s[0], Max: s[len(s)-1]}
}
