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
// that sees every session also gets the sessions outside Irie, after its
// first snapshot and whenever they change.
type subscriber struct {
	c       *Core
	session string // only this session, when set
	agent   string // only this agent's events, when set
	events  bool   // event frames too
	out     chan ipc.Frame
	wake    chan struct{}
	done    chan struct{}

	mu      sync.Mutex
	full    bool
	dirty   map[string]bool
	header  bool        // the header changed
	queue   []ipc.Frame // event frames not sent yet
	outside bool        // an outside frame is due
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
	app := a.View == ipc.ViewApp
	sub := &subscriber{
		c: c, session: a.Session, agent: a.Agent, events: a.Events,
		out: make(chan ipc.Frame), wake: make(chan struct{}, 1), done: make(chan struct{}),
		full: true, dirty: map[string]bool{}, outside: a.Session == "",
	}
	c.mu.Lock()
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
		full, dirty, header, queue, outside := s.full, s.dirty, s.header, s.queue, s.outside
		s.full, s.dirty, s.header, s.queue, s.outside = false, map[string]bool{}, false, nil, false
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
