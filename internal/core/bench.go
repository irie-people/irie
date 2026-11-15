package core

// Benches (DESIGN §6.6). A bench runs one task on several sessions of one
// profile, each on its own, here or on a peer, so as to learn how well
// the profile does such work and to get the task done well all the same:
// once the runs are through, a judge on a stronger profile scores each run,
// makes the one result from the best of them, filling what all of them
// missed, and rewrites the profile's playbook, the rules its agents read
// at the start of every task, so that the next runs avoid the misses it
// saw. Each run's score rates it, good or bad, which teaches the router
// the profile's accuracy for the task's class.
//
// This core keeps the benches it started, in benches/, and drives each
// from its sweep: a restarted core carries on with them. A bench on a peer
// starts its sessions there as typed tasks, so the peer reads its own
// profiles and repos, and the playbook goes in each run's task; its
// ratings teach this core's router as well as the peer's.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/route"
)

const (
	// benchEvery is how many ticks pass between looks at the benches.
	benchEvery = 5
	// briefMessage is the most of a run's last message the judge's brief
	// quotes; the judge can read the rest in the run's folder.
	briefMessage = 6000
	// maxRule is the longest a playbook rule may be.
	maxRule = 300
	// verdictAsks is how often a judge whose verdict does not parse is
	// asked again for it.
	verdictAsks = 2
	// verdictWait is how long a judge asked again has to answer.
	verdictWait = 30 * time.Minute
)

// benchFile is a bench as the core keeps it: what clients see, and what
// the core needs to carry on with it after a restart.
type benchFile struct {
	ipc.Bench
	Asked   int       `json:"asked,omitempty"`   // how often the judge was asked again for its verdict
	AskedAt time.Time `json:"asked_at,omitzero"` // when it last was
	Unread  string    `json:"unread,omitempty"`  // the last message of the judge's that did not parse
	// Playbook is the profile's playbook as the runs on a peer got it.
	Playbook []string `json:"playbook,omitempty"`
}

// benchState is the benches this core started. Its lock is taken before
// spawnMu and mu, and never while either is held.
type benchState struct {
	mu   sync.Mutex
	all  map[string]*benchFile
	pb   map[string]playbookEntry // the playbooks, by profile, as last read; under c.mu
	busy atomic.Bool              // a sweep runs
}

// newBenchID names a bench as newID names a session, with a b.
func newBenchID(t time.Time) string { return "b" + newID(t)[1:] }

// benchFor reads where a bench's runs go: the profile, the judge and the
// peer, if any. A bench runs one profile, never auto: its point is to
// measure that profile.
func (c *Core) benchFor(a ipc.BenchArgs) (profile, judge string, p *peerState, err error) {
	cfg := c.cfg()
	profile = strings.TrimPrefix(strings.TrimSpace(a.Profile), "+")
	if profile == "" {
		for _, name := range cfg.Route.Profiles {
			if q, ok := cfg.Profiles[name]; ok && q.Local != nil {
				profile = name
				break
			}
		}
		if profile == "" {
			return "", "", nil, ipc.Errorf(ipc.CodeBadArgs, "name the profile to bench, such as +local: route.profiles has no local one")
		}
	}
	if profile == config.Auto {
		return "", "", nil, ipc.Errorf(ipc.CodeBadArgs, "a bench measures one profile; name it rather than +%s", config.Auto)
	}
	judge = strings.TrimPrefix(strings.TrimSpace(a.Judge), "+")
	if judge == "" {
		judge = cfg.Bench.Judge
	}
	if a.Host == "" {
		for _, name := range []string{profile, judge} {
			if _, ok := c.profile(name); !ok {
				return "", "", nil, ipc.Errorf(ipc.CodeNotFound, "no profile +%s", name)
			}
		}
		return profile, judge, nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var hit []string
	for _, name := range c.peerNames() {
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(a.Host)) {
			hit = append(hit, name)
		}
	}
	switch {
	case len(hit) == 0:
		return "", "", nil, ipc.Errorf(ipc.CodeNotFound, "no peer %q; the peers are %s", a.Host, strings.Join(c.peerNames(), ", "))
	case len(hit) > 1:
		return "", "", nil, ipc.Errorf(ipc.CodeAmbiguous, "%q could be %s", a.Host, strings.Join(hit, " or "))
	}
	p = c.peers[hit[0]]
	if !p.up {
		return "", "", nil, ipc.Errorf(ipc.CodePeerDown, "%s isn't answering just now, so the bench didn't start", p.name)
	}
	if why := c.hooksDiffer(p); why != "" {
		return "", "", nil, ipc.Errorf(ipc.CodeBadArgs, "Not started on %s: %s.", p.name, why)
	}
	return profile, judge, p, nil
}

