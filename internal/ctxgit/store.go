// Package ctxgit keeps agents' contexts in git (DESIGN §4.13). The store is
// a bare repository of Irie's own, private to the user: each session is a
// branch named by its id, each turn of its conversation a commit whose
// subject is the prompt that started it, and a fork a branch that shares
// its source's history up to the turn it was forked at. A commit's tree
// holds the conversation so far, a pair of files a turn: turns/NNNN.jsonl,
// the transcript's own lines, from which a fork's transcript is made, and
// turns/NNNN.md, the turn as a person reads it. So git itself works on
// contexts: log, show, diff, tag, merge-base, bundle.
package ctxgit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Session is what the store is told of a session when it syncs one.
type Session struct {
	ID           string   // Irie's session id, which names its branch
	Name         string   // its name in the list
	Conversation string   // the harness's id of the conversation it carries on
	Model        string   // the model it runs
	Profile      string   // the profile it runs on
	Code         string   // the commit its folder's checkout is at; "" outside git
	Dirty        int      // files not committed in that checkout
	Working      bool     // its turn is still going
	Final        bool     // it has ended: what its last turn's bookkeeping holds is stored too
	Transcripts  []string // its transcripts, oldest first
}

// Turn is one commit of a context, as the log tells it.
type Turn struct {
	Commit       string    `json:"commit"`
	Time         time.Time `json:"time"`
	Turn         int       `json:"turn"`
	Prompt       string    `json:"prompt"`          // the commit's subject: the prompt that started the turn
	Said         string    `json:"said,omitempty"`  // the turn's last reply, cut
	Session      string    `json:"session"`         // the session that made the commit
	Name         string    `json:"name,omitempty"`  // its name then
	Conversation string    `json:"conversation"`    // the harness's conversation id
	Leaf         string    `json:"leaf"`            // the uuid of the turn's last entry
	Model        string    `json:"model,omitempty"` // the model then
	Code         string    `json:"code,omitempty"`  // the commit its checkout was at
	Dirty        int       `json:"dirty,omitempty"` // files it had not committed
	Partial      bool      `json:"partial,omitempty"`
	Cut          string    `json:"cut,omitempty"` // a fork's cut: the uuid its last turn ends at
}

// Store is the repository and what it has read of each session.
type Store struct {
	dir   string
	mu    sync.Mutex
	feeds map[string]*feed // by transcript path
	heads map[string]*head // by session id
	syncs int              // syncs so far, for gc
}

// head is what a session's branch holds: its turns, oldest first, along its
// first parents, and the turns folder of the newest.
type head struct {
	turns []Turn
	tree  []treeEntry // turns/ of the newest commit; nil until known
}

// Open opens the store at dir, making it if it is not there.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir, feeds: map[string]*feed{}, heads: map[string]*head{}}
	if exists(filepath.Join(dir, "HEAD")) {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, err
	}
	if _, err := run("", nil, "init", "--quiet", "--bare", "--object-format=sha1", dir); err != nil {
		return nil, err
	}
	os.Chmod(dir, 0o700)
	// Reflogs for every ref, so a branch's earlier heads can be found again;
	// a bare repository keeps none by default.
	if _, err := s.git(nil, "config", "core.logAllRefUpdates", "always"); err != nil {
		return nil, err
	}
	os.WriteFile(filepath.Join(dir, "description"), []byte("Irie's contexts: a branch per session, a commit per turn (DESIGN §4.13)\n"), 0o600)
	return s, nil
}

// Dir is where the store is, for git --git-dir.
func (s *Store) Dir() string { return s.dir }

// Branch is the ref of a session's context.
func Branch(session string) string { return "refs/heads/" + session }

var turnFile = regexp.MustCompile(`^(\d+)\.(jsonl|md)$`)

