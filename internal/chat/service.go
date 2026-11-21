//go:build !nochat

package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// How long what the core read stays fresh: a look sooner answers from
// memory, so a tab that looks every few seconds asks the service at most
// twice a minute.
const (
	spacesFresh = 5 * time.Minute
	pageFresh   = 30 * time.Second
)

// pageSize is how many messages a read brings unless asked for another
// number.
const pageSize = 50

// Service is the chat plugin's part of the core: each account's connector
// and what it last read, in memory, and the agents' drafts and log, on
// disk. Do answers the chat op, and holds every call to the rules of
// docs/proposals/chat.md §5.
type Service struct {
	Store Store             // the tokens
	Dir   string            // <state>/chat: drafts.json and log.jsonl
	Bases map[string]string // a fake service's address for a kind, in tests
	HTTP  *http.Client      // nil = one with Wait as its timeout
	Now   func() time.Time  // nil = time.Now
	// Config is the chat settings as config.toml has them now, and
	// Configure writes settings to it, as the config op does.
	Config    func() config.Chat
	Configure func(sets []ipc.ConfigSet) error

	once   sync.Once
	mu     sync.Mutex
	conns  map[string]*conn
	drafts *drafts
	log    *agentLog
	// posting holds one agent's post in a channel at a time, so two at
	// once can't both find room in its count.
	posting map[string]*sync.Mutex
}

// Caller is who asks: the person, or an agent and its session.
type Caller struct {
	Agent   bool
	Session string // the agent's session ID
	Name    string // its name now
}

type conn struct {
	signin sync.Mutex // one sign-in at a time
	mu     sync.Mutex // the rest
	kind   string
	c      Connector
	me     Me
	state  string
	why    string
	spaces []ipc.ChatSpace
	at     time.Time // when spaces were read
	pages  map[string]page
}

type page struct {
	msgs []ipc.ChatMessage
	at   time.Time
}

func (s *Service) init() {
	s.once.Do(func() {
		if s.Now == nil {
			s.Now = time.Now
		}
		if s.HTTP == nil {
			s.HTTP = &http.Client{Timeout: Wait}
		}
		s.conns = map[string]*conn{}
		s.posting = map[string]*sync.Mutex{}
		s.drafts = &drafts{path: filepath.Join(s.Dir, "drafts.json"), now: s.Now}
		s.log = &agentLog{path: filepath.Join(s.Dir, "log.jsonl"), now: s.Now}
	})
}

// refuse is a refusal of what an agent asked, in words.
func refuse(format string, a ...any) error { return ipc.Errorf(ipc.CodeBadArgs, format, a...) }

// Do answers the chat op for who.
func (s *Service) Do(ctx context.Context, who Caller, a ipc.ChatArgs) (ipc.ChatResult, error) {
	s.init()
	cfg := s.Config()
	if who.Agent && !cfg.Agents {
		return ipc.ChatResult{}, refuse("Chat is closed to agents: chat.agents is off. Tell your person what you would post instead.")
	}
	all := Accounts(cfg)
	switch a.Do {
	case ipc.ChatList:
		return s.result(who, cfg, ""), nil
	case ipc.ChatChannels:
		return s.channels(ctx, who, cfg, all, a)
	case ipc.ChatRead:
		return s.read(ctx, who, cfg, all, a)
	case ipc.ChatPost:
		return s.post(ctx, who, cfg, all, a)
	case ipc.ChatEdit, ipc.ChatDelete:
		return s.change(ctx, who, cfg, all, a)
	case ipc.ChatSend:
		if who.Agent {
			return ipc.ChatResult{}, refuse("Only the person sends a draft: it waits for them in %s's Chat.", brand.Name)
		}
		return s.send(ctx, cfg, all, a)
	case ipc.ChatDiscard:
		return s.discard(who, cfg, a)
	case ipc.ChatAgents:
		if who.Agent {
			return ipc.ChatResult{}, refuse("Only the person says what agents may do in a channel.")
		}
		return s.agents(ctx, all, a)
	case ipc.ChatLog:
		r := s.result(who, cfg, "")
		limit := a.Limit
		if limit <= 0 {
			limit = 50
		}
		for _, e := range s.log.read() {
			if len(r.Log) == limit {
				break
			}
			if (who.Agent && e.Session != who.Session) || (a.Account != "" && e.Account != a.Account) {
				continue
			}
			r.Log = append(r.Log, e)
		}
		r.Text = LogText(r.Log)
		return r, nil
	case ipc.ChatManifest:
		r := s.result(who, cfg, "")
		r.Manifest = SlackManifest()
		r.Text = SlackSteps()
		return r, nil
	case ipc.ChatSeen:
		if who.Agent {
			return ipc.ChatResult{}, refuse("Mentions are the person's.")
		}
		return s.result(who, cfg, ""), nil
	case ipc.ChatAdd, ipc.ChatRemove, ipc.ChatTest:
		if who.Agent {
			return ipc.ChatResult{}, refuse("Only the person adds, checks or removes a chat account, and its token never reaches an agent.")
		}
		switch a.Do {
		case ipc.ChatAdd:
			return s.add(ctx, all, a.New)
		case ipc.ChatRemove:
			return s.remove(all, a.Account)
		}
		return s.test(ctx, all, a.Account)
	}
	return ipc.ChatResult{}, refuse("The chat op has no %q: channels, read, post, edit, delete, send, discard, agents, log, seen, add, remove, test or manifest.", a.Do)
}

