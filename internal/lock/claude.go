package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// claudeConfig reads what Claude Code reads of the user's own setup as a
// session starts (code.claude.com/docs: mcp, settings, claude-directory):
// the MCP servers in ~/.claude.json, for every folder and for the
// session's own, and in the project's .mcp.json, and the hooks of the
// user's settings.json and the project's. With CLAUDE_CONFIG_DIR set,
// ~/.claude and ~/.claude.json are in that folder.
type claudeConfig struct {
	home, dir, cwd string
}

// configMost is the most of a config file a lock reads: ~/.claude.json
// holds every project's state, and can grow large.
const configMost = 64 << 20

// claudeJSON is Claude Code's own config file: ~/.claude.json, or
// .claude.json in CLAUDE_CONFIG_DIR.
func (c claudeConfig) claudeJSON() string {
	if c.dir != "" && c.dir != filepath.Join(c.home, ".claude") {
		return filepath.Join(c.dir, ".claude.json")
	}
	return filepath.Join(c.home, ".claude.json")
}

// serverJSON is one entry of an mcpServers object. Its env and headers,
// which hold secrets, are never read.
type serverJSON struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	URL     string   `json:"url"`
}

// servers are the user's own MCP servers that a session in c.cwd loads,
// each name once as Claude Code ranks the scopes: the folder's own (local)
// first, then the project's .mcp.json, then those for every folder (user).
// A project's server waits on the user's approval, which the lock cannot
// see, so it is listed as it may load.
func (c claudeConfig) servers() ([]Server, []string) {
	var notes []string
	var top struct {
		MCPServers map[string]serverJSON `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]serverJSON `json:"mcpServers"`
		} `json:"projects"`
	}
	if b, err := readSmall(c.claudeJSON()); err != nil {
		notes = append(notes, err.Error())
	} else if b != nil && json.Unmarshal(b, &top) != nil {
		notes = append(notes, "Claude Code's config, "+c.tilde(c.claudeJSON())+", didn't read as JSON")
	}
	var project struct {
		MCPServers map[string]serverJSON `json:"mcpServers"`
	}
	if c.cwd != "" {
		if b, err := readSmall(filepath.Join(c.cwd, ".mcp.json")); err != nil {
			notes = append(notes, err.Error())
		} else if b != nil && json.Unmarshal(b, &project) != nil {
			notes = append(notes, "the project's .mcp.json didn't read as JSON")
		}
	}
	var out []Server
	seen := map[string]bool{}
	add := func(from string, m map[string]serverJSON) {
		for _, name := range sortedKeys(m) {
			if seen[name] {
				continue
			}
			seen[name] = true
			s := m[name]
			out = append(out, Server{Name: name, From: from, Type: s.Type, Command: s.Command, Args: s.Args, URL: s.URL})
		}
	}
	if p, ok := top.Projects[c.cwd]; ok {
		add(ipc.LockFromLocal, p.MCPServers)
	} else if real, err := filepath.EvalSymlinks(c.cwd); err == nil {
		add(ipc.LockFromLocal, top.Projects[real].MCPServers)
	}
	add(ipc.LockFromProject, project.MCPServers)
	add(ipc.LockFromUser, top.MCPServers)
	return out, notes
}

// irieSkills are the skills of Irie's that a session gets, in the plugin
// folder its command line passes, each as /irie:<name> with Irie's
// version and the hash of its files.
func irieSkills(plugin, version string) ([]ipc.LockPart, []string) {
	if plugin == "" {
		return nil, nil
	}
	dir := filepath.Join(plugin, "skills")
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("%s's skills didn't read: %v", brand.Name, err)}
	}
	var out []ipc.LockPart
	var notes []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") || !e.IsDir() {
			continue
		}
		h, err := DirHash(filepath.Join(dir, e.Name()))
		if err != nil {
			notes = append(notes, fmt.Sprintf("skill %s didn't hash: %v", e.Name(), err))
		}
		out = append(out, ipc.LockPart{Name: brand.Slug + ":" + e.Name(), From: ipc.LockFromIrie, Version: version, Hash: h})
	}
	return out, notes
}

// hooks are the user's own hooks and the project's, from their settings
// files, each handler as its event and matcher, its program, and the hash
// of its whole command, whose other words may hold a secret.
func (c claudeConfig) hooks() ([]ipc.LockPart, []string) {
	var out []ipc.LockPart
	var notes []string
	var files []struct{ path, from string }
	if c.dir != "" {
		files = append(files, struct{ path, from string }{filepath.Join(c.dir, "settings.json"), ipc.LockFromUser})
	}
	if c.cwd != "" {
		files = append(files, struct{ path, from string }{filepath.Join(c.cwd, ".claude", "settings.json"), ipc.LockFromProject})
	}
	for _, f := range files {
		b, err := readSmall(f.path)
		if err != nil {
			notes = append(notes, err.Error())
			continue
		}
		if b == nil {
			continue
		}
		var s struct {
			Hooks map[string][]struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
					URL     string `json:"url"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if json.Unmarshal(b, &s) != nil {
			notes = append(notes, c.tilde(f.path)+" didn't read as JSON")
			continue
		}
		for _, event := range sortedKeys(s.Hooks) {
			for _, g := range s.Hooks[event] {
				for _, h := range g.Hooks {
					name := event
					if g.Matcher != "" && g.Matcher != "*" {
						name += " " + g.Matcher
					}
					part := ipc.LockPart{Name: name, From: f.from}
					switch {
					case h.Type == "command" || h.Type == "" && h.Command != "":
						prog, _ := firstWord(strings.TrimSpace(h.Command))
						part.Command = filepath.Base(prog)
						part.Hash = Hash([]byte(event + "\x00" + g.Matcher + "\x00" + h.Command))
					case h.URL != "":
						part.Command = h.Type + " " + site(h.URL)
						part.Hash = Hash([]byte(event + "\x00" + g.Matcher + "\x00" + h.URL))
					default:
						part.Command = h.Type
					}
					out = append(out, part)
				}
			}
		}
	}
	return out, notes
}

