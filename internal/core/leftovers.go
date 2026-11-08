package core

// Leftovers (DESIGN §2.9). An Irie that a test run or an agent starts for a
// check has a tmux server of its own, a daemon, which runs on after the run
// with everything in it, its core, sidebars, hosts and agents, for days,
// and holds memory and ptys all the while. A Go test run cleans after
// itself (internal/testutil), but only on a branch that has its reaper,
// and nothing cleans after an Irie an agent starts by hand. So the user's
// core stops each such Irie once its run is over:
//
//   - a test run's (testutil names its socket irie-test-*) once no Go test
//     binary that started before it still runs;
//   - one an agent of this core's started, which its server's environment
//     says (IRIE_SESSION, with this core's IRIE_SOCKET), once that session
//     is closed;
//   - either once it has run a day: no check takes that long.
//
// Its team and dock servers go with it. Every other tmux server is left
// be: the core's own, the user's, and one an agent started for something
// that is not an Irie.

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/tmux"
)

const (
	leftoverEvery  = 60             // ticks: a look a minute
	leftoverMaxAge = 24 * time.Hour // no check runs a day
	leftoverTests  = "irie-test-"   // testutil's sockets (DESIGN §8.1)
)

// leftoverGrace is how long an ended server and its panes' first processes
// get to exit, and leftoverRestGrace how much longer the rest of its tree
// gets, before what still runs is killed.
var leftoverGrace, leftoverRestGrace = 5 * time.Second, time.Second

// leftoversOff reports whether this core leaves other Iries be: a test
// core (IRIE_LEFTOVERS=off, internal/testutil) would stop the servers of
// the tests that run beside it.
func leftoversOff() bool { return os.Getenv("IRIE_LEFTOVERS") == "off" }

// sideServers are the servers an Irie runs beside its own, by the end of
// their socket's name and their tmux.conf's (§2.3, the right dock).
var sideServers = map[string]string{"-teams": "tmux-teams.conf", "-dock": "tmux-dock.conf"}

// tmuxServer is a tmux server, as its command line and environment say.
type tmuxServer struct {
	proc.Proc
	path    string    // its socket's path
	socket  string    // the socket's name
	irie    bool      // an Irie's own server: it holds tmux.AgentsSession
	side    string    // a side server's ending ("-teams"); "" for any other
	ours    bool      // started from one of this core's sessions: IRIE_SOCKET is this core's
	session string    // which session, when ours
	started time.Time // when it started; zero when the system cannot say
}

// readServer reads a tmux server from the command line it keeps, the one
// of the client that started it with new-session, and its environment.
// Only global flags come before the command; new-session's own after it.
func readServer(argv, env []string, us string) (tmuxServer, bool) {
	s := tmuxServer{socket: "default"}
	var sockPath, conf string
	i := 1
	for ; i < len(argv) && strings.HasPrefix(argv[i], "-"); i++ {
		switch argv[i] {
		case "-L", "-S", "-f", "-T", "-c":
			if i+1 == len(argv) {
				return s, false
			}
			switch argv[i] {
			case "-L":
				s.socket = argv[i+1]
			case "-S":
				sockPath = argv[i+1]
			case "-f":
				conf = filepath.Base(argv[i+1])
			}
			i++
		}
	}
	if i == len(argv) || (argv[i] != "new-session" && argv[i] != "new") {
		return s, false
	}
	for i++; i < len(argv) && argv[i] != "--"; i++ {
		switch argv[i] {
		case "-c", "-e", "-f", "-F", "-n", "-t", "-x", "-y":
			i++
		case "-s":
			if i+1 < len(argv) && argv[i+1] == tmux.AgentsSession {
				s.irie = true
			}
			i++
		}
	}
	vars := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	if sockPath != "" {
		s.path, s.socket = sockPath, filepath.Base(sockPath)
	} else {
		dir := vars["TMUX_TMPDIR"]
		if dir == "" {
			dir = "/tmp"
		}
		s.path = filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()), s.socket)
	}
	for end, name := range sideServers {
		if strings.HasSuffix(s.socket, end) && conf == name {
			s.side = end
		}
	}
	if us != "" && vars["IRIE_SOCKET"] == us {
		s.ours, s.session = true, vars["IRIE_SESSION"]
	}
	return s, true
}

