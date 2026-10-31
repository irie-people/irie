package proc

import (
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTreeListsParentsBeforeChildren(t *testing.T) {
	tb := NewTable([]Proc{
		{PID: 1, PPID: 0}, {PID: 10, PPID: 1}, {PID: 20, PPID: 10}, {PID: 12, PPID: 10},
		{PID: 30, PPID: 20}, {PID: 40, PPID: 1},
	})
	var got []int
	for _, p := range tb.Tree(10) {
		got = append(got, p.PID)
	}
	if want := []int{12, 20, 30}; !slices.Equal(got, want) {
		t.Errorf("Tree(10) = %v, want %v", got, want)
	}
	if n := len(tb.Tree(30)); n != 0 {
		t.Errorf("a leaf has %d descendants", n)
	}
}

func TestAliveTellsAReusedPidApart(t *testing.T) {
	tb := NewTable([]Proc{{PID: 5, PPID: 1, Start: 100}, {PID: 6, PPID: 1, Start: 50, Zombie: true}})
	if !tb.Alive(Proc{PID: 5, Start: 100}) {
		t.Error("the same process is not alive")
	}
	if tb.Alive(Proc{PID: 5, Start: 99}) {
		t.Error("a process that reused the pid counts as the old one")
	}
	if tb.Alive(Proc{PID: 6, Start: 50}) {
		t.Error("a zombie counts as alive")
	}
}

func TestReadSeesAStoppedChildInItsOwnGroup(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no process table here")
	}
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		syscall.Kill(pid, syscall.SIGKILL)
		cmd.Wait()
	})
	tb, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := tb.Get(pid)
	if !ok {
		t.Fatal("the child is not in the table")
	}
	if p.PPID != os.Getpid() || p.PGID != pid || p.Stopped || p.Name != "sleep" {
		t.Errorf("child = %+v, want ppid %d, its own group, running, named sleep", p, os.Getpid())
	}
	if !slices.ContainsFunc(tb.Tree(os.Getpid()), func(q Proc) bool { return q.PID == pid }) {
		t.Error("the child is not in this process's tree")
	}
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		tb, err = Read()
		if err != nil {
			t.Fatal(err)
		}
		if p, _ := tb.Get(pid); p.Stopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stopped child does not read as stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !tb.Alive(p) {
		t.Error("the stopped child does not read as alive")
	}
}

func TestSetBackgroundTakesAProcessInAndOut(t *testing.T) {
	if !CanBackground {
		if err := SetBackground(os.Getpid(), true); err == nil {
			t.Error("SetBackground works where CanBackground says it cannot")
		}
		t.Skip("no background state here")
	}
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		syscall.Kill(pid, syscall.SIGKILL)
		cmd.Wait()
	})
	// ps shows the scheduling priority, which the background state lowers
	// to the least there is.
	pri := func() string {
		out, err := exec.Command("ps", "-o", "pri=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	// While a session is promoted, or the machine is busy and the session
	// is not on show, the running Irie keeps the session's tree in the
	// background and puts this child back there each second, so nothing
	// here could show a difference.
	if self, _ := exec.Command("ps", "-o", "pri=", "-p", strconv.Itoa(os.Getpid())).Output(); strings.TrimSpace(string(self)) == "4" {
		t.Skip("the test runs in the background, as a session's processes do while another is promoted or on show")
	}
	// Out of the background first: while a session is promoted, the others'
	// processes, a test one of them runs too, start their children there.
	if err := SetBackground(pid, false); err != nil {
		t.Fatal(err)
	}
	before := pri()
	if err := SetBackground(pid, true); err != nil {
		t.Fatal(err)
	}
	if got := pri(); got != "4" {
		t.Errorf("in the background the priority is %s, want 4 (it was %s)", got, before)
	}
	if err := SetBackground(pid, false); err != nil {
		t.Fatal(err)
	}
	if got := pri(); got != before {
		t.Errorf("out of the background the priority is %s, want %s again", got, before)
	}
}

func TestReadLoadSaysHowBusyTheMachineIs(t *testing.T) {
	l, err := ReadLoad()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		if err == nil {
			t.Error("ReadLoad works on a system it does not know")
		}
		t.Skip("no load here")
	}
	if err != nil {
		t.Fatal(err)
	}
	if l.CPUs < 1 || l.Avg < 0 || l.Avg > float64(l.CPUs)*1000 {
		t.Errorf("load = %+v", l)
	}
	if runtime.GOOS == "darwin" && l.Memory != 1 && l.Memory != 2 && l.Memory != 4 {
		t.Errorf("memory pressure = %d, want 1, 2 or 4", l.Memory)
	}
}

func TestGroupAndOneSeeAStoppedChild(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no process table here")
	}
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	g, err := Group(pid)
	if err != nil || len(g) != 1 || g[0].PID != pid || g[0].PGID != pid || g[0].Name != "sleep" {
		t.Fatalf("Group(%d) = %+v, %v; want the child alone", pid, g, err)
	}
	syscall.Kill(pid, syscall.SIGSTOP)
	stopped := func() bool {
		p, err := One(pid)
		return err == nil && p.Stopped
	}
	for deadline := time.Now().Add(5 * time.Second); !stopped() && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if !stopped() {
		t.Fatal("One never saw the child stopped")
	}
	syscall.Kill(pid, syscall.SIGCONT)
	for deadline := time.Now().Add(5 * time.Second); stopped() && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if p, err := One(pid); err != nil || p.Stopped || p.PPID != os.Getpid() {
		t.Errorf("after SIGCONT: %+v, %v", p, err)
	}
	cmd.Process.Kill()
	cmd.Wait()
	if p, err := One(pid); err == nil {
		t.Errorf("One of a reaped child = %+v, want an error", p)
	}
}
