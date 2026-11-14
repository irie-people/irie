package mail

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// session is one account's IMAP connection, kept open between looks so a
// message opens at once.
type session struct {
	acct    Account
	c       *imapclient.Client
	caps    imap.CapSet
	folders map[string]string // a folder's role, such as trash, → its mailbox there
	used    time.Time
}

// dialWait is how long a connection may take to open.
const dialWait = 15 * time.Second

// guard closes the connection when ctx ends, which ends any command
// waiting on it: go-imap's commands take no context of their own.
func guard(ctx context.Context, c *imapclient.Client) (stop func() bool) {
	return context.AfterFunc(ctx, func() { c.Close() })
}

// dial opens a connection to the account's IMAP server and signs in.
func dial(ctx context.Context, a Account, password string, pins *Pins) (*session, error) {
	opts := &imapclient.Options{
		TLSConfig:   pins.TLS(a.IMAP),
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
		Dialer:      &net.Dialer{Timeout: dialWait},
	}
	type dialed struct {
		c   *imapclient.Client
		err error
	}
	ch := make(chan dialed, 1)
	go func() {
		var d dialed
		if a.IMAP.TLS {
			d.c, d.err = imapclient.DialTLS(a.IMAP.Addr(), opts)
		} else {
			d.c, d.err = imapclient.DialStartTLS(a.IMAP.Addr(), opts)
		}
		ch <- d
	}()
	var d dialed
	select {
	case d = <-ch:
	case <-ctx.Done():
		go func() {
			if d := <-ch; d.c != nil {
				d.c.Close()
			}
		}()
		return nil, ctx.Err()
	}
	if d.err != nil {
		return nil, explain(a, "connect", d.err)
	}
	c := d.c
	defer guard(ctx, c)()
	if err := c.Login(a.Username, password).Wait(); err != nil {
		c.Close()
		return nil, explain(a, "login", err)
	}
	s := &session{acct: a, c: c, caps: c.Caps(), used: time.Now()}
	if err := s.findFolders(); err != nil {
		c.Close()
		return nil, explain(a, "list", err)
	}
	return s, nil
}

func (s *session) close() {
	if s == nil || s.c == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		s.c.Logout().Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	s.c.Close()
}

// alive says whether the connection still stands.
func (s *session) alive() bool {
	select {
	case <-s.c.Closed():
		return false
	default:
		return s.c.State() == imap.ConnStateAuthenticated || s.c.State() == imap.ConnStateSelected
	}
}

// roleAttrs are the special-use marks of RFC 6154 for each role.
var roleAttrs = map[imap.MailboxAttr]string{
	imap.MailboxAttrSent:    ipc.FolderSent,
	imap.MailboxAttrDrafts:  ipc.FolderDrafts,
	imap.MailboxAttrArchive: ipc.FolderArchive,
	imap.MailboxAttrJunk:    ipc.FolderJunk,
	imap.MailboxAttrTrash:   ipc.FolderTrash,
	imap.MailboxAttrFlagged: ipc.FolderStarred,
	imap.MailboxAttrAll:     folderAll,
}

// folderAll is Gmail's All Mail, where its archived messages are, so its
// archive.
const folderAll = "all"

// roleNames are the usual names of each role's mailbox, for a server that
// doesn't mark them, in lower case.
var roleNames = map[string][]string{
	ipc.FolderSent:    {"sent", "sent mail", "sent items", "sent messages"},
	ipc.FolderDrafts:  {"drafts"},
	ipc.FolderArchive: {"archive", "archives"},
	ipc.FolderJunk:    {"spam", "junk", "junk email", "junk e-mail", "bulk mail"},
	ipc.FolderTrash:   {"trash", "bin", "deleted items", "deleted messages"},
	ipc.FolderStarred: {"starred", "flagged"},
	folderAll:         {"all mail"},
}

