//go:build !nojammin

package jammin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/repo"
)

// The files source: music files in a folder of the person's, jammin.folder,
// played by afplay(1), which macOS has. The core starts afplay for each
// track in a session of its own, so the track plays on while the core
// restarts, as it does at each install, and keeps what plays in
// Dir/files.json; a core after it watches the same afplay and starts the
// next track when it ends. Pause stops afplay with SIGSTOP and play goes on
// with SIGCONT. afplay can't start partway into a track, so the source has
// no seek, and the volume is the Mac's.

// audioKinds are the files afplay plays, by their extension. Apple Music's
// protected .m4p isn't one.
var audioKinds = []string{".mp3", ".m4a", ".aac", ".aif", ".aiff", ".aifc", ".wav", ".flac", ".caf", ".mp4a"}

// Limits on a walk of the folder, so a vast one can't stall the core.
const (
	filesMost    = 20000
	filesDeepest = 12
	filesFresh   = time.Minute // how long a walk of the folder lasts
)

// covers are the artwork files a folder of music may hold, in the order
// they are taken.
var covers = []string{"cover.jpg", "cover.jpeg", "cover.png", "folder.jpg", "folder.png", "front.jpg", "front.png", "album.jpg", "album.png"}

// filesURI is how a files item's URI starts: then the path from the folder,
// a folder's ending in /.
const filesURI = "files:"

// Player starts afplay and signals it; tests stand in for it.
type Player interface {
	// Start plays file in a session of its own, and gives its pid and its
	// start as the process table has it.
	Start(file string) (pid int, start int64, err error)
	// Alive says whether pid is still the afplay that started at start.
	Alive(pid int, start int64) bool
	Signal(pid int, sig syscall.Signal) error
}

// Afplay plays with macOS's afplay(1).
type Afplay struct {
	Bin string // "" = /usr/bin/afplay
}

func (a Afplay) Start(file string) (int, int64, error) {
	bin := a.Bin
	if bin == "" {
		bin = "/usr/bin/afplay"
	}
	cmd := exec.Command(bin, file)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, 0, err
	}
	pid := cmd.Process.Pid
	// Reaped while this core lives; a core after it leaves that to launchd.
	go cmd.Wait()
	p, err := proc.One(pid)
	if err != nil {
		return pid, 0, nil
	}
	return pid, p.Start, nil
}

func (Afplay) Alive(pid int, start int64) bool {
	p, err := proc.One(pid)
	return err == nil && !p.Zombie && (start == 0 || p.Start == start)
}

func (Afplay) Signal(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }

// filesState is what plays, kept in Dir/files.json for the core after this
// one.
type filesState struct {
	Folder   string   `json:"folder"`    // the folder it plays from, ~ expanded
	Queue    []string `json:"queue"`     // the tracks in the order they play, by path from the folder
	At       int      `json:"at"`        // the track in Queue that plays or is paused
	List     string   `json:"list"`      // what plays: a folder's URI, or the whole folder's
	PID      int      `json:"pid"`       // afplay's; 0 = nothing plays
	Start    int64    `json:"start"`     // afplay's start, as the process table has it
	Began    int64    `json:"began"`     // when the track began, in Unix ms
	PausedAt int64    `json:"paused_at"` // when it was paused, in Unix ms; 0 = it plays
	PausedMS int64    `json:"paused_ms"` // how long the track was paused before that
	Shuffle  bool     `json:"shuffle"`
	Repeat   string   `json:"repeat"` // off, track or all
}

// trackMeta is what Spotlight knows of a track, or what its name says.
type trackMeta struct {
	title, artist, album string
	duration             int // ms; 0 = unknown
}

// files is the files source.
type files struct {
	sv *Service

	mu       sync.Mutex
	st       filesState
	loaded   bool
	watching bool
	index    []string // the folder's tracks, by path from it, sorted
	indexed  time.Time
	indexOf  string // the folder the index is of
	meta     map[string]trackMeta
}

