package core

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/sysload"
	"github.com/irie-people/irie/internal/testutil"
)

// restConfig is a test core whose fake agent draws Claude Code's prompt
// box, as a resumed agent does, so that a message waits for it.
func restConfig() agentConfig {
	return agentConfig{readyTimeout: "1s", env: map[string]string{"IRIE_FAKE_BOX": "in"}}
}

// paneDead says whether tmux has pane id dead, its process ended.
func (c *coreEnv) paneDead(id string) bool {
	out, err := c.TryTmux("display-message", "-p", "-t", id, "#{pane_dead}")
	return err == nil && strings.TrimSpace(out) == "1"
}

func (c *coreEnv) rest(args ipc.RestArgs) (ipc.RestResult, error) {
	var r ipc.RestResult
	err := c.call(ipc.OpRest, args, &r)
	return r, err
}

// idle spawns a session that ends its turn, and shows it, so that it is
// idle.
func (c *coreEnv) idle(task string) ipc.Session {
	c.t.Helper()
	s := c.spawn(task)
	c.waitState(s.ID, ipc.StateDone)
	c.show(s.ID, false)
	return c.waitState(s.ID, ipc.StateIdle)
}

func TestARestEndsTheAgentAndKeepsTheCard(t *testing.T) {
	c := setupWith(t, restConfig())
	a := c.idle("prompt,stop")
	other := c.idle("prompt,stop")
	if err := c.call(ipc.OpPark, ipc.ParkArgs{Session: a.ID}, nil); err != nil {
		t.Fatal(err)
	}
	before := c.list()

	r, err := c.rest(ipc.RestArgs{Session: a.ID})
	if err != nil || !slices.Equal(r.Sessions, []string{a.ID}) {
		t.Fatalf("rest: %+v, %v", r, err)
	}
	row := c.waitState(a.ID, ipc.StateResting)
	// The card stays as it was: its id, name, pane, place and park.
	if row.Pane != a.Pane || row.Name != a.Name || !row.Parked || row.Rest == nil || row.Rest.Why != ipc.RestYou {
		t.Errorf("resting row %+v", row)
	}
	if !strings.Contains(row.Detail, "opening it, or a message, starts its agent again") {
		t.Errorf("detail %q", row.Detail)
	}
	var order, after []string
	for _, s := range before {
		order = append(order, s.ID)
	}
	for _, s := range c.list() {
		after = append(after, s.ID)
	}
	if !slices.Equal(order, after) {
		t.Errorf("order %v, after the rest %v", after, order)
	}
	// Its agent ended gently, and its pane is dead in its window.
	testutil.Eventually(t, 10*time.Second, "the agent got no SIGTERM", func() bool { return slices.Contains(c.signals(a.ID), "terminated") })
	testutil.Eventually(t, 10*time.Second, "the resting pane is not dead", func() bool { return c.paneDead(a.Pane) })
	if r, _ := c.row(a.ID); r.State != ipc.StateResting {
		t.Errorf("once its agent ended: %+v", r)
	}
	// Freezing it, or resting it again, finds nothing to do.
	if _, err := c.rest(ipc.RestArgs{Session: a.ID}); code(err) != ipc.CodeBusy {
		t.Errorf("a second rest: %v", err)
	}
	if err := c.call(ipc.OpFreeze, ipc.FreezeArgs{Session: a.ID}, nil); err == nil {
		t.Error("a resting session froze")
	}
	// A restarted core still has it resting.
	c.restartCore()
	if r, _ := c.row(a.ID); r.State != ipc.StateResting || r.Pane != a.Pane {
		t.Errorf("after a restart: %+v", r)
	}

	// Opening it starts its agent again, carrying its conversation on, in
	// the same pane.
	c.show(a.ID, false)
	woke := c.waitState(a.ID, ipc.StateIdle)
	if woke.Pane != a.Pane || woke.Rest != nil || !woke.Parked {
		t.Errorf("woken row %+v", woke)
	}
	testutil.Eventually(t, 10*time.Second, "the agent did not start again", func() bool { return len(c.runs(a.ID)) == 2 })
	if runs := c.runs(a.ID); !slices.Contains(runs[1].argv, "--resume") || runs[1].sessionID != a.VendorID {
		t.Errorf("the agent's runs = %+v, want the second to resume %s", runs, a.VendorID)
	}
	var kinds []string
	for _, e := range c.events(a.ID) {
		switch e.Kind {
		case event.SpawnRequested, event.RestOn, event.RestOff, event.UIClosed, event.UIReopened:
			kinds = append(kinds, e.Kind)
		}
	}
	if want := []string{event.SpawnRequested, event.RestOn, event.RestOff}; !slices.Equal(kinds, want) {
		t.Errorf("journal %v, want %v", kinds, want)
	}
	// The other card was left alone.
	if r, _ := c.row(other.ID); r.State != ipc.StateIdle || len(c.runs(other.ID)) != 1 {
		t.Errorf("the other card %+v, runs %d", r, len(c.runs(other.ID)))
	}
}

