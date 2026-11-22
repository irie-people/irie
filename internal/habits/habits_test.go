package habits

import (
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
)

func TestFamilyNamesTheCommandThatCounts(t *testing.T) {
	for cmd, want := range map[string]string{
		`cd /home/alice/irie && git -C /home/alice/irie log --oneline -5`:                   "git log",
		`"$` + brand.Env("BIN") + `" ctl list 2>&1 | head -80`:                              brand.Slug + " ctl list",
		"S=/tmp/x; python3 - \"$S/a\" <<'EOF'\nimport os\nos.system(\"git push\")\nEOF\nls": "python3",
		`sed -n '1,20p' internal/core/dog.go`:                                               "sed -n",
		`sed -i '' 's/a/b/' file.go`:                                                        "sed -i",
		`timeout 90 go test ./internal/core -run TestDog`:                                   "go test",
		`sh tools/check.sh ./internal/core`:                                                 "tools/check.sh",
		`/home/alice/irie/tools/code.sh grep -w foo`:                                        "tools/code.sh grep",
		`echo hi; until grep -q done log; do sleep 5; done`:                                 "until",
		`gh api repos/example/irie/pulls`:                                                   "gh api",
		"# look first\ngit status --short":                                                  "git status",
		`cat <<<"x" | wc -l`:                                                                "cat",
		`if ! git diff --quiet; then git add -A; fi`:                                        "git diff",
		`echo "a; b" && ls -la`:                                                             "ls",
		`cd x`:                                                                              "(none)",
	} {
		if got := Family(cmd); got != want {
			t.Errorf("Family(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestBuildCountsFindsThenReadsWaitsAndRuns(t *testing.T) {
	var cs []Call
	add := func(session, tool, cmd string) {
		c := Call{Session: session, Agent: "main", Tool: tool, Family: tool, Cmd: cmd, MS: 60000}
		if tool == "Bash" {
			c.Family = Family(cmd)
		}
		cs = append(cs, c)
	}
	add("s1", "Bash", "grep -n foo a.go")
	add("s1", "Bash", "sed -n 1,9p a.go") // read just after: counts
	add("s1", "Bash", "grep -n bar a.go")
	add("s2", "Read", "") // another session's read: does not count
	add("s1", "Edit", "")
	add("s1", "Edit", "")
	add("s1", "Read", "") // three calls after the grep: too late
	for range 3 {
		add("s2", "Bash", "sleep 20; irie ctl list")
	}
	r := Build(cs, 10)
	if r.Calls != 10 || r.Sessions != 2 {
		t.Fatalf("calls %d in %d sessions, want 10 in 2", r.Calls, r.Sessions)
	}
	if len(r.Finds) != 1 || r.Finds[0] != (Find{Family: "grep", Calls: 2, ThenRead: 1}) {
		t.Fatalf("finds %+v, want grep 2 calls, 1 then read", r.Finds)
	}
	if r.Waits.Calls != 3 || r.Waits.Minutes != 3 {
		t.Fatalf("waits %+v, want 3 calls of 3 minutes", r.Waits)
	}
	if len(r.Repeats) != 1 || r.Repeats[0].Family != "sleep" || r.Repeats[0].Calls != 3 {
		t.Fatalf("repeats %+v, want 3 sleeps", r.Repeats)
	}
}

func TestCallsJoinsEachStartToItsEnd(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	ev := func(kind, id string, data map[string]any) event.Event {
		return event.Event{TS: event.Stamp(at), Session: "s1", Agent: "main", Kind: kind, Vendor: &event.Vendor{ToolUseID: id}, Data: data}
	}
	cs := Calls([]event.Event{
		ev(event.ToolStarted, "a", map[string]any{"tool": "Bash", "summary": "go test ./internal/core"}),
		ev(event.ToolStarted, "b", map[string]any{"tool": "Read"}),
		ev(event.ToolFailed, "a", map[string]any{"tool": "Bash", "ms": 1500.0, "tests": "failed"}),
		ev(event.ToolFinished, "b", map[string]any{"tool": "Read", "ms": 20.0}),
		ev(event.ToolFinished, "c", map[string]any{"tool": "Read", "ms": 9.0}), // no start: nothing to join
	})
	if len(cs) != 2 {
		t.Fatalf("calls = %+v", cs)
	}
	if c := cs[0]; c.Family != "go test" || c.MS != 1500 || !c.Failed || c.Tests != "failed" || !c.At.Equal(at) || c.Session != "s1" {
		t.Errorf("the test run = %+v", c)
	}
	if c := cs[1]; c.Family != "Read" || c.MS != 20 || c.Failed || c.Cmd != "" {
		t.Errorf("the read = %+v", c)
	}
}

func TestBuildGivesEachKindItsTimeAndShortestExamples(t *testing.T) {
	var cs []Call
	for i, cmd := range []string{"git status --short --branch", "git status", "git status", "git  status -s", "git status -s"} {
		cs = append(cs, Call{Session: "s1", Agent: "main", Tool: "Bash", Cmd: cmd, Family: Family(cmd), MS: float64(i+1) * 60000})
	}
	cs = append(cs, Call{Session: "s1", Agent: "main", Tool: "Read", Family: "Read", MS: 30000})
	r := Build(cs, 0)
	var git Kind
	for _, f := range r.Families {
		if f.Family == "git status" {
			git = f
		}
	}
	// Five calls of 1 to 5 minutes: 15 in all; the same command twice is one example.
	if git.Calls != 5 || git.Minutes != 15 || strings.Join(git.Examples, " | ") != "git status | git status -s" {
		t.Errorf("git status = %+v", git)
	}
	if len(r.Families) != 2 || r.Families[1].Family != "Read" || r.Families[1].Examples != nil || r.Families[1].Minutes != 0.5 {
		t.Errorf("families = %+v", r.Families)
	}
	if r := Build(cs, 1); len(r.Families) != 1 {
		t.Errorf("top 1 kept %d kinds", len(r.Families))
	}
}

func TestExampleIsOneLineCutAndMasked(t *testing.T) {
	for cmd, want := range map[string]string{
		"cd /home/alice/irie\ngo vet ./...":              "cd /home/alice/irie⏎go vet ./...",
		"GITHUB_TOKEN=ghp_abc123 gh pr list":             "GITHUB_TOKEN=… gh pr list",
		`curl -H "Authorization: Bearer abcdefgh1234" x`: `curl -H "Authorization: Bearer …" x`,
		"echo sk-abcdefghijklmnopqrstu":                  "echo …",
	} {
		if got := Example(cmd); got != want {
			t.Errorf("Example(%q) = %q, want %q", cmd, got, want)
		}
	}
	long := Example(strings.Repeat("é", 500))
	if n := len([]rune(long)); n != MaxExample || !strings.HasSuffix(long, "…") {
		t.Errorf("a long command shows %d characters: %q", n, long)
	}
}