func (*files) Name() string  { return ipc.JamFiles }
func (*files) Title() string { return "Files" }

func (f *files) player() Player {
	if f.sv.Player != nil {
		return f.sv.Player
	}
	return Afplay{}
}

// folder is jammin.folder, ~ expanded, and as config.toml has it.
func (f *files) folder() (string, string) {
	name := f.sv.config().Folder
	if name == "" {
		name = "~/Music"
	}
	return repo.Expand(name, f.sv.home()), name
}

func (f *files) statePath() string { return filepath.Join(f.sv.Dir, "files.json") }

// load reads what played from the core before this one, once.
func (f *files) load() {
	if f.loaded {
		return
	}
	f.loaded = true
	if b, err := os.ReadFile(f.statePath()); err == nil {
		_ = json.Unmarshal(b, &f.st)
	}
	if f.st.Repeat == "" {
		f.st.Repeat = ipc.JamRepeatOff
	}
}

// save keeps what plays for the core after this one.
func (f *files) save() {
	if f.sv.Dir == "" {
		return
	}
	b, _ := json.Marshal(f.st)
	if err := os.MkdirAll(f.sv.Dir, 0o700); err == nil {
		_ = filetx.WriteFile(f.statePath(), b, 0o600)
	}
}

// Resume watches afplay for a track that played on while the core
// restarted, so the next track starts when it ends. It asks nothing of
// anyone else.
func (f *files) Resume() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	if f.st.PID != 0 && !f.player().Alive(f.st.PID, f.st.Start) && f.st.PausedAt == 0 {
		// It ended while no core watched: go on to the next.
		f.advance()
	}
	f.watch()
}

// watchEvery is how often the watcher looks whether the track ended.
const watchEvery = 500 * time.Millisecond

// watch starts the watcher, while something plays, until the Service
// closes. Under the lock.
func (f *files) watch() {
	if f.watching || f.st.PID == 0 {
		return
	}
	f.watching = true
	quit := f.sv.quitting()
	go func() {
		t := time.NewTicker(watchEvery)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-t.C:
			}
			if !f.tick() {
				return
			}
		}
	}()
}

// tick goes on to the next track if the one playing ended, and says
// whether something still plays.
func (f *files) tick() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.st.PID == 0 {
		f.watching = false
		return false
	}
	if f.st.PausedAt == 0 && !f.player().Alive(f.st.PID, f.st.Start) {
		f.advance()
	}
	return true
}

// advance goes on from a track that ended by itself: the same again with
// repeat track, the next, or the first again with repeat all; else it
// stops. Under the lock.
func (f *files) advance() {
	switch {
	case f.st.Repeat == ipc.JamRepeatTrack:
	case f.st.At+1 < len(f.st.Queue):
		f.st.At++
	case f.st.Repeat == ipc.JamRepeatAll && len(f.st.Queue) > 0:
		f.st.At = 0
	default:
		f.st.PID, f.st.Start, f.st.PausedAt, f.st.PausedMS = 0, 0, 0, 0
		f.save()
		return
	}
	_ = f.playAt(f.st.At)
}

// playAt plays the queue's track i from its start, stopping what plays
// first. Under the lock.
func (f *files) playAt(i int) error {
	f.stopTrack()
	if i < 0 || i >= len(f.st.Queue) {
		f.save()
		return errors.New("there's no track there")
	}
	file := filepath.Join(f.st.Folder, f.st.Queue[i])
	pid, start, err := f.player().Start(file)
	if err != nil {
		f.st.PID = 0
		f.save()
		return fmt.Errorf("afplay didn't start: %v", err)
	}
	f.st.At, f.st.PID, f.st.Start = i, pid, start
	f.st.Began, f.st.PausedAt, f.st.PausedMS = f.sv.now().UnixMilli(), 0, 0
	f.save()
	f.watch()
	return nil
}

