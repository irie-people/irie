package main

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// smallCheckout is a checkout in small that leaves Privacy and Mail out: a
// command, the packages it needs and those only a plugin's files need, a
// skill's files embedded, and the Mac app's Swift.
var smallCheckout = map[string]string{
	"go.mod":                  "module example.com/m\n\ngo 1.26\n\nignore ./mac\n",
	"internal/brand/brand.go": "package brand\n\n// Tags leave Privacy and Mail out.\nconst Tags = \"noprivacy,nomail\"\n",
	"cmd/app/main.go": "package main\n\nimport (\n\t\"example.com/m/internal/a\"\n\t\"example.com/m/internal/brand\"\n\t\"example.com/m/internal/skills\"\n)\n\n" +
		"func main() { println(brand.Tags, a.A, len(skills.Names())) }\n",
	"cmd/app/privacy.go":     "//go:build !noprivacy\n\npackage main\n\nimport \"example.com/m/internal/b\"\n\nvar _ = b.B\n",
	"cmd/app/privacy_off.go": "//go:build noprivacy\n\npackage main\n\n// privacyOff stands in for privacy.go.\nvar privacyOff = true\n",
	"cmd/app/vpn.go":         "//go:build !novpn && nomail\n\npackage main\n\nvar vpn = true\n",
	// Built for Windows alone, yet what it imports stays.
	"cmd/app/win_windows.go":         "package main\n\nimport \"example.com/m/internal/e\"\n\nvar _ = e.E\n",
	"internal/a/a.go":                "package a\n\nconst A = 1\n",
	"internal/a/a_test.go":           "package a\n\nimport (\n\t\"testing\"\n\n\t\"example.com/m/internal/c\"\n)\n\nfunc TestA(t *testing.T) { _ = c.C }\n",
	"internal/b/b.go":                "package b\n\nconst B = 1\n",
	"internal/b/testdata/b.txt":      "data\n",
	"internal/c/c.go":                "package c\n\nconst C = 1\n",
	"internal/d/d.go":                "package d\n\nconst D = 1\n",
	"internal/e/e.go":                "package e\n\nconst E = 1\n",
	"internal/f/f.go":                "//go:build !nomail\n\npackage f\n",
	"internal/f/README.md":           "Mail's own.\n",
	"internal/testutil/fake/main.go": "package main\n\nfunc main() {}\n",
	"internal/skills/skills.go": "package skills\n\nimport \"embed\"\n\n//go:embed kept shared.md\nvar kept embed.FS\n\n" +
		"// Names are the skills.\nfunc Names() []string { return []string{\"kept\"} }\n",
	"internal/skills/privacy.go":     "//go:build !noprivacy\n\npackage skills\n\nimport \"embed\"\n\n//go:embed gone shared.md\nvar gone embed.FS\n",
	"internal/skills/kept/SKILL.md":  "kept\n",
	"internal/skills/gone/SKILL.md":  "gone\n",
	"internal/skills/gone/.DS_Store": "\n",
	"internal/skills/shared.md":      "shared\n",
	"mac/Sources/App/Mail.swift":     "#if !NOMAIL\nimport SwiftUI\n\nstruct Mail {}\n#endif\n",
	"mac/Sources/App/Store.swift":    "struct Store {\n    #if !NOMAIL\n    var mail = 1\n    #endif\n    var vpn = 2\n}\n",
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for f, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// readFiles is every file under dir, by path relative to it.
func readFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestRunRefusesACheckoutWithoutTags(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod":                  "module example.com/m\n\ngo 1.26\n",
		"internal/brand/brand.go": "package brand\n\nconst (\n\tName = \"Irie\"\n\tTags = \"\"\n)\n",
	})
	err := run(dir, true, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "Tags in ") || !strings.Contains(err.Error(), "is empty") {
		t.Errorf("error %v, want Tags refused for being empty", err)
	}
}

func TestRunPrunesACheckout(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list, go mod tidy, go build and go vet")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	dir := t.TempDir()
	writeFiles(t, dir, smallCheckout)

	var out, goOut bytes.Buffer
	if err := run(dir, true, &out, &goOut); err != nil {
		t.Fatalf("-n: %v\n%s", err, out.Bytes())
	}
	if got := readFiles(t, dir); !reflect.DeepEqual(got, smallCheckout) {
		t.Fatalf("-n changed the checkout:\n%s", out.Bytes())
	}
	for _, line := range []string{
		"delete  cmd/app/privacy.go (//go:build !noprivacy)",
		"rewrite cmd/app/vpn.go: //go:build !novpn && nomail becomes //go:build !novpn",
		"delete  internal/b/ (2 files)",
		"delete  internal/skills/gone/ (2 files), embedded by internal/skills/privacy.go",
		"delete  mac/Sources/App/Mail.swift: no code left",
		"-n: nothing changed",
	} {
		if !strings.Contains(out.String(), line+"\n") {
			t.Errorf("-n does not say %q:\n%s", line, out.Bytes())
		}
	}

	out.Reset()
	if err := run(dir, false, &out, &goOut); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out.Bytes(), goOut.Bytes())
	}
	want := map[string]string{}
	for f, src := range smallCheckout {
		want[f] = src
	}
	for _, f := range []string{
		"cmd/app/privacy.go", "internal/skills/privacy.go",
		"internal/b/b.go", "internal/b/testdata/b.txt", "internal/d/d.go", "internal/f/f.go", "internal/f/README.md",
		"internal/skills/gone/SKILL.md", "internal/skills/gone/.DS_Store",
		"mac/Sources/App/Mail.swift",
	} {
		delete(want, f)
	}
	want["cmd/app/privacy_off.go"] = "package main\n\n// privacyOff stands in for privacy.go.\nvar privacyOff = true\n"
	want["cmd/app/vpn.go"] = "//go:build !novpn\n\npackage main\n\nvar vpn = true\n"
	want["mac/Sources/App/Store.swift"] = "struct Store {\n    var vpn = 2\n}\n"
	got := readFiles(t, dir)
	for f := range got {
		if _, ok := want[f]; !ok {
			t.Errorf("%s is left", f)
		}
	}
	for f, src := range want {
		if got[f] != src {
			t.Errorf("%s: got %q, want %q", f, got[f], src)
		}
	}
	for _, d := range []string{"internal/b", "internal/d", "internal/f", "internal/skills/gone"} {
		if _, err := os.Stat(filepath.Join(dir, d)); err == nil {
			t.Errorf("%s/ is left", d)
		}
	}
}

func TestRunFailsLoudlyWhenThePrunedTreeDoesNotBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list, go mod tidy and go build")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	dir := t.TempDir()
	// main.go uses what only the privacy plugin's file declares.
	writeFiles(t, dir, map[string]string{
		"go.mod":                  "module example.com/m\n\ngo 1.26\n",
		"internal/brand/brand.go": "package brand\n\nconst Tags = \"noprivacy\"\n",
		"cmd/app/main.go":         "package main\n\nfunc main() { println(privacyOn) }\n",
		"cmd/app/privacy.go":      "//go:build !noprivacy\n\npackage main\n\nconst privacyOn = true\n",
	})
	var out, goOut bytes.Buffer
	err := run(dir, false, &out, &goOut)
	if err == nil || !strings.Contains(err.Error(), "go build ./... failed after pruning") {
		t.Errorf("error %v, want go build's failure\n%s", err, goOut.Bytes())
	}
	if !strings.Contains(goOut.String(), "undefined: privacyOn") {
		t.Errorf("go build's own words are not passed on:\n%s", goOut.Bytes())
	}
}
