package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// skillName is the Agent Skills spec's name: lowercase letters, digits and
// hyphens, at most 64.
var skillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func TestBundledSkillsAreWellFormed(t *testing.T) {
	all := Bundled()
	if len(all) == 0 {
		t.Fatal("no bundled skills")
	}
	for _, s := range all {
		fm := Frontmatter(s.Files["SKILL.md"])
		switch {
		case fm == nil:
			t.Errorf("%s: SKILL.md has no frontmatter", s.Name)
		case fm["name"] != s.Name:
			t.Errorf("%s: frontmatter name %q, want the folder's name", s.Name, fm["name"])
		case !skillName.MatchString(s.Name) || len(s.Name) > 64:
			t.Errorf("%s: not a skill name", s.Name)
		case s.Description == "" || len(s.Description) > 1024:
			t.Errorf("%s: description of %d characters; want 1 to 1024", s.Name, len(s.Description))
		case fm["license"] == "":
			t.Errorf("%s: no license", s.Name)
		}
		if strings.Contains(s.Description, "<") || strings.Contains(s.Description, ">") {
			t.Errorf("%s: angle brackets in the description", s.Name)
		}
	}
	if !slices.ContainsFunc(all, func(s Skill) bool { return s.Name == "present" }) {
		t.Error("present is not bundled")
	}
}

func TestFrontmatter(t *testing.T) {
	for _, c := range []struct {
		in   string
		want map[string]string
	}{
		{"---\nname: a\ndescription: \"Says: hi\"\nmetadata:\n  k: v\n---\nbody", map[string]string{"name": "a", "description": "Says: hi", "metadata": ""}},
		{"name: a\n---\n", nil}, // the first line must be ---
		{"---\nname: a\n", nil}, // never closed
		{"\r\n---\r\nname: a\r\n---", nil},
		{"---\r\nname: a\r\n---\r\n", map[string]string{"name": "a"}},
	} {
		got := Frontmatter([]byte(c.in))
		if (got == nil) != (c.want == nil) || len(got) != len(c.want) {
			t.Errorf("Frontmatter(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("Frontmatter(%q)[%q] = %q, want %q", c.in, k, got[k], v)
			}
		}
	}
}

func TestChooseSaysWhy(t *testing.T) {
	all := []Skill{{Name: "present"}, {Name: "other"}}
	on := Settings{Enabled: true, Profile: true}
	for _, c := range []struct {
		s    Settings
		want []string // each skill's reason, "" when it goes in
	}{
		{on, []string{"", ""}},
		{Settings{Enabled: false, Profile: true}, []string{"skills are off (skills.enabled)", "skills are off (skills.enabled)"}},
		{Settings{Enabled: true, Profile: false}, []string{"this profile takes no skills", "this profile takes no skills"}},
		{Settings{Enabled: true, Profile: true, Disabled: []string{"other"}}, []string{"", "turned off (skills.disabled)"}},
	} {
		got := Choose(all, c.s)
		wantOn := 0
		for i, ch := range got {
			if ch.Why != c.want[i] || ch.On != (c.want[i] == "") {
				t.Errorf("Choose(%+v)[%s] = %v %q, want %q", c.s, ch.Name, ch.On, ch.Why, c.want[i])
			}
			if c.want[i] == "" {
				wantOn++
			}
		}
		if n := len(On(got)); n != wantOn {
			t.Errorf("On(Choose(%+v)) = %d skills, want %d", c.s, n, wantOn)
		}
	}
}

// tempRoot is a temporary skills root whose sealed folders are opened
// again before the test's temporary dir is removed.
func tempRoot(t *testing.T) string {
	root := t.TempDir()
	t.Cleanup(func() { remove(root) })
	return root
}

func TestBuildMakesAReadOnlyPlugin(t *testing.T) {
	root := tempRoot(t)
	set := Bundled()
	dir, err := Build(root, set, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != filepath.Join(root, "plugins") {
		t.Fatalf("folder %s is not under %s/plugins", dir, root)
	}
	var manifest map[string]any
	b, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &manifest); err != nil || manifest["name"] != "irie" || manifest["version"] != "1.2.3" || manifest["license"] != "Apache-2.0" {
		t.Errorf("manifest %s", b)
	}
	skill := filepath.Join(dir, "skills", "present", "SKILL.md")
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	var want []byte
	for _, s := range set {
		if s.Name == "present" {
			want = s.Files["SKILL.md"]
		}
	}
	if string(got) != string(want) {
		t.Error("the skill's file differs from the bundled one")
	}
	if fi, _ := os.Stat(skill); fi.Mode().Perm() != 0o444 {
		t.Errorf("SKILL.md is %v, want read-only", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o555 {
		t.Errorf("the folder is %v, want sealed", fi.Mode().Perm())
	}
	again, err := Build(root, set, "1.2.3")
	if err != nil || again != dir {
		t.Errorf("a second build gave %s, %v; want the same folder", again, err)
	}
	other, err := Build(root, set, "1.2.4")
	if err != nil || other == dir {
		t.Errorf("another version gave %s, %v; want a folder of its own", other, err)
	}
	if !Same(dir, Layout(set, "1.2.3")) {
		t.Error("the first folder changed when another was built")
	}
}

func TestBuildPutsBackAnEditedFolder(t *testing.T) {
	root := tempRoot(t)
	set := Bundled()
	dir, err := Build(root, set, "1")
	if err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(dir, "skills", "present", "SKILL.md")
	// An agent that edits a skill where it finds it, and adds a file.
	os.Chmod(filepath.Dir(skill), 0o755)
	os.Chmod(skill, 0o644)
	if err := os.WriteFile(skill, []byte("---\nname: present\n---\nrewritten\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(skill), "extra.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Same(dir, Layout(set, "1")) {
		t.Fatal("an edited folder looks the same")
	}
	again, err := Build(root, set, "1")
	if err != nil || again != dir {
		t.Fatalf("rebuild gave %s, %v", again, err)
	}
	if !Same(dir, Layout(set, "1")) {
		t.Error("the rebuild did not put the folder back")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(skill), "extra.md")); err == nil {
		t.Error("the added file is still there")
	}
}

func TestPruneKeepsWhatSessionsUse(t *testing.T) {
	root := tempRoot(t)
	set := Bundled()
	used, err := Build(root, set, "1")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := Build(root, set, "2")
	if err != nil {
		t.Fatal(err)
	}
	half := filepath.Join(root, "plugins", ".new-123")
	os.MkdirAll(filepath.Join(half, "skills"), 0o755)
	removed, err := Prune(root, []string{used + "/"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{half, stale}
	slices.Sort(removed)
	slices.Sort(want)
	if !slices.Equal(removed, want) {
		t.Errorf("removed %v, want %v", removed, want)
	}
	if !Same(used, Layout(set, "1")) {
		t.Error("the folder in use was touched")
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("the stale folder is still there")
	}
	if got, err := Prune(filepath.Join(root, "missing"), nil); err != nil || got != nil {
		t.Errorf("Prune of a missing root = %v, %v", got, err)
	}
}
