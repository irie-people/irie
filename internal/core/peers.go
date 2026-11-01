package core

// Peers (docs/proposals/multi-host.md §4): the other machines' cores this
// one is paired with, kept in peers.json by `irie peer add`. The core
// follows each one's sessions through its gateway, as a phone does, and
// shows them in its own list after its own, each as a row whose id is the
// peer's name, a slash and that core's own id, with Host set. A call or a
// subscription for such a row goes on to its own core, and ^name in the
// task bar starts a task there. Nothing here touches a session of this
// machine's, and losing a peer only marks its rows: its sessions run on
// there, under its own core.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/peer"
	"github.com/irie-people/irie/internal/tmux"
)

const (
	// peerQuiet is how long a peer's stream may bring no frame, not even
	// the gateway's ping every 15 s, before it counts as gone.
	peerQuiet = 40 * time.Second
	// peerRetryMax is the longest wait before following a peer again.
	peerRetryMax = 30 * time.Second
	// peerCallTimeout bounds a call passed on to a peer, as the gateway
	// bounds its own call to its core.
	peerCallTimeout = 2 * time.Minute
)

// peerState is what the core knows of one peer. Its fields are the core's
// to change, under c.mu.
type peerState struct {
	name    string
	conf    peer.Peer
	client  *peer.Client
	rows    map[string]ipc.Session // by the peer's own id
	header  *ipc.Header
	placed  []time.Time        // when the balancer last put tasks there (place.go)
	fetchq  chan ipc.Session   // rows whose work is to be fetched (fetch.go)
	done    map[string]bool    // rows whose ended turn was queued, by the peer's id
	fetched map[string]fetched // what was fetched of each row
	up      bool               // its stream runs and sent its snapshot
	since   time.Time          // when it came up or went down
	problem string             // why it is down
	stop    context.CancelFunc
}

// loadPeers reads peers.json and follows each peer in it. A peer whose
// entry changed is followed afresh; one no longer in it is no longer
// followed, and its rows go.
func (c *Core) loadPeers() error {
	conf, err := peer.Load(c.paths.State)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.peers == nil {
		c.peers = map[string]*peerState{}
	}
	for name, p := range c.peers {
		if n, ok := conf[name]; !ok || n != p.conf {
			p.stop()
			delete(c.peers, name)
		}
	}
	var start []*peerState
	for _, name := range peer.Names(conf) {
		if c.peers[name] != nil {
			continue
		}
		pc := conf[name]
		p := &peerState{name: name, conf: pc, rows: map[string]ipc.Session{}, since: time.Now(), problem: "not reached yet",
			fetchq: make(chan ipc.Session, 64), done: map[string]bool{}, fetched: map[string]fetched{}}
		cl, err := peer.New(pc.URL, pc.Token, pc.Fingerprint)
		if err != nil {
			p.problem = err.Error()
			p.stop = func() {}
			c.peers[name] = p
			continue
		}
		p.client = cl
		ctx, cancel := context.WithCancel(context.Background())
		p.stop = cancel
		c.peers[name] = p
		start = append(start, p)
		go c.followPeer(ctx, p)
		go c.fetchLoop(p, ctx.Done())
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	c.publishHeader()
	for _, p := range start {
		c.log.Info("peer: following", "peer", p.name, "url", p.conf.URL)
	}
	return nil
}

// followPeer follows one peer until ctx ends, again after each stream
// ends: at once after one that ran, then after 1, 2, 4 … 30 s.
func (c *Core) followPeer(ctx context.Context, p *peerState) {
	wait := time.Second
	for ctx.Err() == nil {
		ran, err := c.streamPeer(ctx, p)
		if ctx.Err() != nil {
			return
		}
		c.peerDown(p, err)
		if ran {
			wait = time.Second
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
		wait = min(2*wait, peerRetryMax)
	}
}

// streamPeer follows one stream of the peer's until it ends, and says
// whether it got as far as a snapshot.
func (c *Core) streamPeer(ctx context.Context, p *peerState) (bool, error) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	frames, errs, err := p.client.Subscribe(sctx, ipc.SubscribeArgs{})
	if err != nil {
		return false, err
	}
	quiet := time.NewTimer(peerQuiet)
	defer quiet.Stop()
	ran := false
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				err := <-errs
				return ran, err
			}
			quiet.Reset(peerQuiet)
			if f.T == ipc.FrameSnapshot {
				ran = true
			}
			if err := c.safePeerFrame(p, f); err != nil {
				return ran, err
			}
		case <-quiet.C:
			return ran, fmt.Errorf("no word from it for %v", peerQuiet)
		case <-ctx.Done():
			return ran, ctx.Err()
		}
	}
}