// stopTrack ends the afplay that plays, if any, waking it first should it
// be paused. Under the lock.
func (f *files) stopTrack() {
	if f.st.PID == 0 {
		return
	}
	p := f.player()
	if p.Alive(f.st.PID, f.st.Start) {
		_ = p.Signal(f.st.PID, syscall.SIGCONT)
		_ = p.Signal(f.st.PID, syscall.SIGTERM)
	}
	f.st.PID, f.st.Start = 0, 0
}

// walk lists the folder's tracks, by path from it, at most once a minute.
// Under the lock.
func (f *files) walk(folder string) ([]string, error) {
	if f.indexOf == folder && f.sv.now().Sub(f.indexed) < filesFresh {
		return f.index, nil
	}
	info, err := os.Stat(folder)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s isn't a folder", folder)
	}
	var out []string
	err = filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // a folder it may not read is skipped
		}
		name := d.Name()
		if p != folder && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(folder, p)
		if d.IsDir() {
			// An app's or a project's bundle holds no music to play here.
			if ext := strings.ToLower(filepath.Ext(name)); ext == ".app" || ext == ".band" || ext == ".logicx" || ext == ".musiclibrary" || strings.Count(rel, string(filepath.Separator)) >= filesDeepest {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && slices.Contains(audioKinds, strings.ToLower(filepath.Ext(name))) && !strings.Contains(rel, "\n") {
			out = append(out, rel)
			if len(out) >= filesMost {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(out)
	f.index, f.indexed, f.indexOf = out, f.sv.now(), folder
	return out, nil
}

// track is what a track is, from Spotlight's metadata, else its name and
// its folder's, with its length from afinfo when Spotlight doesn't know
// it. Kept for the core's life. Under the lock.
func (f *files) track(folder, rel string) trackMeta {
	file := filepath.Join(folder, rel)
	if m, ok := f.meta[file]; ok {
		return m
	}
	m := trackMeta{title: strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))}
	if dir := filepath.Dir(rel); dir != "." {
		m.album = filepath.Base(dir)
	}
	if out, err := f.sv.command("/usr/bin/mdls", "-raw", "-nullMarker", "", "-name", "kMDItemTitle", "-name", "kMDItemAuthors", "-name", "kMDItemAlbum", "-name", "kMDItemDurationSeconds", file); err == nil {
		v := strings.Split(out, "\x00")
		for len(v) < 4 {
			v = append(v, "")
		}
		if t := strings.TrimSpace(v[0]); t != "" {
			m.title = t
		}
		m.artist = firstOf(v[1])
		if a := strings.TrimSpace(v[2]); a != "" {
			m.album = a
		}
		if s, err := strconv.ParseFloat(strings.TrimSpace(v[3]), 64); err == nil && s > 0 {
			m.duration = int(s * 1000)
		}
	}
	if m.duration == 0 {
		if out, err := f.sv.command("/usr/bin/afinfo", file); err == nil {
			if _, after, ok := strings.Cut(out, "estimated duration:"); ok {
				if s, err := strconv.ParseFloat(strings.Fields(after + " 0")[0], 64); err == nil && s > 0 {
					m.duration = int(s * 1000)
				}
			}
		}
	}
	if f.meta == nil {
		f.meta = map[string]trackMeta{}
	}
	if len(f.meta) > 500 {
		clear(f.meta)
	}
	f.meta[file] = m
	return m
}

// firstOf is the first value of mdls's list in its raw form, such as
// ("Night Owls", "The Tide"), or the value itself.
func firstOf(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") {
		return s
	}
	for _, line := range strings.Split(strings.Trim(s, "()"), "\n") {
		if v := strings.Trim(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ",")), `"`); v != "" {
			return v
		}
	}
	return ""
}

// cover is the artwork of a track's folder, if it has one.
func cover(folder, rel string) string {
	dir := filepath.Join(folder, filepath.Dir(rel))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, want := range covers {
		for _, e := range ents {
			if strings.EqualFold(e.Name(), want) && e.Type().IsRegular() {
				return filepath.Join(dir, e.Name())
			}
		}
	}
	return ""
}

