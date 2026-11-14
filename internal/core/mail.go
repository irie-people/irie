//go:build !nomail

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/mail"
)

// The mail plugin's part of the core (docs/proposals/mail.md): the mail op,
// which every client's Mail draws from. go build -tags nomail leaves this
// file out, and mail_off.go stands in.

const mailBuilt = true

// mailState is the mail client, made at the first call.
type mailState struct {
	once sync.Once
	sv   *mail.Service
}

// mailWait is how long one call may take: a list reads every account at
// once, so this is one slow server's time.
const mailWait = 40 * time.Second

func (c *Core) mailService() *mail.Service {
	c.mail.once.Do(func() {
		dir := filepath.Join(c.paths.State, "mail")
		c.mail.sv = &mail.Service{
			Store: mail.DefaultStore(filepath.Join(dir, "keys")),
			Pins:  &mail.Pins{Path: filepath.Join(dir, "pins.json")},
		}
	})
	return c.mail.sv
}

// mailClose closes the mail connections, as the core stops.
func (c *Core) mailClose() {
	if c.mail.sv != nil {
		c.mail.sv.Close()
	}
}

// mailLive is how each account stands by the core's last look, for the
// plugin's parts: it reads nothing.
func (c *Core) mailLive() []ipc.MailAccount {
	all, err := mail.Accounts(c.cfg().Mail)
	if err != nil || len(all) == 0 {
		return nil
	}
	return c.mailService().Accounts(all)
}

// mailOp answers the mail op. peer is the process that asks: mail is the
// person's alone, so a call from inside an agent is refused, a look as
// much as a send, whatever the words in a message ask of it.
func (c *Core) mailOp(peer int, a ipc.MailArgs) (ipc.MailResult, error) {
	cfg := c.cfg()
	if !cfg.MailOn() {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "Mail is off, as its plugin is: `%s plugins on mail` turns it on.", brand.Slug)
	}
	if c.fromAgent(peer) {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "Mail is yours alone: an agent can't read, change or send it. Open it in %s's Mail.", brand.Name)
	}
	all, err := mail.Accounts(cfg.Mail)
	if err != nil {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	sv := c.mailService()
	ctx, cancel := context.WithTimeout(context.Background(), mailWait)
	defer cancel()
	done := func(err error) (ipc.MailResult, error) {
		if err != nil {
			var ie *ipc.Error
			if errors.As(err, &ie) {
				return ipc.MailResult{}, err
			}
			return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", said(err))
		}
		r := ipc.MailResult{Accounts: sv.Accounts(all)}
		r.Text = mail.Summary(r, time.Now())
		return r, nil
	}
	switch a.Do {
	case ipc.MailList:
		limit := a.Limit
		if limit <= 0 {
			limit = cfg.Mail.Fetch
		}
		r := sv.List(ctx, all, mail.ListArgs{
			Accounts: a.Accounts, Folder: a.Folder, Query: strings.TrimSpace(a.Query),
			Limit: min(limit, 500), Refresh: a.Refresh, Fresh: cfg.Mail.Fresh,
		})
		r.Text = mail.Summary(r, time.Now())
		return r, nil
	case ipc.MailRead:
		body, err := sv.Read(ctx, all, a.ID, a.Seen)
		if err != nil {
			return done(err)
		}
		r, _ := done(nil)
		r.Message = &body
		return r, nil
	case ipc.MailFlag:
		return done(sv.Flag(ctx, all, a.ID, a.Flag))
	case ipc.MailMove:
		return done(sv.Move(ctx, all, a.ID, a.To))
	case ipc.MailSend:
		if a.Draft == nil {
			return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "Send what? The draft is missing.")
		}
		if err := sv.Send(ctx, all, *a.Draft); err != nil {
			return done(err)
		}
		r, _ := done(nil)
		r.Text = "Sent."
		return r, nil
	case ipc.MailAdd:
		return c.mailAdd(ctx, sv, all, a.New)
	case ipc.MailRemove:
		return c.mailRemove(sv, all, a.Name)
	case ipc.MailTest, ipc.MailTrust:
		x, ok := mail.Find(all, a.Name)
		if !ok {
			return ipc.MailResult{}, ipc.Errorf(ipc.CodeNotFound, "No mail account is called %s.", a.Name)
		}
		if a.Do == ipc.MailTrust {
			for _, s := range []mail.Server{x.IMAP, x.SMTP} {
				if s.Local() {
					if err := sv.Pins.Forget(s.Addr()); err != nil {
						return done(err)
					}
				}
			}
		}
		sv.Forget(x.Name)
		r := sv.List(ctx, all, mail.ListArgs{Accounts: []string{x.Name}, Limit: cfg.Mail.Fetch, Refresh: true, Fresh: cfg.Mail.Fresh})
		r.Messages = nil
		for _, s := range r.Accounts {
			if s.Name == x.Name {
				r.Text = x.Name + ": " + s.State
				if s.Problem != "" {
					r.Text += ". " + s.Problem
				}
			}
		}
		return r, nil
	}
	return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "The mail op has no %q: list, read, flag, move, send, add, remove, test or trust.", a.Do)
}

