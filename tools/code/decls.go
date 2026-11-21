package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

// A decl is a declaration in a file: a Go func, type, const or var, a
// Swift type, function or property, or a Markdown section. Lines count
// from 1.
type decl struct {
	name   string // the bare name: dogScan, Store, or a section's number
	parent string // the type it belongs to: a method's receiver, a Swift member's type
	label  string // its first line, trimmed: func (c *Core) dogScan(now time.Time) []jot
	start  int    // its first line, the doc comment above it included
	line   int    // the line the declaration itself starts on
	end    int    // its last line
	body   bool   // a function or a computed property: what is declared inside is local
}

// contains says whether the declaration covers line n.
func (d decl) contains(n int) bool { return d.start <= n && n <= d.end }

// innermost is the smallest declaration that covers line n, or nil: a
// method rather than the type it sits in, a subsection rather than its
// section.
func innermost(ds []decl, n int) *decl {
	var best *decl
	for i := range ds {
		d := &ds[i]
		if d.contains(n) && (best == nil || d.end-d.start < best.end-best.start) {
			best = d
		}
	}
	return best
}

// declsOf reads the declarations of a file by its kind; a file of
// another kind has none.
func declsOf(path string, src []byte) []decl {
	switch {
	case strings.HasSuffix(path, ".go"):
		return goDecls(path, src)
	case strings.HasSuffix(path, ".swift"):
		return swiftDecls(src)
	case strings.HasSuffix(path, ".md"):
		return mdDecls(src)
	}
	return nil
}

// firstLine is the text of line n, trimmed, without an opening brace at
// its end, and cut to a length that reads at a glance.
func firstLine(lines []string, n int) string {
	if n < 1 || n > len(lines) {
		return ""
	}
	s := strings.TrimSpace(lines[n-1])
	s = strings.TrimSpace(strings.TrimSuffix(s, "{"))
	if len(s) > 140 {
		s = s[:137] + "..."
	}
	return s
}

// splitLines splits a file into its lines, without their newlines.
func splitLines(src []byte) []string {
	s := strings.TrimSuffix(string(src), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// goDecls reads a Go file's top-level declarations, and the single
// specs of a const, var or type block, with go/parser. A file that does
// not parse gives what was read before the error.
func goDecls(path string, src []byte) []decl {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if f == nil {
		return nil
	}
	lines := splitLines(src)
	line := func(p token.Pos) int { return fset.Position(p).Line }
	var ds []decl
	add := func(name, parent string, doc *ast.CommentGroup, from, to token.Pos, body bool) {
		d := decl{name: name, parent: parent, line: line(from), end: line(to), body: body}
		d.start = d.line
		if doc != nil {
			d.start = line(doc.Pos())
		}
		d.label = firstLine(lines, d.line)
		ds = append(ds, d)
	}
	for _, x := range f.Decls {
		switch x := x.(type) {
		case *ast.FuncDecl:
			add(x.Name.Name, recvType(x.Recv), x.Doc, x.Pos(), x.End(), true)
		case *ast.GenDecl:
			if x.Tok == token.IMPORT {
				continue
			}
			if x.Lparen.IsValid() {
				add("", "", x.Doc, x.Pos(), x.End(), false)
			}
			for _, s := range x.Specs {
				doc := x.Doc
				if x.Lparen.IsValid() {
					doc = nil
				}
				switch s := s.(type) {
				case *ast.TypeSpec:
					if s.Doc != nil {
						doc = s.Doc
					}
					from := s.Pos()
					if !x.Lparen.IsValid() {
						from = x.Pos()
					}
					add(s.Name.Name, "", doc, from, s.End(), false)
				case *ast.ValueSpec:
					if s.Doc != nil {
						doc = s.Doc
					}
					from := s.Pos()
					if !x.Lparen.IsValid() {
						from = x.Pos()
					}
					for _, n := range s.Names {
						add(n.Name, "", doc, from, s.End(), false)
					}
				}
			}
		}
	}
	return ds
}

// recvType is the name of a method's receiver type, without its pointer
// or type parameters; empty for a function.
func recvType(r *ast.FieldList) string {
	if r == nil || len(r.List) == 0 {
		return ""
	}
	t := r.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// goIdents lists the lines where an identifier named name is used in a
// Go file: in code, never in a comment or a string, and never where it
// is declared.
func goIdents(path string, src []byte, name string) []int {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if f == nil {
		return nil
	}
	declared := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			declared[x.Name] = true
		case *ast.TypeSpec:
			declared[x.Name] = true
		case *ast.ValueSpec:
			for _, id := range x.Names {
				declared[id] = true
			}
		}
		return true
	})
	var out []int
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name && !declared[id] {
			out = append(out, fset.Position(id.Pos()).Line)
		}
		return true
	})
	return out
}

