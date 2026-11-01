// Package config loads Irie's TOML config (DESIGN §5.5). Every key is
// optional, and an unknown key is an error that names it. This version knows
// the ui, spawn, profiles, local, route, repos, worktree, tmux, journal,
// hold, usage, dog and trod sections.
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/irie-people/irie/internal/route"
)

type Config struct {
	UI       UI                 `toml:"ui"`
	Spawn    Spawn              `toml:"spawn"`
	Profiles map[string]Profile `toml:"profiles"`
	Local    Local              `toml:"local"`
	Route    Route              `toml:"route"`
	Repos    Repos              `toml:"repos"`
	Worktree Worktree           `toml:"worktree"`
	Tmux     Tmux               `toml:"tmux"`
	Journal  Journal            `toml:"journal"`
	Hold     Hold               `toml:"hold"`
	Usage    Usage              `toml:"usage"`
	Dog      Dog                `toml:"dog"`
	Trod     Trod               `toml:"trod"`
	Remote   Remote             `toml:"remote"`
}

// Remote is the phone app (docs/PHONE.md): this machine's Irie, served over
// HTTPS on its local network to the phones paired with it. The core keeps
// `irie remote serve` running while it is on.
type Remote struct {
	On     bool   `toml:"on"`     // serve the phone app
	Port   int    `toml:"port"`   // its HTTPS port; the setup page is on the port below, over plain HTTP
	Name   string `toml:"name"`   // what the phone calls this machine; "" = its host name
	Listen string `toml:"listen"` // the address to listen on, such as 192.168.1.20; "" = every address of this machine
}

// DefaultRemotePort is the phone app's HTTPS port; the setup page is on the
// port below it.
const DefaultRemotePort = 7387

// Auto is the profile name of Irie's router (DESIGN §6.4): a task spawned
// on it runs on the profile with the lowest cost per success.
const Auto = "auto"

// Route is how auto picks a profile, and what goes with it (DESIGN §6.4).
type Route struct {
	Profiles     []string                      `toml:"profiles"`      // what auto chooses between
	MinAccuracy  float64                       `toml:"min_accuracy"`  // a profile below this for a task's class is never chosen
	PriorWeight  int                           `toml:"prior_weight"`  // how many outcomes the starting accuracies count as
	Pressure     float64                       `toml:"pressure"`      // from this much of the plan's five hours used, in percent, cloud tokens cost more
	Failover     bool                          `toml:"failover"`      // carry on a task on a cheaper profile when its session hits a usage limit
	Delegate     bool                          `toml:"delegate"`      // tell Claude Code sessions they can hand subtasks to cheaper profiles
	DelegateMode string                        `toml:"delegate_mode"` // the permission mode of a delegated session
	Accuracy     map[string]map[string]float64 `toml:"accuracy"`      // starting guesses: first-time success, by profile and class
	Cost         map[string]float64            `toml:"cost"`          // starting guesses: a task's cost relative to one on +claude
	Words        route.Words                   `toml:"words"`         // what makes a task small, medium or hard
}

// Prior returns where a profile starts, for the router.
func (r Route) Prior(profile string) (route.Prior, bool) {
	acc, ok := r.Accuracy[profile]
	if !ok {
		return route.Prior{}, false
	}
	cost, ok := r.Cost[profile]
	if !ok {
		return route.Prior{}, false
	}
	return route.Prior{Accuracy: maps.Clone(acc), Cost: cost}, true
}

// Priors returns the priors of the route's profiles.
func (r Route) Priors() map[string]route.Prior {
	out := map[string]route.Prior{}
	for _, p := range r.Profiles {
		if pr, ok := r.Prior(p); ok {
			out[p] = pr
		}
	}
	return out
}

// Permission modes Claude Code takes (--permission-mode).
var PermissionModes = []string{"acceptEdits", "auto", "bypassPermissions", "default", "dontAsk", "plan"}

type UI struct {
	SidebarWidth int    `toml:"sidebar_width"` // columns
	FocusKey     string `toml:"focus_key"`     // tmux key that toggles sidebar and slot
	FlipKey      string `toml:"flip_key"`      // tmux key that flips the slot between conversation and terminal; "" for none
	View         string `toml:"view"`          // what the slot shows first: conversation | terminal
	MaxSubRows   int    `toml:"max_sub_rows"`
	RedOnDone    bool   `toml:"red_on_done"`
	Ghostty      string `toml:"ghostty"` // auto | off
	Theme        string `toml:"theme"`   // the palette of the sidebar and the slot, one of Themes
	// HelperStyle is how a view draws a helper beside its session: as
	// cards and plain words (browser) or as a terminal (terminal).
	HelperStyle string `toml:"helper_style"`
	// FreezeDog is how long Irie's window may leave the freeze dog's ping
	// unanswered before the dog force quits it and opens it again; 0 =
	// never (DESIGN §2.4).
	FreezeDog time.Duration `toml:"freeze_dog"`
}

// MinFreezeDog is the shortest ui.freeze_dog: a window busy for a moment,
// as when the machine pages, is not frozen.
const MinFreezeDog = 3 * time.Second

