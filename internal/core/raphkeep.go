package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/raphael"
	"github.com/irie-people/irie/internal/sysload"
)

// Raffi keeps room for swap (DESIGN §4.11, §2.9). macOS grows the swap
// onto the startup disk when memory runs short, and on 2026-11-23 a disk
// 97% full left it no room, and the kernel panicked. So while the free
// disk is under sysload.Room's buffer he clears caches that their tools
// make again, by himself, with no press, oldest and safest first, until
// the buffer is back (raphael.Keep), and says each clear in core.log and in
// his report. What is still short after, the person's own files, he weighs
// and tells them of once, with the biggest to clear, each with a fix that
// asks first. After a restart he reads the newest kernel panic report and
// tells them in plain words what happened.
//
// He tells the person through the header (Tell): every client shows a
// tell no client has shown yet, the Mac app as a notice its Notices tab
// keeps, and the first the person reads says so (op raphael, told), after
// which no header carries it. Israfil, once he is here, can relay them.

// keepEvery is how often, in ticks, he reads the room.
const keepEvery = 15

const (
	keepPause     = 5 * time.Minute  // between two runs while the disk stays short
	ownEvery      = time.Hour        // between two weighings of the person's own files while it stays short
	roomTellEvery = 6 * time.Hour    // between two tells of a disk short of room, unless it falls under the floor
	tellKeep      = 24 * time.Hour   // how long the header carries a tell no client has shown
	panicEvery    = time.Minute      // how often he looks for a new restart report
	keptClears    = 20               // the clears he keeps, newest first
	keptTells     = 20               // the tells he keeps, newest first
	keepRunFor    = 20 * time.Minute // the longest a run may take
)

// keepState is Raffi's keeping of room. It has a lock of its own, taken
// after c.mu and c.raph.mu and never before them.
type keepState struct {
	mu           sync.Mutex
	running      atomic.Bool
	last         time.Time            // when the last run ended
	short        bool                 // the disk was short of the buffer at the last run, after it cleared
	starved      bool                 // and under the floor
	roomTold     time.Time            // when he last told the person the disk was short
	cleared      []ipc.RaphaelCleared // newest first
	own          []raphael.Finding    // the person's own biggest things, while the disk is short
	ownAt        time.Time            // when he weighed them
	tells        []keptTell           // newest first
	restart      *raphael.Panic       // the last restart he told of
	reports      string               // the restart reports he has read, by their paths
	panicChecked time.Time            // when he last looked for one

	// What tests replace.
	readRoom func() (sysload.Room, error) // sysload.ReadRoom
	places   *raphael.Places              // raphael.FindPlaces
	panicDir string                       // raphael.PanicDir, on macOS
	weighOwn func(context.Context, raphael.Runner, raphael.OwnInput) []raphael.Finding
}

// keptTell is a tell, and when a client the person reads showed it.
type keptTell struct {
	ipc.Tell
	Told *time.Time `json:"told,omitempty"`
}

// room reads the machine's room now.
func (k *keepState) room() (sysload.Room, error) {
	if k.readRoom != nil {
		return k.readRoom()
	}
	return sysload.ReadRoom()
}

// keepFile keeps what he cleared and told, for the core after this one.
func (c *Core) keepFile() string { return filepath.Join(c.paths.State, "raphael-room.json") }

// keepSaved is what raphael-room.json keeps.
type keepSaved struct {
	Last     time.Time            `json:"last,omitempty"`
	Short    bool                 `json:"short,omitempty"`
	Starved  bool                 `json:"starved,omitempty"`
	RoomTold time.Time            `json:"room_told,omitempty"`
	Cleared  []ipc.RaphaelCleared `json:"cleared,omitempty"`
	Tells    []keptTell           `json:"tells,omitempty"`
	Restart  *raphael.Panic       `json:"restart,omitempty"`
	Reports  string               `json:"reports,omitempty"`
}

