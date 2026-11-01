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
	if want := (UI{SidebarWidth: 34, FocusKey: "C-]", FlipKey: `C-\`, View: "conversation", MaxSubRows: 6, RedOnDone: false, Ghostty: "auto", HelperStyle: "browser", Theme: "rosewater", FreezeDog: 10 * time.Second}); c.UI != want {
		t.Errorf("ui = %+v, want %+v", c.UI, want)
	}
	if want := (Spawn{DefaultProfile: "auto", DefaultCwd: "", ReadyTimeout: 30 * time.Second, HideTmux: true, CloseWhenDone: false, CloseDelay: 30 * time.Second, Teammates: "off", FollowUpWindow: 2 * time.Minute, FollowUpDelay: 8 * time.Second, Preferences: Preferences()}); !reflect.DeepEqual(c.Spawn, want) {
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
	wantBig := Profile{
		Harness: "claude-code",
		Command: "claude",
		Args:    []string{},
		Env:     map[string]string{},
		Local:   &LocalModel{Runtime: "llama-server", MinContext: 65536, Enforce: true},
	}
	if got := c.Profiles["big"]; !reflect.DeepEqual(got, wantBig) {
		t.Errorf("profiles.big = %+v, want %+v", got, wantBig)
	}
	wantMikael := Profile{Harness: "claude-code", Command: "claude", Model: "claude-opus-5-5", Effort: "high", PermissionMode: "auto", Args: []string{}, Env: map[string]string{}, Arch: "mikael"}
	if got := c.Profiles["mikael"]; !reflect.DeepEqual(got, wantMikael) {
		t.Errorf("profiles.mikael = %+v, want %+v", got, wantMikael)
	}
	if len(c.Profiles) != 6 {
		t.Errorf("default profiles = %v, want claude, sonnet, haiku, local, big and mikael", c.Profiles)
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
	if want := (Local{Port: 11435, Context: 65536, KVCache: "q8_0", Env: map[string]string{}, LlamaPort: 11436, LlamaArgs: []string{}}); !reflect.DeepEqual(c.Local, want) {
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
	if want := (Trod{
		Start: "23:00", End: "07:00", Away: 15 * time.Minute, Questions: true, Nudges: 8, Retry: 15 * time.Minute,
		Within: 24 * time.Hour, MaxWorking: 4, MaxPlanUse: 90, KeepAwake: true, Rules: TrodRules(), Guidelines: TrodGuidelines(),
	}); !reflect.DeepEqual(c.Trod, want) {
		t.Errorf("night = %+v, want %+v", c.Trod, want)
	}
	if want := (Arch{Mikael: Mikael{On: true, Door: true, Good: []string{}}}); !reflect.DeepEqual(c.Arch, want) {
		t.Errorf("arch = %+v, want %+v", c.Arch, want)
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
freeze_dog = "20s"

[spawn]
default_profile = "fast"
default_cwd = "/home/alice/scratch"
ready_timeout = "45s"
close_when_done = true
close_delay = "5s"
hide_tmux = false
teammates = "panes"
follow_up_window = "0s"
follow_up_delay = "1s"
preferences = ["Text me in one line."]

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
llama_server = "/home/alice/llama/llama-server"
llama_port = 11600
llama_args = ["--cpu-moe"]
models = "~/gguf"

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

[trod]
on = true
start = "22:30"
end = "6:45"
away = "30m"
questions = false
nudges = 3
retry = "5m"
within = "0s"
max_working = 0
max_plan_use = 75.5
keep_awake = false
rules = ["Never touch production."]
guidelines = []
`))
	if err != nil {
		t.Fatal(err)
	}
	if want := (UI{SidebarWidth: 40, FocusKey: "C-o", FlipKey: "M-t", View: "terminal", MaxSubRows: 3, RedOnDone: true, Ghostty: "off", HelperStyle: "terminal", Theme: "lilac", FreezeDog: 20 * time.Second}); c.UI != want {
		t.Errorf("ui = %+v, want %+v", c.UI, want)
	}
	if want := (Spawn{DefaultProfile: "fast", DefaultCwd: "/home/alice/scratch", ReadyTimeout: 45 * time.Second, HideTmux: false, CloseWhenDone: true, CloseDelay: 5 * time.Second, Teammates: "panes", FollowUpWindow: 0, FollowUpDelay: time.Second, Preferences: []string{"Text me in one line."}}); !reflect.DeepEqual(c.Spawn, want) {
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
	wantLocal := Local{Ollama: "/home/alice/bin/ollama", Port: 11500, Context: 131072, KVCache: "q4_0", MemoryGiB: 24, Env: map[string]string{"OLLAMA_MODELS": "/home/alice/models"},
		LlamaServer: "/home/alice/llama/llama-server", LlamaPort: 11600, LlamaArgs: []string{"--cpu-moe"}, Models: "~/gguf"}
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
	// Your own rules replace the built-in ones; an empty list has none.
	if want := (Trod{
		On: true, Start: "22:30", End: "6:45", Away: 30 * time.Minute, Nudges: 3, Retry: 5 * time.Minute, MaxPlanUse: 75.5,
		Rules: []string{"Never touch production."}, Guidelines: []string{},
	}); !reflect.DeepEqual(c.Trod, want) {
		t.Errorf("night = %+v, want %+v", c.Trod, want)
	}
	// The defaults themselves are untouched.
	if d := Default(); d.Profiles["claude"].Model != "claude-opus-5-5" || len(d.Worktree.Copy) != 2 || len(d.Repos.Aliases) != 0 {
		t.Error("loading a file changed the defaults")
	}
}

