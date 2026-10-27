package proc

import (
	"golang.org/x/sys/unix"
)

// From the macOS SDK: sys/proc.h for the states, sys/resource.h for the
// background state that setpriority(2) sets.
const (
	sstop = 4 // SSTOP
	szomb = 5 // SZOMB

	prioDarwinProcess = 4      // PRIO_DARWIN_PROCESS: the second argument is a pid
	prioDarwinBG      = 0x1000 // PRIO_DARWIN_BG
)

// CanBackground reports whether this system has a background state that a
// process can put another of its user's processes in, and take it out of.
const CanBackground = true

func read() ([]Proc, error) {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	out := make([]Proc, 0, len(kps))
	for i := range kps {
		k := &kps[i]
		out = append(out, Proc{
			PID: int(k.Proc.P_pid), PPID: int(k.Eproc.Ppid), PGID: int(k.Eproc.Pgid),
			Start:   k.Proc.P_starttime.Sec*1_000_000 + int64(k.Proc.P_starttime.Usec),
			Stopped: k.Proc.P_stat == sstop, Zombie: k.Proc.P_stat == szomb,
			Name: unix.ByteSliceToString(k.Proc.P_comm[:]),
		})
	}
	return out, nil
}

// SetBackground puts a process in macOS's background state, or takes it
// out: the lowest CPU priority, throttled disk I/O, and throttled network
// I/O on sockets it opens from then on (setpriority(2), taskpolicy(8)'s -b
// and -B). Its children from then on inherit the state; those it has
// already do not. Any process may do this to another of its user's, and
// undo it, which a raised nice value would not allow without root.
func SetBackground(pid int, on bool) error {
	prio := 0
	if on {
		prio = prioDarwinBG
	}
	return unix.Setpriority(prioDarwinProcess, pid, prio)
}