// swiftMask is a Swift file with its comments and the insides of its
// string literals blanked to spaces, its newlines kept, so that braces
// and names can be found in it by line and column. Block comments nest,
// as Swift's do; a multi-line string runs to its closing """; a raw
// string #"..."# to the same number of #s.
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
		case src[i] == '"' || (src[i] == '#' && rawStringStart(src, i)):
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
	close := []byte(quote + strings.Repeat("#", hashes))
	j := i + len(quote)
	for j < len(src) {
		if hashes == 0 && src[j] == '\\' {
			j += 2
			continue
		}
		if quote == `"` && src[j] == '\n' {
			return j // an unclosed string ends with its line
		}
		if bytes.HasPrefix(src[j:], close) {
			return j + len(close)
		}
		j++
	}
	return len(src)
}

// swiftDeclRE finds a declaration at the start of a masked Swift line:
// attributes and modifiers, the keyword, then the name.
var swiftDeclRE = regexp.MustCompile(`^\s*(?:@[\w.]+(?:\([^)]*\))?\s+)*(?:(?:public|private|fileprivate|internal|open|package|final|static|class|override|mutating|nonmutating|nonisolated|convenience|required|lazy|weak|unowned|indirect|dynamic|optional|isolated|consuming|borrowing)(?:\([a-z]+\))?\s+)*(func|init|deinit|subscript|struct|class|enum|protocol|extension|actor|var|let|typealias)\b\s*([^\s(<:={,]*)`)

// swiftDecls reads a Swift file's declarations: types, extensions,
// functions, initialisers and properties, each to its closing brace or,
// without a body, to the end of its statement. What is declared inside
// a function's or a computed property's body is local, so it is left
// out.
func swiftDecls(src []byte) []decl {
	m := swiftMask(src)
	lines := splitLines(src)
	starts := lineStarts(m)
	var ds []decl
	type open struct {
		name     string
		end      int
		localish bool
	}
	var stack []open
	for i := 0; i < len(starts); i++ {
		n := i + 1
		for len(stack) > 0 && stack[len(stack)-1].end < n {
			stack = stack[:len(stack)-1]
		}
		lineEnd := len(m)
		if i+1 < len(starts) {
			lineEnd = starts[i+1]
		}
		g := swiftDeclRE.FindSubmatchIndex(m[starts[i]:lineEnd])
		if g == nil {
			continue
		}
		if len(stack) > 0 && stack[len(stack)-1].localish {
			continue
		}
		kw := string(m[starts[i]+g[2] : starts[i]+g[3]])
		name := string(m[starts[i]+g[4] : starts[i]+g[5]])
		switch kw {
		case "init", "deinit", "subscript":
			name = kw
		}
		endOff, hasBody := swiftDeclEnd(m, starts[i]+g[2])
		d := decl{name: name, line: n, start: n, end: lineOf(starts, endOff), label: firstLine(lines, n)}
		d.body = hasBody && (kw == "func" || kw == "init" || kw == "deinit" || kw == "subscript" || kw == "var" || kw == "let")
		if len(stack) > 0 {
			d.parent = stack[len(stack)-1].name
		}
		d.start = docStart(lines, n)
		ds = append(ds, d)
		stack = append(stack, open{name: name, end: d.end, localish: d.body})
	}
	return ds
}

