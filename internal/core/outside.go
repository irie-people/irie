package core

// Sessions outside Irie (DESIGN §4.6). Claude Code lists the machine's live
// sessions with `claude agents --json`. The core asks it every outsideEvery
// while someone looks, leaves out the sessions Irie hosts, and tells
// subscribers when the rest change. The list travels apart from Irie's own
// rows: nothing of it is journalled, and it derives no state.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/tmux"
)

// outsideEvery is how often the core lists the sessions outside Irie while
// someone looks: a tmux client on session irie, or the Mac app. A run of
// `claude agents --json` takes about 0.1 s and a little more CPU, so this
// costs about 4% of one core while anyone looks and nothing otherwise, and a
// session that turns idle in another terminal can be moved in within 3 s.
const outsideEvery = 3 * time.Second

// outsideTimeout bounds one run of the CLI, which normally takes 0.1 s.
var outsideTimeout = 5 * time.Second

// outside is what the core knows of the sessions outside Irie. c.mu guards
// list and problem; runMu makes runs take turns and guards ran.
type outside struct {
	list    []ipc.OutsideSession // the last good list, without Irie's own, oldest first
	problem string               // why the last run failed; "" when it went fine

	runMu sync.Mutex
	ran   time.Time // when the last run started
}

// claudeProfile is the profile whose command lists the sessions:
// spawn.default_profile when it is a Claude Code profile, else the first
// Claude Code profile by name.
func (c *Core) claudeProfile() (string, config.Profile, bool) {
	if p, ok := c.cfg.Profiles[c.cfg.Spawn.DefaultProfile]; ok && p.Harness == "claude-code" {
		return c.cfg.Spawn.DefaultProfile, p, true
	}
	for _, name := range slices.Sorted(maps.Keys(c.cfg.Profiles)) {
		if c.cfg.Profiles[name].Harness == "claude-code" {
			return name, c.cfg.Profiles[name], true
		}
	}
	return "", config.Profile{}, false
}

// looking reports whether anyone can see the list: a tmux client on session
// irie, or the Mac app. The caller holds c.mu.
func (c *Core) looking() bool {
	return c.appViews > 0 || c.snap.Attached(tmux.MainSession) > 0
}

// watchOutside lists the sessions outside Irie every outsideEvery while
// someone looks.
func (c *Core) watchOutside() {
	t := time.NewTicker(outsideEvery / 6)
	defer t.Stop()
	for range t.C {
		c.mu.Lock()
		look := c.looking()
		c.mu.Unlock()
		if look {
			c.refreshOutside(outsideEvery, false)
		}
	}
}

// refreshOutside lists the sessions outside Irie, unless the last run
// started less than maxAge ago, and tells subscribers when the list or its
// problem changed. Unless wait is set it gives up at once while another run
// is going, and the cached list stands. A run that fails keeps the last list
// and records the problem. The caller holds no lock.
func (c *Core) refreshOutside(maxAge time.Duration, wait bool) error {
	o := &c.outside
	if wait {
		o.runMu.Lock()
	} else if !o.runMu.TryLock() {
		return nil
	}
	defer o.runMu.Unlock()
	if maxAge > 0 && time.Since(o.ran) < maxAge {
		return nil
	}
	o.ran = time.Now()
	_, p, ok := c.claudeProfile()
	if !ok {
		return errors.New("no profile runs Claude Code")
	}
	list, err := listOutside(p.Command, c.agentEnv(p))

	// A session that changed its id, as /clear does, is still Irie's when
	// its process runs in an agent pane. Only then is the process table read.
	var parents map[int]int
	c.mu.Lock()
	_, hosts := c.ownSessions()
	c.mu.Unlock()
	if err == nil && len(hosts) > 0 && slices.ContainsFunc(list, func(o ipc.OutsideSession) bool { return o.PID > 1 }) {
		var perr error
		if parents, perr = processParents(); perr != nil {
			c.log.Warn("outside: ps", "err", perr)
		}
	}

	c.mu.Lock()
	changed := c.setOutside(list, parents, err)
	c.mu.Unlock()
	if changed {
		c.publishOutside()
	}
	return err
}

