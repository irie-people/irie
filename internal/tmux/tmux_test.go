package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/testutil"
)

var want = Want{Bin: "/opt/irie/bin/irie", SidebarWidth: 34, Version: "v1"}

// sound is a healthy layout: the core window, the sidebar and the placeholder
// in irie:0, and one agent window sized like the slot.
func sound() Snapshot {
	return Snapshot{Panes: []Pane{
		{Session: AgentsSession, WindowID: "@1", WindowIndex: 0, WindowPanes: 1, ID: "%0", Index: 0, Width: 80, Height: 24, WindowWidth: 80, WindowHeight: 24, Role: RoleCore},
		{Session: MainSession, WindowID: "@2", WindowIndex: 0, WindowPanes: 2, ID: "%1", Index: 0, Width: 34, Height: 24, WindowWidth: 80, WindowHeight: 24, Role: RoleUI, Version: "v1"},
		{Session: MainSession, WindowID: "@2", WindowIndex: 0, WindowPanes: 2, ID: "%2", Index: 1, Width: 45, Height: 24, WindowWidth: 80, WindowHeight: 24, Role: RolePlaceholder},
		{Session: AgentsSession, WindowID: "@3", WindowIndex: 1, WindowPanes: 1, ID: "%3", Index: 0, Width: 45, Height: 24, WindowWidth: 45, WindowHeight: 24, Role: RoleAgent, IrieSession: "s1"},
	}}
}

func edit(s Snapshot, id string, f func(*Pane)) Snapshot {
	out := Snapshot{Panes: slices.Clone(s.Panes)}
	for i := range out.Panes {
		if out.Panes[i].ID == id {
			f(&out.Panes[i])
		}
	}
	return out
}

func drop(s Snapshot, id string) Snapshot {
	out := Snapshot{}
	for _, p := range s.Panes {
		if p.ID != id {
			out.Panes = append(out.Panes, p)
		}
	}
	return out
}

// shown returns sound() with the agent %3 in the slot and the placeholder %2
// in the agent's window.
func shown() Snapshot {
	s := edit(sound(), "%2", func(p *Pane) {
		p.Session, p.WindowID, p.WindowIndex, p.WindowPanes, p.Index = AgentsSession, "@3", 1, 1, 0
		p.WindowWidth, p.WindowHeight = 45, 24
	})
	return edit(s, "%3", func(p *Pane) {
		p.Session, p.WindowID, p.WindowIndex, p.WindowPanes, p.Index = MainSession, "@2", 0, 2, 1
		p.WindowWidth, p.WindowHeight = 80, 24
	})
}

func first(cmds [][]string) string {
	if len(cmds) == 0 {
		return ""
	}
	return strings.Join(cmds[0], " ")
}

func TestPlanLeavesASoundLayoutAlone(t *testing.T) {
	if cmds := Plan(sound(), want); cmds != nil {
		t.Errorf("plan = %q", cmds)
	}
	if cmds := Plan(shown(), want); cmds != nil {
		t.Errorf("plan with an agent shown = %q", cmds)
	}
}

