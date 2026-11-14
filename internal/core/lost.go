package core

// Sessions lost with Irie's tmux server (DESIGN §4.5). The list lives in
// that server: each row is an agent's pane, and its name, its place and
// whether it is pinned or parked are tmux options. When the server ends
// with agents in it, as when the Mac restarts or loses power, the rows go
// with it and nothing marks them closed, so neither history nor reopen
// could reach them. So the core looks for them as it starts: a session
// whose host ran, which has no pane and was not closed, was lost. It
// records each as closed on its own, with lost set, so history lists it
// and reopen brings it back as it was: its conversation carried on, pinned
// or parked as before, and frozen again once its agent starts if it was
// frozen. A session whose folder has not changed for journal.keep_days is
// left be, as its journal is gone.

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/journal"
)

// lostOne is a session folder findLost found lost.
type lostOne struct {
	id      string
	spec    host.Spec
	harness string    // its harness, from its spec or its profile
	born    time.Time // when its spec was written: the start of its last run
	record  closedRecord
}

// findLost records the sessions lost with their tmux server as closed on
// their own, with lost set, the one that started first first, so history
// lists the newest first. It runs once, as the core starts, after the first
// tick has adopted every pane tmux has, and before any client connects.
func (c *Core) findLost(now time.Time) {
	c.mu.Lock()
	// A server that does not answer could hide live sessions: then
	// nothing is lost.
	if err := c.refresh(); err != nil {
		c.mu.Unlock()
		c.log.Warn("lost", "err", err)
		return
	}
	taken := map[string]bool{}
	for _, p := range c.snap.Panes {
		taken[p.IrieSession] = true
	}
	for id := range c.sessions {
		taken[id] = true
	}
	for id := range c.closes.done {
		taken[id] = true
	}
	c.mu.Unlock()

	lost := c.lostFolders(now, taken)
	if len(lost) == 0 {
		return
	}
	ids := make([]string, len(lost))
	since := now
	for i, l := range lost {
		ids[i] = l.id
		if l.born.Before(since) {
			since = l.born
		}
	}
	evs, err := journal.ReadSessions(c.paths.Journal(), since, ids...)
	if err != nil {
		c.log.Warn("lost: journal", "err", err)
	}
	byID := map[string][]event.Event{}
	for _, e := range evs {
		byID[e.Session] = append(byID[e.Session], e)
	}
	for i := range lost {
		lost[i].record = c.lostRecord(lost[i], byID[lost[i].id], now)
	}
	slices.SortStableFunc(lost, func(a, b lostOne) int { return a.record.Created.Compare(b.record.Created) })

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range lost {
		r := l.record
		if c.sessions[r.ID] != nil {
			continue
		}
		data := map[string]any{
			"name": r.Name, "named": r.Named, "created": r.Created.Format(time.RFC3339Nano), "vendor_id": r.VendorID, "resume": r.Resume, "lost": true,
		}
		for k, on := range map[string]bool{"parked": r.Parked, "pinned": r.Pinned, "frozen": r.Frozen} {
			if on {
				data[k] = true
			}
		}
		e := event.Event{TS: event.Stamp(r.Closed), Session: r.ID, Source: event.SourceIrie, Harness: l.harness, Kind: event.UIClosed, Data: data}
		if _, err := c.journal.Append(e); err != nil {
			c.log.Warn("lost: journal", "session", r.ID, "err", err)
			continue
		}
		c.addClosed(r)
		c.log.Info("lost", "session", r.ID, "name", r.Name, "resume", r.Resume, "parked", r.Parked, "pinned", r.Pinned, "frozen", r.Frozen)
	}
}

// lostFolders lists the session folders whose session was lost: its host
// ran, nothing in taken names it, and its folder changed within
// journal.keep_days of now.
func (c *Core) lostFolders(now time.Time, taken map[string]bool) []lostOne {
	entries, err := os.ReadDir(c.paths.Sessions())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			c.log.Warn("lost", "err", err)
		}
		return nil
	}
	cut := now.AddDate(0, 0, -max(c.cfg().Journal.KeepDays, 1))
	var out []lostOne
	for _, d := range entries {
		id := d.Name()
		if !d.IsDir() || !sessionID.MatchString(id) || taken[id] {
			continue
		}
		if _, err := os.Stat(c.paths.HostFile(id)); err != nil {
			continue
		}
		spec, err := host.ReadSpec(c.paths.SpecFile(id))
		if err != nil {
			continue
		}
		fi, err := os.Stat(c.paths.SpecFile(id))
		if err != nil || newest(c.paths.Session(id)).Before(cut) {
			continue
		}
		h := spec.Harness
		if h == "" {
			h = c.cfg().Profiles[spec.Profile].Harness
		}
		if h == "" {
			continue
		}
		out = append(out, lostOne{id: id, spec: spec, harness: h, born: fi.ModTime()})
	}
	return out
}

// newest is when anything in dir last changed, dir itself included.
func newest(dir string) time.Time {
	var t time.Time
	if fi, err := os.Stat(dir); err == nil {
		t = fi.ModTime()
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && fi.ModTime().After(t) {
			t = fi.ModTime()
		}
	}
	return t
}

