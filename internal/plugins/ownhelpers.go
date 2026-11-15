package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/brand"
)

// Helpers of your own (DESIGN §4.17, docs/proposals/plugin-panes.md §5):
// a manifest's [helpers.<id>] tables. A helper is a program of yours that
// Irie keeps running while its plugin is on, in a window of irie-agents,
// such as a listener that turns what you text yourself into tasks. It may
// keep a status file, which the helpers op passes to the Mac app's Helpers
// tab in the right sidebar. A manifest is data, so a manifest with a
// helper that runs a program is off until you turn its plugin on: one
// dropped in the folder, by an agent say, can't run anything by itself.

// helperFile is one of a manifest's [helpers.<id>] tables.
type helperFile struct {
	Title  string   `toml:"title"`
	Icon   string   `toml:"icon"`
	Run    []string `toml:"run"`
	Dir    string   `toml:"dir"`
	Status string   `toml:"status"`
	Open   string   `toml:"open"`
}

// Helper is a helper of yours, as its manifest names it.
type Helper struct {
	ID       string   // its plugin's name, a dot and its own, such as inbox.listen
	Plugin   string   // the plugin it comes with
	Title    string   // its name in the Helpers tab
	Icon     string   // an SF Symbols name; "" = the client's own
	Run      []string // the program Irie keeps running, and its arguments; none = it only shows a status
	Dir      string   // the folder it runs in
	Status   string   // the status file it keeps, which Irie reads; "" = none
	Open     string   // the file or folder its Open button opens; "" = none
	Manifest string   // the manifest it comes from
	// Problem says why it can't run, in a sentence: a table that doesn't
	// read. "" = it reads.
	Problem string
	On      bool // its plugin is on, as the config has it (Helpers)
}

// maxHelperArgs is the most words a helper's run may have.
const maxHelperArgs = 64

// homeDir finds the home folder that a helper's ~ stands for.
var homeDir = os.UserHomeDir

// helperPath reads one of a helper's paths: ~ for the home folder, and
// then absolute. "" stays "".
func helperPath(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		home, err := homeDir()
		if err != nil {
			return "", false
		}
		raw = filepath.Join(home, strings.TrimPrefix(raw, "~"))
	}
	if !filepath.IsAbs(raw) {
		return "", false
	}
	return filepath.Clean(raw), true
}

// helperPart is the part a [helpers.<id>] table makes.
func helperPart(p Plugin, id string, f helperFile) Part {
	title := line(f.Title, 40)
	if title == "" {
		title = id
	}
	h := &Helper{ID: p.Name + "." + id, Plugin: p.Name, Title: title, Manifest: p.Manifest}
	x := Part{Name: "helpers." + id, Title: title, Brings: []string{"a helper in the Mac app's Helpers tab"}, own: always, Helper: h}
	fails := func(why string) Part {
		h.Problem = why
		x.Summary = "A helper of yours, once its [helpers." + id + "] table reads."
		x.check = func(Env, bool) (bool, string) { return false, why }
		return x
	}
	if !manifestName.MatchString(id) {
		return fails("[helpers." + id + "] isn't a helper's name: lowercase letters, digits and single hyphens, such as [helpers.listen].")
	}
	if icon := strings.TrimSpace(f.Icon); symbolName.MatchString(icon) {
		h.Icon = icon
	}
	if len(f.Run) > maxHelperArgs {
		return fails("run has more than " + strconv.Itoa(maxHelperArgs) + " words.")
	}
	for i, w := range f.Run {
		if strings.ContainsAny(w, "\x00\n\r") {
			return fails("run's word " + strconv.Itoa(i+1) + " holds a line break or a NUL.")
		}
		if i == 0 {
			w = strings.TrimSpace(w)
			if w == "" {
				return fails("run starts with an empty word: its first is the program, such as run = [\"python3\", \"~/inbox/listen.py\"].")
			}
			if strings.Contains(w, "/") || strings.HasPrefix(w, "~") {
				abs, ok := helperPath(w)
				if !ok {
					return fails("run's program " + strconv.Quote(w) + " isn't a name on your PATH or a full path: name it as python3, or as /usr/bin/python3 or ~/bin/listen.")
				}
				w = abs
			}
		} else if strings.HasPrefix(w, "~/") {
			// A path to a file of yours, as the program takes it.
			w, _ = helperPath(w)
		}
		h.Run = append(h.Run, w)
	}
	var ok bool
	if h.Dir, ok = helperPath(f.Dir); !ok {
		return fails("dir = " + strconv.Quote(f.Dir) + " isn't a full path: name it as ~/inbox or /Users/alice/inbox.")
	}
	if h.Dir == "" {
		h.Dir, _ = homeDir()
	}
	if h.Status, ok = helperPath(f.Status); !ok {
		return fails("status = " + strconv.Quote(f.Status) + " isn't a full path to a file: name it as ~/.local/state/inbox/status.json.")
	}
	if h.Open, ok = helperPath(f.Open); !ok {
		return fails("open = " + strconv.Quote(f.Open) + " isn't a full path: name it as ~/Documents/Inbox.")
	}
	switch {
	case len(h.Run) > 0:
		x.Summary = "A helper of yours that " + brand.Name + " keeps running while this plugin is on: " + filepath.Base(h.Run[0]) + "."
	case h.Status != "":
		x.Summary = "A helper of yours that shows the status in " + h.Status + "."
	default:
		return fails("[helpers." + id + "] has nothing to do: run names the program to keep running, status the file it keeps.")
	}
	x.check = func(_ Env, on bool) (bool, string) { return checkHelper(*h, on) }
	return x
}

// checkHelper says whether a helper has what it needs: the program it
// runs. The core says whether it runs now (the helpers op).
func checkHelper(h Helper, on bool) (bool, string) {
	if len(h.Run) == 0 {
		return true, "It shows its status file's state in the Helpers tab."
	}
	if _, err := HelperProgram(h); err != nil {
		return false, err.Error()
	}
	if !on {
		return true, "Off: " + brand.Name + " starts " + filepath.Base(h.Run[0]) + " once the plugin is on."
	}
	return true, brand.Name + " keeps " + filepath.Base(h.Run[0]) + " running in a window of its own."
}

// HelperProgram is the program a helper runs, found as a shell would find
// it: a full path, or a name on the PATH.
func HelperProgram(h Helper) (string, error) {
	if len(h.Run) == 0 {
		return "", errNoProgram
	}
	path, err := exec.LookPath(h.Run[0])
	if err != nil {
		return "", helperError(strconv.Quote(h.Run[0]) + " isn't a program on this computer, so the helper can't start: install it, or give its full path in run.")
	}
	return path, nil
}

type helperError string

func (e helperError) Error() string { return string(e) }

const errNoProgram = helperError("The helper has no program to run.")

// Helpers are your helpers, from the manifests in the plugins folder, in
// the order of their files and then their names, each with whether its
// plugin is on. A manifest that doesn't read brings none.
func Helpers(e Env) []Helper {
	mine, _ := Mine(e.Paths.Plugins())
	var out []Helper
	for _, p := range mine {
		if p.problem != "" {
			continue
		}
		on := p.on(e.Config)
		for _, x := range p.Parts {
			if x.Helper == nil {
				continue
			}
			h := *x.Helper
			h.On = on
			h.Run = slices.Clone(h.Run)
			out = append(out, h)
		}
	}
	return out
}
