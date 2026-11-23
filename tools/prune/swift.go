package main

import (
	"bytes"
	"fmt"
	"go/build/constraint"
	"regexp"
	"strings"
)

// Step 4 reads the Mac app's #if blocks as step 1 reads //go:build lines.
// Each Go tag noX is the Swift condition NOX (nomail is NOMAIL). In a block
// whose conditions use only NO* names, with !, &&, || and parentheses, a
// branch the copy's flags make false goes; one they make true stays without
// its #if, and the branches after it go; one that depends on other names
// stays as it is. A block with another condition, such as os() or
// canImport(), keeps that branch as it is, and blocks inside any branch
// left are settled in turn. A file left with no code but its comments and
// imports goes, such as one whose code is wholly #if !NOMAIL … #endif.

// swiftFlag is a name step 4 reads: NO and capitals.
var swiftFlag = regexp.MustCompile(`^NO[A-Z0-9_]+$`)

// swiftFlags are the Swift conditions the copy's tags set: NOX for each
// tag noX.
func swiftFlags(tags []string) map[string]bool {
	m := map[string]bool{}
	for _, t := range tags {
		if strings.HasPrefix(t, "no") && swiftFlag.MatchString(strings.ToUpper(t)) {
			m[strings.ToUpper(t)] = true
		}
	}
	return m
}

// swiftCond reads an #if or #elseif condition, or says it is not one of
// NO* names alone, which step 4 leaves as it is.
func swiftCond(cond string) (constraint.Expr, bool) {
	x, err := constraint.Parse("//go:build " + cond)
	if err != nil {
		return nil, false
	}
	ok := true
	eachTag(x, func(t string) {
		if !swiftFlag.MatchString(t) {
			ok = false
		}
	})
	return x, ok
}

// swiftNode is a line of a Swift file, or an #if block starting there.
type swiftNode struct {
	line  int
	block *swiftBlock
}

// swiftBlock is an #if block: its branches, from the #if, then any #elseif
// and #else, to its #endif.
type swiftBlock struct {
	branches []swiftBranch
	end      int
}

type swiftBranch struct {
	line int    // the directive's
	kind string // #if, #elseif or #else
	cond string
	body []swiftNode
}

// swiftDirective reads a line of a masked Swift file as a directive: #if,
// #elseif, #else or #endif and its condition, or "" for any other line.
func swiftDirective(masked string) (kind, cond string) {
	t := strings.TrimSpace(masked)
	for _, k := range []string{"#elseif", "#else", "#endif", "#if"} {
		rest, ok := strings.CutPrefix(t, k)
		if !ok || rest != "" && !strings.ContainsRune(" \t(!", rune(rest[0])) {
			continue
		}
		return k, strings.TrimSpace(rest)
	}
	return "", ""
}

// parseSwift reads a Swift file's lines into lines and #if blocks, nested.
func parseSwift(masked []string) ([]swiftNode, error) {
	var top []swiftNode
	var open []*swiftBlock
	body := func() *[]swiftNode {
		if len(open) == 0 {
			return &top
		}
		b := open[len(open)-1]
		return &b.branches[len(b.branches)-1].body
	}
	for i, l := range masked {
		kind, cond := swiftDirective(l)
		switch kind {
		case "":
			*body() = append(*body(), swiftNode{line: i})
		case "#if":
			b := &swiftBlock{branches: []swiftBranch{{line: i, kind: kind, cond: cond}}}
			*body() = append(*body(), swiftNode{line: i, block: b})
			open = append(open, b)
		case "#elseif", "#else":
			if len(open) == 0 {
				return nil, fmt.Errorf("line %d: %s without #if", i+1, kind)
			}
			b := open[len(open)-1]
			if b.branches[len(b.branches)-1].kind == "#else" {
				return nil, fmt.Errorf("line %d: %s after #else", i+1, kind)
			}
			b.branches = append(b.branches, swiftBranch{line: i, kind: kind, cond: cond})
		case "#endif":
			if len(open) == 0 {
				return nil, fmt.Errorf("line %d: #endif without #if", i+1)
			}
			open[len(open)-1].end = i
			open = open[:len(open)-1]
		}
	}
	if len(open) > 0 {
		return nil, fmt.Errorf("line %d: #if without #endif", open[len(open)-1].branches[0].line+1)
	}
	return top, nil
}

// pruneSwift is step 4 for one Swift file: its text with the blocks the
// copy's flags settle settled, and how many it settled.
func pruneSwift(src []byte, flags map[string]bool) ([]byte, int, error) {
	text := string(src)
	final := strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	masked := strings.Split(strings.TrimSuffix(string(swiftMask(src)), "\n"), "\n")
	nodes, err := parseSwift(masked)
	if err != nil {
		return nil, 0, err
	}
	o := &swiftOut{src: lines, flags: flags}
	o.nodes(nodes)
	if o.settled == 0 {
		return src, 0, nil
	}
	out := o.lines
	// A block gone at the end leaves no blank lines after the code.
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" && strings.TrimSpace(lines[len(lines)-1]) != "" {
		out = out[:len(out)-1]
	}
	s := strings.Join(out, "\n")
	if final && len(out) > 0 {
		s += "\n"
	}
	return []byte(s), o.settled, nil
}

