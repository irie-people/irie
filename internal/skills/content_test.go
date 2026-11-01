package skills

import (
	"regexp"
	"strings"
	"testing"
)

// grants are the frontmatter fields that change what an agent may do, which
// no skill of Irie's sets (docs/proposals/skills.md §4.1).
var grants = []string{"allowed-tools", "disallowed-tools", "hooks", "shell", "model", "effort", "context", "agent"}

// TestBundledSkillsGrantNothing: every skill that comes with Irie is
// Markdown only, grants no tools, runs nothing as it loads (Claude Code runs
// an exclamation mark and a backtick before the skill reaches the model),
// and names no one.
func TestBundledSkillsGrantNothing(t *testing.T) {
	email := regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	home := regexp.MustCompile(`/home/[a-z]`)
	for _, s := range Bundled() {
		fm := Frontmatter(s.Files["SKILL.md"])
		for _, k := range grants {
			if _, ok := fm[k]; ok {
				t.Errorf("%s declares %s", s.Name, k)
			}
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
			if strings.Contains(text, "/Users/") || home.MatchString(strings.ReplaceAll(text, "/home/alice", "")) {
				t.Errorf("%s/%s: a home folder; use ~ or /home/alice", s.Name, p)
			}
		}
	}
}

// TestGuidesAreTheUsers: a guide, such as the dead man's switch, runs only
// when the person starts it, so it costs no context in other sessions and
// no agent starts it on its own.
func TestGuidesAreTheUsers(t *testing.T) {
	found := false
	for _, s := range Bundled() {
		if s.Name != "dead-mans-switch" {
			continue
		}
		found = true
		if Frontmatter(s.Files["SKILL.md"])["disable-model-invocation"] != "true" {
			t.Errorf("%s: want disable-model-invocation: true", s.Name)
		}
	}
	if !found {
		t.Error("dead-mans-switch is not bundled")
	}
}
