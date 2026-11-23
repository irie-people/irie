package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestSplitEmbedReadsWordsAndQuotes(t *testing.T) {
	got, err := splitEmbed(" a b\t`c d` \"e\\x20f\" all:g")
	want := []string{"a", "b", "c d", "e f", "all:g"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
	for _, bad := range []string{` "open`, " `open", ` "a"b`} {
		if _, err := splitEmbed(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// writeTree writes files, by path relative to dir, each holding its path.
func writeTree(t *testing.T, dir string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveEmbedFindsWhatTheGoCommandEmbeds(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "pkg/assets/x.txt", "pkg/assets/.hidden", "pkg/assets/_draft.txt",
		"pkg/assets/sub/y.txt", "pkg/assets/.dir/z.txt", "pkg/assets/mod/go.mod", "pkg/assets/mod/w.txt",
		"p[1]/a.txt", "p1/a.txt")
	for _, c := range []struct {
		pattern string
		want    []string
	}{
		{"assets", []string{"pkg/assets/sub/y.txt", "pkg/assets/x.txt"}},
		{"all:assets", []string{"pkg/assets/.dir/z.txt", "pkg/assets/.hidden", "pkg/assets/_draft.txt", "pkg/assets/sub/y.txt", "pkg/assets/x.txt"}},
		// A name matched itself is embedded whatever it begins with.
		{"assets/*.txt", []string{"pkg/assets/_draft.txt", "pkg/assets/x.txt"}},
		{"none*", nil},
	} {
		ms, err := resolveEmbed(root, "pkg", c.pattern)
		if err != nil {
			t.Errorf("%s: %v", c.pattern, err)
			continue
		}
		var got []string
		for _, m := range ms {
			got = append(got, m.files...)
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.pattern, got, c.want)
		}
	}
	// The package's own folder is no pattern: p[1] is not p1.
	if ms, err := resolveEmbed(root, "p[1]", "a.txt"); err != nil || len(ms) != 1 || ms[0].path != "p[1]/a.txt" {
		t.Errorf("a.txt in p[1]: %v, %v", ms, err)
	}
	for _, bad := range []string{"../pkg", "/abs", "a//b", "[", "."} {
		if _, err := resolveEmbed(root, "pkg", bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestEmbedsReadsEveryEmbedLine(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "pkg/a.md", "pkg/b/c.md")
	src := []byte("package pkg\n\nimport \"embed\"\n\n//go:embed a.md\nvar a string\n\nvar (\n\t//go:embed b\n\tb embed.FS\n)\n\n// A comment that says //go:embed x embeds nothing.\nconst s = \"//go:embed y\"\n")
	ms, err := embeds(root, "pkg/p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range ms {
		got = append(got, m.path)
	}
	if want := []string{"pkg/a.md", "pkg/b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
