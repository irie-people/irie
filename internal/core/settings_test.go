package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/tmux"
)

// settingsCore is a core with no tmux behind it whose config file is
// file, in a folder of its own.
func settingsCore(t *testing.T, file string) *Core {
	t.Helper()
	dir := t.TempDir()
	p, err := paths.New(func(k string) string {
		return map[string]string{"HOME": "/home/alice", "XDG_CONFIG_HOME": dir}[k]
	}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if file != "" {
		if err := os.MkdirAll(p.Config, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.ConfigFile(), []byte(file), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(p.ConfigFile())
	if err != nil {
		cfg = config.Default()
	}
	return newCore(Options{Config: cfg, Paths: p, Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{})
}

// configure calls the config op as a client does, with values in JSON.
func configure(c *Core, kv ...string) (ipc.ConfigResult, error) {
	var a ipc.ConfigArgs
	for i := 0; i+1 < len(kv); i += 2 {
		a.Set = append(a.Set, ipc.ConfigSet{Key: kv[i], Value: json.RawMessage(kv[i+1])})
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return ipc.ConfigResult{}, err
	}
	r, err := c.Call(ipc.OpConfig, raw)
	if err != nil {
		return ipc.ConfigResult{}, err
	}
	return r.(ipc.ConfigResult), nil
}

const mine = `# Every task runs on +claude.
[spawn]
default_profile = "claude" # mine
close_when_done = false
`

func TestSettingsEditTheFileAndApplyAtOnce(t *testing.T) {
	c := settingsCore(t, mine)
	next := frames(t, c)
	if f, ok := next(time.Second); !ok || f.T != ipc.FrameSnapshot {
		t.Fatalf("first frame %+v", f)
	}
	r, err := configure(c, "spawn.default_profile", `"sonnet"`, "spawn.close_when_done", `true`, "ui.red_on_done", `true`, "spawn.close_delay", `"1m"`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(c.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	want := "# Every task runs on +claude.\n[spawn]\ndefault_profile = \"sonnet\" # mine\nclose_when_done = true\nclose_delay = \"1m\"\n\n[ui]\nred_on_done = true\n"
	if string(b) != want {
		t.Errorf("the file is\n%s\nwant\n%s", b, want)
	}
	if fi, err := os.Stat(c.paths.ConfigFile()); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("the file's mode: %v %v", fi.Mode(), err)
	}
	if s := c.cfg().Spawn; s.DefaultProfile != "sonnet" || !s.CloseWhenDone || s.CloseDelay != time.Minute || !c.cfg().UI.RedOnDone {
		t.Errorf("the core runs on %+v, ui %+v", s, c.cfg().UI)
	}
	if spawn := r.Config["spawn"].(map[string]any); spawn["default_profile"] != "sonnet" || spawn["close_delay"] != "1m" {
		t.Errorf("the result says spawn = %v", spawn)
	}
	if r.Path != c.paths.ConfigFile() || r.Problem != "" {
		t.Errorf("result path %q, problem %q", r.Path, r.Problem)
	}
	// Every sidebar hears the change at once.
	for {
		f, ok := next(time.Second)
		if !ok {
			t.Fatal("no header frame came")
		}
		if f.T == ipc.FrameHeader {
			if f.Header == nil || !f.Header.RedOnDone || !f.Header.CloseWhenDone {
				t.Errorf("the header frame lacks the change: %+v", f.Header)
			}
			break
		}
	}
	// Null puts a key back to its default.
	if _, err := configure(c, "spawn.close_delay", `null`); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(c.paths.ConfigFile()); strings.Contains(string(b), "close_delay") || c.cfg().Spawn.CloseDelay != 30*time.Second {
		t.Errorf("close_delay is still set: %s", b)
	}
}

func TestSettingsRefuseWhatWouldNotLoad(t *testing.T) {
	c := settingsCore(t, mine)
	for _, kv := range [][2]string{
		{"spawn.default_profile", `"nobody"`},
		{"spawn.close_when_done", `"yes"`},
		{"spawn.surprise", `1`},
		{"route.min_accuracy", `2`},
	} {
		_, err := configure(c, kv[0], kv[1])
		var e *ipc.Error
		if !errors.As(err, &e) || e.Code != ipc.CodeBadArgs {
			t.Errorf("%s = %s: %v, want bad_args", kv[0], kv[1], err)
		}
	}
	if b, _ := os.ReadFile(c.paths.ConfigFile()); string(b) != mine {
		t.Errorf("a refused change touched the file:\n%s", b)
	}
	if c.cfg().Spawn.DefaultProfile != "claude" {
		t.Errorf("a refused change touched the config: %+v", c.cfg().Spawn)
	}
}

func TestSettingsWriteThroughALink(t *testing.T) {
	c := settingsCore(t, "")
	real := filepath.Join(t.TempDir(), "dotfiles-irie.toml")
	if err := os.WriteFile(real, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.paths.Config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, c.paths.ConfigFile()); err != nil {
		t.Fatal(err)
	}
	if _, err := configure(c, "spawn.close_when_done", `true`); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(c.paths.ConfigFile()); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link is gone: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(real); !strings.Contains(string(b), "close_when_done = true") {
		t.Errorf("the linked file is\n%s", b)
	}
}

// Two cores on one config file share no lock in memory. Each edits a key
// of its own over and over, both at once, and the file's write lock keeps
// either from writing back a file it read before the other's last edit.
func TestSettingsOfTwoCoresBothLand(t *testing.T) {
	a := settingsCore(t, mine)
	cfg, err := config.Load(a.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	b := newCore(Options{Config: cfg, Paths: a.paths, Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{})
	const times = 25
	var wg sync.WaitGroup
	for _, w := range []struct {
		c     *Core
		key   string
		value func(int) string
	}{
		{a, "spawn.close_delay", func(i int) string { return fmt.Sprintf(`"%ds"`, i+1) }},
		{b, "journal.keep_days", func(i int) string { return strconv.Itoa(i + 1) }},
	} {
		wg.Go(func() {
			for i := range times {
				if _, err := configure(w.c, w.key, w.value(i)); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	got, err := config.Load(a.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if got.Spawn.CloseDelay != times*time.Second || got.Journal.KeepDays != times {
		t.Errorf("close_delay %v and keep_days %d, want %v and %d: an edit was lost", got.Spawn.CloseDelay, got.Journal.KeepDays, times*time.Second, times)
	}
	if file, _ := os.ReadFile(a.paths.ConfigFile()); !strings.Contains(string(file), `default_profile = "claude" # mine`) {
		t.Errorf("the file lost its own lines:\n%s", file)
	}
}

func TestSettingsMakeTheFileWhenThereIsNone(t *testing.T) {
	c := settingsCore(t, "")
	if _, err := configure(c, "ui.red_on_done", `true`); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(c.paths.ConfigFile())
	if err != nil || string(b) != "[ui]\nred_on_done = true\n" {
		t.Errorf("the new file is %q, %v", b, err)
	}
	if fi, err := os.Stat(c.paths.ConfigFile()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the new file's mode: %v %v", fi.Mode(), err)
	}
}

func TestSettingsSayWhenTheFileDoesNotLoad(t *testing.T) {
	c := settingsCore(t, "[spawn\n")
	r, err := configure(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Problem, c.paths.ConfigFile()) || r.Config["spawn"].(map[string]any)["default_profile"] != config.Auto {
		t.Errorf("problem %q, config %v", r.Problem, r.Config["spawn"])
	}
	if _, err := configure(c, "spawn.close_when_done", `true`); err == nil {
		t.Error("a file that does not parse was edited")
	}
}

// TestAnAgentCannotLowerTheArchAgentsGuards: from inside an agent's pane,
// the config op raises Mikael's and Gabriel's guards but never lowers them,
// nor rewords the good Mikael hears, and refuses a change that would, whole;
// every other setting stays an agent's to change, and the person may lower
// a guard.
func TestAnAgentCannotLowerTheArchAgentsGuards(t *testing.T) {
	c := settingsCore(t, mine)
	// This test's own parent stands for an agent's host.
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	agent, person := os.Getpid(), 0
	from := func(peer int, kv ...string) error {
		var a ipc.ConfigArgs
		for i := 0; i+1 < len(kv); i += 2 {
			a.Set = append(a.Set, ipc.ConfigSet{Key: kv[i], Value: json.RawMessage(kv[i+1])})
		}
		raw, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.CallFrom(peer, ipc.OpConfig, raw)
		return err
	}
	before, err := os.ReadFile(c.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][]string{
		{"arch.mikael.on", "false"},
		{"arch.mikael.door", "false"},
		{"arch.mikael.good", `["anything goes"]`},
		{"arch.gabriel.guard", "false"},
		{"arch.gabriel.on", "false"},
		// One lowered guard refuses the whole change.
		{"spawn.close_when_done", "true", "arch.mikael.door", "false"},
	} {
		if err := from(agent, kv...); err == nil || !strings.Contains(err.Error(), "An agent can't turn off or reword an arch agent's guard") {
			t.Errorf("%v from an agent: err = %v", kv, err)
		}
	}
	if after, _ := os.ReadFile(c.paths.ConfigFile()); string(after) != string(before) {
		t.Errorf("a refused change was written:\n%s", after)
	}
	if a := c.cfg().Arch; !a.Mikael.On || !a.Mikael.Door || len(a.Mikael.Good) != 0 || !a.Gabriel.On || !a.Gabriel.Guard || c.cfg().Spawn.CloseWhenDone {
		t.Errorf("a refused change took: %+v", a)
	}

	// Any other setting is an agent's to change.
	if err := from(agent, "spawn.close_when_done", "true"); err != nil || !c.cfg().Spawn.CloseWhenDone {
		t.Errorf("close_when_done from an agent: %v", err)
	}
	// The person lowers a guard, and an agent may raise it again.
	if err := from(person, "arch.mikael.door", "false"); err != nil || c.cfg().Arch.Mikael.Door {
		t.Fatalf("the door from the person: %v", err)
	}
	if err := from(agent, "arch.mikael.door", "true"); err != nil || !c.cfg().Arch.Mikael.Door {
		t.Errorf("raising the door from an agent: %v", err)
	}
	// With the window guard down, Gabriel himself guards nothing an agent
	// could lower.
	if err := from(person, "arch.gabriel.guard", "false"); err != nil {
		t.Fatal(err)
	}
	if err := from(agent, "arch.gabriel.on", "false"); err != nil || c.cfg().Arch.Gabriel.On {
		t.Errorf("Gabriel off from an agent, with his guard down: %v", err)
	}
	// The core's own calls, without a caller, are the person's.
	if _, err := configure(c, "arch.mikael.on", "false"); err != nil || c.cfg().Arch.Mikael.On {
		t.Errorf("Mikael off through Call: %v", err)
	}
}
