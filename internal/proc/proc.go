// Package proc reads the machine's process table, for what Irie does to a
// session's whole process tree: freeze stops it and thaw continues it
// (DESIGN §4.3), and promote puts the other sessions' trees in the
// background. One read is a single sysctl on macOS and a walk of /proc on
// Linux; neither starts a process.
package proc

import (
	"bytes"
	"encoding/binary"
	"maps"
	"slices"
	"strings"
	"time"
)

// Proc is one process.
type Proc struct {
	PID, PPID, PGID int
	// Start is when the process started, in the system's own unit. It tells
	// a process from a later one that reuses its pid.
	Start   int64
	Stopped bool   // stopped by a signal: ps's T
	Zombie  bool   // exited, not yet reaped
	Traced  bool   // a debugger holds it
	Name    string // the command's name, cut short by the system
}

// Table is every process on the machine at one moment.
type Table struct {
	procs map[int]Proc
	kids  map[int][]int // by parent, in pid order
}

// Group returns the processes of one process group, as a terminal's job
// control sees it: a pane's first process and what it started.
func Group(pgid int) ([]Proc, error) { return readGroup(pgid) }

// One returns one process.
func One(pid int) (Proc, error) { return readOne(pid) }

// Read returns the process table.
func Read() (Table, error) {
	ps, err := read()
	if err != nil {
		return Table{}, err
	}
	return NewTable(ps), nil
}

// Args returns the arguments one of the user's own processes was started
// with, and the environment it started with, as ps -E shows them. Another
// user's the system shows to root alone.
func Args(pid int) (argv, env []string, err error) { return readArgs(pid) }

// Started returns when p started by the clock, when the system says.
func (p Proc) Started() (time.Time, bool) { return started(p.Start) }

// parseProcargs2 reads macOS's kern.procargs2 (xnu's sysctl_procargsx):
// the argument count as a 32-bit integer, the program's path, NULs up to
// the first argument, then the arguments and the environment, each ended
// by a NUL. An empty string ends the environment; the system's own strings
// follow it.
func parseProcargs2(b []byte) (argv, env []string, ok bool) {
	if len(b) < 4 {
		return nil, nil, false
	}
	argc := int(binary.NativeEndian.Uint32(b))
	b = b[4:]
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return nil, nil, false
	}
	b = bytes.TrimLeft(b[i:], "\x00")
	next := func() (string, bool) {
		i := bytes.IndexByte(b, 0)
		if i < 0 {
			return "", false
		}
		s := string(b[:i])
		b = b[i+1:]
		return s, true
	}
	for len(argv) < argc {
		s, ok := next()
		if !ok {
			return nil, nil, false
		}
		argv = append(argv, s)
	}
	for {
		s, ok := next()
		if !ok || s == "" {
			return argv, env, true
		}
		env = append(env, s)
	}
}

// splitNUL splits Linux's /proc/<pid>/cmdline or environ: strings each
// ended by a NUL.
func splitNUL(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}

// All returns every process, in pid order.
func (t Table) All() []Proc {
	out := make([]Proc, 0, len(t.procs))
	for _, pid := range slices.Sorted(maps.Keys(t.procs)) {
		out = append(out, t.procs[pid])
	}
	return out
}

// NewTable makes a table of ps, as Read does from the system's.
func NewTable(ps []Proc) Table {
	t := Table{procs: make(map[int]Proc, len(ps)), kids: map[int][]int{}}
	for _, p := range ps {
		if p.PID <= 0 {
			continue
		}
		t.procs[p.PID] = p
		if p.PPID != p.PID {
			t.kids[p.PPID] = append(t.kids[p.PPID], p.PID)
		}
	}
	for _, k := range t.kids {
		slices.Sort(k)
	}
	return t
}

// Get returns the process with the pid.
func (t Table) Get(pid int) (Proc, bool) {
	p, ok := t.procs[pid]
	return p, ok
}

// Alive reports whether p still runs: its pid names a process that started
// when p did.
func (t Table) Alive(p Proc) bool {
	q, ok := t.procs[p.PID]
	return ok && q.Start == p.Start && !q.Zombie
}

// Newest is the start of the process that started last: whatever starts
// later has a later start.
func (t Table) Newest() int64 {
	var n int64
	for _, p := range t.procs {
		n = max(n, p.Start)
	}
	return n
}

// Children returns pid's children, in pid order.
func (t Table) Children(pid int) []Proc {
	out := make([]Proc, 0, len(t.kids[pid]))
	for _, k := range t.kids[pid] {
		out = append(out, t.procs[k])
	}
	return out
}

// Tree returns root's descendants, without root, level by level: every
// process comes after its parent.
func (t Table) Tree(root int) []Proc {
	var out []Proc
	seen := map[int]bool{root: true}
	for todo := slices.Clone(t.kids[root]); len(todo) > 0; {
		pid := todo[0]
		todo = todo[1:]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		out = append(out, t.procs[pid])
		todo = append(todo, t.kids[pid]...)
	}
	return out
}