// result is what every answer holds: the accounts and the drafts, an
// agent's own drafts only.
func (s *Service) result(who Caller, cfg config.Chat, text string) ipc.ChatResult {
	session := ""
	if who.Agent {
		session = who.Session
	}
	r := ipc.ChatResult{Accounts: s.Accounts(Accounts(cfg)), Drafts: s.drafts.all(session), Text: text}
	if r.Text == "" {
		r.Text = Summary(r)
	}
	return r
}

// Accounts are the accounts, each as the core's last look found it: it
// asks no service.
func (s *Service) Accounts(all []Account) []ipc.ChatAccount {
	s.init()
	out := []ipc.ChatAccount{}
	for _, a := range all {
		k, _ := KindOf(a.Kind)
		w := ipc.ChatAccount{Name: a.Name, Kind: a.Kind, Service: ServiceName(a.Kind), Colour: a.Colour, State: ipc.ChatUnknown, Agents: a.Agents, AgentsRead: a.AgentsRead, Can: k.Can}
		if w.Agents == "" {
			w.Agents = config.ChatAgentsDraft
		}
		if k.New == nil {
			w.State, w.Problem = ipc.ChatError, ServiceName(a.Kind)+" isn't in this build of "+brand.Name+"."
		}
		s.mu.Lock()
		x := s.conns[a.Name]
		s.mu.Unlock()
		if x != nil {
			x.mu.Lock()
			if x.state != "" {
				w.State, w.Problem = x.state, x.why
			}
			w.Bot, w.BotID, w.Hidden, w.Fix = x.me.Name, x.me.ID, x.me.Hidden, x.me.Fix
			x.mu.Unlock()
		}
		out = append(out, w)
	}
	return out
}

// Forget drops what the core holds of account name, so the next call signs
// in again: after its token changed, or it was removed.
func (s *Service) Forget(name string) {
	s.init()
	s.mu.Lock()
	delete(s.conns, name)
	s.mu.Unlock()
}

// open is account a's connection, signed in: once, then again only after a
// look failed.
func (s *Service) open(ctx context.Context, a Account) (*conn, error) {
	s.mu.Lock()
	x := s.conns[a.Name]
	if x == nil || x.kind != a.Kind {
		x = &conn{kind: a.Kind, pages: map[string]page{}}
		s.conns[a.Name] = x
	}
	s.mu.Unlock()
	x.signin.Lock()
	defer x.signin.Unlock()
	x.mu.Lock()
	ok := x.c != nil && x.state == ipc.ChatOK
	x.mu.Unlock()
	if ok {
		return x, nil
	}
	fail := func(state string, err error) (*conn, error) {
		x.mu.Lock()
		x.c, x.state, x.why = nil, state, Sentence(err)
		x.mu.Unlock()
		return nil, err
	}
	k, found := KindOf(a.Kind)
	if !found {
		return fail(ipc.ChatError, refuse("%s isn't in this build of %s.", ServiceName(a.Kind), brand.Name))
	}
	bot, err := s.Store.Get(a.Name)
	if errors.Is(err, ErrNoToken) {
		return fail(ipc.ChatNeeds, refuse("%s has no token kept: `%s chat add %s %s` takes one.", a.Name, brand.Slug, a.Kind, a.Name))
	} else if err != nil {
		return fail(ipc.ChatError, fmt.Errorf("the Keychain: %v", err))
	}
	app, err := s.Store.Get(AppKey(a.Name))
	if err != nil && !errors.Is(err, ErrNoToken) {
		return fail(ipc.ChatError, fmt.Errorf("the Keychain: %v", err))
	}
	c := k.New(Tokens{Bot: bot, App: app}, s.Bases[a.Kind], s.HTTP)
	me, err := c.Me(ctx)
	if err != nil {
		return fail(ipc.ChatError, err)
	}
	x.mu.Lock()
	x.c, x.me, x.state, x.why = c, me, ipc.ChatOK, ""
	x.mu.Unlock()
	return x, nil
}

