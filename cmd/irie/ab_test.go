package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// abBuild writes a shell script that stands in for a build of a program.
func abBuild(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func abCode(err error) int {
	var ec exitCode
	if errors.As(err, &ec) {
		return ec.code
	}
	if err != nil {
		return 1
	}
	return 0
}

func TestIrieABSaysTheOutputIsTheSame(t *testing.T) {
	gold := abBuild(t, "gold", `echo "tool v1"; echo "$1"`)
	nw := abBuild(t, "new", `echo "tool v2"; echo "$1"`)
	var out bytes.Buffer
	err := abCmd([]string{"-n", "2", "--ignore", `v\d`, gold, nw, "--", "an arg"}, &out)
	if err != nil {
		t.Fatalf("got %v\n%s", err, out.String())
	}
	for _, want := range []string{"args    an arg", "runs    2 each, taking turns, after 1 each to warm up", "output  the same: 15 bytes in 2 lines, exit 0", "time", "with the same output"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report lacks %q:\n%s", want, out.String())
		}
	}
}

func TestIrieABExitsOneWhenTheOutputsDiffer(t *testing.T) {
	gold := abBuild(t, "gold", `echo a; echo b`)
	nw := abBuild(t, "new", `echo a; echo c`)
	var out bytes.Buffer
	err := abCmd([]string{"-n", "1", gold, nw}, &out)
	if abCode(err) != 1 || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("got %v, want exit 1 saying the output differs", err)
	}
	if !strings.Contains(out.String(), "output  differs at line 2:\n  gold  b\n  new   c") {
		t.Errorf("the report does not name the line:\n%s", out.String())
	}
}

func TestIrieABPrintsJSON(t *testing.T) {
	gold := abBuild(t, "gold", `echo same`)
	var out bytes.Buffer
	if err := abCmd([]string{"-n", "1", "--warmup", "0", "--json", gold, gold}, &out); err != nil {
		t.Fatal(err)
	}
	var r struct {
		Same bool
		Runs int
		Time struct{ Unit string }
	}
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("%v:\n%s", err, out.String())
	}
	if !r.Same || r.Runs != 1 || r.Time.Unit != "s" {
		t.Errorf("got %+v", r)
	}
}

func TestIrieABNeedsTwoBuildsAndArgumentsAfterDashes(t *testing.T) {
	gold := abBuild(t, "gold", `true`)
	for _, args := range [][]string{{gold}, {gold, gold, "stray"}, {"-n", "0", gold, gold}} {
		if err := abCmd(args, &bytes.Buffer{}); abCode(err) != 2 {
			t.Errorf("%q: got %v, want a usage error", args, err)
		}
	}
	if err := abCmd([]string{"-n", "1", gold, filepath.Join(t.TempDir(), "missing")}, &bytes.Buffer{}); abCode(err) != 3 {
		t.Errorf("a build that is not there: got %v, want exit 3", err)
	}
}
