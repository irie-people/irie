package core

// Undo and redo (DESIGN §4.5). Every action the user takes that Irie can
// take back is recorded as a step of one linear history, 100 deep, kept in
// state.json so it survives a restart. A new step clears the redo stack;
// undo and redo themselves record nothing.
//
// A kind of action plugs in with registerAction: what its undo does and what
// its redo does, each an effect with a check and a do. The check says what
// makes the effect uncertain now, such as a session that was renamed, is
// still at work or is gone; then nothing changes, and the core asks the
// user what to do instead of guessing. The answer names the question it
// answers, so a stale one is refused.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/harness"
	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
)

// maxHistory is how many steps the history keeps, undo and redo together.
const maxHistory = 100

// stateVersion is the version of state.json this core writes.
const stateVersion = 1

// Directions.
const (
	dirUndo = "undo"
	dirRedo = "redo"
)

// action is one step of the history: an action the user took, on one
// session, identified by its id and never by its row.
type action struct {
	ID      string         `json:"id"`      // the step's own id, which a question names
	Kind    string         `json:"kind"`    // its actionKind
	Session string         `json:"session"` // the session it acted on
	Name    string         `json:"name"`    // the session's name as the step left it
	Label   string         `json:"label"`   // what the header shows, as `close "api-fix"`
	At      time.Time      `json:"at"`
	Data    map[string]any `json:"data,omitempty"` // what the kind needs to undo it, such as when it closed
}

func (a action) with(k string, v any) action {
	a.Data = maps.Clone(a.Data)
	if a.Data == nil {
		a.Data = map[string]any{}
	}
	if v == nil {
		delete(a.Data, k)
	} else {
		a.Data[k] = v
	}
	return a
}

func (a action) str(k string) string {
	s, _ := a.Data[k].(string)
	return s
}

// history is the undo and redo stacks, the next step to take last in each.
type history struct {
	Undo []action `json:"undo"`
	Redo []action `json:"redo"`
}

// stack is the stack dir takes from, and other the one it puts on.
func (h *history) stack(dir string) (from, to *[]action) {
	if dir == dirUndo {
		return &h.Undo, &h.Redo
	}
	return &h.Redo, &h.Undo
}

// push records a new step: it clears the redo stack and drops the oldest
// step past maxHistory.
func (h *history) push(a action) {
	h.Undo = append(h.Undo, a)
	if n := len(h.Undo) - maxHistory; n > 0 {
		h.Undo = slices.Delete(h.Undo, 0, n)
	}
	h.Redo = nil
}

// top is the step dir takes next.
func (h *history) top(dir string) (action, bool) {
	from, _ := h.stack(dir)
	if len(*from) == 0 {
		return action{}, false
	}
	return (*from)[len(*from)-1], true
}

// finish moves the step id, which dir has just taken, onto the other stack
// as a. When a new step came in meanwhile, id is no longer next: it leaves
// the history, which stays linear. It reports whether the step moved.
func (h *history) finish(dir, id string, a action) bool {
	from, to := h.stack(dir)
	if t, ok := h.top(dir); ok && t.ID == id {
		*from = (*from)[:len(*from)-1]
		*to = append(*to, a)
		if n := len(h.Undo) + len(h.Redo) - maxHistory; n > 0 {
			*to = slices.Delete(*to, 0, min(n, len(*to)))
		}
		return true
	}
	h.drop(dir, id)
	return false
}

// drop takes the step id out of dir's stack, and reports whether it was there.
func (h *history) drop(dir, id string) bool {
	from, _ := h.stack(dir)
	n := len(*from)
	*from = slices.DeleteFunc(*from, func(a action) bool { return a.ID == id })
	return len(*from) < n
}

// labels are what the header shows: the next undo and redo, or "".
func (h *history) labels() (undo, redo string) {
	if a, ok := h.top(dirUndo); ok {
		undo = a.Label
	}
	if a, ok := h.top(dirRedo); ok {
		redo = a.Label
	}
	return undo, redo
}

// stateFile is state.json. Keys this version does not know, such as a later
// version's, are kept as they are.
type stateFile struct {
	hist  history
	extra map[string]json.RawMessage
}