// roleParents are the mailboxes whose children may hold the roles, as
// Gmail's [Gmail] and Dovecot's INBOX do; any other folder's Archive is
// the person's own.
var roleParents = []string{"", "[gmail]", "[google mail]", "inbox"}

// folders reads the account's mailboxes and finds each role's.
func (s *session) findFolders() error {
	opts := &imap.ListOptions{ReturnSpecialUse: s.caps.Has(imap.CapSpecialUse)}
	list, err := s.c.List("", "*", opts).Collect()
	if err != nil {
		return err
	}
	s.folders = roles(list)
	return nil
}

func roles(list []*imap.ListData) map[string]string {
	out := map[string]string{ipc.FolderInbox: "INBOX"}
	selectable := func(l *imap.ListData) bool {
		return !slices.Contains(l.Attrs, imap.MailboxAttrNoSelect) && !slices.Contains(l.Attrs, imap.MailboxAttrNonExistent)
	}
	for _, l := range list {
		if !selectable(l) {
			continue
		}
		for _, a := range l.Attrs {
			for attr, role := range roleAttrs {
				if strings.EqualFold(string(a), string(attr)) && out[role] == "" {
					out[role] = l.Mailbox
				}
			}
		}
	}
	for _, l := range list {
		if !selectable(l) {
			continue
		}
		parent, leaf := "", strings.ToLower(l.Mailbox)
		if l.Delim != 0 {
			if i := strings.LastIndex(l.Mailbox, string(l.Delim)); i >= 0 {
				parent, leaf = strings.ToLower(l.Mailbox[:i]), strings.ToLower(l.Mailbox[i+1:])
			}
		}
		if !slices.Contains(roleParents, parent) {
			continue
		}
		for role, names := range roleNames {
			if out[role] == "" && slices.Contains(names, leaf) {
				out[role] = l.Mailbox
			}
		}
	}
	// Gmail archives into All Mail.
	if out[ipc.FolderArchive] == "" && out[folderAll] != "" {
		out[ipc.FolderArchive] = out[folderAll]
	}
	return out
}

// mailbox is the role's mailbox, or why the account has none.
func (s *session) mailbox(role string) (string, error) {
	if m := s.folders[role]; m != "" {
		return m, nil
	}
	return "", fmt.Errorf("%s has no %s folder", s.acct.Name, role)
}

// open selects the mailbox for reading and changing, and says its
// UIDVALIDITY, which every message's ID carries.
func (s *session) open(mailbox string) (uint32, error) {
	// Selected again even when it is already, so the server says what
	// came meanwhile.
	d, err := s.c.Select(mailbox, nil).Wait()
	if err != nil {
		return 0, err
	}
	return d.UIDValidity, nil
}

// envelopeItems are what a list shows of each message.
var envelopeItems = imap.FetchOptions{UID: true, Flags: true, Envelope: true, InternalDate: true, RFC822Size: true}

// list reads a folder's newest messages, at most limit, or with query,
// the newest of those that hold its words.
func (s *session) list(role string, limit int, query string) ([]ipc.MailMessage, error) {
	mbox, err := s.mailbox(role)
	if err != nil {
		return nil, err
	}
	d, err := s.c.Select(mbox, nil).Wait()
	if err != nil {
		return nil, err
	}
	var set imap.NumSet
	switch {
	case query != "":
		crit := &imap.SearchCriteria{Text: strings.Fields(query)}
		r, err := s.c.UIDSearch(crit, nil).Wait()
		if err != nil {
			return nil, err
		}
		uids := r.AllUIDs()
		slices.Sort(uids)
		if len(uids) > limit {
			uids = uids[len(uids)-limit:]
		}
		if len(uids) == 0 {
			return nil, nil
		}
		set = imap.UIDSetNum(uids...)
	case d.NumMessages == 0:
		return nil, nil
	default:
		from := uint32(1)
		if d.NumMessages > uint32(limit) {
			from = d.NumMessages - uint32(limit) + 1
		}
		var seq imap.SeqSet
		seq.AddRange(from, d.NumMessages)
		set = seq
	}
	opts := envelopeItems
	bufs, err := s.c.Fetch(set, &opts).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]ipc.MailMessage, 0, len(bufs))
	for _, b := range bufs {
		if b.UID == 0 {
			continue
		}
		out = append(out, summary(s.acct, role, mbox, d.UIDValidity, b))
	}
	return out, nil
}

