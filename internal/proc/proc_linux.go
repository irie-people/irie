package proc

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// CanBackground reports whether this system has a background state that a
// process can put another of its user's processes in, and take it out of.
// Linux lets a process raise another's nice value but not lower it again
// without privilege, so promote leaves CPU and I/O alone here.
const CanBackground = false

func read() ([]Proc, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	out := make([]Proc, 0, len(ents))
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue // it exited meanwhile
		}
		if p, ok := parseStat(pid, string(b)); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// readGroup reads the whole table: Linux lists no process group alone.
func readGroup(pgid int) ([]Proc, error) {
	all, err := read()
	if err != nil {
		return nil, err
	}
	var out []Proc
	for _, p := range all {
		if p.PGID == pgid {
			out = append(out, p)
		}
	}
	return out, nil
}

func readOne(pid int) (Proc, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return Proc{}, err
	}
	p, ok := parseStat(pid, string(b))
	if !ok {
		return Proc{}, fmt.Errorf("/proc/%d/stat: unreadable", pid)
	}
	return p, nil
}

// parseStat reads /proc/<pid>/stat (proc(5)): pid (comm) state ppid pgrp
// session tty_nr tpgid flags … with starttime the 22nd field. The command
// may hold spaces and parentheses, so it ends at the last ')'.
func parseStat(pid int, s string) (Proc, bool) {
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return Proc{}, false
	}
	f := strings.Fields(s[end+1:])
	// f[0] is the state, the 3rd field; starttime, the 22nd, is f[19].
	if len(f) < 20 {
		return Proc{}, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	pgid, err2 := strconv.Atoi(f[2])
	start, err3 := strconv.ParseInt(f[19], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return Proc{}, false
	}
	return Proc{
		PID: pid, PPID: ppid, PGID: pgid, Start: start,
		Stopped: f[0] == "T", Zombie: f[0] == "Z", Name: s[open+1 : end],
	}, true
}

// SetBackground is not available on Linux; see CanBackground.
func SetBackground(pid int, on bool) error { return errors.ErrUnsupported }

// readLoad reads /proc/loadavg, whose first field is the one-minute
// average (proc(5)). Linux gives no single memory pressure level.
func readLoad() (Load, error) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return Load{}, err
	}
	return parseLoadavg(string(b))
}

func parseLoadavg(s string) (Load, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return Load{}, fmt.Errorf("/proc/loadavg: empty")
	}
	avg, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return Load{}, fmt.Errorf("/proc/loadavg: %w", err)
	}
	return Load{Avg: avg, CPUs: runtime.NumCPU()}, nil
}