// loadKeep reads what a core before this one kept.
func (c *Core) loadKeep() {
	b, err := os.ReadFile(c.keepFile())
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Warn("raphael: room", "err", err)
		}
		return
	}
	var s keepSaved
	if err := json.Unmarshal(b, &s); err != nil {
		c.log.Warn("raphael: room", "err", err)
		return
	}
	k := &c.raph.keep
	k.mu.Lock()
	k.last, k.short, k.starved, k.roomTold = s.Last, s.Short, s.Starved, s.RoomTold
	k.cleared, k.tells, k.restart, k.reports = s.Cleared, s.Tells, s.Restart, s.Reports
	k.mu.Unlock()
}

func (c *Core) saveKeep() {
	k := &c.raph.keep
	k.mu.Lock()
	s := keepSaved{Last: k.last, Short: k.short, Starved: k.starved, RoomTold: k.roomTold, Cleared: k.cleared, Tells: k.tells, Restart: k.restart, Reports: k.reports}
	k.mu.Unlock()
	b, err := json.Marshal(s)
	if err == nil {
		err = os.MkdirAll(c.paths.State, 0o700)
	}
	if err == nil {
		err = filetx.WriteFile(c.keepFile(), b, 0o600)
	}
	if err != nil {
		c.log.Warn("raphael: room", "err", err)
	}
}

// keepSweep reads the room, starts a run when the disk is short of the
// buffer, and looks for a new restart report. The caller holds no lock.
func (c *Core) keepSweep() {
	cfg := c.cfg().Arch.Raphael
	if !cfg.On || raphaelOff() {
		return
	}
	now := time.Now()
	c.notePanic(now)
	k := &c.raph.keep
	room, err := k.room()
	if err != nil || !room.Known() {
		return
	}
	if !room.Short() {
		k.mu.Lock()
		ended := k.short || len(k.own) > 0
		k.short, k.starved, k.own, k.ownAt = false, false, nil, time.Time{}
		k.mu.Unlock()
		if ended {
			c.log.Info("raphael: the disk has the room he keeps for swap again", "room", room.Words())
			c.saveKeep()
		}
		return
	}
	k.mu.Lock()
	due := now.Sub(k.last) >= keepPause
	k.mu.Unlock()
	if due && k.running.CompareAndSwap(false, true) {
		go c.keepRun(now)
	}
}

// keepRun clears caches until the buffer is back, says each clear, and
// tells the person once when it is still short, with their biggest things.
func (c *Core) keepRun(now time.Time) {
	k := &c.raph.keep
	defer k.running.Store(false)
	home, _ := os.UserHomeDir()
	ctx, cancel := context.WithTimeout(context.Background(), keepRunFor)
	defer cancel()
	run := c.raph.run()
	places := k.places
	if places == nil {
		p := raphael.FindPlaces(ctx, run, home)
		places = &p
	}
	res, err := raphael.Keep(ctx, run, raphael.KeepInput{Home: home, Now: now, Places: *places, Room: k.room})
	if err != nil {
		c.log.Warn("raphael: keeping room for swap", "err", err)
	}
	for _, x := range res.Cleared {
		args := []any{"what", x.What, "path", x.Path, "size", raphael.Size(x.Size), "freed", raphael.Size(x.Freed), "free", raphael.Size(x.Free)}
		if x.Error != "" {
			args = append(args, "err", x.Error)
		}
		c.log.Info("raphael: cleared by himself, to keep room for swap", args...)
	}
	for _, why := range res.Left {
		c.log.Info("raphael: left a cache be", "why", why)
	}
	room := res.Room
	k.mu.Lock()
	weigh := room.Short() && time.Since(k.ownAt) >= ownEvery
	k.mu.Unlock()
	var own []raphael.Finding
	if weigh {
		w := k.weighOwn
		if w == nil {
			w = raphael.OwnFindings
		}
		own = w(ctx, run, raphael.OwnInput{Home: home, Now: time.Now()})
	}
	k.mu.Lock()
	k.last = time.Now()
	for _, x := range res.Cleared {
		k.cleared = append([]ipc.RaphaelCleared{{At: x.At, ID: x.ID, What: x.What, Path: x.Path, Size: x.Size, Freed: x.Freed, Free: x.Free, Error: x.Error}}, k.cleared...)
	}
	k.cleared = k.cleared[:min(len(k.cleared), keptClears)]
	if weigh {
		k.own, k.ownAt = own, time.Now()
	}
	if !room.Short() {
		k.own, k.ownAt = nil, time.Time{}
	}
	// He tells once a while: as the disk first stays short, and again as
	// it falls under the floor.
	tell := room.Short() && (!k.short && time.Since(k.roomTold) >= roomTellEvery || room.Starved() && !k.starved)
	k.short, k.starved = room.Short(), room.Starved()
	if tell {
		k.roomTold = time.Now()
	}
	ownNow := slices.Clone(k.own)
	k.mu.Unlock()
	if tell {
		c.tell(roomTell(room, res.Cleared, ownNow, time.Now()))
	}
	c.saveKeep()
	c.publishHeader()
}