// isTestBinary reports whether p runs a Go test binary, pkg.test. The
// system cuts a long name short (16 bytes on macOS, 15 on Linux), and then
// its path says.
func isTestBinary(p proc.Proc) bool {
	if strings.HasSuffix(p.Name, ".test") {
		return true
	}
	if len(p.Name) < 15 {
		return false
	}
	argv, _, err := proc.Args(p.PID)
	return err == nil && len(argv) > 0 && strings.HasSuffix(argv[0], ".test")
}

// leftoverLook is what one look at the machine finds: every tmux server
// but this core's own, and when each Go test binary that runs now started,
// with blind set when one's start could not be read.
type leftoverLook struct {
	servers []tmuxServer
	tests   []time.Time
	blind   bool
}

// lookForLeftovers reads t for the leftover's look. own are the sockets
// this core runs, by name, and self the process it runs as: the server it
// runs under is its own whatever its name.
func lookForLeftovers(t proc.Table, own []string, us string, self int) leftoverLook {
	var l leftoverLook
	mine := 0
	for p, ok := t.Get(self); ok; p, ok = t.Get(p.PPID) {
		if p.Name == "tmux" {
			mine = p.PID
			break
		}
		if p.PPID == p.PID || p.PPID <= 1 {
			break
		}
	}
	for _, p := range t.All() {
		if isTestBinary(p) {
			if at, ok := p.Started(); ok {
				l.tests = append(l.tests, at)
			} else {
				l.blind = true
			}
			continue
		}
		if p.Name != "tmux" || p.PID == mine || p.Zombie {
			continue
		}
		argv, env, err := proc.Args(p.PID)
		if err != nil {
			continue
		}
		s, ok := readServer(argv, env, us)
		if !ok || slices.Contains(own, s.socket) {
			continue
		}
		s.Proc = p
		s.started, _ = p.Started()
		l.servers = append(l.servers, s)
	}
	return l
}

// leftoverWhy says why s's run is over, or "" while it may go on; open
// holds this core's sessions.
func (l leftoverLook) leftoverWhy(s tmuxServer, open map[string]bool, now time.Time) string {
	old := !s.started.IsZero() && now.Sub(s.started) >= leftoverMaxAge
	switch {
	case strings.HasPrefix(s.socket, leftoverTests):
		if old {
			return "a test run's, a day old"
		}
		if l.blind || s.started.IsZero() {
			return ""
		}
		for _, at := range l.tests {
			// A test binary makes its servers after it starts; the second
			// makes up for a start Linux reads to a hundredth.
			if !at.After(s.started.Add(time.Second)) {
				return ""
			}
		}
		return "its test run is over"
	case !s.irie || !s.ours || s.session == "":
		return ""
	case !open[s.session]:
		return "session " + s.session + ", whose agent started it, is closed"
	case old:
		return "it has run a day"
	}
	return ""
}

// leftovers decides which servers of l to stop, and why: an Irie whose run
// is over (leftoverWhy), and a side server of one that stops or is gone.
func (l leftoverLook) leftovers(open map[string]bool, now time.Time) map[string]string {
	stop := map[string]string{}
	paths := map[string]bool{}
	for _, s := range l.servers {
		paths[s.path] = true
		if s.side == "" || strings.HasPrefix(s.socket, leftoverTests) {
			if why := l.leftoverWhy(s, open, now); why != "" {
				stop[s.path] = why
			}
		}
	}
	for _, s := range l.servers {
		if s.side == "" || strings.HasPrefix(s.socket, leftoverTests) || !s.ours {
			continue
		}
		main := filepath.Join(filepath.Dir(s.path), strings.TrimSuffix(s.socket, s.side))
		switch why, ok := stop[main]; {
		case ok:
			stop[s.path] = "its Irie stops: " + why
		case !paths[main]:
			stop[s.path] = "its Irie is gone"
		}
	}
	return stop
}

