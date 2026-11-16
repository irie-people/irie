package designcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/roots"
)

// root is Irie's checkout, which holds the copies.
const root = "../.."

// TestCopiesMatchTheLibrary: every file that keeps some of the library's
// values keeps the library's, and the themes page's pictures and tables
// are drawn from it.
func TestCopiesMatchTheLibrary(t *testing.T) {
	probs := Check(root)
	for _, p := range probs {
		t.Error(p)
	}
	if len(probs) > 0 {
		t.Log("tools/design.sh sync writes the library's values into the copies")
	}
}

// TestRolesMatchTheMacApp: the library's roles are Make a Theme's, with
// its titles and groups (mac/Sources/Irie/Looks.swift, ColorRole.all).
func TestRolesMatchTheMacApp(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(root, "mac", "Sources", "Irie", "Looks.swift"))
	if err != nil {
		t.Skip("no mac/Sources/Irie/Looks.swift")
	}
	re := regexp.MustCompile(`\.init\(key: "(\w+)", path: \\\.\w+, title: "([^"]*)", kind: \.(\w+)\)`)
	swift := map[string][2]string{}
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		swift[m[1]] = [2]string{m[2], m[3]}
	}
	roles := roots.Roles()
	if len(swift) != len(roles) {
		t.Errorf("Looks.swift has %d roles, the library %d", len(swift), len(roles))
	}
	for _, r := range roles {
		got, ok := swift[r.Key]
		switch {
		case !ok:
			t.Errorf("Looks.swift has no %s", r.Key)
		case got[0] != r.Title:
			t.Errorf("%s: Looks.swift calls it %q, the library %q", r.Key, got[0], r.Title)
		case got[1] != string(r.Group):
			t.Errorf("%s: Looks.swift's kind is %s, the library's group %s", r.Key, got[1], r.Group)
		}
	}
}

// TestTheThemesPageIsChecked: an old colour in a table row is found and
// written again; an old colour in a picture is found and left for a new
// drawing.
func TestTheThemesPageIsChecked(t *testing.T) {
	rw, _ := roots.PaletteByID("rosewater")
	oldNeeds := "#A23B2E"
	src, err := os.ReadFile(filepath.Join(root, ThemesPage))
	if err != nil {
		t.Fatal(err)
	}
	table := strings.Index(string(src), "<table")
	row := strings.Index(string(src)[table:], "<code>"+rw.Light["needs"]+"</code>")
	if table < 0 || row < 0 {
		t.Fatal("no rosewater table with light needs")
	}
	pic := strings.Index(string(src), `fill="`+rw.Light["card"]+`"`)
	if pic < 0 || pic > table {
		t.Fatal("no rosewater light card in a picture")
	}
	page := string(src)
	at := table + row + len("<code>")
	page = page[:at] + oldNeeds + page[at+7:]
	page = page[:pic+len(`fill="`)] + "#ABCDEF" + page[pic+len(`fill="`)+7:]
	_, probs := themesPage(page)
	var tableFound, pictureFound bool
	for _, p := range probs {
		tableFound = tableFound || strings.Contains(p.What, "light needs is "+oldNeeds)
		pictureFound = pictureFound || strings.Contains(p.What, "has #ABCDEF")
	}
	if !tableFound || !pictureFound {
		t.Fatalf("problems %v, want the table's needs and the picture's #ABCDEF", probs)
	}
	fixed, _ := themesPage(page)
	_, left := themesPage(fixed)
	if len(left) != 1 || !strings.Contains(left[0].What, "#ABCDEF") {
		t.Errorf("after writing the tables: %v, want only the picture's colour", left)
	}
	if strings.Contains(fixed, oldNeeds) {
		t.Error("the table keeps its old needs")
	}
}

// TestTheMixIsThePicturesMix: the needs card's edge in Rosewater's light
// picture is 45% needs over its tint, as the page drew it.
func TestTheMixIsThePicturesMix(t *testing.T) {
	rw, _ := roots.PaletteByID("rosewater")
	if got := roots.Mix(rw.Light["needs"], rw.Light["needs_tint"], 0.45); got != "#C98D82" {
		t.Errorf("Mix = %s, want #C98D82", got)
	}
}

// fnKey finds a shortcut that takes fn: a SwiftUI keyboardShortcut whose
// modifiers have .function, or an AppKit item's mask set with it. It gives
// each one's line.
var fnKey = regexp.MustCompile(`\.keyboardShortcut\([^)]*\.function\b|keyEquivalentModifierMask\s*=[^\n]*\.function\b`)

func fnKeyLines(src string) []int {
	var lines []int
	for _, m := range fnKey.FindAllStringIndex(src, -1) {
		lines = append(lines, strings.Count(src[:m[0]], "\n")+1)
	}
	return lines
}

