package mail

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// Service reads and acts on the mail accounts for the core: a connection
// per account, kept open between looks and closed after idle minutes, and
// each folder's newest messages in memory, never in a file.
type Service struct {
	Store Store
	Pins  *Pins
	// Idle is how long an unused connection stays open; 0 = idleWait.
	Idle time.Duration

	mu       sync.Mutex
	accts    map[string]*acct
	sweeping bool
	closed   bool
}

// accountWait is the most one account's part of a list may take: an
// account slower than that says so in its row, and the list comes without
// it.
const accountWait = 20 * time.Second

// idleWait is how long an unused connection stays open by default:
// opening one again takes a second or so.
const idleWait = 5 * time.Minute

// acct is what the service holds of one account.
type acct struct {
	a Account

	mu      sync.Mutex // one command at a time on its connection
	s       *session
	lists   map[string]folderList // by folder role: its newest messages
	unread  int
	counted bool // unread was read
	problem string
	needs   bool // it has no password
	fetched time.Time
}

type folderList struct {
	msgs  []ipc.MailMessage
	at    time.Time
	limit int
}

// sync takes the configured accounts: new ones in, removed ones out with
// their connections, and a changed one with a new connection.
func (sv *Service) sync(all []Account) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if sv.accts == nil {
		sv.accts = map[string]*acct{}
	}
	keep := map[string]bool{}
	for _, a := range all {
		keep[a.Name] = true
		x, ok := sv.accts[a.Name]
		if !ok {
			sv.accts[a.Name] = &acct{a: a, lists: map[string]folderList{}}
			continue
		}
		if x.a != a {
			go func(x *acct, a Account) {
				x.mu.Lock()
				defer x.mu.Unlock()
				same := x.a.Address == a.Address && x.a.Username == a.Username && x.a.IMAP == a.IMAP
				x.a = a
				if !same {
					x.s.close()
					x.s, x.lists, x.counted, x.problem, x.needs = nil, map[string]folderList{}, false, "", false
				}
			}(x, a)
		}
	}
	for name, x := range sv.accts {
		if !keep[name] {
			delete(sv.accts, name)
			go func(x *acct) {
				x.mu.Lock()
				defer x.mu.Unlock()
				x.s.close()
				x.s = nil
			}(x)
		}
	}
}

func (sv *Service) get(name string) (*acct, error) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	x, ok := sv.accts[name]
	if !ok {
		return nil, ipc.Errorf(ipc.CodeNotFound, "No mail account is called %s.", name)
	}
	return x, nil
}

// with runs fn on the account's connection, opening one when it has none.
// When the connection drops under fn, it tries once more on a new one.
func (sv *Service) with(ctx context.Context, x *acct, fn func(*session) error) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	for try := 0; ; try++ {
		if x.s == nil || !x.s.alive() {
			x.s.close()
			x.s = nil
			password, err := sv.Store.Get(x.a.Address)
			if errors.Is(err, ErrNoPassword) {
				x.needs = true
				return fmt.Errorf("%s has no password yet: add the account again in Mail, or run `"+brand.Slug+" mail add`", x.a.Name)
			}
			if err != nil {
				return fmt.Errorf("the Keychain: %v", err)
			}
			x.needs = false
			s, err := dial(ctx, x.a, password, sv.Pins)
			if err != nil {
				x.problem = err.Error()
				return err
			}
			x.s = s
			sv.sweepSoon()
		}
		stop := guard(ctx, x.s.c)
		err := fn(x.s)
		stop()
		if err == nil {
			x.s.used = time.Now()
			x.problem = ""
			return nil
		}
		if x.s.alive() || try > 0 || ctx.Err() != nil {
			if ctx.Err() != nil {
				err = fmt.Errorf("%s took too long to answer", x.a.Name)
			}
			if !x.s.alive() {
				x.s.close()
				x.s = nil
				x.problem = err.Error()
			}
			return err
		}
		// The connection dropped, as an idle one does: once more.
		x.s.close()
		x.s = nil
	}
}

// sweepSoon starts the sweep that closes idle connections, if it isn't
// running.
func (sv *Service) sweepSoon() {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if sv.sweeping || sv.closed {
		return
	}
	sv.sweeping = true
	go sv.sweep()
}

