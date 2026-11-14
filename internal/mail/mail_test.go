package mail

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"net"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// selfSigned is a certificate for 127.0.0.1 that signs itself, as Proton
// Mail Bridge's does.
func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

type quiet struct{}

func (quiet) Printf(string, ...any) {}

// imapServer is an IMAP server on 127.0.0.1 with STARTTLS, as Proton Mail
// Bridge runs, holding one user with the usual folders.
type imapServer struct {
	addr string
	port int
	user *imapmemserver.User
	srv  *imapserver.Server
}

func newIMAP(t *testing.T, username, password string) *imapServer {
	t.Helper()
	mem := imapmemserver.New()
	u := imapmemserver.NewUser(username, password)
	for _, m := range []string{"INBOX", "Archive", "Sent", "Spam", "Trash", "Drafts", "Clients", "Clients/Archive"} {
		if err := u.Create(m, nil); err != nil {
			t.Fatal(err)
		}
	}
	mem.AddUser(u)
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:      imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}},
		Logger:    quiet{},
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return &imapServer{addr: ln.Addr().String(), port: ln.Addr().(*net.TCPAddr).Port, user: u, srv: srv}
}

func (s *imapServer) add(t *testing.T, mbox, raw string, when time.Time, flags ...imap.Flag) {
	t.Helper()
	raw = strings.ReplaceAll(raw, "\n", "\r\n")
	if _, err := s.user.Append(mbox, strings.NewReader(raw), &imap.AppendOptions{Flags: flags, Time: when}); err != nil {
		t.Fatal(err)
	}
}