func (f *files) Look(ctx context.Context) ipc.JamSource {
	folder, named := f.folder()
	st := ipc.JamSource{Name: f.Name(), Title: f.Title(), Folder: named}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		st.State, st.Problem = ipc.JamNeeds, "There's no folder at "+named+": Choose Folder in the Jammin tab, or set jammin.folder, to play the music files you keep."
		return st
	}
	st.State = ipc.JamReady
	st.Can = []string{ipc.JamCanPlay, ipc.JamCanSkip, ipc.JamCanShuffle, ipc.JamCanRepeat, ipc.JamCanLists, ipc.JamCanSearch}
	if f.st.PID != 0 && f.st.PausedAt == 0 && !f.player().Alive(f.st.PID, f.st.Start) {
		f.advance()
	}
	if f.st.PID == 0 || f.st.At >= len(f.st.Queue) || f.st.Folder != folder {
		return st
	}
	rel := f.st.Queue[f.st.At]
	m := f.track(folder, rel)
	now := f.sv.now().UnixMilli()
	upTo := now
	if f.st.PausedAt != 0 {
		upTo = f.st.PausedAt
	}
	pos := upTo - f.st.Began - f.st.PausedMS
	if pos < 0 {
		pos = 0
	}
	if m.duration > 0 && pos > int64(m.duration) {
		pos = int64(m.duration)
	}
	n := &ipc.JamNow{
		Playing:  f.st.PausedAt == 0,
		Title:    m.title,
		Album:    m.album,
		Kind:     "track",
		URI:      filesURI + filepath.ToSlash(rel),
		Art:      cover(folder, rel),
		Duration: m.duration,
		Position: int(pos),
		At:       now,
		Shuffle:  f.st.Shuffle,
		Repeat:   f.st.Repeat,
	}
	if m.artist != "" {
		n.Artists = []string{m.artist}
	}
	if f.st.List != "" {
		n.Context = &ipc.JamItem{Kind: "folder", Title: listTitle(f.st.List, named), URI: f.st.List}
	}
	st.Now = n
	return st
}

// listTitle is a list's name: its folder's, or the whole folder's.
func listTitle(uri, named string) string {
	rel := strings.TrimSuffix(strings.TrimPrefix(uri, filesURI), "/")
	if rel == "" {
		return named
	}
	return filepath.Base(rel)
}

// within reads a files URI as a path from the folder, and whether it
// names a folder: it may not leave the folder.
func within(uri string) (rel string, isDir bool, err error) {
	rest, ok := strings.CutPrefix(uri, filesURI)
	if !ok {
		return "", false, fmt.Errorf("%q isn't one of your files: they go by files:PATH", uri)
	}
	isDir = rest == "" || strings.HasSuffix(rest, "/")
	rel = filepath.Clean(filepath.FromSlash(strings.TrimSuffix(rest, "/")))
	if rel == "." {
		return "", true, nil
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, fmt.Errorf("%q leaves your music folder", uri)
	}
	return rel, isDir, nil
}

// inList are the tracks of the folder a list names, by path from the
// music folder.
func inList(all []string, dir string) []string {
	if dir == "" {
		return all
	}
	var out []string
	for _, rel := range all {
		if strings.HasPrefix(rel, dir+string(filepath.Separator)) {
			out = append(out, rel)
		}
	}
	return out
}

// order is a list's tracks in the order they play: as they are, or
// shuffled with first first.
func order(tracks []string, shuffle bool, first string) []string {
	q := slices.Clone(tracks)
	if shuffle {
		rand.Shuffle(len(q), func(i, j int) { q[i], q[j] = q[j], q[i] })
		if i := slices.Index(q, first); i > 0 {
			q[0], q[i] = q[i], q[0]
		}
	}
	return q
}

