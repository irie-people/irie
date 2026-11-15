// Package lock writes and checks agent locks (DESIGN §4.21). A lock is one
// session's setup as its agent started, as a lock file records exact
// versions: its harness and the harness's version, its profile, model and
// permission mode, its MCP servers with their packages, Irie's plugins,
// its skills and its hooks, each plugin and skill with a hash of its files.
// A check asks OSV.dev, only when asked, about the harness and package
// versions the locks name, and keeps what it found, which marks the
// sessions that run a version with a known advisory. The idea comes from a
// talk-to-camera video by lizthedeveloper (2026-09-19), offered freely:
// CVEs cover packages, but nothing records an agent's whole setup to check.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
)

// File is a session's lock, in its folder.
const File = "lock.json"

// The harnesses Irie runs, by the id their adapters give.
const (
	harnessClaude = "claude-code"
)

// Server is an MCP server as a session's config names it: a program it
// starts, or an address it calls.
type Server struct {
	Name    string
	From    string // ipc.LockFromIrie and the rest
	Type    string // stdio, http, sse or ws; "" = stdio
	Command string
	Args    []string
	URL     string
}

// Plugin is one of Irie's plugins that is on (DESIGN §4.17). Manifest is
// the file one of your own comes from; "" for one of Irie's, which is code
// in the irie binary.
type Plugin struct {
	Name     string
	Manifest string
}

// Input is what the core knows of a session as its agent starts.
type Input struct {
	Session string
	At      string // ipc.LockSpawn and the rest
	Now     time.Time

	Irie    string // Irie's version
	IrieBin string // the irie binary, whose hash Irie's own parts carry

	Harness string // the adapter's id: claude-code, generic or tool
	Binary  string // the CLI's binary, as its PATH finds it; "" = not found
	Version string // the CLI's version, as its probe read it; "" = unknown
	Profile string
	Model   string
	Argv    []string // its command line: its permission mode and Irie's skills

	Settings []byte   // Irie's settings file for it, whose hooks are Irie's
	MCP      []Server // Irie's own MCP servers for it
	Strict   bool     // it loads Irie's servers and no other (--strict-mcp-config)
	Plugins  []Plugin

	Home      string // the user's home
	ClaudeDir string // Claude Code's config folder: ~/.claude, or CLAUDE_CONFIG_DIR
	Cwd       string
}

// Gather makes the lock of the session in, reading what its agent reads as
// it starts: Claude Code's config for the user's own MCP servers and
// hooks, and Irie's plugin folder for Irie's skills.
func Gather(in Input) ipc.Lock {
	l := ipc.Lock{
		Session: in.Session, Written: in.Now.UTC().Truncate(time.Millisecond), At: in.At,
		Profile: in.Profile, Model: in.Model, PermissionMode: permissionMode(in.Argv),
	}
	irieHash, err := FileHash(in.IrieBin)
	if err != nil && in.IrieBin != "" {
		l.Notes = append(l.Notes, fmt.Sprintf("%s's binary didn't read: %v", brand.Name, err))
	}
	l.Irie = ipc.LockPart{Name: brand.Slug, From: ipc.LockFromIrie, Version: in.Irie, Command: in.IrieBin, Hash: irieHash}
	l.Harness = harnessPart(in)
	for _, p := range in.Plugins {
		part := ipc.LockPart{Name: p.Name, From: ipc.LockFromIrie, Version: in.Irie, Hash: irieHash}
		if p.Manifest != "" {
			part = ipc.LockPart{Name: p.Name, From: ipc.LockFromUser, Command: p.Manifest}
			if part.Hash, err = FileHash(p.Manifest); err != nil {
				l.Notes = append(l.Notes, fmt.Sprintf("plugin %s's manifest didn't read: %v", p.Name, err))
			}
		}
		l.Plugins = append(l.Plugins, part)
	}
	for _, s := range in.MCP {
		l.MCP = append(l.MCP, serverPart(s, in))
	}
	if in.Harness != harnessClaude {
		return l
	}
	c := claudeConfig{home: in.Home, dir: in.ClaudeDir, cwd: in.Cwd}
	if !in.Strict {
		servers, notes := c.servers()
		l.Notes = append(l.Notes, notes...)
		for _, s := range servers {
			if !slices.ContainsFunc(l.MCP, func(p ipc.LockPart) bool { return p.Name == s.Name }) {
				l.MCP = append(l.MCP, serverPart(s, in))
			}
		}
	}
	skills, notes := irieSkills(pluginDir(in.Argv), in.Irie)
	l.Skills, l.Notes = skills, append(l.Notes, notes...)
	if h, ok := irieHooks(in.Settings, in.Irie); ok {
		l.Hooks = append(l.Hooks, h)
	}
	hooks, notes := c.hooks()
	l.Hooks, l.Notes = append(l.Hooks, hooks...), append(l.Notes, notes...)
	return l
}

// HarnessName is what a harness is called in words.
func HarnessName(id string) string {
	switch id {
	case harnessClaude:
		return "Claude Code"
	case "generic":
		return "a CLI"
	case "tool":
		return "a tool"
	}
	return id
}

