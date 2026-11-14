package core

// Playbooks (DESIGN §6.6): a profile's rules for how to work, which the
// judge of a bench rewrites from the mistakes it saw, and which every
// session on the profile is told as it starts, so that the profile's
// agents get better at their work from one bench to the next. A playbook is
// a Markdown file of its own, playbooks/<profile>.md, whose lines that
// start with "- " are its rules: the user can read and edit it, and an
// edit counts from the next session on. Each one a judge replaces is kept
// in playbooks/history/.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
)

// playbookEntry is a playbook as the core last read it.
type playbookEntry struct {
	mod   time.Time
	size  int64
	rules []string
}

// fileSafe makes a profile's name a file's: letters, digits, dots,
// hyphens and underscores stay, anything else becomes an underscore.
func fileSafe(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '_'
	}, name)
}

// playbookFile is where a profile's playbook is kept.
func (c *Core) playbookFile(profile string) string {
	return filepath.Join(c.paths.Playbooks(), fileSafe(profile)+".md")
}

// playbook is a profile's rules, read again only once its file changes.
// The caller holds c.mu.
func (c *Core) playbook(profile string) []string {
	path := c.playbookFile(profile)
	fi, err := os.Stat(path)
	if err != nil {
		delete(c.benches.pb, profile)
		return nil
	}
	if e, ok := c.benches.pb[profile]; ok && e.mod.Equal(fi.ModTime()) && e.size == fi.Size() {
		return e.rules
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	rules := parsePlaybook(string(data))
	if c.benches.pb == nil {
		c.benches.pb = map[string]playbookEntry{}
	}
	c.benches.pb[profile] = playbookEntry{mod: fi.ModTime(), size: fi.Size(), rules: rules}
	return rules
}

// parsePlaybook reads a playbook's rules: its lines that start with "- ".
func parsePlaybook(text string) []string {
	var rules []string
	for _, line := range strings.Split(text, "\n") {
		if r, ok := strings.CutPrefix(strings.TrimRight(line, " \t\r"), "- "); ok && strings.TrimSpace(r) != "" {
			rules = append(rules, strings.TrimSpace(r))
		}
	}
	return rules
}

// cleanRules makes a judge's rules a playbook: each on one line and cut to
// maxRule, without blanks or repeats, and at most max of them.
func cleanRules(in []string, max int) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, r := range in {
		r = cleanText(strings.TrimLeft(strings.TrimSpace(r), "-*• "), maxRule)
		key := strings.ToLower(r)
		if r == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
		if len(out) == max {
			break
		}
	}
	return out
}

// setPlaybook keeps rules as a profile's playbook, which bench from wrote,
// and the playbook it replaces in playbooks/history/.
func (c *Core) setPlaybook(profile string, rules []string, from string, now time.Time) error {
	dir := c.paths.Playbooks()
	if err := os.MkdirAll(filepath.Join(dir, "history"), 0o700); err != nil {
		return err
	}
	path := c.playbookFile(profile)
	if old, err := os.ReadFile(path); err == nil {
		kept := filepath.Join(dir, "history", fileSafe(profile)+"-"+now.UTC().Format("20060102T150405Z")+".md")
		if err := filetx.WriteFile(kept, old, 0o600); err != nil {
			return err
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Playbook for +%s\n\n", profile)
	fmt.Fprintf(&b, "The judge of bench %s wrote these rules on %s, from the mistakes it saw. %s tells every session on +%s them as it starts. Edit them as you like: each line that starts with \"- \" is a rule.\n\n",
		from, now.UTC().Format("2006-01-02 15:04 UTC"), brand.Name, profile)
	for _, r := range rules {
		b.WriteString("- " + r + "\n")
	}
	if err := filetx.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.benches.pb, profile)
	c.mu.Unlock()
	return nil
}

// playbookIntro opens a profile's rules wherever they are given.
func playbookIntro(profile string) string {
	return "These rules for +" + profile + " come from judging earlier work on it; follow them on every task:"
}

// playbookNote is what a session on a profile with a playbook is told as
// it starts: the rules. The caller holds c.mu.
func (c *Core) playbookNote(x *session) string {
	rules := c.playbook(x.profile)
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[" + brand.Name + "] " + playbookIntro(x.profile))
	for _, r := range rules {
		b.WriteString("\n- " + r)
	}
	return b.String()
}
