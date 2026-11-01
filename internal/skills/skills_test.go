package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The fields that change what an agent may do, and so never appear in
// Irie's own skills (skills.md §4.1, §6.1).
var grants = []string{"allowed-tools", "disallowed-tools", "hooks", "shell", "model", "effort", "context", "agent"}

func TestBuiltinSkillsAreWellFormed(t *testing.T) {
	all, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("no built-in skills")
	}
	email := regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	for _, s := range all {
		fm, err := Frontmatter(s.Files["SKILL.md"])
		if err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
		for _, k := range grants {
			if _, ok := fm[k]; ok {
				t.Errorf("%s declares %s", s.Name, k)
			}
		}
		// Irie's skills are guides the user starts: they cost no context
		// until then, and the agent never starts one on its own.
		if !s.UserOnly {
			t.Errorf("%s: want disable-model-invocation: true", s.Name)
		}
		if n := len(s.Description) + len(fm["when_to_use"]); n > 1536 {
			t.Errorf("%s: description is %d characters; Claude Code cuts it at 1,536", s.Name, n)
		}
		for p, b := range s.Files {
			if !strings.HasSuffix(p, ".md") {
				t.Errorf("%s/%s: a skill of Irie's is Markdown only; its helpers are irie commands", s.Name, p)
			}
			text := string(b)
			if strings.Contains(text, "!`") || strings.Contains(text, "```!") {
				t.Errorf("%s/%s: injected command", s.Name, p)
			}
			for _, m := range email.FindAllString(text, -1) {
				if !strings.HasSuffix(m, "@example.com") && !strings.HasSuffix(m, "@hc-ping.com") {
					t.Errorf("%s/%s: e-mail address %s; use example.com", s.Name, p, m)
				}
			}
			if strings.Contains(text, "/Users/") || regexp.MustCompile(`/home/[a-z]`).MatchString(strings.ReplaceAll(text, "/home/alice", "")) {
				t.Errorf("%s/%s: a home folder; use ~ or /home/alice", s.Name, p)
			}
		}
	}
}

func TestFrontmatter(t *testing.T) {
	fm, err := Frontmatter([]byte("---\r\nname: x\r\ndescription: \"Says: hi\"\r\n# a comment\r\ndisable-model-invocation: true\r\n---\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	if fm["name"] != "x" || fm["description"] != "Says: hi" || fm["disable-model-invocation"] != "true" {
		t.Errorf("got %q", fm)
	}
	for _, bad := range []string{"name: x\n", "---\nname: x\n", "---\n  name: x\n---\n", "---\nname\n---\n"} {
		if _, err := Frontmatter([]byte(bad)); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestEnabled(t *testing.T) {
	all := []Skill{{Name: "a"}, {Name: "b"}}
	if got := Enabled(all, false, nil); len(got) != 0 {
		t.Errorf("off: got %v", got)
	}
	if got := Enabled(all, true, []string{"a"}); len(got) != 1 || got[0].Name != "b" {
		t.Errorf("a off: got %v", got)
	}
}

func testSet() []Skill {
	return []Skill{{Name: "guide", Description: "A guide.", UserOnly: true, Files: map[string][]byte{
		"SKILL.md":         []byte("---\nname: guide\ndescription: A guide.\ndisable-model-invocation: true\n---\nDo it.\n"),
		"reference/one.md": []byte("One.\n"),
	}}}
}

func TestFolderLayoutAndModes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	dir, err := Folder(root, testSet())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != root || len(filepath.Base(dir)) != 16 {
		t.Errorf("folder %s", dir)
	}
	var m struct{ Name string }
	b, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil || json.Unmarshal(b, &m) != nil || m.Name != Plugin {
		t.Fatalf("manifest %s, %v", b, err)
	}
	for _, p := range []string{"skills/guide/SKILL.md", "skills/guide/reference/one.md", ".claude-plugin/plugin.json"} {
		fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o444 {
			t.Errorf("%s: mode %v, want read-only", p, fi.Mode().Perm())
		}
	}
	// The same set gets the same folder; another set, another.
	again, err := Folder(root, testSet())
	if err != nil || again != dir {
		t.Errorf("again: %s, %v; want %s", again, err, dir)
	}
	other := testSet()
	other[0].Files["reference/one.md"] = []byte("One, changed.\n")
	if d, _ := Folder(root, other); d == dir {
		t.Error("a changed skill kept the folder's name")
	}
}

func TestFolderIsMadeAgainWhenChanged(t *testing.T) {
	root := t.TempDir()
	dir, err := Folder(root, testSet())
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "skills", "guide", "SKILL.md")
	for name, tamper := range map[string]func(){
		"edited": func() { os.Chmod(f, 0o644); os.WriteFile(f, []byte("Do something else.\n"), 0o644) },
		"added":  func() { os.WriteFile(filepath.Join(dir, "skills", "guide", "extra.md"), []byte("x"), 0o444) },
		"chmod":  func() { os.Chmod(f, 0o644) },
		"gone":   func() { os.Chmod(filepath.Dir(f), 0o755); os.Remove(f) },
	} {
		tamper()
		if Check(dir, Contents(testSet())) == nil {
			t.Fatalf("%s: Check passed", name)
		}
		again, err := Folder(root, testSet())
		if err != nil || again != dir {
			t.Fatalf("%s: %s, %v", name, again, err)
		}
		if err := Check(dir, Contents(testSet())); err != nil {
			t.Errorf("%s: not made again: %v", name, err)
		}
	}
}

func TestPrune(t *testing.T) {
	root := t.TempDir()
	keep, err := Folder(root, testSet())
	if err != nil {
		t.Fatal(err)
	}
	other := testSet()
	other[0].Files["reference/two.md"] = []byte("Two.\n")
	gone, err := Folder(root, other)
	if err != nil {
		t.Fatal(err)
	}
	if err := Prune(root, map[string]bool{keep: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("kept folder: %v", err)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Errorf("unused folder still there: %v", err)
	}
	if err := Prune(filepath.Join(root, "none"), nil); err != nil {
		t.Errorf("no folder: %v", err)
	}
}

func TestCommand(t *testing.T) {
	if got := (Skill{Name: "dead-mans-switch"}).Command(); got != "/irie:dead-mans-switch" {
		t.Errorf("got %s", got)
	}
}