// HelperStyles are the ways ui.helper_style draws a helper.
var HelperStyles = []string{"browser", "terminal"}

// Themes are the sidebar's palettes (DESIGN §5.5), the Mac app's. Each has a
// light and a dark set; the terminal's background picks one.
var Themes = []string{"rosewater", "terracotta", "lilac", "evergreen"}

type Spawn struct {
	DefaultProfile string        `toml:"default_profile"`
	DefaultCwd     string        `toml:"default_cwd"` // "" = a new scratch dir per task
	ReadyTimeout   time.Duration `toml:"ready_timeout"`
	HideTmux       bool          `toml:"hide_tmux"`
	CloseWhenDone  bool          `toml:"close_when_done"` // close a session once its turn ends and it stays quiet for close_delay
	CloseDelay     time.Duration `toml:"close_delay"`     // how long a finished session waits before it closes
	// Teammates is what Claude Code sessions do with agent teams: run each
	// teammate in a terminal pane of its own, which the Mac app shows beside
	// its session (panes), or start no teams (off).
	Teammates string `toml:"teammates"`
}

// TeammateModes are the values of spawn.teammates.
var TeammateModes = []string{"panes", "off"}

// Profile is a named launch recipe.
type Profile struct {
	Harness        string            `toml:"harness"`
	Command        string            `toml:"command"`
	Model          string            `toml:"model"`
	Effort         string            `toml:"effort"`
	PermissionMode string            `toml:"permission_mode"`
	Args           []string          `toml:"args"`
	Env            map[string]string `toml:"env"`
	Task           string            `toml:"task"`  // how a generic agent gets its task: arg | paste; "" = arg
	Local          *LocalModel       `toml:"local"` // the agent runs on a model of a local runtime
	Cost           string            `toml:"cost"`  // what the + list says it costs, such as "free, 50 requests a day"; "" = Irie's own reading
}

// LocalModel is how a profile runs on a local model (DESIGN §6.1). With no
// model of its own, the profile runs the one `irie setup local` chose.
type LocalModel struct {
	Runtime    string `toml:"runtime"`     // ollama | llama-server
	MinContext int    `toml:"min_context"` // a spawn is refused below this context, in tokens
	Enforce    bool   `toml:"enforce"`     // false: spawn anyway, with a warning, when the context or memory is short
}

// DefaultMinContext is Ollama's guidance for coding agents: 64k tokens.
const DefaultMinContext = 65536

// Runtimes are the servers a local profile may run on (DESIGN §6.1, §6.5).
var Runtimes = []string{"ollama", "llama-server"}

// Local is the Ollama server local profiles run on (DESIGN §6.3): Irie's
// own, which it runs in its tmux server with the settings below, or one the
// user runs at url.
type Local struct {
	Ollama    string            `toml:"ollama"`     // the ollama binary; "" = PATH, then the usual install places
	URL       string            `toml:"url"`        // a server you run instead, such as "http://127.0.0.1:11434"; "" = Irie runs its own
	Port      int               `toml:"port"`       // Irie's own server listens on 127.0.0.1 at this port
	Context   int               `toml:"context"`    // its context length in tokens (OLLAMA_CONTEXT_LENGTH)
	KVCache   string            `toml:"kv_cache"`   // its context cache type (OLLAMA_KV_CACHE_TYPE): f16, q8_0 or q4_0
	MemoryGiB int               `toml:"memory_gib"` // the most memory local models may take; 0 = two thirds of the machine's
	Env       map[string]string `toml:"env"`        // more settings for its server, such as OLLAMA_MODELS

	// Irie's own llama-server, for profiles with runtime = "llama-server"
	// (DESIGN §6.5). It takes local.context and local.kv_cache too.
	LlamaServer string   `toml:"llama_server"` // the llama-server binary, any llama.cpp build; "" = PATH, then Homebrew's
	LlamaPort   int      `toml:"llama_port"`   // it listens on 127.0.0.1 at this port
	LlamaArgs   []string `toml:"llama_args"`   // more arguments for it, such as a build's own
	Models      string   `toml:"models"`       // a folder of GGUF files it may run besides Ollama's models; "" = $XDG_DATA_HOME/irie/models
}

// KV cache types Ollama offers (DESIGN §6.3).
var KVCaches = []string{"f16", "q8_0", "q4_0"}

// Usage is how Irie learns what a session costs (DESIGN §6.3).
type Usage struct {
	// StatusLine makes Irie the status line of Claude Code sessions, to read
	// their cost, context and plan use. Irie runs the user's own status
	// line and shows what it prints.
	StatusLine bool `toml:"status_line"`
}

// Dog is the jot dog (DESIGN §4.8): it sees each task you jot down through
// until its work has landed. When a jot's turn ends with work left in its
// own worktree, uncommitted or not yet in its repo's land branch, it sends
// the agent back to finish, a few times at most between two of your
// prompts, and the jot's row says what is left.
type Dog struct {
	Watch  bool              `toml:"watch"`  // check a jot's own worktree when its turn ends, and show what is left
	Nudges int               `toml:"nudges"` // how often it sends one jot back to finish between two of your prompts; 0 = never
	Land   map[string]string `toml:"land"`   // by repo (alias, folder name or path), the branch its work must reach, such as live
}

