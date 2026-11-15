package brand

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// root is the repository's top, from this package's folder.
const root = "../.."

func TestNamesAreWellFormed(t *testing.T) {
	if !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(Slug) {
		t.Errorf("Slug = %q: want lower-case letters, digits and hyphens, starting with a letter", Slug)
	}
	// Only letters, digits, spaces, hyphens and full stops: the name goes
	// into shell scripts, tmux.conf, XML, HTML and format strings as it is.
	if !regexp.MustCompile(`^[\p{L}\p{N}]([\p{L}\p{N}.-]| [\p{L}\p{N}])*$`).MatchString(Name) || utf8.RuneCountInString(Name) > MaxName {
		t.Errorf("Name = %q: want up to %d letters, digits, spaces, hyphens and full stops, starting with a letter or digit, one space between words", Name, MaxName)
	}
	if Exe == "" || len(Exe) > 15 {
		t.Errorf("Exe = %q: want 1 to 15 ASCII letters and digits", Exe)
	}
	if Bar != "cheer" && Bar != "tips" {
		t.Errorf("Bar = %q: want cheer or tips", Bar)
	}
	for _, tag := range strings.Split(Tags, ",") {
		if tag != "" && tag != "novpn" && tag != "noprivacy" && tag != "nowriting" && tag != "nomarkets" && tag != "nomail" && tag != "nobitwarden" {
			t.Errorf("Tags = %q: %q is no tag of Irie's; they are novpn, noprivacy, nowriting, nomarkets, nomail and nobitwarden", Tags, tag)
		}
	}
	if want := strings.ToUpper(strings.ReplaceAll(Slug, "-", "_")); EnvPrefix != want {
		t.Errorf("EnvPrefix = %q: want %q, Slug in capitals with underscores for hyphens", EnvPrefix, want)
	}
	if !strings.HasSuffix(Site, "/") {
		t.Errorf("Site = %q: want a slash at the end", Site)
	}
	if got, want := Env("SESSION"), strings.ToUpper(strings.ReplaceAll(Slug, "-", "_"))+"_SESSION"; got != want {
		t.Errorf("Env(SESSION) = %q, want %q", got, want)
	}
	if App != Name+".app" {
		t.Errorf("App = %q", App)
	}
}

// TestScriptAgrees: the build scripts read the same names through
// tools/brand.sh.
func TestScriptAgrees(t *testing.T) {
	out, err := exec.Command("sh", filepath.Join(root, "tools", "brand.sh")).Output()
	if err != nil {
		t.Fatalf("tools/brand.sh: %v", err)
	}
	want := map[string]string{
		"BRAND_NAME": Name, "BRAND_SLUG": Slug, "BRAND_ENV": EnvPrefix,
		"BRAND_BUNDLE_ID": BundleID, "BRAND_SITE": Site, "BRAND_REPO": Repo, "BRAND_TAGLINE": Tagline,
		"BRAND_BAR": Bar, "BRAND_EXE": Exe, "BRAND_TAGS": Tags,
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = strings.Trim(v, "'")
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("tools/brand.sh says %s=%q, brand.go %q", k, got[k], v)
		}
	}
}

// TestSwiftIsCurrent: the Mac app's Brand.swift is the one tools/brand.sh
// writes from this package, so the app and the core never differ.
func TestSwiftIsCurrent(t *testing.T) {
	want, err := exec.Command("sh", filepath.Join(root, "tools", "brand.sh"), "--swift").Output()
	if err != nil {
		t.Fatalf("tools/brand.sh --swift: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "mac", "Sources", "Irie", "Brand.swift"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("mac/Sources/Irie/Brand.swift is behind internal/brand/brand.go: run tools/brand.sh --swift >mac/Sources/Irie/Brand.swift")
	}
}

// TestExe: a long name, or one with spaces or letters outside ASCII, still
// makes an executable pgrep -x finds.
func TestExe(t *testing.T) {
	for _, c := range []struct{ name, slug, want string }{
		{"Irie", "irie", "Irie"},
		{"Acme Agent Workbench Pro", "acme", "AcmeAgentWorkbe"},
		{"Ä.B-c 9", "abc", "Bc9"},
		{"Ñandú", "nandu", "and"},
		{"東京", "tokyo", "tokyo"},
	} {
		if got := exe(c.name, c.slug); got != c.want {
			t.Errorf("exe(%q, %q) = %q, want %q", c.name, c.slug, got, c.want)
		}
	}
}