// readSmall reads a config file: nil and no error when there is none.
func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s didn't read: %v", path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%s didn't read: %v", path, err)
	}
	if fi.Size() > configMost {
		return nil, fmt.Errorf("%s is over %d MiB, so it was not read", path, configMost>>20)
	}
	b := make([]byte, fi.Size())
	if _, err := f.ReadAt(b, 0); err != nil && fi.Size() > 0 {
		return nil, fmt.Errorf("%s didn't read: %v", path, err)
	}
	return b, nil
}

// tilde writes a path under the home folder with ~.
func (c claudeConfig) tilde(p string) string {
	if c.home != "" {
		if rest, ok := strings.CutPrefix(p, c.home+string(filepath.Separator)); ok {
			return "~/" + rest
		}
	}
	return p
}

// firstWord is a command's program, after any VAR=value words; one in
// quotes keeps its spaces.
func firstWord(cmd string) (string, string) {
	for {
		var word, rest string
		switch {
		case cmd == "":
			return "", ""
		case cmd[0] == '"' || cmd[0] == '\'':
			if end := strings.IndexByte(cmd[1:], cmd[0]); end >= 0 {
				word, rest = cmd[1:end+1], cmd[end+2:]
				break
			}
			word = cmd
		default:
			if i := strings.IndexAny(cmd, " \t"); i >= 0 {
				word, rest = cmd[:i], cmd[i:]
			} else {
				word = cmd
			}
		}
		if k, _, ok := strings.Cut(word, "="); ok && k != "" && !strings.ContainsAny(k, `/"'$`) {
			cmd = strings.TrimSpace(rest)
			continue
		}
		return word, rest
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