// roomTell is what he tells the person of a disk still short of the room
// he keeps for swap after he cleared what he could: the point, what he
// cleared, the biggest of theirs, and where to clear them.
func roomTell(room sysload.Room, cleared []raphael.Cleared, own []raphael.Finding, now time.Time) ipc.Tell {
	t := ipc.Tell{ID: "room:" + now.UTC().Format(time.RFC3339Nano), From: "raphael", At: now, Level: raphael.Warn,
		Text: fmt.Sprintf("Your disk is short of the room %s keeps for swap: %s free, under %s.", brand.Raffi, sysload.Size(room.DiskFree), sysload.Size(room.Buffer()))}
	if room.Starved() {
		t.Text = fmt.Sprintf("Your disk is nearly full: %s free, under the %s floor, so new heavy work waits.", sysload.Size(room.DiskFree), sysload.Size(room.Floor()))
	}
	var total int64
	for _, x := range cleared {
		total += x.Size
	}
	if total > 0 {
		t.Lines = append(t.Lines, fmt.Sprintf("%s cleared %s of caches by %sself.", brand.Raffi, raphael.Size(total), brand.Him))
	} else {
		t.Lines = append(t.Lines, brand.Raffi+" found no cache "+brand.He+" could clear.")
	}
	if len(own) > 0 {
		var parts []string
		for _, f := range own[:min(len(own), 3)] {
			parts = append(parts, raphael.OwnName(f)+" "+raphael.Size(f.Size))
		}
		t.Lines = append(t.Lines, "Biggest of yours: "+strings.Join(parts, ", ")+".")
		t.Lines = append(t.Lines, brand.Raffi+"'s row in Settings › Arch Agents clears each, on your OK.")
	}
	return t
}

// notePanic looks, at most once a minute, for a restart report newer than
// those he has read, and tells the person once of a restart in the last
// three days.
func (c *Core) notePanic(now time.Time) {
	k := &c.raph.keep
	dir := k.panicDir
	if dir == "" {
		if runtime.GOOS != "darwin" {
			return
		}
		dir = raphael.PanicDir
	}
	k.mu.Lock()
	if now.Sub(k.panicChecked) < panicEvery {
		k.mu.Unlock()
		return
	}
	k.panicChecked = now
	k.mu.Unlock()
	panicFile, reset := raphael.RestartReports(dir)
	if panicFile == "" && reset == "" {
		return
	}
	key := panicFile + "\n" + reset
	k.mu.Lock()
	seen := k.reports == key
	k.mu.Unlock()
	if seen {
		return
	}
	p, ok, err := raphael.ReadRestart(panicFile, reset)
	if err != nil {
		c.log.Warn("raphael: a panic report", "err", err)
	}
	k.mu.Lock()
	k.reports = key
	told := ok && k.restart != nil && k.restart.File == p.File
	fresh := ok && !told && now.Sub(p.At) < 3*24*time.Hour
	if fresh {
		k.restart = &p
	}
	k.mu.Unlock()
	if fresh {
		text, lines := p.Tell()
		c.log.Warn("raphael: the computer restarted", "at", p.At, "report", p.File, "said", text, "low_swap", p.LowSwap, "most_memory", p.TopWords())
		if p.LowSwap {
			if room, err := k.room(); err == nil && room.Known() {
				lines = append(lines, fmt.Sprintf("%s now keeps %s free for swap.", brand.Raffi, sysload.Size(room.Buffer())))
			}
		}
		c.tell(ipc.Tell{ID: "panic:" + filepath.Base(p.File), From: "raphael", At: now, Level: raphael.Warn, Text: text, Lines: lines[:min(len(lines), 3)]})
	}
	c.saveKeep()
}