// fixture is a service with a Proton-like account on a test server.
type fixture struct {
	sv    *Service
	all   []Account
	srv   *imapServer
	store Files
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	srv := newIMAP(t, "alice@example.com", "bridge-pw")
	dir := t.TempDir()
	store := Files{Dir: filepath.Join(dir, "keys")}
	if err := store.Set("alice@example.com", "bridge-pw"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Mail{Accounts: map[string]config.MailAccount{
		"proton": {Kind: config.MailProton, Address: "alice@example.com", IMAP: srv.addr, SMTP: "127.0.0.1:1"},
	}}
	all, err := Accounts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sv := &Service{Store: store, Pins: &Pins{Path: filepath.Join(dir, "pins.json")}}
	t.Cleanup(sv.Close)
	return &fixture{sv: sv, all: all, srv: srv, store: store}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

const (
	plainMsg = `From: Bob Example <bob@example.com>
To: alice@example.com
Subject: Lunch on Friday?
Date: Mon, 09 Nov 2026 10:00:00 +0000
Message-ID: <lunch@example.com>
Content-Type: text/plain; charset=utf-8

Shall we have lunch on Friday? The usual place.
`
	altMsg = `From: =?utf-8?q?Zo=C3=AB?= <zoe@example.com>
To: Alice <alice@example.com>
Cc: carol@example.com
Subject: =?iso-8859-1?q?R=E9union_lundi?=
Date: Tue, 10 Nov 2026 09:00:00 +0000
Message-ID: <reunion@example.com>
References: <older@example.com>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="mixed"

--mixed
Content-Type: multipart/alternative; boundary="alt"

--alt
Content-Type: text/plain; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

La r=E9union est lundi =E0 10h. Voici l'ordre du jour en pi=E8ce jointe.
--alt
Content-Type: text/html; charset=utf-8

<p>La réunion est <b>lundi</b> à 10h.</p>
--alt--
--mixed
Content-Type: application/pdf; name="agenda.pdf"
Content-Disposition: attachment; filename="agenda.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQKJcOkw7zDtsOfCg==
--mixed--
`
	htmlMsg = `From: news@example.com
To: alice@example.com
Subject: This week
Date: Wed, 11 Nov 2026 08:00:00 +0000
MIME-Version: 1.0
Content-Type: text/html; charset=utf-8
Content-Transfer-Encoding: base64

` + "PGh0bWw+PGJvZHk+PGgxPldoYXQncyBuZXc8L2gxPjxwPkEgbmV3IHJlbGVhc2UgaXMgb3V0LjwvcD48c2NyaXB0PmFsZXJ0KDEpPC9zY3JpcHQ+PC9ib2R5PjwvaHRtbD4=" + `
`
)

func TestListReadFlagMove(t *testing.T) {
	f := newFixture(t)
	f.srv.add(t, "INBOX", plainMsg, time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC), imap.FlagSeen)
	f.srv.add(t, "INBOX", altMsg, time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
	f.srv.add(t, "INBOX", htmlMsg, time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC))

	r := f.sv.List(ctx(t), f.all, ListArgs{Limit: 50, Fresh: time.Minute})
	if len(r.Accounts) != 1 || r.Accounts[0].State != ipc.MailReady || r.Accounts[0].Unread != 2 {
		t.Fatalf("accounts = %+v, want proton ready with 2 unread", r.Accounts)
	}
	if a := r.Accounts[0]; a.Provider != "Proton Mail" || a.Web == "" {
		t.Errorf("account = %+v, want Proton Mail with its web page", a)
	}
	var subjects []string
	for _, m := range r.Messages {
		subjects = append(subjects, m.Subject)
	}
	if want := []string{"This week", "Réunion lundi", "Lunch on Friday?"}; !slices.Equal(subjects, want) {
		t.Fatalf("subjects = %q, want %q, newest first", subjects, want)
	}
	reunion := r.Messages[1]
	if reunion.From != "Zoë" || reunion.FromAddr != "zoe@example.com" || !reunion.Unread || reunion.Folder != ipc.FolderInbox || reunion.Account != "proton" {
		t.Errorf("message = %+v", reunion)
	}

	// The pinned certificate is kept for the next connection.
	if b, err := os.ReadFile(f.sv.Pins.Path); err != nil || !strings.Contains(string(b), f.srv.addr) {
		t.Errorf("pins = %s, %v; want %s's certificate", b, err, f.srv.addr)
	}

	body, err := f.sv.Read(ctx(t), f.all, reunion.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.Text, "La réunion est lundi à 10h") {
		t.Errorf("text = %q, want the text part in UTF-8", body.Text)
	}
	if !strings.Contains(body.HTML, "<b>lundi</b>") {
		t.Errorf("html = %q", body.HTML)
	}
	if len(body.Attachments) != 1 || body.Attachments[0].Name != "agenda.pdf" || body.Attachments[0].Type != "application/pdf" {
		t.Errorf("attachments = %+v", body.Attachments)
	}
	if body.MessageID != "reunion@example.com" || !slices.Equal(body.References, []string{"older@example.com"}) {
		t.Errorf("message id %q, references %q", body.MessageID, body.References)
	}
	if len(body.Cc) != 1 || body.Cc[0] != "carol@example.com" || body.Unread {
		t.Errorf("cc %q, unread %v: want carol and read", body.Cc, body.Unread)
	}
	if got := f.sv.Accounts(f.all)[0].Unread; got != 1 {
		t.Errorf("unread after reading = %d, want 1", got)
	}

	news, err := f.sv.Read(ctx(t), f.all, r.Messages[0].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(news.Text, "A new release is out.") || strings.Contains(news.Text, "alert") {
		t.Errorf("text of an HTML message = %q, want its words without the script", news.Text)
	}

	if err := f.sv.Flag(ctx(t), f.all, reunion.ID, ipc.FlagStar); err != nil {
		t.Fatal(err)
	}
	if err := f.sv.Move(ctx(t), f.all, r.Messages[0].ID, ipc.FolderArchive); err != nil {
		t.Fatal(err)
	}
	r = f.sv.List(ctx(t), f.all, ListArgs{Limit: 50, Fresh: time.Minute})
	if len(r.Messages) != 2 || !r.Messages[0].Starred || r.Accounts[0].Unread != 0 {
		t.Fatalf("after star and archive: %+v, %+v", r.Messages, r.Accounts)
	}
	arch := f.sv.List(ctx(t), f.all, ListArgs{Folder: ipc.FolderArchive, Limit: 50, Fresh: time.Minute})
	if len(arch.Messages) != 1 || arch.Messages[0].Subject != "This week" || arch.Messages[0].Folder != ipc.FolderArchive {
		t.Fatalf("archive = %+v; want the newsletter, in the top Archive, not Clients/Archive", arch.Messages)
	}

	// Words find a message.
	found := f.sv.List(ctx(t), f.all, ListArgs{Query: "lunch", Limit: 50, Fresh: time.Minute})
	if len(found.Messages) != 1 || found.Messages[0].Subject != "Lunch on Friday?" {
		t.Errorf("search = %+v", found.Messages)
	}
}

func TestListStaysFreshThenRefreshes(t *testing.T) {
	f := newFixture(t)
	f.srv.add(t, "INBOX", plainMsg, time.Now())
	if r := f.sv.List(ctx(t), f.all, ListArgs{Limit: 10, Fresh: time.Hour}); len(r.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(r.Messages))
	}
	f.srv.add(t, "INBOX", htmlMsg, time.Now())
	if r := f.sv.List(ctx(t), f.all, ListArgs{Limit: 10, Fresh: time.Hour}); len(r.Messages) != 1 {
		t.Errorf("a fresh list was read again: %d messages", len(r.Messages))
	}
	// A connection the server dropped is opened again.
	sv := f.sv
	x, _ := sv.get("proton")
	x.mu.Lock()
	x.s.c.Close()
	x.mu.Unlock()
	if r := f.sv.List(ctx(t), f.all, ListArgs{Limit: 10, Fresh: time.Hour, Refresh: true}); len(r.Messages) != 2 || r.Accounts[0].State != ipc.MailReady {
		t.Errorf("after refresh: %d messages, %+v", len(r.Messages), r.Accounts)
	}
}

func TestAccountsThatFailSayWhy(t *testing.T) {
	f := newFixture(t)
	cfg := config.Mail{Accounts: map[string]config.MailAccount{
		"proton": {Kind: config.MailProton, Address: "alice@example.com", IMAP: f.srv.addr},
		"wrong":  {Kind: config.MailProton, Address: "bob@example.com", IMAP: f.srv.addr},
		"nopass": {Kind: config.MailProton, Address: "carol@example.com", IMAP: f.srv.addr},
		"down":   {Kind: config.MailProton, Address: "dan@example.com", IMAP: "127.0.0.1:1"},
	}}
	all, err := Accounts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{"bob@example.com", "dan@example.com"} {
		if err := f.store.Set(addr, "nope"); err != nil {
			t.Fatal(err)
		}
	}
	f.srv.add(t, "INBOX", plainMsg, time.Now())
	r := f.sv.List(ctx(t), all, ListArgs{Limit: 10, Fresh: time.Minute})
	if len(r.Messages) != 1 {
		t.Errorf("messages = %d: the account that works should still show its own", len(r.Messages))
	}
	states := map[string]ipc.MailAccount{}
	for _, a := range r.Accounts {
		states[a.Name] = a
	}
	if a := states["wrong"]; a.State != ipc.MailError || !strings.Contains(a.Problem, "Bridge password") {
		t.Errorf("wrong password: %+v", a)
	}
	if a := states["nopass"]; a.State != ipc.MailNeeds {
		t.Errorf("no password: %+v", a)
	}
	if a := states["down"]; a.State != ipc.MailError || !strings.Contains(a.Problem, "Bridge isn't running") {
		t.Errorf("bridge down: %+v", a)
	}
	if a := states["proton"]; a.State != ipc.MailReady {
		t.Errorf("proton: %+v", a)
	}
}

func TestAccountsServersAndColours(t *testing.T) {
	cfg := config.Mail{Accounts: map[string]config.MailAccount{
		"work":     {Kind: config.MailGmail, Address: "alice@example.com"},
		"personal": {Kind: config.MailGmail, Address: "alice.personal@example.com"},
		"proton":   {Kind: config.MailProton, Address: "alice@proton.example"},
		"fastmail": {Kind: config.MailIMAP, Address: "alice@fastmail.example", IMAP: "imap.fastmail.example:993", SMTP: "starttls://smtp.fastmail.example:587"},
		"pinned":   {Kind: config.MailIMAP, Address: "a@b.example", IMAP: "tls://127.0.0.1:1143", SMTP: "127.0.0.1:1025", Colour: 1},
	}}
	all, err := Accounts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	colours := map[string]int{}
	for _, a := range all {
		names = append(names, a.Name)
		colours[a.Name] = a.Colour
	}
	if want := []string{"personal", "work", "proton", "fastmail", "pinned"}; !slices.Equal(names, want) {
		t.Errorf("order = %q, want Gmail's, then Proton's, then the rest, each by name: %q", names, want)
	}
	// pinned takes colour 0 itself; the rest spread from the next free.
	if colours["pinned"] != 0 || colours["personal"] != 3 || colours["work"] != 1 || colours["proton"] != 4 || colours["fastmail"] != 2 {
		t.Errorf("colours = %v", colours)
	}
	seen := map[int]bool{}
	for _, c := range colours {
		if seen[c] {
			t.Errorf("two accounts share colour %d: %v", c, colours)
		}
		seen[c] = true
	}
	g, _ := Find(all, "work")
	if g.IMAP != gmailIMAP || g.SMTP != gmailSMTP || g.Username != "alice@example.com" {
		t.Errorf("gmail servers = %+v", g)
	}
	p, _ := Find(all, "proton")
	if p.IMAP.Addr() != "127.0.0.1:1143" || p.IMAP.TLS || p.SMTP.Addr() != "127.0.0.1:1025" || !p.IMAP.Local() {
		t.Errorf("proton servers = %+v", p)
	}
	fm, _ := Find(all, "fastmail")
	if !fm.IMAP.TLS || fm.SMTP.TLS || fm.SMTP.Port != 587 {
		t.Errorf("imap servers = %+v", fm)
	}
	pin, _ := Find(all, "pinned")
	if !pin.IMAP.TLS || pin.SMTP.TLS {
		t.Errorf("tls:// = %+v", pin)
	}
	if _, err := Accounts(config.Mail{Accounts: map[string]config.MailAccount{"x": {Kind: config.MailIMAP, Address: "a@b.example"}}}); err == nil {
		t.Error("an imap account without servers was taken")
	}
}

func TestNamesAndIDs(t *testing.T) {
	taken := map[string]bool{"alice": true}
	for _, c := range []struct{ addr, kind, want string }{
		{"Alice@example.com", "gmail", "alice-2"},
		{"bob.smith+news@example.com", "gmail", "bob-smith-news"},
		{"___@example.com", "proton", "proton"},
		{"a-very-long-name-indeed-yes@example.com", "gmail", "a-very-long-name-ind"},
	} {
		if got := NameFor(c.addr, c.kind, taken); got != c.want {
			t.Errorf("NameFor(%q) = %q, want %q", c.addr, got, c.want)
		}
	}
	id := MessageID{Account: "proton", Validity: 7, UID: 42, Mailbox: "Folders/a|b"}
	got, err := ParseID(id.String())
	if err != nil || got != id {
		t.Errorf("ParseID(%q) = %+v, %v", id.String(), got, err)
	}
	for _, bad := range []string{"", "proton|7|0|INBOX", "Proton|7|1|INBOX", "proton|x|1|INBOX", "proton|7|1|"} {
		if _, err := ParseID(bad); err == nil {
			t.Errorf("ParseID(%q) took it", bad)
		}
	}
}

func TestRoles(t *testing.T) {
	l := func(name string, attrs ...imap.MailboxAttr) *imap.ListData {
		return &imap.ListData{Mailbox: name, Delim: '/', Attrs: attrs}
	}
	gmail := roles([]*imap.ListData{
		l("INBOX"), l("[Gmail]", imap.MailboxAttrNoSelect),
		l("[Gmail]/All Mail", imap.MailboxAttrAll), l("[Gmail]/Corbeille", imap.MailboxAttrTrash),
		l("[Gmail]/Sent Mail", imap.MailboxAttrSent), l("[Gmail]/Spam", imap.MailboxAttrJunk),
		l("[Gmail]/Starred", imap.MailboxAttrFlagged), l("Work/Archive"),
	})
	want := map[string]string{"inbox": "INBOX", "archive": "[Gmail]/All Mail", "all": "[Gmail]/All Mail", "trash": "[Gmail]/Corbeille", "sent": "[Gmail]/Sent Mail", "junk": "[Gmail]/Spam", "starred": "[Gmail]/Starred"}
	for role, m := range want {
		if gmail[role] != m {
			t.Errorf("gmail %s = %q, want %q", role, gmail[role], m)
		}
	}
	plain := roles([]*imap.ListData{l("INBOX"), l("Archive"), l("Sent"), l("Spam"), l("Trash"), l("Starred"), l("Folders/Trash")})
	if plain["archive"] != "Archive" || plain["junk"] != "Spam" || plain["trash"] != "Trash" || plain["starred"] != "Starred" {
		t.Errorf("by name = %v", plain)
	}
	dovecot := roles([]*imap.ListData{{Mailbox: "INBOX", Delim: '.'}, {Mailbox: "INBOX.Sent", Delim: '.'}, {Mailbox: "INBOX.Trash", Delim: '.'}})
	if dovecot["sent"] != "INBOX.Sent" || dovecot["trash"] != "INBOX.Trash" {
		t.Errorf("dovecot = %v", dovecot)
	}
}

func TestPins(t *testing.T) {
	dir := t.TempDir()
	p := &Pins{Path: filepath.Join(dir, "pins.json")}
	s := Server{Host: "127.0.0.1", Port: 1143}
	a, b := selfSigned(t), selfSigned(t)
	check := p.TLS(s).VerifyPeerCertificate
	if err := check(a.Certificate, nil); err != nil {
		t.Fatalf("first certificate: %v", err)
	}
	if err := check(a.Certificate, nil); err != nil {
		t.Errorf("the same certificate again: %v", err)
	}
	if err := check(b.Certificate, nil); !errors.Is(err, ErrPinChanged) {
		t.Errorf("another certificate: %v, want ErrPinChanged", err)
	}
	if err := p.Forget(s.Addr()); err != nil {
		t.Fatal(err)
	}
	if err := check(b.Certificate, nil); err != nil {
		t.Errorf("after Forget: %v", err)
	}
	if c := p.TLS(Server{Host: "imap.gmail.com", Port: 993}); c.InsecureSkipVerify || c.VerifyPeerCertificate != nil {
		t.Error("a server elsewhere must show a certificate the system trusts")
	}
	if st, err := os.Stat(p.Path); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("pins file: %v, %v", st, err)
	}
}

