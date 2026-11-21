package ipc

import "time"

// OpBench starts a bench, or lists the benches (DESIGN §6.6): several
// sessions of one profile do the same task, each on its own, then a judge
// on a stronger profile scores each, makes the one result from the best of
// them, and rewrites the profile's playbook, the rules its agents read at
// the start of every task. Each run's score rates it, which teaches the
// router how accurate the profile is.
const OpBench = "bench"

// BenchArgs start a bench of Task, or with no Task list the benches.
// Profile is the runs' profile ("" = the first local one in
// route.profiles); Repo the repo they run in, each in a worktree of its
// own ("" = a scratch folder each); From a git repo each run, and the
// judge, starts from instead, in a copy of its last commit in a scratch
// folder of its own, on the machine they run on; Host a peer to run them
// on ("" = here); Runs
// how many (0 = bench.runs); Judge the judge's profile ("" =
// bench.judge); and Timeout how long the runs may work before the ones
// still at work are stopped and judged as they stand (0 = bench.timeout).
type BenchArgs struct {
	Task    string        `json:"task,omitempty"`
	Profile string        `json:"profile,omitempty"`
	Repo    string        `json:"repo,omitempty"`
	From    string        `json:"from,omitempty"`
	Host    string        `json:"host,omitempty"`
	Runs    int           `json:"runs,omitempty"`
	Judge   string        `json:"judge,omitempty"`
	Timeout time.Duration `json:"timeout,omitempty"`
}

// BenchResult is the bench started, or every bench, the newest first.
// Shown says Bench is one asked after by its id, which a task of nothing
// but a bench's id does, rather than one that started.
type BenchResult struct {
	Bench   *Bench  `json:"bench,omitempty"`
	Benches []Bench `json:"benches,omitempty"`
	Shown   bool    `json:"shown,omitempty"`
}

// A bench's states.
const (
	BenchRunning = "running" // its runs are at work
	BenchJudging = "judging" // its judge is at work
	BenchDone    = "done"    // judged: its runs are rated and the result made
	BenchFailed  = "failed"  // it stopped short; Problem says why
)

// Bench is one bench. Its sessions' ids are as this core lists them: a
// peer's are its name, a slash and its own id.
type Bench struct {
	ID      string     `json:"id"`
	Task    string     `json:"task"`
	Profile string     `json:"profile"`
	Class   string     `json:"class"` // the task's class, which its ratings teach the router
	Host    string     `json:"host,omitempty"`
	Repo    string     `json:"repo,omitempty"`
	From    string     `json:"from,omitempty"` // the git repo each session starts from, in a copy of its own
	Judge   string     `json:"judge"`          // the judge's profile
	State   string     `json:"state"`
	Runs    []BenchRun `json:"runs"`
	JudgeID string     `json:"judge_id,omitempty"` // the judge's session
	Final   string     `json:"final,omitempty"`    // what the judge made, and where
	Rules   int        `json:"rules,omitempty"`    // how many rules the playbook has after it
	Problem string     `json:"problem,omitempty"`
	Started time.Time  `json:"started"`
	Judged  *time.Time `json:"judged,omitempty"` // when the judge started
	Ended   *time.Time `json:"ended,omitempty"`
	// Timeout is how long its runs had: a run still at work after it was
	// stopped. A bench kept before benches had their own has none, and
	// went by bench.timeout.
	Timeout time.Duration `json:"timeout,omitempty"`
}

// BenchRun is one run of a bench: its session and how the judge scored it.
type BenchRun struct {
	Session string `json:"session"`
	Score   *int   `json:"score,omitempty"` // 0 to 100
	Missed  string `json:"missed,omitempty"`
	Verdict string `json:"verdict,omitempty"` // good or bad, from the score
	Stopped bool   `json:"stopped,omitempty"` // still at work at the bench's timeout, so stopped and judged as it stood
}
