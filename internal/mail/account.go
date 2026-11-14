// Package mail is the mail plugin's mail client (docs/proposals/mail.md):
// Gmail, Proton Mail through Proton Mail Bridge, and any other IMAP
// account, read into one list, each account in a colour of its own.
//
// It speaks IMAP to read and SMTP to send, with TLS on every connection,
// and keeps what it reads in memory only: nothing of a message reaches a
// file. Each account's password lives in the macOS Keychain (secret.go).
package mail

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/config"
)

// Account is one mail account, with its servers worked out.
type Account struct {
	Name     string
	Kind     string // config.MailGmail, MailProton or MailIMAP
	Address  string
	Username string // its login: the address unless set
	IMAP     Server
	SMTP     Server
	Colour   int // 0 to 5, among Irie's six colours
}

// Server is a mail server.
type Server struct {
	Host string
	Port int
	TLS  bool // TLS from the start; false = STARTTLS
}

// Addr is its host:port.
func (s Server) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// Local says whether it runs on this computer, as Proton Mail Bridge does:
// its certificate is its own, so Irie trusts the first it sees (pins.go).
func (s Server) Local() bool {
	if s.Host == "localhost" {
		return true
	}
	ip := net.ParseIP(s.Host)
	return ip != nil && ip.IsLoopback()
}

// The kinds' own servers. Proton Mail Bridge listens on this computer, by
// default on 1143 for IMAP and 1025 for SMTP, both with STARTTLS.
var (
	gmailIMAP  = Server{Host: "imap.gmail.com", Port: 993, TLS: true}
	gmailSMTP  = Server{Host: "smtp.gmail.com", Port: 465, TLS: true}
	protonIMAP = Server{Host: "127.0.0.1", Port: 1143}
	protonSMTP = Server{Host: "127.0.0.1", Port: 1025}
)

// Provider is the name a kind shows under.
func Provider(kind string) string {
	switch kind {
	case config.MailGmail:
		return "Gmail"
	case config.MailProton:
		return "Proton Mail"
	}
	return "Mail"
}

// spread is the order colours go to accounts that leave the pick to Irie:
// each near the opposite side of the wheel from the one before, as cards
// side by side are (internal/core, hue.go).
var spread = []int{0, 3, 1, 4, 2, 5}

// Accounts are the configured accounts, Gmail's first, then Proton
// Mail's, then the rest, each kind by name, with their servers and
// colours worked out.
func Accounts(m config.Mail) ([]Account, error) {
	var out []Account
	for name, c := range m.Accounts {
		a, err := account(name, c)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b Account) int {
		if d := slices.Index(config.MailKinds, a.Kind) - slices.Index(config.MailKinds, b.Kind); d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	// An account's own colour first, then the rest from spread, skipping
	// any taken while some are free.
	used := map[int]bool{}
	for i, a := range out {
		if c := m.Accounts[a.Name].Colour; c > 0 {
			out[i].Colour = c - 1
			used[c-1] = true
		}
	}
	next := 0
	for i, a := range out {
		if m.Accounts[a.Name].Colour > 0 {
			continue
		}
		pick := spread[next%len(spread)]
		for tries := 0; used[pick] && tries < len(spread) && len(used) < len(spread); tries++ {
			next++
			pick = spread[next%len(spread)]
		}
		next++
		out[i].Colour = pick
		used[pick] = true
	}
	return out, nil
}

func account(name string, c config.MailAccount) (Account, error) {
	a := Account{Name: name, Kind: c.Kind, Address: c.Address, Username: c.Username}
	if a.Username == "" {
		a.Username = c.Address
	}
	switch c.Kind {
	case config.MailGmail:
		a.IMAP, a.SMTP = gmailIMAP, gmailSMTP
	case config.MailProton:
		a.IMAP, a.SMTP = protonIMAP, protonSMTP
	}
	for _, s := range []struct {
		value string
		to    *Server
	}{{c.IMAP, &a.IMAP}, {c.SMTP, &a.SMTP}} {
		if s.value == "" {
			continue
		}
		host, port, tls, err := config.MailServer(s.value)
		if err != nil {
			return Account{}, fmt.Errorf("mail.accounts.%s: %v", name, err)
		}
		*s.to = Server{Host: host, Port: port, TLS: tls}
	}
	if a.IMAP.Host == "" || a.SMTP.Host == "" {
		return Account{}, fmt.Errorf("mail.accounts.%s: an imap account names its imap and smtp servers", name)
	}
	return a, nil
}

// Find is the account named name.
func Find(all []Account, name string) (Account, bool) {
	for _, a := range all {
		if a.Name == name {
			return a, true
		}
	}
	return Account{}, false
}

// NameFor is a name for a new account at address: the address's own part
// before @, made a name, else the kind's, numbered past any taken.
func NameFor(address, kind string, taken map[string]bool) string {
	local, _, _ := strings.Cut(strings.ToLower(address), "@")
	var b strings.Builder
	for _, r := range local {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '-' || r == '_' || r == '+':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	base := strings.Trim(b.String(), "-")
	if len(base) > 20 {
		base = strings.TrimRight(base[:20], "-")
	}
	if !config.MailName.MatchString(base) {
		base = kind
	}
	name := base
	for n := 2; taken[name]; n++ {
		name = base + "-" + strconv.Itoa(n)
	}
	return name
}

// Web is the page of the provider's web mail for the account, where it has
// one.
func Web(a Account) string {
	switch a.Kind {
	case config.MailGmail:
		return "https://mail.google.com/mail/?authuser=" + url.QueryEscape(a.Address)
	case config.MailProton:
		return "https://mail.proton.me/"
	}
	return ""
}

// WebMessage is the message's page in the provider's web mail, found by
// its Message-ID, where the provider can.
func WebMessage(a Account, messageID string) string {
	if a.Kind != config.MailGmail || messageID == "" {
		return ""
	}
	return Web(a) + "#search/" + url.PathEscape("rfc822msgid:"+messageID)
}