// swiftOut writes a Swift file's lines with its blocks settled.
type swiftOut struct {
	src     []string
	flags   map[string]bool
	lines   []string
	settled int  // blocks changed
	dropped bool // a block just went: a blank line after it goes too where one is already there
}

func (o *swiftOut) emit(l string) {
	blank := strings.TrimSpace(l) == ""
	if o.dropped && blank && (len(o.lines) == 0 || strings.TrimSpace(o.lines[len(o.lines)-1]) == "") {
		o.dropped = false
		return
	}
	o.dropped = false
	o.lines = append(o.lines, l)
}

func (o *swiftOut) nodes(ns []swiftNode) {
	for _, n := range ns {
		if n.block == nil {
			o.emit(o.src[n.line])
		} else {
			o.block(n.block)
		}
	}
}

// block writes b settled: its branches that may still be taken, or the
// one sure to be, without its #if.
func (o *swiftOut) block(b *swiftBlock) {
	type kept struct {
		head string
		body []swiftNode
	}
	var ks []kept
	changed := false
	var sure *swiftBranch // the branch taken whenever those before it are not
	for i := range b.branches {
		br := &b.branches[i]
		v := always
		if br.kind != "#else" {
			v = depends
			if x, ok := swiftCond(br.cond); ok {
				_, v = settle(x, o.flags)
			}
		}
		if v == never {
			changed = true
			continue
		}
		head := o.src[br.line]
		switch {
		case v == always && len(ks) == 0:
			sure = br
		case v == always && br.kind == "#else":
			ks = append(ks, kept{head, br.body})
		case v == always:
			// An #elseif sure to be taken is the #else.
			changed = true
			ks = append(ks, kept{indent(head) + "#else", br.body})
		case len(ks) == 0 && br.kind == "#elseif":
			// The first branch left starts the block.
			changed = true
			ks = append(ks, kept{strings.Replace(head, "#elseif", "#if", 1), br.body})
		default:
			ks = append(ks, kept{head, br.body})
		}
		if v == always {
			changed = changed || i < len(b.branches)-1
			break
		}
	}
	if sure != nil {
		o.settled++
		before := len(o.lines)
		o.nodes(sure.body)
		o.dropped = len(o.lines) == before
		return
	}
	if len(ks) == 0 {
		o.settled++
		o.dropped = true
		return
	}
	if changed {
		o.settled++
	}
	for _, k := range ks {
		o.emit(k.head)
		o.nodes(k.body)
	}
	o.emit(o.src[b.end])
}

func indent(l string) string { return l[:len(l)-len(strings.TrimLeft(l, " \t"))] }

// swiftHasCode says whether a Swift file holds code other than comments
// and imports.
func swiftHasCode(src []byte) bool {
	for _, l := range strings.Split(string(swiftMask(src)), "\n") {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, "import ") {
			return true
		}
	}
	return false
}

// swiftMask is a Swift file with its comments and the insides of its
// string literals blanked to spaces, its newlines kept, so that a
// directive is found only where the compiler reads one: as tools/code's,
// block comments nest, a multi-line string runs to its closing """ and a
// raw string #"..."# to the same number of #s.
func swiftMask(src []byte) []byte {
	m := bytes.Clone(src)
	blank := func(i, j int) {
		for k := i; k < j && k < len(m); k++ {
			if m[k] != '\n' {
				m[k] = ' '
			}
		}
	}
	n := len(src)
	for i := 0; i < n; {
		switch {
		case src[i] == '/' && i+1 < n && src[i+1] == '/':
			j := i
			for j < n && src[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case src[i] == '/' && i+1 < n && src[i+1] == '*':
			depth, j := 0, i
			for j < n {
				if src[j] == '/' && j+1 < n && src[j+1] == '*' {
					depth++
					j += 2
				} else if src[j] == '*' && j+1 < n && src[j+1] == '/' {
					depth--
					j += 2
					if depth == 0 {
						break
					}
				} else {
					j++
				}
			}
			blank(i, j)
			i = j
		case src[i] == '"' || src[i] == '#' && rawStringStart(src, i):
			j := stringEnd(src, i)
			blank(i, j)
			i = j
		default:
			i++
		}
	}
	return m
}

// rawStringStart says whether the #s at i open a raw string: #"..."#.
func rawStringStart(src []byte, i int) bool {
	for i < len(src) && src[i] == '#' {
		i++
	}
	return i < len(src) && src[i] == '"'
}

// stringEnd is the index just past the string literal that starts at i.
func stringEnd(src []byte, i int) int {
	hashes := 0
	for i < len(src) && src[i] == '#' {
		hashes++
		i++
	}
	quote := `"`
	if bytes.HasPrefix(src[i:], []byte(`"""`)) {
		quote = `"""`
	}
	end := []byte(quote + strings.Repeat("#", hashes))
	j := i + len(quote)
	for j < len(src) {
		if hashes == 0 && src[j] == '\\' {
			j += 2
			continue
		}
		if quote == `"` && src[j] == '\n' {
			return j // an unclosed string ends with its line
		}
		if bytes.HasPrefix(src[j:], end) {
			return j + len(end)
		}
		j++
	}
	return len(src)
}