// leftoverSweep stops the Iries whose run is over, with what they run. One
// sweep runs at a time.
func (c *Core) leftoverSweep() {
	if leftoversOff() || !c.leftSweeping.CompareAndSwap(false, true) {
		return
	}
	defer c.leftSweeping.Store(false)
	t, err := proc.Read()
	if err != nil {
		return
	}
	own := []string{c.tmux.Socket, c.team.Socket, c.dock.Socket}
	l := lookForLeftovers(t, own, c.paths.Socket(), os.Getpid())
	if len(l.servers) == 0 {
		return
	}
	c.mu.Lock()
	open := make(map[string]bool, len(c.sessions))
	for id := range c.sessions {
		open[id] = true
	}
	c.mu.Unlock()
	stop := l.leftovers(open, time.Now())
	if len(stop) == 0 {
		return
	}
	var servers []proc.Proc
	for _, s := range l.servers {
		if why, ok := stop[s.path]; ok {
			servers = append(servers, s.Proc)
			c.log.Info("leftover stopped", "socket", s.socket, "pid", s.PID, "processes", len(t.Tree(s.PID))+1,
				"age", time.Since(s.started).Round(time.Minute).String(), "why", why)
		}
	}
	if left := stopServers(t, servers); len(left) > 0 {
		c.log.Warn("leftover still running", "processes", len(left), "pids", pidsOf(left))
	}
}

// stopServers ends tmux servers and everything they run, as testutil does
// after a test: their trees are read first, while every process still
// hangs under its server; SIGTERM ends a server, which hangs up its panes,
// and SIGCONT lets a stopped one see it. After leftoverGrace for the
// servers and their panes' first processes, and leftoverRestGrace for the
// rest, what still runs of the trees is killed. It returns what then still
// runs, such as a process stuck on a disk that does not answer.
func stopServers(t proc.Table, servers []proc.Proc) []proc.Proc {
	var first, rest []proc.Proc
	for _, s := range servers {
		first = append(first, s)
		for _, p := range t.Tree(s.PID) {
			if p.PPID == s.PID {
				first = append(first, p)
			} else {
				rest = append(rest, p)
			}
		}
	}
	for _, s := range stillRunning(servers) {
		syscall.Kill(s.PID, syscall.SIGTERM)
		syscall.Kill(s.PID, syscall.SIGCONT)
	}
	deadline := time.Now().Add(leftoverGrace)
	waitGone(first, deadline)
	if d := time.Now().Add(leftoverRestGrace); d.Before(deadline) {
		deadline = d
	}
	waitGone(rest, deadline)
	all := append(first, rest...)
	for _, p := range stillRunning(all) {
		syscall.Kill(p.PID, syscall.SIGKILL)
	}
	waitGone(all, time.Now().Add(time.Second))
	return stillRunning(all)
}

// waitGone waits until no process of ps runs, or until deadline.
func waitGone(ps []proc.Proc, deadline time.Time) {
	for len(stillRunning(ps)) > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}

// stillRunning returns the processes of ps that still run: the same
// processes, not later ones that took their pids.
func stillRunning(ps []proc.Proc) []proc.Proc {
	t, err := proc.Read()
	if err != nil {
		return nil
	}
	var out []proc.Proc
	for _, p := range ps {
		if t.Alive(p) {
			out = append(out, p)
		}
	}
	return out
}

func pidsOf(ps []proc.Proc) []int {
	out := make([]int, len(ps))
	for i, p := range ps {
		out[i] = p.PID
	}
	return out
}
