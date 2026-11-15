package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/tmux"
)

// Your helpers (DESIGN §4.17): programs of yours, each from a [helpers.<id>]
// table of a manifest in the plugins folder, that the core keeps running
// while their plugin is on, and whose status files the helpers op passes on
// to the Mac app's Helpers tab, in the right sidebar.

// ownHelperWindow names your helpers' windows in irie-agents.
const ownHelperWindow = brand.Slug + "-helper"

// ownHelperLook is how often the core reads the manifests to see which of
// your helpers to keep running: one you turn on starts within it.
const ownHelperLook = 5 * time.Second

// ownHelperRetry is the least time between two starts of one helper: one that
// exits at once starts again at most this often.
const ownHelperRetry = 30 * time.Second

// ownHelperStatusMax is the largest status file of a helper's that is read.
const ownHelperStatusMax = 64 << 10

// ownHelperKeep is what the core keeps of your helpers between ticks.
type ownHelperKeep struct {
	looked time.Time            // when it last read the manifests
	want   []plugins.Helper     // the helpers to keep running, as it read them then
	at     map[string]time.Time // when it last started each, by id
	why    map[string]string    // why each one doesn't start, in a sentence, by id
}

// ownHelperSpec is what a helper's pane runs with, kept in @irie_version, so
// one whose program, arguments or folder changed starts again. Irie's own
// version isn't in it: an install leaves your helpers as they run.
func ownHelperSpec(h plugins.Helper) string {
	q := []string{strconv.Quote(h.Dir)}
	for _, w := range h.Run {
		q = append(q, strconv.Quote(w))
	}
	return strings.Join(q, " ")
}

// keepOwnHelpers keeps each helper of yours whose plugin is on running in a
// window of its own in irie-agents, under irie runtime, which ends the
// helper's whole process group when its pane goes. One that exited starts
// again, at most every ownHelperRetry; one whose plugin went off, or whose
// manifest went, stops. Like every pane of Irie's own, a helper's is never
// an agent's, so none of this disturbs an agent (DESIGN §2.1, principle 6),
// and a restart of the core leaves them running. The caller holds c.mu.
func (c *Core) keepOwnHelpers(now time.Time) {
	k := &c.ownHelpers
	if now.Sub(k.looked) >= ownHelperLook {
		k.looked = now
		k.want = nil
		for _, h := range plugins.Helpers(plugins.Env{Config: *c.cfg(), Paths: c.paths}) {
			if h.On && len(h.Run) > 0 && h.Problem == "" {
				k.want = append(k.want, h)
			}
		}
	}
	want := map[string]bool{}
	for _, h := range k.want {
		want[h.ID] = true
	}
	var cmds [][]string
	have := map[string]tmux.Pane{}
	for _, p := range c.snap.ByRole(tmux.RoleHelper) {
		if _, dup := have[p.Name]; dup || !want[p.Name] {
			cmds = append(cmds, []string{"kill-pane", "-t", p.ID})
			c.log.Info("helper: stopped", "helper", p.Name, "pane", p.ID)
			continue
		}
		have[p.Name] = p
	}
	for _, h := range k.want {
		spec := ownHelperSpec(h)
		pane, ok := have[h.ID]
		if ok && !pane.Dead && pane.Version == spec {
			continue
		}
		if now.Sub(k.at[h.ID]) < ownHelperRetry {
			continue
		}
		if k.at == nil {
			k.at, k.why = map[string]time.Time{}, map[string]string{}
		}
		k.at[h.ID] = now
		if _, err := plugins.HelperProgram(h); err != nil {
			if k.why[h.ID] != err.Error() {
				c.log.Warn("helper: can't start", "helper", h.ID, "err", err)
			}
			k.why[h.ID] = err.Error()
			continue
		}
		delete(k.why, h.ID)
		env := []string{"-e", brand.Env("BIN") + "=" + c.bin, "-e", brand.Env("HELPER") + "=" + h.ID}
		run := append([]string{"--", c.bin, "runtime", "--"}, h.Run...)
		started := now.UTC().Format(time.RFC3339)
		if !ok {
			args := append([]string{"new-window", "-d", "-t", tmux.AgentsSession + ":", "-n", ownHelperWindow, "-c", h.Dir, "-P", "-F", "#{pane_id}"}, env...)
			out, err := c.tmux.Run(append(args, run...)...)
			if err != nil {
				c.log.Warn("helper: start", "helper", h.ID, "err", err)
				continue
			}
			id := strings.TrimSpace(out)
			cmds = append(cmds, []string{"set-option", "-p", "-t", id, tmux.OptRole, tmux.RoleHelper},
				[]string{"set-option", "-p", "-t", id, tmux.OptName, h.ID},
				[]string{"set-option", "-p", "-t", id, tmux.OptVersion, spec},
				[]string{"set-option", "-p", "-t", id, tmux.OptCreated, started})
			c.log.Info("helper: started", "helper", h.ID, "pane", id, "run", h.Run[0])
			continue
		}
		cmds = append(cmds, append(append([]string{"respawn-pane", "-k", "-t", pane.ID, "-c", h.Dir}, env...), run...),
			[]string{"set-option", "-p", "-t", pane.ID, tmux.OptVersion, spec},
			[]string{"set-option", "-p", "-t", pane.ID, tmux.OptCreated, started})
		c.log.Info("helper: restarted", "helper", h.ID, "pane", pane.ID, "dead", pane.Dead, "exit", pane.DeadStatus)
	}
	if len(cmds) > 0 {
		if _, err := c.tmux.RunList(cmds); err != nil {
			c.log.Warn("helper", "err", err)
		}
	}
}

