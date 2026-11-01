package core

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/sysload"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/testutil/peertest"
)

// writeHooks writes a settings.json with these hooks under home.
func writeHooks(t *testing.T, home, hooks string) {
	t.Helper()
	testutil.WriteFiles(t, home, map[string]string{".claude/settings.json": `{"model":"opus","hooks":` + hooks + `}`})
}

func TestTwoMachinesTellTheyRunTheSameHooks(t *testing.T) {
	// Each machine has the guard's program in a folder of its own.
	mac, linux := t.TempDir(), t.TempDir()
	for _, home := range []string{mac, linux} {
		testutil.WriteFiles(t, home, map[string]string{".bun/bin/bun": "#!/bin/sh\n"})
		os.Chmod(filepath.Join(home, ".bun/bin/bun"), 0o755)
	}
	guard := func(home string) string {
		return `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + home + `/.bun/bin/bun \"$CLAUDE_PROJECT_DIR/guard.ts\""}]}],` +
			`"Stop":[{"hooks":[{"type":"command","command":"NOTIFY=1 ~/bin/say done"}]}]}`
	}
	writeHooks(t, mac, guard(mac))
	writeHooks(t, linux, guard(linux))
	a, missA := userHooks(mac, "/nonexistent")
	b, missB := userHooks(linux, "/nonexistent")
	if len(a) != 2 || !slices.Equal(a, b) {
		t.Errorf("the same hooks differ: %v and %v", a, b)
	}
	// ~/bin/say is on neither machine.
	if !slices.Equal(missA, []string{"~/bin/say"}) || !slices.Equal(missB, missA) {
		t.Errorf("missing = %v, %v", missA, missB)
	}

	// A machine without the guard runs other hooks.
	bare := t.TempDir()
	writeHooks(t, bare, `{"Stop":[{"hooks":[{"type":"command","command":"NOTIFY=1 ~/bin/say done"}]}]}`)
	c, _ := userHooks(bare, "/nonexistent")
	if len(c) != 1 || slices.Equal(c, a) {
		t.Errorf("hooks without the guard = %v", c)
	}
	if h, m := userHooks(t.TempDir(), "/nonexistent"); h != nil || m != nil {
		t.Errorf("no settings: %v, %v", h, m)
	}
}

// placeCore is a core with peers whose adverts the test sets, in auto.
func placeCore(t *testing.T, busyHere bool) *Core {
	t.Helper()
	c := newPeerCore(t)
	c.cfg().Place.Mode = config.PlaceAuto
	c.cfg().Peers = map[string]config.Peer{"Asahi": {Repos: []string{"irie"}}, "Atlas": {}}
	c.machine = localmodel.Machine{Total: 18 << 30, Available: 6 << 30}
	c.load = sysload.Load{CPUs: 12, RunQueue: 4, Pressure: sysload.PressureNormal, PTYs: 100, PTYMax: 511}
	if busyHere {
		c.load.RunQueue = 16
	}
	c.mach = &ipc.Machine{Name: "Mac", Hooks: []string{"h1"}}
	return c
}

func peerWith(name string, runQueue float64, freeGiB int64, m ipc.Machine) *peerState {
	return &peerState{name: name, up: true, stop: func() {}, rows: map[string]ipc.Session{}, header: &ipc.Header{
		Load:    &ipc.Load{CPUs: 10, RunQueue: runQueue, Pressure: sysload.PressureNormal, PTYs: 8, PTYMax: 4096},
		Memory:  &ipc.Memory{Total: 16 << 30, Available: freeGiB << 30},
		Machine: &m,
	}}
}

func TestTheBalancerMovesATaskOnlyWhenItShould(t *testing.T) {
	ok := ipc.Machine{Claude: "ok", Hooks: []string{"h1", "h2"}, Profiles: []string{"claude", "sonnet"}, Disk: 300 << 30}
	now := time.Now()
	for _, tc := range []struct {
		name     string
		busyHere bool
		peers    []*peerState
		args     ipc.SpawnArgs
		want     string
	}{
		{"a calm machine keeps its task", false, []*peerState{peerWith("Asahi", 0.5, 8, ok)}, ipc.SpawnArgs{Task: "x"}, ""},
		{"a busy one sends it where there is room", true, []*peerState{peerWith("Asahi", 0.5, 8, ok)}, ipc.SpawnArgs{Task: "x"}, "Asahi"},
		{"to the roomier peer", true, []*peerState{peerWith("Asahi", 6, 8, ok), peerWith("Atlas", 0.5, 8, ok)}, ipc.SpawnArgs{Task: "x"}, "Atlas"},
		{"never where a hook program is missing", true, []*peerState{peerWith("Asahi", 0.5, 8, func() ipc.Machine { m := ok; m.HooksMissing = []string{"bun"}; return m }())}, ipc.SpawnArgs{Task: "x"}, ""},
		{"never where the hooks differ", true, []*peerState{peerWith("Asahi", 0.5, 8, func() ipc.Machine { m := ok; m.Hooks = []string{"h2"}; return m }())}, ipc.SpawnArgs{Task: "x"}, ""},
		{"never where Claude Code is not signed in", true, []*peerState{peerWith("Asahi", 0.5, 8, func() ipc.Machine { m := ok; m.Claude = "missing"; return m }())}, ipc.SpawnArgs{Task: "x"}, ""},
		{"never with a profile it lacks", true, []*peerState{peerWith("Asahi", 0.5, 8, ok)}, ipc.SpawnArgs{Task: "x", Profile: "local"}, ""},
		{"a repo listed for it goes", true, []*peerState{peerWith("Asahi", 0.5, 8, ok)}, ipc.SpawnArgs{Task: "x", Repo: "irie"}, "Asahi"},
		{"a repo not listed stays", true, []*peerState{peerWith("Atlas", 0.5, 8, ok)}, ipc.SpawnArgs{Task: "x", Repo: "irie"}, ""},
		{"a delegated subtask stays with its parent", true, []*peerState{peerWith("Asahi", 0.5, 8, ok)}, ipc.SpawnArgs{Task: "x", Delegate: true, Parent: "s1"}, ""},
		{"not to a peer with too little more room", true, []*peerState{peerWith("Asahi", 7, 3, ok)}, ipc.SpawnArgs{Task: "x"}, ""},
		{"not to a peer that is down", true, []*peerState{func() *peerState { p := peerWith("Asahi", 0.5, 8, ok); p.up = false; return p }()}, ipc.SpawnArgs{Task: "x"}, ""},
	} {
		c := placeCore(t, tc.busyHere)
		c.peers = map[string]*peerState{}
		for _, p := range tc.peers {
			c.peers[p.name] = p
		}
		c.mu.Lock()
		p, why := c.placeAuto(tc.args, now)
		c.mu.Unlock()
		got := ""
		if p != nil {
			got = p.name
			if !strings.Contains(why, "Mac was busy (run queue 16 on 12 CPUs)") || !strings.Contains(why, got+" had") {
				t.Errorf("%s: why = %q", tc.name, why)
			}
		}
		if got != tc.want {
			t.Errorf("%s: placed on %q, want %q", tc.name, got, tc.want)
		}
	}

	// A burst does not all go to one machine: each task placed there counts
	// against it until its load shows it.
	c := placeCore(t, true)
	c.peers = map[string]*peerState{"Asahi": peerWith("Asahi", 0.5, 8, ok)}
	n := 0
	for i := 0; i < 10; i++ {
		c.mu.Lock()
		p, _ := c.placeAuto(ipc.SpawnArgs{Task: "x"}, now)
		if p != nil {
			p.placed = append(p.placed, now)
			n++
		}
		c.mu.Unlock()
	}
	if n == 0 || n > 4 {
		t.Errorf("%d of 10 tasks went to the one peer", n)
	}
	c.mu.Lock()
	if p, _ := c.placeAuto(ipc.SpawnArgs{Task: "x"}, now.Add(placeHold+time.Second)); p == nil {
		t.Error("the peer took nothing more once its placements aged")
	}
	c.mu.Unlock()

	// With place.mode = local, nothing moves.
	c = placeCore(t, true)
	c.cfg().Place.Mode = config.PlaceLocal
	c.peers = map[string]*peerState{"Asahi": peerWith("Asahi", 0.5, 8, ok)}
	c.mu.Lock()
	if p, _ := c.placeAuto(ipc.SpawnArgs{Task: "x"}, now); p != nil {
		t.Error("local mode placed a task on a peer")
	}
	c.mu.Unlock()
}

func TestATaskTypedHereGoesToTheRoomierMachine(t *testing.T) {
	g := peertest.Start(t)
	g.Core.Start(nil, &ipc.Header{
		Load:    &ipc.Load{CPUs: 10, RunQueue: 0.5, Pressure: 1, PTYs: 8, PTYMax: 4096},
		Memory:  &ipc.Memory{Total: 16 << 30, Available: 8 << 30},
		Machine: &ipc.Machine{Name: "Asahi", Claude: "ok", Profiles: []string{"claude"}},
	})
	g.Core.Set(ipc.OpInput, ipc.InputResult{Did: "started", Op: ipc.OpSpawn, Text: "Started fix it.", Session: &ipc.Session{ID: "s5", Name: "fix it"}})
	c := placeCore(t, true)
	c.mach = &ipc.Machine{Name: "Mac"}
	pairWith(t, c, g, "Asahi")
	testutil.Eventually(t, 10*time.Second, "the peer never came up", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		p := c.peers["Asahi"]
		return p.up && p.header != nil && p.header.Machine != nil
	})
	r, err := c.input(ipc.InputArgs{Text: "fix it"})
	if err != nil {
		t.Fatal(err)
	}
	if calls := g.Core.Calls(ipc.OpInput); len(calls) != 1 || calls[0].Args["text"] != "fix it" {
		t.Errorf("input there = %+v", calls)
	}
	if r.Session == nil || r.Session.ID != "Asahi/s5" || !strings.Contains(r.Text, "Mac was busy") {
		t.Errorf("result = %+v", r)
	}

	// ^name is refused where the hooks differ.
	c.mu.Lock()
	c.mach = &ipc.Machine{Name: "Mac", Hooks: []string{"guard"}}
	c.mu.Unlock()
	if _, err := c.input(ipc.InputArgs{Text: "^asahi fix it"}); err == nil || !strings.Contains(err.Error(), "doesn't run the Claude Code hooks") {
		t.Errorf("^asahi with other hooks: %v", err)
	}
	if n := len(g.Core.Calls(ipc.OpInput)); n != 1 {
		t.Errorf("%d inputs reached the peer", n)
	}
}
