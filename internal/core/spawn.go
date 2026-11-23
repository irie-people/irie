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

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/harness/generic"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/learn"
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

// newScratch makes a new scratch folder named after base: the first free
// of base, base-2 and so on.
func (c *Core) newScratch(base string) (string, error) {
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

// workdir resolves where a task runs: the requested directory in place,
// else spawn.default_cwd, else a new scratch dir named after base, so an
// untargeted task never runs in $HOME.
func (c *Core) workdir(requested, base string) (string, error) {
	dir := requested
	if dir == "" {
		dir = c.cfg().Spawn.DefaultCwd
	}
	if dir == "" {
		return c.newScratch(base)
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
	// Below the disk's floor the swap may not grow, so a new agent waits
	// until there is room (DESIGN §2.9); force starts it all the same.
	if why := c.roomNow().HeavyWaits(); why != "" && !a.Force {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBusy, "not started: %s", why)
	}
	profileName := a.Profile
	if profileName == "" {
		profileName = c.cfg().Spawn.DefaultProfile
	}
	// A task's class teaches the router whichever profile runs it; a task
	// carried on keeps the class of the one it continues, and a subtask
	// taken over that of the session it takes over from.
	var class, why string
	var over takeOver
	if a.Instead != "" {
		var err error
		if over, err = c.checkHandOn(a); err != nil {
			return ipc.Session{}, err
		}
		class, why = over.class, over.why
		// It goes on under the name of the subtask it takes over, which
		// was the user's only when it was theirs, as a failover's does.
		if a.Name == "" {
			a.Name, named = over.name, over.named
		}
	} else {
		class, why = c.classify(a.Task)
	}
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
	for k, v := range map[string]string{"parent": a.Parent, "continues": a.Continues, "instead": a.Instead} {
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
	// An agent of your own works in its folder, in place, where its
	// CLAUDE.md, skills and records are, unless the task names a place.
	if profile.Folder != "" && a.Repo == "" && a.Cwd == "" {
		a.Repo, a.InPlace = profile.Folder, true
	}
	if a.Model != "" {
		if profile.Harness != "claude-code" || profile.Local != nil {
			return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "+%s takes no model for one task: pick a Claude Code profile, or +%s<model> or +%s<model> for a local model", profileName, localPrefix, llamaPrefix)
		}
		profile.Model = a.Model
	}
	// A profile that starts on one of Irie's skills, such as +chaos, hands
	// the task to the skill (DESIGN §4.19), or says why it can't, and so
	// does a task that names its own skill, as a plugin's word does
	// (DESIGN §4.17); the person's words name the session, as a guide's
	// do. It probes the CLI, as the spawn does below, before taking the
	// lock.
	skill, who := profile.Skill, "+"+profileName
	if a.Skill != "" {
		skill, who = a.Skill, "This task"
	}
	if skill != "" {
		if err := c.skillCheck(who, profile, skill); err != nil {
			return ipc.Session{}, err
		}
		if a.Name == "" {
			a.Name, named = title(a.Task), false
		}
		a.Task = skillTask(skill, a.Task)
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
	// The session a subtask is taken over from stops only now, once
	// nothing but the spawn itself can fail.
	if a.Instead != "" {
		c.handOn(a, profileName)
		a.Task = handOnNote(over.profile, a.Wait) + a.Task
	}
	return c.spawnProfile(a, named, profileName, profile, warnings, extra)
}

// spawnProfile spawns a on profile, which is ready to run: routed, its
// local model checked. extra goes into spawn.requested.
func (c *Core) spawnProfile(a ipc.SpawnArgs, named bool, profileName string, profile config.Profile, warnings []string, extra map[string]any) (ipc.Session, error) {
	h, err := harnessFor(profile.Harness)
	if err != nil {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "profile %s: %v", profileName, err)
	}
	tutor, err := c.tutorFor(a, h.ID())
	if err != nil {
		return ipc.Session{}, err
	}
	if tutor != "" {
		if extra == nil {
			extra = map[string]any{}
		}
		extra["tutor"] = tutor
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
	// A session no one watches refuses what would wait on a permission
	// prompt, where its Claude Code can (DESIGN §6.6).
	if a.Unattended {
		if extra == nil {
			extra = map[string]any{}
		}
		extra["unattended"] = true
		if h.ID() == "claude-code" && !claude.AtLeast(probe.Version, claude.DenyPromptsVersion) {
			probe.Warnings = append(probe.Warnings, fmt.Sprintf("no one watches this session, but a Claude Code before %s asks rather than refuses, so it may wait on a permission prompt", claude.DenyPromptsVersion))
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
	if a.Folder != "" {
		base = slug(a.Folder)
	}
	// Anonymous research takes its compartment first, and work that an
	// anonymous session hands on, or a failover carries on, stays in its
	// compartment (DESIGN §4.16).
	if a.Anon == "" {
		c.mu.Lock()
		for _, from := range []string{a.Parent, a.Continues} {
			if p := c.sessions[from]; from != "" && p != nil && p.anon != "" {
				a.Anon = p.anon
			}
		}
		c.mu.Unlock()
	}
	var comp *compartment
	if a.Anon != "" {
		an, err := c.anonName(a.Anon, base)
		if err != nil {
			return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
		}
		got, notes, err := c.prepareAnon(an, a.AnonRoute)
		if err != nil {
			c.log.Warn("spawn refused", "anon", an, "err", err)
			return ipc.Session{}, err
		}
		comp = &got
		probe.Warnings = append(probe.Warnings, notes...)
		if extra == nil {
			extra = map[string]any{}
		}
		extra["anon"] = got.Name
	}
	pl, err := c.place(a, base)
	if err != nil {
		c.log.Warn("spawn failed", "err", err)
		return ipc.Session{}, err
	}
	c.mu.Lock()
	pl.name, pl.named = unique(name, c.names(nil)), named
	// A subtask taken over is the same task on purpose: the session before
	// it was slow, not wrong.
	if hash, _ := extra["task_hash"].(string); hash != "" && a.Instead == "" {
		class, _ := extra["class"].(string)
		c.escalate(hash, class, profileName, time.Now())
	}
	x, err := c.launch(job{place: pl, profile: profileName, run: profile, h: h, probe: probe, task: a.Task, data: extra, tutor: tutor, anon: comp, unattended: a.Unattended})
	if err == nil {
		// Its space and group first: where it goes in the list follows
		// them (groups.go).
		c.placeInSpace(x.id, a)
		c.placeInGroup(x.id, a)
		c.placeNew(x.id, a)
		if a.Top {
			c.toTop(x.id)
		}
	}
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
	// A group --group made goes to the sidebars with the row in it.
	c.publishHeader()
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
	next    string         // a resumed conversation's next prompt, as a fork's task (ctx.go)
	resume  string         // the vendor id of the conversation to carry on
	data    map[string]any // more data for spawn.requested
	reopen  *closedRecord  // the closed session that comes back, with its id and place in the list
	tutor   string         // its tutor mode, explain or teach; "" when off (learn.tutor)
	anon    *compartment   // the compartment it researches in, anonymously; nil when it doesn't (anon.go)
	asked   bool           // its model was asked for by name, as a fork's --model: no fallback takes its place (fallback.go)
	// unattended is a session no one watches, such as a bench's run: what
	// would wait on a permission prompt is refused (DESIGN §6.6).
	unattended bool
	// rest brings a reopened session back resting: its pane runs no agent
	// until a wake (rest.go).
	rest bool
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
	warnings := append(slices.Clone(pl.warnings), j.probe.Warnings...)
	// While its model's own usage limit holds, the profile's own model
	// gives way to its fallback (fallback.go); a model asked for by name
	// stays.
	if p, ok := c.profile(j.profile); ok && profile.Model == p.Model && !j.asked {
		if m, why := c.startsOn(profile, time.Now()); m != profile.Model {
			profile.Model, warnings = m, append(warnings, why)
		}
	}
	x := &session{
		id: id, name: pl.name, named: pl.named, agent: "main", created: created,
		cwd: pl.cwd, repo: pl.repo, worktree: pl.worktree, branch: pl.branch, copied: pl.copied,
		profile: j.profile, harness: h.ID(), observed: h.Levels().Has(harness.Observed), paste: profile.Task == config.TaskPaste,
		vendorID: vendorID, warnings: warnings,
		model: profile.Model, local: profile.Local != nil, tutor: j.tutor, out: c.outcome(id),
		tally: tokenTally{since: created},
	}

	if err := os.MkdirAll(c.paths.Session(id), 0o700); err != nil {
		return nil, err
	}
	// A reopened session starts unfrozen (freeze.go), with no jots
	// waiting for it from before it closed (follow.go).
	os.Remove(c.paths.FreezeFile(id))
	os.Remove(c.paths.FollowUpFile(id))
	s, argv, files, spec, err := c.prepareRun(j, x, profile, h)
	if err != nil {
		return nil, err
	}

	// One command list: the window is born with its options and the slot's
	// size, so nothing ever sees it half made. Its temporary name is the
	// unique session id.
	w, ht := 80, 24
	if slot := c.snap.SlotPane(); slot != nil {
		w, ht = slot.Width, slot.Height
	}
	// Or the size the Mac app shows a session with no panes at: a new one
	// has none yet (sizes.go).
	if z := c.sizes.all; c.appViews > 0 && z.W > 0 && z.H > 0 {
		w, ht = z.W, z.H
	}
	size := []string{"-x", strconv.Itoa(w), "-y", strconv.Itoa(ht)}
	// A session that comes back resting runs no agent: its pane ends at
	// once, and tmux keeps it, dead, with the card, until a wake runs the
	// host on the same spec (rest.go).
	run := "host"
	if j.rest {
		run = "rested"
	}
	target := tmux.AgentsSession + ":=" + id
	opt := func(k, v string) []string { return []string{"set-option", "-p", "-t", target, k, v} }
	out, err := c.tmux.RunList([][]string{
		{"set-option", "-t", tmux.AgentsSession, "default-size", fmt.Sprintf("%dx%d", w, ht)},
		{"new-window", "-d", "-t", tmux.AgentsSession + ":", "-n", id, "-c", x.cwd, "-P", "-F", "#{pane_id}", "--", c.bin, run, "--spec", spec},
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
	// Its agent lock: its setup as its agent starts (locks.go).
	c.lockLater(x, j, s, argv, files)
	// Its created time too, which the pane keeps, for a session lost with
	// the server (lost.go).
	data := map[string]any{"cwd": x.cwd, "profile": j.profile, "created": x.created.Format(time.RFC3339Nano)}
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

// prepareRun writes what x's agent starts from for job j, on profile and
// harness h: the files its harness asks for and the spec its host runs,
// from which a spawn, a reopen and a wake start it alike (rest.go). The
// caller holds c.mu.
func (c *Core) prepareRun(j job, x *session, profile config.Profile, h harness.Harness) (s harness.Spawn, argv []string, files map[string][]byte, spec string, err error) {
	id := x.id
	dir := c.paths.Session(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return harness.Spawn{}, nil, nil, "", err
	}
	// Nor with what the arch agents brought it then (arch.go).
	os.Remove(filepath.Join(dir, harness.ArchFile))
	// Nor with the MCP servers it had then.
	os.Remove(filepath.Join(dir, harness.MCPFile))
	s = harness.Spawn{
		Session: id, Task: j.task, Next: j.next, Cwd: x.cwd, Repo: x.repo, Dir: dir, VendorID: x.vendorID,
		Bin: c.bin, HoldMax: c.cfg().Hold.Max, Probe: j.probe,
		StatusLine: c.cfg().Usage.StatusLine, Home: home(), Environ: c.agentEnv(profile),
		Arch:  c.archFor(profile, h.ID()),
		Tutor: x.tutor, Visual: x.tutor != "" && c.cfg().Learn.Visual, Tile: c.cfg().Learn.Pictures == config.PicturesTile,
		Unattended: j.unattended,
	}
	// Irie's tips go under the agent's spinner, where its harness has one
	// (docs/proposals/learning.md).
	if c.cfg().Learn.AgentTips {
		s.Tips = learn.Get().Tips
	}
	// Only the user's name goes to the agent: Claude Code titles a session
	// it gets no name for, and the row follows the title (title.go).
	if x.named {
		s.Name = x.name
	}
	// Agent teams need the session's anchor on the team server first; a
	// session whose anchor cannot be made starts without them.
	var anchor teamAnchor
	if c.teams(h.ID()) && j.anon == nil {
		a, err := c.anchor(id)
		if err != nil {
			c.log.Warn("teams", "session", id, "err", err)
		} else {
			anchor, s.Teams = a, true
		}
	}
	// Irie's skills, for a harness that loads them as a plugin.
	if j.probe.Flags["--plugin-dir"] {
		s.PluginDir = c.skillsFolder(profile, x)
	}
	// An anonymous session's servers and shut paths (DESIGN §4.16); else
	// Irie's browser, for a harness that takes MCP servers (DESIGN §4.15).
	if j.anon != nil {
		if h.ID() != "claude-code" {
			return harness.Spawn{}, nil, nil, "", fmt.Errorf("anonymous research runs only on Claude Code, which can shut its paths; +%s runs on %s", j.profile, h.ID())
		}
		if err := c.anonLaunch(&s, x, *j.anon); err != nil {
			return harness.Spawn{}, nil, nil, "", err
		}
	} else if j.probe.Flags["--mcp-config"] {
		if mcp, warn := c.browserMCP(id); warn != "" {
			x.warnings = append(x.warnings, warn)
		} else if mcp != nil {
			s.MCP = mcp
		}
		// A writing session's own way to Variorum (docs/proposals/writers.md).
		for name, srv := range c.variorumMCP(x.cwd) {
			if s.MCP == nil {
				s.MCP = map[string]harness.MCPServer{}
			}
			s.MCP[name] = srv
		}
		// Your plugins' tools, after Irie's own (DESIGN §4.17).
		s.MCP = c.ownToolsMCP(x, s.MCP)
	}
	launch := h.Launch
	if j.resume != "" {
		r, ok := h.(harness.Resumer)
		if !ok {
			return harness.Spawn{}, nil, nil, "", fmt.Errorf("%s cannot carry a conversation on", h.ID())
		}
		launch = r.Resume
	}
	var env map[string]string
	argv, env, files, err = launch(profile, s)
	if err != nil {
		return harness.Spawn{}, nil, nil, "", err
	}
	for f, b := range files {
		if err := filetx.WriteFile(filepath.Join(dir, f), b, 0o600); err != nil {
			return harness.Spawn{}, nil, nil, "", err
		}
	}
	// Irie's own variables let the agent's hooks find the core. tmux's are
	// removed, so an agent's own tmux commands cannot reach Irie's server.
	if env == nil {
		env = map[string]string{}
	}
	env[brand.Env("SESSION")], env[brand.Env("SOCKET")], env[brand.Env("BIN")] = id, c.paths.Socket(), c.bin
	var unset []string
	if c.cfg().Spawn.HideTmux {
		unset = []string{"TMUX", "TMUX_PANE"}
	}
	// With teams, TMUX names the team server instead.
	var path []string
	if s.Teams {
		bin, err := c.armTeam(id, dir, anchor, profile, env)
		if err != nil {
			return harness.Spawn{}, nil, nil, "", err
		}
		path = []string{bin}
	}
	spec = c.paths.SpecFile(id)
	if err := host.WriteSpec(spec, host.Spec{
		Session: id, Profile: j.profile, Harness: h.ID(), Argv: argv, Env: env, Unset: unset, Path: path, Cwd: x.cwd, Warnings: x.warnings,
		Repo: x.repo, Worktree: x.worktree, Branch: x.branch, Copied: x.copied, Paste: x.paste,
		Model: x.model, Local: x.local, Tutor: x.tutor, Anon: x.anon,
	}); err != nil {
		return harness.Spawn{}, nil, nil, "", err
	}
	return s, argv, files, spec, nil
}
