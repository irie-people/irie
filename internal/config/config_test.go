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
	if want := (UI{SidebarWidth: 34, FocusKey: "C-]", FlipKey: `C-\`, View: "conversation", MaxSubRows: 6, RedOnDone: false, Ghostty: "auto", HelperStyle: "browser", Theme: "rosewater"}); c.UI != want {
		t.Errorf("ui = %+v, want %+v", c.UI, want)
	}
	if want := (Spawn{DefaultProfile: "auto", DefaultCwd: "", ReadyTimeout: 30 * time.Second, HideTmux: true, CloseWhenDone: false, CloseDelay: 30 * time.Second, Teammates: "off"}); c.Spawn != want {
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
	wantSonnet := Profile{
		Harness:        "claude-code",
		Command:        "claude",
		Model:          "claude-sonnet-5-5",
		Effort:         "medium",
		PermissionMode: "auto",
		Args:           []string{},
		Env:            map[string]string{},
	}
	if got := c.Profiles["sonnet"]; !reflect.DeepEqual(got, wantSonnet) {
		t.Errorf("profiles.sonnet = %+v, want %+v", got, wantSonnet)
	}
	wantLocal := Profile{
		Harness: "claude-code",
		Command: "claude",
		Args:    []string{},
		Env:     map[string]string{},
		Local:   &LocalModel{Runtime: "ollama", MinContext: 65536, Enforce: true},
	}
	if got := c.Profiles["local"]; !reflect.DeepEqual(got, wantLocal) {
		t.Errorf("profiles.local = %+v, want %+v", got, wantLocal)
	}
	wantHaiku := Profile{Harness: "claude-code", Command: "claude", Model: "claude-haiku-5-5", PermissionMode: "auto", Args: []string{}, Env: map[string]string{}}
	if got := c.Profiles["haiku"]; !reflect.DeepEqual(got, wantHaiku) {
		t.Errorf("profiles.haiku = %+v, want %+v", got, wantHaiku)
	}
	if len(c.Profiles) != 4 {
		t.Errorf("default profiles = %v, want claude, sonnet, haiku and local", c.Profiles)
	}
	r := c.Route
	if !reflect.DeepEqual(r.Profiles, []string{"local", "haiku", "sonnet", "claude"}) || r.MinAccuracy != 0.85 || r.PriorWeight != 10 ||
		r.Pressure != 80 || !r.Failover || !r.Delegate || r.DelegateMode != "acceptEdits" {
		t.Errorf("route = %+v", r)
	}
	if r.Accuracy["claude"]["hard"] != 0.9 || r.Accuracy["local"]["small"] != 0.85 || r.Cost["sonnet"] != 0.25 || r.Cost["local"] != 0 {
		t.Errorf("route priors = %v, %v", r.Accuracy, r.Cost)
	}
	if len(r.Words.Small) == 0 || len(r.Words.Hard) == 0 {
		t.Errorf("route words = %+v", r.Words)
	}
	if want := (Local{Port: 11435, Context: 65536, KVCache: "q8_0", Env: map[string]string{}}); !reflect.DeepEqual(c.Local, want) {
		t.Errorf("local = %+v, want %+v", c.Local, want)
	}
	if want := (Usage{StatusLine: true}); c.Usage != want {
		t.Errorf("usage = %+v, want %+v", c.Usage, want)
	}
	if want := (Repos{Roots: []string{}, Aliases: map[string]string{}}); !reflect.DeepEqual(c.Repos, want) {
		t.Errorf("repos = %+v, want %+v", c.Repos, want)
	}
	wantTree := Worktree{
		Root:         "",
		Base:         "HEAD",
		BranchPrefix: "irie/",
		Copy:         []string{".claude/settings.local.json", "CLAUDE.local.md"},
		Submodules:   true,
	}
	if !reflect.DeepEqual(c.Worktree, wantTree) {
		t.Errorf("worktree = %+v, want %+v", c.Worktree, wantTree)
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
	if want := (Dog{Watch: true, Nudges: 2, Land: map[string]string{}}); !reflect.DeepEqual(c.Dog, want) {
		t.Errorf("dog = %+v, want %+v", c.Dog, want)
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
flip_key = "M-t"
view = "terminal"
max_sub_rows = 3
red_on_done = true
ghostty = "off"
helper_style = "terminal"
theme = "lilac"

[spawn]
default_profile = "fast"
default_cwd = "/home/alice/scratch"
ready_timeout = "45s"
close_when_done = true
close_delay = "5s"
hide_tmux = false
teammates = "panes"

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

[profiles.aider]
harness = "generic"
command = "aider"
args = ["--model", "ollama_chat/gpt-oss:20b"]
task = "paste"
cost = "free, 50 requests a day"

[profiles.small]
model = "qwen3.5:4b"
local = { runtime = "ollama", min_context = 32768, enforce = false }

[local]
ollama = "/home/alice/bin/ollama"
port = 11500
context = 131072
kv_cache = "q4_0"
memory_gib = 24
env = { OLLAMA_MODELS = "/home/alice/models" }

[repos]
roots = ["~/code", "/home/alice/src"]
aliases = { web = "~/code/website" }

[worktree]
root = "/home/alice/trees"
base = "origin/main"
branch_prefix = "agent/"
copy = [".env.local"]
submodules = false

[tmux]
path = "/opt/homebrew/bin/tmux"
socket = "irie-alt"

[journal]
keep_days = 7
tool_input = "none"

[hold]
max = "2h"

[usage]
status_line = false

[dog]
watch = false
nudges = 5
land = { web = "live", "~/code/api" = "main" }
`))
	if err != nil {
		t.Fatal(err)
	}
	if want := (UI{SidebarWidth: 40, FocusKey: "C-o", FlipKey: "M-t", View: "terminal", MaxSubRows: 3, RedOnDone: true, Ghostty: "off", HelperStyle: "terminal", Theme: "lilac"}); c.UI != want {
		t.Errorf("ui = %+v, want %+v", c.UI, want)
	}
	if want := (Spawn{DefaultProfile: "fast", DefaultCwd: "/home/alice/scratch", ReadyTimeout: 45 * time.Second, HideTmux: false, CloseWhenDone: true, CloseDelay: 5 * time.Second, Teammates: "panes"}); c.Spawn != want {
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
	wantAider := Profile{
		Harness: "generic",
		Command: "aider",
		Args:    []string{"--model", "ollama_chat/gpt-oss:20b"},
		Env:     map[string]string{},
		Task:    "paste",
		Cost:    "free, 50 requests a day",
	}
	if got := c.Profiles["aider"]; !reflect.DeepEqual(got, wantAider) {
		t.Errorf("profiles.aider = %+v, want %+v", got, wantAider)
	}
	wantSmall := Profile{
		Harness: "claude-code",
		Model:   "qwen3.5:4b",
		Args:    []string{},
		Env:     map[string]string{},
		Local:   &LocalModel{Runtime: "ollama", MinContext: 32768, Enforce: false},
	}
	if got := c.Profiles["small"]; !reflect.DeepEqual(got, wantSmall) {
		t.Errorf("profiles.small = %+v, want %+v", got, wantSmall)
	}
	wantLocal := Local{Ollama: "/home/alice/bin/ollama", Port: 11500, Context: 131072, KVCache: "q4_0", MemoryGiB: 24, Env: map[string]string{"OLLAMA_MODELS": "/home/alice/models"}}
	if !reflect.DeepEqual(c.Local, wantLocal) {
		t.Errorf("local = %+v, want %+v", c.Local, wantLocal)
	}
	if c.Usage.StatusLine {
		t.Error("usage.status_line = true, want false")
	}
	if want := (Repos{Roots: []string{"~/code", "/home/alice/src"}, Aliases: map[string]string{"web": "~/code/website"}}); !reflect.DeepEqual(c.Repos, want) {
		t.Errorf("repos = %+v, want %+v", c.Repos, want)
	}
	if want := (Worktree{Root: "/home/alice/trees", Base: "origin/main", BranchPrefix: "agent/", Copy: []string{".env.local"}, Submodules: false}); !reflect.DeepEqual(c.Worktree, want) {
		t.Errorf("worktree = %+v, want %+v", c.Worktree, want)
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
	if want := (Dog{Watch: false, Nudges: 5, Land: map[string]string{"web": "live", "~/code/api": "main"}}); !reflect.DeepEqual(c.Dog, want) {
		t.Errorf("dog = %+v, want %+v", c.Dog, want)
	}
	// The defaults themselves are untouched.
	if d := Default(); d.Profiles["claude"].Model != "claude-opus-5-5" || len(d.Worktree.Copy) != 2 || len(d.Repos.Aliases) != 0 {
		t.Error("loading a file changed the defaults")
	}
}

func TestAnEmptyCopyListCopiesNothing(t *testing.T) {
	c, err := Load(writeFile(t, "[worktree]\ncopy = []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Worktree.Copy == nil || len(c.Worktree.Copy) != 0 {
		t.Errorf("worktree.copy = %#v, want an empty list", c.Worktree.Copy)
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

func TestPartialLocalTableKeepsDefaults(t *testing.T) {
	c, err := Load(writeFile(t, `
[profiles.mine]
model = "qwen3.5:9b"
local = { runtime = "ollama" }

[profiles.local]
model = "qwen3.5:4b"
`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Profiles["mine"].Local, (&LocalModel{Runtime: "ollama", MinContext: 65536, Enforce: true}); !reflect.DeepEqual(got, want) {
		t.Errorf("profiles.mine.local = %+v, want %+v", got, want)
	}
	// Setting the built-in local profile's model keeps it local.
	if p := c.Profiles["local"]; p.Model != "qwen3.5:4b" || p.Local == nil || p.Local.Runtime != "ollama" {
		t.Errorf("profiles.local = %+v", p)
	}
	if c.Profiles["claude"].Local != nil || c.Profiles["sonnet"].Local != nil {
		t.Error("a cloud profile became local")
	}
	// The defaults' local table is not shared with a loaded config.
	if Default().Profiles["local"].Local == c.Profiles["local"].Local {
		t.Error("loading shares the default local table")
	}
}

func TestPartialRouteTablesKeepTheirDefaults(t *testing.T) {
	c, err := Load(writeFile(t, `
[spawn]
default_profile = "claude"

[route]
min_accuracy = 0.9
profiles = ["haiku", "claude"]

[route.accuracy]
local = { small = 0.95 }

[route.cost]
sonnet = 0.3

[route.words]
small = ["chore"]
`))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Route
	if c.Spawn.DefaultProfile != "claude" || r.MinAccuracy != 0.9 || !reflect.DeepEqual(r.Profiles, []string{"haiku", "claude"}) {
		t.Errorf("route = %+v, spawn = %+v", r, c.Spawn)
	}
	// One class of one profile changes; the rest keep their starting guesses.
	if r.Accuracy["local"]["small"] != 0.95 || r.Accuracy["local"]["medium"] != 0.40 || r.Accuracy["claude"]["hard"] != 0.9 {
		t.Errorf("accuracy = %v", r.Accuracy)
	}
	if r.Cost["sonnet"] != 0.3 || r.Cost["claude"] != 1 {
		t.Errorf("cost = %v", r.Cost)
	}
	if !reflect.DeepEqual(r.Words.Small, []string{"chore"}) || len(r.Words.Hard) == 0 {
		t.Errorf("words = %+v", r.Words)
	}
	if p, ok := r.Prior("haiku"); !ok || p.Cost != 0.02 || p.Accuracy["small"] != 0.92 {
		t.Errorf("prior = %+v, %v", p, ok)
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
		{"[repos]\nroot = []\n", "repos.root"},
		{"[worktree]\nbranchprefix = \"x/\"\n", "worktree.branchprefix"},
		{"[local]\ncontext_length = 1\n", "local.context_length"},
		{"[usage]\nstatusline = true\n", "usage.statusline"},
		{"[profiles.local]\nlocal = { runtime = \"ollama\", min_ctx = 1 }\n", "profiles.local.local.min_ctx"},
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
		{"[spawn]\nteammates = \"tabs\"\n", "spawn.teammates"},
		{"[ui]\nhelper_style = \"tabs\"\n", "ui.helper_style"},
		{"[ui]\ntheme = \"\"\n", "ui.theme"},
		{"[ui]\nsidebar_width = 2\n", "sidebar_width"},
		{"[ui]\nfocus_key = \"C-] x\"\n", "focus_key"},
		{"[ui]\nmax_sub_rows = -1\n", "max_sub_rows"},
		{"[spawn]\nready_timeout = \"-1s\"\n", "ready_timeout"},
		{"[spawn]\nclose_delay = \"-1s\"\n", "close_delay"},
		{"[profiles.claude]\nharness = \"bogus\"\n", "harness"},
		{"[tmux]\nsocket = \"a/b\"\n", "socket"},
		{"[tmux]\npath = \"tmux\"\n", "path"},
		{"[ui]\nsidebar_width = \"wide\"\n", "sidebar_width"},
		{"[journal]\nkeep_days = 0\n", "keep_days"},
		{"[journal]\ntool_input = \"everything\"\n", "tool_input"},
		{"[hold]\nmax = \"0s\"\n", "hold.max"},
		{"[repos]\nroots = [\"code\"]\n", "repos.roots"},
		{"[repos.aliases]\nweb = \"code/website\"\n", "repos.aliases.web"},
		{"[repos.aliases]\n\"we b\" = \"/home/alice/code/website\"\n", "repos.aliases"},
		{"[repos.aliases]\n\"web!\" = \"/home/alice/code/website\"\n", "repos.aliases"},
		{"[worktree]\nroot = \"trees\"\n", "worktree.root"},
		{"[worktree]\nbase = \"\"\n", "worktree.base"},
		{"[worktree]\nbase = \"-x\"\n", "worktree.base"},
		{"[worktree]\nbranch_prefix = \"a b/\"\n", "worktree.branch_prefix"},
		{"[worktree]\nbranch_prefix = \"a..b/\"\n", "worktree.branch_prefix"},
		{"[worktree]\ncopy = [\"../outside\"]\n", "worktree.copy"},
		{"[worktree]\ncopy = [\"/etc/hosts\"]\n", "worktree.copy"},
		{"[worktree]\ncopy = [\"\"]\n", "worktree.copy"},
		{"[profiles.claude]\ntask = \"type\"\n", "task"},
		{"[profiles.claude]\ntask = \"paste\"\n", "task"},
		{"[profiles.g]\nharness = \"generic\"\n", "profiles.g.command"},
		{"[profiles.g]\nharness = \"generic\"\ncommand = \"aider\"\nmodel = \"claude-opus-5-5\"\n", "profiles.g.model"},
		{"[profiles.local]\nlocal = { runtime = \"lmstudio\" }\n", "profiles.local.local.runtime"},
		{"[profiles.g]\nharness = \"generic\"\ncommand = \"aider\"\nlocal = { runtime = \"ollama\" }\n", "only the claude-code harness"},
		{"[profiles.local]\nlocal = { runtime = \"ollama\", min_context = 0 }\n", "min_context"},
		{"[profiles.local]\nlocal = { runtime = \"ollama\", min_context = 200000 }\n", "above local.context"},
		{"[profiles.claude]\ncost = \"a\\nb\"\n", "profiles.claude.cost: must be one line"},
		{"[local]\nollama = \"ollama\"\n", "local.ollama"},
		{"[local]\nurl = \"127.0.0.1:11434\"\n", "local.url"},
		{"[local]\nurl = \"ftp://127.0.0.1\"\n", "local.url"},
		{"[local]\nport = 0\n", "local.port"},
		{"[local]\nport = 70000\n", "local.port"},
		{"[local]\ncontext = 100\n", "local.context"},
		{"[local]\nkv_cache = \"q2\"\n", "local.kv_cache"},
		{"[local]\nmemory_gib = -1\n", "local.memory_gib"},
		{"[local.env]\nOLLAMA_HOST = \"0.0.0.0:1\"\n", "OLLAMA_HOST"},
		{"[profiles.auto]\nmodel = \"x\"\n", "auto is Irie's router"},
		{"[route]\nprofiles = [\"nope\"]\n", "route.profiles: \"nope\" is not a profile"},
		{"[profiles.mine]\nmodel = \"x\"\n\n[route]\nprofiles = [\"mine\"]\n", "needs route.accuracy.mine"},
		{"[route]\nmin_accuracy = 0\n", "route.min_accuracy"},
		{"[route]\nmin_accuracy = 1.5\n", "route.min_accuracy"},
		{"[route]\nprior_weight = -1\n", "route.prior_weight"},
		{"[route]\npressure = 120\n", "route.pressure"},
		{"[route]\ndelegate_mode = \"yolo\"\n", "route.delegate_mode"},
		{"[route.accuracy]\nlocal = { tiny = 0.5 }\n", "route.accuracy.local.tiny"},
		{"[route.accuracy]\nlocal = { small = 2 }\n", "route.accuracy.local.small"},
		{"[route.cost]\nlocal = -1\n", "route.cost.local"},
		{"[dog]\nnudges = -1\n", "dog.nudges"},
		{"[dog]\nland = { irie = \"\" }\n", "dog.land.irie"},
		{"[dog]\nland = { irie = \"-x\" }\n", "dog.land.irie"},
		{"[dog]\nland = { irie = \"a..b\" }\n", "dog.land.irie"},
		{"[dog]\nland = { irie = \"live now\" }\n", "dog.land.irie"},
		{"[dog]\nland = { \" \" = \"live\" }\n", "dog.land"},
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

// TestThemesAreNamed: a theme the sidebar does not have is refused with the
// names of those it has, and each of those loads.
func TestThemesAreNamed(t *testing.T) {
	_, err := Load(writeFile(t, "[ui]\ntheme = \"solarized\"\n"))
	if err == nil || !strings.Contains(err.Error(), `ui.theme = "solarized": must be one of rosewater, terracotta, lilac, evergreen`) {
		t.Errorf("error = %v", err)
	}
	for _, name := range Themes {
		c, err := Load(writeFile(t, "[ui]\ntheme = \""+name+"\"\n"))
		if err != nil || c.UI.Theme != name {
			t.Errorf("theme %s: %v, got %q", name, err, c.UI.Theme)
		}
	}
}

func TestPrintRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := Default().Write(&buf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`model = "claude-opus-5-5"`, `ready_timeout = "30s"`, `socket = "irie"`, `keep_days = 30`, `tool_input = "summary"`, `branch_prefix = "irie/"`, `submodules = true`, `runtime = "ollama"`, `kv_cache = "q8_0"`, `status_line = true`, `theme = "rosewater"`} {
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
