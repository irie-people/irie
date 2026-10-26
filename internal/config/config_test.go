package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultsMatchDesign(t *testing.T) {
	c := Default()
	if want := (UI{SidebarWidth: 34, FocusKey: "C-]", MaxSubRows: 6, RedOnDone: false, Ghostty: "auto"}); c.UI != want {
		t.Errorf("ui = %+v, want %+v", c.UI, want)
	}
	if want := (Spawn{DefaultProfile: "claude", DefaultCwd: "", ReadyTimeout: 30 * time.Second, HideTmux: true}); c.Spawn != want {
		t.Errorf("spawn = %+v, want %+v", c.Spawn, want)
	}
	want := Profile{
		Harness:        "claude-code",
		Command:        "claude",
		Model:          "claude-opus-5-5",
		Effort:         "max",
		PermissionMode: "auto",
		Args:           []string{},
		Env:            map[string]string{},
	}
	if got := c.Profiles["claude"]; !reflect.DeepEqual(got, want) {
		t.Errorf("profiles.claude = %+v, want %+v", got, want)
	}
	if len(c.Profiles) != 1 {
		t.Errorf("default profiles = %v, want only claude", c.Profiles)
	}
	if want := (Tmux{Path: "", Socket: "irie"}); c.Tmux != want {
		t.Errorf("tmux = %+v, want %+v", c.Tmux, want)
	}
	if want := (Journal{KeepDays: 30, ToolInput: "summary"}); c.Journal != want {
		t.Errorf("journal = %+v, want %+v", c.Journal, want)
	}
	if want := (Hold{Max: 24 * time.Hour}); c.Hold != want {
		t.Errorf("hold = %+v, want %+v", c.Hold, want)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("defaults do not validate: %v", err)
	}
}

func TestMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, Default()) {
		t.Errorf("got %+v, want defaults", c)
	}
}

func TestFileOverridesEachKey(t *testing.T) {
	c, err := Load(writeFile(t, `
[ui]
sidebar_width = 40
focus_key = "C-o"
max_sub_rows = 3
red_on_done = true
ghostty = "off"

[spawn]
default_profile = "fast"
default_cwd = "/home/alice/scratch"
ready_timeout = "45s"
hide_tmux = false

[profiles.claude]
harness = "claude-code"
command = "/home/alice/bin/claude"
model = "claude-sonnet-5-5"
effort = "high"
permission_mode = "plan"
args = ["--verbose"]
env = { FOO = "bar" }

[profiles.fast]
model = "claude-haiku-5-5"

[tmux]
path = "/opt/homebrew/bin/tmux"
socket = "irie-alt"

[journal]
keep_days = 7
tool_input = "none"

[hold]
max = "2h"
`))
	if err != nil {
		t.Fatal(err)
	}
	if want := (UI{SidebarWidth: 40, FocusKey: "C-o", MaxSubRows: 3, RedOnDone: true, Ghostty: "off"}); c.UI != want {
		t.Errorf("ui = %+v, want %+v", c.UI, want)
	}
	if want := (Spawn{DefaultProfile: "fast", DefaultCwd: "/home/alice/scratch", ReadyTimeout: 45 * time.Second, HideTmux: false}); c.Spawn != want {
		t.Errorf("spawn = %+v, want %+v", c.Spawn, want)
	}
	wantClaude := Profile{
		Harness:        "claude-code",
		Command:        "/home/alice/bin/claude",
		Model:          "claude-sonnet-5-5",
		Effort:         "high",
		PermissionMode: "plan",
		Args:           []string{"--verbose"},
		Env:            map[string]string{"FOO": "bar"},
	}
	if got := c.Profiles["claude"]; !reflect.DeepEqual(got, wantClaude) {
		t.Errorf("profiles.claude = %+v, want %+v", got, wantClaude)
	}
	if got := c.Profiles["fast"]; got.Model != "claude-haiku-5-5" || got.Harness != "claude-code" {
		t.Errorf("profiles.fast = %+v", got)
	}
	if want := (Tmux{Path: "/opt/homebrew/bin/tmux", Socket: "irie-alt"}); c.Tmux != want {
		t.Errorf("tmux = %+v, want %+v", c.Tmux, want)
	}
	if want := (Journal{KeepDays: 7, ToolInput: "none"}); c.Journal != want {
		t.Errorf("journal = %+v, want %+v", c.Journal, want)
	}
	if want := (Hold{Max: 2 * time.Hour}); c.Hold != want {
		t.Errorf("hold = %+v, want %+v", c.Hold, want)
	}
	// The defaults themselves are untouched.
	if Default().Profiles["claude"].Model != "claude-opus-5-5" {
		t.Error("loading a file changed the defaults")
	}
}