// lostRecord is what reopen needs of lost session l, from its spec and the
// events evs of its last runs: the conversation the agent last named; the
// name the core last recorded for it, or else the one it got at spawn, or
// else the title Claude Code gave its conversation, or else its task's;
// and whether it was pinned, parked or frozen.
func (c *Core) lostRecord(l lostOne, evs []event.Event, now time.Time) closedRecord {
	r := closedRecord{ID: l.id, Closed: now.UTC().Truncate(time.Millisecond), Auto: true, Lost: true}
	var t track
	// A run starts over (track.apply), but the name the core recorded
	// last stays until it records another.
	name, named, haveName := "", false, false
	var created time.Time
	for _, e := range evs {
		t.apply(e)
		switch e.Kind {
		case event.SpawnRequested:
			// The pane's own created time, which a spawn records; before
			// it did, the spawn's time is within a moment of it.
			if created, _ = time.Parse(time.RFC3339Nano, e.Str("created")); created.IsZero() {
				created = e.Time()
			}
		case event.UIClosed:
			name, named, haveName = e.Str("name"), e.Data["named"] == true, true
		case event.UIReopened:
			if n := e.Str("name"); n != "" {
				name, haveName = n, true
			}
		case event.UIRenamed:
			name, named, haveName = e.Str("after"), e.Data["named"] != false, true
		}
	}
	r.Created = created
	if r.Created.IsZero() {
		r.Created = l.born.UTC().Truncate(time.Millisecond)
	}
	r.VendorID = t.vendorID
	if r.VendorID == "" {
		r.VendorID = argAfter(l.spec.Argv, "--resume")
	}
	if r.VendorID == "" {
		r.VendorID = argAfter(l.spec.Argv, "--session-id")
	}
	// Its own transcripts, as transcriptsOf finds them.
	var transcripts []string
	for _, p := range t.own {
		if _, err := os.Stat(p); err == nil {
			transcripts = append(transcripts, p)
		}
	}
	if len(transcripts) == 0 && l.harness == "claude-code" && r.VendorID != "" && l.spec.Cwd != "" {
		if p := claudeTranscript(c.claudeConfig(l.spec.Profile), l.spec.Cwd, r.VendorID); p != "" {
			transcripts = append(transcripts, p)
		}
	}
	h, err := harnessFor(l.harness)
	_, resumes := h.(harness.Resumer)
	_, taskErr := os.Stat(c.paths.TaskFile(l.id))
	// The journal may lack the run's events, as when it could not be
	// written, so a transcript on disk counts as a conversation too.
	r.Resume = err == nil && resumes && r.VendorID != "" && (taskErr != nil || t.conversed() || len(transcripts) > 0)

	switch given := argAfter(l.spec.Argv, "--name"); {
	case haveName && name != "":
		r.Name, r.Named = name, named
	case given != "":
		r.Name, r.Named = given, true
	default:
		for _, p := range transcripts {
			if t := lastTitle(p); t != "" {
				r.Name = titleCase(cleanText(t, maxName))
			}
		}
		if r.Name == "" {
			if task, err := os.ReadFile(c.paths.TaskFile(l.id)); err == nil {
				r.Name = title(string(task))
			}
		}
		if r.Name == "" {
			r.Name = l.id
		}
	}
	// freeze.json is there while it is frozen, and a thaw removes it, so it
	// holds even what a journal that could not be written lacks.
	_, noFreeze := os.Stat(c.paths.FreezeFile(l.id))
	r.Pinned, r.Parked, r.Frozen = t.pinned, t.parked, noFreeze == nil
	return r
}

// argAfter is the argument after flag in argv, or "".
func argAfter(argv []string, flag string) string {
	if i := slices.Index(argv, flag); i >= 0 && i+1 < len(argv) {
		return argv[i+1]
	}
	return ""
}

// lastTitle is the last title Claude Code gave the conversation whose
// transcript is at path, or "" when it gave none, or when a name given in
// Claude Code itself took its place (titleLine).
func lastTitle(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	title := ""
	br := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if t, ok := titleLine(bytes.TrimSpace(line)); ok {
			title = t
		}
		if errors.Is(err, io.EOF) {
			return title
		}
		if err != nil {
			return ""
		}
	}
}

// claudeConfig is the Claude Code config folder the agents of profile use:
// the profile's CLAUDE_CONFIG_DIR, else the core's own, else ~/.claude.
func (c *Core) claudeConfig(profile string) string {
	if dir := c.cfg().Profiles[profile].Env["CLAUDE_CONFIG_DIR"]; dir != "" {
		return dir
	}
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// comeBack puts a lost session that reopen brought back where it was:
// pinned last in Pinned, or parked last in Parked, as before; and, if it
// was frozen, frozen again once its agent has started (refreezeLost). The
// caller holds c.mu.
func (c *Core) comeBack(x *session, r closedRecord) {
	switch {
	case r.Pinned:
		if c.setPinned(x.id, true) {
			c.toEnd(x.id)
		}
	case r.Parked:
		if changed, _ := c.setParked(x.id, true); changed {
			c.toEnd(x.id)
		}
	}
	x.refreeze = r.Frozen
}

// refreezeLost freezes each lost session that was frozen, once reopen has
// brought it back and its agent has started, as it was left. The caller
// holds c.mu.
func (c *Core) refreezeLost() {
	for _, x := range c.sessions {
		if !x.refreeze || !x.track.started || x.track.frozen != nil {
			continue
		}
		x.refreeze = false
		if err := c.freezeLocked(x, ""); err != nil {
			c.log.Warn("lost: freeze", "session", x.id, "err", err)
		}
	}
}
