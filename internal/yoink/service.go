package yoink

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
)

// MaxRunning is how many downloads run at once. One more waits for the
// person to cancel one or for one to finish, so links pasted in a hurry
// can't fill the disk or take the whole network.
const MaxRunning = 3

// KeepDone is how many finished downloads the list keeps; older ones go
// from it, and their files stay.
const KeepDone = 30

// maxLooks is how many links yt-dlp may look at at once.
const maxLooks = 2

// lookWait is the longest a look at a link may take: the first one also
// fetches the pinned yt-dlp.
var lookWait = 2 * time.Minute

// stopWait is how long a canceled download has to stop before it is
// killed.
var stopWait = 3 * time.Second

// maxInfo is the most of yt-dlp's JSON a look reads: a long video's runs
// to a few megabytes, with its every format and caption.
const maxInfo = 32 << 20

// maxRead is the most of a log one read takes: past it, only the newest.
const maxRead = 4 << 20

// lookedFor is how long a look stands for a download started after it.
const lookedFor = 10 * time.Minute

// The files in a download's folder.
const (
	recordFile = "download.json" // what it is, and what it came to
	outFile    = "out.log"       // yt-dlp's output
	errFile    = "err.log"       // yt-dlp's errors
	partsDir   = "parts"         // the parts it writes until they are whole
)

// Service is the core's downloads: those that run, and the last ones that
// finished. Each has a folder of its own in Dir, so a download carries on
// while the core restarts, and the core after picks it up.
type Service struct {
	// Dir holds a folder for each download, named by its ID.
	Dir string

	mu      sync.Mutex
	jobs    map[string]*job
	loaded  bool
	looking int
	looked  map[string]look
}

// look is a link looked at, and when.
type look struct {
	info ipc.YoinkInfo
	at   time.Time
}

// record is what a download's folder keeps of it.
type record struct {
	ID      string    `json:"id"`
	URL     string    `json:"url"`
	Title   string    `json:"title"`
	Site    string    `json:"site,omitempty"`
	Get     string    `json:"get"`
	Folder  string    `json:"folder"`
	Started time.Time `json:"started"`
	// PID is its process group's leader, and PStart when that started,
	// which tells it from a later process that reuses its pid.
	PID      int   `json:"pid,omitempty"`
	PStart   int64 `json:"pstart,omitempty"`
	Canceled bool  `json:"canceled,omitempty"`
	// What it came to: State is done, failed or canceled once it ends,
	// "" while it runs.
	State string    `json:"state,omitempty"`
	File  string    `json:"file,omitempty"`
	Size  int64     `json:"size,omitempty"`
	Error string    `json:"error,omitempty"`
	Ended time.Time `json:"ended,omitzero"`
}

// job is a download and what the core knows of it now.
type job struct {
	record
	dir    string
	prog   progress
	offs   [2]int64      // how far its two logs are read
	exited chan struct{} // closed once its process has ended and it is finished
}

// ids are a download's ID: eight hex digits, as its folder is named.
var ids = regexp.MustCompile(`^[0-9a-f]{8}$`)

// Formats says what the link raw offers: its title, and what of it can be
// downloaded, with about how big each is.
func (s *Service) Formats(e Engine, raw string) (ipc.YoinkInfo, error) {
	url, err := CheckURL(raw)
	if err != nil {
		return ipc.YoinkInfo{}, err
	}
	return s.look(e, url)
}

// look asks yt-dlp what url offers, and keeps the answer a while for a
// download started after it.
func (s *Service) look(e Engine, url string) (ipc.YoinkInfo, error) {
	if len(e.Argv) == 0 {
		return ipc.YoinkInfo{}, errors.New("there is no yt-dlp to run")
	}
	s.mu.Lock()
	if s.looking >= maxLooks {
		s.mu.Unlock()
		return ipc.YoinkInfo{}, errors.New("yt-dlp is looking at two links already: give this one again in a moment")
	}
	s.looking++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.looking--
		s.mu.Unlock()
	}()
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return ipc.YoinkInfo{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lookWait)
	defer cancel()
	args := e.lookArgs(url)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir, cmd.Env = s.Dir, e.env()
	// Its own group, so a look that takes too long ends with every
	// process it started.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	out := &capped{max: maxInfo}
	var errs lines
	cmd.Stdout, cmd.Stderr = out, &errs
	err := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return ipc.YoinkInfo{}, fmt.Errorf("yt-dlp took more than %s to read that link: give it again in a while", lookWait)
	case out.over:
		return ipc.YoinkInfo{}, errors.New("yt-dlp said more of that link than Yoink reads: give the link of one video")
	case err != nil:
		var p progress
		for _, ln := range errs.all() {
			p.line(ln)
		}
		code := -1
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			code = ee.ExitCode()
		} else {
			return ipc.YoinkInfo{}, fmt.Errorf("couldn't run yt-dlp: %w", err)
		}
		return ipc.YoinkInfo{}, errors.New(strings.TrimSuffix(p.problem(code), "."))
	}
	info, err := parseInfo(url, out.Bytes())
	if err != nil {
		return ipc.YoinkInfo{}, err
	}
	s.mu.Lock()
	if s.looked == nil {
		s.looked = map[string]look{}
	}
	for k, l := range s.looked {
		if time.Since(l.at) > lookedFor {
			delete(s.looked, k)
		}
	}
	if len(s.looked) < 64 {
		s.looked[url] = look{info: info, at: time.Now()}
	}
	s.mu.Unlock()
	return info, nil
}