// readState reads state.json. A missing file is an empty history; so are
// missing stacks, as in a file from before undo. A file that is not JSON is
// moved aside to state.json.bad and the history starts empty.
func readState(path string) (stateFile, error) {
	st := stateFile{extra: map[string]json.RawMessage{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st.extra); err != nil {
		os.Rename(path, path+".bad")
		return stateFile{extra: map[string]json.RawMessage{}}, fmt.Errorf("%s is not JSON, so it was moved to %s.bad: %v", path, path, err)
	}
	var problems []string
	for _, k := range []string{dirUndo, dirRedo} {
		raw, ok := st.extra[k]
		delete(st.extra, k)
		if !ok || string(raw) == "null" {
			continue
		}
		var list []action
		if err := json.Unmarshal(raw, &list); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", k, err))
			continue
		}
		list = slices.DeleteFunc(list, func(a action) bool { return a.ID == "" || a.Kind == "" })
		if k == dirUndo {
			st.hist.Undo = list
		} else {
			st.hist.Redo = list
		}
	}
	delete(st.extra, "version")
	if n := len(st.hist.Undo) - maxHistory; n > 0 {
		st.hist.Undo = st.hist.Undo[n:]
	}
	if n := len(st.hist.Undo) + len(st.hist.Redo) - maxHistory; n > 0 {
		st.hist.Redo = st.hist.Redo[min(n, len(st.hist.Redo)):]
	}
	if problems != nil {
		return st, fmt.Errorf("%s: a stack could not be read and starts empty: %s", path, strings.Join(problems, "; "))
	}
	return st, nil
}

// writeState writes state.json atomically.
func writeState(path string, st stateFile) error {
	out := map[string]any{}
	for k, v := range st.extra {
		out[k] = v
	}
	undo, redo := st.hist.Undo, st.hist.Redo
	if undo == nil {
		undo = []action{}
	}
	if redo == nil {
		redo = []action{}
	}
	out["version"], out[dirUndo], out[dirRedo] = stateVersion, undo, redo
	return writeJSON(path, out)
}

// loadState reads the history at start.
func (c *Core) loadState() {
	st, err := readState(c.paths.StateFile())
	if err != nil {
		c.log.Warn("state.json", "err", err)
	}
	c.steps = st
}

// saveState writes the history. The caller holds c.mu.
func (c *Core) saveState() {
	if err := writeState(c.paths.StateFile(), c.steps); err != nil {
		c.log.Warn("state.json", "err", err)
	}
}

// actionKind is one kind of undoable action: the verb of its label, and
// what its undo and its redo do.
type actionKind struct {
	verb       string
	undo, redo effect
}

// effect is what one direction of an action does.
type effect struct {
	// check lists what makes the effect uncertain now, the most pressing
	// first; none when it can go ahead. The caller holds c.mu.
	check func(c *Core, a action) []problem
	// do takes the effect, without c.mu, and returns the step as it then
	// stands, such as the session's new name. On an error nothing changed.
	do func(c *Core, a action) (action, error)
}

// problem is one reason an effect is uncertain, and what the user may do
// about it besides skipping the step or cancelling.
type problem struct {
	code      string             // stable, such as "busy"; a question's id names it
	question  string             // one line, in plain words
	options   []ipc.ChoiceOption // keys ChoiceApply or ChoiceSettle
	recommend string             // the safest key, which is offered first
}

// actionKinds are the kinds of action the history knows, by name.
var actionKinds = map[string]actionKind{}

// registerAction adds a kind of undoable action. A later action, such as
// rename or park, registers its kind here and calls recordAction once it
// has been done; undo, redo, questions, state.json and the header follow.
func registerAction(kind string, k actionKind) {
	if _, dup := actionKinds[kind]; dup {
		panic("core: action kind registered twice: " + kind)
	}
	actionKinds[kind] = k
}

// Action kinds of sessions: start (spawn), close and reopen.
const (
	actSpawn  = "spawn"
	actClose  = "close"
	actReopen = "reopen"
)

func init() {
	closeEff := effect{check: checkClose, do: doClose}
	reopenEff := effect{check: checkReopen, do: doReopen}
	registerAction(actSpawn, actionKind{verb: "start", undo: closeEff, redo: reopenEff})
	registerAction(actClose, actionKind{verb: "close", undo: reopenEff, redo: closeEff})
	registerAction(actReopen, actionKind{verb: "reopen", undo: closeEff, redo: reopenEff})
}

