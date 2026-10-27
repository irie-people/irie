package core

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
)

// frameGap spaces frames of rows to a client: at most 10 a second (DESIGN
// §2.4).
const frameGap = 100 * time.Millisecond

// maxQueue is the most event frames kept for a client that reads slowly;
// later ones are dropped.
const maxQueue = 10000

// subscriber coalesces changes for one client. Between two frames it only
// remembers which rows changed; a frame then carries their latest state, as
// one upsert or remove, or as a snapshot when several rows changed. Event
// frames, for a client that asked for them, are never coalesced. A client
// that sees every session also gets the sessions outside Irie, the closed
// sessions and the history's labels, after its first snapshot and whenever
// they change. A client that
// follows one session's conversation gets all of it, then what changed.
type subscriber struct {
	c            *Core
	session      string // only this session, when set
	agent        string // only this agent's events, when set
	events       bool   // event frames too
	conversation bool   // the session's conversation frames too
	out          chan ipc.Frame
	wake         chan struct{}
	done         chan struct{}

	mu      sync.Mutex
	full    bool
	dirty   map[string]bool
	header  bool        // the header changed
	queue   []ipc.Frame // event frames not sent yet
	outside bool        // an outside frame is due
	closed  bool        // a closed frame is due
	hist    bool        // a history frame is due
	talk    bool        // a conversation frame may be due

	talkSent bool   // the conversation's first frame went out; only loop uses it
	talkSeen uint64 // the conversation's version the client has; only loop uses it
}

// Subscribe starts a stream of frames for one client (ipc.Handler).
func (c *Core) Subscribe(raw json.RawMessage) (<-chan ipc.Frame, func(), error) {
	var a ipc.SubscribeArgs
	if err := decode(raw, &a); err != nil {
		return nil, nil, err
	}
	if a.View != "" && a.View != ipc.ViewApp {
		return nil, nil, ipc.Errorf(ipc.CodeBadArgs, "unknown view %q", a.View)
	}
	if a.Conversation && a.Session == "" {
		return nil, nil, ipc.Errorf(ipc.CodeBadArgs, "a conversation needs a session")
	}
	if isHelper(a.Agent) && !helperAgent.MatchString(a.Agent) {
		return nil, nil, ipc.Errorf(ipc.CodeBadArgs, "no helper %q", a.Agent)
	}
	app := a.View == ipc.ViewApp
	sub := &subscriber{
		c: c, session: a.Session, agent: a.Agent, events: a.Events, conversation: a.Conversation,
		out: make(chan ipc.Frame), wake: make(chan struct{}, 1), done: make(chan struct{}),
		full: true, dirty: map[string]bool{}, outside: a.Session == "", closed: a.Session == "", hist: a.Session == "", talk: a.Conversation,
	}
	c.mu.Lock()
	if a.Conversation {
		x := c.sessions[a.Session]
		if x == nil {
			c.mu.Unlock()
			return nil, nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		if isHelper(a.Agent) {
			c.followHelper(x, a.Agent)
		} else {
			x.convSubs++
			c.conversationOf(x)
		}
	}
	c.subs[sub] = true
	if app {
		c.appViews++
	}
	c.mu.Unlock()
	if app {
		go c.tick()                              // zoom the slot now, not at the next tick
		go c.refreshOutside(outsideEvery, false) // someone looks now
	}
	sub.wake <- struct{}{}
	go sub.loop()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.subs, sub)
			if app {
				c.appViews--
			}
			if x := c.sessions[sub.session]; x != nil && sub.conversation {
				if isHelper(sub.agent) {
					c.unfollowHelper(x, sub.agent)
				} else {
					x.convSubs--
				}
			}
			c.mu.Unlock()
			close(sub.done)
			if app {
				go c.tick()
			}
		})
	}
	return sub.out, cancel, nil
}

// publish tells every subscriber which rows the frames touch.
func (c *Core) publish(frames []ipc.Frame) {
	if len(frames) == 0 {
		return
	}
	c.mu.Lock()
	subs := make([]*subscriber, 0, len(c.subs))
	for s := range c.subs {
		subs = append(subs, s)
	}
	c.mu.Unlock()
	for _, s := range subs {
		s.note(frames)
	}
}

