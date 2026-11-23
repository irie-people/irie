package testrun

import (
	"regexp"
	"strings"
	"testing"
)

func TestRuns(t *testing.T) {
	for cmd, want := range map[string]bool{
		"go test ./...": true,
		"cd /home/alice/repo && go test -count=1 ./internal/core/ 2>&1 | tail -20": true,
		"GOFLAGS=-count=1 go test ./x":                                             true,
		"tools/check.sh ./internal/core":                                           true,
		"sh tools/check.sh":                                                        true,
		"tools/live.sh":                                                            true,
		"timeout 600 cargo test -p store":                                          true,
		"npm run test":                                                             true,
		"bun test":                                                                 true,
		"python3 -m pytest tests/":                                                 true,
		"make check":                                                               true,
		"if go test ./...; then echo ok; fi":                                       true,
		"for p in a b; do go test ./$p; done":                                      true,
		"(cd web && npx vitest run)":                                               true,
		"python3 -I - <<'EOF'\nprint(1)\nEOF\ngo test ./x":                         true,
		"go vet ./...":                                                             false,
		"go build ./... && gofmt -l .":                                             false,
		"grep -rn 'go test' docs/":                                                 false,
		"echo go test":                                                             false,
		"git log --oneline | head":                                                 false,
		"cat > run.sh <<'EOF'\ngo test ./...\nEOF":                                 false,
		"sh $S/ship.sh":                                                            false,
		"./mac/snapshot.sh /tmp/shots":                                             false,
	} {
		if got := Runs(cmd); got != want {
			t.Errorf("Runs(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestRead(t *testing.T) {
	const (
		goPass  = "ok  \texample.com/x\t0.112s\nok  \texample.com/y\t(cached)\n"
		goFail  = "--- FAIL: TestThing (0.00s)\n    thing_test.go:12: got 1, want 2\nFAIL\nFAIL\texample.com/x\t0.112s\nok  \texample.com/y\t(cached)\nFAIL\n"
		goBuild = "# example.com/x\n./x.go:3:2: undefined: y\nFAIL\texample.com/x [build failed]\nFAIL\n"
	)
	for _, c := range []struct {
		name, cmd, out string
		exit           int
		want, line     string
	}{
		{"go passes", "go test ./...", goPass, 0, Passed, ""},
		{"go fails", "go test ./...", goFail, 1, Failed, "--- FAIL: TestThing (0.00s)"},
		{"a failure piped through tail still shows", "go test ./... 2>&1 | tail -3", "FAIL\texample.com/x\t0.112s\nok  \texample.com/y\t(cached)\nFAIL\n", 0, Failed, "FAIL\texample.com/x\t0.112s"},
		{"a build failure", "go test ./x", goBuild, 1, Failed, "FAIL\texample.com/x [build failed]"},
		{"a panic", "go test ./x", "panic: runtime error: index out of range [3] with length 3\n\ngoroutine 7 [running]:\n", 2, Failed, "panic: runtime error: index out of range [3] with length 3"},
		// grep -v exits 1 when it leaves nothing, which is when every
		// package passed: the code is grep's, so the run is unread.
		{"grep -v leaves nothing", "go test ./... 2>&1 | grep -v '^ok\\|no test files'", "", 1, Ran, ""},
		{"; echo hides the code", "go test ./x > /dev/null 2>&1; echo done", "done\n", 0, Ran, ""},
		{"the runner's own code", "go test ./x > out.txt 2>&1", "", 1, Failed, ""},
		{"&& echo keeps the runner's code", "go test ./x && echo ok", "", 1, Failed, ""},
		{"a later command's failure is not the tests'", "go test ./x && git commit -qm wip", goPass + "nothing to commit\n", 1, Passed, ""},
		{"an install held up by its lock", "tools/live.sh", "live: another install is running; wait for it to finish, then run this again\n", 1, Ran, ""},
		{"an install whose tests fail", "tools/live.sh", goFail, 1, Failed, "--- FAIL: TestThing (0.00s)"},
		{"check.sh and gofmt", "tools/check.sh", "check: gofmt would change these files; gofmt -w them:\ncheck:   internal/x/x.go\n", 1, Failed, "check: gofmt would change these files; gofmt -w them:"},
		{"cargo fails", "cargo test", "test result: FAILED. 1 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out\n\nerror: test failed, to rerun pass `--lib`\n", 101, Failed, "test result: FAILED. 1 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out"},
		{"cargo passes", "cargo test", "test result: ok. 3 passed; 0 failed; 0 ignored\n", 0, Passed, ""},
		{"pytest fails", "pytest -q", "FAILED tests/test_x.py::test_y - assert 1 == 2\n==== 1 failed, 2 passed in 0.12s ====\n", 1, Failed, "FAILED tests/test_x.py::test_y - assert 1 == 2"},
		{"pytest passes", "pytest -q", "==== 3 passed, 1 xfailed in 0.10s ====\n", 0, Passed, ""},
		{"bun fails", "bun test", " 15 pass\n 1 fail\n", 1, Failed, "1 fail"},
		{"bun passes", "bun test", " 16 pass\n 0 fail\n", 0, Passed, ""},
		{"jest fails", "npm test", "Tests:       1 failed, 2 passed, 3 total\n", 1, Failed, "Tests:       1 failed, 2 passed, 3 total"},
		{"jest passes", "npm test", "Tests:       3 passed, 3 total\n", 0, Passed, ""},
		{"xctest fails", "swift test", "\t Executed 5 tests, with 1 failure (0 unexpected) in 0.1 (0.1) seconds\n", 1, Failed, "Executed 5 tests, with 1 failure (0 unexpected) in 0.1 (0.1) seconds"},
		{"xctest passes", "swift test", "\t Executed 5 tests, with 0 failures (0 unexpected) in 0.1 (0.1) seconds\n", 0, Passed, ""},
		{"no tests", "git status", "FAIL\n", 1, "", ""},
	} {
		got, line := Read(c.cmd, c.out, c.exit)
		if got != c.want || line != c.line {
			t.Errorf("%s: Read = %q, %q; want %q, %q", c.name, got, line, c.want, c.line)
		}
	}
	long := "--- FAIL: " + strings.Repeat("TestAVeryLongName", 20) + " (0.00s)"
	if _, line := Read("go test ./x", long, 1); len(line) > lineMax+len("…") || !strings.HasSuffix(line, "…") {
		t.Errorf("a long line = %q", line)
	}
}

func TestSaysPassed(t *testing.T) {
	for msg, want := range map[string]bool{
		"All tests pass.": true,
		"The full test suite passed, and every agent kept running.": true,
		"**Verified:** the full test suite passes":                  true,
		"Built and installed; `go vet` and `go test` passed.":       true,
		"Done: 16 tests pass and the app relaunched.":               true,
		"tools/check.sh passes.":                                    true,
		"It passes all the tests.":                                  true,
		"The new test passes, and so does the rest of the package.": true,
		"Tests: pass":          true,
		"Tests: 33 of 34 pass": false,
		"The tests didn’t pass on the first try.":   false,
		"Once the tests pass, I'll ship it.":        false,
		"I couldn't run the tests.":                 false,
		"Tests pass locally but fail in CI.":        false,
		"All tests pass except TestFlaky.":          false,
		"I'll run the tests next.":                  false,
		"The tests should pass now.":                false,
		"Tests passed before my change.":            false,
		"The check.sh run failed.":                  false,
		"Nothing passes yet.":                       false,
		"The page builds. It works in both themes.": false,
	} {
		if got := SaysPassed(msg); got != want {
			t.Errorf("SaysPassed(%q) = %v, want %v", msg, got, want)
		}
	}
}

// TestEveryPatternCompiles: the patterns compile on their first use, so a
// bad one would fail only then; this compiles each.
func TestEveryPatternCompiles(t *testing.T) {
	for _, p := range []func() *regexp.Regexp{runner, wrapper, lead, harmless, failMark, passMark, firstMark, passClaim, hedge, sentence} {
		if p() == nil {
			t.Error("a pattern did not compile")
		}
	}
}