// peerFrame takes in one frame of the peer's. A frame is another
// machine's word, so none may stop this core: the lock is let go however
// the frame is taken, and a frame that goes wrong ends the peer's stream
// (streamPeer), which marks it down until it is followed again.
func (c *Core) peerFrame(p *peerState, f ipc.Frame) {
	frames, header, ok := c.takePeerFrame(p, f)
	if !ok {
		return
	}
	c.publish(frames)
	if header {
		c.publishHeader()
	}
}

// takePeerFrame changes what the core knows of p by f, and returns the
// frames for this core's subscribers, and whether the header changed too.
func (c *Core) takePeerFrame(p *peerState, f ipc.Frame) (frames []ipc.Frame, header, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.peers[p.name] != p {
		return nil, false, false
	}
	switch f.T {
	case ipc.FrameSnapshot:
		p.rows = make(map[string]ipc.Session, len(f.Sessions))
		for _, s := range f.Sessions {
			if s.ID == "" {
				continue
			}
			p.rows[s.ID] = s
			c.queueFetch(p, s, nil)
		}
		if f.Header != nil {
			p.header = f.Header
		} else if p.header == nil {
			p.header = &ipc.Header{}
		}
		if !p.up {
			p.up, p.since, p.problem = true, time.Now(), ""
			c.log.Info("peer: up", "peer", p.name, "sessions", len(f.Sessions))
		}
		header = true
	case ipc.FrameUpsert:
		if f.Session == nil || f.Session.ID == "" {
			return nil, false, false
		}
		var was *ipc.Session
		if old, ok := p.rows[f.Session.ID]; ok {
			was = &old
		}
		p.rows[f.Session.ID] = *f.Session
		c.queueFetch(p, *f.Session, was)
	case ipc.FrameRemove:
		delete(p.rows, f.ID)
	case ipc.FrameHeader:
		if f.Header == nil {
			return nil, false, false
		}
		p.header, header = f.Header, true
	default:
		return nil, false, false
	}
	return c.changes(), header, true
}

// safePeerFrame is peerFrame, with a frame that goes wrong as an error.
func (c *Core) safePeerFrame(p *peerState, f ipc.Frame) (err error) {
	defer func() {
		if r := recover(); r != nil {
			c.log.Error("peer: a frame went wrong", "peer", p.name, "frame", f.T, "panic", r)
			err = fmt.Errorf("a frame of its went wrong here: %v", r)
		}
	}()
	c.peerFrame(p, f)
	return nil
}

// peerDown marks a peer gone, with why. Its rows stay, marked.
func (c *Core) peerDown(p *peerState, err error) {
	c.mu.Lock()
	if c.peers[p.name] != p {
		c.mu.Unlock()
		return
	}
	problem := "its stream ended"
	if err != nil {
		problem = err.Error()
	}
	if p.up {
		p.up, p.since = false, time.Now()
		c.log.Warn("peer: down", "peer", p.name, "err", problem)
	}
	p.problem = problem
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	c.publishHeader()
}

// peerRows are the peers' sessions as rows of this list, after its own
// from on: each peer's in its own order, the peers by name. A row is shown
// while it is the picked one and the slot draws its conversation. The
// caller holds c.mu.
func (c *Core) peerRows(from int) []ipc.Session {
	if len(c.peers) == 0 {
		return nil
	}
	slot := c.snap.SlotPane()
	drawn := slot != nil && slot.Role == tmux.RolePlaceholder
	var out []ipc.Session
	for _, name := range c.peerNames() {
		p := c.peers[name]
		rows := make([]ipc.Session, 0, len(p.rows))
		for _, s := range p.rows {
			if s.Host != "" {
				continue // the peer's own peers' rows: never passed on
			}
			rows = append(rows, s)
		}
		slices.SortStableFunc(rows, func(a, b ipc.Session) int { return a.Order - b.Order })
		for _, s := range rows {
			s.ID = name + "/" + s.ID
			s.Host = name
			if s.Parent != "" {
				s.Parent = name + "/" + s.Parent
			}
			where := "on " + name
			if !p.up {
				t := p.since
				s.HostDown = &t
				where = name + " isn't answering"
			}
			s.Detail = hostDetail(where, s)
			if note := p.fetchNote(strings.TrimPrefix(s.ID, name+"/")); note != "" {
				s.Detail += " · " + note
			}
			s.Pane = ""
			s.Shown = drawn && c.selected == s.ID
			s.Order = from + len(out)
			out = append(out, s)
		}
	}
	return out
}