// turnOf is the turn a file of the turns folder is of; a name that is no
// turn's counts as past every turn, so it goes when turns are replaced.
func turnOf(name string) int {
	m := turnFile.FindStringSubmatch(name)
	if m == nil {
		return 1 << 30
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// turnName is the name of turn t's file of the kind ext.
func turnName(t int, ext string) string { return fmt.Sprintf("%04d.%s", t, ext) }

// Sync stores the turns of s's transcripts that its branch does not have
// yet, a commit each, and returns the branch's newest commit. A line of the
// conversation that goes back to an older turn, as a /rewind does, goes on
// from that turn's commit, and the branch's earlier head is kept under
// refs/irie/rewound/. A transcript that shares nothing with the branch,
// such as one /clear started, goes on from the branch's head with its own
// turns from the first.
func (st *Store) Sync(s Session) (string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s.ID == "" {
		return "", errors.New("ctxgit: a session needs an id")
	}
	h, err := st.head(s.ID)
	if err != nil {
		return "", err
	}
	for i, path := range s.Transcripts {
		f := st.feeds[path]
		if f == nil {
			f = newFeed(path)
			st.feeds[path] = f
		}
		f.used = time.Now()
		if err := f.read(); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		if f.leaf == nil {
			continue
		}
		last := i == len(s.Transcripts)-1
		if !last && slices.ContainsFunc(h.turns, func(t Turn) bool { return t.Leaf == f.leaf.id }) {
			continue
		}
		if err := st.commitLine(s, h, f, f.chain(), last && s.Working, !last || s.Final); err != nil {
			return "", err
		}
	}
	st.trim(maxNodes)
	st.maybeGC()
	if len(h.turns) == 0 {
		return "", nil
	}
	return h.turns[len(h.turns)-1].Commit, nil
}

// maxNodes is how many transcript entries the store keeps in memory, over
// every transcript it reads: about 200 bytes each. Past it, the transcripts
// read longest ago are let go, to be read again from the top when their
// session next ends a turn.
const maxNodes = 200_000

// trim lets go of the transcripts read longest ago until at most max
// entries are kept.
func (st *Store) trim(max int) {
	total := 0
	for _, f := range st.feeds {
		total += len(f.nodes)
	}
	for total > max && len(st.feeds) > 1 {
		var oldest *feed
		for _, f := range st.feeds {
			if oldest == nil || f.used.Before(oldest.used) {
				oldest = f
			}
		}
		total -= len(oldest.nodes)
		delete(st.feeds, oldest.path)
	}
}

// gcEvery is how many syncs go by between two looks by git gc --auto,
// which packs the loose objects the store writes once there are enough.
const gcEvery = 200

// maybeGC runs git gc --auto every gcEvery syncs, in the background.
func (st *Store) maybeGC() {
	st.syncs++
	if st.syncs%gcEvery != 0 {
		return
	}
	go st.git(nil, "gc", "--auto", "--quiet")
}

// lean is a turn with only what syncing needs, which is all a head keeps.
func lean(t Turn) Turn {
	return Turn{Commit: t.Commit, Turn: t.Turn, Leaf: t.Leaf, Partial: t.Partial, Cut: t.Cut, Session: t.Session}
}

// Forget drops what the store keeps in memory for a session, such as once
// it has closed. Its branch stays.
func (st *Store) Forget(s Session) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.heads, s.ID)
	for _, p := range s.Transcripts {
		delete(st.feeds, p)
	}
}