// Start starts a download of the link raw into folder, as get says: best,
// audio or a height. It looks at the link first, unless it did a moment
// ago, so a playlist or a link yt-dlp can't read is refused at once.
func (s *Service) Start(e Engine, folder, raw, get string) (ipc.YoinkDownload, error) {
	url, err := CheckURL(raw)
	if err != nil {
		return ipc.YoinkDownload{}, err
	}
	if get, err = ParseGet(get); err != nil {
		return ipc.YoinkDownload{}, err
	}
	if !filepath.IsAbs(folder) {
		return ipc.YoinkDownload{}, fmt.Errorf("downloads land in a folder named by its whole path, and %q isn't one", folder)
	}
	s.load()
	if err := s.room(); err != nil {
		return ipc.YoinkDownload{}, err
	}
	s.mu.Lock()
	l, ok := s.looked[url]
	s.mu.Unlock()
	info := l.info
	if !ok || time.Since(l.at) > lookedFor {
		if info, err = s.look(e, url); err != nil {
			return ipc.YoinkDownload{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.roomLocked(); err != nil {
		return ipc.YoinkDownload{}, err
	}
	j, cmd, err := s.launch(e, record{URL: url, Title: info.Title, Site: info.Site, Get: get, Folder: folder})
	if err != nil {
		return ipc.YoinkDownload{}, err
	}
	go s.wait(j, cmd)
	return s.view(j), nil
}

// launch starts yt-dlp for r in a folder of its own, in a session of its
// own, writing to files there, so it runs on whatever happens to the core;
// the caller waits for cmd. The caller holds s.mu.
func (s *Service) launch(e Engine, r record) (*job, *exec.Cmd, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, nil, err
	}
	for {
		r.ID = newID()
		if _, taken := s.jobs[r.ID]; !taken {
			break
		}
	}
	j := &job{record: r, dir: filepath.Join(s.Dir, r.ID)}
	parts := filepath.Join(j.dir, partsDir)
	if err := os.MkdirAll(parts, 0o700); err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*job, *exec.Cmd, error) {
		os.RemoveAll(j.dir)
		return nil, nil, err
	}
	out, err := os.OpenFile(filepath.Join(j.dir, outFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fail(err)
	}
	defer out.Close()
	errs, err := os.OpenFile(filepath.Join(j.dir, errFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fail(err)
	}
	defer errs.Close()
	args := e.downloadArgs(r.URL, r.Get, r.Folder, parts)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir, cmd.Env = j.dir, e.env()
	// The files, not pipes to the core: yt-dlp writes to them itself, so
	// a core that stops takes nothing from it.
	cmd.Stdout, cmd.Stderr = out, errs
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	j.Started = time.Now()
	if err := cmd.Start(); err != nil {
		return fail(fmt.Errorf("couldn't start yt-dlp: %w", err))
	}
	j.PID = cmd.Process.Pid
	if p, err := proc.One(j.PID); err == nil {
		j.PStart = p.Start
	}
	j.exited = make(chan struct{})
	if s.jobs == nil {
		s.jobs = map[string]*job{}
	}
	s.jobs[j.ID] = j
	s.save(j)
	return j, cmd, nil
}

// wait waits for the download this core started to end.
func (s *Service) wait(j *job, cmd *exec.Cmd) {
	code := 0
	if err := cmd.Wait(); err != nil {
		code = -1
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			code = ee.ExitCode()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finish(j, code)
}

// watch waits for a download a core before this one started to end: it
// can't wait for another's child, so it looks each second.
func (s *Service) watch(j *job) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		if !alive(j.PID, j.PStart) {
			s.mu.Lock()
			s.finish(j, -1)
			s.mu.Unlock()
			return
		}
	}
}

// alive says whether the process pid that started at start runs.
func alive(pid int, start int64) bool {
	if pid <= 0 {
		return false
	}
	p, err := proc.One(pid)
	return err == nil && !p.Zombie && (start == 0 || p.Start == start)
}

// finish records what the download came to, once its process has ended:
// done once yt-dlp has put the file in the folder, else canceled at the
// person's word, else failed, with yt-dlp's reason. The parts it wrote go
// either way. code is its exit code, -1 when no one saw it end. The caller
// holds s.mu.
func (s *Service) finish(j *job, code int) {
	if j.State != "" {
		return
	}
	s.read(j, true)
	switch {
	case j.prog.file != "":
		j.State, j.File = ipc.YoinkDone, j.prog.file
		if st, err := os.Stat(j.File); err == nil {
			j.Size = st.Size()
		}
	case j.Canceled:
		j.State = ipc.YoinkCanceled
	default:
		j.State, j.Error = ipc.YoinkFailed, j.prog.problem(code)
	}
	j.Ended = time.Now()
	os.RemoveAll(filepath.Join(j.dir, partsDir))
	if j.State != ipc.YoinkFailed {
		os.Remove(filepath.Join(j.dir, outFile))
		os.Remove(filepath.Join(j.dir, errFile))
	}
	s.save(j)
	if j.exited != nil {
		close(j.exited)
	}
	s.prune()
}

// Cancel stops the download id and removes the parts it wrote. It answers
// once the download has stopped, or a few seconds on.
func (s *Service) Cancel(id string) (ipc.YoinkDownload, error) {
	s.load()
	s.mu.Lock()
	j := s.jobs[id]
	switch {
	case j == nil:
		s.mu.Unlock()
		return ipc.YoinkDownload{}, fmt.Errorf("there is no download %q: `"+brand.Slug+" yoink list` names them", id)
	case j.State != "":
		v := s.view(j)
		s.mu.Unlock()
		return v, fmt.Errorf("that download has %s already", map[string]string{ipc.YoinkDone: "finished", ipc.YoinkFailed: "failed", ipc.YoinkCanceled: "been canceled"}[j.State])
	}
	j.Canceled = true
	s.save(j)
	pid, start, exited := j.PID, j.PStart, j.exited
	s.mu.Unlock()
	// Its whole group: uvx, Python and the ffmpeg it runs.
	if alive(pid, start) {
		syscall.Kill(-pid, syscall.SIGTERM)
	}
	select {
	case <-exited:
	case <-time.After(stopWait):
		if alive(pid, start) {
			syscall.Kill(-pid, syscall.SIGKILL)
		}
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view(j), nil
}

// Clear forgets the download id, or every finished one when id is "", and
// says how many went. The files they made stay where they are.
func (s *Service) Clear(id string) (int, error) {
	s.load()
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != "" {
		j := s.jobs[id]
		switch {
		case j == nil:
			return 0, fmt.Errorf("there is no download %q: `"+brand.Slug+" yoink list` names them", id)
		case j.State == "":
			return 0, errors.New("that download still runs: cancel it first")
		}
		s.forget(j)
		return 1, nil
	}
	n := 0
	for _, j := range s.jobs {
		if j.State != "" {
			s.forget(j)
			n++
		}
	}
	return n, nil
}

// forget takes a finished download off the list, with its folder. The
// caller holds s.mu.
func (s *Service) forget(j *job) {
	delete(s.jobs, j.ID)
	os.RemoveAll(j.dir)
}

// List is the downloads, newest first, each with how far it is.
func (s *Service) List() []ipc.YoinkDownload {
	s.load()
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([]*job, 0, len(s.jobs))
	for _, j := range s.jobs {
		all = append(all, j)
	}
	slices.SortFunc(all, func(a, b *job) int { return b.Started.Compare(a.Started) })
	out := make([]ipc.YoinkDownload, 0, len(all))
	for _, j := range all {
		out = append(out, s.view(j))
	}
	return out
}

// Running is how many downloads run now.
func (s *Service) Running() int {
	s.load()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running()
}

// running counts the downloads that run. The caller holds s.mu.
func (s *Service) running() int {
	n := 0
	for _, j := range s.jobs {
		if j.State == "" {
			n++
		}
	}
	return n
}

// room says why another download can't start now, or nil when it can.
func (s *Service) room() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.roomLocked()
}

func (s *Service) roomLocked() error {
	if s.running() >= MaxRunning {
		return fmt.Errorf("%d downloads run already, the most at once: wait for one to finish, or cancel one", MaxRunning)
	}
	return nil
}

// view is the download as the wire has it, with how far it is now. The
// caller holds s.mu.
func (s *Service) view(j *job) ipc.YoinkDownload {
	d := ipc.YoinkDownload{
		ID: j.ID, URL: j.URL, Title: j.Title, Site: j.Site, Get: j.Get, Label: Label(j.Get),
		Started: j.Started, Ended: j.Ended,
	}
	switch j.State {
	case "":
		s.read(j, false)
		j.prog.show(&d)
		if j.Canceled {
			d.Step = "Canceling"
		}
	case ipc.YoinkDone:
		d.State, d.Step, d.File, d.Done, d.Total = j.State, "Done", j.File, j.Size, j.Size
	case ipc.YoinkFailed:
		d.State, d.Error = j.State, j.Error
	default:
		d.State, d.Step = j.State, "Canceled"
	}
	return d
}

// read takes the lines yt-dlp wrote since the last read. A last line with
// no end yet waits for the next, unless it is final: the process is gone.
// The caller holds s.mu.
func (s *Service) read(j *job, final bool) {
	for i, name := range []string{outFile, errFile} {
		f, err := os.Open(filepath.Join(j.dir, name))
		if err != nil {
			continue
		}
		if st, err := f.Stat(); err == nil {
			if st.Size() < j.offs[i] {
				j.offs[i] = 0
			}
			if st.Size()-j.offs[i] > maxRead {
				j.offs[i] = st.Size() - maxRead
			}
		}
		_, err = f.Seek(j.offs[i], io.SeekStart)
		var data []byte
		if err == nil {
			data, _ = io.ReadAll(io.LimitReader(f, maxRead))
		}
		f.Close()
		end := bytes.LastIndexByte(data, '\n') + 1
		if final {
			end = len(data)
		}
		for ln := range strings.SplitSeq(string(data[:end]), "\n") {
			j.prog.line(ln)
		}
		j.offs[i] += int64(end)
	}
}

// load reads the downloads a core before this one left in Dir, once: one
// that still runs is watched until it ends, and one that ended while no
// core watched is finished from what it wrote.
func (s *Service) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return
	}
	s.loaded = true
	if s.jobs == nil {
		s.jobs = map[string]*job{}
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !ids.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(s.Dir, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, recordFile))
		var r record
		if err != nil || json.Unmarshal(data, &r) != nil || r.ID != e.Name() {
			// A folder a core left before it could say what it holds.
			os.RemoveAll(dir)
			continue
		}
		j := &job{record: r, dir: dir}
		s.jobs[r.ID] = j
		if r.State != "" {
			continue
		}
		j.exited = make(chan struct{})
		if alive(r.PID, r.PStart) {
			go s.watch(j)
		} else {
			s.finish(j, -1)
		}
	}
	s.prune()
}