func (sv *Service) sweep() {
	idle := cmp.Or(sv.Idle, idleWait)
	t := time.NewTicker(min(idle, time.Minute))
	defer t.Stop()
	for range t.C {
		sv.mu.Lock()
		if sv.closed {
			sv.mu.Unlock()
			return
		}
		list := make([]*acct, 0, len(sv.accts))
		for _, x := range sv.accts {
			list = append(list, x)
		}
		sv.mu.Unlock()
		open := 0
		for _, x := range list {
			if !x.mu.TryLock() {
				open++
				continue
			}
			if x.s != nil && time.Since(x.s.used) > idle {
				x.s.close()
				x.s = nil
			}
			if x.s != nil {
				open++
			}
			x.mu.Unlock()
		}
		if open == 0 {
			sv.mu.Lock()
			sv.sweeping = false
			sv.mu.Unlock()
			return
		}
	}
}

// Close closes every connection at once, without waiting on a command
// under way or a server's goodbye, as the core does when it stops.
func (sv *Service) Close() {
	sv.mu.Lock()
	sv.closed = true
	list := make([]*acct, 0, len(sv.accts))
	for _, x := range sv.accts {
		list = append(list, x)
	}
	sv.mu.Unlock()
	for _, x := range list {
		// The connection closes under a command still running, which
		// then fails at once and lets go of the account.
		if x.mu.TryLock() {
			if x.s != nil {
				x.s.c.Close()
				x.s = nil
			}
			x.mu.Unlock()
			continue
		}
		go func(x *acct) {
			x.mu.Lock()
			defer x.mu.Unlock()
			if x.s != nil {
				x.s.c.Close()
				x.s = nil
			}
		}(x)
	}
}

// ListArgs say what a list holds.
type ListArgs struct {
	Accounts []string      // by name; empty = every one
	Folder   string        // such as inbox
	Query    string        // only messages with these words
	Limit    int           // how many of each account's newest
	Refresh  bool          // read the accounts now, however fresh
	Fresh    time.Duration // how long a look stays fresh
}

// List is a folder's newest messages across the accounts, newest first,
// with how every account stands. An account that fails says why in its
// own row, and the others' messages still come.
func (sv *Service) List(ctx context.Context, all []Account, a ListArgs) ipc.MailResult {
	sv.sync(all)
	if a.Folder == "" {
		a.Folder = ipc.FolderInbox
	}
	want := map[string]bool{}
	for _, n := range a.Accounts {
		want[n] = true
	}
	var wg sync.WaitGroup
	results := make([][]ipc.MailMessage, len(all))
	for i, ac := range all {
		if len(want) > 0 && !want[ac.Name] {
			continue
		}
		x, err := sv.get(ac.Name)
		if err != nil {
			continue
		}
		wg.Add(1)
		go func(i int, x *acct) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, accountWait)
			defer cancel()
			results[i] = sv.listOne(ctx, x, a)
		}(i, x)
	}
	wg.Wait()
	var msgs []ipc.MailMessage
	for _, r := range results {
		msgs = append(msgs, r...)
	}
	slices.SortStableFunc(msgs, func(p, q ipc.MailMessage) int {
		if c := q.Date.Compare(p.Date); c != 0 {
			return c
		}
		return cmp.Compare(p.Account, q.Account)
	})
	return ipc.MailResult{Accounts: sv.Accounts(all), Messages: msgs}
}

// listOne is one account's part of a list: from memory while fresh, else
// read from the server.
func (sv *Service) listOne(ctx context.Context, x *acct, a ListArgs) []ipc.MailMessage {
	if a.Query == "" && !a.Refresh {
		x.mu.Lock()
		l, ok := x.lists[a.Folder]
		x.mu.Unlock()
		if ok && time.Since(l.at) < a.Fresh && l.limit >= a.Limit {
			return trim(l.msgs, a.Limit)
		}
	}
	var msgs []ipc.MailMessage
	err := sv.with(ctx, x, func(s *session) error {
		var err error
		if msgs, err = s.list(a.Folder, a.Limit, a.Query); err != nil {
			return err
		}
		if n, err := s.unread(); err == nil {
			x.unread, x.counted = n, true
		}
		return nil
	})
	if err != nil {
		x.mu.Lock()
		x.problem = err.Error()
		l := x.lists[a.Folder]
		x.mu.Unlock()
		// What was read before still shows, under the account's problem.
		if a.Query == "" {
			return trim(l.msgs, a.Limit)
		}
		return nil
	}
	x.mu.Lock()
	x.fetched = time.Now()
	if a.Query == "" {
		x.lists[a.Folder] = folderList{msgs: msgs, at: x.fetched, limit: a.Limit}
	}
	x.mu.Unlock()
	return msgs
}

