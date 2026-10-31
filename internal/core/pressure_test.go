package core

import (
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/proc"
)

func TestTheMachineIsBusyWhenItRunsShort(t *testing.T) {
	p := config.Default().Priority // a CPU and a GiB kept for the session on show
	gib := int64(localmodel.GiB)
	m := localmodel.Machine{Total: 18 * gib, Available: 9 * gib} // the reserve is 2 GiB
	for _, c := range []struct {
		name string
		l    proc.Load
		m    localmodel.Machine
		want string
	}{
		{"room to spare", proc.Load{Avg: 6, CPUs: 12, Memory: 1}, m, ""},
		{"one CPU left", proc.Load{Avg: 11, CPUs: 12, Memory: 1}, m, ""},
		{"less than a CPU left", proc.Load{Avg: 11.5, CPUs: 12, Memory: 1}, m, shortCPU},
		{"the kernel warns", proc.Load{Avg: 2, CPUs: 12, Memory: 2}, m, shortMemory},
		{"the kernel is critical", proc.Load{Avg: 2, CPUs: 12, Memory: 4}, m, shortMemory},
		{"less free than the reserve and the headroom", proc.Load{Avg: 2, CPUs: 12, Memory: 1},
			localmodel.Machine{Total: 18 * gib, Available: 3*gib - 1}, shortMemory},
		{"memory before CPU", proc.Load{Avg: 30, CPUs: 12, Memory: 2}, m, shortMemory},
		{"no memory reading", proc.Load{Avg: 2, CPUs: 12}, localmodel.Machine{}, ""},
	} {
		if got := (pressure{}).next(time.Now(), c.l, c.m, p).why; got != c.want {
			t.Errorf("%s: busy = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTheMachineIsCalmOnlyAfterItHasHadRoomForAWhile(t *testing.T) {
	p := config.Default().Priority
	gib := int64(localmodel.GiB)
	m := localmodel.Machine{Total: 18 * gib, Available: 9 * gib}
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	busyLoad := proc.Load{Avg: 20, CPUs: 12, Memory: 1}
	b := pressure{}.next(now, busyLoad, m, p)
	if b.why != shortCPU {
		t.Fatalf("busy = %q, want cpu", b.why)
	}
	// Under the line but not by a quarter of the CPUs: still busy, so the
	// trees do not flap in and out around it.
	b = b.next(now.Add(time.Minute), proc.Load{Avg: 10, CPUs: 12, Memory: 1}, m, p)
	if b.why != shortCPU || !b.calm.IsZero() {
		t.Fatalf("just under the line: %+v, want still busy and not calm", b)
	}
	quiet := proc.Load{Avg: 4, CPUs: 12, Memory: 1}
	start := now.Add(2 * time.Minute)
	b = b.next(start, quiet, m, p)
	if b.why != shortCPU || !b.calm.Equal(start) {
		t.Fatalf("calm begins: %+v", b)
	}
	b = b.next(start.Add(calmFor-time.Second), quiet, m, p)
	if b.why != shortCPU {
		t.Fatalf("calm for less than %v: %+v, want still busy", calmFor, b)
	}
	// A busy moment within starts the wait over.
	again := b.next(start.Add(calmFor-time.Second), busyLoad, m, p)
	if again.why != shortCPU || !again.calm.IsZero() {
		t.Fatalf("busy again: %+v", again)
	}
	b = b.next(start.Add(calmFor), quiet, m, p)
	if b.why != "" {
		t.Fatalf("calm for %v: %+v, want not busy", calmFor, b)
	}
	// Short of memory, it stays busy until twice the headroom is free.
	low := localmodel.Machine{Total: 18 * gib, Available: 2*gib + gib/2}
	b = b.next(now, quiet, low, p)
	if b.why != shortMemory {
		t.Fatalf("short of memory: %+v", b)
	}
	b = b.next(now.Add(time.Hour), quiet, localmodel.Machine{Total: 18 * gib, Available: 3*gib + gib/2}, p)
	if b.why != shortMemory || !b.calm.IsZero() {
		t.Fatalf("a GiB over the line is not calm yet: %+v", b)
	}
}

func TestAHeadroomOfEveryCPUKeepsTheMachineBusy(t *testing.T) {
	p := config.Priority{Focus: true, HeadroomCPUs: 1000}
	b := pressure{}.next(time.Now(), proc.Load{Avg: 0, CPUs: 12, Memory: 1}, localmodel.Machine{}, p)
	if b.why != shortCPU {
		t.Errorf("busy = %q, want cpu", b.why)
	}
	b = b.next(time.Now().Add(time.Hour), proc.Load{Avg: 0, CPUs: 12, Memory: 1}, localmodel.Machine{}, p)
	if b.why != shortCPU {
		t.Errorf("an hour later busy = %q, want cpu still", b.why)
	}
}
