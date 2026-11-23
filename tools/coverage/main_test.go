package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const module = "example.com/m"

func m(pkg string, statements, covered, skipped int) measure {
	return measure{pkg: pkg, result: &result{statements, covered}, skipped: skipped}
}

func darwin(pkgs map[string]floor) *floors {
	return &floors{on: "darwin", pkgs: pkgs}
}

// everyFolder is a repository where every package is there, with tests.
func everyFolder(string) (bool, bool) { return true, true }

func TestANewPackageGetsAFloorHalfAPointUnderItsCoverage(t *testing.T) {
	fl := &floors{pkgs: map[string]floor{}}
	o := check(fl, []measure{
		m("internal/a", 541, 471, 0),  // 87.06%
		m("internal/b", 1000, 805, 2), // 80.5%
		m("internal/c", 41, 41, 0),
		m("internal/d", 300, 1, 0),
	}, "darwin", everyFolder)
	want := map[string]floor{"internal/a": {86, 0}, "internal/b": {80, 2}, "internal/c": {99, 0}, "internal/d": {0, 0}}
	if !reflect.DeepEqual(fl.pkgs, want) || fl.on != "darwin" {
		t.Errorf("floors %v on %q, want %v on darwin", fl.pkgs, fl.on, want)
	}
	if !o.changed || len(o.problems) != 0 {
		t.Errorf("outcome %+v", o)
	}
}

func TestUnderTheFloorFails(t *testing.T) {
	fl := darwin(map[string]floor{"internal/core": {61, 3}})
	o := check(fl, []measure{m("internal/core", 20000, 12150, 3)}, "darwin", everyFolder)
	if len(o.problems) != 1 || o.changed {
		t.Fatalf("outcome %+v", o)
	}
	if want := "internal/core: its tests ran 60.7% of its statements (12150 of 20000), under its floor of 61%"; o.problems[0] != want {
		t.Errorf("problem %q, want %q", o.problems[0], want)
	}
	if fl.pkgs["internal/core"].pct != 61 {
		t.Error("a floor is never lowered")
	}
	// 35.96% is under 36%, and says so.
	fl = darwin(map[string]floor{"internal/paths": {36, 0}})
	o = check(fl, []measure{m("internal/paths", 89, 32, 0)}, "darwin", everyFolder)
	if want := "internal/paths: its tests ran 35.9% of its statements (32 of 89), under its floor of 36%"; len(o.problems) != 1 || o.problems[0] != want {
		t.Errorf("problems %q, want %q", o.problems, want)
	}
}

func TestAFloorRisesAPointAtATime(t *testing.T) {
	for _, c := range []struct {
		covered int
		want    int
	}{
		{800, 80},  // 80.0%: on the floor
		{814, 80},  // 81.4%: not a point and a half over it
		{815, 81},  // 81.5%
		{909, 90},  // 90.9%
		{1000, 99}, // all
	} {
		fl := darwin(map[string]floor{"internal/a": {80, 1}})
		o := check(fl, []measure{m("internal/a", 1000, c.covered, 0)}, "darwin", everyFolder)
		if got := fl.pkgs["internal/a"]; got.pct != c.want || len(o.problems) != 0 || o.changed != (c.want != 80) {
			t.Errorf("%d of 1000 covered: floor %v, outcome %+v; want %d%%", c.covered, got, o, c.want)
		}
		if c.want != 80 && fl.pkgs["internal/a"].skipped != 0 {
			t.Error("a raised floor keeps the skips of the run that raised it")
		}
	}
}

func TestARunThatSkippedMoreIsNotHeld(t *testing.T) {
	fl := darwin(map[string]floor{"internal/host": {30, 1}})
	o := check(fl, []measure{m("internal/host", 1000, 200, 4)}, "darwin", everyFolder)
	if len(o.problems) != 0 || o.changed || len(o.notes) != 1 || !strings.Contains(o.notes[0], "4 of its tests skipped here, 1 when the floor was set") {
		t.Errorf("outcome %+v", o)
	}
	o = check(fl, []measure{m("internal/host", 1000, 200, 1)}, "darwin", everyFolder)
	if len(o.problems) != 1 {
		t.Errorf("as many skips as when the floor was set: outcome %+v", o)
	}
}

func TestAnotherSystemHoldsNothing(t *testing.T) {
	fl := darwin(map[string]floor{"internal/a": {90, 0}})
	o := check(fl, []measure{m("internal/a", 100, 10, 0), m("internal/new", 10, 10, 0)}, "linux", everyFolder)
	if len(o.problems) != 0 || o.changed || len(fl.pkgs) != 1 || len(o.notes) != 1 {
		t.Errorf("outcome %+v, floors %v", o, fl.pkgs)
	}
}

