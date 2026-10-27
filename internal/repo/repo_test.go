package repo

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// mkrepo makes a directory that looks like a git checkout: one holding .git.
func mkrepo(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// tree builds a home whose code root holds:
//
//	website             a repo
//	api                 a repo
//	linked              a checkout whose .git is a file, as in a linked worktree
//	clients/acme        a repo two levels down
//	clients/api         another api, two levels down
//	notes               a plain directory
//	.hidden             a repo in a hidden directory
//	deep/a/b            a repo three levels down
//	website/vendor/lib  a repo inside a repo
func tree(t *testing.T) (string, Options) {
	t.Helper()
	home := t.TempDir()
	code := filepath.Join(home, "code")
	for _, r := range []string{"website", "api", "clients/acme", "clients/api", ".hidden", "deep/a/b", "website/vendor/lib"} {
		mkrepo(t, filepath.Join(code, r))
	}
	mkdir(t, filepath.Join(code, "linked"))
	if err := os.WriteFile(filepath.Join(code, "linked", ".git"), []byte("gitdir: /home/alice/code/website/.git/worktrees/linked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mkdir(t, filepath.Join(code, "notes"))
	return home, Options{Roots: []string{"~/code"}, Aliases: map[string]string{}, Home: home}
}

func TestRootsAreScannedTwoLevelsDeep(t *testing.T) {
	home, o := tree(t)
	code := filepath.Join(home, "code")
	for name, want := range map[string]string{
		"website":      filepath.Join(code, "website"),
		"linked":       filepath.Join(code, "linked"),
		"acme":         filepath.Join(code, "clients", "acme"),
		"clients/acme": filepath.Join(code, "clients", "acme"),
		"clients/api":  filepath.Join(code, "clients", "api"),
		"WebSite":      filepath.Join(code, "website"),
	} {
		if got, err := Find(name, o); err != nil || got != want {
			t.Errorf("Find(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, name := range []string{"b", "lib", "hidden", ".hidden", "notes", "deep", "clients"} {
		var nf *NotFoundError
		if got, err := Find(name, o); !errors.As(err, &nf) {
			t.Errorf("Find(%q) = %q, %v; want not found", name, got, err)
		}
	}
}

func TestAmbiguousNamesListTheirPaths(t *testing.T) {
	_, o := tree(t)
	_, err := Find("api", o)
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("Find(api) = %v, want ambiguous", err)
	}
	for _, want := range []string{"@api", "~/code/api", "~/code/clients/api"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
}

func TestAliasesComeFirst(t *testing.T) {
	home, o := tree(t)
	o.Aliases = map[string]string{"api": "~/code/clients/api", "Web": "/home/alice/elsewhere/web"}
	// An alias answers even for a name the roots hold twice.
	if got, err := Find("api", o); err != nil || got != filepath.Join(home, "code", "clients", "api") {
		t.Errorf("Find(api) = %q, %v", got, err)
	}
	if got, err := Find("web", o); err != nil || got != "/home/alice/elsewhere/web" {
		t.Errorf("Find(web) = %q, %v", got, err)
	}
}

func TestRecentReposComeLast(t *testing.T) {
	home, o := tree(t)
	tools := mkrepo(t, filepath.Join(home, "elsewhere", "tools"))
	website := mkrepo(t, filepath.Join(home, "elsewhere", "website"))
	gone := filepath.Join(home, "elsewhere", "gone")
	o.Recent = []string{tools, website, gone}
	if got, err := Find("tools", o); err != nil || got != tools {
		t.Errorf("Find(tools) = %q, %v", got, err)
	}
	// The roots answer before the recent repos do.
	if got, err := Find("website", o); err != nil || got != filepath.Join(home, "code", "website") {
		t.Errorf("Find(website) = %q, %v", got, err)
	}
	// A recent repo that has gone is not found.
	if got, err := Find("gone", o); err == nil {
		t.Errorf("Find(gone) = %q", got)
	}
}

func TestExactCaseWinsOverFoldedCase(t *testing.T) {
	home := t.TempDir()
	mkrepo(t, filepath.Join(home, "a", "Tool"))
	mkrepo(t, filepath.Join(home, "b", "tool"))
	o := Options{Roots: []string{filepath.Join(home, "a"), filepath.Join(home, "b")}, Home: home}
	if got, err := Find("tool", o); err != nil || got != filepath.Join(home, "b", "tool") {
		t.Errorf("Find(tool) = %q, %v", got, err)
	}
	if got, err := Find("Tool", o); err != nil || got != filepath.Join(home, "a", "Tool") {
		t.Errorf("Find(Tool) = %q, %v", got, err)
	}
	var amb *AmbiguousError
	if _, err := Find("TOOL", o); !errors.As(err, &amb) {
		t.Errorf("Find(TOOL) = %v, want ambiguous", err)
	}
}

func TestUnknownNamesGetTheClosest(t *testing.T) {
	_, o := tree(t)
	_, err := Find("webiste", o)
	var nf *NotFoundError
	if !errors.As(err, &nf) || len(nf.Closest) == 0 || nf.Closest[0] != "website" || len(nf.Closest) > 3 {
		t.Fatalf("Find(webiste) = %v (%+v)", err, nf)
	}
	if !strings.Contains(err.Error(), "@webiste") || !strings.Contains(err.Error(), "@website") {
		t.Errorf("error %q", err)
	}
	// With no repos known at all, the error says where they come from.
	_, err = Find("website", Options{Home: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "repos.roots") {
		t.Errorf("Find with nothing known = %v", err)
	}
}

func TestPathsAreExpandedNotSearched(t *testing.T) {
	o := Options{Home: "/home/alice"}
	for in, want := range map[string]string{
		"/srv/code/x/": "/srv/code/x",
		"~/code/x":     "/home/alice/code/x",
		"~":            "/home/alice",
	} {
		if got, err := Find(in, o); err != nil || got != want {
			t.Errorf("Find(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "./x", "../x", ".", "~alice/x"} {
		if got, err := Find(in, o); err == nil {
			t.Errorf("Find(%q) = %q, want an error", in, got)
		}
	}
}

func TestCompleteNames(t *testing.T) {
	home, o := tree(t)
	o.Aliases = map[string]string{"web": "~/code/website"}
	o.Recent = []string{mkrepo(t, filepath.Join(home, "elsewhere", "tools"))}
	for prefix, want := range map[string][]string{
		"we":  {"web", "website"},
		"WE":  {"web", "website"},
		"cl":  {"clients/acme", "clients/api"},
		"a":   {"acme", "api"},
		"to":  {"tools"},
		"zzz": nil,
	} {
		if got := Complete(prefix, o); !slices.Equal(got, want) {
			t.Errorf("Complete(%q) = %q, want %q", prefix, got, want)
		}
	}
}

func TestCompletePaths(t *testing.T) {
	home, o := tree(t)
	code := filepath.Join(home, "code")
	if err := os.WriteFile(filepath.Join(code, "README"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for prefix, want := range map[string][]string{
		"~":         {"~/"},
		"~/co":      {"~/code/"},
		"~/code/cl": {"~/code/clients/"},
		"~/code/":   {"~/code/api/", "~/code/clients/", "~/code/deep/", "~/code/linked/", "~/code/notes/", "~/code/website/"},
		"~/code/.h": {"~/code/.hidden/"},
		code + "/w": {code + "/website/"},
		"~/nope/":   nil,
	} {
		if got := Complete(prefix, o); !slices.Equal(got, want) {
			t.Errorf("Complete(%q) = %q, want %q", prefix, got, want)
		}
	}
}
