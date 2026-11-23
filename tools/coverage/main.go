// Command coverage holds each package's test coverage to its floor in
// floor.txt beside it, a floor that only rises (DESIGN §2.12).
// tools/check.sh runs it, through tools/coverage.sh, on what the test
// runner measured (tools/test.sh --coverout): for each package, its
// statements, how many its tests ran, and how many of its tests skipped.
//
// A package under its floor fails the check. A package whose coverage
// rose, and a new one, gets a floor of its coverage less a point and a
// half (margin), in whole percent; the check writes it, for the change to
// commit, unless the run failed. The margin is for coverage that moves
// between runs of the same code, as a select takes one channel or the
// other. A floor is never lowered to let a change through: the change
// brings tests. Two branches that each raised a floor merge as a union
// (.gitattributes), and the higher floor holds.
//
// Coverage differs from machine to machine: a test may skip where a
// program or a permission it needs is missing. A floor keeps how many of
// the package's tests skipped when it was set, and a run that skipped
// more isn't held to it. The floors are for the system they were measured
// on (the file's `on` line); elsewhere they are shown, not held.
//
// The idea is Blitz's tools/coverage/floor.txt (PROVENANCE.md). It uses
// the standard library alone, so tools/gotool.sh builds it once for each
// version.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// floorFile is the floors' file, from the top of the repository.
const floorFile = "tools/coverage/floor.txt"

// A floor is the least coverage a package must keep, in whole percent,
// and how many of its tests skipped when it was set.
type floor struct {
	pct     int
	skipped int
}

// floors is the floor file: its words before the floors, the system they
// hold on, and each package's floor by its folder in the repository.
type floors struct {
	head string
	on   string
	pkgs map[string]floor
}

// measure is one package's coverage from the runner: nil when the run
// couldn't tell it.
type measure struct {
	pkg     string
	result  *result
	skipped int
}

type result struct {
	statements, covered int
}

func (r result) pct() float64 {
	return 100 * float64(r.covered) / float64(r.statements)
}

// shown is a share as the check says it: cut, not rounded, to a tenth, so
// that 35.96% under a floor of 36% reads 35.9%, not 36.0%.
func shown(pct float64) string {
	// The epsilon keeps a share that is exactly a tenth, such as 51.5,
	// from falling to the tenth below it on a rounding error.
	return strconv.FormatFloat(math.Floor(pct*10+1e-9)/10, 'f', 1, 64) + "%"
}

func main() {
	if len(os.Args) != 2 || strings.HasPrefix(os.Args[1], "-") {
		fmt.Fprintln(os.Stderr, "usage: tools/coverage.sh FILE (what tools/test.sh --coverout=FILE wrote)")
		os.Exit(2)
	}
	module, err := modulePath("go.mod")
	if err != nil {
		fail(err)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fail(err)
	}
	ms, err := readMeasures(f, module)
	f.Close()
	if err != nil {
		fail(fmt.Errorf("%s: %w", os.Args[1], err))
	}
	fl, err := readFloors(floorFile)
	if err != nil {
		fail(err)
	}
	r := check(fl, ms, runtime.GOOS, hasTests)
	for _, n := range r.notes {
		fmt.Println("coverage: " + n)
	}
	if r.changed {
		if err := writeFloors(floorFile, fl); err != nil {
			fail(err)
		}
	}
	for _, p := range r.problems {
		fmt.Fprintln(os.Stderr, "coverage: "+p)
	}
	if len(r.problems) > 0 {
		fmt.Fprintln(os.Stderr, "coverage: test what the change adds; never lower a floor to let a change through ("+floorFile+")")
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "coverage:", err)
	os.Exit(2)
}

// modulePath is the module's path, from its go.mod.
func modulePath(gomod string) (string, error) {
	b, err := os.ReadFile(gomod)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if m, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(m), `"`), nil
		}
	}
	return "", errors.New(gomod + ": no module line")
}

// readMeasures reads the runner's lines, `import statements covered
// skipped` or `import -`, each package by its folder in the module.
func readMeasures(r io.Reader, module string) ([]measure, error) {
	var out []measure
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		m := measure{pkg: folder(f[0], module)}
		switch {
		case len(f) == 2 && f[1] == "-":
		case len(f) == 4:
			var n [3]int
			for i := range n {
				v, err := strconv.Atoi(f[i+1])
				if err != nil || v < 0 {
					return nil, fmt.Errorf("%q isn't a package's coverage", line)
				}
				n[i] = v
			}
			if n[1] > n[0] {
				return nil, fmt.Errorf("%q covers more statements than it has", line)
			}
			m.result = &result{statements: n[0], covered: n[1]}
			m.skipped = n[2]
		default:
			return nil, fmt.Errorf("%q isn't a package's coverage", line)
		}
		out = append(out, m)
	}
	return out, s.Err()
}

// folder is a package's folder in the module: "." for the module's own.
func folder(importPath, module string) string {
	if importPath == module {
		return "."
	}
	return strings.TrimPrefix(importPath, module+"/")
}