// failed notes on a's connection that a call failed because the service no
// longer takes its token.
func (s *Service) failed(x *conn, err error) error {
	if Auth(err) {
		x.mu.Lock()
		x.c, x.state, x.why = nil, ipc.ChatError, Sentence(err)
		x.mu.Unlock()
	}
	return err
}

// Sentence is an error as the sentence a person reads.
func Sentence(err error) string {
	if err == nil {
		return ""
	}
	var ie *ipc.Error
	if errors.As(err, &ie) {
		return ie.Msg
	}
	s := strings.TrimSpace(err.Error())
	if s != "" && !strings.ContainsAny(s[len(s)-1:], ".!?:") {
		s += "."
	}
	return s
}

// spaces are account a's servers and channels, from memory while fresh,
// each channel with what agents may do in it now.
func (s *Service) spaces(ctx context.Context, a Account, refresh bool) (*conn, []ipc.ChatSpace, error) {
	x, err := s.open(ctx, a)
	if err != nil {
		return nil, nil, err
	}
	x.mu.Lock()
	sp, at := x.spaces, x.at
	x.mu.Unlock()
	if refresh || sp == nil || s.Now().Sub(at) > spacesFresh {
		sp, err = x.c.Spaces(ctx)
		if err != nil {
			return nil, nil, s.failed(x, err)
		}
		x.mu.Lock()
		x.spaces, x.at = sp, s.Now()
		x.mu.Unlock()
	}
	out := make([]ipc.ChatSpace, len(sp))
	for i, sc := range sp {
		sc.Account = a.Name
		sc.Channels = slices.Clone(sc.Channels)
		for j := range sc.Channels {
			sc.Channels[j].Account = a.Name
			sc.Channels[j].Agents = a.AgentsIn(sc.Channels[j].ID)
		}
		out[i] = sc
	}
	return x, out, nil
}

// split takes ACCOUNT: off the front of a channel's words.
func split(account, ref string) (string, string) {
	if account == "" {
		if i := strings.Index(ref, ":"); i > 0 && config.ChatName.MatchString(ref[:i]) {
			return ref[:i], ref[i+1:]
		}
	}
	return account, ref
}

// resolve finds the channel ref names in account's servers: by its ID, by
// #name, or by SERVER/#name where two servers share a name.
func (s *Service) resolve(ctx context.Context, all []Account, account, ref string) (Account, *conn, ipc.ChatChannel, error) {
	account, ref = split(account, strings.TrimSpace(ref))
	a, err := Find(all, account)
	if err != nil {
		return Account{}, nil, ipc.ChatChannel{}, err
	}
	if ref == "" {
		return Account{}, nil, ipc.ChatChannel{}, refuse("Which channel? Give its #name or its ID: `%s chat channels %s` lists them.", brand.Slug, a.Name)
	}
	for try := 0; try < 2; try++ {
		x, sp, err := s.spaces(ctx, a, try > 0)
		if err != nil {
			return Account{}, nil, ipc.ChatChannel{}, err
		}
		server, name := "", strings.TrimPrefix(ref, "#")
		if i := strings.LastIndex(ref, "/"); i > 0 {
			server, name = strings.TrimSpace(ref[:i]), strings.TrimPrefix(strings.TrimSpace(ref[i+1:]), "#")
		}
		var found []ipc.ChatChannel
		var where []string
		for _, sc := range sp {
			for _, ch := range sc.Channels {
				if ch.ID == ref {
					return a, x, ch, nil
				}
				if strings.EqualFold(ch.Name, name) && (server == "" || strings.EqualFold(sc.Name, server)) {
					found = append(found, ch)
					where = append(where, sc.Name+"/#"+ch.Name+" ("+ch.ID+")")
				}
			}
		}
		// A channel beats a thread of the same name.
		if len(found) > 1 {
			var channels []ipc.ChatChannel
			for _, ch := range found {
				if ch.Kind != ipc.ChannelThread {
					channels = append(channels, ch)
				}
			}
			if len(channels) == 1 {
				found = channels
			}
		}
		switch {
		case len(found) == 1:
			return a, x, found[0], nil
		case len(found) > 1:
			return Account{}, nil, ipc.ChatChannel{}, ipc.Errorf(ipc.CodeAmbiguous, "More than one channel is called #%s: say which, as one of %s.", name, strings.Join(where, ", "))
		}
		// An ID the lists lack, such as a DM's or an archived thread's,
		// the service may still know.
		if try == 0 && config.ChatID.MatchString(ref) {
			if g, ok := x.c.(interface {
				Channel(context.Context, string) (ipc.ChatChannel, error)
			}); ok {
				if ch, err := g.Channel(ctx, ref); err == nil {
					ch.Account, ch.Agents = a.Name, a.AgentsIn(ch.ID)
					return a, x, ch, nil
				}
			}
		}
	}
	return Account{}, nil, ipc.ChatChannel{}, ipc.Errorf(ipc.CodeNotFound, "%s has no channel %s that its bot can see: `%s chat channels %s` lists them.", a.Name, ref, brand.Slug, a.Name)
}

