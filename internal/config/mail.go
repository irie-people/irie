package config

import (
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
)

// The mail plugin's settings (docs/proposals/mail.md) live here, so the
// config needs none of the plugin's packages: a build without the plugin
// (go build -tags nomail) leaves them out of the binary, and still reads a
// config.toml that sets them.

// Mail is the mail plugin's accounts: Gmail, Proton Mail through Proton
// Mail Bridge, and any other IMAP account, read in one list. Each
// account's password lives in the macOS Keychain, never in this file.
type Mail struct {
	// Accounts are the mail accounts, by the short name each message
	// shows, such as personal or proton.
	Accounts map[string]MailAccount `toml:"accounts"`
	// Fetch is how many of a folder's newest messages each account shows.
	Fetch int `toml:"fetch"`
	// Fresh is how long a folder's messages stay fresh: a look after it
	// reads the account again.
	Fresh time.Duration `toml:"fresh"`
}

// MailAccount is one mail account.
type MailAccount struct {
	Kind     string `toml:"kind"`     // gmail, proton or imap
	Address  string `toml:"address"`  // its email address, which it sends from
	Username string `toml:"username"` // its login; "" = the address
	// IMAP and SMTP are its servers as host:port, "" for the kind's own:
	// Gmail's, or Proton Mail Bridge's on this computer. Port 993 or 465,
	// or a tls:// in front, means TLS from the start; any other port, or
	// starttls://, STARTTLS. A server that offers neither is refused.
	IMAP string `toml:"imap"`
	SMTP string `toml:"smtp"`
	// Colour is which of Irie's six colours marks its messages, 1 to 6;
	// 0 = Irie picks, so accounts side by side differ.
	Colour int `toml:"colour"`
}

// The kinds of mail account.
const (
	MailGmail  = "gmail"
	MailProton = "proton"
	MailIMAP   = "imap"
)

// MailKinds are the kinds, in the order Irie lists accounts by.
var MailKinds = []string{MailGmail, MailProton, MailIMAP}

// MailColours is how many colours an account can take.
const MailColours = 6

// MailName is the form of an account's name: short, lower case, as it
// shows on every message.
var MailName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,23}$`)

// mailAddress is a plain address, user@host, with nothing a command line
// or a keychain query would read as more than one word.
var mailAddress = regexp.MustCompile(`^[^\s"'\\<>(),;:@]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)

// MailAddress says whether s is a plain email address, user@host.
func MailAddress(s string) bool { return len(s) <= 254 && mailAddress.MatchString(s) }

// MailServer reads an account's imap or smtp setting: its host, its port
// and whether TLS starts at once rather than through STARTTLS.
func MailServer(s string) (host string, port int, tls bool, err error) {
	rest := s
	scheme := ""
	if i := strings.Index(s, "://"); i >= 0 {
		scheme, rest = s[:i], s[i+3:]
		if scheme != "tls" && scheme != "starttls" {
			return "", 0, false, fmt.Errorf("must start tls:// or starttls://, or be host:port")
		}
	}
	h, p, err := net.SplitHostPort(rest)
	if err != nil {
		return "", 0, false, fmt.Errorf("must be host:port")
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return "", 0, false, fmt.Errorf("must be host:port, with a port from 1 to 65535")
	}
	if h == "" || strings.ContainsAny(h, " /\\\"'") {
		return "", 0, false, fmt.Errorf("%q is not a host name or address", h)
	}
	switch scheme {
	case "tls":
		tls = true
	case "starttls":
		tls = false
	default:
		tls = n == 993 || n == 465
	}
	return h, n, tls, nil
}

func defaultMail() Mail {
	return Mail{Accounts: map[string]MailAccount{}, Fetch: 50, Fresh: time.Minute}
}

// checkMail adds what is wrong with the mail settings.
func checkMail(m Mail, bad func(string, ...any)) {
	if m.Fetch < 1 || m.Fetch > 500 {
		bad("mail.fetch = %d: must be from 1 to 500", m.Fetch)
	}
	if m.Fresh < 10*time.Second {
		bad("mail.fresh = %s: must be at least 10s, so "+brand.Name+" asks the servers no more than they allow", m.Fresh)
	}
	for _, name := range sortedKeys(m.Accounts) {
		a := m.Accounts[name]
		key := "mail.accounts." + name
		if !MailName.MatchString(name) {
			bad("%s: an account's name is lower case letters, digits and -, at most 24, such as personal", key)
		}
		if !slices.Contains(MailKinds, a.Kind) {
			bad("%s.kind = %q: must be gmail, proton or imap", key, a.Kind)
		}
		if !MailAddress(a.Address) {
			bad("%s.address = %q: must be an email address, such as alice@example.com", key, a.Address)
		}
		if a.Username != "" && (strings.ContainsAny(a.Username, " \t\r\n\"'\\") || len(a.Username) > 254) {
			bad("%s.username = %q: must be one word, or empty for the address", key, a.Username)
		}
		for _, s := range []struct{ name, value string }{{"imap", a.IMAP}, {"smtp", a.SMTP}} {
			if s.value == "" {
				if a.Kind == MailIMAP {
					bad("%s.%s: an imap account names its server, host:port", key, s.name)
				}
				continue
			}
			if _, _, _, err := MailServer(s.value); err != nil {
				bad("%s.%s = %q: %v", key, s.name, s.value, err)
			}
		}
		if a.Colour < 0 || a.Colour > MailColours {
			bad("%s.colour = %d: must be from 1 to %d, or 0 for "+brand.Name+" to pick", key, a.Colour, MailColours)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
