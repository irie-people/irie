package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// proc is one process as ps listed it.
type proc struct {
	pid, ppid int
	cpu       time.Duration // user and system time so far
	rss       int64         // resident size in bytes
	comm      string        // the executable's path
}

// table is the machine's processes at one moment, by pid.
type table struct {
	at    time.Time
	procs map[int]proc
	kids  map[int][]int
}

// readTable runs ps once.
func readTable() (table, error) {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,time=,rss=,comm=").Output()
	if err != nil {
		return table{}, fmt.Errorf("ps: %w", err)
	}
	return parseTable(out, time.Now()), nil
}

// parseTable reads `ps -o pid=,ppid=,time=,rss=,comm=`. The command comes
// last, so it may hold spaces.
func parseTable(out []byte, at time.Time) table {
	t := table{at: at, procs: map[int]proc{}, kids: map[int][]int{}}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		cpu, err3 := parseCPU(f[2])
		kb, err4 := strconv.ParseInt(f[3], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		t.procs[pid] = proc{pid: pid, ppid: ppid, cpu: cpu, rss: kb << 10, comm: strings.Join(f[4:], " ")}
		if pid != ppid {
			t.kids[ppid] = append(t.kids[ppid], pid)
		}
	}
	return t
}

// parseCPU reads ps's cumulative CPU time: macOS prints minutes:seconds
// with hundredths (102:13.10), Linux [days-]hours:minutes:seconds.
func parseCPU(s string) (time.Duration, error) {
	var days int64
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.ParseInt(d, 10, 64)
		if err != nil {
			return 0, err
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("cpu time %q", s)
	}
	sec, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, err
	}
	total := sec
	mult := 60.0
	for i := len(parts) - 2; i >= 0; i-- {
		n, err := strconv.ParseInt(parts[i], 10, 64)
		if err != nil {
			return 0, err
		}
		total += float64(n) * mult
		mult *= 60
	}
	total += float64(days) * 86400
	return time.Duration(total * float64(time.Second)), nil
}

// tree is root and every process under it.
func (t table) tree(root int) []int {
	if _, ok := t.procs[root]; !ok {
		return nil
	}
	var out []int
	for todo := []int{root}; len(todo) > 0; {
		pid := todo[len(todo)-1]
		todo = append(todo[:len(todo)-1], t.kids[pid]...)
		out = append(out, pid)
	}
	return out
}

// Proc is one part of Irie over a window: how many processes it had at the
// end, the share of one core they used, and their resident size at the end.
type Proc struct {
	N   int     `json:"n"`
	CPU float64 `json:"cpu"` // percent of one core
	RSS int64   `json:"rss"` // bytes
}

// usage is a set of processes' CPU between two tables, as a share of one
// core, and their size in the second. A process in both counts what it used
// between them; one that started between them counts all it used; one that
// ended counts nothing, since ps no longer lists it.
func usage(a, b table, pids []int) Proc {
	var cpu time.Duration
	var out Proc
	for _, pid := range pids {
		p, ok := b.procs[pid]
		if !ok {
			continue
		}
		out.N++
		out.RSS += p.rss
		if q, ok := a.procs[pid]; ok && q.comm == p.comm {
			cpu += max(p.cpu-q.cpu, 0)
		} else {
			cpu += p.cpu
		}
	}
	if secs := b.at.Sub(a.at).Seconds(); secs > 0 {
		out.CPU = 100 * cpu.Seconds() / secs
	}
	return out
}

// panes lists each Irie pane's role and process from the tmux server.
func panes(socket string) (map[string][]int, int, error) {
	out, err := exec.Command("tmux", "-L", socket, "list-panes", "-a", "-F", "#{@irie_role} #{pane_pid}").Output()
	if err != nil {
		return nil, 0, fmt.Errorf("tmux -L %s list-panes: %w", socket, err)
	}
	roles := map[string][]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		role, pid, ok := strings.Cut(line, " ")
		if n, err := strconv.Atoi(pid); ok && err == nil && role != "" {
			roles[role] = append(roles[role], n)
		}
	}
	server := 0
	if out, err := exec.Command("tmux", "-L", socket, "display-message", "-p", "#{pid}").Output(); err == nil {
		server, _ = strconv.Atoi(strings.TrimSpace(string(out)))
	}
	return roles, server, nil
}

// parts groups the processes of a table into Irie's parts: the core, the
// sidebar, the placeholder, the phone gateway, the pane hosts, the agents
// under them, the tmux server and the Mac app.
func parts(t table, roles map[string][]int, server int) map[string][]int {
	out := map[string][]int{}
	for role, name := range map[string]string{"core": "core", "ui": "sidebar", "placeholder": "placeholder", "remote": "remote"} {
		for _, pid := range roles[role] {
			out[name] = append(out[name], t.tree(pid)...)
		}
	}
	for _, host := range roles["agent"] {
		if _, ok := t.procs[host]; !ok {
			continue
		}
		out["hosts"] = append(out["hosts"], host)
		for _, pid := range t.tree(host)[1:] {
			out["agents"] = append(out["agents"], pid)
		}
	}
	if server > 0 {
		out["tmux"] = []int{server}
	}
	for pid, p := range t.procs {
		if strings.HasSuffix(p.comm, "/Irie.app/Contents/MacOS/Irie") {
			out["app"] = append(out["app"], pid)
		}
	}
	return out
}