func label(verb, name string) string { return fmt.Sprintf("%s %q", verb, name) }

func newActionID() string {
	var b [6]byte
	rand.Read(b[:])
	return "a" + hex.EncodeToString(b[:])
}

// recordAction records that the user took an action of kind on session id,
// now done, and tells clients the history changed. It never runs for what
// undo and redo do.
func (c *Core) recordAction(kind, id string, data map[string]any) {
	k, ok := actionKinds[kind]
	if !ok {
		c.log.Warn("history: unknown action kind", "kind", kind)
		return
	}
	c.mu.Lock()
	a := action{ID: newActionID(), Kind: kind, Session: id, Name: c.nameOf(id), At: time.Now().UTC().Truncate(time.Millisecond), Data: data}
	if r, ok := c.closedRecord(id); ok && c.sessions[id] == nil {
		a = a.with("closed", r.Closed.Format(time.RFC3339Nano))
	}
	a.Label = label(k.verb, a.Name)
	c.steps.hist.push(a)
	c.saveState()
	c.mu.Unlock()
	c.publishHistory()
}

// nameOf is a session's name, open or closed; its id when Irie has neither.
// The caller holds c.mu.
func (c *Core) nameOf(id string) string {
	if x := c.sessions[id]; x != nil {
		return x.name
	}
	if r, ok := c.closedRecord(id); ok {
		return r.Name
	}
	return id
}

// closedRecord is session id's closed record. The caller holds c.mu.
func (c *Core) closedRecord(id string) (closedRecord, bool) {
	for _, r := range c.closes.list {
		if r.ID == id {
			return r, true
		}
	}
	return closedRecord{}, false
}

// whereIs says whether session id is open, closed or gone. The caller holds
// c.mu.
func (c *Core) whereIs(id string) string {
	if c.sessions[id] != nil {
		return "open"
	}
	if _, ok := c.closedRecord(id); ok {
		return "closed"
	}
	return "gone"
}

// publishHistory tells every subscriber that sees the whole list that the
// history changed.
func (c *Core) publishHistory() {
	c.mu.Lock()
	subs := slices.Collect(maps.Keys(c.subs))
	c.mu.Unlock()
	for _, s := range subs {
		s.noteHistory()
	}
}

// historyFrame is what clients hear of the history. The caller holds c.mu.
func (c *Core) historyFrame() ipc.Frame {
	u, r := c.steps.hist.labels()
	return ipc.Frame{T: ipc.FrameHistory, Undo: u, Redo: r}
}

// choiceID names a question: the step, the direction, and the problems
// accepted so far with the one asked last.
func choiceID(a action, dir string, codes []string) string {
	return a.ID + ":" + dir + ":" + strings.Join(codes, "+")
}

func parseChoiceID(s string) (id, dir string, codes []string, ok bool) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
		return "", "", nil, false
	}
	return parts[0], parts[1], strings.Split(parts[2], "+"), true
}

// firstOpen is the first problem not accepted.
func firstOpen(ps []problem, accepted []string) *problem {
	for i := range ps {
		if !slices.Contains(accepted, ps[i].code) {
			return &ps[i]
		}
	}
	return nil
}

// choice is the question p asks about step a: its own options, then skip
// and cancel, the recommended one first.
func choice(a action, dir string, accepted []string, p problem) *ipc.Choice {
	again := "u"
	if dir == dirRedo {
		again = "U"
	}
	opts := append(slices.Clone(p.options),
		ipc.ChoiceOption{Key: ipc.ChoiceSkip, Label: "Skip this one (drop it from history)",
			Detail: fmt.Sprintf("Leaves everything as it is and takes %s out of the history, so %s moves on to the next step.", a.Label, again)},
		ipc.ChoiceOption{Key: ipc.ChoiceCancel, Label: "Cancel (change nothing)",
			Detail: "Leaves everything as it is, the history too."})
	rec := p.recommend
	if rec == "" {
		rec = ipc.ChoiceCancel
	}
	slices.SortStableFunc(opts, func(x, y ipc.ChoiceOption) int {
		switch {
		case x.Key == rec && y.Key != rec:
			return -1
		case y.Key == rec && x.Key != rec:
			return 1
		}
		return 0
	})
	for i := range opts {
		opts[i].Recommended = opts[i].Key == rec
	}
	return &ipc.Choice{ID: choiceID(a, dir, append(slices.Clone(accepted), p.code)), Question: p.question, Options: opts}
}

