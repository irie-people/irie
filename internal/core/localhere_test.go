package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/ledger"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/testutil/ollamafake"
	"github.com/irie-people/irie/internal/tmux"
)

// portOf is the port of a test server's address.
func portOf(t *testing.T, raw string) int {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestNoModelLoadsOnThisComputerWhileLocalHereIsOff: with local.here
// false, neither of Irie's own servers starts, for a spawn or a look,
// and a spawn on a model of this computer is refused, saying why.
func TestNoModelLoadsOnThisComputerWhileLocalHereIsOff(t *testing.T) {
	a := agentConfig{extra: localProfile(t) + fmt.Sprintf("[profiles.big]\ncommand = %q\n", testutil.BuildFakeAgent(t))}
	log, _ := ownServer(t, &a)
	a.local = "here = false\n" + a.local + fmt.Sprintf("llama_server = %q\nllama_port = %d\n", testutil.BuildFakeLlama(t), testutil.FreePort(t))
	c := setupWith(t, a)

	for _, prof := range []string{"local", "ollama:qwen3.5:9b", "big"} {
		var r ipc.SpawnResult
		err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: prof}, &r)
		if err == nil || !strings.Contains(err.Error(), "local models are off on this computer (local.here = false)") {
			t.Errorf("spawn +%s: %v; want it refused, as local models are off here", prof, err)
		}
	}
	var lr ipc.LocalResult
	if err := c.call("local", ipc.LocalArgs{Start: true, Llama: true}, &lr); err != nil {
		t.Fatal(err)
	}
	if lr.Running || !strings.Contains(lr.Error, "local.here = false") || lr.Llama == nil || !strings.Contains(lr.Llama.Error, "local.here = false") {
		t.Errorf("ctl local = %+v; want neither server started, saying why", lr)
	}
	for _, p := range c.panes() {
		if r := p["@irie_role"]; r == tmux.RoleRuntime || r == tmux.RoleLlama {
			t.Errorf("a %s window started: %v", r, p)
		}
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Irie's own Ollama ran (%v)", err)
	}
}

// TestSideTasksDoWithoutAModelWhileLocalHereIsOff: with local.here false,
// find and the router read words only, and a card says why none is made,
// with no call to a model server on this computer.
func TestSideTasksDoWithoutAModelWhileLocalHereIsOff(t *testing.T) {
	embeds := ollamafake.New(0, []ollamafake.Model{{Name: "embed-fake", Size: 300 << 20, Embeds: true}})
	esrv := httptest.NewServer(embeds.Handler())
	defer esrv.Close()
	chats := &cardOllama{reply: `{"title":"t","body":[]}`}
	csrv := httptest.NewServer(chats)
	defer csrv.Close()
	cfg := config.Default()
	cfg.Local.Here, cfg.Local.Port = false, portOf(t, csrv.URL)
	cfg.Route.Embed.Model, cfg.Route.Embed.URL = "embed-fake", esrv.URL
	cfg.Materialize.Sessions = true
	c := bareCore(t, cfg)

	if _, err := c.embed(context.Background(), cfg.Route.Embed, []string{"fix the typo"}, ledger.Route); !errors.Is(err, localmodel.ErrOffHere) {
		t.Errorf("embed: %v; want it not asked, as local models are off here", err)
	}
	if cos, _, note := c.findMeaning("typo", []findDoc{{parts: []string{"fix the typo in the README"}}}); cos != nil || note != localmodel.ErrOffHere.Error() {
		t.Errorf("find: %v, %q; want words only", cos, note)
	}
	r, err := c.materializeOp(ipc.MaterializeArgs{ID: "x", Prompt: "Anything?", Wait: 10 * time.Second})
	if err != nil || r.State != "cold" || !strings.Contains(r.Err, "local.here = false") {
		t.Errorf("card: %+v, %v; want none made, saying why", r, err)
	}
	if _, err := c.materializeOp(ipc.MaterializeArgs{Session: "s1"}); err == nil || !strings.Contains(err.Error(), "local.here = false") {
		t.Errorf("session card: %v; want none asked for", err)
	}
	if ms, st := c.tabModels(); ms != nil || st != serverNone {
		t.Errorf("the + list: %v, %q; want no local models and no server starting", ms, st)
	}
	if embeds.Embeds != 0 || chats.count() != 0 {
		t.Errorf("%d texts embedded and %d chats; want no model called", embeds.Embeds, chats.count())
	}

	// A server on another computer may still run them.
	if err := c.modelsOff("http://gpu.example.com:11434"); err != nil {
		t.Errorf("another computer's server: %v; want it allowed", err)
	}
}

