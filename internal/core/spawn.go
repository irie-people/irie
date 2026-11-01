package core

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/harness/generic"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/route"
	"github.com/irie-people/irie/internal/tmux"
)

func harnessFor(id string) (harness.Harness, error) {
	switch id {
	case "claude-code":
		return claude.Harness{}, nil
	case "generic":
		return generic.Harness{}, nil
	case "tool":
		return toolHarness{}, nil
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

// slug names a session's folder and branch after the first words of its
// task's first line.
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

// names returns the names the sessions but one have. The caller holds c.mu.
func (c *Core) names(but *session) map[string]bool {
	taken := map[string]bool{}
	for _, x := range c.sessions {
		if x != but {
			taken[x.name] = true
		}
	}
	return taken
}

// unique returns name, or name-2, name-3 … when it is taken.
func unique(name string, taken map[string]bool) string {
	for n := 1; ; n++ {
		if !taken[numbered(name, n)] {
			return numbered(name, n)
		}
	}
}

// workdir resolves where a task runs: the requested directory in place,
// else spawn.default_cwd, else a new scratch dir named after base, so an
// untargeted task never runs in $HOME.
func (c *Core) workdir(requested, base string) (string, error) {
	dir := requested
	if dir == "" {
		dir = c.cfg().Spawn.DefaultCwd
	}
	if dir == "" {
		if err := os.MkdirAll(c.paths.Scratch(), 0o700); err != nil {
			return "", err
		}
		for n := 1; ; n++ {
			d := c.paths.ScratchDir(numbered(base, n))
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
// through tmux and is never typed into a TUI, unless a generic profile asks
// for it to be pasted. It goes to the agent as an argument, or as a file
// when it is large. Whether the agent then comes up, fails or stalls,
// observe and the hooks tell. named says whether a.Name, when there is one,
// is the user's (title.go), as a name a client sends is.
func (c *Core) spawn(a ipc.SpawnArgs, named bool) (ipc.Session, error) {
	if strings.TrimSpace(a.Task) == "" {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "empty task")
	}
	if a.Cwd != "" && a.Repo != "" {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "a task runs in a cwd or in a repo, not both")
	}
	profileName := a.Profile
	if profileName == "" {
		profileName = c.cfg().Spawn.DefaultProfile
	}
	// A task's class teaches the router whichever profile runs it; a task
	// carried on keeps the class of the one it continues.
	class, why := route.Classify(a.Task, c.cfg().Route.Words)
	if a.Continues != "" {
		c.mu.Lock()
		if o := c.outcomes[a.Continues]; o != nil && o.class != "" {
			class, why = o.class, "the class of the task it carries on"
		}
		c.mu.Unlock()
	}
	extra := map[string]any{"class": class, "task_hash": route.Fingerprint(a.Task)}
	if profileName == config.Auto {
		ch, err := c.autoRoute(a, class, why)
		if err != nil {
			c.log.Info("not routed", "err", err)
			return ipc.Session{}, err
		}
		profileName, extra["routed"], extra["route"] = ch.Profile, true, ch.Reason
	}
	for k, v := range map[string]string{"parent": a.Parent, "continues": a.Continues} {
		if v != "" {
			extra[k] = v
		}
	}
	profile, ok := c.profile(profileName)
	if !ok {
		return ipc.Session{}, ipc.Errorf(ipc.CodeNotFound, "no profile +%s; the profiles are %s, +%s to route by cost per success, and +%s<model> or +%s<model> for a local model", profileName, strings.Join(slices.Sorted(maps.Keys(c.cfg().Profiles)), ", "), config.Auto, localPrefix, llamaPrefix)
	}
	switch {
	case a.PermissionMode != "":
		profile.PermissionMode = a.PermissionMode
	case a.Delegate:
		profile.PermissionMode = c.cfg().Route.DelegateMode
	}
	if a.Model != "" {
		if profile.Harness != "claude-code" || profile.Local != nil {
			return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "+%s takes no model for one task: pick a Claude Code profile, or +%s<model> or +%s<model> for a local model", profileName, localPrefix, llamaPrefix)
		}
		profile.Model = a.Model
	}
	// A local profile's model is checked, and may be loaded, before
	// anything else; like probing, it happens before taking the lock.
	var warnings []string
	if profile.Local != nil {
		var err error
		if warnings, err = c.prepareLocal(profileName, &profile, a.Force); err != nil {
			c.log.Warn("spawn refused", "profile", profileName, "err", err)
			return ipc.Session{}, err
		}
	}
	return c.spawnProfile(a, named, profileName, profile, warnings, extra, false)
}

// spawnProfile spawns a on profile, which is ready to run: routed, its
// local model checked. extra goes into spawn.requested. withSkills hands the
// session Irie's own skills, in their plugin folder (guide.go).
func (c *Core) spawnProfile(a ipc.SpawnArgs, named bool, profileName string, profile config.Profile, warnings []string, extra map[string]any, withSkills bool) (ipc.Session, error) {
	h, err := harnessFor(profile.Harness)
	if err != nil {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "profile %s: %v", profileName, err)
	}
	if m := c.readMachine(); m.Total > 0 && m.Available < localmodel.Reserve(m) {
		warnings = append(warnings, fmt.Sprintf("memory is low: %s free of %s", localmodel.Bytes(m.Available), localmodel.Bytes(m.Total)))
	}
	// Probing runs the CLI, so it happens before taking the lock.
	var probe harness.Probe
	if p, ok := h.(harness.Prober); ok {
		probe = p.Probe(profile, c.agentEnv(profile))
	}
	probe.Warnings = append(warnings, probe.Warnings...)
	// The plugin folder is files on disk, made before taking the lock.
	var plugin string
	if withSkills {
		var err error
		if plugin, err = c.pluginDir(); err != nil {
			return ipc.Session{}, err
		}
	}

	// Spawns run one at a time, so a worktree's slug cannot race another
	// spawn's name, and one that a promoted session delegates goes first.
	// Making a worktree runs git, which can take a while, so c.mu stays free
	// meanwhile for hooks and the sidebar.
	c.spawnMu.LockAt(c.levelOf(a.Parent, a.Continues))
	defer c.spawnMu.Unlock()
	// A name given is shown as given; else the task's title names the
	// session until Claude Code titles it. The slug names its folder.
	name, base := cleanText(a.Name, maxName), slug(a.Name)
	if name == "" {
		name, named, base = title(a.Task), false, slug(a.Task)
	}
	pl, err := c.place(a, base)
	if err != nil {
		c.log.Warn("spawn failed", "err", err)
		return ipc.Session{}, err
	}
	c.mu.Lock()
	pl.name, pl.named = unique(name, c.names(nil)), named
	if hash, _ := extra["task_hash"].(string); hash != "" {
		class, _ := extra["class"].(string)
		c.escalate(hash, class, profileName, time.Now())
	}
	x, err := c.launch(job{place: pl, profile: profileName, run: profile, h: h, probe: probe, task: a.Task, data: extra, plugin: plugin})
	if err == nil && pl.repo != "" {
		c.addRecent(pl.repo)
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	if err != nil {
		c.log.Warn("spawn failed", "err", err)
		return ipc.Session{}, err
	}
	c.log.Info("spawned", "session", x.id, "name", x.name, "cwd", x.cwd, "worktree", x.worktree)
	return x.wire(), nil
}

// agentEnv is, as near as the core can tell, the environment an agent of
// profile p starts with: the core's own without tmux's variables and those
// that tie it to another session or terminal pane, and the profile's.
func (c *Core) agentEnv(p config.Profile) []string {
	env := host.WithoutIdentity(tmux.WithoutEnv(os.Environ(), "TMUX", "TMUX_PANE"))
	for _, k := range slices.Sorted(maps.Keys(p.Env)) {
		env = append(env, k+"="+p.Env[k])
	}
	return env
}

// job is what launch starts: a new task in the place that place found, or
// a conversation that carries on in a place of its own.
type job struct {
	place
	profile string         // the profile's name
	run     config.Profile // the profile as it runs: routed, its local model checked
	h       harness.Harness
	probe   harness.Probe
	task    string         // the task; empty when the job resumes
	resume  string         // the vendor id of the conversation to carry on
	data    map[string]any // more data for spawn.requested
	reopen  *closedRecord  // the closed session that comes back, with its id and place in the list
	plugin  string         // Irie's plugin folder with the skills it gets; "" for none
}

// launch writes the session's files, creates its window and records the
// spawn. The caller holds c.mu.
func (c *Core) launch(j job) (*session, error) {
	if err := c.refresh(); err != nil {
		return nil, err
	}
	id, created := newID(time.Now()), time.Now().UTC().Truncate(time.Millisecond)
	for c.sessions[id] != nil {
		id = newID(time.Now())
	}
	if j.reopen != nil {
		id, created = j.reopen.ID, j.reopen.Created
	}
	// A resumed conversation keeps its id; a new one gets its id from Irie.
	vendorID := j.resume
	if vendorID == "" {
		vendorID = harness.NewUUID()
	}
	pl, h, profile := j.place, j.h, j.run
	x := &session{
		id: id, name: pl.name, named: pl.named, agent: "main", created: created,
		cwd: pl.cwd, repo: pl.repo, worktree: pl.worktree, branch: pl.branch, copied: pl.copied,
		profile: j.profile, harness: h.ID(), observed: h.Levels().Has(harness.Observed), paste: profile.Task == config.TaskPaste,
		vendorID: vendorID, warnings: append(slices.Clone(pl.warnings), j.probe.Warnings...),
		model: profile.Model, local: profile.Local != nil, out: c.outcome(id),
	}

	dir := c.paths.Session(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// A reopened session starts unfrozen (freeze.go), with no jots
	// waiting for it from before it closed (follow.go).
	os.Remove(c.paths.FreezeFile(id))
	os.Remove(c.paths.FollowUpFile(id))
	s := harness.Spawn{
		Session: id, Task: j.task, Cwd: x.cwd, Repo: x.repo, Dir: dir, VendorID: x.vendorID,
		Bin: c.bin, HoldMax: c.cfg().Hold.Max, Probe: j.probe,
		StatusLine: c.cfg().Usage.StatusLine, Home: home(), Environ: c.agentEnv(profile),
		PluginDir: j.plugin,
	}
	// Only the user's name goes to the agent: Claude Code titles a session
	// it gets no name for, and the row follows the title (title.go).
	if x.named {
		s.Name = x.name
	}
	// Agent teams need the session's anchor on the team server first; a
	// session whose anchor cannot be made starts without them.
	var anchor teamAnchor
	if c.teams(h.ID()) {
		a, err := c.anchor(id)
		if err != nil {
			c.log.Warn("teams", "session", id, "err", err)
		} else {
			anchor, s.Teams = a, true
		}
	}
	launch := h.Launch
	if j.resume != "" {
		r, ok := h.(harness.Resumer)
		if !ok {
			return nil, fmt.Errorf("%s cannot carry a conversation on", h.ID())
		}
		launch = r.Resume
	}
	argv, env, files, err := launch(profile, s)
	if err != nil {
		return nil, err
	}
	for f, b := range files {
		if err := filetx.WriteFile(filepath.Join(dir, f), b, 0o600); err != nil {
			return nil, err
		}
	}
	// Irie's own variables let the agent's hooks find the core. tmux's are
	// removed, so an agent's own tmux commands cannot reach Irie's server.
	if env == nil {
		env = map[string]string{}
	}
	env["IRIE_SESSION"], env["IRIE_SOCKET"], env["IRIE_BIN"] = id, c.paths.Socket(), c.bin
	var unset []string
	if c.cfg().Spawn.HideTmux {
		unset = []string{"TMUX", "TMUX_PANE"}
	}
	// With teams, TMUX names the team server instead.
	var path []string
	if s.Teams {
		bin, err := c.armTeam(id, dir, anchor, profile, env)
		if err != nil {
			return nil, err
		}
		path = []string{bin}
	}
	spec := c.paths.SpecFile(id)
	if err := host.WriteSpec(spec, host.Spec{
		Session: id, Profile: j.profile, Harness: h.ID(), Argv: argv, Env: env, Unset: unset, Path: path, Cwd: x.cwd, Warnings: x.warnings,
		Repo: x.repo, Worktree: x.worktree, Branch: x.branch, Copied: x.copied, Paste: x.paste,
		Model: x.model, Local: x.local, Skills: j.plugin != "",
	}); err != nil {
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
		{"new-window", "-d", "-t", tmux.AgentsSession + ":", "-n", id, "-c", x.cwd, "-P", "-F", "#{pane_id}", "--", c.bin, "host", "--spec", spec},
		opt(tmux.OptSession, id),
		opt(tmux.OptAgent, x.agent),
		opt(tmux.OptRole, tmux.RoleAgent),
		opt(tmux.OptName, x.name),
		opt(tmux.OptNamed, flag(x.named)),
		opt(tmux.OptCreated, x.created.Format(time.RFC3339Nano)),
		opt(tmux.OptVendorID, x.vendorID),
		append([]string{"resize-window", "-t", target}, size...),
		// A name is any text, such as a title, which tmux would expand as
		// a format.
		{"rename-window", "-t", target, "--", tmux.Literal(x.name)},
	})
	if err != nil {
		c.tmux.Run("kill-window", "-t", target)
		return nil, err
	}
	x.pane = strings.TrimSpace(out)
	c.sessions[id] = x
	data := map[string]any{"cwd": x.cwd, "profile": j.profile}
	if j.resume == "" {
		data["task_bytes"] = len(j.task)
	}
	for k, v := range map[string]string{"repo": x.repo, "worktree": x.worktree, "branch": x.branch} {
		if v != "" {
			data[k] = v
		}
	}
	maps.Copy(data, j.data)
	c.record(x, event.Event{Source: event.SourceIrie, Kind: event.SpawnRequested, Data: data})
	// A tool takes no task, so it is at work from the start, and one that
	// finishes at once has exited rather than failed to start.
	if h.ID() == "tool" {
		c.record(x, event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.PromptSubmitted, Data: map[string]any{"chars": 0}})
	}
	return x, nil
}
