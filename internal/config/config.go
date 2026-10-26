// Package config loads Irie's TOML config (DESIGN §5.5). Every key is
// optional, and an unknown key is an error that names it. This version knows
// the ui, spawn, profiles and tmux sections.
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
	Tmux     Tmux               `toml:"tmux"`
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
}

type Tmux struct {
	Path   string `toml:"path"`   // "" = PATH, then the usual install dirs
	Socket string `toml:"socket"` // tmux -L name
}

// Harnesses lists the harness ids this version can launch.
var Harnesses = []string{"claude-code"}

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
		Tmux: Tmux{Socket: "irie"},
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
		Tmux     *Tmux                     `toml:"tmux"`
	}{UI: &c.UI, Spawn: &c.Spawn, Tmux: &c.Tmux}
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
	socketName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	tmuxKey    = regexp.MustCompile(`^[!-~]+$`) // printable, no spaces
)

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
		if h := c.Profiles[name].Harness; !slices.Contains(Harnesses, h) {
			bad("profiles.%s.harness = %q: must be one of %s", name, h, strings.Join(Harnesses, ", "))
		}
	}
	if !socketName.MatchString(c.Tmux.Socket) {
		bad("tmux.socket = %q: use letters, digits, '.', '_' and '-'", c.Tmux.Socket)
	}
	if c.Tmux.Path != "" && !filepath.IsAbs(c.Tmux.Path) {
		bad("tmux.path = %q: must be an absolute path", c.Tmux.Path)
	}
	return errors.Join(errs...)
}

// Write prints c as TOML.
func (c Config) Write(w io.Writer) error {
	return toml.NewEncoder(w).Encode(c)
}