func TestAMessageWakesARestingSession(t *testing.T) {
	c := setupWith(t, restConfig())
	a := c.idle("prompt,stop")
	if _, err := c.rest(ipc.RestArgs{Session: a.ID}); err != nil {
		t.Fatal(err)
	}
	c.waitState(a.ID, ipc.StateResting)
	testutil.Eventually(t, 10*time.Second, "the resting pane is not dead", func() bool { return c.paneDead(a.Pane) })
	if err := c.call(ipc.OpSend, ipc.SendArgs{Session: a.ID, Text: "carry on"}, nil); err != nil {
		t.Fatal(err)
	}
	// The woken agent took the message as a new turn.
	testutil.Eventually(t, 15*time.Second, "the woken agent never got the message", func() bool {
		woke := false
		for _, e := range c.events(a.ID) {
			switch {
			case e.Kind == event.RestOff:
				woke = true
			case woke && e.Kind == event.PromptSubmitted:
				return true
			}
		}
		return false
	})
	if runs := c.runs(a.ID); len(runs) != 2 || !slices.Contains(runs[1].argv, "--resume") {
		t.Errorf("runs %+v", runs)
	}
}

func TestARestingFrozenSessionWakesOnlyAtItsThaw(t *testing.T) {
	c := setupWith(t, restConfig())
	a := c.idle("prompt,stop")
	if err := c.call(ipc.OpFreeze, ipc.FreezeArgs{Session: a.ID}, nil); err != nil {
		t.Fatal(err)
	}
	c.waitState(a.ID, ipc.StateFrozen)
	if _, err := c.rest(ipc.RestArgs{Session: a.ID}); err != nil {
		t.Fatal(err)
	}
	row := c.waitState(a.ID, ipc.StateResting)
	if row.Frozen == nil || !strings.HasPrefix(row.Detail, "Frozen and resting") {
		t.Errorf("row %+v", row)
	}
	// A frozen agent ends too, gently: it is continued, then sent SIGTERM,
	// and nothing stops it again on the way out.
	testutil.Eventually(t, 10*time.Second, "the frozen agent did not end", func() bool { return c.paneDead(a.Pane) })
	exited := false
	for _, l := range c.agentLog(a.ID) {
		exited = exited || l["t"] == "exit"
	}
	if !exited || !slices.Contains(c.signals(a.ID), "terminated") {
		t.Errorf("the frozen agent did not end at its SIGTERM: %v", c.agentLog(a.ID))
	}
	// Opening it, or a message, leaves it be: it waits for its thaw.
	c.show(a.ID, false)
	if err := c.call(ipc.OpSend, ipc.SendArgs{Session: a.ID, Text: "hello"}, nil); code(err) != ipc.CodeBusy {
		t.Errorf("a message to a frozen resting session: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if n := len(c.runs(a.ID)); n != 1 {
		t.Fatalf("it woke while frozen: %d runs", n)
	}
	if err := c.call(ipc.OpThaw, ipc.FreezeArgs{Session: a.ID}, nil); err != nil {
		t.Fatal(err)
	}
	woke := c.waitState(a.ID, ipc.StateIdle)
	if woke.Frozen != nil {
		t.Errorf("thawed row %+v", woke)
	}
	testutil.Eventually(t, 10*time.Second, "the thaw did not start its agent", func() bool { return len(c.runs(a.ID)) == 2 })
}

func TestOnlyAnIdleSessionWithAConversationRests(t *testing.T) {
	c := setupWith(t, restConfig())
	working := c.spawn("prompt")
	c.waitState(working.ID, ipc.StateWorking)
	done := c.spawn("prompt,stop")
	c.waitState(done.ID, ipc.StateDone)
	asks := c.spawn("prompt,permission")
	c.waitState(asks.ID, ipc.StateNeedsYou)
	if _, err := c.rest(ipc.RestArgs{Session: working.ID}); code(err) != ipc.CodeBusy || !strings.Contains(err.Error(), "working") {
		t.Errorf("rest of a working session: %v", err)
	}
	if _, err := c.rest(ipc.RestArgs{Session: asks.ID}); code(err) != ipc.CodeBusy || !strings.Contains(err.Error(), "waits on you") {
		t.Errorf("rest of a session that waits on you: %v", err)
	}
	// Work of its own in the background would end with its agent.
	bg := c.idle("prompt,stop:bg")
	if _, err := c.rest(ipc.RestArgs{Session: bg.ID}); code(err) != ipc.CodeBusy || !strings.Contains(err.Error(), "background") {
		t.Errorf("rest of a session with work in the background: %v", err)
	}
	// You may rest a finished turn you have not looked at; Raffi leaves it.
	r, err := c.rest(ipc.RestArgs{Idle: true})
	if err != nil || len(r.Sessions) != 0 {
		t.Errorf("rest of every idle one: %+v, %v", r, err)
	}
	if _, err := c.rest(ipc.RestArgs{Session: done.ID}); err != nil {
		t.Errorf("rest of a finished turn: %v", err)
	}
}

func TestRaffiRestsTheLongestIdleCardsWhileMemoryIsShort(t *testing.T) {
	c := prepare(t, agentConfig{readyTimeout: "1s", extra: "[arch.gabriel]\nword = \"0s\"\n"})
	t.Setenv(brand.Env("REST"), "")
	t.Setenv(restIdleEnv, "0s")
	// 1 GiB of 16 free: 6%, under restBelow.
	t.Setenv(FakeMemoryEnv, "16/1")
	c.up()
	oldest := c.spawn("prompt,stop")
	c.waitState(oldest.ID, ipc.StateDone)
	working := c.spawn("prompt")
	c.waitState(working.ID, ipc.StateWorking)
	unseen := c.spawn("prompt,stop")
	c.waitState(unseen.ID, ipc.StateDone)
	// Seen, it is idle; the one on show is yours, and never rests.
	c.show(oldest.ID, false)
	shown := c.spawn("prompt,stop")
	c.waitState(shown.ID, ipc.StateDone)
	c.show(shown.ID, false)

	row := c.waitState(oldest.ID, ipc.StateResting)
	if row.Rest == nil || row.Rest.Why != ipc.RestMemory {
		t.Errorf("rested %+v", row)
	}
	time.Sleep(1500 * time.Millisecond)
	for _, s := range []ipc.Session{working, unseen, shown} {
		if r, _ := c.row(s.ID); r.State == ipc.StateResting {
			t.Errorf("%s rested: %+v", s.Name, r)
		}
	}
}

func TestRestShortStartsAtTheWarningAndStopsWithRoomToSpare(t *testing.T) {
	gib := int64(1 << 30)
	m := func(free int64) localmodel.Machine { return localmodel.Machine{Total: 18 * gib, Available: free * gib} }
	disk := sysload.Room{RAM: 18 * gib, DiskTotal: 926 * gib, DiskFree: 100 * gib}
	for _, c := range []struct {
		name     string
		m        localmodel.Machine
		pressure int
		warned   time.Duration
		room     sysload.Room
		was      bool
		short    bool
		atOnce   int
	}{
		{"room to spare", m(9), sysload.PressureNormal, 0, disk, false, false, 0},
		{"under restBelow", m(2), sysload.PressureNormal, 0, disk, false, true, restAtOnce},
		{"a warning that passes, as a build raises", m(9), sysload.PressureWarn, 5 * time.Second, disk, false, false, 0},
		{"the kernel's warning that lasts", m(9), sysload.PressureWarn, restWarnFor, disk, false, true, restAtOnce},
		{"the warning while short", m(9), sysload.PressureWarn, 0, disk, true, true, restAtOnce},
		{"critical", m(9), sysload.PressureCritical, 0, disk, false, true, restAtOnceDire},
		{"short until restUntil", m(4), sysload.PressureNormal, 0, disk, true, true, restAtOnce},
		{"not short past restUntil", m(5), sysload.PressureNormal, 0, disk, true, false, 0},
		{"swap with no room to grow", m(9), sysload.PressureNormal, 0, sysload.Room{RAM: 18 * gib, DiskTotal: 926 * gib, DiskFree: 8 * gib, SwapUsed: gib}, false, true, restAtOnceDire},
		{"a full disk without swap", m(9), sysload.PressureNormal, 0, sysload.Room{RAM: 18 * gib, DiskTotal: 926 * gib, DiskFree: 8 * gib}, false, false, 0},
	} {
		short, atOnce := restShort(c.m, c.pressure, c.warned, c.room, c.was)
		if short != c.short || atOnce != c.atOnce {
			t.Errorf("%s: short %v, %d at once; want %v, %d", c.name, short, atOnce, c.short, c.atOnce)
		}
	}
}

func (c *coreEnv) reopenWith(a ipc.ReopenArgs) (ipc.Session, error) {
	var r ipc.ReopenResult
	err := c.call(ipc.OpReopen, a, &r)
	return r.Session, err
}

func TestARestoreBringsBackRestingWhatWasNotAtWork(t *testing.T) {
	c := setupWith(t, restConfig())
	idle := c.idle("prompt,stop")
	frozen := c.idle("prompt,stop")
	c.freeze(frozen.ID, true)
	working := c.spawn("prompt")
	c.waitState(working.ID, ipc.StateWorking)
	c.park(idle.ID, true)

	// The server ends with the agents in it, as at a restart.
	if _, err := c.TryTmux("kill-server"); err != nil {
		t.Fatal(err)
	}
	c.up()
	lost := lostOf(c.listAll())
	if len(lost) != 3 || lost[idle.ID].Working || lost[frozen.ID].Working || !lost[working.ID].Working {
		t.Fatalf("lost %+v, want only the working one at work", lost)
	}

	// The idle one comes back resting, parked as it was, with no agent.
	s, err := c.reopenWith(ipc.ReopenArgs{Session: idle.ID, Rest: true})
	if err != nil || s.State != ipc.StateResting || s.Rest == nil || s.Rest.Why != ipc.RestRestore || !s.Parked {
		t.Fatalf("reopened %+v, %v", s, err)
	}
	// The frozen one comes back resting and frozen.
	f, err := c.reopenWith(ipc.ReopenArgs{Session: frozen.ID, Rest: true})
	if err != nil || f.State != ipc.StateResting || f.Frozen == nil {
		t.Fatalf("reopened frozen %+v, %v", f, err)
	}
	// The working one starts again.
	w, err := c.reopenWith(ipc.ReopenArgs{Session: working.ID})
	if err != nil || w.State == ipc.StateResting {
		t.Fatalf("reopened working %+v, %v", w, err)
	}
	// Past the ready timeout, the resting ones still rest: their panes
	// ran no agent, which is no failed start.
	time.Sleep(1500 * time.Millisecond)
	for _, id := range []string{idle.ID, frozen.ID} {
		if r, _ := c.row(id); r.State != ipc.StateResting {
			t.Errorf("%s after the ready timeout: %+v", id, r)
		}
		if n := len(c.runs(id)); n != 1 {
			t.Errorf("%s ran its agent %d times, want once, before the restart", id, n)
		}
	}
	testutil.Eventually(t, 10*time.Second, "the working one did not start again", func() bool { return len(c.runs(working.ID)) == 2 })

	// Opening the idle one starts its agent, carrying its conversation on.
	c.show(idle.ID, false)
	c.waitState(idle.ID, ipc.StateIdle)
	testutil.Eventually(t, 10*time.Second, "the idle one did not wake", func() bool { return len(c.runs(idle.ID)) == 2 })
	if runs := c.runs(idle.ID); runs[1].sessionID != idle.VendorID || !slices.Contains(runs[1].argv, "--resume") {
		t.Errorf("runs %+v", runs)
	}
	// The frozen one waits for its thaw.
	c.show(frozen.ID, false)
	time.Sleep(500 * time.Millisecond)
	if n := len(c.runs(frozen.ID)); n != 1 {
		t.Errorf("the frozen one woke when shown: %d runs", n)
	}
	c.freeze(frozen.ID, false)
	testutil.Eventually(t, 10*time.Second, "the thaw did not wake it", func() bool { return len(c.runs(frozen.ID)) == 2 })
}

func TestANewSessionWaitsWhileTheDiskIsUnderItsFloor(t *testing.T) {
	c := prepare(t, agentConfig{readyTimeout: "1s"})
	// 18 GiB of RAM, 8 GiB free on the disk and swap in use: under the
	// 10 GiB floor, as on 2026-11-23.
	t.Setenv(FakeRoomEnv, "18/8/1")
	c.up()
	var r ipc.SpawnResult
	err := c.call(ipc.OpSpawn, ipc.SpawnArgs{Task: "prompt,stop"}, &r)
	if code(err) != ipc.CodeBusy || !strings.Contains(err.Error(), "of the disk is free, under the 10.7 GB floor") {
		t.Fatalf("spawn below the floor: %v", err)
	}
	if ls := c.list(); len(ls) != 0 {
		t.Errorf("rows %+v", ls)
	}
	// Force starts it all the same.
	if err := c.call(ipc.OpSpawn, ipc.SpawnArgs{Task: "prompt,stop", Force: true}, &r); err != nil {
		t.Fatal(err)
	}
	c.waitState(r.Session.ID, ipc.StateDone)
}