// readFloors reads the floor file; a file not there yet has no floors.
func readFloors(path string) (*floors, error) {
	fl := &floors{pkgs: map[string]floor{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fl, nil
	}
	if err != nil {
		return nil, err
	}
	var head strings.Builder
	inHead := true
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			if inHead {
				head.WriteString(line + "\n")
			}
			continue
		}
		inHead = false
		f := strings.Fields(t)
		if f[0] == "on" && len(f) == 2 {
			fl.on = f[1]
			continue
		}
		bad := fmt.Errorf("%s: %q is not `PACKAGE FLOOR SKIPPED`", path, t)
		if len(f) != 3 {
			return nil, bad
		}
		pct, err1 := strconv.Atoi(f[1])
		skipped, err2 := strconv.Atoi(f[2])
		if err1 != nil || err2 != nil || pct < 0 || pct > 100 || skipped < 0 {
			return nil, bad
		}
		// A package listed twice, as a union merge leaves two branches'
		// raises: the higher floor holds.
		if old, ok := fl.pkgs[f[0]]; !ok || pct > old.pct {
			fl.pkgs[f[0]] = floor{pct, skipped}
		}
	}
	fl.head = strings.TrimRight(head.String(), "\n")
	return fl, nil
}

// writeFloors writes the floors in the order of their packages, whole or
// not at all.
func writeFloors(path string, fl *floors) error {
	var b strings.Builder
	if fl.head != "" {
		b.WriteString(fl.head + "\n\n")
	}
	fmt.Fprintf(&b, "on %s\n", fl.on)
	pkgs := make([]string, 0, len(fl.pkgs))
	for p := range fl.pkgs {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	for _, p := range pkgs {
		fmt.Fprintf(&b, "%s %d %d\n", p, fl.pkgs[p].pct, fl.pkgs[p].skipped)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// hasTests says whether a package's folder is there, and whether it has a
// test file.
func hasTests(dir string) (there, tests bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, false
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_test.go") {
			return true, true
		}
	}
	return true, false
}

// outcome is what the check found: its notes, the problems that fail it,
// and whether the floors changed.
type outcome struct {
	notes, problems []string
	changed         bool
}

// margin is how far under a package's coverage its floor is set, in
// points. Over three runs of the same code, five packages' coverage moved,
// internal/sdk's the most, by a point (78.5% to 79.5%): a floor raised on
// a run that took the longer paths must hold on one that took the shorter.
const margin = 1.5

// set is the floor a coverage sets: margin under it, in whole percent.
func set(pct float64) int {
	return max(0, int(math.Floor(pct-margin)))
}

// check holds each measured package to its floor, raises the floors that
// coverage rose past, sets new packages' and drops the floors of packages
// that are gone. On another system than the floors' it holds and changes
// nothing.
func check(fl *floors, ms []measure, goos string, tests func(string) (bool, bool)) outcome {
	var o outcome
	if fl.on == "" {
		fl.on = goos
	}
	if fl.on != goos {
		o.notes = append(o.notes, fmt.Sprintf("the floors are measured on %s, so on %s they aren't held", fl.on, goos))
		return o
	}
	var raised, added, unknown []string
	measured := map[string]bool{}
	for _, m := range ms {
		measured[m.pkg] = true
		if m.result == nil {
			unknown = append(unknown, m.pkg)
			continue
		}
		if m.result.statements == 0 {
			continue
		}
		pct := m.result.pct()
		f, ok := fl.pkgs[m.pkg]
		switch {
		case !ok:
			fl.pkgs[m.pkg] = floor{set(pct), m.skipped}
			added = append(added, fmt.Sprintf("%s %d%%", m.pkg, set(pct)))
		case pct < float64(f.pct) && m.skipped > f.skipped:
			o.notes = append(o.notes, fmt.Sprintf("%s: %s of its statements ran, under its floor of %d%%, but %d of its tests skipped here, %d when the floor was set, so it isn't held to it",
				m.pkg, shown(pct), f.pct, m.skipped, f.skipped))
		case pct < float64(f.pct):
			o.problems = append(o.problems, fmt.Sprintf("%s: its tests ran %s of its statements (%d of %d), under its floor of %d%%",
				m.pkg, shown(pct), m.result.covered, m.result.statements, f.pct))
		case set(pct) > f.pct:
			fl.pkgs[m.pkg] = floor{set(pct), m.skipped}
			raised = append(raised, fmt.Sprintf("%s %d%% to %d%%", m.pkg, f.pct, set(pct)))
		}
	}
	var gone []string
	for p := range fl.pkgs {
		if measured[p] {
			continue
		}
		switch there, hasTests := tests(p); {
		case !there:
			delete(fl.pkgs, p)
			gone = append(gone, p)
		case !hasTests:
			o.problems = append(o.problems, fmt.Sprintf("%s: it has a floor of %d%% but no tests", p, fl.pkgs[p].pct))
		}
	}
	for _, l := range []*[]string{&raised, &added, &gone, &unknown} {
		sort.Strings(*l)
	}
	// A run that fails changes no floor: the change will run again, and
	// raise them then, rather than leave the file changed for nothing.
	if len(o.problems) > 0 {
		raised, added, gone = nil, nil, nil
	}
	if len(raised) > 0 {
		o.notes = append(o.notes, "raised the floors of "+strings.Join(raised, ", "))
	}
	if len(added) > 0 {
		o.notes = append(o.notes, "set floors for "+strings.Join(added, ", "))
	}
	if len(gone) > 0 {
		o.notes = append(o.notes, "dropped the floors of packages that are gone: "+strings.Join(gone, ", "))
	}
	o.changed = len(raised)+len(added)+len(gone) > 0
	if o.changed {
		o.notes = append(o.notes, "commit "+floorFile+" with the change")
	}
	if len(unknown) > 0 {
		o.notes = append(o.notes, "not measured this run, so not held: "+strings.Join(unknown, ", "))
	}
	return o
}