// Trod is the trod bot (DESIGN §4.10): while you are away at night, it
// keeps your sessions going until their tasks are through. An agent that
// asks you something is told to decide itself; a turn an API error
// stopped, a jot that is not through, and a usage limit that reset without
// the session carrying on are sent on again; and the Mac stays awake while
// any of them has work left. Every agent it sends on is told your rules
// and guidelines. It keeps within the plan: past max_plan_use it sends
// nothing on, and past it in the week's window it freezes what would
// still spend the week's allotment.
type Trod struct {
	On         bool          `toml:"on"`           // work the shift every night, from start to end
	Start      string        `toml:"start"`        // when it starts, in local time, such as "23:00"
	End        string        `toml:"end"`          // when it ends; the same as start means around the clock
	Away       time.Duration `toml:"away"`         // how long the Mac must have had no keyboard or mouse input before it steps in; 0 = at once
	Questions  bool          `toml:"questions"`    // tell an agent that asks you something to decide itself
	Nudges     int           `toml:"nudges"`       // how often it may send one session on in a night
	Retry      time.Duration `toml:"retry"`        // how long a stopped session waits before it is sent on again
	Within     time.Duration `toml:"within"`       // it sends on only sessions that stopped this recently; 0 = however long ago
	MaxWorking int           `toml:"max_working"`  // it sends a session on only while fewer than this many work; 0 = no limit
	MaxPlanUse float64       `toml:"max_plan_use"` // percent: past this in either of the plan's windows it sends nothing on, and past it in the week's it freezes what still spends
	KeepAwake  bool          `toml:"keep_awake"`   // keep the Mac from sleeping while sessions have work left
	Rules      []string      `toml:"rules"`        // what every agent it sends on must, or must not, do
	Guidelines []string      `toml:"guidelines"`   // how those agents decide while you are away
}

// TrodRules are the trod bot's rules until you write your own: nothing
// leaves the machine, nothing that was there is lost, and nothing costs
// money.
func TrodRules() []string {
	return []string{
		"Don't push, publish or send anything off this machine: no git push, releases, posts, emails or messages.",
		"Don't delete or overwrite what you didn't make, and never rewrite git history.",
		"Don't spend money, sign up for anything, or change accounts, plans, keys or settings outside your task; never buy usage credits or turn on extra usage.",
	}
}

// TrodGuidelines are the trod bot's guidelines until you write your
// own.
func TrodGuidelines() []string {
	return []string{
		"When you have to choose, take the option you would recommend, and say in your last message which you took and why.",
		"Finish the task you were given; note new ideas and extra work for the morning instead of starting them.",
		"If a lock, an install or another session holds you up, wait a few minutes and try again before you stop.",
		"Commit and land your work the way the repo's AGENTS.md or CLAUDE.md says.",
	}
}

// Shift is the trod bot that t falls in, or else the last one before t:
// when it started and when it ends, and whether t is in it. With the same
// start and end, the shift goes around the clock, a day at a time from its
// start. Times are t's location's, so a shift keeps its hours across a
// change of clocks.
func (n Trod) Shift(t time.Time) (from, to time.Time, in bool) {
	sh, sm, _ := clockTime(n.Start)
	eh, em, _ := clockTime(n.End)
	at := func(day time.Time, h, m int) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, t.Location())
	}
	from = at(t, sh, sm)
	if from.After(t) {
		from = at(t.AddDate(0, 0, -1), sh, sm)
	}
	to = at(from, eh, em)
	if !to.After(from) {
		to = at(from.AddDate(0, 0, 1), eh, em)
	}
	return from, to, t.Before(to)
}

// clockTime reads a time of day such as "23:00" or "7:30".
func clockTime(s string) (h, m int, ok bool) {
	g := clockPattern.FindStringSubmatch(s)
	if g == nil {
		return 0, 0, false
	}
	h, _ = strconv.Atoi(g[1])
	m, _ = strconv.Atoi(g[2])
	return h, m, true
}

var clockPattern = regexp.MustCompile(`^([01]?[0-9]|2[0-3]):([0-5][0-9])$`)

// Most rules and guidelines the trod bot takes, and the longest each
// may be: they go into every message it sends.
const (
	maxTrodLines = 20
	maxTrodLine  = 400
)

// Repos is where an @name finds its repo: aliases first, then the git repos
// two levels under the roots.
type Repos struct {
	Roots   []string          `toml:"roots"`   // absolute or ~ paths
	Aliases map[string]string `toml:"aliases"` // name → absolute or ~ path
}

// Worktree is how an @repo task gets its own checkout.
type Worktree struct {
	Root         string   `toml:"root"` // "" = $XDG_DATA_HOME/irie/worktrees
	Base         string   `toml:"base"` // what a new branch starts from
	BranchPrefix string   `toml:"branch_prefix"`
	Copy         []string `toml:"copy"` // untracked files copied in, besides those of the repo's .worktreeinclude
	Submodules   bool     `toml:"submodules"`
}