func trim(msgs []ipc.MailMessage, limit int) []ipc.MailMessage {
	if limit <= 0 || len(msgs) <= limit {
		return slices.Clone(msgs)
	}
	// They come oldest first from the server; the newest are last.
	return slices.Clone(msgs[len(msgs)-limit:])
}

// Accounts are every account and how it stands, by what the service has
// seen: it reads nothing for this.
func (sv *Service) Accounts(all []Account) []ipc.MailAccount {
	sv.sync(all)
	out := make([]ipc.MailAccount, 0, len(all))
	for _, a := range all {
		w := ipc.MailAccount{Name: a.Name, Kind: a.Kind, Provider: Provider(a.Kind), Address: a.Address, Colour: a.Colour, Web: Web(a), State: ipc.MailChecking}
		if x, err := sv.get(a.Name); err == nil {
			x.mu.Lock()
			w.Unread, w.Fetched = x.unread, x.fetched
			switch {
			case x.needs:
				w.State, w.Problem = ipc.MailNeeds, "No password yet."
			case x.problem != "":
				w.State, w.Problem = ipc.MailError, x.problem
			case x.counted || !x.fetched.IsZero():
				w.State = ipc.MailReady
			}
			x.mu.Unlock()
		}
		out = append(out, w)
	}
	return out
}

// Read is the message whole, marked read with seen.
func (sv *Service) Read(ctx context.Context, all []Account, idText string, seen bool) (ipc.MailBody, error) {
	sv.sync(all)
	id, x, err := sv.find(idText)
	if err != nil {
		return ipc.MailBody{}, err
	}
	var body ipc.MailBody
	err = sv.with(ctx, x, func(s *session) error {
		var err error
		body, err = s.read(id, seen)
		return err
	})
	if err != nil {
		return ipc.MailBody{}, err
	}
	if seen {
		if old, ok := sv.update(x, idText, func(m *ipc.MailMessage) bool { m.Unread = false; return true }); ok && old.Unread && old.Folder == ipc.FolderInbox {
			sv.count(x, -1)
		}
	}
	return body, nil
}

