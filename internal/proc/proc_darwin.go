package proc

import (
	"encoding/binary"
	"fmt"
	"runtime"

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

func read() ([]Proc, error) { return readKinfo("kern.proc.all") }

// readGroup reads one process group with a single sysctl (kern.proc.pgrp).
func readGroup(pgid int) ([]Proc, error) { return readKinfo("kern.proc.pgrp", pgid) }

func readOne(pid int) (Proc, error) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return Proc{}, err
	}
	if k.Proc.P_pid != int32(pid) {
		// The system answers a pid that names no process with an empty
		// record.
		return Proc{}, unix.ESRCH
	}
	return fromKinfo(k), nil
}

func readKinfo(name string, args ...int) ([]Proc, error) {
	kps, err := unix.SysctlKinfoProcSlice(name, args...)
	if err != nil {
		return nil, err
	}
	out := make([]Proc, 0, len(kps))
	for i := range kps {
		out = append(out, fromKinfo(&kps[i]))
	}
	return out, nil
}

func fromKinfo(k *unix.KinfoProc) Proc {
	return Proc{
		PID: int(k.Proc.P_pid), PPID: int(k.Eproc.Ppid), PGID: int(k.Eproc.Pgid),
		Start:   k.Proc.P_starttime.Sec*1_000_000 + int64(k.Proc.P_starttime.Usec),
		Stopped: k.Proc.P_stat == sstop, Zombie: k.Proc.P_stat == szomb,
		Name: unix.ByteSliceToString(k.Proc.P_comm[:]),
	}
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

// readLoad reads the run queue's averages (vm.loadavg, a struct loadavg:
// three fixed-point numbers, then their scale as a long) and the kernel's
// memory pressure.
func readLoad() (Load, error) {
	b, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return Load{}, err
	}
	if len(b) < 24 {
		return Load{}, fmt.Errorf("vm.loadavg: %d bytes, want 24", len(b))
	}
	scale := binary.LittleEndian.Uint64(b[16:24])
	if scale == 0 {
		return Load{}, fmt.Errorf("vm.loadavg: no scale")
	}
	l := Load{Avg: float64(binary.LittleEndian.Uint32(b[0:4])) / float64(scale), CPUs: runtime.NumCPU()}
	if n, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level"); err == nil {
		l.Memory = int(n)
	}
	return l, nil
}
