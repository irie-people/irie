package main

import "testing"

func TestFamilyNamesTheCommandThatCounts(t *testing.T) {
	for cmd, want := range map[string]string{
		`cd /home/alice/irie && git -C /home/alice/irie log --oneline -5`:                   "git log",
		`"$IRIE_BIN" ctl list 2>&1 | head -80`:                                              "irie ctl list",
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
		if got := family(cmd); got != want {
			t.Errorf("family(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestBuildCountsFindsThenReadsWaitsAndRuns(t *testing.T) {
	var cs []call
	add := func(agent, tool, cmd string) {
		c := call{agent: agent, tool: tool, family: tool, cmd: cmd, ms: 60000}
		if tool == "Bash" {
			c.family = family(cmd)
		}
		cs = append(cs, c)
	}
	add("s1/main", "Bash", "grep -n foo a.go")
	add("s1/main", "Bash", "sed -n 1,9p a.go") // read just after: counts
	add("s1/main", "Bash", "grep -n bar a.go")
	add("s2/main", "Read", "") // another session's read: does not count
	add("s1/main", "Edit", "")
	add("s1/main", "Edit", "")
	add("s1/main", "Read", "") // three calls after the grep: too late
	for range 3 {
		add("s2/main", "Bash", "sleep 20; irie ctl list")
	}
	r := build(cs, 10)
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
