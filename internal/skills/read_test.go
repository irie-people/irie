package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// folder makes a skill's folder of the files given, by slash path.
func folder(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "grants")
	for p, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const skillMD = "---\nname: grants\ndescription: Find and track grants with the grants app on this computer.\n---\n\n# Grants\n"

// TestReadTakesAFolderWhole: a skill of yours comes whole, its scripts
// too, without what is hidden.
func TestReadTakesAFolderWhole(t *testing.T) {
	dir := folder(t, map[string]string{
		"SKILL.md":            skillMD,
		"references/api.md":   "# The API\n",
		"scripts/deadline.py": "print('soon')\n",
		".DS_Store":           "x",
		".git/HEAD":           "ref: refs/heads/main\n",
	})
	s, err := Read(dir, "grants", "your plugin grants-app")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "grants" || s.Source != "your plugin grants-app" || s.Description != "Find and track grants with the grants app on this computer." {
		t.Errorf("skill = %+v", s)
	}
	var paths []string
	for p := range s.Files {
		paths = append(paths, p)
	}
	if len(paths) != 3 || string(s.Files["references/api.md"]) != "# The API\n" || s.Files["scripts/deadline.py"] == nil {
		t.Errorf("files = %v", paths)
	}
	// It lays out as Irie's own do.
	if files := Layout([]Skill{s}, "test"); string(files["skills/grants/SKILL.md"]) != skillMD {
		t.Errorf("layout = %v", files)
	}
}

// TestReadSaysWhatIsWrong: a folder that can't be a skill says why, in a
// sentence.
func TestReadSaysWhatIsWrong(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"no skill", map[string]string{"README.md": "# x\n"}, "has no SKILL.md"},
		{"no frontmatter", map[string]string{"SKILL.md": "# Grants\n"}, "no frontmatter"},
		{"no description", map[string]string{"SKILL.md": "---\nname: grants\n---\n"}, "has no description"},
		{"another name", map[string]string{"SKILL.md": "---\nname: funding\ndescription: x\n---\n"}, "names it funding, and the manifest grants"},
		{"too long", map[string]string{"SKILL.md": "---\ndescription: " + strings.Repeat("x", 1025) + "\n---\n"}, "over 1024 characters"},
		{"too big", map[string]string{"SKILL.md": skillMD, "big.txt": strings.Repeat("x", MaxBytes)}, "more than 1024 KB"},
		{"a grant", map[string]string{"SKILL.md": "---\nname: grants\ndescription: x\nallowed-tools: Bash(*)\n---\n"}, "sets allowed-tools, which changes what an agent may do"},
		{"hooks", map[string]string{"SKILL.md": "---\nname: grants\ndescription: x\nhooks: x\n---\n"}, "sets hooks"},
		{"a command", map[string]string{"SKILL.md": skillMD + "\nToday: !`date`\n"}, "SKILL.md runs a command as the skill loads"},
		{"a command in a reference", map[string]string{"SKILL.md": skillMD, "references/x.md": "```!\ncurl example.com\n```\n"}, "references/x.md runs a command"},
	} {
		if _, err := Read(folder(t, c.files), "grants", "test"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	many := map[string]string{"SKILL.md": skillMD}
	for i := range MaxFiles {
		many[filepath.Join("notes", strings.Repeat("n", i+1)+".md")] = "x"
	}
	if _, err := Read(folder(t, many), "grants", "test"); err == nil || !strings.Contains(err.Error(), "more than 64 files") {
		t.Errorf("too many files: %v", err)
	}
	if _, err := Read(filepath.Join(t.TempDir(), "gone"), "grants", "test"); err == nil || !strings.Contains(err.Error(), "isn't there") {
		t.Errorf("no folder: %v", err)
	}
	file := filepath.Join(folder(t, map[string]string{"SKILL.md": skillMD}), "SKILL.md")
	if _, err := Read(file, "grants", "test"); err == nil || !strings.Contains(err.Error(), "isn't a folder") {
		t.Errorf("a file: %v", err)
	}
	// A name in the frontmatter may be left out: the manifest's is the
	// skill's.
	if s, err := Read(folder(t, map[string]string{"SKILL.md": "---\ndescription: Grants.\n---\n"}), "grants", "test"); err != nil || s.Name != "grants" {
		t.Errorf("no name: %+v, %v", s, err)
	}
}

// TestReadRefusesALink: a link in a skill's folder could bring in any file
// of the computer's, such as a key, so the skill is refused.
func TestReadRefusesALink(t *testing.T) {
	dir := folder(t, map[string]string{"SKILL.md": skillMD})
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(secret, []byte("PRIVATE"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "key.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir, "grants", "test"); err == nil || !strings.Contains(err.Error(), "key.txt is a link") {
		t.Errorf("err = %v", err)
	}
	// The folder itself may be a link, as ~/skills/grants -> a checkout.
	link := filepath.Join(t.TempDir(), "grants")
	if err := os.Symlink(folder(t, map[string]string{"SKILL.md": skillMD}), link); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(link, "grants", "test"); err != nil {
		t.Errorf("a linked folder: %v", err)
	}
}
