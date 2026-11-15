package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/route"
	"github.com/irie-people/irie/internal/testutil"
)

// benching makes the fake agent a cheap profile to bench and a judge whose
// turn ends with the text of the file say. bench is more of the [bench]
// table.
func benching(t *testing.T, say string, bench ...string) agentConfig {
	fake := testutil.BuildFakeAgent(t)
	return agentConfig{extra: fmt.Sprintf(`[profiles.cheap]
command = %q
permission_mode = "auto"
env = { IRIE_FAKE_VERSION = "2.1.319" }

[profiles.judge]
command = %q
permission_mode = "auto"
env = { IRIE_FAKE_SCRIPT = "prompt,stop", IRIE_FAKE_SAY = %q, IRIE_FAKE_VERSION = "2.1.319" }

[route]
profiles = ["cheap", "fake"]
prior_weight = 2

[route.accuracy]
cheap = { small = 0.9, medium = 0.5, hard = 0.1 }
fake = { small = 0.95, medium = 0.9, hard = 0.9 }

[route.cost]
cheap = 0.05
fake = 1.0

[bench]
judge = "judge"
%s
`, fake, fake, say, strings.Join(bench, "\n"))}
}

func (c *coreEnv) benches() []ipc.Bench {
	c.t.Helper()
	var r ipc.BenchResult
	if err := c.call("bench", ipc.BenchArgs{}, &r); err != nil {
		c.t.Fatalf("bench list: %v", err)
	}
	return r.Benches
}

// waitBench waits until bench id is done or failed.
func (c *coreEnv) waitBench(id string) ipc.Bench {
	c.t.Helper()
	var last ipc.Bench
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		for _, b := range c.benches() {
			if b.ID == id {
				last = b
			}
		}
		if last.State == ipc.BenchDone || last.State == ipc.BenchFailed {
			return last
		}
	}
	c.t.Fatalf("bench %s did not finish: %+v", id, last)
	return last
}

