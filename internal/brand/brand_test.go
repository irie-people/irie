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
		if tag != "" && tag != "novpn" && tag != "noprivacy" && tag != "nowriting" && tag != "nomarkets" && tag != "nomail" && tag != "nobitwarden" && tag != "noterms" && tag != "noextension" && tag != "noyoink" && tag != "nochat" && tag != "noobsidian" && tag != "nofounding" && tag != "nojammin" {
			t.Errorf("Tags = %q: %q is no tag of Irie's; they are novpn, noprivacy, nowriting, nomarkets, nomail, nobitwarden, noterms, noextension, noyoink, nochat, noobsidian, nofounding and nojammin", Tags, tag)
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
	// The script reads brand.go from disk. A test of another name built
	// with go test -overlay compiles a renamed brand.go, which the script
	// can't see; a copy renamed on disk, as CI's, is checked in full.
	src, err := os.ReadFile(filepath.Join(root, "internal", "brand", "brand.go"))
	if err != nil {
		t.Fatal(err)
	}
	if m := regexp.MustCompile(`(?m)^\tName = "([^"]*)"$`).FindSubmatch(src); m == nil || string(m[1]) != Name {
		t.Skipf("built from another brand.go than the one on disk (go test -overlay), named %q", Name)
	}
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

// pesto is text for a copy named Pesto.
func pesto(s string) string { return text(s, "Pesto", "pesto", "PESTO", "https://pesto.example.com/") }

// TestText: a copy's texts send its people and agents to its own command,
// folders, variables and guide, and keep the ids, keys and links that are
// data.
func TestText(t *testing.T) {
	for in, want := range map[string]string{
		"Irie's sidebar":                                       "Pesto's sidebar",
		"run `irie up` first":                                  "run `pesto up` first",
		"type `irie`":                                          "type `pesto`",
		"irie ctl dog lists them":                              "pesto ctl dog lists them",
		"irie chat post acme:#news - <<'POST'":                 "pesto chat post acme:#news - <<'POST'",
		"h('code', { text: 'irie remote pair' })":              "h('code', { text: 'pesto remote pair' })",
		"goes through irie away":                               "goes through pesto away",
		"in `~/.config/irie/config.toml`":                      "in `~/.config/pesto/config.toml`",
		"in `$XDG_STATE_HOME/irie/chain`":                      "in `$XDG_STATE_HOME/pesto/chain`",
		"a branch irie/fix-it":                                 "a branch pesto/fix-it",
		"/irie:out-of-sight":                                   "/pesto:out-of-sight",
		"$IRIE_BIN and IRIE_SESSION":                           "$PESTO_BIN and PESTO_SESSION",
		`url = "https://irie-people.github.io/irie/keys.html"`: `url = "https://pesto.example.com/keys.html"`,
		// Data, and links that are not the guide's, stay.
		`id = "irie.park"`:                     `id = "irie.park"`,
		"localStorage.getItem('irie.task')":    "localStorage.getItem('irie.task')",
		"{ client: 'irie-phone' }":             "{ client: 'irie-phone' }",
		"#{@irie_role}":                        "#{@irie_role}",
		"https://github.com/irie-people/irie":  "https://github.com/irie-people/irie",
		"go get github.com/irie-people/irie/x": "go get github.com/irie-people/irie/x",
		"`irie up` or `go run ./cmd/irie`":     "`pesto up` or `go run ./cmd/irie`",
		"see mac/Sources/Irie/Spaces.swift":    "see mac/Sources/Irie/Spaces.swift",
		"fairies":                              "fairies",
	} {
		if got := pesto(in); got != want {
			t.Errorf("text(%q) = %q, want %q", in, got, want)
		}
	}
	// Irie's own names leave a text as it is.
	for _, s := range []string{"`irie up`, irie ctl, ~/.config/irie/, Irie, IRIE_BIN", guide + "keys.html"} {
		if got := text(s, "Irie", "irie", "IRIE", guide); got != s {
			t.Errorf("Irie's own text(%q) = %q", s, got)
		}
	}
}

// TestTextsTellTheCommand: every text that Text renames, the skills, the
// arch agents' words, the tips and the phone app's pages, names the command
// where Text can tell it from data, so a copy's texts never send its people
// or agents to Irie's.
func TestTextsTellTheCommand(t *testing.T) {
	var files []string
	for _, pat := range []string{
		"internal/skills/bundled/*/*.md", "internal/skills/bundled/*/*/*.md",
		"internal/arch/agents/*.md", "internal/learn/learn.toml",
		"internal/remote/web/*", "internal/remote/web/*/*",
	} {
		m, err := filepath.Glob(filepath.Join(root, pat))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	if len(files) < 10 {
		t.Fatalf("found %d texts: has a folder moved?", len(files))
	}
	for _, path := range files {
		switch filepath.Ext(path) {
		case ".md", ".toml", ".html", ".js", ".css", ".webmanifest", ".json":
		default:
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		rel, _ := filepath.Rel(root, path)
		line := func(i int) int { return 1 + strings.Count(s[:i], "\n") }
		for i := 0; ; i++ {
			j := strings.Index(s[i:], "irie")
			if j < 0 {
				break
			}
			i += j
			starts := i == 0 || !strings.ContainsRune("_.-@abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", rune(s[i-1]))
			after := strings.TrimLeft(s[i+4:], " ")
			switch {
			// A sentence that ends on the command: Text keeps it, as it
			// keeps an id such as irie.park.
			case starts && strings.HasPrefix(s[i+4:], ".") && (i+5 == len(s) || strings.ContainsRune(" \t\r\n'\"`)", rune(s[i+5]))):
				t.Errorf("%s:%d: a sentence ends on irie, which a copy keeps as it would an id: put the command in backquotes", rel, line(i))
			// In a script, irie as a word is a variable, which a copy's
			// rename would break.
			case filepath.Ext(path) == ".js" && commandWord(s, i) && (strings.HasPrefix(after, "=") && !strings.HasPrefix(after, "==") || strings.HasPrefix(after, "(")):
				t.Errorf("%s:%d: irie names something in the script, which a copy's rename would change: call it something else", rel, line(i))
			}
		}
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
