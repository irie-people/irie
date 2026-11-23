package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// tree makes the files under a temporary folder and returns its repo.
func tree(t *testing.T, files ...string) *repo {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return newRepo(root)
}

func paths(fs []finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.path)
	}
	return out
}

func TestScanFindsThePathsThatAreGone(t *testing.T) {
	r := tree(t, "internal/core/dog.go", "tools/check.sh", "mac/build.sh")
	text := "The dog lives in `internal/core/dog.go`, checked by `tools/check.sh`.\n" +
		"| `internal/mirror/` | gone | M8 |\n" +
		"`mac/`, `mac/build.sh`, and `mac/nothing.sh`;\n" +
		"Words that aren't paths: `irie ctl list`, `core`, `ui.bar`, `v1.2.3`.\n"
	got := scan("docs/X.md", text, r.exists)
	want := []finding{{"docs/X.md", 2, "internal/mirror/"}, {"docs/X.md", 3, "mac/nothing.sh"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scan = %v, want %v", got, want)
	}
	if s := got[0].String(); s != "docs/X.md:2: `internal/mirror/` names nothing in the repository" {
		t.Errorf("message %q", s)
	}
}

func TestMarkedAndPlaceholderPathsAreLeftAlone(t *testing.T) {
	r := tree(t)
	text := "For example, `internal/foo/bar.go` would hold it; e.g. `cmd/x`.\n" +
		"| `e2e/` (planned) | tests | M11 |\n" +
		"In `e2e/`(planned, M11), and `internal/<name>/`, `tools/$TOOL`, `~/x`, `internal/a//b`.\n" +
		"An address, `cmd/irie@v1`, a version range, `internal/a..b`, and an ellipsis, `internal/…`.\n"
	if got := scan("d.md", text, r.exists); len(got) != 0 {
		t.Errorf("scan = %v, want nothing", got)
	}
}

func TestAPathMayEndInALineAHeadingOrAPattern(t *testing.T) {
	r := tree(t, "internal/core/pane.go", "docs/MAC.md", "internal/testutil/x.go")
	text := "`internal/core/pane.go:12-20`, `internal/core/pane.go:7`, `internal/core/pane.go:3–9`,\n" +
		"`docs/MAC.md#code-cards`, `./internal/testutil/...`, `internal/core/pane.go`.\n" +
		"`internal/core/gone.go:12`, `docs/GONE.md#x`, `./internal/gone/...`\n"
	got := paths(scan("d.md", text, r.exists))
	want := []string{"internal/core/gone.go", "docs/GONE.md", "internal/gone/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scan = %v, want %v", got, want)
	}
}