func TestAnEmptyPreferencesListTellsNothing(t *testing.T) {
	c, err := Load(writeFile(t, "[spawn]\npreferences = []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Spawn.Preferences == nil || len(c.Spawn.Preferences) != 0 {
		t.Errorf("spawn.preferences = %#v, want an empty list", c.Spawn.Preferences)
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
		{"[ui]\nfreeze_dog = \"1s\"\n", "ui.freeze_dog = 1s: must be 0s, for never, or at least 3s"},
		{"[ui]\nfreeze_dog = \"-10s\"\n", "ui.freeze_dog"},
		{"[spawn]\nready_timeout = \"-1s\"\n", "ready_timeout"},
		{"[spawn]\nclose_delay = \"-1s\"\n", "close_delay"},
		{"[spawn]\nfollow_up_window = \"-1m\"\n", "spawn.follow_up_window"},
		{"[spawn]\nfollow_up_delay = \"-1s\"\n", "spawn.follow_up_delay"},
		{"[profiles.claude]\nharness = \"bogus\"\n", "harness"},
		{"[tmux]\nsocket = \"a/b\"\n", "socket"},
		{"[tmux]\npath = \"tmux\"\n", "path"},
		{"[ui]\nsidebar_width = \"wide\"\n", "sidebar_width"},
		{"[journal]\nkeep_days = 0\n", "keep_days"},
		{"[journal]\ntool_input = \"everything\"\n", "tool_input"},
		{"[hold]\nmax = \"0s\"\n", "hold.max"},
		{"[skills]\ndisabled = [\"Present\"]\n", "skills.disabled"},
		{"[skills]\ndisabled = [\"a--b\"]\n", "skills.disabled"},
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
		{"[local]\nllama_port = 11435\n", "local.llama_port"},
		{"[local]\nllama_port = 0\n", "local.llama_port"},
		{"[local]\nllama_server = \"llama-server\"\n", "local.llama_server"},
		{"[local]\nmodels = \"gguf\"\n", "local.models"},
		{"[local]\nllama_args = [\"--port\", \"1\"]\n", "--port is Irie's to set"},
		{"[local]\nllama_args = [\"-c=4096\"]\n", "-c is Irie's to set"},
		{"[profiles.big]\nlocal = { runtime = \"llama-server\", min_context = 200000 }\n", "above local.context"},
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
		{"[trod]\nstart = \"11pm\"\n", "trod.start"},
		{"[trod]\nend = \"24:00\"\n", "trod.end"},
		{"[trod]\nend = \"7:5\"\n", "trod.end"},
		{"[trod]\naway = \"-1m\"\n", "trod.away"},
		{"[trod]\nnudges = -1\n", "trod.nudges"},
		{"[trod]\nretry = \"0s\"\n", "trod.retry"},
		{"[trod]\nwithin = \"-1h\"\n", "trod.within"},
		{"[trod]\nmax_working = -2\n", "trod.max_working"},
		{"[trod]\nmax_plan_use = 0\n", "trod.max_plan_use"},
		{"[trod]\nmax_plan_use = 101\n", "trod.max_plan_use"},
		{"[trod]\nrules = [\"Be kind.\", \"  \"]\n", "trod.rules[1]: is empty"},
		{"[trod]\nguidelines = [\"one\\ntwo\"]\n", "trod.guidelines[0]: must be one line"},
		{"[trod]\nrules = [\"" + strings.Repeat("x", 401) + "\"]\n", "trod.rules[0]: must be at most 400"},
		{"[trod]\nrules = [" + strings.Repeat("\"x\", ", 21) + "]\n", "trod.rules: at most 20"},
		{"[spawn]\npreferences = [\"Be brief.\", \"  \"]\n", "spawn.preferences[1]: is empty"},
		{"[spawn]\npreferences = [\"one\\ntwo\"]\n", "spawn.preferences[0]: must be one line"},
		{"[spawn]\npreferences = [\"" + strings.Repeat("x", 401) + "\"]\n", "spawn.preferences[0]: must be at most 400"},
		{"[spawn]\npreferences = [" + strings.Repeat("\"x\", ", 16) + "]\n", "spawn.preferences: at most 15"},
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

// TestNightShiftHours: the shift that a time falls in, or the last one
// before it, overnight, by day and around the clock.
func TestNightShiftHours(t *testing.T) {
	loc := time.FixedZone("EDT", -4*3600)
	at := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, loc) }
	for _, c := range []struct {
		start, end string
		t          time.Time
		from, to   time.Time
		in         bool
	}{
		// Overnight: in it after the start and before the end, either side of midnight.
		{"23:00", "07:00", at(31, 23, 30), at(31, 23, 0), at(32, 7, 0), true},
		{"23:00", "07:00", at(31, 2, 0), at(30, 23, 0), at(31, 7, 0), true},
		{"23:00", "07:00", at(31, 23, 0), at(31, 23, 0), at(32, 7, 0), true},
		// Out of it by day, the last shift is last night's; its end is not in it.
		{"23:00", "07:00", at(31, 12, 0), at(30, 23, 0), at(31, 7, 0), false},
		{"23:00", "07:00", at(31, 7, 0), at(30, 23, 0), at(31, 7, 0), false},
		// A day shift.
		{"9:00", "17:30", at(31, 12, 0), at(31, 9, 0), at(31, 17, 30), true},
		{"9:00", "17:30", at(31, 8, 59), at(30, 9, 0), at(30, 17, 30), false},
		{"9:00", "17:30", at(31, 20, 0), at(31, 9, 0), at(31, 17, 30), false},
		// Around the clock, a day at a time from its start.
		{"23:00", "23:00", at(31, 12, 0), at(30, 23, 0), at(31, 23, 0), true},
		{"0:00", "00:00", at(31, 0, 0), at(31, 0, 0), at(32, 0, 0), true},
	} {
		n := Trod{Start: c.start, End: c.end}
		from, to, in := n.Shift(c.t)
		if !from.Equal(c.from) || !to.Equal(c.to) || in != c.in {
			t.Errorf("%s–%s at %s: %s–%s %v, want %s–%s %v", c.start, c.end, c.t.Format("Jan 2 15:04"),
				from.Format("Jan 2 15:04"), to.Format("Jan 2 15:04"), in, c.from.Format("Jan 2 15:04"), c.to.Format("Jan 2 15:04"), c.in)
		}
	}
	// A change of clocks keeps the shift's hours: the night of 1 November
	// 2026, when New York's clocks go back, is nine hours long.
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	from, to, in := Trod{Start: "23:00", End: "07:00"}.Shift(time.Date(2026, 11, 1, 3, 0, 0, 0, ny))
	if !in || from.Hour() != 23 || to.Hour() != 7 || to.Sub(from) != 9*time.Hour {
		t.Errorf("across the change of clocks: %s–%s %v", from, to, in)
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

func TestFreezeDogIsADurationAndZeroIsNever(t *testing.T) {
	for body, want := range map[string]time.Duration{
		"":                            10 * time.Second,
		"[ui]\nfreeze_dog = \"0s\"\n": 0,
		"[ui]\nfreeze_dog = \"3s\"\n": 3 * time.Second,
		"[ui]\nfreeze_dog = \"1m\"\n": time.Minute,
	} {
		c, err := Load(writeFile(t, body))
		if err != nil {
			t.Errorf("%q: %v", body, err)
			continue
		}
		if c.UI.FreezeDog != want {
			t.Errorf("%q: freeze_dog = %v, want %v", body, c.UI.FreezeDog, want)
		}
	}
	if got := Tree(Default())["ui"].(map[string]any)["freeze_dog"]; got != "10s" {
		t.Errorf("the config op shows freeze_dog as %v, want 10s", got)
	}
}

func TestSkillsAreOnUntilTurnedOff(t *testing.T) {
	d := Default()
	if !d.Skills.Enabled || len(d.Skills.Disabled) != 0 {
		t.Errorf("default skills = %+v, want on with none left out", d.Skills)
	}
	for name, p := range d.Profiles {
		if !p.TakesSkills() {
			t.Errorf("profiles.%s takes no skills by default", name)
		}
	}
	c, err := Load(writeFile(t, `
[skills]
disabled = ["present"]

[profiles.local]
skills = false

[profiles.mine]
command = "claude"
`))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Skills.Enabled || !reflect.DeepEqual(c.Skills.Disabled, []string{"present"}) {
		t.Errorf("skills = %+v", c.Skills)
	}
	if c.Profiles["local"].TakesSkills() {
		t.Error("profiles.local takes skills after skills = false")
	}
	if c.Profiles["local"].Local == nil {
		t.Error("profiles.local lost its local table")
	}
	if !c.Profiles["mine"].TakesSkills() || !c.Profiles["claude"].TakesSkills() {
		t.Error("a profile that says nothing about skills takes none")
	}
	off, err := Load(writeFile(t, "[skills]\nenabled = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if off.Skills.Enabled || off.Skills.Disabled == nil {
		t.Errorf("skills = %+v, want off with an empty list", off.Skills)
	}
}

func TestArchMikaelFromTheFile(t *testing.T) {
	c, err := Load(writeFile(t, "[arch.mikael]\ndoor = false\ngood = [\"Give credit where it is due.\", \"Be honest with clients.\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Mikael{On: true, Door: false, Good: []string{"Give credit where it is due.", "Be honest with clients."}}); !reflect.DeepEqual(c.Arch.Mikael, want) {
		t.Errorf("arch.mikael = %+v, want %+v", c.Arch.Mikael, want)
	}
	c, err = Load(writeFile(t, "[arch.mikael]\non = false\n"))
	if err != nil || c.Arch.Mikael.On || !c.Arch.Mikael.Door || c.Arch.Mikael.Good == nil {
		t.Errorf("arch.mikael = %+v, %v; want off, the rest kept", c.Arch.Mikael, err)
	}
	if _, err := Load(writeFile(t, "[arch.uriel]\non = true\n")); err == nil || !strings.Contains(err.Error(), "arch.uriel") {
		t.Errorf("an arch agent Irie lacks: %v", err)
	}
}

func TestArchValidation(t *testing.T) {
	for body, want := range map[string]string{
		"[arch.mikael]\ngood = [\"\"]\n":                                                     "arch.mikael.good[0]: is empty",
		"[arch.mikael]\ngood = [\"one\\ntwo\"]\n":                                            "arch.mikael.good[0]: must be one line",
		"[arch.mikael]\ngood = [\"" + strings.Repeat("g", 401) + "\"]\n":                     "at most 400 characters",
		"[arch.mikael]\ngood = [" + strings.Repeat("\"g\", ", 21) + "]\n":                    "arch.mikael.good: at most 20",
		"[profiles.helper]\narch = \"nobody\"\n":                                             `profiles.helper.arch = "nobody": the arch agents are mikael`,
		"[profiles.helper]\nharness = \"generic\"\ncommand = \"aider\"\narch = \"mikael\"\n": "profiles.helper.arch: only the claude-code harness",
		"[profiles.helper]\narch = \"mikael\"\nargs = [\"--agent\", \"x\"]\n":                "profiles.helper.args: a profile that runs as an arch agent",
	} {
		if _, err := Load(writeFile(t, body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", body, err, want)
		}
	}
	c, err := Load(writeFile(t, "[profiles.mikael-sonnet]\nmodel = \"claude-sonnet-5-5\"\narch = \"mikael\"\n"))
	if err != nil || c.Profiles["mikael-sonnet"].Arch != "mikael" {
		t.Errorf("a profile of the user's own that runs as Mikael: %+v, %v", c.Profiles["mikael-sonnet"], err)
	}
}