func TestComposeThreadsAnAnswer(t *testing.T) {
	a := Account{Name: "personal", Kind: config.MailGmail, Address: "alice@example.com"}
	d := ipc.MailDraft{
		From: "personal", To: []string{"Bob Example <bob@example.com>, carol@example.com"}, Bcc: []string{"dan@example.com"},
		Subject: "Re: Réunion lundi", Body: "Yes, I'll be there.\nSee you =)\n" + strings.Repeat("long ", 30),
	}
	answers := &ipc.MailBody{MessageID: "reunion@example.com", References: []string{"older@example.com"}}
	msg, id, err := Compose(a, d, answers, time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	for _, want := range []string{
		"From: <alice@example.com>\r\n",
		"To: \"Bob Example\" <bob@example.com>, <carol@example.com>\r\n",
		"Subject: =?utf-8?q?Re:_R=C3=A9union_lundi?=\r\n",
		"In-Reply-To: <reunion@example.com>\r\n",
		"References: <older@example.com> <reunion@example.com>\r\n",
		"Message-ID: <" + id + ">\r\n",
		"Content-Transfer-Encoding: quoted-printable\r\n",
		"See you =3D)\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("message lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "dan@example.com") {
		t.Error("Bcc shows in the message")
	}
	if !strings.HasSuffix(id, "@example.com") {
		t.Errorf("message id %q", id)
	}
	for _, line := range strings.Split(s, "\r\n") {
		if len(line) > 78 {
			t.Errorf("line longer than 78: %q", line)
		}
	}
	if _, _, err := Compose(a, ipc.MailDraft{From: "personal", To: []string{"not an address"}}, nil, time.Now()); err == nil {
		t.Error("a bad address was taken")
	}
	if _, _, err := Compose(a, ipc.MailDraft{From: "personal"}, nil, time.Now()); err == nil {
		t.Error("a message to no one was taken")
	}
}

// smtpServer is an SMTP server on 127.0.0.1 with STARTTLS and AUTH PLAIN,
// as Proton Mail Bridge's, that keeps what it is sent.
type smtpServer struct {
	addr       string
	user, pass string
	cert       tls.Certificate

	mu    sync.Mutex
	from  string
	rcpts []string
	data  string
}

func newSMTP(t *testing.T, user, pass string) *smtpServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &smtpServer{addr: ln.Addr().String(), user: user, pass: pass, cert: selfSigned(t)}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(c)
		}
	}()
	return s
}

