package design

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// root is the repository, which holds the copies.
const root = ".."

// TestCopiesMatchTheLibrary: every file that keeps some of the library's
// values keeps the library's, and the generated files are the ones the
// library makes now.
func TestCopiesMatchTheLibrary(t *testing.T) {
	probs := Check(root)
	for _, p := range probs {
		t.Error(p)
	}
	if len(probs) > 0 {
		t.Log("tools/design.sh sync writes the library's values into the copies")
	}
}

// TestRolesMatchTheMacApp: the roles are Make a Theme's, with its titles
// and groups (mac/Sources/Irie/Looks.swift, ColorRole.all).
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

// TestEveryPaletteIsWhole: each palette has every role in both modes, and
// the palettes are the ones tokens.json lists, in its order.
func TestEveryPaletteIsWhole(t *testing.T) {
	var ids []string
	for _, p := range Palettes() {
		ids = append(ids, p.ID)
		for _, dark := range []bool{false, true} {
			if got := len(p.Mode(dark)); got != len(roles) {
				t.Errorf("%s: %d colours in a mode, want %d", p.ID, got, len(roles))
			}
		}
	}
	if !slices.Equal(ids, IDs()) {
		t.Errorf("palettes %v, tokens.json lists %v", ids, IDs())
	}
	if _, ok := PaletteByID(Default()); !ok {
		t.Errorf("no default palette %q", Default())
	}
}

// TestContrastIsWCAGs: the contrast ratio of the colours WCAG's own
// examples use.
func TestContrastIsWCAGs(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want float64
	}{{"#FFFFFF", "#000000", 21}, {"#777777", "#FFFFFF", 4.48}, {"#595959", "#FFFFFF", 7.0}} {
		if got := Contrast(c.a, c.b); got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("Contrast(%s, %s) = %.3f, want %.2f", c.a, c.b, got, c.want)
		}
	}
}

// TestSyncPutsTheLibraryBack: a copy whose values were changed by hand
// gets the library's back, and nothing else in it changes.
func TestSyncPutsTheLibraryBack(t *testing.T) {
	tmp := t.TempDir()
	want := map[string][]byte{}
	for _, c := range Copies {
		src, err := os.ReadFile(filepath.Join(root, c.Path))
		if err != nil {
			t.Fatal(err)
		}
		want[c.Path] = src
		// Every colour a digit off, and every agents' number 1 more.
		edits, probs := scan(c, src)
		if len(probs) > 0 {
			t.Fatalf("%s: %v", c.Path, probs)
		}
		out := slices.Clone(src)
		for _, e := range edits {
			if e.colour {
				out[e.start] = map[bool]byte{true: '1', false: '0'}[out[e.start] == '0']
			} else {
				out[e.start] = map[bool]byte{true: '1', false: '2'}[out[e.start] != '1']
			}
		}
		path := filepath.Join(tmp, c.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(tmp, "design", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, ThemesPage, tmp, nil)
	if probs := Check(tmp); len(probs) == 0 {
		t.Fatal("Check finds nothing wrong with copies changed by hand")
	}
	changed, probs := Sync(tmp)
	if len(probs) > 0 {
		t.Fatalf("Sync: %v", probs)
	}
	if len(changed) != len(Copies)+len(generated(tmp)) {
		t.Errorf("Sync changed %v", changed)
	}
	for path, src := range want {
		got, err := os.ReadFile(filepath.Join(tmp, path))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(src) {
			t.Errorf("%s is not as it was after Sync", path)
		}
	}
	if probs := Check(tmp); len(probs) > 0 {
		t.Errorf("Check after Sync: %v", probs)
	}
}

// TestScanReadsABlock: a block's names in each spelling, its own names,
// its mode keys, and what it cannot read.
func TestScanReadsABlock(t *testing.T) {
	ev, _ := PaletteByID("evergreen")
	src := strings.Join([]string{
		"/* design: evergreen light page=sidebar */",
		"--page: " + ev.Light["sidebar"] + "; --needs-tint: #000000;",
		"/* design: end */",
		"// design: evergreen",
		"light: Swatches(needsTint: 0x" + ev.Light["needs_tint"][1:] + ",",
		"dark: [\"accent_ink\": 0x" + ev.Dark["accent_ink"][1:] + "]",
		"// design: end",
		"<!-- design: terminal termInk=ink -->",
		"termInk = fixed(0x" + TheTerminal().Ink[1:] + ")",
		"<!-- design: end -->",
		"",
	}, "\n")
	c := Copy{Path: "x", Blocks: []string{"evergreen light", "evergreen", "terminal"}}
	edits, probs := scan(c, []byte(src))
	if len(probs) > 0 {
		t.Fatal(probs)
	}
	var wrong []string
	for _, e := range edits {
		if e.differs() {
			wrong = append(wrong, e.what)
		}
	}
	if len(edits) != 5 || !slices.Equal(wrong, []string{"evergreen light needs_tint"}) {
		t.Errorf("%d values, %v not the library's; want 5, [evergreen light needs_tint]", len(edits), wrong)
	}

	for _, bad := range []struct{ src, want string }{
		{"/* design: evergreen light */\n--bar: #183F30;\n/* design: end */\n", "--bar is no colour"},
		{"/* design: evergreen light */\n--ink: #1E2B22;\n", "no design: end"},
		{"/* design: evergreen */\n--ink: #1E2B22;\n/* design: end */\n", "before a light or dark key"},
		{"/* design: solarized light */\n/* design: end */\n", "names no palette"},
		{"/* design: evergreen light */\n/* design: end */\n", "has no colours"},
	} {
		_, probs := scan(Copy{Path: "x", Blocks: []string{"evergreen light"}}, []byte(bad.src))
		if !slices.ContainsFunc(probs, func(p Problem) bool { return strings.Contains(p.What, bad.want) }) {
			t.Errorf("%q: problems %v, want one saying %q", bad.src, probs, bad.want)
		}
	}
}

// copyFile copies the repository's file path under dir, changed by edit.
func copyFile(t *testing.T, path, dir string, edit func(string) string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	out := string(src)
	if edit != nil {
		out = edit(out)
	}
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(path)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, path), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTheThemesPageIsChecked: an old colour in a table row is found and
// written again; an old colour in a picture is found and left for a new
// drawing.
func TestTheThemesPageIsChecked(t *testing.T) {
	rw, _ := PaletteByID("rosewater")
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
	var table1, picture1 bool
	for _, p := range probs {
		table1 = table1 || strings.Contains(p.What, "light needs is "+oldNeeds)
		picture1 = picture1 || strings.Contains(p.What, "has #ABCDEF")
	}
	if !table1 || !picture1 {
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

// TestMixIsThePicturesMix: the needs card's edge in Rosewater's light
// picture, 45% needs over its tint.
func TestMixIsThePicturesMix(t *testing.T) {
	rw, _ := PaletteByID("rosewater")
	if got := Mix(rw.Light["needs"], rw.Light["needs_tint"], 0.45); got != "#C98D82" {
		t.Errorf("Mix = %s, want #C98D82", got)
	}
}