func TestBenchJudgesItsRunsAndKeepsAPlaybook(t *testing.T) {
	say := filepath.Join(t.TempDir(), "verdict.md")
	verdict := "I judged them all.\n\n```json\n" +
		`{"runs": [{"run": 1, "score": 90, "missed": "nothing"}, {"run": 2, "score": 40, "missed": "left the typo"}, {"run": 3, "score": 75, "missed": "no test"}],` +
		` "final": "run 1 with run 3's test, in my folder", "playbook": ["Read a file before you edit it.", "- Run the tests before you finish.", "read a file before you edit it."]}` +
		"\n```\n"
	if err := os.WriteFile(say, []byte(verdict), 0o600); err != nil {
		t.Fatal(err)
	}
	// Its runs stay open once judged, for their rows to be read here.
	c := setupWith(t, benching(t, say, "close = false"))

	// A bench runs one profile: auto, and a profile it doesn't know, are
	// refused, and with no local profile in route.profiles it needs one.
	for _, a := range []ipc.BenchArgs{{Task: "x", Profile: "auto"}, {Task: "x", Profile: "nope"}, {Task: "x"}, {Task: "x", Profile: "cheap", Runs: 9}} {
		if err := c.call("bench", a, nil); err == nil {
			t.Errorf("bench %+v started", a)
		}
	}

	task := "prompt,write:out.txt,stop\nfix the typo in README"
	var r ipc.BenchResult
	if err := c.call("bench", ipc.BenchArgs{Task: task, Profile: "cheap"}, &r); err != nil {
		t.Fatal(err)
	}
	b := r.Bench
	if b == nil || len(b.Runs) != 3 || b.Profile != "cheap" || b.Judge != "judge" || b.Class != route.Small || b.State != ipc.BenchRunning {
		t.Fatalf("bench = %+v", b)
	}
	// Each run is a session of its own on the profile, on the task as
	// typed, named for the bench.
	for i, run := range b.Runs {
		s, ok := c.row(run.Session)
		if !ok || s.Profile != "cheap" || !strings.HasPrefix(s.Name, fmt.Sprintf("bench %d/3 · ", i+1)) || s.Class != route.Small {
			t.Errorf("run %d = %+v", i+1, s)
		}
		// No one watches a run, so it refuses what would wait on a
		// permission prompt.
		if refuses, unattended := c.unattended(run.Session); !refuses || !unattended {
			t.Errorf("run %d: refuses prompts %v, unattended %v", i+1, refuses, unattended)
		}
	}

	// A task of nothing but a bench's id shows that bench, and starts none.
	var shown ipc.BenchResult
	if err := c.call("bench", ipc.BenchArgs{Task: " " + b.ID + "\n", Profile: "cheap"}, &shown); err != nil {
		t.Fatal(err)
	}
	if !shown.Shown || shown.Bench == nil || shown.Bench.ID != b.ID || len(shown.Bench.Runs) != 3 {
		t.Errorf("bench %s by its id = %+v", b.ID, shown)
	}
	if all := c.benches(); len(all) != 1 {
		t.Errorf("asking after a bench by its id started one: %d benches", len(all))
	}

	// Once they are through, the judge's verdict rates them by its scores
	// (bench.pass is 70) and its rules become the profile's playbook.
	done := c.waitBench(b.ID)
	if done.State != ipc.BenchDone || done.JudgeID == "" || done.Final != "run 1 with run 3's test, in my folder" || done.Rules != 2 {
		t.Fatalf("judged bench = %+v", done)
	}
	for i, want := range []struct {
		score   int
		verdict string
	}{{90, ipc.Good}, {40, ipc.Bad}, {75, ipc.Good}} {
		run := done.Runs[i]
		if run.Score == nil || *run.Score != want.score || run.Verdict != want.verdict {
			t.Errorf("run %d = %+v, want %d %s", i+1, run, want.score, want.verdict)
		}
		if s, _ := c.row(run.Session); s.Rated != want.verdict {
			t.Errorf("run %d's row is rated %q, want %q", i+1, s.Rated, want.verdict)
		}
	}
	judge, ok := c.row(done.JudgeID)
	if !ok || judge.Profile != "judge" || !strings.HasPrefix(judge.Name, "bench judge · ") {
		t.Errorf("judge = %+v", judge)
	}
	if refuses, unattended := c.unattended(done.JudgeID); !refuses || !unattended {
		t.Errorf("judge: refuses prompts %v, unattended %v", refuses, unattended)
	}
	if task, err := os.ReadFile(c.paths.TaskFile(done.JudgeID)); err != nil || !strings.Contains(string(task), "<task>\n"+"prompt,write:out.txt,stop") || !strings.Contains(string(task), "Run 3 (session "+done.Runs[2].Session+")") {
		t.Errorf("the judge's brief = %q, %v", task, err)
	}

	// The ratings teach the router: cheap has three outcomes on small work.
	ch := c.routeDry(task)
	i := slices.IndexFunc(ch.Candidates, func(cand route.Candidate) bool { return cand.Profile == "cheap" })
	if i < 0 || ch.Candidates[i].Outcomes != 3 {
		t.Errorf("after the bench = %+v", ch)
	}

	// The playbook is a file the user can read and edit, without the
	// judge's repeat, and the next session on the profile hears it.
	data, err := os.ReadFile(filepath.Join(c.paths.Playbooks(), "cheap.md"))
	if err != nil || !slices.Equal(parsePlaybook(string(data)), []string{"Read a file before you edit it.", "Run the tests before you finish."}) {
		t.Fatalf("playbook = %q, %v", data, err)
	}
	var sr ipc.SpawnResult
	if err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "cheap"}, &sr); err != nil {
		t.Fatal(err)
	}
	c.waitState(sr.Session.ID, ipc.StateWorking)
	// Someone may answer this one's prompts, so it asks.
	if refuses, unattended := c.unattended(sr.Session.ID); refuses || unattended {
		t.Errorf("a session started by hand: refuses prompts %v, unattended %v", refuses, unattended)
	}
	if told := c.startNoteOf(sr.Session.ID); !strings.Contains(told, "These rules for +cheap come from judging earlier work on it; follow them on every task:\n- Read a file before you edit it.\n- Run the tests before you finish.") {
		t.Errorf("a session on +cheap was told %q", told)
	}
	// One on another profile is not.
	other := c.spawn("prompt")
	c.waitState(other.ID, ipc.StateWorking)
	if told := c.startNoteOf(other.ID); strings.Contains(told, "These rules") {
		t.Errorf("a session on +fake was told %q", told)
	}
}