type Tmux struct {
	Path   string `toml:"path"`   // "" = PATH, then the usual install dirs
	Socket string `toml:"socket"` // tmux -L name
}

type Journal struct {
	KeepDays  int    `toml:"keep_days"`
	ToolInput string `toml:"tool_input"` // summary | none
}

// Hold is how long a paused agent may be held. Hook timeouts are set above
// it, so a harness never lets a held call through on its own.
type Hold struct {
	Max time.Duration `toml:"max"`
}

// Harnesses lists the harness ids this version can launch.
var Harnesses = []string{"claude-code", "generic"}

// How a generic agent gets its task (profile.task).
const (
	TaskArg   = "arg"   // as its last argument
	TaskPaste = "paste" // pasted into its terminal once it runs, then Enter
)

// Default returns the built-in config. A task with no profile goes to auto,
// which runs it on the profile with the lowest cost per success: hard work
// on claude, Opus 5.5 on max effort in auto mode; ordinary work on sonnet,
// Sonnet 5.5 on medium effort; small work on haiku, Haiku 5.5, or on local,
// Claude Code on a model of Irie's own Ollama server, which costs no tokens
// but takes the machine's memory (DESIGN §6.4).
func Default() Config {
	return Config{
		UI: UI{SidebarWidth: 34, FocusKey: "C-]", FlipKey: `C-\`, View: "conversation", MaxSubRows: 6, RedOnDone: false, Ghostty: "auto", HelperStyle: "browser", Theme: "rosewater", FreezeDog: 10 * time.Second},
		Spawn: Spawn{
			DefaultProfile: Auto,
			ReadyTimeout:   30 * time.Second,
			HideTmux:       true,
			CloseDelay:     30 * time.Second,
			Teammates:      "off",
		},
		Profiles: map[string]Profile{
			"claude": {
				Harness:        "claude-code",
				Command:        "claude",
				Model:          "claude-opus-5-5",
				Effort:         "max",
				PermissionMode: "auto",
				Args:           []string{},
				Env:            map[string]string{},
			},
			"sonnet": {
				Harness:        "claude-code",
				Command:        "claude",
				Model:          "claude-sonnet-5-5",
				Effort:         "medium",
				PermissionMode: "auto",
				Args:           []string{},
				Env:            map[string]string{},
			},
			"haiku": {
				Harness:        "claude-code",
				Command:        "claude",
				Model:          "claude-haiku-5-5",
				PermissionMode: "auto",
				Args:           []string{},
				Env:            map[string]string{},
			},
			"local": {
				Harness: "claude-code",
				Command: "claude",
				Args:    []string{},
				Env:     map[string]string{},
				Local:   &LocalModel{Runtime: "ollama", MinContext: DefaultMinContext, Enforce: true},
			},
			"big": {
				Harness: "claude-code",
				Command: "claude",
				Args:    []string{},
				Env:     map[string]string{},
				Local:   &LocalModel{Runtime: "llama-server", MinContext: DefaultMinContext, Enforce: true},
			},
		},
		Local: Local{Port: 11435, Context: DefaultMinContext, KVCache: "q8_0", Env: map[string]string{}, LlamaPort: 11436, LlamaArgs: []string{}},
		Route: DefaultRoute(),
		Repos: Repos{Roots: []string{}, Aliases: map[string]string{}},
		Worktree: Worktree{
			Base:         "HEAD",
			BranchPrefix: "irie/",
			Copy:         []string{".claude/settings.local.json", "CLAUDE.local.md"},
			Submodules:   true,
		},
		Tmux:    Tmux{Socket: "irie"},
		Journal: Journal{KeepDays: 30, ToolInput: "summary"},
		Hold:    Hold{Max: 24 * time.Hour},
		Usage:   Usage{StatusLine: true},
		Dog:     Dog{Watch: true, Nudges: 2, Land: map[string]string{}},
		Trod: Trod{
			Start: "23:00", End: "07:00", Away: 15 * time.Minute, Questions: true, Nudges: 8,
			Retry: 15 * time.Minute, Within: 24 * time.Hour, MaxWorking: 4, MaxPlanUse: 90, KeepAwake: true,
			Rules: TrodRules(), Guidelines: TrodGuidelines(),
		},
		Remote: Remote{Port: DefaultRemotePort},
	}
}

// DefaultRoute is where routing starts (DESIGN §6.4). The accuracies are
// guesses, which the user's own outcomes replace; with the floor at 85%
// they keep hard work on claude. The costs follow Anthropic's list prices:
// Sonnet 5.5 costs half as much as Opus 5.5 per token and spends fewer
// thinking tokens at medium effort than at max, and Haiku 5.5 costs a
// fortieth. A local model costs no tokens.
func DefaultRoute() Route {
	return Route{
		Profiles:     []string{"local", "haiku", "sonnet", "claude"},
		MinAccuracy:  0.85,
		PriorWeight:  10,
		Pressure:     80,
		Failover:     true,
		Delegate:     true,
		DelegateMode: "acceptEdits",
		Accuracy: map[string]map[string]float64{
			"local":  {route.Small: 0.85, route.Medium: 0.40, route.Hard: 0.05},
			"haiku":  {route.Small: 0.92, route.Medium: 0.70, route.Hard: 0.30},
			"sonnet": {route.Small: 0.96, route.Medium: 0.88, route.Hard: 0.65},
			"claude": {route.Small: 0.98, route.Medium: 0.94, route.Hard: 0.90},
		},
		Cost: map[string]float64{"local": 0, "haiku": 0.02, "sonnet": 0.25, "claude": 1},
		Words: route.Words{
			Small:  slices.Clone(route.DefaultWords.Small),
			Medium: slices.Clone(route.DefaultWords.Medium),
			Hard:   slices.Clone(route.DefaultWords.Hard),
		},
	}
}

// fillRoute puts back the default priors a file leaves out: a table that
// names one profile, or one class, keeps the others.
func fillRoute(r *Route) {
	d := DefaultRoute()
	if r.Accuracy == nil {
		r.Accuracy = map[string]map[string]float64{}
	}
	for p, classes := range d.Accuracy {
		if r.Accuracy[p] == nil {
			r.Accuracy[p] = classes
			continue
		}
		for class, v := range classes {
			if _, ok := r.Accuracy[p][class]; !ok {
				r.Accuracy[p][class] = v
			}
		}
	}
	if r.Cost == nil {
		r.Cost = map[string]float64{}
	}
	for p, v := range d.Cost {
		if _, ok := r.Cost[p]; !ok {
			r.Cost[p] = v
		}
	}
	if r.Profiles == nil {
		r.Profiles = []string{}
	}
}

// Load reads the config file at path over the defaults. A missing file gives
// the defaults.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}
	return Parse(data, path)
}

// Parse reads a config file's contents over the defaults; path names the
// file in errors.
func Parse(data []byte, path string) (Config, error) {
	c := Default()

	// Sections decode over the defaults. Profiles decode one by one, so a file
	// that sets only profiles.claude.model keeps that profile's other defaults.
	file := struct {
		UI       *UI                       `toml:"ui"`
		Spawn    *Spawn                    `toml:"spawn"`
		Profiles map[string]toml.Primitive `toml:"profiles"`
		Local    *Local                    `toml:"local"`
		Route    *Route                    `toml:"route"`
		Repos    *Repos                    `toml:"repos"`
		Worktree *Worktree                 `toml:"worktree"`
		Tmux     *Tmux                     `toml:"tmux"`
		Journal  *Journal                  `toml:"journal"`
		Hold     *Hold                     `toml:"hold"`
		Usage    *Usage                    `toml:"usage"`
		Dog      *Dog                      `toml:"dog"`
		Trod     *Trod                     `toml:"trod"`
		Remote   *Remote                   `toml:"remote"`
	}{UI: &c.UI, Spawn: &c.Spawn, Local: &c.Local, Route: &c.Route, Repos: &c.Repos, Worktree: &c.Worktree, Tmux: &c.Tmux, Journal: &c.Journal, Hold: &c.Hold, Usage: &c.Usage, Dog: &c.Dog, Trod: &c.Trod, Remote: &c.Remote}
	md, err := toml.Decode(string(data), &file)
	if err != nil {
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	for _, name := range slices.Sorted(maps.Keys(file.Profiles)) {
		p, ok := c.Profiles[name]
		if !ok {
			p = Profile{Harness: "claude-code"}
		}
		p.Args = slices.Clone(p.Args)
		p.Env = maps.Clone(p.Env)
		// A local table decodes over its defaults, so it may name only its
		// runtime.
		switch {
		case p.Local != nil:
			l := *p.Local
			p.Local = &l
		case md.IsDefined("profiles", name, "local"):
			p.Local = &LocalModel{MinContext: DefaultMinContext, Enforce: true}
		}
		if err := md.PrimitiveDecode(file.Profiles[name], &p); err != nil {
			return c, fmt.Errorf("config %s: profiles.%s: %w", path, name, err)
		}
		if p.Args == nil {
			p.Args = []string{}
		}
		if p.Env == nil {
			p.Env = map[string]string{}
		}
		c.Profiles[name] = p
	}
	if c.Repos.Roots == nil {
		c.Repos.Roots = []string{}
	}
	if c.Repos.Aliases == nil {
		c.Repos.Aliases = map[string]string{}
	}
	if c.Worktree.Copy == nil {
		c.Worktree.Copy = []string{}
	}
	if c.Local.Env == nil {
		c.Local.Env = map[string]string{}
	}
	if c.Dog.Land == nil {
		c.Dog.Land = map[string]string{}
	}
	if c.Trod.Rules == nil {
		c.Trod.Rules = []string{}
	}
	if c.Trod.Guidelines == nil {
		c.Trod.Guidelines = []string{}
	}
	fillRoute(&c.Route)
	if keys := md.Undecoded(); len(keys) > 0 {
		names := make([]string, len(keys))
		for i, k := range keys {
			names[i] = fmt.Sprintf("%q", k.String())
		}
		return c, fmt.Errorf("config %s: unknown key %s", path, strings.Join(names, ", "))
	}
	if err := c.Validate(); err != nil {
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	return c, nil
}

var (
	socketName  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	tmuxKey     = regexp.MustCompile(`^[!-~]+$`) // printable, no spaces
	repoName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	branchChars = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
)

// hasPrior reports whether a route profile has an accuracy for every class
// and a cost.
func hasPrior(r Route, p string) bool {
	if _, ok := r.Cost[p]; !ok {
		return false
	}
	for _, class := range route.Classes {
		if _, ok := r.Accuracy[p][class]; !ok {
			return false
		}
	}
	return true
}

// pathLike reports whether p is an absolute path or a ~ path.
func pathLike(p string) bool { return filepath.IsAbs(p) || p == "~" || strings.HasPrefix(p, "~/") }

// branchPrefix reports whether p can start a git branch name: the safe part
// of git's ref rules, so that a name it builds never reads as an option or
// a range.
func branchPrefix(p string) bool {
	if p == "" {
		return true
	}
	if !branchChars.MatchString(p) || strings.Contains(p, "..") || strings.Contains(p, "//") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "-") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// inRepo reports whether p is a path inside a repo: relative, and never
// climbing out of it.
func inRepo(p string) bool {
	clean := filepath.Clean(p)
	return p != "" && clean != "." && !filepath.IsAbs(p) && !slices.Contains(strings.Split(filepath.ToSlash(clean), "/"), "..")
}

// Validate checks values that decode but make no sense.
func (c Config) Validate() error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if c.UI.SidebarWidth < 10 {
		bad("ui.sidebar_width = %d: must be at least 10", c.UI.SidebarWidth)
	}
	if !tmuxKey.MatchString(c.UI.FocusKey) || strings.ContainsAny(c.UI.FocusKey, `'"#`) {
		bad("ui.focus_key = %q: must be a tmux key name such as C-]", c.UI.FocusKey)
	}
	if c.UI.FlipKey != "" && (!tmuxKey.MatchString(c.UI.FlipKey) || strings.ContainsAny(c.UI.FlipKey, `'"#`) || c.UI.FlipKey == c.UI.FocusKey) {
		bad("ui.flip_key = %q: must be a tmux key name such as C-\\, other than ui.focus_key, or empty for none", c.UI.FlipKey)
	}
	if c.UI.View != "conversation" && c.UI.View != "terminal" {
		bad("ui.view = %q: must be conversation or terminal", c.UI.View)
	}
	if c.UI.MaxSubRows < 0 {
		bad("ui.max_sub_rows = %d: must not be negative", c.UI.MaxSubRows)
	}
	if c.UI.Ghostty != "auto" && c.UI.Ghostty != "off" {
		bad("ui.ghostty = %q: must be auto or off", c.UI.Ghostty)
	}
	if !slices.Contains(TeammateModes, c.Spawn.Teammates) {
		bad("spawn.teammates = %q: must be panes or off", c.Spawn.Teammates)
	}
	if !slices.Contains(HelperStyles, c.UI.HelperStyle) {
		bad("ui.helper_style = %q: must be browser or terminal", c.UI.HelperStyle)
	}
	if !slices.Contains(Themes, c.UI.Theme) {
		bad("ui.theme = %q: must be one of %s", c.UI.Theme, strings.Join(Themes, ", "))
	}
	if c.UI.FreezeDog != 0 && c.UI.FreezeDog < MinFreezeDog {
		bad("ui.freeze_dog = %v: must be 0s, for never, or at least %v", c.UI.FreezeDog, MinFreezeDog)
	}
	if _, ok := c.Profiles[c.Spawn.DefaultProfile]; !ok && c.Spawn.DefaultProfile != Auto {
		bad("spawn.default_profile = %q: no such profile, and not auto", c.Spawn.DefaultProfile)
	}
	if _, ok := c.Profiles[Auto]; ok {
		bad("profiles.%s: auto is Irie's router; give the profile another name", Auto)
	}
	r := c.Route
	for _, p := range r.Profiles {
		switch _, isProfile := c.Profiles[p]; {
		case !isProfile:
			bad("route.profiles: %q is not a profile", p)
		case !hasPrior(r, p):
			bad("route.profiles: %q needs route.accuracy.%s and route.cost.%s", p, p, p)
		}
	}
	if r.MinAccuracy <= 0 || r.MinAccuracy > 1 {
		bad("route.min_accuracy = %v: must be above 0 and at most 1", r.MinAccuracy)
	}
	if r.PriorWeight < 0 {
		bad("route.prior_weight = %d: must not be negative", r.PriorWeight)
	}
	if r.Pressure < 0 || r.Pressure > 100 {
		bad("route.pressure = %v: must be a percentage", r.Pressure)
	}
	if !slices.Contains(PermissionModes, r.DelegateMode) {
		bad("route.delegate_mode = %q: must be one of %s", r.DelegateMode, strings.Join(PermissionModes, ", "))
	}
	for _, p := range slices.Sorted(maps.Keys(r.Accuracy)) {
		for _, class := range slices.Sorted(maps.Keys(r.Accuracy[p])) {
			if !slices.Contains(route.Classes, class) {
				bad("route.accuracy.%s.%s: the classes are %s", p, class, strings.Join(route.Classes, ", "))
			} else if v := r.Accuracy[p][class]; v < 0 || v > 1 {
				bad("route.accuracy.%s.%s = %v: must be from 0 to 1", p, class, v)
			}
		}
	}
	for _, p := range slices.Sorted(maps.Keys(r.Cost)) {
		if r.Cost[p] < 0 {
			bad("route.cost.%s = %v: must not be negative", p, r.Cost[p])
		}
	}
	if c.Spawn.ReadyTimeout <= 0 {
		bad("spawn.ready_timeout = %v: must be positive", c.Spawn.ReadyTimeout)
	}
	if c.Spawn.CloseDelay < 0 {
		bad("spawn.close_delay = %v: must not be negative", c.Spawn.CloseDelay)
	}
	for _, name := range slices.Sorted(maps.Keys(c.Profiles)) {
		p := c.Profiles[name]
		if !slices.Contains(Harnesses, p.Harness) {
			bad("profiles.%s.harness = %q: must be one of %s", name, p.Harness, strings.Join(Harnesses, ", "))
		}
		switch p.Task {
		case "", TaskArg:
		case TaskPaste:
			if p.Harness != "generic" {
				bad("profiles.%s.task = %q: only the generic harness takes its task by paste", name, p.Task)
			}
		default:
			bad("profiles.%s.task = %q: must be arg or paste", name, p.Task)
		}
		if strings.ContainsAny(p.Cost, "\n\r") {
			bad("profiles.%s.cost: must be one line", name)
		}
		if l := p.Local; l != nil {
			switch {
			case !slices.Contains(Runtimes, l.Runtime):
				bad("profiles.%s.local.runtime = %q: must be one of %s", name, l.Runtime, strings.Join(Runtimes, ", "))
			case p.Harness != "claude-code":
				bad("profiles.%s.local: only the claude-code harness runs on a local model yet", name)
			case l.MinContext < 1:
				bad("profiles.%s.local.min_context = %d: must be positive", name, l.MinContext)
			case (c.Local.URL == "" || l.Runtime == "llama-server") && l.MinContext > c.Local.Context:
				bad("profiles.%s.local.min_context = %d is above local.context = %d, so Irie's own server could never run it", name, l.MinContext, c.Local.Context)
			}
		}
		if p.Harness != "generic" {
			continue
		}
		if p.Command == "" {
			bad("profiles.%s.command: the generic harness needs a command", name)
		}
		for _, f := range [][2]string{{"model", p.Model}, {"effort", p.Effort}, {"permission_mode", p.PermissionMode}} {
			if f[1] != "" {
				bad("profiles.%s.%s = %q: the generic harness has no such setting; put the CLI's own flags in args", name, f[0], f[1])
			}
		}
	}
	if c.Local.Ollama != "" && !filepath.IsAbs(c.Local.Ollama) {
		bad("local.ollama = %q: must be an absolute path", c.Local.Ollama)
	}
	if c.Local.URL != "" {
		if u, err := url.Parse(c.Local.URL); err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
			bad("local.url = %q: must be an http:// or https:// URL, such as http://127.0.0.1:11434", c.Local.URL)
		}
	}
	if c.Local.Port < 1 || c.Local.Port > 65535 {
		bad("local.port = %d: must be a TCP port", c.Local.Port)
	}
	if c.Local.LlamaPort < 1 || c.Local.LlamaPort > 65535 || c.Local.LlamaPort == c.Local.Port {
		bad("local.llama_port = %d: must be a TCP port other than local.port", c.Local.LlamaPort)
	}
	if c.Local.LlamaServer != "" && !filepath.IsAbs(c.Local.LlamaServer) {
		bad("local.llama_server = %q: must be an absolute path", c.Local.LlamaServer)
	}
	if c.Local.Models != "" && !pathLike(c.Local.Models) {
		bad("local.models = %q: must be a path", c.Local.Models)
	}
	for _, a := range c.Local.LlamaArgs {
		for _, own := range []string{"-m", "--model", "--host", "--port", "-c", "--ctx-size", "-np", "--parallel", "--alias", "-a"} {
			if a == own || strings.HasPrefix(a, own+"=") {
				bad("local.llama_args: %s is Irie's to set; use local.models, local.llama_port or local.context", own)
			}
		}
	}
	if c.Local.Context < 2048 {
		bad("local.context = %d: must be at least 2048 tokens", c.Local.Context)
	}
	if !slices.Contains(KVCaches, c.Local.KVCache) {
		bad("local.kv_cache = %q: must be one of %s", c.Local.KVCache, strings.Join(KVCaches, ", "))
	}
	if c.Local.MemoryGiB < 0 {
		bad("local.memory_gib = %d: must not be negative", c.Local.MemoryGiB)
	}
	if _, ok := c.Local.Env["OLLAMA_HOST"]; ok {
		bad("local.env: OLLAMA_HOST is Irie's to set; use local.port, or local.url for a server you run")
	}
	for _, r := range c.Repos.Roots {
		if !pathLike(r) {
			bad("repos.roots: %q must be an absolute or ~/ path", r)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(c.Repos.Aliases)) {
		if !repoName.MatchString(name) {
			bad("repos.aliases: %q: a name is letters, digits, '.', '_' and '-', and starts with a letter or digit", name)
		}
		if p := c.Repos.Aliases[name]; !pathLike(p) {
			bad("repos.aliases.%s = %q: must be an absolute or ~/ path", name, p)
		}
	}
	if c.Worktree.Root != "" && !pathLike(c.Worktree.Root) {
		bad("worktree.root = %q: must be an absolute or ~/ path", c.Worktree.Root)
	}
	if strings.TrimSpace(c.Worktree.Base) == "" || strings.HasPrefix(c.Worktree.Base, "-") {
		bad("worktree.base = %q: must name a commit, such as HEAD", c.Worktree.Base)
	}
	if !branchPrefix(c.Worktree.BranchPrefix) {
		bad("worktree.branch_prefix = %q: must be able to start a git branch name, such as irie/", c.Worktree.BranchPrefix)
	}
	for _, f := range c.Worktree.Copy {
		if !inRepo(f) {
			bad("worktree.copy: %q must be a path inside the repo", f)
		}
	}
	if !socketName.MatchString(c.Tmux.Socket) {
		bad("tmux.socket = %q: use letters, digits, '.', '_' and '-'", c.Tmux.Socket)
	}
	if c.Tmux.Path != "" && !filepath.IsAbs(c.Tmux.Path) {
		bad("tmux.path = %q: must be an absolute path", c.Tmux.Path)
	}
	if c.Journal.KeepDays < 1 {
		bad("journal.keep_days = %d: must be at least 1", c.Journal.KeepDays)
	}
	if c.Journal.ToolInput != "summary" && c.Journal.ToolInput != "none" {
		bad("journal.tool_input = %q: must be summary or none", c.Journal.ToolInput)
	}
	if c.Hold.Max <= 0 {
		bad("hold.max = %v: must be positive", c.Hold.Max)
	}
	if c.Dog.Nudges < 0 {
		bad("dog.nudges = %d: must not be negative", c.Dog.Nudges)
	}
	for _, repo := range slices.Sorted(maps.Keys(c.Dog.Land)) {
		branch := c.Dog.Land[repo]
		switch {
		case strings.TrimSpace(repo) == "":
			bad("dog.land: a repo needs a name, such as irie")
		case !branchChars.MatchString(branch) || strings.HasPrefix(branch, "-") || strings.Contains(branch, ".."):
			bad("dog.land.%s = %q: must name a branch, such as live", repo, branch)
		}
	}
	n := c.Trod
	for _, f := range [][2]string{{"start", n.Start}, {"end", n.End}} {
		if _, _, ok := clockTime(f[1]); !ok {
			bad("trod.%s = %q: must be a time of day, such as 23:00", f[0], f[1])
		}
	}
	if n.Away < 0 {
		bad("trod.away = %v: must not be negative", n.Away)
	}
	if n.Nudges < 0 {
		bad("trod.nudges = %d: must not be negative", n.Nudges)
	}
	if n.Retry <= 0 {
		bad("trod.retry = %v: must be positive", n.Retry)
	}
	if n.Within < 0 {
		bad("trod.within = %v: must not be negative", n.Within)
	}
	if n.MaxWorking < 0 {
		bad("trod.max_working = %d: must not be negative", n.MaxWorking)
	}
	if n.MaxPlanUse <= 0 || n.MaxPlanUse > 100 {
		bad("trod.max_plan_use = %v: must be a percentage above 0", n.MaxPlanUse)
	}
	for _, list := range []struct {
		key   string
		lines []string
	}{{"rules", n.Rules}, {"guidelines", n.Guidelines}} {
		if len(list.lines) > maxTrodLines {
			bad("trod.%s: at most %d, as each goes into every message the trod bot sends", list.key, maxTrodLines)
		}
		for i, l := range list.lines {
			switch {
			case strings.TrimSpace(l) == "":
				bad("trod.%s[%d]: is empty", list.key, i)
			case strings.ContainsAny(l, "\r\n"):
				bad("trod.%s[%d]: must be one line", list.key, i)
			case utf8.RuneCountInString(l) > maxTrodLine:
				bad("trod.%s[%d]: must be at most %d characters", list.key, i, maxTrodLine)
			}
		}
	}
	rm := c.Remote
	if rm.Port < 2 || rm.Port > 65535 {
		bad("remote.port = %d: must be a port from 2 to 65535, since the setup page takes the one below", rm.Port)
	}
	if rm.Listen != "" && net.ParseIP(rm.Listen) == nil {
		bad("remote.listen = %q: must be an IP address of this machine, or empty for all of them", rm.Listen)
	}
	if strings.ContainsFunc(rm.Name, unicode.IsControl) || utf8.RuneCountInString(rm.Name) > 60 {
		bad("remote.name = %q: must be one line of at most 60 characters", rm.Name)
	}
	return errors.Join(errs...)
}

// Write prints c as TOML.
func (c Config) Write(w io.Writer) error {
	return toml.NewEncoder(w).Encode(c)
}
