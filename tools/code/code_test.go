package main

import (
	"reflect"
	"strings"
	"testing"
)

// spans lists each declaration as name@start-end, its parent first when
// it has one.
func spans(ds []decl) []string {
	var out []string
	for _, d := range ds {
		n := d.name
		if d.parent != "" {
			n = d.parent + "." + n
		}
		out = append(out, n+"@"+span(d))
	}
	return out
}

func TestGoDeclsTakeDocCommentsReceiversAndBlockSpecs(t *testing.T) {
	src := `package p

// Core is the core.
type Core struct {
	n int
}

// run runs.
func (c *Core) run() {
	_ = "func fake() {}"
}

func (l *List[T]) Len() int { return 0 }

const (
	a = 1
	// b is two.
	b = 2
)

var v = 3
`
	got := spans(goDecls("p.go", []byte(src)))
	want := []string{"Core@3-6", "Core.run@8-11", "List.Len@13", "@15-19", "a@16", "b@17-18", "v@21"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestInnermostPrefersTheSmallestDeclaration(t *testing.T) {
	ds := goDecls("p.go", []byte("package p\n\nconst (\n\ta = 1\n\tb = 2\n)\n"))
	if d := innermost(ds, 4); d == nil || d.name != "a" {
		t.Fatalf("line 4 is in %+v, want a", d)
	}
	if d := innermost(ds, 3); d == nil || d.name != "" {
		t.Fatalf("line 3 is in %+v, want the block", d)
	}
	if d := innermost(ds, 1); d != nil {
		t.Fatalf("line 1 is in %+v, want none", d)
	}
}

func TestSwiftMaskBlanksCommentsAndStrings(t *testing.T) {
	src := "a /* x /* y } */ z } */ b // {\n\"{\\\"}\" c \"\"\"\n{\n\"\"\" d #\"{\"# e\n"
	m := string(swiftMask([]byte(src)))
	if strings.ContainsAny(m, "{}") {
		t.Fatalf("braces left in %q", m)
	}
	if got := strings.Fields(m); !reflect.DeepEqual(got, []string{"a", "b", "c", "d", "e"}) {
		t.Errorf("code left: %q", got)
	}
	if strings.Count(m, "\n") != strings.Count(src, "\n") {
		t.Fatalf("lines changed: %q", m)
	}
}

func TestSwiftDeclsNestMembersAndLeaveLocalsOut(t *testing.T) {
	src := `import SwiftUI

/// The store.
@MainActor
final class Store {
    @State var a = 1
    private(set) var rows: [Int] = [] {
        didSet { derive() }
    }
    func derive() {
        let local = "} not a brace"
        _ = local
    }
    struct Row: Equatable {
        var id: String
    }
}

protocol P {
    func need() -> Int
    var x: Int { get }
}
`
	got := spans(swiftDecls([]byte(src)))
	want := []string{"Store@3-17", "Store.a@6", "Store.rows@7-9", "Store.derive@10-13", "Store.Row@14-16", "Row.id@15", "P@19-22", "P.need@20", "P.x@21"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMarkdownSectionsEndAtTheNextHeadingOfTheirLevel(t *testing.T) {
	src := "# Doc\n## 1. One\ntext\n### 1.1 Sub\n```\n# not a heading\n```\n## §2 Two\nmore\n"
	got := spans(mdDecls([]byte(src)))
	want := []string{"Doc@1-9", "1@2-7", "1.1@4-7", "2@8-9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestGoIdentsSkipCommentsStringsAndDeclarations(t *testing.T) {
	src := "package p\n\n// run is run\nfunc run() {}\n\nfunc main() {\n\t_ = \"run\"\n\trun()\n\tf := run\n\t_ = f\n}\n"
	if got := goIdents("p.go", []byte(src), "run"); !reflect.DeepEqual(got, []int{8, 9}) {
		t.Fatalf("got %v, want [8 9]", got)
	}
}

func TestPickPutsEachGroupInOneListWithinTheBudget(t *testing.T) {
	var gs []*group
	for i, n := range []int{40, 5, 200, 12, 90, 1, 300, 7} {
		d := &decl{start: 10 * i, end: 10*i + n - 1}
		gs = append(gs, &group{file: "f.go", d: d, hits: []int{d.start}})
	}
	for _, budget := range []int{0, 50, 300, 10000} {
		whole, listed := pick(gs, budget)
		used := 0
		for _, g := range whole {
			used += g.size()
		}
		if used > budget {
			t.Errorf("budget %d: printed %d lines", budget, used)
		}
		if len(whole)+len(listed) != len(gs) {
			t.Fatalf("budget %d: %d whole and %d listed of %d", budget, len(whole), len(listed), len(gs))
		}
		seen := map[*group]bool{}
		for _, l := range [][]*group{whole, listed} {
			for i, g := range l {
				if seen[g] {
					t.Fatalf("budget %d: a group is in both lists", budget)
				}
				seen[g] = true
				if i > 0 && l[i-1].d.start > g.d.start {
					t.Errorf("budget %d: out of order", budget)
				}
			}
		}
	}
}

func TestPickPrefersMatchesPerLineAndKeepsALongOneThatFits(t *testing.T) {
	g := func(start, size, hits int) *group {
		return &group{file: "f.go", d: &decl{start: start, end: start + size - 1}, hits: make([]int, hits)}
	}
	long, a, b := g(1, 250, 1), g(300, 20, 1), g(400, 30, 3)
	whole, listed := pick([]*group{long, a, b}, 100)
	if len(whole) != 2 || whole[0] != a || whole[1] != b || len(listed) != 1 || listed[0] != long {
		t.Fatalf("whole %v, listed %v: want the two short ones whole, in order", whole, listed)
	}
	if whole, _ := pick([]*group{long}, 300); len(whole) != 1 {
		t.Fatal("a single declaration that fits the budget is not printed whole")
	}
}
