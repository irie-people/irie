package designcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/irie-people/design"
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
	roles := design.Roles()
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
	rw, _ := design.PaletteByID("rosewater")
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
	rw, _ := design.PaletteByID("rosewater")
	if got := design.Mix(rw.Light["needs"], rw.Light["needs_tint"], 0.45); got != "#C98D82" {
		t.Errorf("Mix = %s, want #C98D82", got)
	}
}