// prune keeps the KeepDone newest finished downloads. The caller holds
// s.mu.
func (s *Service) prune() {
	var done []*job
	for _, j := range s.jobs {
		if j.State != "" {
			done = append(done, j)
		}
	}
	if len(done) <= KeepDone {
		return
	}
	slices.SortFunc(done, func(a, b *job) int { return b.Ended.Compare(a.Ended) })
	for _, j := range done[KeepDone:] {
		s.forget(j)
	}
}

// save writes j's record to its folder, whole or not at all. The caller
// holds s.mu.
func (s *Service) save(j *job) {
	data, err := json.MarshalIndent(j.record, "", "  ")
	if err != nil {
		return
	}
	tmp := filepath.Join(j.dir, recordFile+".new")
	if os.WriteFile(tmp, data, 0o600) == nil {
		os.Rename(tmp, filepath.Join(j.dir, recordFile))
	}
}

// newID is a new download's ID.
func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// capped keeps what is written to it, up to max bytes, and fails past it,
// so a look that says too much ends.
type capped struct {
	bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.Len()+len(p) > c.max {
		c.over = true
		return 0, errors.New("too much")
	}
	return c.Buffer.Write(p)
}

// lines keeps the last 64 KB written to it, for the error at its end.
type lines struct{ b []byte }

func (l *lines) Write(p []byte) (int, error) {
	l.b = append(l.b, p...)
	if over := len(l.b) - 64<<10; over > 0 {
		l.b = l.b[over:]
	}
	return len(p), nil
}

func (l *lines) all() []string { return strings.Split(string(l.b), "\n") }