func (f *files) Do(ctx context.Context, a ipc.JamminArgs) error {
	folder, named := f.folder()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	p := f.player()
	playing := f.st.PID != 0 && p.Alive(f.st.PID, f.st.Start) && f.st.Folder == folder
	switch a.Do {
	case ipc.JamPlay, ipc.JamToggle:
		if a.Do == ipc.JamToggle && playing && f.st.PausedAt == 0 {
			return f.pause()
		}
		if strings.TrimSpace(a.URI) == "" && playing {
			return f.resume()
		}
		return f.start(folder, named, strings.TrimSpace(a.URI))
	case ipc.JamPause:
		if !playing {
			return errors.New("nothing of your files plays")
		}
		return f.pause()
	case ipc.JamNext:
		if !playing {
			return errors.New("nothing of your files plays")
		}
		i := f.st.At + 1
		if i >= len(f.st.Queue) {
			if f.st.Repeat != ipc.JamRepeatAll {
				f.stopTrack()
				f.save()
				return nil
			}
			i = 0
		}
		return f.playAt(i)
	case ipc.JamPrevious:
		if !playing {
			return errors.New("nothing of your files plays")
		}
		// As a player's own button: the track from its start, unless it
		// has only just begun.
		i := f.st.At
		upTo := f.sv.now().UnixMilli()
		if f.st.PausedAt != 0 {
			upTo = f.st.PausedAt
		}
		if upTo-f.st.Began-f.st.PausedMS < 3000 && i > 0 {
			i--
		}
		return f.playAt(i)
	case ipc.JamShuffle:
		f.st.Shuffle = *a.On
		if playing && len(f.st.Queue) > 0 {
			// The rest of the list in the new order, this track first.
			cur := f.st.Queue[f.st.At]
			rest := slices.Clone(f.st.Queue)
			if !f.st.Shuffle {
				slices.Sort(rest)
			}
			q := order(rest, f.st.Shuffle, cur)
			f.st.Queue, f.st.At = q, slices.Index(q, cur)
		}
		f.save()
		return nil
	case ipc.JamRepeat:
		f.st.Repeat = a.Repeat
		f.save()
		return nil
	}
	return fmt.Errorf("your files take no %q", a.Do)
}

// start plays a list or a track, or the whole folder from its start. A
// track plays within its folder, so the music goes on after it. Under the
// lock.
func (f *files) start(folder, named, uri string) error {
	all, err := f.walk(folder)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		return errors.New("there are no music files in " + named + " that afplay plays")
	}
	list, first := filesURI, ""
	tracks := all
	if uri != "" {
		rel, isDir, err := within(uri)
		if err != nil {
			return err
		}
		if isDir {
			list = filesURI + filepath.ToSlash(rel)
			if rel != "" {
				list += "/"
			}
			tracks = inList(all, rel)
		} else {
			if !slices.Contains(all, rel) {
				return fmt.Errorf("%s holds no %s that afplay plays", named, filepath.ToSlash(rel))
			}
			dir := filepath.Dir(rel)
			if dir == "." {
				dir = ""
			}
			list = filesURI + filepath.ToSlash(dir)
			if dir != "" {
				list += "/"
			}
			tracks, first = inList(all, dir), rel
		}
		if len(tracks) == 0 {
			return errors.New("that folder holds no music files that afplay plays")
		}
	} else if f.st.List != "" && f.st.Folder == folder && len(f.st.Queue) > 0 {
		// Play with nothing named goes on from the track that played last.
		return f.playAt(min(f.st.At, len(f.st.Queue)-1))
	}
	if first == "" && f.st.Shuffle {
		first = tracks[rand.IntN(len(tracks))]
	}
	q := order(tracks, f.st.Shuffle, first)
	at := 0
	if first != "" {
		at = slices.Index(q, first)
	}
	f.st.Folder, f.st.Queue, f.st.List = folder, q, list
	return f.playAt(at)
}

// pause stops afplay where it is. Under the lock.
func (f *files) pause() error {
	if f.st.PausedAt != 0 {
		return nil
	}
	if err := f.player().Signal(f.st.PID, syscall.SIGSTOP); err != nil {
		return fmt.Errorf("afplay didn't pause: %v", err)
	}
	f.st.PausedAt = f.sv.now().UnixMilli()
	f.save()
	return nil
}