func (s *smtpServer) handle(c net.Conn) {
	defer func() { c.Close() }()
	tp := textproto.NewConn(c)
	tp.PrintfLine("220 test ESMTP")
	secure := false
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch strings.ToUpper(f[0]) {
		case "EHLO", "HELO":
			if secure {
				tp.PrintfLine("250-test")
				tp.PrintfLine("250 AUTH PLAIN")
			} else {
				tp.PrintfLine("250-test")
				tp.PrintfLine("250 STARTTLS")
			}
		case "STARTTLS":
			tp.PrintfLine("220 go ahead")
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{s.cert}})
			if err := tc.Handshake(); err != nil {
				return
			}
			c, tp, secure = tc, textproto.NewConn(tc), true
		case "AUTH":
			b, _ := base64.StdEncoding.DecodeString(f[len(f)-1])
			p := strings.Split(string(b), "\x00")
			if secure && len(p) == 3 && p[1] == s.user && p[2] == s.pass {
				tp.PrintfLine("235 ok")
			} else {
				tp.PrintfLine("535 5.7.8 Username and Password not accepted")
			}
		case "MAIL":
			s.mu.Lock()
			s.from = strings.Trim(strings.TrimPrefix(strings.ToUpper(f[1][:5]), "FROM:")+f[1][5:], "<>")
			s.mu.Unlock()
			tp.PrintfLine("250 ok")
		case "RCPT":
			s.mu.Lock()
			s.rcpts = append(s.rcpts, strings.Trim(f[1][3:], "<>"))
			s.mu.Unlock()
			tp.PrintfLine("250 ok")
		case "DATA":
			tp.PrintfLine("354 go on")
			b, _ := tp.ReadDotBytes()
			s.mu.Lock()
			s.data = string(b)
			s.mu.Unlock()
			tp.PrintfLine("250 queued")
		case "QUIT":
			tp.PrintfLine("221 bye")
			return
		default:
			tp.PrintfLine("250 ok")
		}
	}
}

