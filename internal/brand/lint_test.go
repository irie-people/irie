package brand

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The literals that keep Irie's name on purpose, by file: data formats that
// must read the same in every copy, which no person reads as the name.
var keep = map[string][]string{
	// The context store's git trailers (DESIGN §4.13): a trailer's key
	// can't hold a space, and old commits keep theirs.
	"internal/ctxgit/message.go": {"Irie-Session", "Irie-Name", "Irie-Conversation", "Irie-Turn", "Irie-Leaf", "Irie-Model", "Irie-Code", "Irie-Dirty", "Irie-Partial", "Irie-Cut"},
	// Journal values: the harness of a closed session's events, and an
	// event's source.
	"internal/core/undo.go":   {"irie"},
	"internal/event/event.go": {"irie"},
	// The design library's copies in the Mac and iPhone apps' sources,
	// whose folders keep Irie's name in every copy (DESIGN §2.10).
	"internal/designcheck/designcheck.go": {"mac/Sources/Irie/Theme.swift", "ios/Irie.swiftpm/Sources/Look.swift"},
}

// named says whether s holds the name: Irie, or irie as a word, as in
// `irie up` or ~/.config/irie, but not in irie-people (the project's
// home), @irie_ (tmux options) or IRIE_FAKE_ (tests' hooks).
func named(s string) bool {
	if strings.Contains(s, "Irie") {
		return true
	}
	word := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	for i := 0; ; {
		j := strings.Index(s[i:], "irie")
		if j < 0 {
			return false
		}
		a, b := i+j, i+j+4
		i = b
		if a > 0 && (word(s[a-1]) || s[a-1] == '@' || s[a-1] == '.' && strings.HasSuffix(s[:a], "irie-hq.")) || b < len(s) && word(s[b]) {
			continue
		}
		if rest := s[b:]; strings.HasPrefix(rest, "-people") || strings.HasPrefix(rest, "-hq") || strings.HasPrefix(rest, "-n") && strings.HasSuffix(s[:a], "data-") {
			continue
		}
		if strings.HasSuffix(s[:a], "irie-people.") {
			continue // io.github.irie-people.irie: the phone profile's id, as it was
		}
		return true
	}
}

// archNamed says whether s names an arch agent as a person reads it: Mikael,
// Gabriel, Raphael, Raffi or Raguel as a word. Their keys, such as gabriel
// in arch.gabriel, stay.
func archNamed(s string) bool { return archName.MatchString(s) }

var archName = regexp.MustCompile(`\b(Mikael|Gabriel|Raphael|Raffi|Raguel|Ragu)\b`)

