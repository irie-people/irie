//go:build !nochat

package chat

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// The mentions and DMs heard as they come (docs/proposals/chat.md §4), in
// memory only: the newest mentionsKept, and how many came since the person
// last looked. They go with the core, as every message read does.

// mentionsKept is how many mentions the core keeps.
const mentionsKept = 50

// listenEvery is how often the core looks at which accounts there are, to
// start a listener for a new one and stop one whose account went.
const listenEvery = 15 * time.Second

// After a listener fails, how long it waits to try again: a minute, or ten
// after the service refused it for good, as a token it no longer takes.
const (
	listenRetry   = time.Minute
	listenRefused = 10 * time.Minute
)

type heardList struct {
	mu     sync.Mutex
	list   []ipc.ChatMention // newest first
	unseen int
}

// listener is one account's connection that hears messages as they come.
type listener struct {
	cancel context.CancelFunc
	kind   string
	user   string

	mu      sync.Mutex
	on      bool   // it hears now
	problem string // why it doesn't
}

func (l *listener) set(on bool, problem string) {
	l.mu.Lock()
	l.on, l.problem = on, problem
	l.mu.Unlock()
}

// Listen hears every account's mentions and DMs as they come while on says
// the plugin is on, until ctx ends. It looks every listenEvery at which
// accounts there are: it starts a listener for each new one that can hear,
// and stops each whose account went or changed.
func (s *Service) Listen(ctx context.Context, on func() bool) {
	s.init()
	t := time.NewTicker(listenEvery)
	defer t.Stop()
	for {
		s.listen(ctx, on())
		select {
		case <-ctx.Done():
			s.listen(ctx, false)
			return
		case <-t.C:
		}
	}
}

// listen starts and stops the listeners once, as the accounts stand now.
func (s *Service) listen(ctx context.Context, on bool) {
	want := map[string]Account{}
	if on && ctx.Err() == nil {
		for _, a := range Accounts(s.Config()) {
			if k, ok := KindOf(a.Kind); ok && k.Can.Listen {
				want[a.Name] = a
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, l := range s.listening {
		if a, ok := want[name]; !ok || a.Kind != l.kind || a.User != l.user {
			l.cancel()
			delete(s.listening, name)
		}
	}
	for name, a := range want {
		if _, ok := s.listening[name]; ok {
			continue
		}
		lctx, cancel := context.WithCancel(ctx)
		l := &listener{cancel: cancel, kind: a.Kind, user: a.User}
		s.listening[name] = l
		go s.hear(lctx, a, l)
	}
}

// hear keeps one account's listener going until ctx ends.
func (s *Service) hear(ctx context.Context, a Account, l *listener) {
	for ctx.Err() == nil {
		x, err := s.open(ctx, a)
		if err == nil {
			ln, ok := x.c.(Listener)
			if !ok {
				return
			}
			l.set(true, "")
			err = ln.Listen(ctx, a.User, func(e Event) { s.heardOne(a, e) })
			if Auth(err) {
				s.failed(x, err)
			}
		}
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, ErrNoListen) {
			// It stays so until the account changes, which starts another.
			l.set(false, "")
			return
		}
		l.set(false, Sentence(err))
		wait := listenRetry
		var r *Refused
		if errors.As(err, &r) {
			wait = listenRefused
		}
		if sleepCtx(ctx, wait) != nil {
			return
		}
	}
}

// stopListening stops account name's listener, so the next look starts
// another with what it holds now, such as a new token.
func (s *Service) stopListening(name string) {
	if l := s.listening[name]; l != nil {
		l.cancel()
		delete(s.listening, name)
	}
}

// heardOne keeps a mention or DM; one heard again, as after a resume,
// counts once.
func (s *Service) heardOne(a Account, e Event) {
	m := ipc.ChatMention{
		Account: a.Name, Channel: e.Message.Channel, ChannelName: e.ChannelName, Message: e.Message.ID,
		Author: e.Message.Author, Text: e.Message.Text, Hidden: e.Message.Hidden, DM: e.DM, You: e.You,
		Time: e.Message.Time, Link: e.Message.Link,
	}
	h := &s.heard
	h.mu.Lock()
	defer h.mu.Unlock()
	if slices.ContainsFunc(h.list, func(x ipc.ChatMention) bool { return x.Account == m.Account && x.Message == m.Message }) {
		return
	}
	h.list = append([]ipc.ChatMention{m}, h.list...)
	if len(h.list) > mentionsKept {
		h.list = h.list[:mentionsKept]
	}
	h.unseen = min(h.unseen+1, mentionsKept)
}

// mentions are the mentions heard, newest first, and how many are new.
func (s *Service) mentions() ([]ipc.ChatMention, int) {
	h := &s.heard
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.list), h.unseen
}

// seen marks the mentions seen, as the person looked at them.
func (s *Service) seen() {
	s.heard.mu.Lock()
	s.heard.unseen = 0
	s.heard.mu.Unlock()
}

// forgetMentions drops account's mentions, as it is removed.
func (s *Service) forgetMentions(account string) {
	h := &s.heard
	h.mu.Lock()
	defer h.mu.Unlock()
	n := len(h.list)
	h.list = slices.DeleteFunc(h.list, func(x ipc.ChatMention) bool { return x.Account == account })
	h.unseen = max(0, h.unseen-(n-len(h.list)))
}

// hearing says whether account name hears as messages come, and why not.
func (s *Service) hearing(name string) (bool, string) {
	s.mu.Lock()
	l := s.listening[name]
	s.mu.Unlock()
	if l == nil {
		return false, ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.on, l.problem
}