// helpersOp answers the helpers op: each helper of yours, whether it runs,
// and what its status file says; with Restart, after starting the one
// named again, at once. It reads the manifests and the status files, and
// runs nothing but that restart.
func (c *Core) helpersOp(a ipc.HelpersArgs) (ipc.HelpersResult, error) {
	all := plugins.Helpers(plugins.Env{Config: *c.cfg(), Paths: c.paths})
	if a.Restart {
		var h *plugins.Helper
		for i := range all {
			if all[i].ID == a.ID {
				h = &all[i]
			}
		}
		if h == nil {
			return ipc.HelpersResult{}, ipc.Errorf(ipc.CodeBadArgs, "No helper is called %q.", a.ID)
		}
		if !h.On || len(h.Run) == 0 {
			return ipc.HelpersResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s doesn't run: its plugin is off, or it has no program.", h.Title)
		}
		c.mu.Lock()
		c.ownHelpers.looked = time.Time{}
		delete(c.ownHelpers.at, h.ID)
		for _, p := range c.snap.ByRole(tmux.RoleHelper) {
			if p.Name == h.ID {
				// A version that no spec has makes the next tick start it again.
				if _, err := c.tmux.Run("set-option", "-p", "-t", p.ID, tmux.OptVersion, "restart"); err == nil {
					_ = c.refresh()
				}
			}
		}
		c.keepOwnHelpers(time.Now())
		c.mu.Unlock()
	}
	// tmux's panes as they are now: the core's own look can be a tick old,
	// and a helper just started or stopped would read the other way.
	snap, err := c.tmux.Snapshot()
	c.mu.Lock()
	if err != nil {
		snap = c.snap
	}
	panes := map[string]tmux.Pane{}
	for _, p := range snap.ByRole(tmux.RoleHelper) {
		panes[p.Name] = p
	}
	why := map[string]string{}
	for id, w := range c.ownHelpers.why {
		why[id] = w
	}
	c.mu.Unlock()
	out := ipc.HelpersResult{Helpers: []ipc.HelperInfo{}}
	for _, h := range all {
		w := ipc.HelperInfo{ID: h.ID, Plugin: h.Plugin, Title: h.Title, Icon: h.Icon, On: h.On, Runs: len(h.Run) > 0,
			Open: h.Open, Manifest: h.Manifest, Items: []ipc.HelperItem{}}
		p, ok := panes[h.ID]
		w.Running = ok && !p.Dead
		if w.Running {
			w.Since = p.Created
		}
		switch {
		case h.Problem != "":
			w.Problem = h.Problem
		case !w.Runs || !h.On:
		case why[h.ID] != "":
			w.Problem = why[h.ID]
		case ok && p.Dead:
			w.Problem = fmt.Sprintf("It stopped (exit %d). %s starts it again within %d seconds; its window in irie-agents keeps what it printed last.", p.DeadStatus, brand.Name, int(ownHelperRetry/time.Second))
		}
		if h.Status != "" {
			readOwnHelperStatus(h.Status, &w)
		}
		out.Helpers = append(out.Helpers, w)
	}
	return out, nil
}