// unread counts the unread messages in the inbox.
func (s *session) unread() (int, error) {
	if m := s.c.Mailbox(); m != nil && m.Name == "INBOX" {
		r, err := s.c.UIDSearch(&imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}, nil).Wait()
		if err != nil {
			return 0, err
		}
		return len(r.AllUIDs()), nil
	}
	d, err := s.c.Status("INBOX", &imap.StatusOptions{NumUnseen: true}).Wait()
	if err != nil {
		return 0, err
	}
	if d.NumUnseen == nil {
		return 0, nil
	}
	return int(*d.NumUnseen), nil
}

// summary is a fetched message as a list shows it.
func summary(a Account, role, mbox string, validity uint32, b *imapclient.FetchMessageBuffer) ipc.MailMessage {
	m := ipc.MailMessage{
		ID:      MessageID{Account: a.Name, Validity: validity, UID: uint32(b.UID), Mailbox: mbox}.String(),
		Account: a.Name,
		Folder:  role,
		Date:    b.InternalDate,
		Size:    b.RFC822Size,
		Unread:  !slices.Contains(b.Flags, imap.FlagSeen),
		Starred: slices.Contains(b.Flags, imap.FlagFlagged),
	}
	m.Answered = slices.Contains(b.Flags, imap.FlagAnswered)
	if e := b.Envelope; e != nil {
		m.Subject = clean(e.Subject)
		if !e.Date.IsZero() {
			m.Date = e.Date
		}
		if len(e.From) > 0 {
			m.FromAddr = e.From[0].Addr()
			m.From = clean(e.From[0].Name)
			if m.From == "" {
				m.From = m.FromAddr
			}
		}
		m.To = addrs(e.To)
	}
	return m
}

// clean is a header's text on one line, with nothing a list can't draw.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// addrs are addresses as people read them: "Name <a@b>", or the address.
func addrs(list []imap.Address) []string {
	var out []string
	for _, a := range list {
		addr := a.Addr()
		if addr == "" {
			continue
		}
		if n := clean(a.Name); n != "" {
			out = append(out, n+" <"+addr+">")
		} else {
			out = append(out, addr)
		}
	}
	return out
}

// MessageID names one message: its account, its mailbox and its place
// there. The UIDVALIDITY keeps an ID from naming another message once a
// server numbers a mailbox afresh.
type MessageID struct {
	Account  string
	Validity uint32
	UID      uint32
	Mailbox  string
}

// String is the ID as the wire carries it: account|validity|uid|mailbox,
// the mailbox last, since its name may hold anything.
func (id MessageID) String() string {
	return id.Account + "|" + strconv.FormatUint(uint64(id.Validity), 10) + "|" + strconv.FormatUint(uint64(id.UID), 10) + "|" + id.Mailbox
}

// ParseID reads a message's ID.
func ParseID(s string) (MessageID, error) {
	p := strings.SplitN(s, "|", 4)
	if len(p) != 4 || !config.MailName.MatchString(p[0]) || p[3] == "" {
		return MessageID{}, fmt.Errorf("%q is no message's ID", s)
	}
	v, err1 := strconv.ParseUint(p[1], 10, 32)
	u, err2 := strconv.ParseUint(p[2], 10, 32)
	if err1 != nil || err2 != nil || u == 0 {
		return MessageID{}, fmt.Errorf("%q is no message's ID", s)
	}
	return MessageID{Account: p[0], Validity: uint32(v), UID: uint32(u), Mailbox: p[3]}, nil
}