func TestSendAnswersThroughBridge(t *testing.T) {
	f := newFixture(t)
	smtpd := newSMTP(t, "alice@example.com", "bridge-pw")
	cfg := config.Mail{Accounts: map[string]config.MailAccount{
		"proton": {Kind: config.MailProton, Address: "alice@example.com", IMAP: f.srv.addr, SMTP: smtpd.addr},
	}}
	all, err := Accounts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.srv.add(t, "INBOX", plainMsg, time.Now())
	r := f.sv.List(ctx(t), all, ListArgs{Limit: 10, Fresh: time.Minute})
	if len(r.Messages) != 1 {
		t.Fatalf("messages = %+v", r.Messages)
	}
	d := ipc.MailDraft{From: "proton", To: []string{"bob@example.com"}, Bcc: []string{"dan@example.com"}, Subject: "Re: Lunch on Friday?", Body: "Yes!", Reply: r.Messages[0].ID}
	if err := f.sv.Send(ctx(t), all, d); err != nil {
		t.Fatal(err)
	}
	smtpd.mu.Lock()
	from, rcpts, data := smtpd.from, smtpd.rcpts, smtpd.data
	smtpd.mu.Unlock()
	if from != "alice@example.com" || !slices.Equal(rcpts, []string{"bob@example.com", "dan@example.com"}) {
		t.Errorf("from %q to %q", from, rcpts)
	}
	if !strings.Contains(data, "In-Reply-To: <lunch@example.com>") || !strings.Contains(data, "Yes!") {
		t.Errorf("sent:\n%s", data)
	}
	r = f.sv.List(ctx(t), all, ListArgs{Limit: 10, Fresh: time.Minute, Refresh: true})
	if !r.Messages[0].Answered {
		t.Error("the message answered isn't marked answered")
	}

	// A wrong password says what Bridge wants.
	if err := f.store.Set("alice@example.com", "wrong"); err != nil {
		t.Fatal(err)
	}
	if err := f.sv.Send(ctx(t), all, ipc.MailDraft{From: "proton", To: []string{"bob@example.com"}, Subject: "x", Body: "y"}); err == nil || !strings.Contains(err.Error(), "Bridge password") {
		t.Errorf("wrong password: %v", err)
	}
}