// swiftDeclEnd finds where the declaration whose keyword is at i ends,
// in a masked file: at the brace that closes its body, or at the end of
// its statement when it has none. A newline ends a statement unless a
// bracket is open or the next line goes on with the signature.
func swiftDeclEnd(m []byte, i int) (end int, body bool) {
	depth := 0 // ( and [
	for j := i; j < len(m); j++ {
		switch m[j] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '{':
			if depth <= 0 {
				return matchBrace(m, j), true
			}
		case '\n':
			if depth <= 0 && !continues(m[j+1:]) {
				return j, false
			}
		}
	}
	return len(m), false
}

// continues says whether the next line goes on with a declaration's
// signature rather than starting a statement of its own.
func continues(rest []byte) bool {
	s := bytes.TrimLeft(rest, " \t")
	for _, p := range []string{"->", "where ", "throws", "rethrows", "async", "{", ")", "]"} {
		if bytes.HasPrefix(s, []byte(p)) {
			return true
		}
	}
	return false
}

// matchBrace is the index of the brace that closes the one at i.
func matchBrace(m []byte, i int) int {
	depth := 0
	for j := i; j < len(m); j++ {
		switch m[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return len(m) - 1
}

// docStart is the first line of the comment and attribute lines just
// above line n, or n. An attribute line that declares something itself,
// such as @State var x, is not one of them.
func docStart(lines []string, n int) int {
	s := n
	for s > 1 {
		t := strings.TrimSpace(lines[s-2])
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "@") && !swiftDeclRE.MatchString(t) {
			s--
			continue
		}
		break
	}
	return s
}

// lineStarts is the offset of each line's first byte.
func lineStarts(b []byte) []int {
	if len(b) == 0 {
		return nil
	}
	out := []int{0}
	for i, c := range b {
		if c == '\n' && i+1 < len(b) {
			out = append(out, i+1)
		}
	}
	return out
}

// lineOf is the line, counted from 1, of the byte at off.
func lineOf(starts []int, off int) int {
	lo, hi := 0, len(starts)
	for lo < hi {
		mid := (lo + hi) / 2
		if starts[mid] <= off {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// mdHeadingRE finds a Markdown heading and a section number at its start,
// such as 4.8 or §4.8.
var mdHeadingRE = regexp.MustCompile(`^(#+)\s+(?:§\s*)?([0-9]+(?:\.[0-9]+)*\.?)?`)

// mdDecls reads a Markdown file's sections, each from its heading to
// the line before the next heading at its level or above. Lines in code
// blocks are never taken for headings.
func mdDecls(src []byte) []decl {
	lines := splitLines(src)
	var ds []decl
	var levels []int
	code := false
	for i, l := range lines {
		if strings.HasPrefix(l, "```") || strings.HasPrefix(l, "~~~") {
			code = !code
			continue
		}
		g := mdHeadingRE.FindStringSubmatch(l)
		if code || g == nil {
			continue
		}
		name := strings.TrimSuffix(g[2], ".")
		if name == "" {
			name = strings.TrimSpace(strings.TrimLeft(l, "#"))
		}
		ds = append(ds, decl{name: name, label: strings.TrimSpace(l), start: i + 1, line: i + 1, end: len(lines)})
		levels = append(levels, len(g[1]))
	}
	for i := range ds {
		for j := i + 1; j < len(ds); j++ {
			if levels[j] <= levels[i] {
				ds[i].end = ds[j].line - 1
				break
			}
		}
	}
	return ds
}

// wordLines lists the lines of a masked file where name stands as a
// whole word.
func wordLines(m []byte, name string) []int {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	starts := lineStarts(m)
	var out []int
	last := 0
	for _, x := range re.FindAllIndex(m, -1) {
		if n := lineOf(starts, x[0]); n != last {
			out = append(out, n)
			last = n
		}
	}
	return out
}