func (c *Core) peerNames() []string {
	names := make([]string, 0, len(c.peers))
	for n := range c.peers {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// peersInfo is how each peer is, for the header and the peers op. The
// caller holds c.mu.
func (c *Core) peersInfo() []ipc.PeerInfo {
	var out []ipc.PeerInfo
	for _, name := range c.peerNames() {
		p := c.peers[name]
		i := ipc.PeerInfo{Name: name, URL: p.conf.URL, Up: p.up, Since: p.since, Problem: p.problem, Sessions: len(p.rows)}
		if h := p.header; h != nil {
			i.Version, i.Load, i.Memory = h.Version, h.Load, h.Memory
		}
		switch {
		case !p.up:
			i.Refuses = "it isn't answering"
		default:
			i.Refuses = c.hooksDiffer(p)
			if i.Refuses == "" && p.header != nil && p.header.Machine != nil && p.header.Machine.Claude != "ok" {
				i.Refuses = "Claude Code isn't signed in there (claude auth login, there)"
			}
		}
		out = append(out, i)
	}
	return out
}

// peersOp answers the peers op, reading peers.json first when asked.
func (c *Core) peersOp(a ipc.PeersArgs) (ipc.PeersResult, error) {
	if a.Reload {
		if err := c.loadPeers(); err != nil {
			return ipc.PeersResult{}, err
		}
	}
	var got []ipc.PeerFetch
	if a.Fetch != "" {
		got = c.fetchNow(a.Fetch)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return ipc.PeersResult{Name: c.machineName(), Peers: c.peersInfo(), Fetched: got}, nil
}

// splitPeerID splits a peer's row id, name/id, when name is a peer's.
// The caller holds c.mu.
func (c *Core) splitPeerID(id string) (*peerState, string, bool) {
	name, rest, ok := strings.Cut(id, "/")
	if !ok || rest == "" {
		return nil, "", false
	}
	p := c.peers[name]
	return p, rest, p != nil
}

// isPeerRow reports whether id is one of the peers' rows. The caller holds
// c.mu.
func (c *Core) isPeerRow(id string) bool {
	p, rest, ok := c.splitPeerID(id)
	if !ok {
		return false
	}
	_, there := p.rows[rest]
	return there
}

// peerOps are the ops that act on one session and go on to its own core
// when it is one of the peers'. Reorder does not: a peer's rows keep its
// own order.
var peerOps = map[string]bool{
	ipc.OpShow: true, ipc.OpSend: true, ipc.OpClose: true, ipc.OpRename: true, ipc.OpPark: true,
	ipc.OpUnpark: true, ipc.OpPriority: true, ipc.OpFreeze: true, ipc.OpThaw: true, ipc.OpRate: true,
	ipc.OpResult: true, ipc.OpScreen: true, ipc.OpKeys: true, ipc.OpConversation: true, ipc.OpReorder: true,
}

// forward passes a call about one of the peers' rows on to its own core:
// such an op whose args name a session as name/id goes there with the
// peer's own id, and what comes back has its sessions' ids made this
// list's. ok says whether op was the peer's to answer.
func (c *Core) forward(op string, raw json.RawMessage) (res any, ok bool, err error) {
	if !peerOps[op] || len(raw) == 0 || !bytes.Contains(raw, []byte("/")) {
		return nil, false, nil
	}
	c.mu.Lock()
	none := len(c.peers) == 0
	c.mu.Unlock()
	if none {
		return nil, false, nil
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(raw, &args) != nil {
		return nil, false, nil
	}
	var id string
	if json.Unmarshal(args["session"], &id) != nil || !strings.Contains(id, "/") {
		return nil, false, nil
	}
	c.mu.Lock()
	p, rest, found := c.splitPeerID(id)
	var cl *peer.Client
	up := false
	if found {
		cl, up = p.client, p.up
	}
	c.mu.Unlock()
	if !found {
		return nil, false, nil
	}
	switch {
	case op == ipc.OpShow:
		// Even while its machine is away: the row shows what was last heard.
		return nil, true, c.showPeerRow(id)
	case cl == nil || !up:
		return nil, true, ipc.Errorf(ipc.CodePeerDown, "%s isn't answering just now; its sessions run on there", p.name)
	case op == ipc.OpReorder:
		return nil, true, ipc.Errorf(ipc.CodeBadArgs, "%s's sessions keep the order they have there; move them on %s", p.name, p.name)
	}
	args["session"], _ = json.Marshal(rest)
	ctx, cancel := context.WithTimeout(context.Background(), peerCallTimeout)
	defer cancel()
	var out json.RawMessage
	if err := cl.Call(ctx, op, args, &out); err != nil {
		var e *ipc.Error
		if errors.As(err, &e) {
			return nil, true, e
		}
		return nil, true, ipc.Errorf(ipc.CodePeerDown, "%s: %v", p.name, err)
	}
	return hostIDs(p.name, out), true, nil
}

// hostIDs makes the ids of the sessions in a peer's result this list's:
// a "session" object's id, and each of "sessions".
func hostIDs(name string, raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return raw
	}
	fix := func(b json.RawMessage) json.RawMessage {
		var s map[string]any
		if json.Unmarshal(b, &s) != nil {
			return b
		}
		if id, ok := s["id"].(string); ok && id != "" {
			s["id"], s["host"] = name+"/"+id, name
		}
		out, err := json.Marshal(s)
		if err != nil {
			return b
		}
		return out
	}
	if b, ok := m["session"]; ok && string(b) != "null" {
		m["session"] = fix(b)
	}
	if b, ok := m["sessions"]; ok {
		var list []json.RawMessage
		if json.Unmarshal(b, &list) == nil {
			for i := range list {
				list[i] = fix(list[i])
			}
			m["sessions"], _ = json.Marshal(list)
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// showPeerRow picks one of the peers' rows. Its terminal is on its own
// machine, so the slot draws its conversation, which the placeholder
// follows through this core.
func (c *Core) showPeerRow(id string) error {
	c.mu.Lock()
	c.selected, c.split = id, ""
	var cmds [][]string
	if slot := c.snap.SlotPane(); slot != nil && slot.Role != tmux.RolePlaceholder {
		out, err := tmux.SwapOut(c.snap, slot.ID)
		if err != nil {
			c.mu.Unlock()
			return err
		}
		cmds = out
	}
	cmds = append(cmds, c.keepCmds()...)
	_, err := c.tmux.RunList(cmds)
	if err == nil {
		err = c.refresh()
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return err
}

// subscribePeer follows one of the peers' sessions through its own core:
// its rows, its events and its conversation, with its id made this list's.
func (c *Core) subscribePeer(a ipc.SubscribeArgs) (<-chan ipc.Frame, func(), error) {
	c.mu.Lock()
	p, rest, found := c.splitPeerID(a.Session)
	var cl *peer.Client
	up := false
	if found {
		cl, up = p.client, p.up
	}
	c.mu.Unlock()
	if !found {
		return nil, nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	if cl == nil || !up {
		return nil, nil, ipc.Errorf(ipc.CodePeerDown, "%s isn't answering just now; its sessions run on there", p.name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	in, _, err := cl.Subscribe(ctx, ipc.SubscribeArgs{Session: rest, Agent: a.Agent, Conversation: a.Conversation})
	if err != nil {
		cancel()
		var e *ipc.Error
		if errors.As(err, &e) {
			return nil, nil, e
		}
		return nil, nil, ipc.Errorf(ipc.CodePeerDown, "%s: %v", p.name, err)
	}
	out := make(chan ipc.Frame)
	go func() {
		defer close(out)
		for f := range in {
			if f.T == ipc.FramePing {
				continue
			}
			hostFrame(p.name, &f)
			select {
			case out <- f:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, cancel, nil
}

// hostDetail is a peer's row's detail, which says where it runs first, so
// every client shows it: "on Asahi · running go test", or "Asahi isn't
// answering · …" while that machine is away.
func hostDetail(where string, s ipc.Session) string {
	d := s.Detail
	if d == "" {
		d = stateWords[s.State]
	}
	if d == "" {
		return where
	}
	return where + " · " + d
}

// stateWords say a row's state where it has no detail of its own.
var stateWords = map[string]string{
	ipc.StateFailed: "failed", ipc.StateExited: "exited", ipc.StateFrozen: "frozen", ipc.StatePaused: "paused",
	ipc.StateNeedsYou: "needs you", ipc.StateStalled: "stalled", ipc.StatePausing: "pausing",
	ipc.StateStarting: "starting", ipc.StateWorking: "working", ipc.StateDone: "done", ipc.StateIdle: "idle",
}

// hostFrame makes the ids in one of a peer's frames this list's.
func hostFrame(name string, f *ipc.Frame) {
	fix := func(s *ipc.Session) {
		s.ID, s.Host, s.Pane, s.Shown = name+"/"+s.ID, name, "", false
		s.Detail = hostDetail("on "+name, *s)
		if s.Parent != "" {
			s.Parent = name + "/" + s.Parent
		}
	}
	if f.Session != nil {
		s := *f.Session
		fix(&s)
		f.Session = &s
	}
	if len(f.Sessions) > 0 {
		list := slices.Clone(f.Sessions)
		for i := range list {
			fix(&list[i])
		}
		f.Sessions = list
	}
	if f.ID != "" {
		f.ID = name + "/" + f.ID
	}
	if f.Conversation != nil && f.Conversation.Session != "" {
		cv := *f.Conversation
		cv.Session = name + "/" + cv.Session
		f.Conversation = &cv
	}
}

// placeOn reads a ^name among the first line's words: the machine to start
// a task on (docs/proposals/multi-host.md §4.3). It names a peer, or this
// machine, by any unique start of its name in any case; such a word is
// taken out of the text. A ^ word that names no machine stays as typed.
// The caller holds c.mu.
func (c *Core) placeOn(text string) (p *peerState, rest string, here bool) {
	first, after, multi := strings.Cut(text, "\n")
	words := strings.Fields(first)
	for i, w := range words {
		v, ok := strings.CutPrefix(w, "^")
		if !ok || v == "" {
			continue
		}
		mine := strings.HasPrefix(strings.ToLower(c.machineName()), strings.ToLower(v))
		var hit []string
		for _, name := range c.peerNames() {
			if strings.HasPrefix(strings.ToLower(name), strings.ToLower(v)) {
				hit = append(hit, name)
			}
		}
		if len(hit)+boolInt(mine) != 1 {
			continue
		}
		line := strings.Join(slices.Delete(slices.Clone(words), i, i+1), " ")
		if multi {
			line += "\n" + after
		}
		if mine {
			return nil, line, true
		}
		return c.peers[hit[0]], line, false
	}
	return nil, text, false
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// inputOn starts what was typed on a peer: the peer reads the words itself,
// so @repo and +profile mean what they mean there.
func (c *Core) inputOn(p *peerState, a ipc.InputArgs) (ipc.InputResult, error) {
	c.mu.Lock()
	cl, up, name := p.client, p.up, p.name
	c.mu.Unlock()
	if cl == nil || !up {
		return ipc.InputResult{}, ipc.Errorf(ipc.CodePeerDown, "%s isn't answering just now, so the task didn't start anywhere", name)
	}
	a.Session, a.Outside = "", ""
	ctx, cancel := context.WithTimeout(context.Background(), peerCallTimeout)
	defer cancel()
	var raw json.RawMessage
	if err := cl.Call(ctx, ipc.OpInput, a, &raw); err != nil {
		var e *ipc.Error
		if errors.As(err, &e) {
			return ipc.InputResult{}, e
		}
		return ipc.InputResult{}, ipc.Errorf(ipc.CodePeerDown, "%s: %v; the task may not have started", name, err)
	}
	var r ipc.InputResult
	if err := json.Unmarshal(hostIDs(name, raw), &r); err != nil {
		return ipc.InputResult{}, err
	}
	if r.Text != "" {
		r.Text = "On " + name + ": " + r.Text
	}
	r.Show = false // its terminal is there
	return r, nil
}
