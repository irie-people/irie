// Package sysload reads how busy the machine is: the work waiting to run,
// the CPUs, the kernel's memory pressure and the ptys in use. A core shows
// it in its header, which is also what it advertises to the cores it is
// paired with (docs/proposals/multi-host.md §4.2). A read is a few sysctls
// on macOS and a few small files on Linux; it starts no process.
package sysload

import "runtime"

// Load is how busy the machine is at one moment.
type Load struct {
	CPUs int // logical CPUs
	// RunQueue is the work waiting to run, averaged over the last minute:
	// how many CPUs it would keep busy.
	RunQueue float64
	// Pressure is the kernel's memory pressure: PressureNormal, Warn or
	// Critical, or 0 where the system does not say.
	Pressure int
	PTYs     int // ptys in use: every tmux pane takes one
	PTYMax   int // the most there can be; 0 where the system does not say
}

// The kernel's memory pressure, as macOS numbers its levels.
const (
	PressureNormal   = 1
	PressureWarn     = 2
	PressureCritical = 4
)

// Read reads the machine's load.
func Read() (Load, error) {
	l, err := read()
	if l.CPUs == 0 {
		l.CPUs = runtime.NumCPU()
	}
	return l, err
}
