package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// lockFixture is a lock op's answer: a session whose Claude Code has an
// advisory and whose MCP server has none, after a check.
func lockFixture(now time.Time) ipc.LockResult {
	claude := ipc.LockPackage{Ecosystem: "npm", Name: "@anthropic-ai/claude-code", Version: "2.1.250"}
	server := ipc.LockPackage{Ecosystem: "npm", Name: "@example/fake-mcp", Version: "1.2.3"}
	l := ipc.Lock{
		Session: "s0123456789ab", Written: now.Add(-2 * time.Hour), At: ipc.LockSpawn,
		Irie:    ipc.LockPart{Name: "irie", From: ipc.LockFromIrie, Version: "1.5.0", Command: "/home/alice/.local/share/irie/bin/irie", Hash: "sha256:9f2c1a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f7"},
		Harness: ipc.LockPart{Name: "claude-code", Version: "2.1.250", Command: "/home/alice/.local/share/claude/versions/2.1.250", Package: &claude},
		Profile: "fable", Model: "claude-opus-5-5", PermissionMode: "acceptEdits",
		MCP: []ipc.LockPart{
			{Name: "irie-browser", From: ipc.LockFromIrie, Version: "1.5.0", Command: "irie browser mcp", Hash: "sha256:00"},
			{Name: "fake", From: ipc.LockFromUser, Version: "1.2.3", Command: "npx @example/fake-mcp@1.2.3", Package: &server, Hash: "sha256:11"},
		},
		Plugins: []ipc.LockPart{{Name: "writing", From: ipc.LockFromIrie, Version: "1.5.0", Hash: "sha256:22"}},
		Skills:  []ipc.LockPart{{Name: "irie:present", From: ipc.LockFromIrie, Version: "1.5.0", Hash: "sha256:33"}},
		Hooks: []ipc.LockPart{
			{Name: "irie hook", From: ipc.LockFromIrie, Version: "1.5.0", Command: "irie hook", Events: []string{"PreToolUse", "Stop"}, Hash: "sha256:44"},
			{Name: "PreToolUse Bash", From: ipc.LockFromUser, Command: "guard.sh", Hash: "sha256:55"},
		},
	}
	checked := now.Add(-time.Hour)
	marked := []ipc.LockPackage{
		{Ecosystem: "npm", Name: claude.Name, Version: claude.Version, Part: "Claude Code", Checked: true, Advisories: 1, Fixed: "2.1.251"},
		{Ecosystem: "npm", Name: server.Name, Version: server.Version, Part: "MCP server fake", Checked: true},
	}
	return ipc.LockResult{
		Sessions: []ipc.LockSession{
			{ID: l.Session, Name: "fix the parser", Open: true, Lock: &l, Packages: marked},
			{ID: "s0123456789ac", Name: "older work", Open: true, None: "no lock: it started before Irie wrote locks, and gets one when it is reopened, resumed or forked"},
			{ID: "Asahi/s0123456789ad", Name: "night work", Host: "Asahi", Open: true, Packages: marked[:1]},
		},
		Packages: []ipc.LockFound{
			{Ecosystem: "npm", Name: claude.Name, Version: claude.Version, Part: "Claude Code", Checked: checked, Sessions: []string{"fix the parser", "night work"},
				Advisories: []ipc.LockAdvisory{{ID: "GHSA-test-0001", Summary: "Claude Code: the Edit tool can write outside the workspace", Fixed: "2.1.251"}}},
			{Ecosystem: "npm", Name: server.Name, Version: server.Version, Part: "MCP server fake", Checked: checked, Sessions: []string{"fix the parser"}},
		},
		Checked: checked, OSV: "https://api.osv.dev",
	}
}

func TestIrieLockPrintsALinePerPart(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	var b bytes.Buffer
	if err := printLocks(&b, lockFixture(now), now); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"fix the parser (s0123456789ab), locked at its spawn 2 hours ago",
		"irie (irie)",
		"sha256:9f2c1a3b4c5d\n",
		"harness      claude-code",
		"npm @anthropic-ai/claude-code: 1 advisory, fixed in 2.1.251",
		"profile      +fable",
		"model        claude-opus-5-5",
		"permissions  acceptEdits",
		"mcp          fake (user)",
		"npm @example/fake-mcp: no known advisory",
		"plugin       writing (irie)",
		"skill        irie:present (irie)",
		"hooks        irie hook (irie)",
		"2 events",
		"hook         PreToolUse Bash (user)",
		"older work (s0123456789ac)\n  no lock: it started before",
		"night work (Asahi/s0123456789ad), on Asahi, whose lock is there",
		"OSV.dev was asked 1 hour ago; irie lock --check asks it now, sending only names and versions.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("irie lock lacks %q:\n%s", want, got)
		}
	}
	// One line a part, with no padding after it: the session's lock has 11.
	lines := 0
	for _, l := range strings.Split(strings.SplitN(got, "\n\n", 2)[0], "\n")[1:] {
		if strings.TrimSpace(l) != "" {
			lines++
		}
	}
	if lines != 11 {
		t.Errorf("the lock printed %d lines, want 11:\n%s", lines, got)
	}
	for _, l := range strings.Split(got, "\n") {
		if strings.HasSuffix(l, " ") {
			t.Errorf("a line ends in spaces: %q", l)
		}
	}
}

func TestIrieLockCheckPrintsEachAdvisory(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	var b bytes.Buffer
	if err := printLockCheck(&b, lockFixture(now)); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"Asked OSV.dev about 2 package versions: 1 with known advisories.",
		"Claude Code 2.1.250 (npm @anthropic-ai/claude-code), in 2 sessions: fix the parser, night work",
		"GHSA-test-0001  Claude Code: the Edit tool can write outside the workspace  fixed in 2.1.251",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("irie lock --check lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "fake-mcp") {
		t.Errorf("a version with no advisory is listed:\n%s", got)
	}
	r := lockFixture(now)
	r.Packages = r.Packages[1:]
	b.Reset()
	printLockCheck(&b, r)
	if got := b.String(); got != "Asked OSV.dev about 1 package version: none has a known advisory.\n" {
		t.Errorf("with none: %q", got)
	}
}

func TestPsCountsAdvisories(t *testing.T) {
	now := time.Now()
	rows := []ipc.Session{
		{ID: "s1", Name: "parser", State: ipc.StateWorking, Created: now, Lock: &ipc.LockRow{Advisories: 2, Words: "Claude Code 2.1.250: 2 advisories, fixed in 2.1.251"}},
		{ID: "s2", Name: "lexer", State: ipc.StateDone, Created: now, Lock: &ipc.LockRow{}},
	}
	var b bytes.Buffer
	printProcs(&b, procsOf(rows, now))
	got := b.String()
	if !strings.Contains(got, "· 1 run a version with known advisories (irie lock)") || !strings.Contains(got, "ADV") {
		t.Errorf("ps:\n%s", got)
	}
	var adv []string
	for _, l := range strings.Split(got, "\n")[2:] {
		if f := strings.Fields(l); len(f) > 8 {
			adv = append(adv, f[8])
		}
	}
	if strings.Join(adv, " ") != "2 -" {
		t.Errorf("ADV column %v:\n%s", adv, got)
	}
}