// mailAdd adds an account once its password signs in, or, for an address
// already added, takes its new password.
func (c *Core) mailAdd(ctx context.Context, sv *mail.Service, all []mail.Account, n *ipc.MailNew) (ipc.MailResult, error) {
	if n == nil {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "Add which account? Its kind, address and password are missing.")
	}
	n.Address = strings.TrimSpace(n.Address)
	if !config.MailAddress(n.Address) {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "%q isn't an email address.", n.Address)
	}
	password := n.Password
	switch n.Kind {
	case config.MailGmail:
		// Google shows an app password in four groups of four.
		password = strings.ReplaceAll(strings.TrimSpace(password), " ", "")
	case config.MailProton:
		// Bridge's passwords have no spaces; a paste may bring some along.
		password = strings.TrimSpace(password)
	}
	if err := mail.CheckPassword(password); err != nil {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", said(err))
	}
	taken := map[string]bool{}
	for _, x := range all {
		taken[x.Name] = true
		if strings.EqualFold(x.Address, n.Address) && x.Kind == n.Kind {
			// The same account: a new password for it.
			if err := sv.Test(ctx, x, password); err != nil {
				return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", said(err))
			}
			if err := sv.Store.Set(x.Address, password); err != nil {
				return ipc.MailResult{}, fmt.Errorf("the Keychain: %v", err)
			}
			sv.Forget(x.Name)
			r := ipc.MailResult{Accounts: sv.Accounts(all)}
			r.Text = "Took the new password for " + x.Name + "."
			return r, nil
		}
	}
	name := strings.TrimSpace(n.Name)
	if name == "" {
		name = mail.NameFor(n.Address, n.Kind, taken)
	}
	if !config.MailName.MatchString(name) {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "%q can't name an account: lower case letters, digits and -, at most 24.", name)
	}
	if taken[name] {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "There is already an account called %s.", name)
	}
	acct := config.MailAccount{Kind: n.Kind, Address: n.Address, Username: strings.TrimSpace(n.Username), IMAP: strings.TrimSpace(n.IMAP), SMTP: strings.TrimSpace(n.SMTP), Colour: n.Colour}
	cfg := *c.cfg()
	cfg.Mail.Accounts = map[string]config.MailAccount{name: acct}
	if err := cfg.Validate(); err != nil {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	one, err := mail.Accounts(cfg.Mail)
	if err != nil {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	if err := sv.Test(ctx, one[0], password); err != nil {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", said(err))
	}
	if err := sv.Store.Set(n.Address, password); err != nil {
		return ipc.MailResult{}, fmt.Errorf("the Keychain: %v", err)
	}
	key := "mail.accounts." + name + "."
	sets := []ipc.ConfigSet{{Key: key + "kind", Value: jsonString(acct.Kind)}, {Key: key + "address", Value: jsonString(acct.Address)}}
	for k, v := range map[string]string{"username": acct.Username, "imap": acct.IMAP, "smtp": acct.SMTP} {
		if v != "" {
			sets = append(sets, ipc.ConfigSet{Key: key + k, Value: jsonString(v)})
		}
	}
	if acct.Colour != 0 {
		sets = append(sets, ipc.ConfigSet{Key: key + "colour", Value: json.RawMessage(fmt.Sprint(acct.Colour))})
	}
	if err := c.configure(c.paths.ConfigFile(), sets); err != nil {
		return ipc.MailResult{}, err
	}
	all, _ = mail.Accounts(c.cfg().Mail)
	r := sv.List(ctx, all, mail.ListArgs{Accounts: []string{name}, Limit: c.cfg().Mail.Fetch, Fresh: c.cfg().Mail.Fresh})
	r.Messages = nil
	r.Text = fmt.Sprintf("Added %s (%s, %s).", name, mail.Provider(acct.Kind), acct.Address)
	return r, nil
}

// mailRemove removes the account from config.toml, and its password from
// the Keychain unless another account signs in with it.
func (c *Core) mailRemove(sv *mail.Service, all []mail.Account, name string) (ipc.MailResult, error) {
	x, ok := mail.Find(all, name)
	if !ok {
		return ipc.MailResult{}, ipc.Errorf(ipc.CodeNotFound, "No mail account is called %s.", name)
	}
	if err := c.configure(c.paths.ConfigFile(), []ipc.ConfigSet{{Key: "mail.accounts." + x.Name, Value: json.RawMessage("null")}}); err != nil {
		return ipc.MailResult{}, err
	}
	sv.Forget(x.Name)
	shared := false
	for _, o := range all {
		shared = shared || (o.Name != x.Name && strings.EqualFold(o.Address, x.Address))
	}
	if !shared {
		if err := sv.Store.Delete(x.Address); err != nil {
			c.log.Warn("mail", "remove", x.Name, "err", err)
		}
	}
	rest, _ := mail.Accounts(c.cfg().Mail)
	r := ipc.MailResult{Accounts: sv.Accounts(rest)}
	r.Text = "Removed " + x.Name + "."
	return r, nil
}

// said is an error as a sentence a person reads: a capital, a full stop.
func said(err error) string {
	s := strings.TrimSpace(err.Error())
	r, n := utf8.DecodeRuneInString(s)
	if n > 0 {
		s = string(unicode.ToUpper(r)) + s[n:]
	}
	if last, _ := utf8.DecodeLastRuneInString(s); s != "" && !strings.ContainsRune(".!?…:)", last) {
		s += "."
	}
	return s
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