func TestPartialProfileKeepsDefaults(t *testing.T) {
	c, err := Load(writeFile(t, "[profiles.claude]\nmodel = \"claude-sonnet-5-5\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	p := c.Profiles["claude"]
	if p.Model != "claude-sonnet-5-5" || p.Effort != "max" || p.PermissionMode != "auto" || p.Command != "claude" || p.Harness != "claude-code" {
		t.Errorf("profiles.claude = %+v", p)
	}
}

func TestUnknownKeyIsNamed(t *testing.T) {
	for _, c := range []struct{ body, key string }{
		{"[ui]\nsidebar_widht = 30\n", "ui.sidebar_widht"},
		{"[bogus]\nx = 1\n", "bogus"},
		{"[profiles.claude]\nmodle = \"x\"\n", "profiles.claude.modle"},
		{"[spawn]\nreadytimeout = \"5s\"\n", "spawn.readytimeout"},
		{"top_level = 1\n", "top_level"},
		{"[journal]\nkeepdays = 3\n", "journal.keepdays"},
		{"[hold]\nmaximum = \"1h\"\n", "hold.maximum"},
	} {
		_, err := Load(writeFile(t, c.body))
		if err == nil {
			t.Errorf("%q: no error", c.body)
			continue
		}
		if !strings.Contains(err.Error(), c.key) {
			t.Errorf("%q: error %q does not name %q", c.body, err, c.key)
		}
	}
}

func TestInvalidValuesAreRejected(t *testing.T) {
	for _, c := range []struct{ body, mention string }{
		{"[spawn]\ndefault_profile = \"nope\"\n", "nope"},
		{"[ui]\nghostty = \"sometimes\"\n", "ghostty"},
		{"[ui]\nsidebar_width = 2\n", "sidebar_width"},
		{"[ui]\nfocus_key = \"C-] x\"\n", "focus_key"},
		{"[ui]\nmax_sub_rows = -1\n", "max_sub_rows"},
		{"[spawn]\nready_timeout = \"-1s\"\n", "ready_timeout"},
		{"[profiles.claude]\nharness = \"bogus\"\n", "harness"},
		{"[tmux]\nsocket = \"a/b\"\n", "socket"},
		{"[tmux]\npath = \"tmux\"\n", "path"},
		{"[ui]\nsidebar_width = \"wide\"\n", "sidebar_width"},
		{"[journal]\nkeep_days = 0\n", "keep_days"},
		{"[journal]\ntool_input = \"everything\"\n", "tool_input"},
		{"[hold]\nmax = \"0s\"\n", "hold.max"},
	} {
		_, err := Load(writeFile(t, c.body))
		if err == nil {
			t.Errorf("%q: no error", c.body)
			continue
		}
		if !strings.Contains(err.Error(), c.mention) {
			t.Errorf("%q: error %q does not mention %q", c.body, err, c.mention)
		}
	}
}

func TestPrintRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := Default().Write(&buf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`model = "claude-opus-5-5"`, `ready_timeout = "30s"`, `socket = "irie"`, `keep_days = 30`, `tool_input = "summary"`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("printed config lacks %s:\n%s", want, buf.String())
		}
	}
	c, err := Load(writeFile(t, buf.String()))
	if err != nil {
		t.Fatalf("printed config does not load: %v\n%s", err, buf.String())
	}
	if !reflect.DeepEqual(c, Default()) {
		t.Errorf("round trip = %+v, want defaults", c)
	}
}