func offers(p *problem, key string) bool {
	return slices.ContainsFunc(p.options, func(o ipc.ChoiceOption) bool { return o.Key == key })
}

// undo undoes or redoes, by dir, the next step of the history (DESIGN
// §4.5). One at a time: a second waits for the first. When the step is
// uncertain nothing changes and the result asks what to do; a.Choice
// answers such a question. A failed effect leaves the history as it was.
func (c *Core) undo(dir string, a ipc.UndoArgs) (ipc.UndoResult, error) {
	c.undoMu.Lock()
	defer c.undoMu.Unlock()
	if (a.Choice == "") != (a.ChoiceID == "") {
		return ipc.UndoResult{}, ipc.Errorf(ipc.CodeBadArgs, "a choice needs the id of the question it answers, and the id a choice")
	}
	c.mu.Lock()
	unlock := func() { c.mu.Unlock() }
	// The checks read the registry as tmux has it now. Should tmux not
	// answer, they read it as last seen, and the effect itself fails.
	if err := c.refresh(); err != nil {
		c.log.Warn("history: poll", "err", err)
	}
	top, ok := c.steps.hist.top(dir)
	if !ok {
		unlock()
		return ipc.UndoResult{}, ipc.Errorf(ipc.CodeNotFound, "nothing to %s", dir)
	}
	res := ipc.UndoResult{Label: top.Label}
	var accepted []string
	asked := ""
	if a.ChoiceID != "" {
		id, d, codes, ok := parseChoiceID(a.ChoiceID)
		if !ok || id != top.ID || d != dir {
			unlock()
			return res, ipc.Errorf(ipc.CodeStale, "the history changed since Irie asked, so nothing was done; press %s again", map[string]string{dirUndo: "u", dirRedo: "U"}[dir])
		}
		accepted, asked = codes[:len(codes)-1], codes[len(codes)-1]
	}
	kind, known := actionKinds[top.Kind]
	eff := kind.undo
	if dir == dirRedo {
		eff = kind.redo
	}
	var probs []problem
	if !known {
		probs = []problem{{code: "unknown", question: fmt.Sprintf("This version of Irie cannot %s %s.", dir, top.Label), recommend: ipc.ChoiceSkip}}
	} else {
		probs = eff.check(c, top)
	}
	// Only a problem the user could go ahead past counts as accepted.
	accepted = slices.DeleteFunc(accepted, func(code string) bool {
		i := slices.IndexFunc(probs, func(p problem) bool { return p.code == code })
		return i < 0 || !offers(&probs[i], ipc.ChoiceApply)
	})
	p := firstOpen(probs, accepted)
	before := c.whereIs(top.Session)

	ask := func(p *problem, accepted []string) (ipc.UndoResult, error) {
		unlock()
		res.Did, res.Choice = ipc.DidAsk, choice(top, dir, accepted, *p)
		return res, nil
	}
	switch {
	case a.Choice == ipc.ChoiceCancel:
		unlock()
		res.Did = ipc.DidCancel
		return res, nil
	case a.Choice == ipc.ChoiceSkip:
		c.steps.hist.drop(dir, top.ID)
		c.saveState()
		unlock()
		c.publishHistory()
		c.log.Info("history: skipped", "dir", dir, "action", top.Label)
		res.Did = ipc.DidSkip
		return res, nil
	case a.Choice == ipc.ChoiceSettle:
		if p == nil || p.code != asked || !offers(p, ipc.ChoiceSettle) {
			if p != nil {
				return ask(p, accepted)
			}
			unlock()
			return res, ipc.Errorf(ipc.CodeStale, "things changed since Irie asked, so nothing was done; press %s again", map[string]string{dirUndo: "u", dirRedo: "U"}[dir])
		}
		c.steps.hist.finish(dir, top.ID, top)
		c.saveState()
		c.journalStep(dir, top, before, before, ipc.ChoiceSettle)
		unlock()
		c.publishHistory()
		res.Did = ipc.DidSettle
		return res, nil
	case a.Choice == ipc.ChoiceApply:
		if p != nil && p.code == asked && offers(p, ipc.ChoiceApply) {
			accepted = append(accepted, asked)
			p = firstOpen(probs, accepted)
		}
		if p != nil {
			return ask(p, accepted)
		}
	case a.Choice != "":
		unlock()
		return res, ipc.Errorf(ipc.CodeBadArgs, "unknown choice %q", a.Choice)
	case p != nil:
		return ask(p, nil)
	}
	unlock()

	done, err := eff.do(c, top)
	if err != nil {
		c.log.Warn("history", "dir", dir, "action", top.Label, "err", err)
		return res, err
	}
	c.mu.Lock()
	done.Label = label(kind.verb, done.Name)
	c.steps.hist.finish(dir, top.ID, done)
	c.saveState()
	c.journalStep(dir, done, before, c.whereIs(top.Session), a.Choice)
	c.mu.Unlock()
	c.publishHistory()
	c.log.Info("history", "dir", dir, "action", done.Label)
	res.Label, res.Did = done.Label, ipc.DidUndo
	if dir == dirRedo {
		res.Did = ipc.DidRedo
	}
	return res, nil
}