func (s *subscriber) note(frames []ipc.Frame) {
	s.mu.Lock()
	n := 0
	for _, f := range frames {
		id := f.ID
		if f.Session != nil {
			id = f.Session.ID
		}
		if s.session == "" || s.session == id {
			s.dirty[id] = true
			n++
		}
	}
	s.mu.Unlock()
	if n > 0 {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// noteOutside marks an outside frame due, for a client that sees every
// session.
func (s *subscriber) noteOutside() {
	if s.session != "" {
		return
	}
	s.mu.Lock()
	s.outside = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// noteClosed marks a closed frame due, for a client that sees every session.
func (s *subscriber) noteClosed() {
	if s.session != "" {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// noteHistory marks a history frame due, for a client that sees every
// session.
func (s *subscriber) noteHistory() {
	if s.session != "" {
		return
	}
	s.mu.Lock()
	s.hist = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// noteHeader tells the subscriber that the header changed.
func (s *subscriber) noteHeader() {
	s.mu.Lock()
	s.header = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// noteConversation marks a conversation frame due.
func (s *subscriber) noteConversation() {
	s.mu.Lock()
	s.talk = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// event queues an event frame for a client that wants it. The caller holds
// s.c.mu.
func (s *subscriber) event(e event.Event) {
	if !s.events || s.session != "" && e.Session != s.session || s.agent != "" && e.Agent != s.agent {
		return
	}
	s.mu.Lock()
	if len(s.queue) < maxQueue {
		s.queue = append(s.queue, ipc.Frame{T: ipc.FrameEvent, Event: &e})
	}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *subscriber) loop() {
	defer close(s.out)
	for {
		select {
		case <-s.wake:
		case <-s.done:
			return
		}
		s.mu.Lock()
		full, dirty, header, queue, outside, closed, hist, talk := s.full, s.dirty, s.header, s.queue, s.outside, s.closed, s.hist, s.talk
		s.full, s.dirty, s.header, s.queue, s.outside, s.closed, s.hist, s.talk = false, map[string]bool{}, false, nil, false, false, false, false
		s.mu.Unlock()
		for _, f := range queue {
			select {
			case s.out <- f:
			case <-s.done:
				return
			}
		}
		// A snapshot carries the header; otherwise it has a frame of its own.
		if header && !full && s.session == "" {
			s.c.mu.Lock()
			h := s.c.headerNow()
			s.c.mu.Unlock()
			select {
			case s.out <- ipc.Frame{T: ipc.FrameHeader, Header: &h}:
			case <-s.done:
				return
			}
		}
		var frames []ipc.Frame
		if full || len(dirty) > 0 {
			frames = append(frames, s.frame(full, dirty))
		}
		if outside {
			frames = append(frames, s.outsideFrame())
		}
		if closed {
			frames = append(frames, s.closedFrame())
		}
		if hist {
			s.c.mu.Lock()
			frames = append(frames, s.c.historyFrame())
			s.c.mu.Unlock()
		}
		if talk {
			if f, ok := s.conversationFrame(); ok {
				frames = append(frames, f)
			}
		}
		if len(frames) == 0 {
			continue
		}
		for _, f := range frames {
			select {
			case s.out <- f:
			case <-s.done:
				return
			}
		}
		select {
		case <-time.After(frameGap):
		case <-s.done:
			return
		}
	}
}

func (s *subscriber) frame(full bool, dirty map[string]bool) ipc.Frame {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if !full && len(dirty) == 1 {
		for id := range dirty {
			if row, ok := s.c.published[id]; ok {
				return ipc.Frame{T: ipc.FrameUpsert, Session: &row}
			}
			return ipc.Frame{T: ipc.FrameRemove, ID: id}
		}
	}
	rows := []ipc.Session{}
	for _, row := range s.c.list() {
		if s.session == "" || s.session == row.ID {
			rows = append(rows, row)
		}
	}
	h := s.c.headerNow()
	return ipc.Frame{T: ipc.FrameSnapshot, Sessions: rows, Header: &h}
}

// outsideFrame carries the latest sessions outside Irie.
func (s *subscriber) outsideFrame() ipc.Frame {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	return ipc.Frame{T: ipc.FrameOutside, Outside: s.c.outsideList(), Problem: s.c.outside.problem}
}

// closedFrame carries the closed sessions that reopen can carry on.
func (s *subscriber) closedFrame() ipc.Frame {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	return ipc.Frame{T: ipc.FrameClosed, Closed: s.c.closedList()}
}

// conversationFrame carries the session's conversation: all of it the first
// time, then what changed since the last frame.
func (s *subscriber) conversationFrame() (ipc.Frame, bool) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	x := s.c.sessions[s.session]
	if x == nil {
		return ipc.Frame{}, false
	}
	conv := x.conv
	if isHelper(s.agent) {
		conv = nil
		if h := x.helpers[s.agent]; h != nil {
			conv = h.conv
		}
	}
	if conv == nil {
		return ipc.Frame{}, false
	}
	var cv ipc.Conversation
	ok := true
	if !s.talkSent {
		cv, s.talkSent = conv.all(resetLimit), true
	} else {
		cv, ok = conv.since(s.talkSeen)
	}
	s.talkSeen = conv.ver
	cv.Session = x.id
	if isHelper(s.agent) {
		cv.Agent = s.agent
	}
	return ipc.Frame{T: ipc.FrameConversation, Conversation: &cv}, ok
}
