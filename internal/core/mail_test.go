//go:build !nomail

package core

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
)

type quietLog struct{}

func (quietLog) Printf(string, ...any) {}

// bridge is an IMAP server on 127.0.0.1 with STARTTLS and a certificate it
// signs itself, as Proton Mail Bridge is, with one user.
func bridge(t *testing.T, user, password string) (string, *imapmemserver.User) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	mem := imapmemserver.New()
	u := imapmemserver.NewUser(user, password)
	for _, m := range []string{"INBOX", "Archive", "Sent", "Trash"} {
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
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}},
		Logger:    quietLog{},
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String(), u
}

// TestTheMailOp: an account comes in only once its password signs in,
// with the password in the store and the rest in config.toml; its mail
// lists; :mail says how the accounts stand; and the account goes out
// whole, its password too (docs/proposals/mail.md).
func TestTheMailOp(t *testing.T) {
	t.Setenv(brand.Env("KEYCHAIN"), "off")
	c := bareCore(t, config.Default())
	t.Cleanup(c.mailClose)
	r, err := c.mailOp(0, ipc.MailArgs{})
	if err != nil || len(r.Accounts) != 0 || !strings.Contains(r.Text, "no accounts yet") {
		t.Fatalf("no accounts: %+v, %v", r, err)
	}
	addr, user := bridge(t, "alice@example.com", "bridge-pw")
	raw := "From: Bob <bob@example.com>\r\nTo: alice@example.com\r\nSubject: Lunch?\r\nDate: Mon, 09 Nov 2026 10:00:00 +0000\r\n\r\nFriday?\r\n"
	if _, err := user.Append("INBOX", strings.NewReader(raw), &imap.AppendOptions{Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
	add := ipc.MailNew{Kind: config.MailProton, Address: "alice@example.com", IMAP: addr, SMTP: "127.0.0.1:1025", Password: "wrong"}
	if _, err := c.mailOp(0, ipc.MailArgs{Do: ipc.MailAdd, New: &add}); err == nil || !strings.Contains(err.Error(), "Bridge password") {
		t.Fatalf("wrong password: %v", err)
	}
	if len(c.cfg().Mail.Accounts) != 0 {
		t.Fatal("an account whose password failed was kept")
	}
	add.Password = "bridge-pw"
	r, err = c.mailOp(0, ipc.MailArgs{Do: ipc.MailAdd, New: &add})
	if err != nil || r.Text != "Added alice (Proton Mail, alice@example.com)." {
		t.Fatalf("add: %+v, %v", r, err)
	}
	if a := c.cfg().Mail.Accounts["alice"]; a.Kind != config.MailProton || a.IMAP != addr {
		t.Errorf("config: %+v", c.cfg().Mail.Accounts)
	}
	b, _ := os.ReadFile(c.paths.ConfigFile())
	if !strings.Contains(string(b), "[mail.accounts.alice]") || strings.Contains(string(b), "bridge-pw") {
		t.Errorf("config.toml:\n%s", b)
	}
	keys := filepath.Join(c.paths.State, "mail", "keys")
	if p, err := os.ReadFile(filepath.Join(keys, "alice@example.com")); err != nil || strings.TrimSpace(string(p)) != "bridge-pw" {
		t.Errorf("stored password: %q, %v", p, err)
	}

	r, err = c.mailOp(0, ipc.MailArgs{})
	if err != nil || len(r.Messages) != 1 || r.Messages[0].Subject != "Lunch?" || r.Accounts[0].Unread != 1 || r.Accounts[0].Colour != 0 {
		t.Fatalf("list: %+v, %v", r, err)
	}
	in, err := c.input(ipc.InputArgs{Text: ":mail"}, 0)
	if err != nil || in.Did != ipc.InputAnswered || in.Op != ipc.OpMail || !strings.Contains(in.Text, "Mail: 1 unread in 1 account.") || !strings.Contains(in.Text, "Lunch?") {
		t.Errorf(":mail = %+v, %v", in, err)
	}
	body, err := c.mailOp(0, ipc.MailArgs{Do: ipc.MailRead, ID: r.Messages[0].ID, Seen: true})
	if err != nil || body.Message == nil || body.Message.Text != "Friday?" || body.Accounts[0].Unread != 0 {
		t.Errorf("read: %+v, %v", body, err)
	}

	// The same address again takes a new password, once it signs in.
	again := add
	again.Password = "wrong"
	if _, err := c.mailOp(0, ipc.MailArgs{Do: ipc.MailAdd, New: &again}); err == nil {
		t.Error("a new password that fails was taken")
	}

	// The plugin's parts know the account.
	pr, _ := c.pluginsOp(ipc.PluginsArgs{})
	for _, x := range pr.Plugins[2].Parts {
		if x.Name == "proton" && !strings.Contains(x.Detail, "alice") && !strings.Contains(x.Detail, "Bridge") {
			t.Errorf("proton part: %+v", x)
		}
	}

	r, err = c.mailOp(0, ipc.MailArgs{Do: ipc.MailRemove, Name: "alice"})
	if err != nil || len(r.Accounts) != 0 || len(c.cfg().Mail.Accounts) != 0 {
		t.Fatalf("remove: %+v, %v", r, err)
	}
	if b, _ := os.ReadFile(c.paths.ConfigFile()); strings.Contains(string(b), "alice") {
		t.Errorf("config.toml after remove:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(keys, "alice@example.com")); !os.IsNotExist(err) {
		t.Errorf("the password stayed: %v", err)
	}
}

// TestAnAgentCannotReadMail: mail is the person's alone, so every call from
// inside an agent is refused, typed words too; and the plugin off refuses
// every call.
func TestAnAgentCannotReadMail(t *testing.T) {
	t.Setenv(brand.Env("KEYCHAIN"), "off")
	c := bareCore(t, config.Default())
	// This test's own parent stands for an agent's host.
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	for _, a := range []ipc.MailArgs{{}, {Do: ipc.MailRead, ID: "x|1|1|INBOX"}, {Do: ipc.MailSend, Draft: &ipc.MailDraft{From: "x"}}} {
		if _, err := c.mailOp(os.Getpid(), a); err == nil || !strings.Contains(err.Error(), "an agent can't read") {
			t.Errorf("%+v from an agent: %v", a, err)
		}
	}
	if _, err := c.CallFrom(os.Getpid(), ipc.OpMail, []byte(`{}`)); err == nil {
		t.Error("the op from an agent went through")
	}
	if _, err := c.input(ipc.InputArgs{Text: ":mail"}, os.Getpid()); err == nil {
		t.Error(":mail from an agent went through")
	}
	if _, err := c.mailOp(0, ipc.MailArgs{}); err != nil {
		t.Errorf("the person's own call: %v", err)
	}
	cfg := config.Default()
	cfg.Plugins.Mail = false
	c = bareCore(t, cfg)
	if _, err := c.mailOp(0, ipc.MailArgs{}); err == nil || !strings.Contains(err.Error(), "plugins on mail") {
		t.Errorf("plugin off: %v", err)
	}
}