// benchOp is the bench op: it starts a bench of a.Task, or lists them.
func (c *Core) benchOp(a ipc.BenchArgs) (ipc.BenchResult, error) {
	if strings.TrimSpace(a.Task) == "" {
		return ipc.BenchResult{Benches: c.benchList()}, nil
	}
	runs := a.Runs
	if runs == 0 {
		runs = c.cfg().Bench.Runs
	}
	if runs < 1 || runs > config.MaxBenchRuns {
		return ipc.BenchResult{}, ipc.Errorf(ipc.CodeBadArgs, "a bench runs from 1 to %d sessions, not %d", config.MaxBenchRuns, runs)
	}
	profile, judge, p, err := c.benchFor(a)
	if err != nil {
		return ipc.BenchResult{}, err
	}
	task := strings.TrimSpace(a.Task)
	class, _ := route.Classify(task, c.cfg().Route.Words)
	now := time.Now()
	b := &benchFile{Bench: ipc.Bench{
		ID: newBenchID(now), Task: task, Profile: profile, Class: class, Repo: a.Repo,
		Judge: judge, State: ipc.BenchRunning, Runs: []ipc.BenchRun{}, Started: now,
	}}
	if p != nil {
		b.Host = p.name
		c.mu.Lock()
		b.Playbook = c.playbook(profile)
		c.mu.Unlock()
	}
	// The runs start one after another, as every spawn does; the first may
	// load a local model, which the others then share.
	for i := range runs {
		name := cleanText(fmt.Sprintf("bench %d/%d · %s", i+1, runs, title(task)), maxName)
		s, err := c.benchStart(p, b, b.runTask(i, runs), profile, name)
		if err != nil {
			b.State, b.Problem = ipc.BenchFailed, fmt.Sprintf("run %d of %d did not start: %v", i+1, runs, err)
			t := time.Now()
			b.Ended = &t
			c.keepBench(b)
			return ipc.BenchResult{}, fmt.Errorf("bench %s: %s", b.ID, b.Problem)
		}
		b.Runs = append(b.Runs, ipc.BenchRun{Session: s.ID})
	}
	c.keepBench(b)
	c.log.Info("bench started", "bench", b.ID, "profile", profile, "class", class, "runs", runs, "host", b.Host)
	out := b.Bench
	return ipc.BenchResult{Bench: &out}, nil
}

// runTask is what run i of n is given. Here it is the task as typed, and
// the profile's playbook comes with the start of its session (startNote).
// On a peer it goes as typed text, so its first line holds no @ or + words
// the peer would read, and the playbook goes in it.
func (b *benchFile) runTask(i, n int) string {
	if b.Host == "" {
		return b.Task
	}
	var t strings.Builder
	fmt.Fprintf(&t, "[%s bench] Run %d of %d.", brand.Name, i+1, n)
	if len(b.Playbook) > 0 {
		t.WriteString(" " + playbookIntro(b.Profile) + "\n")
		for _, r := range b.Playbook {
			t.WriteString("- " + r + "\n")
		}
	} else {
		t.WriteString("\n")
	}
	t.WriteString("\nThe task:\n\n" + b.Task)
	return t.String()
}

// benchStart starts one session of bench b: here, or on peer p as a typed
// task, named there as here. No one watches a run or the judge, so each
// is unattended: a tool call that would wait on a permission prompt is
// refused and its turn goes on, rather than wait out bench.timeout.
func (c *Core) benchStart(p *peerState, b *benchFile, task, profile, name string) (ipc.Session, error) {
	if p == nil {
		return c.spawn(ipc.SpawnArgs{Task: task, Profile: profile, Repo: b.Repo, Name: name, Unattended: true}, true)
	}
	r, err := c.inputOn(p, ipc.InputArgs{Text: task, Profile: profile, Repo: b.Repo, Unattended: true})
	if err != nil {
		return ipc.Session{}, err
	}
	if r.Session == nil || r.Did != ipc.InputSpawned {
		return ipc.Session{}, fmt.Errorf("%s did not start it: %s", p.name, r.Text)
	}
	s := *r.Session
	if raw, err := json.Marshal(ipc.RenameArgs{Session: s.ID, Name: name}); err == nil {
		if _, err := c.Call(ipc.OpRename, raw); err == nil {
			s.Name = name
		}
	}
	return s, nil
}

