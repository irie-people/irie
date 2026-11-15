// Package variorum is Irie's way through Variorum's door for AI
// (docs/proposals/writers.md): the two of Variorum's setups that Irie keeps,
// the checks on the program they name, one call to the door over stdio for
// the Variorum tab, and `irie variorum door`, which a writing session runs
// as its own MCP server.
//
// Variorum is Lillian Duarte's writing app for the Mac. Its door is a
// standard MCP server, variorum-mcp inside Variorum.app, which speaks
// newline-delimited JSON-RPC 2.0 on stdio and takes its setup's key from
// its environment. Irie runs no code of Variorum's: it starts that program
// as any MCP client does.
package variorum

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/brand"
)

// The kinds of setup Irie keeps, one for each of Variorum's clients that it
// is: Claude Code's, for writing sessions, and Any MCP client's, for the
// Variorum tab.
const (
	Sessions = "sessions"
	Tab      = "tab"
)

// KeyVar is the variable a setup gives the door its key in.
const KeyVar = "VARIORUM_MCP_KEY"

// maxKey is the longest key the door speaks; a key Variorum makes is far
// shorter.
const maxKey = 256

// Site is where Variorum comes from.
const Site = "https://stargazing.tech/variorum"

// Setup is one of Variorum's setups as Irie keeps it: the door's program
// and the key that lets one client in.
type Setup struct {
	Command string `json:"command"`
	Key     string `json:"key"`
}

// String names the program and never the key, so that a setup printed or
// logged by mistake gives nothing away.
func (s Setup) String() string { return "variorum.Setup{" + s.Command + "}" }

// Where a writer copies a setup, and how Irie takes it.
const (
	copyWhere = "Variorum's Settings ▸ MCP ▸ Connect a client"
	connect   = "press Connect on the Variorum tab, or run `pbpaste | " + brand.Slug + " variorum connect`"
)

// The refusals of a setup that isn't one of Variorum's. None quotes the
// setup, so none can hold its key.
var (
	errEmpty  = errors.New("There's no setup to keep: copy one in " + copyWhere + " first.")
	errShape  = errors.New("That isn't a setup " + brand.Name + " keeps: in " + copyWhere + ", choose Claude Code or Any MCP client and copy its setup with the copy button.")
	errQuote  = errors.New("That setup ends inside a quote: copy it again with the copy button in " + copyWhere + ".")
	errJSON   = errors.New("That setup looks like JSON but doesn't read as JSON: copy it again with the copy button in " + copyWhere + ".")
	errNoKey  = errors.New("That setup has no key for Variorum's door: copy a fresh one in " + copyWhere + ".")
	errEnv    = errors.New("That setup gives Variorum's door more than its key, which " + brand.Name + " doesn't pass on: copy a fresh one in " + copyWhere + ".")
	errArgs   = errors.New("That setup gives Variorum's door arguments, and " + brand.Name + " runs it with none: copy a fresh one in " + copyWhere + ".")
	errHidden = errors.New("That's the setup as Variorum shows it, with its key hidden: copy it with the copy button instead.")
	errKey    = errors.New("That setup's key isn't one Variorum makes: copy a fresh one in " + copyWhere + ".")
)

// Parse reads a setup as Variorum copies it, and tells its kind by its
// shape: a `claude mcp add` line is Claude Code's (Sessions), a JSON
// mcpServers stanza Any MCP client's (Tab). It takes only the program and
// the key. It checks the shape and the key, not the program: Check does.
func Parse(text string) (kind string, s Setup, err error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return "", Setup{}, errEmpty
	case strings.HasPrefix(text, "{"):
		s, err = parseJSON(text)
		kind = Tab
	default:
		s, err = parseCommand(text)
		kind = Sessions
	}
	if err == nil {
		err = checkKey(s.Key)
	}
	if err != nil {
		return "", Setup{}, err
	}
	return kind, s, nil
}

// parseCommand reads Claude Code's setup:
//
//	claude mcp remove -s user variorum >/dev/null 2>&1; claude mcp add -s user variorum -e VARIORUM_MCP_KEY=KEY -- "PROGRAM"
//
// Any commands before the last must be `claude mcp remove`; the last must
// add one server, set only the key, and run the program after `--` with no
// arguments.
func parseCommand(text string) (Setup, error) {
	cmds, err := words(text)
	if err != nil {
		return Setup{}, err
	}
	if len(cmds) == 0 {
		return Setup{}, errShape
	}
	add := cmds[len(cmds)-1]
	for _, c := range cmds[:len(cmds)-1] {
		if !isCommand(c, "claude", "mcp", "remove") {
			return Setup{}, errShape
		}
	}
	if !isCommand(add, "claude", "mcp", "add") {
		return Setup{}, errShape
	}
	opts := add[3:]
	dash := slices.Index(opts, "--")
	switch {
	case dash < 0 || dash == len(opts)-1:
		return Setup{}, errShape
	case dash < len(opts)-2:
		return Setup{}, errArgs
	}
	program, opts := opts[dash+1], opts[:dash]
	var key string
	keys, names := 0, 0
	take := func(env string) error {
		name, value, ok := strings.Cut(env, "=")
		if !ok {
			return errShape
		}
		if name != KeyVar {
			return errEnv
		}
		key = value
		keys++
		return nil
	}
	for i := 0; i < len(opts); i++ {
		o := opts[i]
		switch {
		case o == "-s" || o == "--scope" || o == "-t" || o == "--transport":
			if i++; i == len(opts) {
				return Setup{}, errShape
			}
		case strings.HasPrefix(o, "--scope=") || strings.HasPrefix(o, "--transport="):
		case o == "-e" || o == "--env":
			// Claude Code's -e takes each KEY=VALUE that follows it.
			n := 0
			for i+1 < len(opts) && !strings.HasPrefix(opts[i+1], "-") && strings.Contains(opts[i+1], "=") {
				i++
				n++
				if err := take(opts[i]); err != nil {
					return Setup{}, err
				}
			}
			if n == 0 {
				return Setup{}, errShape
			}
		case strings.HasPrefix(o, "--env="):
			if err := take(strings.TrimPrefix(o, "--env=")); err != nil {
				return Setup{}, err
			}
		case strings.HasPrefix(o, "-"):
			return Setup{}, errShape
		default:
			names++ // the server's name
		}
	}
	switch {
	case names != 1:
		return Setup{}, errShape
	case keys == 0:
		return Setup{}, errNoKey
	case keys > 1:
		return Setup{}, errShape
	}
	return Setup{Command: program, Key: key}, nil
}

