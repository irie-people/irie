package ipc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// opName is how the package names an op's constant, such as OpFreeze.
var opName = regexp.MustCompile(`^Op[A-Z]`)

// ops are the ops this package declares, by their constants' names.
func ops(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				v := s.(*ast.ValueSpec)
				for i, n := range v.Names {
					if !opName.MatchString(n.Name) || i >= len(v.Values) {
						continue
					}
					if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						op, _ := strconv.Unquote(lit.Value)
						out[n.Name] = op
					}
				}
			}
		}
	}
	return out
}

// Every op is marked by where its effect goes (DESIGN §4.5): a new op
// fails here until reach.go and DESIGN's table say whether it leaves Irie.
func TestEveryOpHasAReach(t *testing.T) {
	all := ops(t)
	if len(all) < 60 {
		t.Fatalf("found only %d ops: %v", len(all), all)
	}
	declared := map[string]bool{}
	for name, op := range all {
		declared[op] = true
		if _, ok := reaches[op]; !ok {
			t.Errorf("%s (%q) has no reach: mark it in reach.go and in DESIGN §4.5's table", name, op)
		}
	}
	for op := range reaches {
		if !declared[op] {
			t.Errorf("reach.go marks %q, which is no op", op)
		}
	}
}

// What the history takes back stays inside; a thaw's agent signs in again;
// what an agent reads, what leaves the machine and what Irie does not own
// are outside; an op this version does not know is taken as outside.
func TestReachOf(t *testing.T) {
	for op, want := range map[string]Reach{
		OpList: ReachRead, OpClose: ReachInside, OpFreeze: ReachInside, OpRename: ReachInside,
		OpThaw: ReachInsideOut, OpReopen: ReachInsideOut, OpUndo: ReachInsideOut,
		OpSpawn: ReachOutside, OpSend: ReachOutside, OpMove: ReachOutside, OpPair: ReachOutside,
		"no-such-op": ReachOutside,
	} {
		if got := ReachOf(op); got != want {
			t.Errorf("ReachOf(%q) = %q, want %q", op, got, want)
		}
	}
}