// unattended reports whether session id's settings refuse what would wait
// on a permission prompt, and whether its spawn was recorded as one no one
// watches.
func (c *coreEnv) unattended(id string) (refuses, unattended bool) {
	c.t.Helper()
	data, err := os.ReadFile(filepath.Join(c.paths.Session(id), claude.SettingsFile))
	if err != nil {
		c.t.Fatalf("the settings of %s: %v", id, err)
	}
	var s struct {
		Permissions struct {
			Deny bool `json:"denyInsteadOfPrompting"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		c.t.Fatal(err)
	}
	for _, e := range c.events(id) {
		if e.Kind == event.SpawnRequested && e.Data["unattended"] == true {
			unattended = true
		}
	}
	return s.Permissions.Deny, unattended
}

// startNoteOf is what a session was told as it started.
func (c *coreEnv) startNoteOf(id string) string {
	c.t.Helper()
	told := ""
	for _, r := range c.agentLog(id) {
		if r["t"] != "hook" || r["event"] != "SessionStart" {
			continue
		}
		var out struct {
			HookSpecificOutput struct {
				AdditionalContext string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(fmt.Sprint(r["stdout"])), &out); err == nil {
			told = out.HookSpecificOutput.AdditionalContext
		}
	}
	return told
}

func TestParseVerdict(t *testing.T) {
	good := "Judged.\n```json\n{\"runs\": [{\"run\": 2, \"score\": 50}, {\"run\": 1, \"score\": 99.6, \"missed\": \"x\"}], \"final\": \"f\", \"playbook\": [\"r\"]}\n```"
	v, err := parseVerdict(good, 2)
	if err != nil || len(v.Runs) != 2 || v.Final != "f" || !slices.Equal(v.Playbook, []string{"r"}) {
		t.Fatalf("verdict = %+v, %v", v, err)
	}
	// The last block counts, and bare JSON does too.
	if v, err := parseVerdict("```json\n{\"runs\": [{\"run\": 1, \"score\": 1}]}\n```\nthen\n```\n{\"runs\": [{\"run\": 1, \"score\": 2}]}\n```", 1); err != nil || v.Runs[0].Score != 2 {
		t.Errorf("last block = %+v, %v", v, err)
	}
	if v, err := parseVerdict(`{"runs": [{"run": 1, "score": 3}]}`, 1); err != nil || v.Runs[0].Score != 3 {
		t.Errorf("bare = %+v, %v", v, err)
	}
	for text, why := range map[string]string{
		"no json here":                                   "no json verdict",
		"```json\n{\"runs\": []}\n```":                   "no json verdict",
		`{"runs": [{"run": 1, "score": 3}]}`:             "run 2 has no score",
		`{"runs": [{"run": 3, "score": 3}]}`:             "not one of the 2 runs",
		`{"runs": [{"run": 1, "score": 3}, {"run": 1}]}`: "scored twice",
		`{"runs": [{"run": 1, "score": 101}]}`:           "not from 0 to 100",
	} {
		if _, err := parseVerdict(text, 2); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("parseVerdict(%q) = %v, want %q", text, err, why)
		}
	}
}

func TestCleanRules(t *testing.T) {
	got := cleanRules([]string{" - Read first. ", "", "read first.", "* Test\nit", strings.Repeat("x", 400), "five"}, 4)
	if len(got) != 4 || got[0] != "Read first." || got[1] != "Test it" || len([]rune(got[2])) > maxRule || got[3] != "five" {
		t.Errorf("rules = %q", got)
	}
	if got := parsePlaybook("# Playbook\n\nIntro - not a rule.\n- one\n-two\n- \n  - nested\n- three  \n"); !slices.Equal(got, []string{"one", "three"}) {
		t.Errorf("parsed = %q", got)
	}
	if fileSafe("llama:Ternary-2.9B/x") != "llama_Ternary-2.9B_x" {
		t.Error(fileSafe("llama:Ternary-2.9B/x"))
	}
}

// A judged bench closes its runs and its judge, with bench.close (on by
// default): they go to the history, from which reopen brings one back, and
// the bench keeps their scores and what they missed. A run you pinned
// stays open.
func TestAJudgedBenchClosesItsSessions(t *testing.T) {
	say := filepath.Join(t.TempDir(), "verdict.md")
	verdict := "```json\n" + `{"runs": [{"run": 1, "score": 80, "missed": "no doc comment"}, {"run": 2, "score": 30, "missed": "no tests"}], "final": "run 1, in my folder", "playbook": ["Test every case the task names."]}` + "\n```\n"
	if err := os.WriteFile(say, []byte(verdict), 0o600); err != nil {
		t.Fatal(err)
	}
	c := setupWith(t, benching(t, say))
	var r ipc.BenchResult
	if err := c.call("bench", ipc.BenchArgs{Task: "prompt,write:out.txt,stop\nfix the typo in README", Profile: "cheap", Runs: 2}, &r); err != nil {
		t.Fatal(err)
	}
	runs := r.Bench.Runs
	if err := c.call(ipc.OpPin, ipc.ParkArgs{Session: runs[1].Session}, nil); err != nil {
		t.Fatal(err)
	}
	done := c.waitBench(r.Bench.ID)
	if done.State != ipc.BenchDone || done.Runs[0].Score == nil || *done.Runs[0].Score != 80 || done.Runs[1].Missed != "no tests" {
		t.Fatalf("judged bench = %+v", done)
	}
	// The verdict closes them as the sweep goes on.
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		_, run1 := c.row(runs[0].Session)
		_, judge := c.row(done.JudgeID)
		if !run1 && !judge {
			break
		}
	}
	for name, id := range map[string]string{"run 1": runs[0].Session, "the judge": done.JudgeID} {
		if s, ok := c.row(id); ok {
			t.Errorf("%s is still open: %+v", name, s)
		}
	}
	if s, ok := c.row(runs[1].Session); !ok || !s.Pinned {
		t.Errorf("the pinned run = %+v, %v; want it open", s, ok)
	}
	// Each counts for the router as before, and comes back with reopen.
	ch := c.routeDry("prompt,write:out.txt,stop\nfix the typo in README")
	i := slices.IndexFunc(ch.Candidates, func(cand route.Candidate) bool { return cand.Profile == "cheap" })
	if i < 0 || ch.Candidates[i].Outcomes != 2 {
		t.Errorf("after the bench = %+v", ch)
	}
	if s, err := c.reopen(runs[0].Session); err != nil || s.ID != runs[0].Session {
		t.Errorf("reopen run 1 = %+v, %v", s, err)
	}
}
