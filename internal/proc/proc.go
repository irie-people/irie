// Package proc reads the machine's process table, for what Irie does to a
// session's whole process tree: freeze stops it and thaw continues it
// (DESIGN §4.3), and promote puts the other sessions' trees in the
// background. One read is a single sysctl on macOS and a walk of /proc on
// Linux; neither starts a process.
package proc

import (
	"slices"
)

// Proc is one process.
type Proc struct {
	PID, PPID, PGID int
	// Start is when the process started, in the system's own unit. It tells
	// a process from a later one that reuses its pid.
	Start   int64
	Stopped bool   // stopped by a signal: ps's T
	Zombie  bool   // exited, not yet reaped
	Name    string // the command's name, cut short by the system
}

// Table is every process on the machine at one moment.
type Table struct {
	procs map[int]Proc
	kids  map[int][]int // by parent, in pid order
}

// Read returns the process table.
func Read() (Table, error) {
	ps, err := read()
	if err != nil {
		return Table{}, err
	}
	return NewTable(ps), nil
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