// tell tells the person t once (Tell), unless he told it already.
func (c *Core) tell(t ipc.Tell) {
	k := &c.raph.keep
	k.mu.Lock()
	if slices.ContainsFunc(k.tells, func(x keptTell) bool { return x.ID == t.ID }) {
		k.mu.Unlock()
		return
	}
	k.tells = append([]keptTell{{Tell: t}}, k.tells...)
	k.tells = k.tells[:min(len(k.tells), keptTells)]
	k.mu.Unlock()
	c.log.Info("raphael: told the person", "id", t.ID, "text", t.Text, "lines", strings.Join(t.Lines, " "))
	c.saveKeep()
	c.publishHeader()
}

// tellsNow are the tells no client has shown, from the last day, oldest
// first, for the header. The caller may hold c.mu.
func (c *Core) tellsNow(now time.Time) []ipc.Tell {
	k := &c.raph.keep
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []ipc.Tell
	for i := len(k.tells) - 1; i >= 0; i-- {
		if t := k.tells[i]; t.Told == nil && now.Sub(t.At) < tellKeep {
			out = append(out, t.Tell)
		}
	}
	return out
}

// markTold says a client the person reads showed the tell id, so that no
// header carries it again.
func (c *Core) markTold(id string) {
	k := &c.raph.keep
	k.mu.Lock()
	found := false
	for i := range k.tells {
		if k.tells[i].ID == id && k.tells[i].Told == nil {
			at := time.Now()
			k.tells[i].Told, found = &at, true
		}
	}
	k.mu.Unlock()
	if found {
		c.saveKeep()
		c.publishHeader()
	}
}

// keepFindings are his findings of room and restarts: the person's own
// biggest things while the disk is short, and the last restart for three
// days. The caller may hold c.raph.mu.
func (c *Core) keepFindings(now time.Time) []raphael.Finding {
	k := &c.raph.keep
	k.mu.Lock()
	defer k.mu.Unlock()
	fs := slices.Clone(k.own)
	if k.restart != nil {
		if f, ok := raphael.PanicFinding(*k.restart, now); ok {
			fs = append(fs, f)
		}
	}
	return fs
}

// keepForget drops the finding id, whose fix ran, until a weighing finds
// it again. The caller may hold c.raph.mu.
func (c *Core) keepForget(id string) {
	k := &c.raph.keep
	k.mu.Lock()
	k.own = slices.DeleteFunc(k.own, func(f raphael.Finding) bool { return f.ID == id })
	k.mu.Unlock()
}

// keepReport adds the room, what he cleared and what he told to his
// report. The caller may hold c.raph.mu.
func (c *Core) keepReport(res *ipc.RaphaelResult) {
	k := &c.raph.keep
	room, err := k.room()
	k.mu.Lock()
	defer k.mu.Unlock()
	if err == nil && room.Known() {
		res.Room = roomWords(room)
	}
	res.Cleared = slices.Clone(k.cleared[:min(len(k.cleared), 10)])
	for _, t := range k.tells[:min(len(k.tells), 10)] {
		res.Told = append(res.Told, t.Tell)
	}
}

// roomWords says the free disk against the room he keeps, for his report.
func roomWords(room sysload.Room) string {
	s := fmt.Sprintf("%s of the disk is free. %s keeps %s free for swap, clearing caches by %sself below it; under %s, new heavy work waits.",
		sysload.Size(room.DiskFree), brand.Raffi, sysload.Size(room.Buffer()), brand.Him, sysload.Size(room.Floor()))
	switch {
	case room.Starved():
		s += " It is under that floor now."
	case room.Short():
		s += " It is short of that room now."
	}
	return s
}

// clearedLine says what he cleared by himself in the last hour, for his
// arch row; "" for nothing.
func clearedLine(cleared []ipc.RaphaelCleared, now time.Time) string {
	var total int64
	var last time.Time
	for _, x := range cleared {
		if now.Sub(x.At) < time.Hour {
			total += x.Size
			if x.At.After(last) {
				last = x.At
			}
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("At %s %s cleared %s of caches by %sself, to keep room for swap.", last.Local().Format("15:04"), brand.He, raphael.Size(total), brand.Him)
}