func TestGlobsAndChoicesMustEachMatch(t *testing.T) {
	r := tree(t, "internal/core/vpn_off.go", "site/media/intro.webm", "site/media/intro.mp4")
	for p, want := range map[string]bool{
		"internal/core/*_off.go":         true,
		"internal/*/vpn_off.go":          true,
		"internal/core/*_on.go":          false,
		"site/media/intro.{webm,mp4}":    true,
		"site/media/intro.{webm,mp4,vt}": false,
		"internal/core/":                 true,
	} {
		if got := r.exists(p); got != want {
			t.Errorf("exists(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestNamesMatchInCase(t *testing.T) {
	r := tree(t, "docs/DESIGN.md")
	if !r.exists("docs/DESIGN.md") || r.exists("docs/design.md") || r.exists("Docs/DESIGN.md") {
		t.Error("a name must match in case, as on Linux, though this disk may ignore it")
	}
}

func TestCodeBlocksAreNotRead(t *testing.T) {
	r := tree(t)
	text := "```sh\nx=`internal/gone/a.sh`\n```\n" +
		"~~~~\n```\n`internal/gone/b.sh`\n~~~~\n" +
		"  ```\n  `internal/gone/c.sh`\n  ```\n" +
		"`internal/gone/d.sh`\n"
	if got := paths(scan("d.md", text, r.exists)); !reflect.DeepEqual(got, []string{"internal/gone/d.sh"}) {
		t.Errorf("scan = %v, want only the path outside the code blocks", got)
	}
}

func TestTheDocsAreDocsAndTheTopsGuides(t *testing.T) {
	r := tree(t, "docs/DESIGN.md", "docs/proposals/plan.md", "AGENTS.md", "README.md", "PROVENANCE.md", "CHANGELOG.md")
	got, err := docs(r.root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"docs/DESIGN.md", "AGENTS.md", "README.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("docs = %v, want %v: the records of the past and the plans stay out", got, want)
	}
}

// TestRootsAreTheTopFolders: every folder git keeps at the top is a root a
// doc's path may start with, so a new one is checked from the start.
func TestRootsAreTheTopFolders(t *testing.T) {
	out, err := exec.Command("git", "-C", "../..", "ls-tree", "-d", "--name-only", "HEAD").Output()
	if err != nil {
		t.Skip("no git history here:", err)
	}
	top := strings.Fields(string(out))
	want := slices.DeleteFunc(slices.Clone(roots), func(r string) bool { return r == "e2e" })
	if !reflect.DeepEqual(top, want) {
		t.Errorf("the top folders are %v; roots, but e2e/, which DESIGN plans, are %v", top, want)
	}
}

func TestBuildsOutputIsNotChecked(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("/mac/build/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	found := []finding{{"docs/MAC.md", 1, "mac/build/Irie.app"}, {"docs/MAC.md", 2, "mac/gone.sh"}}
	if got := paths(unignored(found)); !reflect.DeepEqual(got, []string{"mac/gone.sh"}) {
		t.Errorf("unignored = %v, want only the path git keeps", got)
	}
}

// TestACopyPassesOverWhatItsHistoryDeleted: a copy whose brand leaves
// plugins out keeps Irie's docs, which name the code tools/prune took out
// of its source, so there a path the checkout's history deleted, a file, a
// folder of such files or a glob, is no finding, and any other still is.
// Irie's own brand leaves nothing out and gets no such pass.
func TestACopyPassesOverWhatItsHistoryDeleted(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=alice", "-c", "user.email=alice@example.com", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	write := func(name, text string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	brand := func(tags string) string {
		return "package brand\n\nconst (\n\tName = \"Basil\"\n\tTags = \"" + tags + "\"\n)\n"
	}
	write("internal/brand/brand.go", brand(""))
	write("internal/plug/plug.go", "package plug\n")
	write("internal/core/plug_on.go", "package core\n")
	write("internal/core/core.go", "package core\n")
	run("init", "-q")
	run("add", "-A")
	run("commit", "-q", "-m", "feat: a plugin")
	if leavesPluginsOut(dir) {
		t.Error("a brand with no Tags leaves plugins out")
	}

	write("internal/brand/brand.go", brand("noplug"))
	run("rm", "-q", "-r", "internal/plug", "internal/core/plug_on.go")
	run("commit", "-q", "-a", "-m", "chore(copy): take the plugin out")
	if !leavesPluginsOut(dir) {
		t.Error("a brand with Tags leaves no plugins out")
	}
	found := []finding{
		{"docs/DESIGN.md", 1, "internal/plug"},
		{"docs/DESIGN.md", 2, "internal/plug/"},
		{"docs/DESIGN.md", 3, "internal/plug/plug.go"},
		{"docs/DESIGN.md", 4, "internal/core/plug_on.go"},
		{"docs/DESIGN.md", 5, "internal/core/*_on.go"},
		{"docs/DESIGN.md", 6, "internal/never"},
		{"docs/DESIGN.md", 7, "internal/core/gone.go"},
	}
	if got := paths(undeleted(dir, found)); !reflect.DeepEqual(got, []string{"internal/never", "internal/core/gone.go"}) {
		t.Errorf("undeleted = %v, want only the paths no commit deleted", got)
	}
	if leavesPluginsOut(t.TempDir()) {
		t.Error("a checkout with no brand leaves plugins out")
	}
}