// call runs op as a client's request would, so a session of a peer's goes
// on to its own core.
func (c *Core) call(op string, args, out any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	res, err := c.Call(op, raw)
	if err != nil || out == nil {
		return err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// rowOf is a session's row as this core lists it, its own or a peer's.
// The caller holds c.mu.
func (c *Core) rowOf(id string) (ipc.Session, bool) {
	if x := c.sessions[id]; x != nil {
		return x.wire(), true
	}
	if p, rest, ok := c.splitPeerID(id); ok {
		if s, ok := p.rows[rest]; ok {
			s.ID = id
			if !p.up {
				t := p.since
				s.HostDown = &t
			}
			return s, true
		}
	}
	return ipc.Session{}, false
}

// through reports whether a session's work is over for a bench: its turn
// ended with no helper at work, or it stopped, on a machine that answers.
func through(s ipc.Session) bool {
	return s.HostDown == nil && ended(s) && (s.Subs == nil || s.State == ipc.StateExited || s.State == ipc.StateFailed)
}

// benchSweep moves each open bench on: a judge once its runs are through,
// ratings and a playbook once its judge is.
func (c *Core) benchSweep() {
	if !c.benches.busy.CompareAndSwap(false, true) {
		return
	}
	defer c.benches.busy.Store(false)
	// Each step works on a copy, which takes the bench's place once it
	// changed, so a listing never reads a bench half changed.
	c.benches.mu.Lock()
	var live []*benchFile
	for _, b := range c.benches.all {
		if b.State == ipc.BenchRunning || b.State == ipc.BenchJudging {
			cp := *b
			cp.Runs = slices.Clone(b.Runs)
			live = append(live, &cp)
		}
	}
	c.benches.mu.Unlock()
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	for _, b := range live {
		if c.stepBench(b, time.Now()) {
			c.keepBench(b)
		}
	}
}

// stepBench takes bench b a step on, and reports whether it changed.
func (c *Core) stepBench(b *benchFile, now time.Time) bool {
	switch b.State {
	case ipc.BenchRunning:
		return c.stepRuns(b, now)
	case ipc.BenchJudging:
		return c.stepJudge(b, now)
	}
	return false
}

// stepRuns starts the judge once every run is through, or once
// bench.timeout has passed, when the runs still at work are stopped
// (Escape ends a turn) and judged as they stand.
func (c *Core) stepRuns(b *benchFile, now time.Time) bool {
	c.mu.Lock()
	var atWork []int
	for i, r := range b.Runs {
		if s, ok := c.rowOf(r.Session); ok && !through(s) {
			atWork = append(atWork, i)
		}
	}
	c.mu.Unlock()
	if len(atWork) > 0 && now.Sub(b.Started) < c.cfg().Bench.Timeout {
		return false
	}
	for _, i := range atWork {
		b.Runs[i].Stopped = true
		if err := c.call(ipc.OpKeys, ipc.KeysArgs{Session: b.Runs[i].Session, Keys: []string{"Escape"}}, nil); err != nil {
			c.log.Warn("bench stop", "bench", b.ID, "session", b.Runs[i].Session, "err", err)
		}
	}
	var p *peerState
	if b.Host != "" {
		c.mu.Lock()
		p = c.peers[b.Host]
		up := p != nil && p.up
		c.mu.Unlock()
		if !up {
			return len(atWork) > 0 // wait for it to answer again
		}
	}
	brief := c.judgeBrief(b)
	s, err := c.benchStart(p, b, brief, b.Judge, cleanText("bench judge · "+title(b.Task), maxName))
	if err != nil {
		b.State, b.Problem = ipc.BenchFailed, "the judge did not start: "+err.Error()
		b.Ended = &now
		return true
	}
	b.State, b.JudgeID, b.Judged = ipc.BenchJudging, s.ID, &now
	c.log.Info("bench judging", "bench", b.ID, "judge", s.ID, "stopped", len(atWork))
	return true
}

// stepJudge reads the judge's verdict once its turn is through: it rates
// the runs and keeps the playbook. A verdict that does not parse is asked
// for again, a few times.
func (c *Core) stepJudge(b *benchFile, now time.Time) bool {
	c.mu.Lock()
	s, ok := c.rowOf(b.JudgeID)
	c.mu.Unlock()
	fail := func(problem string) bool {
		b.State, b.Problem, b.Ended = ipc.BenchFailed, problem, &now
		c.log.Warn("bench failed", "bench", b.ID, "problem", problem)
		return true
	}
	switch {
	case !ok:
		return fail("the judge was closed before it gave its verdict; rate the runs yourself")
	case s.State == ipc.StateExited || s.State == ipc.StateFailed:
		return fail("the judge stopped before it gave its verdict (" + s.State + "); rate the runs yourself")
	case !through(s):
		return false
	}
	var res ipc.ResultResult
	if err := c.call(ipc.OpResult, ipc.ResultArgs{Session: b.JudgeID}, &res); err != nil {
		c.log.Warn("bench verdict", "bench", b.ID, "err", err)
		return false
	}
	if b.Asked > 0 && res.Text == b.Unread {
		// Asked again, it has not answered yet.
		if now.Sub(b.AskedAt) > verdictWait {
			return fail("the judge did not give a verdict that parses; rate the runs yourself")
		}
		return false
	}
	v, err := parseVerdict(res.Text, len(b.Runs))
	if err != nil {
		if b.Asked >= verdictAsks {
			return fail("the judge's verdict did not parse (" + err.Error() + "); rate the runs yourself")
		}
		ask := fmt.Sprintf("[%s] Your verdict did not parse: %v. End your reply with the verdict alone, as one fenced json block in the shape asked, with one entry for each of the %d runs.", brand.Name, err, len(b.Runs))
		if err := c.call(ipc.OpSend, ipc.SendArgs{Session: b.JudgeID, Text: ask}, nil); err != nil {
			c.log.Warn("bench ask", "bench", b.ID, "err", err)
			return false
		}
		b.Asked, b.AskedAt, b.Unread = b.Asked+1, now, res.Text
		return true
	}
	c.applyVerdict(b, v, now)
	return true
}

// verdict is what a judge says of a bench.
type verdict struct {
	Runs []struct {
		Run    int     `json:"run"`
		Score  float64 `json:"score"`
		Missed string  `json:"missed"`
	} `json:"runs"`
	Final    string   `json:"final"`
	Playbook []string `json:"playbook"`
}

var jsonFence = regexp.MustCompile("(?s)```(?:json|JSON)?[ \t]*\r?\n(.*?)\r?\n[ \t]*```")

// parseVerdict reads a judge's verdict: the last fenced json block of its
// message, else the message itself, with one score from 0 to 100 for each
// of n runs.
func parseVerdict(text string, n int) (verdict, error) {
	var candidates []string
	blocks := jsonFence.FindAllStringSubmatch(text, -1)
	for i := len(blocks) - 1; i >= 0; i-- {
		candidates = append(candidates, blocks[i][1])
	}
	candidates = append(candidates, strings.TrimSpace(text))
	var v verdict
	var err error
	for _, cand := range candidates {
		v = verdict{}
		if err = json.Unmarshal([]byte(cand), &v); err == nil && len(v.Runs) > 0 {
			break
		}
		if err == nil {
			err = errors.New("it has no runs")
		}
	}
	if err != nil {
		return verdict{}, fmt.Errorf("no json verdict: %v", err)
	}
	seen := make([]bool, n)
	for _, r := range v.Runs {
		switch {
		case r.Run < 1 || r.Run > n:
			return verdict{}, fmt.Errorf("run %d is not one of the %d runs", r.Run, n)
		case seen[r.Run-1]:
			return verdict{}, fmt.Errorf("run %d is scored twice", r.Run)
		case r.Score < 0 || r.Score > 100:
			return verdict{}, fmt.Errorf("run %d's score %v is not from 0 to 100", r.Run, r.Score)
		}
		seen[r.Run-1] = true
	}
	if i := slices.Index(seen, false); i >= 0 {
		return verdict{}, fmt.Errorf("run %d has no score", i+1)
	}
	return v, nil
}

// applyVerdict rates each run by its score, keeps the judge's playbook,
// and closes the bench.
func (c *Core) applyVerdict(b *benchFile, v verdict, now time.Time) {
	pass := c.cfg().Bench.Pass
	for _, r := range v.Runs {
		run := &b.Runs[r.Run-1]
		score := int(r.Score + 0.5)
		run.Score, run.Missed = &score, cleanText(r.Missed, 500)
		run.Verdict = ipc.Bad
		if score >= pass {
			run.Verdict = ipc.Good
		}
		c.rateRun(b, *run)
	}
	if v.Playbook != nil {
		rules := cleanRules(v.Playbook, c.cfg().Bench.Rules)
		if err := c.setPlaybook(b.Profile, rules, b.ID, now); err != nil {
			c.log.Warn("playbook", "profile", b.Profile, "err", err)
		}
	}
	c.mu.Lock()
	b.Rules = len(c.playbook(b.Profile))
	c.mu.Unlock()
	b.Final, b.State, b.Ended = cleanText(v.Final, 2000), ipc.BenchDone, &now
	c.log.Info("bench judged", "bench", b.ID, "profile", b.Profile, "class", b.Class, "scores", scores(b.Runs), "rules", b.Rules)
}

// rateRun rates one run as its score says. A run here is rated as the user
// rates a session; a peer's is rated on its own core, and its outcome kept
// here too, with the bench, so that this core's router learns from it.
func (c *Core) rateRun(b *benchFile, r ipc.BenchRun) {
	if err := c.call(ipc.OpRate, ipc.RateArgs{Session: r.Session, Verdict: r.Verdict}, nil); err != nil {
		c.log.Warn("bench rate", "bench", b.ID, "session", r.Session, "err", err)
	}
	if b.Host == "" {
		return
	}
	c.mu.Lock()
	c.benchOutcome(b, r)
	c.mu.Unlock()
}

// benchOutcome is a peer's run of bench b as an outcome of this core's,
// so its rating counts for the profile here. The caller holds c.mu.
func (c *Core) benchOutcome(b *benchFile, r ipc.BenchRun) {
	if r.Verdict == "" {
		return
	}
	o := c.outcome(r.Session)
	o.profile, o.class, o.rated, o.spawned = b.Profile, b.Class, r.Verdict, b.Started
}

func scores(runs []ipc.BenchRun) string {
	var out []string
	for _, r := range runs {
		if r.Score == nil {
			out = append(out, "-")
		} else {
			out = append(out, fmt.Sprint(*r.Score))
		}
	}
	return strings.Join(out, "/")
}

// judgeBrief is the judge's task: what the runs were given, where each
// worked and what it said last, and the four things to do.
func (c *Core) judgeBrief(b *benchFile) string {
	n := len(b.Runs)
	var t strings.Builder
	fmt.Fprintf(&t, "[%s bench] You judge a bench: %d runs of one task, each by its own agent on the same profile, each from the same start and on its own.\n\n", brand.Name, n)
	t.WriteString("The task each run was given:\n\n<task>\n" + b.Task + "\n</task>\n\nThe runs:\n")
	for i, r := range b.Runs {
		c.mu.Lock()
		s, ok := c.rowOf(r.Session)
		c.mu.Unlock()
		fmt.Fprintf(&t, "\nRun %d (session %s)", i+1, r.Session)
		switch {
		case !ok:
			t.WriteString(" was closed before the judging; judge it from what it left, if you can find it, else score it 0.\n")
			continue
		case s.Worktree != "":
			fmt.Fprintf(&t, " worked in the worktree %s, on branch %s.", s.Worktree, s.Branch)
		case s.Cwd != "":
			fmt.Fprintf(&t, " worked in %s.", s.Cwd)
		}
		if r.Stopped {
			fmt.Fprintf(&t, " It was still at work after %s, so it was stopped: judge it as it stands.", c.cfg().Bench.Timeout)
		} else {
			fmt.Fprintf(&t, " Its turn ended (%s).", s.State)
		}
		var res ipc.ResultResult
		if err := c.call(ipc.OpResult, ipc.ResultArgs{Session: r.Session}, &res); err == nil && strings.TrimSpace(res.Text) != "" {
			msg := strings.TrimSpace(res.Text)
			if len(msg) > briefMessage {
				msg = msg[:briefMessage] + " …"
			}
			t.WriteString(" Its last message:\n<message>\n" + msg + "\n</message>\n")
		} else {
			t.WriteString(" It left no last message.\n")
		}
	}
	t.WriteString("\nDo these four things, in order.\n\n")
	t.WriteString("1. Judge each run on its own. Read what it changed: in a git checkout, `git -C <its folder> status` and `git -C <its folder> diff` show what it did not commit, and `git -C <its folder> log` what it did; elsewhere read its folder. Run its tests or its build where there are any. Score it from 0 to 100 for how much of the task it got right, as a careful reviewer grades a pull request: 100 is complete and correct, 70 is right with small gaps, below 50 is wrong or far from done. Note what it missed or got wrong.\n\n")
	where := "in the folder you start in"
	if b.Repo != "" {
		where = "in your own worktree, which starts where theirs did, and commit it on your branch"
	}
	fmt.Fprintf(&t, "2. Make the final result %s: start from the best run, bring in what the others got right, and fill what all of them missed, until the task is done and checked as you checked theirs.\n\n", where)
	fmt.Fprintf(&t, "3. Rewrite the playbook for their profile: short rules, one line each, that its agents read before every task, so that they stop making the mistakes you saw. Keep the rules that still help, drop any that did not, and add one for each mistake a rule would have prevented. Write rules about how to work, such as reading a file before editing it, running the tests or doing every part of the task, not about this one task. At most %d rules.", c.cfg().Bench.Rules)
	rules := b.Playbook
	if b.Host == "" {
		c.mu.Lock()
		rules = c.playbook(b.Profile)
		c.mu.Unlock()
	}
	if len(rules) == 0 {
		t.WriteString(" The playbook is empty so far.\n\n")
	} else {
		t.WriteString(" The playbook now:\n<playbook>\n")
		for _, r := range rules {
			t.WriteString("- " + r + "\n")
		}
		t.WriteString("</playbook>\n\n")
	}
	t.WriteString("4. End your last message with your verdict as one fenced json block, in exactly this shape, with one entry for each run:\n\n```json\n{\"runs\": [")
	for i := range n {
		if i > 0 {
			t.WriteString(", ")
		}
		fmt.Fprintf(&t, "{\"run\": %d, \"score\": 0, \"missed\": \"what it missed or got wrong, in a line\"}", i+1)
	}
	t.WriteString("], \"final\": \"what the final result is, and where it is\", \"playbook\": [\"a rule\", \"another rule\"]}\n```\n")
	return t.String()
}

// keepBench writes bench b to benches/ and keeps it in memory.
func (c *Core) keepBench(b *benchFile) {
	c.benches.mu.Lock()
	if c.benches.all == nil {
		c.benches.all = map[string]*benchFile{}
	}
	c.benches.all[b.ID] = b
	data, err := json.MarshalIndent(b, "", "  ")
	c.benches.mu.Unlock()
	if err == nil {
		dir := c.paths.Benches()
		if err = os.MkdirAll(dir, 0o700); err == nil {
			err = filetx.WriteFile(filepath.Join(dir, b.ID+".json"), append(data, '\n'), 0o600)
		}
	}
	if err != nil {
		c.log.Warn("bench", "bench", b.ID, "err", err)
	}
}

// loadBenches reads the benches this core started, and counts the ratings
// of their runs on peers as outcomes here, over the journal's days, as the
// journal's own outcomes are.
func (c *Core) loadBenches() {
	entries, err := os.ReadDir(c.paths.Benches())
	if err != nil {
		if !os.IsNotExist(err) {
			c.log.Warn("benches", "err", err)
		}
		return
	}
	keep := time.Duration(c.cfg().Journal.KeepDays) * 24 * time.Hour
	all := map[string]*benchFile{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.paths.Benches(), e.Name()))
		if err != nil {
			continue
		}
		var b benchFile
		if json.Unmarshal(data, &b) != nil || b.ID == "" {
			c.log.Warn("benches", "file", e.Name(), "err", "unreadable")
			continue
		}
		all[b.ID] = &b
	}
	c.benches.mu.Lock()
	c.benches.all = all
	c.benches.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, b := range all {
		if b.Host == "" || keep > 0 && time.Since(b.Started) > keep {
			continue
		}
		for _, r := range b.Runs {
			c.benchOutcome(b, r)
		}
	}
}

// benchList is every bench, the newest first.
func (c *Core) benchList() []ipc.Bench {
	c.benches.mu.Lock()
	defer c.benches.mu.Unlock()
	out := make([]ipc.Bench, 0, len(c.benches.all))
	for _, b := range c.benches.all {
		out = append(out, b.Bench)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}