// TestNoMenuKeyTakesFn: no shortcut in the Mac app takes fn (Roots'
// principles §4.8). AppKit drops fn from an app's own menu keys, so fn⌃Tab would show
// and act as ⌃Tab; KeyRouter catches such a key instead.
func TestNoMenuKeyTakesFn(t *testing.T) {
	planted := "Button(\"Next Space\") {}\n    .keyboardShortcut(.tab,\n        modifiers: [.control, .function])\n" +
		"Button(\"Next\") {}.keyboardShortcut(KeyEquivalent(Character(\"1\")), modifiers: .command)\n" +
		"let tokens: [Token: Color] = [.keyword: violet, .function: blue]\n"
	if got := fnKeyLines(planted); !slices.Equal(got, []int{2}) {
		t.Fatalf("the check finds a key with fn on lines %v of the sample, want [2]", got)
	}
	files, _ := filepath.Glob(filepath.Join(root, "mac", "Sources", "Irie", "*.swift"))
	if len(files) == 0 {
		t.Skip("no mac/Sources/Irie")
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range fnKeyLines(string(src)) {
			t.Errorf("%s:%d: a shortcut with fn, which AppKit drops from an app's menus: catch the key in KeyRouter (IrieApp.swift) and name it in the item's help", filepath.Base(f), n)
		}
	}
}

// shadowColour finds a SwiftUI shadow's colour: what follows `.shadow(color:`
// on its line, up to the radius.
var shadowColour = regexp.MustCompile(`\.shadow\(color:\s*([^\n]*?),\s*radius:`)

var (
	opacityCall = regexp.MustCompile(`\.opacity\([^()]*\)`)
	whiteCall   = regexp.MustCompile(`Color\(white:[^()]*\)`)
	anyCall     = regexp.MustCompile(`\(([^()]*)\)`)
)

// neutralShadow says whether a shadow's colour is a neutral one: the
// palette's shadow, black or nothing, at any opacity, picked by a condition
// or not. Anything else, such as an agent's colour or the accent, is a
// coloured shadow.
func neutralShadow(expr string) bool {
	e := expr
	for prev := ""; prev != e; {
		prev = e
		e = opacityCall.ReplaceAllString(e, "")
	}
	e = whiteCall.ReplaceAllString(e, "Color.black")
	for prev := ""; prev != e; {
		prev = e
		e = anyCall.ReplaceAllString(e, "")
	}
	// The colours a chain of conditions picks among: after each ?, the
	// colour up to its :, and both sides of the last :.
	parts := strings.Split(e, "?")
	var colours []string
	if len(parts) == 1 {
		colours = parts
	}
	for i := 1; i < len(parts); i++ {
		sides := strings.Split(parts[i], ":")
		if i < len(parts)-1 {
			colours = append(colours, sides[0])
		} else {
			colours = append(colours, sides...)
		}
	}
	for _, c := range colours {
		switch strings.Trim(strings.TrimSpace(c), "()") {
		case "Theme.shadow", ".black", "Color.black", ".clear", "Color.clear":
		default:
			return false
		}
	}
	return len(colours) > 0
}

// TestShadowsAreNeutral: no shadow in the Mac app takes a colour (Roots'
// principles §1.10): a shadow is the palette's shadow or black, at whatever
// strength, so depth never doubles as a colour's glow. A shadow whose colour
// runs onto another line is not seen.
func TestShadowsAreNeutral(t *testing.T) {
	for expr, want := range map[string]bool{
		"Theme.shadow":                           true,
		"selected ? Theme.shadow : .clear":       true,
		"on ? Theme.shadow : Color.clear":        true,
		".black.opacity(keys ? 0.22 : 0.14)":     true,
		"Color(white: 0, opacity: 0.2)":          true,
		"keys ? ink.opacity(0.3) : .black":       false,
		"Theme.agent(slot: 2)":                   false,
		"a ? Theme.accent : b ? .black : .clear": false,
		"a ? .black : b ? .clear : Theme.shadow": true,
	} {
		if got := neutralShadow(expr); got != want {
			t.Errorf("neutralShadow(%q) = %v, want %v", expr, got, want)
		}
	}
	files, _ := filepath.Glob(filepath.Join(root, "mac", "Sources", "Irie", "*.swift"))
	if len(files) == 0 {
		t.Skip("no mac/Sources/Irie")
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range shadowColour.FindAllStringSubmatchIndex(string(src), -1) {
			expr := string(src[m[2]:m[3]])
			if !neutralShadow(expr) {
				n := strings.Count(string(src[:m[0]]), "\n") + 1
				t.Errorf("%s:%d: a shadow coloured %q: give it Theme.shadow or black, and show the state with an edge or a fill instead", filepath.Base(f), n, expr)
			}
		}
	}
}