// journalStep records an undo or redo in the journal, with what the session
// was before and after: open, closed or gone. The caller holds c.mu.
func (c *Core) journalStep(dir string, a action, before, after, choice string) {
	kind := event.UIUndo
	if dir == dirRedo {
		kind = event.UIRedo
	}
	e := event.Event{Source: event.SourceUser, Kind: kind, Data: map[string]any{
		"action": a.Kind, "label": a.Label, "before": before, "after": after,
	}}
	if choice != "" {
		e.Data["choice"] = choice
	}
	if x := c.sessions[a.Session]; x != nil {
		c.record(x, e)
		return
	}
	// A closed session's events name the harness it ran.
	e.Session, e.Harness = a.Session, "irie"
	if spec, err := host.ReadSpec(c.paths.SpecFile(a.Session)); err == nil && spec.Harness != "" {
		e.Harness = spec.Harness
	}
	var err error
	if e, err = c.journal.Append(e); err != nil {
		c.log.Warn("journal", "kind", e.Kind, "err", err)
	}
	for s := range c.subs {
		s.event(e)
	}
}

// busy reports whether a row's agent is at work or waits on the user.
func busy(state string) bool {
	return state == ipc.StateWorking || state == ipc.StateNeedsYou || state == ipc.StateStalled
}

// checkClose is what makes closing a's session uncertain.
func checkClose(c *Core, a action) []problem {
	x := c.sessions[a.Session]
	if x == nil {
		if _, ok := c.closedRecord(a.Session); ok {
			return []problem{{code: "closed", question: fmt.Sprintf("%q is closed already.", c.nameOf(a.Session)),
				options: []ipc.ChoiceOption{{Key: ipc.ChoiceSettle, Label: "Leave it closed and move on",
					Detail: "Nothing needs closing, so the history moves on as if Irie had closed it."}},
				recommend: ipc.ChoiceSettle}}
		}
		return []problem{gone(a)}
	}
	var ps []problem
	if x.name != a.Name {
		ps = append(ps, problem{code: "renamed", question: fmt.Sprintf("%q is called %q now.", a.Name, x.name),
			options: []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: fmt.Sprintf("Close %q anyway", x.name),
				Detail: "Ends its agent. Its folder and conversation stay, so it can be reopened."}},
			recommend: ipc.ChoiceApply})
	}
	// A frozen session is judged by what it was doing when it froze.
	state := x.wire().State
	frozen := state == ipc.StateFrozen
	if frozen {
		state = x.track.live()
	}
	if busy(state) {
		var what string
		switch {
		case frozen && state == ipc.StateWorking:
			what = "is frozen in the middle of its work"
		case frozen:
			what = "is frozen while it waits for you"
		case state == ipc.StateWorking:
			what = "is still at work"
		default:
			what = "is waiting for you"
		}
		ps = append(ps, problem{code: "busy", question: fmt.Sprintf("%q %s.", x.name, what),
			options: []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: "Close it now",
				Detail: "Stops its agent mid-task. Its folder and conversation stay, so it can be reopened and carry on."}},
			recommend: ipc.ChoiceCancel})
	}
	return ps
}