func (s *Service) channels(ctx context.Context, who Caller, cfg config.Chat, all []Account, a ipc.ChatArgs) (ipc.ChatResult, error) {
	acct, err := Find(all, a.Account)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	if who.Agent && acct.Agents == config.ChatAgentsNone {
		return ipc.ChatResult{}, refuse("%s is closed to agents: its person set agents = \"none\".", acct.Name)
	}
	_, sp, err := s.spaces(ctx, acct, a.Refresh)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	if who.Agent && !acct.AgentsRead {
		// Topics are people's words too.
		for i := range sp {
			for j := range sp[i].Channels {
				sp[i].Channels[j].Topic = ""
			}
		}
	}
	r := s.result(who, cfg, "")
	r.Spaces = sp
	r.Text = ChannelsText(acct.Name, sp)
	return r, nil
}

func (s *Service) read(ctx context.Context, who Caller, cfg config.Chat, all []Account, a ipc.ChatArgs) (ipc.ChatResult, error) {
	account, _ := split(a.Account, a.Channel)
	if who.Agent {
		acct, err := Find(all, account)
		if err != nil {
			return ipc.ChatResult{}, err
		}
		if !acct.AgentsRead {
			return ipc.ChatResult{}, refuse("Agents may not read %s's messages: its person hasn't let them (agents_read). Ask them for what you need, or write a draft.", acct.Name)
		}
	}
	acct, x, ch, err := s.resolve(ctx, all, a.Account, a.Channel)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	limit := a.Limit
	if limit <= 0 {
		limit = pageSize
	}
	limit = min(limit, 100)
	key := ch.ID + fmt.Sprint("/", limit, "/", a.Thread)
	var msgs []ipc.ChatMessage
	x.mu.Lock()
	p, fresh := x.pages[key]
	x.mu.Unlock()
	if a.Before == "" && !a.Refresh && fresh && s.Now().Sub(p.at) < pageFresh {
		msgs = p.msgs
	} else {
		msgs, err = x.c.Messages(ctx, ch, Page{Before: a.Before, Limit: limit, Thread: a.Thread})
		if err != nil {
			return ipc.ChatResult{}, s.failed(x, err)
		}
		if a.Before == "" {
			x.mu.Lock()
			x.pages[key] = page{msgs: msgs, at: s.Now()}
			x.mu.Unlock()
		}
	}
	// Oldest first, as a chat shows them, each an agent posted named by
	// its session.
	out := slices.Clone(msgs)
	slices.Reverse(out)
	posted := map[string]string{}
	for _, e := range s.log.read() {
		if e.Account == acct.Name && e.Do == LogPost {
			posted[e.Message] = cmpOr(e.SessionName, e.Session)
		}
	}
	hidden := false
	for i := range out {
		out[i].Agent = posted[out[i].ID]
		hidden = hidden || out[i].Hidden
	}
	r := s.result(who, cfg, "")
	r.Channel, r.Messages, r.More = &ch, out, len(msgs) == limit
	if hidden {
		x.mu.Lock()
		r.Note = x.me.Fix
		x.mu.Unlock()
	}
	r.Text = MessagesText(ch, out, r.Note, who.Agent)
	return r, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// forget drops channel's newest page, after a post or a change in it.
func (x *conn) forget(channel string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for k := range x.pages {
		if strings.HasPrefix(k, channel+"/") {
			delete(x.pages, k)
		}
	}
}

// check says what is wrong with an agent's words for kind before they go
// anywhere, a draft included.
func check(kind, text string) error {
	if strings.TrimSpace(text) == "" {
		return refuse("Post what? The text is empty.")
	}
	if w := Everyone(kind, text); w != "" {
		return refuse("Agents never ping everyone, here, a role or a group: take %q out. Name the people the post is for, or ask your person to post it.", w)
	}
	if k, ok := KindOf(kind); ok && k.MaxText > 0 && count(text) > k.MaxText {
		return refuse("%s takes at most %d characters a message, and this has %d: make it shorter, or two posts.", k.Service, k.MaxText, count(text))
	}
	return nil
}

func (s *Service) post(ctx context.Context, who Caller, cfg config.Chat, all []Account, a ipc.ChatArgs) (ipc.ChatResult, error) {
	if who.Agent {
		account, _ := split(a.Account, a.Channel)
		if acct, err := Find(all, account); err == nil && acct.Agents == config.ChatAgentsNone {
			return ipc.ChatResult{}, refuse("%s is closed to agents: its person set agents = \"none\".", acct.Name)
		}
	}
	acct, x, ch, err := s.resolve(ctx, all, a.Account, a.Channel)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	p := Post{Channel: ch, Text: strings.TrimRight(a.Text, "\n"), Reply: a.Reply, Thread: a.Thread}
	if !who.Agent {
		// The person's own post goes at once, as they typed it.
		p.Pings = PingAsTyped
		m, err := x.c.Post(ctx, p)
		if err != nil {
			return ipc.ChatResult{}, s.failed(x, err)
		}
		x.forget(ch.ID)
		r := s.result(who, cfg, "Posted in #"+ch.Name+".")
		r.Message = &m
		return r, nil
	}
	if err := check(acct.Kind, p.Text); err != nil {
		return ipc.ChatResult{}, err
	}
	mode := acct.AgentsIn(ch.ID)
	if mode == config.ChatAgentsNone {
		return ipc.ChatResult{}, refuse("#%s is closed to agents: its person set agents = \"none\" there.", ch.Name)
	}
	if mode == config.ChatAgentsDraft || ch.Kind == ipc.ChannelDM || ch.Kind == ipc.ChannelGroup {
		// A draft: the person sends it or not. Agents never write to a
		// person on their own.
		d, err := s.drafts.add(ipc.ChatDraft{
			Account: acct.Name, Channel: ch.ID, ChannelName: ch.Name, Space: s.spaceName(x, ch.Space),
			Reply: p.Reply, Thread: Line(p.Thread, 100), Text: p.Text, Session: who.Session, SessionName: who.Name,
		})
		if err != nil {
			return ipc.ChatResult{}, err
		}
		r := s.result(who, cfg, fmt.Sprintf("Draft %s waits for your person to send or discard it, in %s's Chat or with `%s chat send %s`. `%s chat drafts` says what came of it.", d.ID, brand.Name, brand.Slug, d.ID, brand.Slug))
		r.Draft = &d
		return r, nil
	}
	// The channel lets agents post on their own: one at a time, within
	// the count, pinging no one, and logged.
	m, err := s.agentDo(ctx, who, cfg, acct, x, ch, func() (ipc.ChatMessage, error) {
		p.Pings = PingNone
		return x.c.Post(ctx, p)
	}, LogPost, p.Text)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	r := s.result(who, cfg, "Posted in #"+ch.Name+": "+m.Link)
	r.Message = &m
	return r, nil
}

// agentDo makes an agent's own post or edit in a channel that lets agents
// post: one in the channel at a time, within its count, then logged.
func (s *Service) agentDo(ctx context.Context, who Caller, cfg config.Chat, acct Account, x *conn, ch ipc.ChatChannel, do func() (ipc.ChatMessage, error), what, text string) (ipc.ChatMessage, error) {
	key := acct.Name + "/" + ch.ID
	s.mu.Lock()
	lock := s.posting[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.posting[key] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	if err := s.log.room(acct.Name, ch.ID, ch.Name, cfg.AgentPosts); err != nil {
		return ipc.ChatMessage{}, err
	}
	m, err := do()
	if err != nil {
		return ipc.ChatMessage{}, s.failed(x, err)
	}
	x.forget(ch.ID)
	if err := s.log.add(ipc.ChatLogEntry{
		Time: s.Now(), Session: who.Session, SessionName: who.Name, Account: acct.Name,
		Channel: ch.ID, ChannelName: ch.Name, Do: what, Message: m.ID, Link: m.Link, Text: text,
	}); err != nil {
		return m, fmt.Errorf("it went out, but the log didn't keep it: %v", err)
	}
	return m, nil
}

// spaceName is the name of a server the connection has read.
func (s *Service) spaceName(x *conn, id string) string {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, sc := range x.spaces {
		if sc.ID == id {
			return sc.Name
		}
	}
	return ""
}

// change edits or deletes a message: any the bot may, for the person; for
// an agent, only its own posts in a channel that lets agents post.
func (s *Service) change(ctx context.Context, who Caller, cfg config.Chat, all []Account, a ipc.ChatArgs) (ipc.ChatResult, error) {
	if a.ID == "" {
		return ipc.ChatResult{}, refuse("Which message? Its ID is missing.")
	}
	acct, x, ch, err := s.resolve(ctx, all, a.Account, a.Channel)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	text := strings.TrimRight(a.Text, "\n")
	if !who.Agent {
		if a.Do == ipc.ChatDelete {
			if err := x.c.Delete(ctx, ch, a.ID); err != nil {
				return ipc.ChatResult{}, s.failed(x, err)
			}
			x.forget(ch.ID)
			return s.result(who, cfg, "Deleted."), nil
		}
		m, err := x.c.Edit(ctx, ch, a.ID, text, PingAsTyped)
		if err != nil {
			return ipc.ChatResult{}, s.failed(x, err)
		}
		x.forget(ch.ID)
		r := s.result(who, cfg, "Changed.")
		r.Message = &m
		return r, nil
	}
	if acct.AgentsIn(ch.ID) != config.ChatAgentsPost {
		return ipc.ChatResult{}, refuse("Agents change only their own posts, in a channel that lets them post; #%s doesn't. Write a draft for your person instead.", ch.Name)
	}
	own := slices.ContainsFunc(s.log.read(), func(e ipc.ChatLogEntry) bool {
		return e.Account == acct.Name && e.Message == a.ID && e.Do == LogPost && e.Session == who.Session && e.Draft == ""
	})
	if !own {
		return ipc.ChatResult{}, refuse("An agent changes only what it posted itself, and %s isn't a post of this session's.", a.ID)
	}
	if a.Do == ipc.ChatDelete {
		if err := x.c.Delete(ctx, ch, a.ID); err != nil {
			return ipc.ChatResult{}, s.failed(x, err)
		}
		x.forget(ch.ID)
		if err := s.log.add(ipc.ChatLogEntry{Time: s.Now(), Session: who.Session, SessionName: who.Name, Account: acct.Name, Channel: ch.ID, ChannelName: ch.Name, Do: LogDelete, Message: a.ID}); err != nil {
			return ipc.ChatResult{}, fmt.Errorf("it was deleted, but the log didn't keep it: %v", err)
		}
		return s.result(who, cfg, "Deleted."), nil
	}
	if err := check(acct.Kind, text); err != nil {
		return ipc.ChatResult{}, err
	}
	m, err := s.agentDo(ctx, who, cfg, acct, x, ch, func() (ipc.ChatMessage, error) {
		return x.c.Edit(ctx, ch, a.ID, text, PingNone)
	}, LogEdit, text)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	r := s.result(who, cfg, "Changed.")
	r.Message = &m
	return r, nil
}

// send sends a draft at the person's word, with their changes: it pings the
// people it names, never more, and the log keeps it as an agent's words
// the person sent.
func (s *Service) send(ctx context.Context, cfg config.Chat, all []Account, a ipc.ChatArgs) (ipc.ChatResult, error) {
	d, err := s.drafts.get(a.ID)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	if d.State != ipc.DraftWaiting {
		return ipc.ChatResult{}, refuse("Draft %s was %s already.", d.ID, d.State)
	}
	text := d.Text
	if t := strings.TrimRight(a.Text, "\n"); strings.TrimSpace(t) != "" {
		text = t
	}
	acct, x, ch, err := s.resolve(ctx, all, d.Account, d.Channel)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	m, err := x.c.Post(ctx, Post{Channel: ch, Text: text, Reply: d.Reply, Thread: d.Thread, Pings: PingPeople})
	if err != nil {
		// It still waits, with why it didn't go.
		s.drafts.change(d.ID, func(x *ipc.ChatDraft) error { x.Error = Sentence(err); return nil })
		return ipc.ChatResult{}, s.failed(x, err)
	}
	x.forget(ch.ID)
	logErr := s.log.add(ipc.ChatLogEntry{
		Time: s.Now(), Session: d.Session, SessionName: d.SessionName, Account: acct.Name,
		Channel: ch.ID, ChannelName: ch.Name, Do: LogPost, Message: m.ID, Link: m.Link, Text: text, Draft: d.ID,
	})
	d, err = s.drafts.change(d.ID, func(x *ipc.ChatDraft) error {
		x.State, x.Text, x.Link, x.Error, x.Ended, x.By = ipc.DraftSent, text, m.Link, "", s.Now(), "you"
		return nil
	})
	if err != nil {
		return ipc.ChatResult{}, fmt.Errorf("it went out, but the draft wasn't marked sent: %v", err)
	}
	r := s.result(Caller{}, cfg, "Sent to #"+ch.Name+".")
	if logErr != nil {
		r.Text += " The log didn't keep it: " + Sentence(logErr)
	}
	r.Message, r.Draft = &m, &d
	return r, nil
}

// discard drops a draft: the person's word, or its own agent's while it
// waits.
func (s *Service) discard(who Caller, cfg config.Chat, a ipc.ChatArgs) (ipc.ChatResult, error) {
	by := "you"
	if who.Agent {
		by = "agent"
	}
	d, err := s.drafts.change(a.ID, func(x *ipc.ChatDraft) error {
		if who.Agent && x.Session != who.Session {
			return refuse("An agent discards only its own drafts.")
		}
		if x.State != ipc.DraftWaiting {
			return refuse("Draft %s was %s already.", x.ID, x.State)
		}
		x.State, x.Ended, x.By = ipc.DraftDiscarded, s.Now(), by
		return nil
	})
	if err != nil {
		return ipc.ChatResult{}, err
	}
	r := s.result(who, cfg, "Discarded draft "+d.ID+".")
	r.Draft = &d
	return r, nil
}

// agents sets, at the person's word, what agents may do in a channel, or
// whether they may read an account.
func (s *Service) agents(ctx context.Context, all []Account, a ipc.ChatArgs) (ipc.ChatResult, error) {
	var sets []ipc.ConfigSet
	var said []string
	if a.Channel != "" {
		switch a.Agents {
		case config.ChatAgentsNone, config.ChatAgentsDraft, config.ChatAgentsPost:
		default:
			return ipc.ChatResult{}, refuse("Agents may do none, draft or post in a channel, not %q.", a.Agents)
		}
		acct, _, ch, err := s.resolve(ctx, all, a.Account, a.Channel)
		if err != nil {
			return ipc.ChatResult{}, err
		}
		key := "chat.accounts." + acct.Name + ".channels." + ch.ID
		sets = append(sets, ipc.ConfigSet{Key: key + ".agents", Value: jsonValue(a.Agents)}, ipc.ConfigSet{Key: key + ".name", Value: jsonValue("#" + ch.Name)})
		said = append(said, map[string]string{
			config.ChatAgentsNone:  "Agents do nothing in #" + ch.Name + " now.",
			config.ChatAgentsDraft: "Agents write drafts for you in #" + ch.Name + " now.",
			config.ChatAgentsPost:  fmt.Sprintf("Agents post on their own in #%s now: at most one a minute and %d an hour, never pinging anyone, each in `%s chat log`.", ch.Name, s.Config().AgentPosts, brand.Slug),
		}[a.Agents])
		a.Account = acct.Name
	}
	if a.Read != nil {
		account, _ := split(a.Account, a.Channel)
		acct, err := Find(all, account)
		if err != nil {
			return ipc.ChatResult{}, err
		}
		sets = append(sets, ipc.ConfigSet{Key: "chat.accounts." + acct.Name + ".agents_read", Value: jsonValue(*a.Read)})
		if *a.Read {
			said = append(said, "Agents may read "+acct.Name+"'s messages now.")
		} else {
			said = append(said, "Agents may not read "+acct.Name+"'s messages now.")
		}
	}
	if len(sets) == 0 {
		return ipc.ChatResult{}, refuse("Say what agents may do: in a channel (none, draft or post), or whether they may read an account.")
	}
	if err := s.Configure(sets); err != nil {
		return ipc.ChatResult{}, err
	}
	return s.result(Caller{}, s.Config(), strings.Join(said, " ")), nil
}

func jsonValue(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// add adds an account once its token signs in; for an account of the same
// kind already called that, it takes the new token.
func (s *Service) add(ctx context.Context, all []Account, n *ipc.ChatNew) (ipc.ChatResult, error) {
	if n == nil {
		return ipc.ChatResult{}, refuse("Add which account? Its kind and token are missing.")
	}
	k, ok := KindOf(n.Kind)
	switch {
	case !ok && slices.Contains(config.ChatKinds, n.Kind):
		return ipc.ChatResult{}, refuse("%s isn't in this build of %s yet.", strings.ToUpper(n.Kind[:1])+n.Kind[1:], brand.Name)
	case !ok:
		return ipc.ChatResult{}, refuse("%q is no kind of chat account: %s.", n.Kind, strings.Join(config.ChatKinds, " or "))
	}
	t, err := k.Tidy(Tokens{Bot: strings.TrimSpace(n.Token), App: strings.TrimSpace(n.App)})
	if err != nil {
		return ipc.ChatResult{}, refuse("%s", Sentence(err))
	}
	if n.User != "" && !config.ChatID.MatchString(n.User) {
		return ipc.ChatResult{}, refuse("%q isn't a user ID: letters and digits.", n.User)
	}
	c := k.New(t, s.Bases[n.Kind], s.HTTP)
	me, err := c.Me(ctx)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	if v, ok := c.(interface{ CheckApp(context.Context) error }); ok && t.App != "" {
		if err := v.CheckApp(ctx); err != nil {
			return ipc.ChatResult{}, err
		}
	}
	name := strings.TrimSpace(n.Name)
	taken := map[string]string{}
	for _, a := range all {
		taken[a.Name] = a.Kind
	}
	if name == "" {
		name = NameFor(me.Name, n.Kind, taken)
		// The same bot again takes its account's new token.
		for _, a := range all {
			if a.Kind == n.Kind && s.botID(a.Name) == me.ID {
				name = a.Name
			}
		}
	}
	if !config.ChatName.MatchString(name) {
		return ipc.ChatResult{}, refuse("%q can't name an account: lower case letters, digits and -, at most 24.", name)
	}
	kind, exists := taken[name]
	if exists && kind != n.Kind {
		return ipc.ChatResult{}, refuse("%s is already a %s account: give this one another name.", name, ServiceName(kind))
	}
	if err := s.Store.Set(name, t.Bot); err != nil {
		return ipc.ChatResult{}, fmt.Errorf("the Keychain: %v", err)
	}
	if t.App != "" {
		if err := s.Store.Set(AppKey(name), t.App); err != nil {
			return ipc.ChatResult{}, fmt.Errorf("the Keychain: %v", err)
		}
	}
	key := "chat.accounts." + name
	sets := []ipc.ConfigSet{{Key: key + ".kind", Value: jsonValue(n.Kind)}}
	if n.User != "" {
		sets = append(sets, ipc.ConfigSet{Key: key + ".user", Value: jsonValue(n.User)})
	}
	if err := s.Configure(sets); err != nil {
		return ipc.ChatResult{}, err
	}
	s.Forget(name)
	text := fmt.Sprintf("Added %s (%s, as %s).", name, k.Service, me.Name)
	if exists {
		text = fmt.Sprintf("Took the new token for %s (%s, as %s).", name, k.Service, me.Name)
	}
	if me.Hidden {
		text += " " + me.Fix
	}
	if a, err := Find(Accounts(s.Config()), name); err == nil {
		s.open(ctx, a)
	}
	return s.result(Caller{}, s.Config(), text), nil
}

// NameFor is a short name for an account from its bot's name, one no other
// account has: Acme Helper's bot is acme-helper.
func NameFor(bot, kind string, taken map[string]string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(bot) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	base := strings.Trim(b.String(), "-")
	if len(base) > 20 {
		base = strings.TrimRight(base[:20], "-")
	}
	if base == "" {
		base = kind
	}
	name := base
	for i := 2; ; i++ {
		if _, ok := taken[name]; !ok {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// botID is the bot's user ID that account name's last look found, or "".
func (s *Service) botID(name string) string {
	s.mu.Lock()
	x := s.conns[name]
	s.mu.Unlock()
	if x == nil {
		return ""
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.me.ID
}

// remove removes an account from config.toml with its tokens and drafts.
// The log keeps what agents posted in it.
func (s *Service) remove(all []Account, name string) (ipc.ChatResult, error) {
	a, err := Find(all, name)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	if err := s.Configure([]ipc.ConfigSet{{Key: "chat.accounts." + a.Name, Value: json.RawMessage("null")}}); err != nil {
		return ipc.ChatResult{}, err
	}
	s.Forget(a.Name)
	var problems []string
	for _, key := range []string{a.Name, AppKey(a.Name)} {
		if err := s.Store.Delete(key); err != nil {
			problems = append(problems, Sentence(err))
		}
	}
	if err := s.drafts.forget(a.Name); err != nil {
		problems = append(problems, Sentence(err))
	}
	text := "Removed " + a.Name + "."
	if len(problems) > 0 {
		text += " " + strings.Join(problems, " ")
	}
	return s.result(Caller{}, s.Config(), text), nil
}

// test signs in to an account again.
func (s *Service) test(ctx context.Context, all []Account, name string) (ipc.ChatResult, error) {
	a, err := Find(all, name)
	if err != nil {
		return ipc.ChatResult{}, err
	}
	s.Forget(a.Name)
	_, err = s.open(ctx, a)
	r := s.result(Caller{}, s.Config(), "")
	if err != nil {
		r.Text = a.Name + ": " + Sentence(err)
		return r, nil
	}
	r.Text = a.Name + ": signed in."
	for _, w := range r.Accounts {
		if w.Name == a.Name {
			r.Text = a.Name + ": signed in as " + w.Bot + "."
			if w.Hidden {
				r.Text += " " + w.Fix
			}
		}
	}
	return r, nil
}