// ownHelperStatus is a helper's status file as it reads.
type ownHelperStatus struct {
	State string `json:"state"`
	Line  string `json:"line"`
	Items []struct {
		Title   string `json:"title"`
		Detail  string `json:"detail"`
		State   string `json:"state"`
		Session string `json:"session"`
		Open    string `json:"open"`
	} `json:"items"`
}

var (
	ownHelperStates     = map[string]bool{"ok": true, "working": true, "needs_you": true, "error": true}
	ownHelperItemStates = map[string]bool{"working": true, "waiting": true, "needs_you": true, "done": true, "error": true}
	// ownHelperSession is a session's id as a status file may name it: its
	// own, or a peer's as host/id.
	ownHelperSession = regexp.MustCompile(`^(?:[A-Za-z0-9][A-Za-z0-9_.-]{0,63}/)?s[0-9a-z]{8,24}$`)
)

// readOwnHelperStatus reads a helper's status file into w: its state, its
// line and its items, with what can't be shown left out (a state no one
// knows, a session that isn't one, an open that isn't a file of this
// computer's) and long words cut. A file that doesn't read is the
// helper's problem, in a sentence; none yet is no problem.
func readOwnHelperStatus(path string, w *ipc.HelperInfo) {
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			w.Problem = joinSentences(w.Problem, "Its status file doesn't open: "+err.Error()+".")
		}
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		w.Problem = joinSentences(w.Problem, "Its status file, "+path+", isn't a file.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, ownHelperStatusMax+1))
	if err != nil {
		return
	}
	if len(data) > ownHelperStatusMax {
		w.Problem = joinSentences(w.Problem, fmt.Sprintf("Its status file is over %d KB, the most read.", ownHelperStatusMax>>10))
		return
	}
	var s ownHelperStatus
	if err := json.Unmarshal(data, &s); err != nil {
		w.Problem = joinSentences(w.Problem, "Its status file isn't JSON a helper's status takes: "+err.Error()+".")
		return
	}
	w.Updated = st.ModTime().UTC().Format(time.RFC3339)
	if ownHelperStates[s.State] {
		w.State = s.State
	}
	w.Line = cutLine(s.Line, 200)
	for _, it := range s.Items {
		if len(w.Items) == ipc.HelperItemsMax {
			break
		}
		x := ipc.HelperItem{Title: cutLine(it.Title, 120), Detail: cutLine(it.Detail, 300)}
		if x.Title == "" {
			continue
		}
		if ownHelperItemStates[it.State] {
			x.State = it.State
		}
		if ownHelperSession.MatchString(it.Session) {
			x.Session = it.Session
		}
		if filepath.IsAbs(it.Open) && !strings.ContainsAny(it.Open, "\x00\n") {
			x.Open = filepath.Clean(it.Open)
		}
		w.Items = append(w.Items, x)
	}
}

// cut keeps a status's words to one line of at most n runes.
func cutLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func joinSentences(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}