func (sv *Service) find(idText string) (MessageID, *acct, error) {
	id, err := ParseID(idText)
	if err != nil {
		return MessageID{}, nil, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	x, err := sv.get(id.Account)
	return id, x, err
}

// Flag sets or clears a flag on the message.
func (sv *Service) Flag(ctx context.Context, all []Account, idText, flag string) error {
	sv.sync(all)
	id, x, err := sv.find(idText)
	if err != nil {
		return err
	}
	if err := sv.with(ctx, x, func(s *session) error { return s.flag(id, flag) }); err != nil {
		return err
	}
	old, ok := sv.update(x, idText, func(m *ipc.MailMessage) bool {
		switch flag {
		case ipc.FlagRead:
			m.Unread = false
		case ipc.FlagUnread:
			m.Unread = true
		case ipc.FlagStar:
			m.Starred = true
		case ipc.FlagUnstar:
			m.Starred = false
		}
		return true
	})
	switch {
	case ok && old.Folder == ipc.FolderInbox:
		switch {
		case flag == ipc.FlagRead && old.Unread:
			sv.count(x, -1)
		case flag == ipc.FlagUnread && !old.Unread:
			sv.count(x, +1)
		}
	default:
		// The same message may be in the inbox under another ID: read the
		// inbox afresh at the next look.
		x.mu.Lock()
		delete(x.lists, ipc.FolderInbox)
		x.mu.Unlock()
	}
	return nil
}

// Move moves the message to the folder for role, such as trash.
func (sv *Service) Move(ctx context.Context, all []Account, idText, role string) error {
	sv.sync(all)
	id, x, err := sv.find(idText)
	if err != nil {
		return err
	}
	if err := sv.with(ctx, x, func(s *session) error { return s.move(id, role) }); err != nil {
		return err
	}
	old, ok := sv.update(x, idText, func(*ipc.MailMessage) bool { return false })
	if ok && old.Unread {
		switch {
		case old.Folder == ipc.FolderInbox:
			sv.count(x, -1)
		case role == ipc.FolderInbox:
			sv.count(x, +1)
		}
	}
	x.mu.Lock()
	// The folder it went to holds it now: read that one afresh.
	delete(x.lists, role)
	x.mu.Unlock()
	return nil
}

// update changes the message in every list held, or drops it where keep
// says false, and says how it was before, when a list held it.
func (sv *Service) update(x *acct, id string, change func(*ipc.MailMessage) (keep bool)) (ipc.MailMessage, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	var old ipc.MailMessage
	found := false
	for role, l := range x.lists {
		out := l.msgs[:0:0]
		for _, m := range l.msgs {
			if m.ID == id {
				if !found {
					old, found = m, true
				}
				if !change(&m) {
					continue
				}
			}
			out = append(out, m)
		}
		l.msgs = out
		x.lists[role] = l
	}
	return old, found
}

// count moves the inbox's unread count by d at once, so it follows a
// change made here; the next look reads it from the server.
func (sv *Service) count(x *acct, d int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.unread = max(0, x.unread+d)
}

// Send sends the draft from its account, threaded under the message it
// answers, which is then marked answered.
func (sv *Service) Send(ctx context.Context, all []Account, d ipc.MailDraft) error {
	sv.sync(all)
	a, ok := Find(all, d.From)
	if !ok {
		return ipc.Errorf(ipc.CodeNotFound, "No mail account is called %s.", d.From)
	}
	x, err := sv.get(a.Name)
	if err != nil {
		return err
	}
	var answers *ipc.MailBody
	var rid MessageID
	var rx *acct
	if d.Reply != "" {
		rid, rx, err = sv.find(d.Reply)
		if err != nil {
			return err
		}
		var body ipc.MailBody
		if err := sv.with(ctx, rx, func(s *session) error {
			var err error
			body, err = s.read(rid, false)
			return err
		}); err == nil {
			answers = &body
		}
	}
	msg, _, err := Compose(a, d, answers, time.Now())
	if err != nil {
		return ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	password, err := sv.Store.Get(a.Address)
	if errors.Is(err, ErrNoPassword) {
		x.mu.Lock()
		x.needs = true
		x.mu.Unlock()
		return fmt.Errorf("%s has no password yet: add the account again in Mail", a.Name)
	}
	if err != nil {
		return fmt.Errorf("the Keychain: %v", err)
	}
	if err := send(ctx, a, password, sv.Pins, d, msg); err != nil {
		return err
	}
	if answers != nil {
		_ = sv.with(ctx, rx, func(s *session) error { return s.flag(rid, "answered") })
		_, _ = sv.update(rx, d.Reply, func(m *ipc.MailMessage) bool { m.Answered = true; return true })
	}
	x.mu.Lock()
	delete(x.lists, ipc.FolderSent)
	x.mu.Unlock()
	return nil
}

// Test signs in to the account with the password, and finds its folders,
// without keeping the connection: what an account must pass before Irie
// keeps it.
func (sv *Service) Test(ctx context.Context, a Account, password string) error {
	s, err := dial(ctx, a, password, sv.Pins)
	if err != nil {
		return err
	}
	defer s.close()
	if s.folders[ipc.FolderInbox] == "" {
		return fmt.Errorf("%s has no inbox", a.Address)
	}
	return nil
}

// Forget drops what the service holds of the account, its connection
// too, as after its password changed.
func (sv *Service) Forget(name string) {
	sv.mu.Lock()
	x, ok := sv.accts[name]
	delete(sv.accts, name)
	sv.mu.Unlock()
	if !ok {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.s.close()
	x.s = nil
}
