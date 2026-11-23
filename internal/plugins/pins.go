package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/paths"
)

// A Pin is what a plugin of yours ran when you turned it on (DESIGN
// §4.17): its manifest, and each file in the home folder that one of its
// helpers or tools runs, its program or a script it hands one, each by path
// with the SHA-256 of its bytes. A helper or a tool runs only while what it
// names is what you turned it on with, so an edit of a script, by an agent
// say, waits for your word before it runs.
type Pin struct {
	Files map[string]string `json:"files"`
	At    string            `json:"at"`
}

// Pins are the pins the core keeps, by plugin name.
type Pins map[string]Pin

// pinsFile is where the core keeps them, in Irie's state folder.
func pinsFile(p paths.Paths) string { return filepath.Join(p.State, "plugin-pins.json") }

// maxPinned is the largest file a pin reads; a larger one is left out.
const maxPinned = 64 << 20

// ReadPins reads the pins the core keeps; ok is false when it keeps none
// yet, so nothing is held, or the file doesn't read.
func ReadPins(p paths.Paths) (Pins, bool) {
	b, err := os.ReadFile(pinsFile(p))
	if err != nil {
		return nil, false
	}
	var pins Pins
	if json.Unmarshal(b, &pins) != nil || pins == nil {
		return nil, false
	}
	return pins, true
}

// WritePins writes the pins the core keeps, whole.
func WritePins(p paths.Paths, pins Pins) error {
	b, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		return err
	}
	return filetx.WriteFile(pinsFile(p), append(b, '\n'), 0o600)
}

// SamePins reports whether two sets of pins say the same.
func SamePins(a, b Pins) bool {
	return maps.EqualFunc(a, b, func(x, y Pin) bool { return maps.Equal(x.Files, y.Files) })
}

// PinsNow are the pins of your plugins that run something, as their files
// stand: those named, or with none named, every one that is on.
func PinsNow(e Env, names ...string) Pins {
	mine, _ := Mine(e.Paths.Plugins())
	at := time.Now().UTC().Format(time.RFC3339)
	out := Pins{}
	for _, p := range mine {
		switch {
		case p.problem != "" || !p.code():
		case len(names) > 0 && !slices.Contains(names, p.Name):
		case len(names) == 0 && !p.on(e.Config):
		default:
			out[p.Name] = Pin{Files: Fingerprint(p), At: at}
		}
	}
	return out
}

// code says a plugin of yours runs something of yours: a helper's program,
// or a tool's server, which every agent's session starts.
func (p Plugin) code() bool {
	return p.runs || slices.ContainsFunc(p.Parts, func(x Part) bool { return x.Tools != nil })
}

// Fingerprint is what a plugin of yours runs, as it stands: its manifest,
// and each file in the home folder that a helper's or a tool's run names,
// its program, found on the PATH when it names no path, and its arguments
// that are files, a script say, from the helper's folder when they name no
// path of their own. A file outside the home folder, the system's or a
// package manager's, is left out, so that an update of python3 holds
// nothing.
func Fingerprint(p Plugin) map[string]string {
	home, _ := homeDir()
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		realHome = home
	}
	files := map[string]string{}
	add := func(path string) {
		path = filepath.Clean(path)
		real, err := filepath.EvalSymlinks(path)
		if err != nil || home == "" || !strings.HasPrefix(path, home+string(filepath.Separator)) || !strings.HasPrefix(real, realHome+string(filepath.Separator)) {
			return
		}
		fi, err := os.Stat(path)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxPinned {
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		sum := sha256.Sum256(b)
		files[path] = hex.EncodeToString(sum[:])
	}
	if p.Manifest != "" {
		add(p.Manifest)
	}
	for _, x := range p.Parts {
		var run []string
		dir := ""
		switch {
		case x.Helper != nil:
			run, dir = x.Helper.Run, x.Helper.Dir
		case x.Tools != nil:
			run = x.Tools.Run
		default:
			continue
		}
		d, _ := helperPath(dir)
		for i, w := range run {
			switch {
			case strings.HasPrefix(w, "~/"):
				if abs, ok := helperPath(w); ok {
					add(abs)
				}
			case filepath.IsAbs(w):
				add(w)
			case i == 0:
				if found, err := exec.LookPath(w); err == nil {
					add(found)
				}
			case d != "" && !strings.HasPrefix(w, "-"):
				add(filepath.Join(d, w))
			}
		}
	}
	return files
}

// pinHold says why a plugin of yours that is on may not run what it names,
// or "" when it may: the core keeps pins (started), and what it runs is not
// what you turned it on with, or it was turned on without your word, as a
// switch set by hand in config.toml is.
func (p Plugin) pinHold(pins Pins, started bool) string {
	if !started || p.problem != "" || !p.code() {
		return ""
	}
	again := "Turn the plugin off and on again in Settings › Plugins, or run " + brand.Slug + " plugins on " + p.Name + ", to run it."
	pin, ok := pins[p.Name]
	if !ok {
		return "It was turned on without your word, as by a switch set by hand, so " + brand.Name + " doesn't know what you meant it to run. " + again
	}
	now := Fingerprint(p)
	var changed []string
	for path, sum := range now {
		if pin.Files[path] != sum {
			changed = append(changed, homeShown(path))
		}
	}
	for path := range pin.Files {
		if _, ok := now[path]; !ok {
			changed = append(changed, homeShown(path))
		}
	}
	if len(changed) == 0 {
		return ""
	}
	slices.Sort(changed)
	if len(changed) > 3 {
		changed = append(changed[:3], fmt.Sprintf("%d more", len(changed)-3))
	}
	return "What it runs changed since you turned it on: " + strings.Join(changed, ", ") + ". " + brand.Name + " runs a changed program only at your word. " + strings.Replace(again, "to run it", "to run this version", 1)
}

// homeShown is path with the home folder as ~.
func homeShown(path string) string {
	if home, _ := homeDir(); home != "" && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

// holds reads the pins for e once: why each plugin of yours that is on is
// held, by name (pinHold).
func holds(e Env, mine []Plugin) map[string]string {
	pins, started := ReadPins(e.Paths)
	out := map[string]string{}
	for _, p := range mine {
		if p.on(e.Config) {
			if why := p.pinHold(pins, started); why != "" {
				out[p.Name] = why
			}
		}
	}
	return out
}