// resume goes on from where it paused. Under the lock.
func (f *files) resume() error {
	if f.st.PausedAt == 0 {
		return nil
	}
	if err := f.player().Signal(f.st.PID, syscall.SIGCONT); err != nil {
		return fmt.Errorf("afplay didn't go on: %v", err)
	}
	f.st.PausedMS += f.sv.now().UnixMilli() - f.st.PausedAt
	f.st.PausedAt = 0
	f.save()
	return nil
}

// Lists are the folders that hold music, each a list, the folder itself
// first.
func (f *files) Lists(context.Context) ([]ipc.JamItem, error) {
	folder, named := f.folder()
	f.mu.Lock()
	defer f.mu.Unlock()
	all, err := f.walk(folder)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, rel := range all {
		if dir := filepath.Dir(rel); dir != "." {
			counts[dir]++
		}
	}
	out := []ipc.JamItem{{Kind: "folder", Title: "All of " + named, URI: filesURI, Tracks: len(all)}}
	for _, dir := range slices.Sorted(maps.Keys(counts)) {
		item := ipc.JamItem{Kind: "folder", Title: filepath.Base(dir), URI: filesURI + filepath.ToSlash(dir) + "/", Tracks: counts[dir]}
		if parent := filepath.Dir(dir); parent != "." {
			item.By = filepath.ToSlash(parent)
		}
		out = append(out, item)
		if len(out) > 500 {
			break
		}
	}
	return out, nil
}

// Search finds tracks by their path from the folder, the first 25.
func (f *files) Search(_ context.Context, words string) ([]ipc.JamItem, error) {
	folder, _ := f.folder()
	f.mu.Lock()
	defer f.mu.Unlock()
	all, err := f.walk(folder)
	if err != nil {
		return nil, err
	}
	want := strings.Fields(strings.ToLower(words))
	var out []ipc.JamItem
	for _, rel := range all {
		hay := strings.ToLower(rel)
		if !allIn(hay, want) {
			continue
		}
		dir := filepath.Dir(rel)
		item := ipc.JamItem{Kind: "track", Title: strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)), URI: filesURI + filepath.ToSlash(rel)}
		if dir != "." {
			item.Album = filepath.Base(dir)
			item.Context = filesURI + filepath.ToSlash(dir) + "/"
		}
		out = append(out, item)
		if len(out) == searchMost {
			break
		}
	}
	return out, nil
}

func allIn(hay string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

func (f *files) Devices(context.Context) ([]ipc.JamDevice, error) {
	return nil, errors.New("your files play here, on this Mac's own output")
}

// setFolder keeps the folder of music files as jammin.folder, with the
// home folder as ~, and stops what played from the folder before.
func (f *files) setFolder(name string) error {
	name = strings.TrimSpace(name)
	if !filepath.IsAbs(name) && name != "~" && !strings.HasPrefix(name, "~/") {
		return errors.New("give the folder as an absolute path, or one from ~")
	}
	path := repo.Expand(name, f.sv.home())
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%s isn't a folder", name)
	}
	name = repo.Tilde(path, f.sv.home())
	if f.sv.Configure == nil {
		return errors.New("can't keep the folder: there is no config file to write it to")
	}
	if err := f.sv.Configure([]ipc.ConfigSet{{Key: "jammin.folder", Value: jsonString(name)}}); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	if f.st.Folder != path {
		f.stopTrack()
		f.st.Queue, f.st.At, f.st.List = nil, 0, ""
		f.save()
	}
	f.indexed = time.Time{}
	return nil
}

// command runs a program of macOS's own and gives what it printed: mdls and
// afinfo. Tests stand in for it.
func (s *Service) command(bin string, args ...string) (string, error) {
	if s.Command != nil {
		return s.Command(bin, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}