// isCommand says whether the words of cmd start with want.
func isCommand(cmd []string, want ...string) bool {
	return len(cmd) >= len(want) && slices.Equal(cmd[:len(want)], want)
}

// words splits a line of shell into its commands, each a list of words, as
// a POSIX shell reads them but without expanding anything: quotes and
// backslashes go, and an unquoted ; or newline ends a command. It knows
// nothing more of the shell: $, > and & are letters like any other, and
// nothing it reads ever runs.
func words(line string) ([][]string, error) {
	var cmds [][]string
	var cmd []string
	var w strings.Builder
	in := false // within a word
	endWord := func() {
		if in {
			cmd = append(cmd, w.String())
			w.Reset()
			in = false
		}
	}
	endCommand := func() {
		endWord()
		if len(cmd) > 0 {
			cmds = append(cmds, cmd)
		}
		cmd = nil
	}
	for i := 0; i < len(line); i++ {
		switch ch := line[i]; ch {
		case '\'':
			j := strings.IndexByte(line[i+1:], '\'')
			if j < 0 {
				return nil, errQuote
			}
			w.WriteString(line[i+1 : i+1+j])
			in, i = true, i+1+j
		case '"':
			in = true
			for i++; ; i++ {
				if i == len(line) {
					return nil, errQuote
				}
				if line[i] == '"' {
					break
				}
				// Within double quotes a backslash quotes only these.
				if line[i] == '\\' && i+1 < len(line) && strings.IndexByte("$`\"\\\n", line[i+1]) >= 0 {
					i++
					if line[i] == '\n' {
						continue
					}
				}
				w.WriteByte(line[i])
			}
		case '\\':
			if i+1 == len(line) {
				return nil, errQuote
			}
			i++
			if line[i] != '\n' {
				w.WriteByte(line[i])
				in = true
			}
		case ' ', '\t', '\r':
			endWord()
		case ';', '\n':
			endCommand()
		default:
			w.WriteByte(ch)
			in = true
		}
	}
	endCommand()
	return cmds, nil
}

// parseJSON reads Any MCP client's setup, an mcpServers stanza:
//
//	{"mcpServers": {"variorum": {"command": "PROGRAM", "args": [], "env": {"VARIORUM_MCP_KEY": "KEY"}}}}
//
// or the object inside it, or the server's own.
func parseJSON(text string) (Setup, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &top); err != nil {
		return Setup{}, errJSON
	}
	server := json.RawMessage(text)
	if _, ok := top["command"]; !ok {
		servers := top
		if raw, ok := top["mcpServers"]; ok {
			if err := json.Unmarshal(raw, &servers); err != nil {
				return Setup{}, errShape
			}
		}
		var found bool
		if server, found = pick(servers); !found {
			return Setup{}, errShape
		}
	}
	var s struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		Type    string            `json:"type"`
	}
	if err := json.Unmarshal(server, &s); err != nil || s.Command == "" || s.Type != "" && s.Type != "stdio" {
		return Setup{}, errShape
	}
	if len(s.Args) > 0 {
		return Setup{}, errArgs
	}
	key, ok := s.Env[KeyVar]
	switch {
	case !ok:
		return Setup{}, errNoKey
	case len(s.Env) > 1:
		return Setup{}, errEnv
	}
	return Setup{Command: s.Command, Key: key}, nil
}

// pick is the server of an mcpServers object: its only one, else the one
// Variorum names, variorum or, for its debug build, variorum-dev.
func pick(servers map[string]json.RawMessage) (json.RawMessage, bool) {
	if len(servers) == 1 {
		for _, s := range servers {
			return s, true
		}
	}
	for _, name := range []string{"variorum", "variorum-dev"} {
		if s, ok := servers[name]; ok {
			return s, true
		}
	}
	return nil, false
}

// checkKey refuses a key the door would not speak: one that is empty, too
// long, or holds anything but printable ASCII without spaces.
func checkKey(key string) error {
	switch {
	case key == "":
		return errNoKey
	case strings.Contains(key, "•"):
		return errHidden
	case len(key) > maxKey:
		return errKey
	}
	for i := 0; i < len(key); i++ {
		if key[i] <= ' ' || key[i] >= 0x7f {
			return errKey
		}
	}
	return nil
}
