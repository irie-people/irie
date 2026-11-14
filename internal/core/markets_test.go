//go:build !nomarkets

package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/testutil"
)

// TestTheMarketsOpSaysHowTheMapStands: the op answers the Markets window and
// :markets with how the map stands, what it lacks in words rather than an
// error, so the window can offer what fixes it; a task waits for the plugin
// and for the folder (DESIGN §4.17).
func TestTheMarketsOpSaysHowTheMapStands(t *testing.T) {
	c := bareCore(t, config.Default())
	r, err := c.marketsOp(ipc.MarketsArgs{})
	if err != nil || r.State != ipc.PartPluginOff || !strings.Contains(r.Text, "irie plugins on markets") || r.Page != "" {
		t.Fatalf("plugin off: %+v, %v", r, err)
	}
	if _, err := c.marketsOp(ipc.MarketsArgs{Task: &ipc.SpawnArgs{Task: "add the cloud market"}}); err == nil || !strings.Contains(err.Error(), "irie plugins on markets") {
		t.Errorf("a task with the plugin off: %v", err)
	}
	in, err := c.input(ipc.InputArgs{Text: ":markets"}, 0)
	if err != nil || in.Did != ipc.InputAnswered || in.Op != ipc.OpMarkets || !strings.Contains(in.Text, "plugins on markets") {
		t.Errorf(":markets with the plugin off = %+v, %v", in, err)
	}

	on := true
	if _, err := c.pluginsOp(ipc.PluginsArgs{Plugin: "markets", On: &on}); err != nil || !c.cfg().MarketsOn() {
		t.Fatalf("markets on: %v", err)
	}
	if r, err = c.marketsOp(ipc.MarketsArgs{}); err != nil || r.State != ipc.PartNeeds || !strings.Contains(r.Text, "markets.folder") {
		t.Errorf("no folder: %+v, %v", r, err)
	}
	if _, err := c.marketsOp(ipc.MarketsArgs{Task: &ipc.SpawnArgs{Task: "make the map"}}); err == nil || !strings.Contains(err.Error(), "markets.folder") {
		t.Errorf("a task with no folder: %v", err)
	}

	dir := t.TempDir()
	cfg := *c.cfg()
	cfg.Markets.Folder = dir
	c.useConfig(cfg)
	if r, err = c.marketsOp(ipc.MarketsArgs{}); err != nil || r.State != ipc.PartNeeds || r.Folder != dir || !strings.Contains(r.Text, "No map in") {
		t.Errorf("no page: %+v, %v", r, err)
	}
	page := filepath.Join(dir, plugins.MapPage)
	if err := os.WriteFile(page, []byte("<title>Markets</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, err = c.marketsOp(ipc.MarketsArgs{}); err != nil || r.State != ipc.PartReady || r.Page != page || r.Changed.IsZero() || !strings.Contains(r.Text, "Window › Markets") {
		t.Errorf("ready: %+v, %v", r, err)
	}
}

// TestAMarketsTaskRunsInTheMapsFolder: :markets TASK starts the task in the
// map's folder itself, never a worktree of it, so the window shows each
// rebuild of the page; it is a step of the history like any typed task.
func TestAMarketsTaskRunsInTheMapsFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "maps")
	c := prepare(t, agentConfig{extra: fmt.Sprintf("[plugins]\nmarkets = true\n\n[markets]\nfolder = %q\n", dir)})
	testutil.NewRepo(t, dir, map[string]string{"index.html": "<title>Markets</title>\n"})
	c.up()

	var r ipc.InputResult
	if err := c.call(ipc.OpInput, ipc.InputArgs{Text: ":markets add the cloud market"}, &r); err != nil {
		t.Fatal(err)
	}
	if r.Did != ipc.InputSpawned || r.Op != ipc.OpMarkets || r.Session == nil {
		t.Fatalf("input = %+v", r)
	}
	s := r.Session
	if s.Worktree != "" || s.Branch != "" || s.Repo != dir {
		t.Errorf("session = %+v: want it in %s, in place", s, dir)
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Cwd != dir {
		t.Errorf("cwd = %q, want %q", spec.Cwd, dir)
	}
	c.waitState(s.ID, ipc.StateWorking)
	var u ipc.UndoResult
	if err := c.call(ipc.OpUndo, ipc.UndoArgs{}, &u); err != nil || !strings.Contains(u.Label, "start") {
		t.Errorf("undo = %+v, %v: the task is a step of the history", u, err)
	}
}