// setOutside records a run's list, without Irie's own sessions, or its
// error, and reports whether that changed what subscribers know. A failed
// run keeps the last list; its problem is logged once. The caller holds
// c.mu.
func (c *Core) setOutside(list []ipc.OutsideSession, parents map[int]int, err error) bool {
	o := &c.outside
	problem := ""
	if err != nil {
		problem = "sessions outside Irie: " + err.Error()
		list = o.list
	} else {
		ids, hosts := c.ownSessions()
		list = dropOwn(list, ids, hosts, parents)
	}
	if problem != o.problem {
		if problem != "" {
			c.log.Warn("outside", "problem", problem)
		} else {
			c.log.Info("outside: listing works again")
		}
	}
	same := len(list) == len(o.list) && (len(list) == 0 || reflect.DeepEqual(list, o.list))
	changed := problem != o.problem || !same
	o.list, o.problem = list, problem
	return changed
}

// ownSessions returns the vendor ids of Irie's live sessions, which Irie
// passed with --session-id or --resume, and the pids of their agent panes'
// hosts. The caller holds c.mu.
func (c *Core) ownSessions() (map[string]bool, []int) {
	ids := map[string]bool{}
	for _, x := range c.sessions {
		if !x.dead && x.vendorID != "" {
			ids[x.vendorID] = true
		}
	}
	var hosts []int
	for _, p := range c.snap.ByRole(tmux.RoleAgent) {
		if !p.Dead && p.PID > 1 {
			hosts = append(hosts, p.PID)
		}
	}
	return ids, hosts
}

// dropOwn leaves out of list the sessions Irie hosts: those with one of its
// vendor ids, and those whose process runs under one of its agent panes'
// hosts, by parents (pid to parent pid). That catches an agent after /clear,
// and one started through a wrapper script.
func dropOwn(list []ipc.OutsideSession, ids map[string]bool, hosts []int, parents map[int]int) []ipc.OutsideSession {
	under := func(pid int) bool {
		for range 64 { // a cycle in a racy ps listing cannot loop forever
			if pid <= 1 {
				return false
			}
			if slices.Contains(hosts, pid) {
				return true
			}
			pid = parents[pid]
		}
		return false
	}
	out := []ipc.OutsideSession{}
	for _, o := range list {
		if o.VendorID != "" && ids[o.VendorID] || o.PID > 1 && under(o.PID) {
			continue
		}
		out = append(out, o)
	}
	return out
}

// outsideList returns a copy of the list, never nil. The caller holds c.mu.
func (c *Core) outsideList() []ipc.OutsideSession {
	return append([]ipc.OutsideSession{}, c.outside.list...)
}

// publishOutside tells every subscriber that sees the whole list that the
// sessions outside Irie, or their problem, changed.
func (c *Core) publishOutside() {
	c.mu.Lock()
	subs := make([]*subscriber, 0, len(c.subs))
	for s := range c.subs {
		subs = append(subs, s)
	}
	c.mu.Unlock()
	for _, s := range subs {
		s.noteOutside()
	}
}

// listOutside runs `<command> agents --json` with env and reads the
// sessions it prints.
func listOutside(command string, env []string) ([]ipc.OutsideSession, error) {
	path, err := host.LookPath(command, env)
	if err != nil {
		return nil, fmt.Errorf("%s: command not found", command)
	}
	out, err := runCLI(path, []string{"agents", "--json"}, env)
	if err != nil {
		return nil, err
	}
	return parseAgents(out)
}

// runCLI runs path with args and env in its own process group, which is
// killed when it takes longer than outsideTimeout, and returns its stdout.
// Only stdout is read as output, since the CLI may print warnings on stderr;
// a failure quotes stderr's first line.
func runCLI(path string, args, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), outsideTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	name := strings.Join(append([]string{filepath.Base(path)}, args...), " ")
	switch {
	case ctx.Err() != nil:
		return nil, fmt.Errorf("`%s` did not answer within %v", name, outsideTimeout)
	case err != nil:
		first, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		if first != "" {
			return nil, fmt.Errorf("`%s` failed: %v: %s", name, err, cleanText(first, 200))
		}
		return nil, fmt.Errorf("`%s` failed: %v", name, err)
	}
	return out, nil
}