func TestPackagesGoneOrWithoutTests(t *testing.T) {
	fl := darwin(map[string]floor{
		"internal/gone":      {50, 0},
		"internal/untested":  {70, 0},
		"internal/elsewhere": {80, 0},
		"internal/a":         {90, 0},
	})
	folders := func(p string) (bool, bool) {
		switch p {
		case "internal/gone":
			return false, false
		case "internal/untested":
			return true, false
		}
		return true, true
	}
	o := check(fl, []measure{m("internal/a", 100, 95, 0)}, "darwin", folders)
	if _, ok := fl.pkgs["internal/gone"]; ok || !o.changed {
		t.Errorf("a package that is gone keeps its floor: %v", fl.pkgs)
	}
	if !reflect.DeepEqual(o.problems, []string{"internal/untested: it has a floor of 70% but no tests"}) {
		t.Errorf("problems %q", o.problems)
	}
	if _, ok := fl.pkgs["internal/elsewhere"]; !ok {
		t.Error("a package this run didn't test lost its floor")
	}
}

func TestWhatTheRunCouldNotTellIsNoted(t *testing.T) {
	fl := darwin(map[string]floor{"internal/a": {90, 0}})
	o := check(fl, []measure{{pkg: "internal/a"}, m("internal/web", 0, 0, 0)}, "darwin", everyFolder)
	if len(o.problems) != 0 || o.changed || len(fl.pkgs) != 1 {
		t.Errorf("outcome %+v, floors %v", o, fl.pkgs)
	}
	if len(o.notes) != 1 || !strings.Contains(o.notes[0], "internal/a") {
		t.Errorf("notes %q", o.notes)
	}
}

func TestTheRunnersLines(t *testing.T) {
	in := "# testrunner coverage v1: package statements covered skipped\n" +
		"example.com/m/internal/a 541 471 2\n" +
		"example.com/m/cmd/x -\n" +
		"example.com/m 10 5 0\n"
	got, err := readMeasures(strings.NewReader(in), module)
	if err != nil {
		t.Fatal(err)
	}
	want := []measure{m("internal/a", 541, 471, 2), {pkg: "cmd/x"}, m(".", 10, 5, 0)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readMeasures = %+v, want %+v", got, want)
	}
	for _, bad := range []string{"example.com/m/a 1 2 0\n", "example.com/m/a 1 x 0\n", "example.com/m/a 1 1\n", "example.com/m/a -1 0 0\n"} {
		if _, err := readMeasures(strings.NewReader(bad), module); err == nil {
			t.Errorf("readMeasures(%q) took it", bad)
		}
	}
}

func TestTheFloorFileKeepsItsWordsAndTheHigherOfTwoFloors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "floor.txt")
	text := "# The floors.\n#\n# They only rise.\n\non darwin\ninternal/b 70 0\ninternal/a 80 1\ninternal/b 72 2\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	fl, err := readFloors(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]floor{"internal/a": {80, 1}, "internal/b": {72, 2}}; !reflect.DeepEqual(fl.pkgs, want) || fl.on != "darwin" {
		t.Errorf("floors %v on %q, want %v", fl.pkgs, fl.on, want)
	}
	if err := writeFloors(path, fl); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if want := "# The floors.\n#\n# They only rise.\n\non darwin\ninternal/a 80 1\ninternal/b 72 2\n"; string(b) != want {
		t.Errorf("wrote %q, want %q", b, want)
	}
	for _, bad := range []string{"internal/a 80\n", "internal/a x 0\n", "internal/a 101 0\n"} {
		os.WriteFile(path, []byte(bad), 0o644)
		if _, err := readFloors(path); err == nil {
			t.Errorf("readFloors took %q", bad)
		}
	}
	if fl, err := readFloors(filepath.Join(t.TempDir(), "none.txt")); err != nil || len(fl.pkgs) != 0 {
		t.Errorf("a file not there yet: %v, %v", fl, err)
	}
}

func TestTheModulesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go.mod")
	os.WriteFile(path, []byte("// A module.\nmodule example.com/m\n\ngo 1.26.0\n"), 0o644)
	if got, err := modulePath(path); got != module || err != nil {
		t.Errorf("modulePath = %q, %v", got, err)
	}
}

// TestTheRepositorysFloorsRead: the floors this repository keeps read as
// floors.
func TestTheRepositorysFloorsRead(t *testing.T) {
	fl, err := readFloors("floor.txt")
	if err != nil {
		t.Fatal(err)
	}
	if fl.on == "" || len(fl.pkgs) == 0 || !strings.Contains(fl.head, "Never lower a floor") {
		t.Errorf("floor.txt: on %q, %d floors, words %q", fl.on, len(fl.pkgs), fl.head)
	}
}
