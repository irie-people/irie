package core

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/irie-hq/irie/internal/harness"
	"github.com/irie-hq/irie/internal/harness/claude"
	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/tmux"
)

func harnessFor(id string) (harness.Harness, error) {
	switch id {
	case "claude-code":
		return claude.Harness{}, nil
	}
	return nil, fmt.Errorf("no harness %q", id)
}

const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// newID returns a session id: "s" and 12 base32 characters, 44 bits of Unix
// milliseconds then 16 random bits, so ids sort by creation time.
func newID(t time.Time) string {
	var r [2]byte
	rand.Read(r[:])
	v := uint64(t.UnixMilli())&(1<<44-1)<<16 | uint64(binary.BigEndian.Uint16(r[:]))
	b := []byte("s000000000000")
	for i := 12; i >= 1; i-- {
		b[i] = crockford[v&31]
		v >>= 5
	}
	return string(b)
}

const maxSlug = 32

// slug names a session after the first words of its task's first line.
func slug(task string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	words := strings.FieldsFunc(strings.ToLower(first), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	out := ""
	for i, w := range words {
		next := w
		if out != "" {
			next = out + "-" + w
		}
		if len(next) > maxSlug {
			if out == "" {
				out = w[:maxSlug]
			}
			break
		}
		out = next
		if i == 4 {
			break
		}
	}
	if out == "" {
		return "task"
	}
	return out
}

func numbered(name string, n int) string {
	if n == 1 {
		return name
	}
	return name + "-" + strconv.Itoa(n)
}

// uniqueName returns name, or name-2, name-3 … when a session has it. The
// caller holds c.mu.
func (c *Core) uniqueName(name string) string {
	taken := map[string]bool{}
	for _, x := range c.sessions {
		taken[x.name] = true
	}
	for n := 1; ; n++ {
		if !taken[numbered(name, n)] {
			return numbered(name, n)
		}
	}
}

// workdir resolves where a task runs: the requested directory in place,
// else spawn.default_cwd, else a new scratch dir, so an untargeted task
// never runs in $HOME.
func (c *Core) workdir(requested, name string) (string, error) {
	dir := requested
	if dir == "" {
		dir = c.cfg.Spawn.DefaultCwd
	}
	if dir == "" {
		if err := os.MkdirAll(c.paths.Scratch(), 0o700); err != nil {
			return "", err
		}
		for n := 1; ; n++ {
			d := c.paths.ScratchDir(numbered(name, n))
			err := os.Mkdir(d, 0o700)
			if err == nil {
				return d, nil
			}
			if !errors.Is(err, fs.ErrExist) {
				return "", err
			}
		}
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir[1:], "/"))
	}
	if !filepath.IsAbs(dir) {
		return "", ipc.Errorf(ipc.CodeBadArgs, "%s is not an absolute path", dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", ipc.Errorf(ipc.CodeBadArgs, "no such directory: %s", dir)
	}
	return filepath.Clean(dir), nil
}

// spawn runs the spawn pipeline (DESIGN §4.2): the task never travels
// through tmux and is never typed into a TUI. It goes to the agent as an
// argument, or as a file when it is large.
func (c *Core) spawn(a ipc.SpawnArgs) (ipc.Session, error) {
	if strings.TrimSpace(a.Task) == "" {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "empty task")
	}
	profileName := c.cfg.Spawn.DefaultProfile
	profile := c.cfg.Profiles[profileName]
	h, err := harnessFor(profile.Harness)
	if err != nil {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "profile %s: %v", profileName, err)
	}

	c.mu.Lock()
	x, err := c.launch(a, profileName, h)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	if err != nil {
		c.log.Warn("spawn failed", "err", err)
		return ipc.Session{}, err
	}
	c.log.Info("spawned", "session", x.id, "name", x.name, "cwd", x.cwd)
	return x.wire(), nil
}

// launch writes the session's files and creates its window. The caller
// holds c.mu.
func (c *Core) launch(a ipc.SpawnArgs, profileName string, h harness.Harness) (*session, error) {
	if err := c.refresh(); err != nil {
		return nil, err
	}
	base := slug(a.Task)
	if a.Name != "" {
		base = slug(a.Name)
	}
	name := c.uniqueName(base)
	cwd, err := c.workdir(a.Cwd, name)
	if err != nil {
		return nil, err
	}
	id := newID(time.Now())
	for c.sessions[id] != nil {
		id = newID(time.Now())
	}
	x := &session{
		id: id, name: name, agent: "main", created: time.Now().UTC().Truncate(time.Millisecond),
		cwd: cwd, profile: profileName, vendorID: harness.NewUUID(),
	}

	dir := c.paths.Session(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	argv, env, files, err := h.Launch(c.cfg.Profiles[profileName], harness.Spawn{
		Session: id, Name: name, Task: a.Task, Cwd: cwd, Dir: dir, VendorID: x.vendorID,
	})
	if err != nil {
		return nil, err
	}
	for f, b := range files {
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o600); err != nil {
			return nil, err
		}
	}
	spec := c.paths.SpecFile(id)
	if err := host.WriteSpec(spec, host.Spec{Session: id, Profile: profileName, Argv: argv, Env: env, Cwd: cwd}); err != nil {
		return nil, err
	}

	// One command list: the window is born with its options and the slot's
	// size, so nothing ever sees it half made. Its temporary name is the
	// unique session id.
	w, ht := 80, 24
	if slot := c.snap.SlotPane(); slot != nil {
		w, ht = slot.Width, slot.Height
	}
	size := []string{"-x", strconv.Itoa(w), "-y", strconv.Itoa(ht)}
	target := tmux.AgentsSession + ":=" + id
	opt := func(k, v string) []string { return []string{"set-option", "-p", "-t", target, k, v} }
	out, err := c.tmux.RunList([][]string{
		{"set-option", "-t", tmux.AgentsSession, "default-size", fmt.Sprintf("%dx%d", w, ht)},
		{"new-window", "-d", "-t", tmux.AgentsSession + ":", "-n", id, "-c", cwd, "-P", "-F", "#{pane_id}", "--", c.bin, "host", "--spec", spec},
		opt(tmux.OptSession, id),
		opt(tmux.OptAgent, x.agent),
		opt(tmux.OptRole, tmux.RoleAgent),
		opt(tmux.OptName, name),
		opt(tmux.OptCreated, x.created.Format(time.RFC3339Nano)),
		opt(tmux.OptVendorID, x.vendorID),
		append([]string{"resize-window", "-t", target}, size...),
		{"rename-window", "-t", target, name},
	})
	if err != nil {
		c.tmux.Run("kill-window", "-t", target)
		return nil, err
	}
	x.pane = strings.TrimSpace(out)
	c.sessions[id] = x
	return x, nil
}