// TestASideTaskLoadsAModelOnlyWithRoomForIt: on 2026-11-23 a side task
// loaded a 9B model into a Mac already paging, with its disk 97% full, and
// the Mac panicked. A card's model now loads only into enough free memory,
// with the reserve beside, and never while the disk is under the floor
// the swap needs.
func TestASideTaskLoadsAModelOnlyWithRoomForIt(t *testing.T) {
	f := &cardOllama{reply: `{"title":"t","body":[]}`}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := cardCore(t, srv.URL)
	ask := func(id string) ipc.MaterializeResult {
		r, err := c.materializeOp(ipc.MaterializeArgs{ID: id, Prompt: "Anything?", Wait: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// qwen3:4b, 2.5 GB of weights, takes about 3 GB; with 2 GiB kept free,
	// 4 GiB free is too little.
	t.Setenv(FakeMemoryEnv, "18/4")
	if r := ask("a"); r.State != "cold" || !strings.Contains(r.Err, "too little memory is free") || !strings.Contains(r.Err, "qwen3:4b was not loaded") {
		t.Errorf("4 GiB free: %+v; want it not loaded", r)
	}
	t.Setenv(FakeMemoryEnv, "18/12")
	t.Setenv(FakeRoomEnv, "18/8/0")
	if r := ask("b"); r.State != "cold" || !strings.Contains(r.Err, "floor") || !strings.Contains(r.Err, "qwen3:4b was not loaded") {
		t.Errorf("8 GiB of disk free: %+v; want it not loaded", r)
	}
	if f.count() != 0 {
		t.Fatalf("%d chats; want none while there is no room", f.count())
	}
	t.Setenv(FakeRoomEnv, "18/40/0")
	if r := ask("c"); r.State != "fresh" || f.count() != 1 {
		t.Errorf("with room: %+v, %d chats; want the card made", r, f.count())
	}
}

// psOllama is Irie's own Ollama as easeModels sees it: what it has loaded,
// and the models it is told to let go (keep_alive 0).
type psOllama struct {
	mu       sync.Mutex
	loaded   []string
	unloaded []string
}

func (f *psOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/api/ps":
		var ms []map[string]any
		for _, m := range f.loaded {
			ms = append(ms, map[string]any{"name": m, "size": 5 << 30})
		}
		json.NewEncoder(w).Encode(map[string]any{"models": ms})
	case "/api/generate":
		var asked struct {
			Model     string
			KeepAlive *float64 `json:"keep_alive"`
		}
		json.NewDecoder(r.Body).Decode(&asked)
		if asked.KeepAlive != nil && *asked.KeepAlive == 0 {
			f.loaded = slices.DeleteFunc(f.loaded, func(m string) bool { return m == asked.Model })
			f.unloaded = append(f.unloaded, asked.Model)
		}
		fmt.Fprint(w, `{"done":true}`)
	default:
		http.NotFound(w, r)
	}
}

func (f *psOllama) gone() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.unloaded)
}

// easeCore is a bare core whose tmux is a script that writes down what it
// is asked to do, for easeModels to stop servers with.
func easeCore(t *testing.T, cfg config.Config) (c *Core, ran func() string) {
	t.Helper()
	c = bareCore(t, cfg)
	dir := t.TempDir()
	log := filepath.Join(dir, "tmux.log")
	script := filepath.Join(dir, "tmux")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"$@\" >> '"+log+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c.tmux = &tmux.Runner{Bin: script}
	return c, func() string { b, _ := os.ReadFile(log); return string(b) }
}