// harnessPart is the lock's harness: its id, its version, its binary
// with any link followed, and for Claude Code its npm package.
func harnessPart(in Input) ipc.LockPart {
	h := ipc.LockPart{Name: in.Harness, Version: in.Version, Command: in.Binary}
	if real, err := filepath.EvalSymlinks(in.Binary); err == nil && in.Binary != "" {
		h.Command = real
	}
	if in.Harness == harnessClaude && in.Version != "" {
		h.Package = &ipc.LockPackage{Ecosystem: NPM, Name: ClaudeCode, Version: in.Version}
	}
	return h
}

// serverPart is the lock's entry for an MCP server: Irie's own carry
// Irie's version; another's program and the package it runs, with the hash
// of its whole command, whose other words may hold a secret; an address
// only its scheme and host.
func serverPart(s Server, in Input) ipc.LockPart {
	p := ipc.LockPart{Name: s.Name, From: s.From}
	switch s.Type {
	case "http", "sse", "ws", "streamable-http":
		p.Command = s.Type + " " + site(s.URL)
		p.Hash = Hash([]byte(s.URL))
		return p
	}
	p.Hash = Hash([]byte(strings.Join(append([]string{s.Command}, s.Args...), "\x00")))
	if irieBinary(s.Command, in.IrieBin) {
		p.Version = in.Irie
		p.Command = strings.Join(append([]string{brand.Slug}, s.Args...), " ")
		return p
	}
	p.Command = filepath.Base(s.Command)
	pkg, written := Spec(s.Command, s.Args)
	if written != "" {
		p.Command += " " + written
	}
	if pkg != nil {
		p.Version, p.Package = pkg.Version, pkg
	}
	return p
}

// irieBinary says whether command is the irie binary.
func irieBinary(command, bin string) bool {
	return command != "" && (command == bin || filepath.Base(command) == brand.Slug)
}

// site is an address's scheme and host: its path and query may hold a
// secret.
func site(raw string) string {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return "an address"
	}
	host, _, _ := strings.Cut(rest, "/")
	if _, h, ok := strings.Cut(host, "@"); ok {
		host = h
	}
	host, _, _ = strings.Cut(host, "?")
	return scheme + "://" + host
}

// permissionMode is the mode a command line starts its agent in: its
// --permission-mode, or bypassPermissions for
// --dangerously-skip-permissions; "" = the CLI's default.
func permissionMode(argv []string) string {
	mode := ""
	for i, a := range argv {
		switch {
		case a == "--permission-mode" && i+1 < len(argv):
			mode = argv[i+1]
		case strings.HasPrefix(a, "--permission-mode="):
			mode = strings.TrimPrefix(a, "--permission-mode=")
		case a == "--dangerously-skip-permissions":
			return "bypassPermissions"
		}
	}
	return mode
}

// pluginDir is the folder of Irie's skills a command line passes with
// --plugin-dir; "" = none.
func pluginDir(argv []string) string {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--plugin-dir" {
			return argv[i+1]
		}
	}
	return ""
}

// irieHooks is the lock's entry for Irie's own hooks, from the settings
// file Irie writes for the session: the events they are for, and the hash
// of the whole table.
func irieHooks(settings []byte, version string) (ipc.LockPart, bool) {
	if len(settings) == 0 {
		return ipc.LockPart{}, false
	}
	var s struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if json.Unmarshal(settings, &s) != nil || len(s.Hooks) == 0 {
		return ipc.LockPart{}, false
	}
	b, _ := json.Marshal(s.Hooks) // map keys sort, so the hash is the table's
	events := make([]string, 0, len(s.Hooks))
	for e := range s.Hooks {
		events = append(events, e)
	}
	slices.Sort(events)
	return ipc.LockPart{Name: brand.Slug + " hook", From: ipc.LockFromIrie, Version: version, Command: brand.Slug + " hook", Hash: Hash(b), Events: events}, true
}

// Path is the lock of the session whose folder is dir.
func Path(dir string) string { return filepath.Join(dir, File) }

// Write puts l in dir, the session's folder, as one transaction.
func Write(dir string, l ipc.Lock) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return filetx.WriteFile(Path(dir), append(b, '\n'), 0o600)
}

// ErrNone says a session has no lock: it started before Irie wrote them.
var ErrNone = errors.New("no lock: it started before " + brand.Name + " wrote locks, and gets one when it is reopened, resumed or forked")

// Read is the lock in dir, the session's folder.
func Read(dir string) (ipc.Lock, error) {
	b, err := os.ReadFile(Path(dir))
	if errors.Is(err, os.ErrNotExist) {
		return ipc.Lock{}, ErrNone
	}
	if err != nil {
		return ipc.Lock{}, err
	}
	var l ipc.Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return ipc.Lock{}, fmt.Errorf("its lock didn't read: %v", err)
	}
	return l, nil
}

// Packages are what a check asks about for lock l: its harness's package
// and its servers' that name an exact version, each with what it is in
// the session.
func Packages(l ipc.Lock) []ipc.LockPackage {
	var out []ipc.LockPackage
	add := func(p *ipc.LockPackage, part string) {
		if p == nil || p.Name == "" || p.Version == "" {
			return
		}
		q := *p
		q.Part = part
		if !slices.ContainsFunc(out, func(o ipc.LockPackage) bool { return Key(o) == Key(q) }) {
			out = append(out, q)
		}
	}
	add(l.Harness.Package, HarnessName(l.Harness.Name))
	for _, s := range l.MCP {
		add(s.Package, "MCP server "+s.Name)
	}
	return out
}