// commitLine stores the turns of chain that the branch lacks. working says
// the last turn is still going; final, that no more will come.
//
// Claude Code writes a turn's bookkeeping, such as how long it took and
// what its Stop hooks said, after the Stop hook that stores the turn. Such
// entries alone, with no new message, make no commit of their own: they go
// into the next turn's commit, or, once the session has ended, a last one.
// So a turn is one commit.
func (st *Store) commitLine(s Session, h *head, f *feed, chain []*node, working, final bool) error {
	if len(chain) == 0 {
		return nil
	}
	ts := turns(chain)
	on := make(map[string]bool, len(chain))
	for _, n := range chain {
		on[n.id] = true
	}
	leaf := chain[len(chain)-1].id
	// The base is the newest commit whose last entry is on this line.
	base := -1
	for i := len(h.turns) - 1; i >= 0; i-- {
		if on[h.turns[i].Leaf] {
			base = i
			break
		}
	}
	start, parent, fold := 0, "", -1 // fold: a turn whose bookkeeping goes into the next turn's commit
	var tree []treeEntry
	if base >= 0 {
		b := h.turns[base]
		if b.Leaf == leaf && base == len(h.turns)-1 {
			return nil
		}
		t, endsTurn := locate(ts, b.Leaf)
		start, parent = t, b.Commit
		if endsTurn {
			start = t + 1
		} else if !grew(ts[t], b.Leaf) {
			if t == len(ts)-1 && !final {
				return nil
			}
			fold = t
		}
		var err error
		if tree, err = st.treeOf(h, base); err != nil {
			return err
		}
		tree = slices.DeleteFunc(slices.Clone(tree), func(e treeEntry) bool { return turnOf(e.name) >= start })
	} else if len(h.turns) > 0 {
		parent = h.turns[len(h.turns)-1].Commit
	}
	// A line that went back is a rewind: the old head is kept.
	old := ""
	if len(h.turns) > 0 {
		old = h.turns[len(h.turns)-1].Commit
	}
	if base >= 0 && base < len(h.turns)-1 {
		if err := st.updateRef(fmt.Sprintf("refs/irie/rewound/%s/%d", s.ID, time.Now().UnixMilli()), old, "", "rewound"); err != nil {
			return err
		}
		h.turns = h.turns[:base+1]
	}
	var made []Turn
	for t := start; t < len(ts); t++ {
		if len(ts[t]) == 0 {
			continue
		}
		raw, err := f.lines(ts[t])
		if err != nil {
			return err
		}
		jsonl, err := st.write("blob", raw)
		if err != nil {
			return err
		}
		text, prompt, said := readTurn(raw)
		md, err := st.write("blob", text)
		if err != nil {
			return err
		}
		tree = slices.DeleteFunc(tree, func(e treeEntry) bool {
			return e.name == turnName(t, "jsonl") || e.name == turnName(t, "md")
		})
		tree = append(tree, treeEntry{name: turnName(t, "jsonl"), id: jsonl}, treeEntry{name: turnName(t, "md"), id: md})
		if t == fold && t < len(ts)-1 {
			continue
		}
		end := ts[t][len(ts[t])-1]
		turn := Turn{
			Turn: t, Prompt: prompt, Said: said, Session: s.ID, Name: s.Name,
			Conversation: s.Conversation, Leaf: end.id, Model: s.Model,
			Partial: working && t == len(ts)-1, Time: end.at,
		}
		if t == len(ts)-1 {
			turn.Code, turn.Dirty = s.Code, s.Dirty
		}
		if turn.Time.IsZero() {
			turn.Time = time.Now()
		}
		id, err := st.commitTurn(tree, parent, turn)
		if err != nil {
			return err
		}
		turn.Commit, parent = id, id
		made = append(made, lean(turn))
	}
	if parent == old {
		return nil
	}
	if err := st.updateRef(Branch(s.ID), parent, old, "turn"); err != nil {
		return err
	}
	h.turns = append(h.turns, made...)
	h.tree = tree
	return nil
}

// commitTurn writes the commit of one turn on parent, its tree the turns
// folder given.
func (st *Store) commitTurn(tree []treeEntry, parent string, t Turn) (string, error) {
	sub, err := st.writeTree(tree)
	if err != nil {
		return "", err
	}
	root, err := st.writeTree([]treeEntry{{name: "turns", id: sub, dir: true}})
	if err != nil {
		return "", err
	}
	return st.writeCommit(root, []string{parent}, t.Time, message(t))
}

// grew reports whether a turn has a message after the entry id: more of
// the conversation, not just Claude Code's bookkeeping.
func grew(turn []*node, id string) bool {
	after := false
	for _, n := range turn {
		if after && n.msg {
			return true
		}
		after = after || n.id == id
	}
	return false
}

// locate is the turn of ts that holds the entry id, and whether the entry
// ends it.
func locate(ts [][]*node, id string) (int, bool) {
	for t, nodes := range ts {
		for i, n := range nodes {
			if n.id == id {
				return t, i == len(nodes)-1
			}
		}
	}
	return 0, false
}

// treeOf is the turns folder of the branch's commit i.
func (st *Store) treeOf(h *head, i int) ([]treeEntry, error) {
	if i == len(h.turns)-1 && h.tree != nil {
		return h.tree, nil
	}
	return st.lsTurns(h.turns[i].Commit)
}

// lsTurns lists the turns folder of commit.
func (st *Store) lsTurns(commit string) ([]treeEntry, error) {
	out, err := st.git(nil, "ls-tree", "-z", commit+":turns")
	if err != nil {
		return nil, err
	}
	var tree []treeEntry
	for _, rec := range bytes.Split(out, []byte{0}) {
		// <mode> SP <type> SP <id> TAB <name>
		meta, name, ok := bytes.Cut(rec, []byte{'\t'})
		if !ok {
			continue
		}
		f := strings.Fields(string(meta))
		if len(f) != 3 || f[1] != "blob" {
			continue
		}
		tree = append(tree, treeEntry{name: string(name), id: f[2]})
	}
	return tree, nil
}

// updateRef moves ref from old ("" when it must not exist) to id, with a
// reflog line saying why.
func (st *Store) updateRef(ref, id, old, why string) error {
	args := []string{"update-ref", "--create-reflog", "-m", "irie: " + why, ref, id}
	if old != "" {
		args = append(args, old)
	} else {
		args = append(args, strings.Repeat("0", 40))
	}
	_, err := st.git(nil, args...)
	return err
}