// ease runs one pass of easeModels to its end.
func ease(c *Core) {
	c.readMachine()
	c.easeModels()
	c.ease.running.Lock()
	c.ease.running.Unlock()
}

func TestIdleModelsGoUnderMemoryPressureButNotOneAtWork(t *testing.T) {
	f := &psOllama{loaded: []string{"qwen3.5:9b", "chat:1b"}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	cfg := config.Default()
	cfg.Local.Port = portOf(t, srv.URL)
	c, ran := easeCore(t, cfg)
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%7", Role: tmux.RoleRuntime}}}
	x := &session{id: "s1", name: "local work", local: true, model: "qwen3.5:9b", profile: "local"}
	x.track.activity = working
	c.sessions["s1"] = x

	ease(c)
	if gone := f.gone(); len(gone) != 0 {
		t.Fatalf("with room to spare, %v went; want none", gone)
	}
	// 1 GiB free of 18, under the 2 GiB reserve: what no session at work
	// runs on goes, and the server stays for the next.
	t.Setenv(FakeMemoryEnv, "18/1")
	ease(c)
	if gone := f.gone(); !slices.Equal(gone, []string{"chat:1b"}) {
		t.Errorf("under pressure, %v went; want chat:1b alone, not the model a session works on", gone)
	}
	x.track.activity = done
	ease(c)
	if gone := f.gone(); !slices.Equal(gone, []string{"chat:1b", "qwen3.5:9b"}) {
		t.Errorf("with its turn over, %v went; want its model too", gone)
	}
	if s := ran(); strings.Contains(s, "kill-pane") {
		t.Errorf("tmux ran %q; want the server kept under pressure", s)
	}
}

func TestIriesOwnServersStopWhileLocalHereIsOff(t *testing.T) {
	defer func(g time.Duration) { easeGrace = g }(easeGrace)
	easeGrace = 0
	cfg := config.Default()
	cfg.Local.Here = false
	c, ran := easeCore(t, cfg)
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%7", Role: tmux.RoleRuntime}, {ID: "%8", Role: tmux.RoleLlama, Name: "bonsai-2:27b"}}}
	small := &session{id: "s1", name: "Qwen local", local: true, model: "bonsai-2:9b", profile: "ollama:bonsai-2:9b"}
	big := &session{id: "s2", name: "big work", local: true, model: "bonsai-2:27b", profile: "big"}
	small.track.activity, big.track.activity = working, working
	c.sessions["s1"], c.sessions["s2"] = small, big

	ease(c)
	ease(c)
	if s := ran(); s != "" {
		t.Fatalf("tmux ran %q; want both servers kept while sessions work on them", s)
	}
	// Its turn over, a session keeps no server, as on 2026-11-23 an idle
	// card kept Irie's own Ollama up for a model the Mac no longer had: the
	// first look starts the grace, and the next stops the server.
	small.track.activity = done
	ease(c)
	if s := ran(); s != "" {
		t.Fatalf("tmux ran %q at the first look; want nothing yet", s)
	}
	ease(c)
	if s := ran(); strings.Count(s, "kill-pane -t %7\n") != 1 || strings.Contains(s, "%8") {
		t.Fatalf("tmux ran %q; want Irie's own Ollama stopped, and the llama-server kept for its session at work", s)
	}
	c.snap.Panes = c.snap.Panes[1:] // the core's next look at tmux
	big.track.activity = done
	ease(c)
	ease(c)
	if s := ran(); strings.Count(s, "kill-pane") != 2 || !strings.HasSuffix(s, "kill-pane -t %8\n") {
		t.Errorf("tmux ran %q; want the llama-server stopped once its session's turn was over", s)
	}
}