func gone(a action) problem {
	return problem{code: "gone", question: fmt.Sprintf("%q is gone: Irie no longer has it, open or closed.", a.Name), recommend: ipc.ChoiceSkip}
}

// doClose closes a's session the way the close op does, and notes when.
func doClose(c *Core, a action) (action, error) {
	if err := c.close(ipc.CloseArgs{Session: a.Session}); err != nil {
		return a, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.closedRecord(a.Session); ok {
		a = a.with("closed", r.Closed.Format(time.RFC3339Nano))
	}
	return a, nil
}

// checkReopen is what makes reopening a's session uncertain: whether it
// can come back at all, and whether it would come back as it went.
func checkReopen(c *Core, a action) []problem {
	if x := c.sessions[a.Session]; x != nil {
		return []problem{{code: "open", question: fmt.Sprintf("%q is open already.", x.name),
			options: []ipc.ChoiceOption{{Key: ipc.ChoiceSettle, Label: "Leave it open and move on",
				Detail: "Nothing needs reopening, so the history moves on as if Irie had reopened it."}},
			recommend: ipc.ChoiceSettle}}
	}
	r, ok := c.closedRecord(a.Session)
	if !ok {
		return []problem{gone(a)}
	}
	cannot := func(code, why string) []problem {
		return []problem{{code: code, question: fmt.Sprintf("%q cannot come back: %s.", r.Name, why), recommend: ipc.ChoiceSkip}}
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(r.ID))
	if err != nil {
		return cannot("no-spec", "Irie lost how it ran")
	}
	if fi, err := os.Stat(spec.Cwd); err != nil || !fi.IsDir() {
		return cannot("no-folder", fmt.Sprintf("its folder %s is gone", spec.Cwd))
	}
	profile, ok := c.profile(spec.Profile)
	if !ok {
		return cannot("no-profile", fmt.Sprintf("its profile +%s is gone", spec.Profile))
	}
	var ps []problem
	if when, err := time.Parse(time.RFC3339Nano, a.str("closed")); err == nil && !when.Equal(r.Closed) {
		ps = append(ps, problem{code: "changed", question: fmt.Sprintf("%q was reopened and closed again since.", r.Name),
			options:   []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: "Reopen it anyway", Detail: "Brings back the session as it was closed last."}},
			recommend: ipc.ChoiceApply})
	}
	if c.names()[r.Name] {
		n := unique(r.Name, c.names())
		ps = append(ps, problem{code: "name-taken", question: fmt.Sprintf("Another session is called %q now.", r.Name),
			options:   []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: fmt.Sprintf("Reopen it as %q", n), Detail: fmt.Sprintf("Brings it back in its own row under the name %q.", n)}},
			recommend: ipc.ChoiceApply})
	}
	h, herr := harnessFor(profile.Harness)
	_, resumes := h.(harness.Resumer)
	if !r.Resume || herr != nil || !resumes {
		if _, err := os.Stat(c.paths.TaskFile(r.ID)); err != nil {
			return cannot("no-task", "it has no conversation to carry on and its task is gone")
		}
		if herr == nil && resumes {
			ps = append(ps, problem{code: "fresh", question: fmt.Sprintf("%q never got going, so there is no conversation to carry on.", r.Name),
				options:   []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: "Start its task again", Detail: "Runs its task from the start in the same folder, as a new conversation."}},
				recommend: ipc.ChoiceApply})
		} else {
			ps = append(ps, problem{code: "fresh", question: fmt.Sprintf("%q runs an agent that cannot carry its conversation on.", r.Name),
				options:   []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: "Start its task again", Detail: "Its agent starts the task over and remembers nothing of its last run."}},
				recommend: ipc.ChoiceCancel})
		}
	}
	return ps
}

// doReopen reopens a's session the way the reopen op does: it waits for its
// old agent to end first.
func doReopen(c *Core, a action) (action, error) {
	s, err := c.reopen(ipc.ReopenArgs{Session: a.Session})
	if err != nil {
		return a, err
	}
	a.Name = s.Name
	return a.with("closed", nil), nil
}