func TestPlanRepairs(t *testing.T) {
	s := sound()
	twoPlaceholders := Snapshot{Panes: append(slices.Clone(s.Panes),
		Pane{Session: AgentsSession, WindowID: "@5", WindowIndex: 2, WindowPanes: 1, ID: "%5", Width: 45, Height: 24, WindowWidth: 45, WindowHeight: 24, Role: RolePlaceholder})}
	extraInMain := Snapshot{Panes: append(slices.Clone(s.Panes),
		Pane{Session: MainSession, WindowID: "@2", WindowIndex: 0, WindowPanes: 3, ID: "%6", Index: 2, Width: 10, Height: 24, WindowWidth: 80, WindowHeight: 24, Role: RoleAgent, IrieSession: "s6"})}
	lonePlaceholder := edit(drop(s, "%3"), "%2", func(p *Pane) {
		p.Session, p.WindowID, p.WindowIndex, p.WindowPanes, p.Index = AgentsSession, "@4", 1, 1, 0
	})

	for _, c := range []struct {
		name string
		snap Snapshot
		want string
	}{
		{"no main session", drop(drop(s, "%1"), "%2"), "new-session -d -s irie"},
		{"slot missing", drop(s, "%2"), "split-window -h -d -t %1"},
		{"slot missing, placeholder elsewhere", lonePlaceholder, "join-pane -h -d -s %2 -t %1"},
		{"sidebar missing", drop(s, "%1"), "split-window -h -b -d -t %2"},
		{"sidebar dead", edit(s, "%1", func(p *Pane) { p.Dead = true }), "respawn-pane -k -t %1"},
		{"sidebar too wide", edit(edit(s, "%1", func(p *Pane) { p.Width = 50 }), "%2", func(p *Pane) { p.Width = 29 }), "resize-pane -t %1 -x 34"},
		{"agent window size differs", edit(s, "%3", func(p *Pane) { p.WindowWidth, p.WindowHeight, p.Width, p.Height = 80, 24, 80, 24 }), "resize-window -t @3 -x 45 -y 24"},
		{"shown agent has no home", drop(shown(), "%2"), "new-window -d -t irie-agents: -n irie-home"},
		{"two placeholders", twoPlaceholders, "kill-pane -t %5"},
		{"extra pane in irie:0", extraInMain, "break-pane -d -s %6 -t irie-agents:"},
		{"dead placeholder", edit(s, "%2", func(p *Pane) { p.Dead = true }), "respawn-pane -k -t %2"},
	} {
		got := first(Plan(c.snap, want))
		if !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: plan starts %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPlanCreatesPanesWithRoles(t *testing.T) {
	cmds := Plan(drop(drop(sound(), "%1"), "%2"), want)
	joined := fmt.Sprint(cmds)
	for _, part := range []string{"/opt/irie/bin/irie ui --supervise", "@irie_role ui", "@irie_version v1"} {
		if !strings.Contains(joined, part) {
			t.Errorf("plan %q lacks %q", cmds, part)
		}
	}
}

func TestPlanRecordsThePlaceholdersVersion(t *testing.T) {
	cmds := fmt.Sprint(Plan(drop(sound(), "%2"), want))
	if !strings.Contains(cmds, "/opt/irie/bin/irie placeholder") || !strings.Contains(cmds, "@irie_version v1") {
		t.Errorf("plan = %s", cmds)
	}
}

func TestSwapInCommands(t *testing.T) {
	cmds, err := SwapIn(sound(), "%3", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{
		{"swap-pane", "-d", "-s", "%3", "-t", Slot},
		{"select-pane", "-t", Slot},
	}; !slices.EqualFunc(cmds, want, slices.Equal) {
		t.Errorf("show from the placeholder = %q", cmds)
	}

	// With an agent shown, it goes home first.
	s := Snapshot{Panes: append(shown().Panes,
		Pane{Session: AgentsSession, WindowID: "@7", WindowIndex: 3, WindowPanes: 1, ID: "%7", Width: 45, Height: 24, WindowWidth: 45, WindowHeight: 24, Role: RoleAgent, IrieSession: "s7"})}
	cmds, err = SwapIn(s, "%7", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{
		{"swap-pane", "-d", "-s", Slot, "-t", "%2"},
		{"swap-pane", "-d", "-s", "%7", "-t", Slot},
	}; !slices.EqualFunc(cmds, want, slices.Equal) {
		t.Errorf("show from another agent = %q", cmds)
	}

	// Showing what is already shown only focuses it.
	cmds, _ = SwapIn(shown(), "%3", true)
	if want := [][]string{{"select-pane", "-t", Slot}}; !slices.EqualFunc(cmds, want, slices.Equal) {
		t.Errorf("show of the shown agent = %q", cmds)
	}
	if _, err := SwapIn(sound(), "%99", false); err == nil {
		t.Error("no error for an unknown pane")
	}
}

func TestQuoteArgEndingInSemicolon(t *testing.T) {
	got := listArgs([][]string{{"display-message", "a;"}, {"set-option", "x", "y"}})
	want := []string{"display-message", `a\;`, ";", "set-option", "x", "y"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
}

// Tests against a private tmux server.

func fakeBin(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "irie")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexec sleep 600\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func startServer(t *testing.T) (*Runner, Want) {
	t.Helper()
	bin := testutil.NeedTmux(t)
	e := testutil.NewEnv(t)
	fake := fakeBin(t)
	conf := filepath.Join(t.TempDir(), "tmux.conf")
	if err := os.WriteFile(conf, []byte(Conf(ConfOptions{FocusKey: "C-]", SidebarWidth: 34})), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Runner{Bin: bin, Socket: e.Socket}
	if err := r.StartServer(conf, []string{fake, "core"}); err != nil {
		t.Fatal(err)
	}
	return r, Want{Bin: fake, SidebarWidth: 34, Version: "v1"}
}

func settle(t *testing.T, r *Runner, w Want) Snapshot {
	t.Helper()
	for range 12 {
		s, err := r.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		cmds := Plan(s, w)
		if cmds == nil {
			return s
		}
		if _, err := r.RunList(cmds); err != nil {
			t.Fatalf("plan %q: %v", cmds, err)
		}
	}
	t.Fatal("layout did not settle")
	return Snapshot{}
}

func run(t *testing.T, r *Runner, args ...string) string {
	t.Helper()
	out, err := r.Run(args...)
	if err != nil {
		t.Fatalf("tmux %q: %v", args, err)
	}
	return strings.TrimSpace(out)
}

func TestConfLoadsIntoTmux(t *testing.T) {
	r, _ := startServer(t)
	for _, c := range [][2]string{
		{"show-options -gv status", "off"},
		{"show-options -gv prefix", "None"},
		{"show-options -gv mouse", "on"},
		{"show-options -gv base-index", "0"},
		{"show-options -gwv pane-base-index", "0"},
		{"show-options -gwv remain-on-exit", "on"},
		{"show-options -gv history-limit", "50000"},
		{"show-options -gwv allow-passthrough", "on"},
		{"show-options -sv focus-events", "on"},
		{"show-options -sv extended-keys", "on"},
		{"show-options -gv set-titles", "on"},
		{"show-options -gv set-titles-string", "Irie"},
		{"show-options -sv escape-time", "10"},
		{"show-options -gwv window-size", "latest"},
	} {
		if got := run(t, r, strings.Fields(c[0])...); got != c[1] {
			t.Errorf("%s = %q, want %q", c[0], got, c[1])
		}
	}
	if got := run(t, r, "show-options", "-sv", "terminal-features"); !strings.Contains(got, "xterm*:extkeys") {
		t.Errorf("terminal-features = %q", got)
	}
	var binding string
	for _, line := range strings.Split(run(t, r, "list-keys", "-T", "root"), "\n") {
		if f := strings.Fields(line); len(f) > 3 && f[3] == "C-]" {
			binding = line
		}
	}
	if !strings.Contains(binding, "select-pane -t irie:0.1") || !strings.Contains(binding, "select-pane -t irie:0.0") {
		t.Errorf("focus key binding = %q", binding)
	}
	if got := run(t, r, "show-hooks", "-g", "window-resized"); !strings.Contains(got, "resize-pane -t irie:0.0 -x 34") {
		t.Errorf("window-resized hook = %q", got)
	}
}

func TestSnapshotReadsPaneOptions(t *testing.T) {
	r, _ := startServer(t)
	run(t, r, "set-option", "-p", "-t", AgentsSession+":0", "@irie_role", RoleCore)
	run(t, r, "set-option", "-p", "-t", AgentsSession+":0", "@irie_name", "core pane")
	s, err := r.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Panes) != 1 {
		t.Fatalf("panes = %+v", s.Panes)
	}
	p := s.Panes[0]
	if p.Session != AgentsSession || p.Role != RoleCore || p.Name != "core pane" || p.PID == 0 || !strings.HasPrefix(p.ID, "%") || !strings.HasPrefix(p.WindowID, "@") {
		t.Errorf("pane = %+v", p)
	}
}

func agentWindow(t *testing.T, r *Runner, w Want, id string) string {
	t.Helper()
	out, err := r.RunList([][]string{
		{"new-window", "-d", "-t", AgentsSession + ":", "-n", id, "-P", "-F", "#{pane_id}", "--", w.Bin},
		{"set-option", "-p", "-t", AgentsSession + ":=" + id, "@irie_role", RoleAgent},
		{"set-option", "-p", "-t", AgentsSession + ":=" + id, "@irie_session", id},
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

func paneByID(s Snapshot, id string) Pane {
	for _, p := range s.Panes {
		if p.ID == id {
			return p
		}
	}
	return Pane{}
}

func TestLayoutAndSwapInKeepSizes(t *testing.T) {
	r, w := startServer(t)
	s := settle(t, r, w)
	ui, slot := s.Sidebar(), s.SlotPane()
	if ui == nil || slot == nil || ui.Role != RoleUI || slot.Role != RolePlaceholder || ui.Width != 34 {
		t.Fatalf("layout: sidebar %+v slot %+v", ui, slot)
	}
	if n := len(s.Window(MainSession, 0)); n != 2 {
		t.Fatalf("irie:0 has %d panes", n)
	}

	a := agentWindow(t, r, w, "sa")
	b := agentWindow(t, r, w, "sb")
	s = settle(t, r, w)
	sizeA := [2]int{paneByID(s, a).Width, paneByID(s, a).Height}
	sizeB := [2]int{paneByID(s, b).Width, paneByID(s, b).Height}
	if want := [2]int{slot.Width, slot.Height}; sizeA != want || sizeB != want {
		t.Fatalf("agent sizes %v %v, slot %v", sizeA, sizeB, want)
	}

	show := func(id string, focus bool) Snapshot {
		t.Helper()
		s, err := r.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		cmds, err := SwapIn(s, id, focus)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.RunList(cmds); err != nil {
			t.Fatal(err)
		}
		s, err = r.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	s = show(a, false)
	if got := s.SlotPane(); got == nil || got.ID != a {
		t.Fatalf("slot holds %+v, want %s", got, a)
	}
	s = show(b, true)
	if got := s.SlotPane(); got == nil || got.ID != b {
		t.Fatalf("slot holds %+v, want %s", got, b)
	}
	pa := paneByID(s, a)
	if pa.Session != AgentsSession || pa.WindowPanes != 1 {
		t.Errorf("A did not go home: %+v", pa)
	}
	if got := [2]int{pa.Width, pa.Height}; got != sizeA {
		t.Errorf("A resized from %v to %v", sizeA, got)
	}
	pb := paneByID(s, b)
	if got := [2]int{pb.Width, pb.Height}; got != sizeB {
		t.Errorf("B resized from %v to %v", sizeB, got)
	}
	if got := run(t, r, "display-message", "-p", "-t", MainSession+":0", "#{pane_index}"); got != "1" {
		t.Errorf("active pane after a focused show = %s, want 1", got)
	}
	if cmds := Plan(s, w); cmds != nil {
		t.Errorf("layout after swaps needs %q", cmds)
	}
}

func TestKilledSlotIsRepaired(t *testing.T) {
	r, w := startServer(t)
	settle(t, r, w)
	run(t, r, "kill-pane", "-t", Slot)
	s := settle(t, r, w)
	if slot := s.SlotPane(); slot == nil || slot.Role != RolePlaceholder {
		t.Fatalf("slot after kill = %+v", slot)
	}

	// Kill the slot while an agent is shown: its placeholder comes back.
	a := agentWindow(t, r, w, "sa")
	s = settle(t, r, w)
	cmds, err := SwapIn(s, a, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunList(cmds); err != nil {
		t.Fatal(err)
	}
	run(t, r, "kill-pane", "-t", Slot)
	s = settle(t, r, w)
	if slot := s.SlotPane(); slot == nil || slot.Role != RolePlaceholder {
		t.Fatalf("slot after killing a shown agent = %+v", slot)
	}
	if n := len(s.ByRole(RolePlaceholder)); n != 1 {
		t.Errorf("%d placeholders", n)
	}
}

func TestSnapshotReadsTheCommandAndPasteMode(t *testing.T) {
	r, _ := startServer(t)
	run(t, r, "new-window", "-d", "-t", AgentsSession+":", "-n", "paste", "--", "/bin/sh", "-c", `printf '\033[?2004h'; exec sleep 600`)
	pane := func(window string) (Pane, bool) {
		s, err := r.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range s.Panes {
			if p.WindowID == window {
				return p, true
			}
		}
		return Pane{}, false
	}
	id := run(t, r, "display-message", "-p", "-t", AgentsSession+":=paste", "#{window_id}")
	testutil.Eventually(t, 5*time.Second, "the pane did not ask for bracketed paste", func() bool {
		p, ok := pane(id)
		return ok && p.BracketPaste && p.Command == "sleep"
	})
	core := run(t, r, "display-message", "-p", "-t", AgentsSession+":0", "#{window_id}")
	// The core window runs a script that never asks for it.
	if p, ok := pane(core); !ok || p.BracketPaste || p.Command == "" {
		t.Errorf("core pane = %+v", p)
	}
}