func TestTestSignsIn(t *testing.T) {
	f := newFixture(t)
	a := f.all[0]
	if err := f.sv.Test(ctx(t), a, "bridge-pw"); err != nil {
		t.Errorf("right password: %v", err)
	}
	if err := f.sv.Test(ctx(t), a, "wrong"); err == nil || !strings.Contains(err.Error(), "Bridge password") {
		t.Errorf("wrong password: %v", err)
	}
	g := Account{Name: "g", Kind: config.MailGmail, Address: "alice@example.com", Username: "alice@example.com", IMAP: a.IMAP}
	if err := f.sv.Test(ctx(t), g, "wrong"); err == nil || !strings.Contains(err.Error(), "app password") {
		t.Errorf("gmail's wrong password: %v", err)
	}
}

func TestFilesStore(t *testing.T) {
	s := Files{Dir: filepath.Join(t.TempDir(), "keys")}
	if _, err := s.Get("alice@example.com"); !errors.Is(err, ErrNoPassword) {
		t.Errorf("get before set: %v", err)
	}
	if err := s.Set("alice@example.com", "abcd efgh ijkl mnop"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get("Alice@Example.com"); err != nil || got != "abcd efgh ijkl mnop" {
		t.Errorf("get = %q, %v", got, err)
	}
	if st, err := os.Stat(s.Dir); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("dir mode: %v %v", st, err)
	}
	if err := s.Delete("alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Has("alice@example.com"); ok || err != nil {
		t.Errorf("has after delete: %v %v", ok, err)
	}
	for _, bad := range []string{"", "two\nlines", "\xff\xfe"} {
		if err := s.Set("alice@example.com", bad); err == nil {
			t.Errorf("password %q was taken", bad)
		}
	}
	if err := s.Set("../../etc/passwd", "x"); err == nil {
		t.Error("an address that climbs out was taken")
	}
	for in, want := range map[string]string{"deadbeef": "deadbeef", "70c3a4737377c3b67264": "pässwörd", "plain": "plain", "abc": "abc"} {
		if got := unhex(in); got != want {
			t.Errorf("unhex(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestKeychain keeps passwords in a keychain of its own in a temporary
// folder, never the login keychain, and leaves the keychain search list as
// it was.
func TestKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" || testing.Short() {
		t.Skip("macOS's Keychain")
	}
	path := filepath.Join(t.TempDir(), "test.keychain-db")
	if out, err := exec.Command("/usr/bin/security", "create-keychain", "-p", "test", path).CombinedOutput(); err != nil {
		t.Skipf("security create-keychain: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("/usr/bin/security", "delete-keychain", path).Run() })
	k := Keychain{Path: path}
	for _, pw := range []string{"abcd efgh ijkl mnop", "deadbeef", `p"a\ss'x`, "pässwörd✓"} {
		if err := k.Set("alice@example.com", pw); err != nil {
			t.Fatalf("set %q: %v", pw, err)
		}
		if got, err := k.Get("alice@example.com"); err != nil || got != pw {
			t.Errorf("get = %q, %v; want %q", got, err, pw)
		}
	}
	if ok, err := k.Has("alice@example.com"); !ok || err != nil {
		t.Errorf("has = %v, %v", ok, err)
	}
	if err := k.Delete("alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get("alice@example.com"); !errors.Is(err, ErrNoPassword) {
		t.Errorf("get after delete: %v", err)
	}
	if err := k.Delete("alice@example.com"); err != nil {
		t.Errorf("deleting none: %v", err)
	}
}

func TestSummaryAndTable(t *testing.T) {
	now := time.Date(2026, 11, 14, 15, 0, 0, 0, time.Local)
	r := ipc.MailResult{
		Accounts: []ipc.MailAccount{
			{Name: "personal", Provider: "Gmail", State: ipc.MailReady, Unread: 2},
			{Name: "proton", Provider: "Proton Mail", State: ipc.MailError, Problem: "Proton Mail Bridge isn't running"},
		},
		Messages: []ipc.MailMessage{
			{Account: "personal", From: "Bob", Subject: "Lunch on Friday?", Date: now.Add(-time.Hour), Unread: true},
			{Account: "personal", From: "News", Subject: "", Date: now.AddDate(0, 0, -3), Starred: true},
		},
	}
	s := Summary(r, now)
	for _, want := range []string{"Mail: 2 unread in 2 accounts.", "personal (Gmail): 2 unread", "proton (Proton Mail): couldn't be read: Proton Mail Bridge isn't running", "Lunch on Friday?", "14:00"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
	tb := Table(r, now)
	if !strings.Contains(tb, "! proton (Proton Mail)") || !strings.Contains(tb, "● personal") || !strings.Contains(tb, "★ personal") || !strings.Contains(tb, "(no subject)") {
		t.Errorf("table:\n%s", tb)
	}
	if Summary(ipc.MailResult{}, now) == "" {
		t.Error("no summary without accounts")
	}
	for when, want := range map[time.Time]string{
		now.Add(-time.Hour):   "14:00",
		now.AddDate(0, 0, -2): now.AddDate(0, 0, -2).Format("Mon"),
		now.AddDate(0, -2, 0): now.AddDate(0, -2, 0).Format("2 Jan"),
		now.AddDate(-1, 0, 0): now.AddDate(-1, 0, 0).Format("2 Jan 2006"),
		time.Time{}:           "",
	} {
		if got := When(when, now); got != want {
			t.Errorf("When(%v) = %q, want %q", when, got, want)
		}
	}
}