var (
	uuidRE    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	shortIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

// parseAgents reads the JSON array that `claude agents --json` prints
// (Claude Code's agent view docs), oldest session first. It is lenient: a
// field of an unexpected type reads as empty, an entry that is not an object
// is skipped, and statuses and kinds it does not know are kept. Ids that are
// not well formed are dropped, since they go on a command line, and display
// strings lose their control characters, since they reach terminals. Lines
// before the array, such as a notice, are skipped.
func parseAgents(out []byte) ([]ipc.OutsideSession, error) {
	var raw []json.RawMessage
	err := json.Unmarshal(out, &raw)
	if err != nil {
		if i := bytes.Index(out, []byte("\n[")); i < 0 || json.Unmarshal(out[i+1:], &raw) != nil {
			return nil, fmt.Errorf("bad JSON from `claude agents --json`: %v", err)
		}
	}
	list := []ipc.OutsideSession{}
	for _, r := range raw {
		var e object
		if json.Unmarshal(r, &e) != nil || e == nil {
			continue
		}
		o := ipc.OutsideSession{
			Name: cleanText(e.str("name"), 200), Kind: cleanText(e.str("kind"), 64),
			Status: cleanText(e.str("status"), 64), WaitingFor: cleanText(e.str("waitingFor"), 200),
			State: cleanText(e.str("state"), 64), Cwd: e.str("cwd"),
		}
		if id := e.str("sessionId"); uuidRE.MatchString(id) {
			o.VendorID = id
		}
		if id := e.str("id"); shortIDRE.MatchString(id) {
			o.ID = id
		}
		if pid, ok := e.number("pid"); ok && pid > 1 && pid <= 1<<31-1 {
			o.PID = int(pid)
		}
		if ms, ok := e.number("startedAt"); ok && ms > 0 {
			o.Started = time.UnixMilli(ms).UTC()
		}
		judge(&o)
		list = append(list, o)
	}
	slices.SortStableFunc(list, func(a, b ipc.OutsideSession) int {
		if c := a.Started.Compare(b.Started); c != 0 {
			return c
		}
		return strings.Compare(a.Key(), b.Key())
	})
	return list, nil
}

// object is a JSON object read leniently.
type object map[string]json.RawMessage

func (e object) str(key string) string {
	var s string
	json.Unmarshal(e[key], &s)
	return s
}

func (e object) number(key string) (int64, bool) {
	var f float64
	if json.Unmarshal(e[key], &f) != nil || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

// cleanText turns s into one line of printable text of at most max bytes.
func cleanText(s string, max int) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// judge sets whether move would take o now, and why not (DESIGN §4.6):
// it must be idle, with an id to resume, a process to end and, when it runs
// in the background, a short id for claude stop.
func judge(o *ipc.OutsideSession) {
	o.Movable, o.Why = false, ""
	switch {
	case o.VendorID == "":
		o.Why = "Claude Code gave no session id for it yet"
	case o.Kind != ipc.KindInteractive && o.Kind != ipc.KindBackground:
		o.Why = fmt.Sprintf("Irie cannot move a session of kind %q", o.Kind)
	case o.Status == "":
		o.Why = "its process is not running"
	case o.Status == ipc.StatusBusy:
		o.Why = "it is busy; it can move once it is idle"
	case o.Status == ipc.StatusWaiting:
		what := o.WaitingFor
		if what == "" {
			what = "you"
		}
		o.Why = "it is waiting for " + what + "; answer it where it runs"
	case o.Status != ipc.StatusIdle:
		o.Why = fmt.Sprintf("it is %s; it can move once it is idle", o.Status)
	case o.PID == 0:
		o.Why = "Claude Code gave no process id for it"
	case o.Kind == ipc.KindBackground && o.ID == "":
		o.Why = "Claude Code gave no short id to stop it with"
	case !filepath.IsAbs(o.Cwd):
		o.Why = "Claude Code gave no folder for it"
	default:
		o.Movable = true
	}
}

// processParents maps each process on the machine to its parent, from ps.
func processParents() (map[int]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), outsideTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=", "-o", "ppid=").Output()
	if err != nil {
		return nil, err
	}
	parents := map[int]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			parents[pid] = ppid
		}
	}
	return parents, nil
}