// TestNoNameOutsideBrand: Go code names the product through this package
// (Name, Slug, Env), so a copy renames it here alone (DESIGN §2.10).
func TestNoNameOutsideBrand(t *testing.T) {
	for _, dir := range []string{"internal", "cmd", "core"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			switch {
			case d.IsDir() && (rel == "internal/brand" || rel == "internal/testutil" || d.Name() == "testdata"):
				return filepath.SkipDir
			case d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			skip := map[*ast.BasicLit]bool{}
			for _, is := range f.Imports {
				skip[is.Path] = true
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if fld, ok := n.(*ast.Field); ok && fld.Tag != nil {
					skip[fld.Tag] = true
				}
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || skip[lit] {
					return true
				}
				v, err := strconv.Unquote(lit.Value)
				if err == nil && archNamed(v) {
					t.Errorf("%s:%d: %s names an arch agent; use brand.Mikael, brand.Gabriel, brand.Raphael, brand.Raffi or brand.Raguel, and brand.He, Him and His for him", rel, fset.Position(lit.Pos()).Line, lit.Value)
				}
				if err != nil || !named(v) {
					return true
				}
				for _, k := range keep[rel] {
					if v == k {
						return true
					}
				}
				t.Errorf("%s:%d: %s names the product; use brand.Name, brand.Slug or brand.Env", rel, fset.Position(lit.Pos()).Line, lit.Value)
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestSwiftNamesTheBrand: the Mac app names the product through Brand
// (Brand.swift, written from this package), in every string it shows or
// looks up.
func TestSwiftNamesTheBrand(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(root, "mac", "Sources", "Irie", "*.swift"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no Swift files: %v", err)
	}
	for _, path := range files {
		if filepath.Base(path) == "Brand.swift" {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		runs, err := swiftStrings(string(src))
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
			continue
		}
		for _, r := range runs {
			text := string(src[r[0]:r[1]])
			// A source path, as a fixture's command names it.
			text = strings.ReplaceAll(text, "Sources/Irie/", "")
			if named(text) {
				line := 1 + strings.Count(string(src[:r[0]]), "\n")
				t.Errorf("%s:%d: %q names the product; use \\(Brand.name) or \\(Brand.slug)", filepath.Base(path), line, text)
			}
			if archNamed(text) {
				line := 1 + strings.Count(string(src[:r[0]]), "\n")
				t.Errorf("%s:%d: %q names an arch agent; use \\(Brand.mikael), \\(Brand.gabriel), \\(Brand.raphael), \\(Brand.raffi), \\(Brand.raguel) or \\(Brand.ragu)", filepath.Base(path), line, text)
			}
		}
	}
}

// swiftStrings finds the literal text of every string in a Swift file:
// single-line, multi-line and raw strings, with their interpolations' code
// left out (and the strings inside it found in turn). Comments are skipped.
func swiftStrings(src string) ([][2]int, error) {
	type frame struct {
		str           bool
		quote         string
		hashes, depth int
		run           int
	}
	var out [][2]int
	stack := []frame{{}}
	for i := 0; i < len(src); {
		top := &stack[len(stack)-1]
		if !top.str {
			switch {
			case strings.HasPrefix(src[i:], "//"):
				j := strings.IndexByte(src[i:], '\n')
				if j < 0 {
					return out, nil
				}
				i += j
				continue
			case strings.HasPrefix(src[i:], "/*"):
				depth := 1
				for i += 2; i < len(src) && depth > 0; {
					switch {
					case strings.HasPrefix(src[i:], "/*"):
						depth, i = depth+1, i+2
					case strings.HasPrefix(src[i:], "*/"):
						depth, i = depth-1, i+2
					default:
						i++
					}
				}
				continue
			}
			h := 0
			for i+h < len(src) && src[i+h] == '#' {
				h++
			}
			if q := src[i+h:]; strings.HasPrefix(q, `"`) {
				quote := `"`
				if strings.HasPrefix(q, `"""`) {
					quote = `"""`
				}
				i += h + len(quote)
				stack = append(stack, frame{str: true, quote: quote, hashes: h, run: i})
				continue
			}
			switch src[i] {
			case '(':
				top.depth++
			case ')':
				if top.depth == 0 && len(stack) > 1 {
					stack = stack[:len(stack)-1]
					i++
					stack[len(stack)-1].run = i
					continue
				}
				top.depth--
			}
			i++
			continue
		}
		esc := `\` + strings.Repeat("#", top.hashes)
		end := top.quote + strings.Repeat("#", top.hashes)
		switch {
		case strings.HasPrefix(src[i:], esc+"("):
			out = append(out, [2]int{top.run, i})
			i += len(esc) + 1
			stack = append(stack, frame{})
		case strings.HasPrefix(src[i:], esc):
			i += len(esc) + 1
		case strings.HasPrefix(src[i:], end):
			out = append(out, [2]int{top.run, i})
			i += len(end)
			stack = stack[:len(stack)-1]
		case top.quote == `"` && src[i] == '\n':
			return nil, &os.PathError{Op: "scan", Path: strconv.Itoa(i), Err: os.ErrInvalid}
		default:
			i++
		}
	}
	return out, nil
}
