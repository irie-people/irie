// Package dock reads the right sidebar's Dock (docs/proposals/right-dock.md
// §5.2): the controls a repo lists in .irie/dock.json, or in cmux's
// .cmux/dock.json when it has no Irie one, and the user's own. A control is
// a terminal that runs one command, or a web page. The file's shape is
// cmux's, as its public docs give it (https://cmux.com/docs/dock), so a cmux
// Dock works unchanged; Irie adds scope, which cmux has no use for.
package dock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Control types.
const (
	Terminal = "terminal"
	Browser  = "browser"
)

// Where a control runs, and so how many copies it has.
const (
	// ScopeSession runs a copy per session, in its folder: the default.
	ScopeSession = "session"
	// ScopeRepo runs one copy in the repo's own folder, such as a dev server.
	ScopeRepo = "repo"
	// ScopeGlobal runs one copy for every session.
	ScopeGlobal = "global"
)

// Where a file came from.
const (
	SourceRepo = "repo" // .irie/dock.json in the session's repo
	SourceCmux = "cmux" // .cmux/dock.json there, when it has no Irie one
	SourceMine = "mine" // the user's own, in Irie's config folder
)

// MaxControls is how many controls a file may list.
const MaxControls = 12

// maxFile bounds what is read of a file.
const maxFile = 256 << 10

// Control is one control, with cmux's fields.
type Control struct {
	ID      string            `json:"id"`
	Title   string            `json:"title,omitempty"`
	Command string            `json:"command,omitempty"`
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Chrome  *bool             `json:"chrome,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Height  float64           `json:"height,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Scope   string            `json:"scope,omitempty"`
}

// File is one Dock file, read.
type File struct {
	Path     string
	Source   string
	Hash     string // the SHA-256 of its bytes, in hex
	Controls []Control
	Problem  string // why it does not read, else ""
}

// Mine reports whether the file is the user's own, whose commands run with
// no asking.
func (f File) Mine() bool { return f.Source == SourceMine }

// Parse reads a Dock file's bytes: a JSON object with a controls array.
// Fields it does not know are skipped, as a newer cmux may add some.
func Parse(data []byte) ([]Control, error) {
	var f struct {
		Controls []Control `json:"controls"`
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&f); err != nil {
		return nil, fmt.Errorf("not a Dock file: %v", err)
	}
	if len(f.Controls) > MaxControls {
		return nil, fmt.Errorf("a Dock holds at most %d controls, and this one lists %d", MaxControls, len(f.Controls))
	}
	seen := map[string]bool{}
	out := make([]Control, 0, len(f.Controls))
	for i, c := range f.Controls {
		c.ID = strings.TrimSpace(c.ID)
		if !validID(c.ID) {
			return nil, fmt.Errorf("control %d: its id must be 1 to 40 letters, digits, - or _", i+1)
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("control %q is listed twice", c.ID)
		}
		seen[c.ID] = true
		if c.Type == "" {
			c.Type = Terminal
		}
		switch c.Type {
		case Terminal:
			if strings.TrimSpace(c.Command) == "" {
				return nil, fmt.Errorf("control %q: a terminal control needs a command", c.ID)
			}
		case Browser:
			if !webURL(c.URL) {
				return nil, fmt.Errorf("control %q: a browser control needs an http or https url", c.ID)
			}
		default:
			return nil, fmt.Errorf("control %q: its type must be terminal or browser", c.ID)
		}
		if c.Scope == "" {
			c.Scope = ScopeSession
		}
		if c.Scope != ScopeSession && c.Scope != ScopeRepo && c.Scope != ScopeGlobal {
			return nil, fmt.Errorf("control %q: its scope must be session, repo or global", c.ID)
		}
		if strings.TrimSpace(c.Title) == "" {
			c.Title = c.ID
		}
		if c.Height < 0 {
			c.Height = 0
		}
		for k := range c.Env {
			if k == "" || strings.ContainsAny(k, "= \t\n") {
				return nil, fmt.Errorf("control %q: %q is not an environment variable's name", c.ID, k)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// validID reports whether id can name a control and, inside a tmux
// session's name, its copies: tmux turns "." and ":" in a name into "_".
func validID(id string) bool {
	if id == "" || len(id) > 40 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func webURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Read reads the file at path, which came from source. A file that does not
// read is returned with its Problem, and no controls.
func Read(path, source string) File {
	f := File{Path: path, Source: source}
	data, err := readCapped(path)
	if err != nil {
		f.Problem = err.Error()
		return f
	}
	sum := sha256.Sum256(data)
	f.Hash = hex.EncodeToString(sum[:])
	f.Controls, err = Parse(data)
	if err != nil {
		f.Problem = err.Error()
	}
	return f
}

func readCapped(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxFile {
		return nil, fmt.Errorf("%s is larger than %d KB", filepath.Base(path), maxFile>>10)
	}
	return os.ReadFile(path)
}

// Find finds the repo's Dock file for a session working in dir: in dir, or
// the nearest folder above it up to top, the repo's own top folder, the
// first .irie/dock.json, or else .cmux/dock.json. Outside a repo, top is ""
// and only dir is looked in. It returns "" when there is none.
func Find(dir, top string) (path, source string) {
	dir = filepath.Clean(dir)
	if top != "" {
		top = filepath.Clean(top)
		if rel, err := filepath.Rel(top, dir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			top = ""
		}
	}
	for d := dir; ; d = filepath.Dir(d) {
		for _, c := range []struct{ rel, source string }{{".irie/dock.json", SourceRepo}, {".cmux/dock.json", SourceCmux}} {
			p := filepath.Join(d, c.rel)
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
				return p, c.source
			}
		}
		if top == "" || d == top || d == filepath.Dir(d) {
			return "", ""
		}
	}
}

// FindMine finds the user's own Dock, Irie's at mine, or "" when there is
// none. cmux's own personal file is not read: it holds what the user wanted
// beside cmux's workspaces, which may name tools of another setup.
func FindMine(mine string) (path, source string) {
	if st, err := os.Stat(mine); err == nil && st.Mode().IsRegular() {
		return mine, SourceMine
	}
	return "", ""
}

// Key names a control in the Dock op: its source's kind and its id, since a
// repo's control and one of the user's own may share an id.
func Key(source, id string) string {
	if source == SourceMine {
		return "mine:" + id
	}
	return "repo:" + id
}

// Instance names the tmux session that runs a control's copy for session:
// one per session for ScopeSession, one per repo for ScopeRepo, and one in
// all for ScopeGlobal. repoKey names the repo, such as a hash of its path.
func Instance(c Control, source, session, repoKey string) string {
	kind := "r"
	if source == SourceMine {
		kind = "m"
	}
	switch c.Scope {
	case ScopeRepo:
		return "dock-" + kind + "-" + c.ID + "-repo-" + repoKey
	case ScopeGlobal:
		return "dock-" + kind + "-" + c.ID + "-global"
	}
	return "dock-" + kind + "-" + c.ID + "-" + session
}

// Cwd is where a control's command starts: base when it names no cwd; its
// cwd from relBase when that is relative, as cmux reads it (a repo file's
// from the session's or the repo's folder, the user's own file's from
// home); ~ is home.
func Cwd(c Control, base, relBase, home string) string {
	cwd := strings.TrimSpace(c.Cwd)
	switch {
	case cwd == "":
		return base
	case cwd == "~":
		return home
	case strings.HasPrefix(cwd, "~/"):
		return filepath.Join(home, cwd[2:])
	case filepath.IsAbs(cwd):
		return filepath.Clean(cwd)
	}
	return filepath.Join(relBase, cwd)
}
