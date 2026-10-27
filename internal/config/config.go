// Package config loads Irie's TOML config (DESIGN §5.5). Every key is
// optional, and an unknown key is an error that names it. This version knows
// the ui, spawn, profiles, repos, worktree, tmux, journal and hold sections.
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	UI       UI                 `toml:"ui"`
	Spawn    Spawn              `toml:"spawn"`
	Profiles map[string]Profile `toml:"profiles"`
	Repos    Repos              `toml:"repos"`
	Worktree Worktree           `toml:"worktree"`
	Tmux     Tmux               `toml:"tmux"`
	Journal  Journal            `toml:"journal"`
	Hold     Hold               `toml:"hold"`
}

type UI struct {
	SidebarWidth int    `toml:"sidebar_width"` // columns
	FocusKey     string `toml:"focus_key"`     // tmux key that toggles sidebar and slot
	MaxSubRows   int    `toml:"max_sub_rows"`
	RedOnDone    bool   `toml:"red_on_done"`
	Ghostty      string `toml:"ghostty"` // auto | off
}

type Spawn struct {
	DefaultProfile string        `toml:"default_profile"`
	DefaultCwd     string        `toml:"default_cwd"` // "" = a new scratch dir per task
	ReadyTimeout   time.Duration `toml:"ready_timeout"`
	HideTmux       bool          `toml:"hide_tmux"`
}

// Profile is a named launch recipe.
type Profile struct {
	Harness        string            `toml:"harness"`
	Command        string            `toml:"command"`
	Model          string            `toml:"model"`
	Effort         string            `toml:"effort"`
	PermissionMode string            `toml:"permission_mode"`
	Args           []string          `toml:"args"`
	Env            map[string]string `toml:"env"`
	Task           string            `toml:"task"` // how a generic agent gets its task: arg | paste; "" = arg
}

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

// Default returns the built-in config. The default agent is Opus 5.5 on max
// effort in auto mode.
func Default() Config {
	return Config{
		UI: UI{SidebarWidth: 34, FocusKey: "C-]", MaxSubRows: 6, RedOnDone: false, Ghostty: "auto"},
		Spawn: Spawn{
			DefaultProfile: "claude",
			ReadyTimeout:   30 * time.Second,
			HideTmux:       true,
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
		},
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
	}
}

// Load reads the config file at path over the defaults. A missing file gives
// the defaults.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}

	// Sections decode over the defaults. Profiles decode one by one, so a file
	// that sets only profiles.claude.model keeps that profile's other defaults.
	file := struct {
		UI       *UI                       `toml:"ui"`
		Spawn    *Spawn                    `toml:"spawn"`
		Profiles map[string]toml.Primitive `toml:"profiles"`
		Repos    *Repos                    `toml:"repos"`
		Worktree *Worktree                 `toml:"worktree"`
		Tmux     *Tmux                     `toml:"tmux"`
		Journal  *Journal                  `toml:"journal"`
		Hold     *Hold                     `toml:"hold"`
	}{UI: &c.UI, Spawn: &c.Spawn, Repos: &c.Repos, Worktree: &c.Worktree, Tmux: &c.Tmux, Journal: &c.Journal, Hold: &c.Hold}
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
	if c.UI.MaxSubRows < 0 {
		bad("ui.max_sub_rows = %d: must not be negative", c.UI.MaxSubRows)
	}
	if c.UI.Ghostty != "auto" && c.UI.Ghostty != "off" {
		bad("ui.ghostty = %q: must be auto or off", c.UI.Ghostty)
	}
	if _, ok := c.Profiles[c.Spawn.DefaultProfile]; !ok {
		bad("spawn.default_profile = %q: no such profile", c.Spawn.DefaultProfile)
	}
	if c.Spawn.ReadyTimeout <= 0 {
		bad("spawn.ready_timeout = %v: must be positive", c.Spawn.ReadyTimeout)
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
	return errors.Join(errs...)
}

// Write prints c as TOML.
func (c Config) Write(w io.Writer) error {
	return toml.NewEncoder(w).Encode(c)
}