// errMoved is the answer for a message no longer where its ID says.
var errMoved = errors.New("the message isn't there any more: it was moved or deleted, or the folder renumbered. Refresh the list")

// at selects the message's mailbox and checks that the ID still names it.
func (s *session) at(id MessageID) error {
	v, err := s.open(id.Mailbox)
	if err != nil {
		return err
	}
	if v != id.Validity {
		return errMoved
	}
	return nil
}

// flag sets or clears a flag on the message.
func (s *session) flag(id MessageID, flag string) error {
	if err := s.at(id); err != nil {
		return err
	}
	f := &imap.StoreFlags{Silent: true}
	switch flag {
	case ipc.FlagRead:
		f.Op, f.Flags = imap.StoreFlagsAdd, []imap.Flag{imap.FlagSeen}
	case ipc.FlagUnread:
		f.Op, f.Flags = imap.StoreFlagsDel, []imap.Flag{imap.FlagSeen}
	case ipc.FlagStar:
		f.Op, f.Flags = imap.StoreFlagsAdd, []imap.Flag{imap.FlagFlagged}
	case ipc.FlagUnstar:
		f.Op, f.Flags = imap.StoreFlagsDel, []imap.Flag{imap.FlagFlagged}
	case "answered":
		f.Op, f.Flags = imap.StoreFlagsAdd, []imap.Flag{imap.FlagAnswered}
	default:
		return fmt.Errorf("%q is no flag: read, unread, star or unstar", flag)
	}
	return s.c.Store(imap.UIDSetNum(imap.UID(id.UID)), f, nil).Close()
}

// move moves the message to the role's folder.
func (s *session) move(id MessageID, role string) error {
	switch role {
	case ipc.FolderInbox, ipc.FolderArchive, ipc.FolderJunk, ipc.FolderTrash:
	default:
		return fmt.Errorf("a message moves to the inbox, archive, junk or trash, not %q", role)
	}
	dest, err := s.mailbox(role)
	if err != nil {
		return err
	}
	if dest == id.Mailbox {
		return nil
	}
	// Without MOVE or UIDPLUS, a move would expunge every message marked
	// deleted in the folder, not only this one.
	if !s.caps.Has(imap.CapMove) && !s.caps.Has(imap.CapUIDPlus) {
		return fmt.Errorf("%s's server can't move one message alone", s.acct.Name)
	}
	if err := s.at(id); err != nil {
		return err
	}
	_, err = s.c.Move(imap.UIDSetNum(imap.UID(id.UID)), dest).Wait()
	return err
}

// explain makes a connection's error one a person can act on, naming the
// account's provider and what fixes it.
func explain(a Account, during string, err error) error {
	var ie *imap.Error
	var ne net.Error
	switch {
	case errors.Is(err, ErrPinChanged):
		return err
	case errors.As(err, &ie) && during == "login":
		switch a.Kind {
		case config.MailGmail:
			return fmt.Errorf("Gmail turned the password down for %s. It takes an app password, made at https://myaccount.google.com/apppasswords, not your Google password; with 2-Step Verification on. (%s)", a.Address, ie.Text)
		case config.MailProton:
			return fmt.Errorf("Proton Mail Bridge turned the password down for %s. It takes the Bridge password from Bridge's mailbox details, not your Proton password. (%s)", a.Address, ie.Text)
		}
		return fmt.Errorf("the server turned the password down for %s (%s)", a.Address, ie.Text)
	case errors.Is(err, syscall.ECONNREFUSED) && a.Kind == config.MailProton && a.IMAP.Local():
		return fmt.Errorf("Proton Mail Bridge isn't running, or isn't signed in: open it and sign in to %s", a.Address)
	case errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Errorf("nothing answers at %s", a.IMAP.Addr())
	case errors.As(err, &ne) && ne.Timeout():
		return fmt.Errorf("%s didn't answer in time", a.IMAP.Host)
	}
	return fmt.Errorf("%s: %v", a.Name, err)
}