// head loads what a session's branch holds.
func (st *Store) head(session string) (*head, error) {
	if h := st.heads[session]; h != nil {
		return h, nil
	}
	turns, err := st.log(Branch(session), 0, true)
	if err != nil {
		return nil, err
	}
	slices.Reverse(turns)
	for i := range turns {
		turns[i] = lean(turns[i])
	}
	h := &head{turns: turns}
	st.heads[session] = h
	return h, nil
}

// Log is a session's context, newest turn first, at most limit turns (0:
// all), along first parents: a fork's goes on into its source's turns
// before the fork.
func (st *Store) Log(session string, limit int) ([]Turn, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.log(Branch(session), limit, false)
}

// LogRev is the log from any revision, such as a tag or a commit.
func (st *Store) LogRev(rev string, limit int) ([]Turn, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.log(rev, limit, false)
}

// log reads the turns of rev's first-parent history, newest first. A rev
// that does not exist has none when missingOK.
func (st *Store) log(rev string, limit int, missingOK bool) ([]Turn, error) {
	if missingOK {
		if _, err := st.git(nil, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
			if exitCode(err) == 1 {
				return nil, nil
			}
			return nil, err
		}
	}
	args := []string{"log", "--first-parent", "--format=%H%x1f%ct%x1f%B%x1e"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, err := st.git(nil, append(args, rev, "--")...)
	if err != nil {
		return nil, err
	}
	var turns []Turn
	for _, rec := range bytes.Split(out, []byte{0x1e}) {
		rec = bytes.TrimLeft(rec, "\n")
		f := bytes.SplitN(rec, []byte{0x1f}, 3)
		if len(f) != 3 {
			continue
		}
		t := parseMessage(string(f[2]))
		t.Commit, t.Time = string(f[0]), commitTime(string(f[1]))
		turns = append(turns, t)
	}
	return turns, nil
}

// Resolve finds the commit a point of a session's context names: "" its
// newest turn; N its turn N, -N the turn N before its newest; a tag; a
// commit; or the uuid of an entry of its conversation, which also comes
// back as cut when the entry does not end its turn.
func (st *Store) Resolve(session, at string) (commit, cut string, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	h, err := st.head(session)
	if err != nil {
		return "", "", err
	}
	tip := ""
	if len(h.turns) > 0 {
		tip = h.turns[len(h.turns)-1].Commit
	}
	if at == "" {
		if tip == "" {
			return "", "", fmt.Errorf("%s has no turns stored yet", session)
		}
		return tip, "", nil
	}
	if n, err := strconv.Atoi(at); err == nil {
		if len(h.turns) == 0 {
			return "", "", fmt.Errorf("%s has no turns stored yet", session)
		}
		want := n
		if n < 0 {
			want = h.turns[len(h.turns)-1].Turn + n
		}
		for i := len(h.turns) - 1; i >= 0; i-- {
			if h.turns[i].Turn == want && h.turns[i].Cut == "" {
				return h.turns[i].Commit, "", nil
			}
		}
		return "", "", fmt.Errorf("%s has no turn %d", session, want)
	}
	if uuidRE.MatchString(at) {
		for i := len(h.turns) - 1; i >= 0; i-- {
			if h.turns[i].Leaf == at {
				return h.turns[i].Commit, "", nil
			}
		}
		if tip != "" {
			// The turn file that holds the entry, then the newest commit
			// of that turn.
			out, err := st.git(nil, "grep", "-l", "-F", `"uuid":"`+at+`"`, tip, "--", "turns/*.jsonl")
			if err == nil {
				name := filepath.Base(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
				if m := turnFile.FindStringSubmatch(name); m != nil {
					n, _ := strconv.Atoi(m[1])
					for i := len(h.turns) - 1; i >= 0; i-- {
						if h.turns[i].Turn == n && !h.turns[i].Partial {
							return h.turns[i].Commit, at, nil
						}
					}
				}
			}
		}
		return "", "", fmt.Errorf("no entry %s in %s's context", at, session)
	}
	for _, rev := range []string{"refs/tags/" + at, at} {
		out, err := st.git(nil, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
		if err == nil {
			return strings.TrimSpace(string(out)), "", nil
		}
	}
	return "", "", fmt.Errorf("%q names no turn, tag or commit of %s's context", at, session)
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Transcript is the conversation at commit as transcript lines, oldest
// first, cut after the entry cut when it is given.
func (st *Store) Transcript(commit, cut string) ([]byte, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	tree, err := st.lsTurns(commit)
	if err != nil {
		return nil, err
	}
	type file struct {
		n  int
		id string
	}
	var files []file
	for _, e := range tree {
		if m := turnFile.FindStringSubmatch(e.name); m != nil && m[2] == "jsonl" {
			n, _ := strconv.Atoi(m[1])
			files = append(files, file{n, e.id})
		}
	}
	slices.SortFunc(files, func(a, b file) int { return a.n - b.n })
	var in bytes.Buffer
	for _, f := range files {
		in.WriteString(f.id + "\n")
	}
	out, err := st.git(in.Bytes(), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	var all bytes.Buffer
	for len(out) > 0 {
		header, rest, ok := bytes.Cut(out, []byte{'\n'})
		if !ok {
			break
		}
		f := strings.Fields(string(header))
		if len(f) != 3 {
			return nil, fmt.Errorf("ctxgit: cat-file said %q", header)
		}
		size, err := strconv.Atoi(f[2])
		if err != nil || size > len(rest) {
			return nil, fmt.Errorf("ctxgit: cat-file said %q", header)
		}
		all.Write(rest[:size])
		out = rest[size:]
		out = bytes.TrimPrefix(out, []byte{'\n'})
	}
	if cut == "" {
		return all.Bytes(), nil
	}
	b := all.Bytes()
	for off := 0; off < len(b); {
		end := bytes.IndexByte(b[off:], '\n')
		if end < 0 {
			end = len(b) - off
		}
		var l line
		if lineUUID(b[off:off+end], &l) == cut {
			return b[:off+end+1], nil
		}
		off += end + 1
	}
	return nil, fmt.Errorf("no entry %s at %s", cut, commit[:12])
}

// Cut stores a commit on commit that drops what its last turn holds after
// the entry cut: where a fork from the middle of a turn starts.
func (st *Store) Cut(commit, cut string) (string, error) {
	lines, err := st.Transcript(commit, cut)
	if err != nil {
		return "", err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	tree, err := st.lsTurns(commit)
	if err != nil {
		return "", err
	}
	// The turn files are rebuilt from the lines that are left: the last
	// file is the cut turn's.
	ts, err := splitTurns(lines)
	if err != nil {
		return "", err
	}
	last := len(ts) - 1
	tree = slices.DeleteFunc(tree, func(e treeEntry) bool { return turnOf(e.name) >= last })
	raw := ts[last]
	jsonl, err := st.write("blob", raw)
	if err != nil {
		return "", err
	}
	md, err := st.write("blob", render(raw))
	if err != nil {
		return "", err
	}
	tree = append(tree, treeEntry{name: turnName(last, "jsonl"), id: jsonl}, treeEntry{name: turnName(last, "md"), id: md})
	base, err := st.log(commit, 1, false)
	if err != nil || len(base) == 0 {
		return "", fmt.Errorf("ctxgit: no commit %s", commit)
	}
	t := base[0]
	t.Leaf, t.Cut, t.Partial, t.Said, t.Time = cut, cut, false, lastReply(raw), time.Now()
	return st.commitTurn(tree, commit, t)
}

// splitTurns splits transcript lines into the raw lines of each turn, the
// first holding what came before the first prompt.
func splitTurns(b []byte) ([][]byte, error) {
	out := [][]byte{nil}
	for _, ln := range bytes.SplitAfter(b, []byte{'\n'}) {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var l line
		lineUUID(bytes.TrimRight(ln, "\n"), &l)
		if l.promptText() != "" {
			out = append(out, nil)
		}
		out[len(out)-1] = append(out[len(out)-1], ln...)
	}
	return out, nil
}

// lineUUID reads a transcript line into l and returns its uuid.
func lineUUID(b []byte, l *line) string {
	if err := json.Unmarshal(b, l); err != nil {
		return ""
	}
	return l.UUID
}

// StartBranch makes a session's branch start at commit, as a fork's does. The
// branch must not exist yet.
func (st *Store) StartBranch(session, commit string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := st.updateRef(Branch(session), commit, "", "fork"); err != nil {
		return err
	}
	delete(st.heads, session)
	return nil
}

// Tag names a commit, as a lightweight git tag; an existing tag moves.
func (st *Store) Tag(name, commit string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, err := st.git(nil, "check-ref-format", "refs/tags/"+name); err != nil || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%q cannot be a tag's name", name)
	}
	_, err := st.git(nil, "update-ref", "--create-reflog", "-m", "irie: tag", "refs/tags/"+name, commit)
	return err
}

// Rev resolves any revision of the store, such as a session's branch.
func (st *Store) Rev(rev string) (string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	out, err := st.git(nil, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("no %s in the context store", rev)
	}
	return strings.TrimSpace(string(out)), nil
}
