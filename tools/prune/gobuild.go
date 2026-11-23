package main

import (
	"fmt"
	"go/build/constraint"
	"strings"
)

// goAction is what step 1 does with a Go file.
type goAction int

const (
	goKeep    goAction = iota // no constraint, or one without the copy's tags
	goDelete                  // false whatever else is set
	goUnwrap                  // true whatever else is set: the line goes
	goRewrite                 // depends on other tags: rewritten without the copy's
)

// goEdit is step 1's verdict on a Go file: what to do, the constraint's
// expression before and after, as gofmt prints them, and the file's new
// text when it stays changed.
type goEdit struct {
	action   goAction
	old, new string
	src      []byte
}

// A verdict is what a condition comes to with the copy's tags set.
type verdict int

const (
	depends verdict = iota // on names the copy leaves free
	always
	never
)

// pruneGo is step 1 for one Go file: what its //go:build line comes to
// with every tag in tags true and every other tag, GOOS and GOARCH free.
func pruneGo(src []byte, tags map[string]bool) (goEdit, error) {
	lines := strings.Split(string(src), "\n")
	at, err := buildLine(lines)
	if err != nil || at < 0 {
		return goEdit{}, err
	}
	x, err := constraint.Parse(strings.TrimSpace(lines[at]))
	if err != nil {
		return goEdit{}, fmt.Errorf("line %d: %v", at+1, err)
	}
	if !mentions(x, tags) {
		return goEdit{}, nil
	}
	e := goEdit{old: x.String()}
	y, v := settle(x, tags)
	switch v {
	case never:
		e.action = goDelete
		return e, nil
	case always:
		e.action = goUnwrap
		// The blank line after it goes too, unless a comment above would
		// then run into the package clause and become its doc.
		n := 1
		if at+1 < len(lines) && strings.TrimSpace(lines[at+1]) == "" &&
			(at == 0 || strings.TrimSpace(lines[at-1]) == "") {
			n = 2
		}
		lines = append(lines[:at:at], lines[at+n:]...)
	default:
		e.action = goRewrite
		e.new = y.String()
		lines[at] = "//go:build " + e.new
	}
	e.src = []byte(strings.Join(lines, "\n"))
	return e, nil
}

// buildLine finds the //go:build line among the comments and blank lines
// before a file's code, where go/build looks for it: its index, or -1.
// A second one is an error, and so is a // +build line, which go fix
// turns into //go:build.
func buildLine(lines []string) (int, error) {
	at := -1
	inBlock := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if !inBlock {
			switch {
			case t == "":
				continue
			case constraint.IsGoBuild(t):
				if at >= 0 {
					return -1, fmt.Errorf("line %d: a second //go:build line", i+1)
				}
				at = i
				continue
			case constraint.IsPlusBuild(t):
				return -1, fmt.Errorf("line %d: a // +build line: go fix makes it //go:build", i+1)
			}
		}
		for t != "" {
			switch {
			case inBlock:
				j := strings.Index(t, "*/")
				if j < 0 {
					t = ""
					continue
				}
				inBlock = false
				t = strings.TrimSpace(t[j+2:])
			case strings.HasPrefix(t, "//"):
				t = ""
			case strings.HasPrefix(t, "/*"):
				inBlock = true
				t = strings.TrimSpace(t[2:])
			default:
				return at, nil // the package clause: the header ends
			}
		}
	}
	return at, nil
}

// mentions says whether x names any of tags.
func mentions(x constraint.Expr, tags map[string]bool) bool {
	found := false
	eachTag(x, func(t string) {
		if tags[t] {
			found = true
		}
	})
	return found
}

// eachTag calls f with each tag x names, as often as it names it.
func eachTag(x constraint.Expr, f func(string)) {
	switch x := x.(type) {
	case *constraint.TagExpr:
		f(x.Tag)
	case *constraint.NotExpr:
		eachTag(x.X, f)
	case *constraint.AndExpr:
		eachTag(x.X, f)
		eachTag(x.Y, f)
	case *constraint.OrExpr:
		eachTag(x.X, f)
		eachTag(x.Y, f)
	}
}

// settle is x with every tag in tags true: always or never, or an
// expression of the other names that depends on them. Where setting the
// tags leaves an expression that is still always true or never, such as
// linux || !linux, it says so.
func settle(x constraint.Expr, tags map[string]bool) (constraint.Expr, verdict) {
	y, v := substitute(x, tags)
	if v != depends {
		return nil, v
	}
	var free []string
	seen := map[string]bool{}
	eachTag(y, func(t string) {
		if !seen[t] {
			seen[t] = true
			free = append(free, t)
		}
	})
	if len(free) > 16 {
		return y, depends // too many to try every setting of
	}
	set := map[string]bool{}
	var trues, falses int
	for n := 0; n < 1<<len(free); n++ {
		for i, t := range free {
			set[t] = n&(1<<i) != 0
		}
		if y.Eval(func(t string) bool { return set[t] }) {
			trues++
		} else {
			falses++
		}
	}
	switch {
	case falses == 0:
		return nil, always
	case trues == 0:
		return nil, never
	}
	return y, depends
}

// substitute sets every tag in tags true in x and simplifies what that
// settles: x && true is x, x || true is true, !true is false. What it
// returns depends on the other names unless the verdict says otherwise.
func substitute(x constraint.Expr, tags map[string]bool) (constraint.Expr, verdict) {
	switch x := x.(type) {
	case *constraint.TagExpr:
		if tags[x.Tag] {
			return nil, always
		}
		return x, depends
	case *constraint.NotExpr:
		y, v := substitute(x.X, tags)
		switch v {
		case always:
			return nil, never
		case never:
			return nil, always
		}
		return &constraint.NotExpr{X: y}, depends
	case *constraint.AndExpr:
		l, lv := substitute(x.X, tags)
		r, rv := substitute(x.Y, tags)
		switch {
		case lv == never || rv == never:
			return nil, never
		case lv == always:
			return r, rv
		case rv == always:
			return l, lv
		}
		return &constraint.AndExpr{X: l, Y: r}, depends
	case *constraint.OrExpr:
		l, lv := substitute(x.X, tags)
		r, rv := substitute(x.Y, tags)
		switch {
		case lv == always || rv == always:
			return nil, always
		case lv == never:
			return r, rv
		case rv == never:
			return l, lv
		}
		return &constraint.OrExpr{X: l, Y: r}, depends
	}
	return x, depends
}
