//go:build !nomail

package plugins

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// The mail plugin's parts, by name.
const (
	MailGmail  = "gmail"
	MailProton = "proton"
)

// bridgeApps are where the Proton part looks for Proton Mail Bridge: the
// Applications folders its installer and Homebrew put it in. Tests point
// them elsewhere.
var bridgeApps = []string{"/Applications/Proton Mail Bridge.app", "~/Applications/Proton Mail Bridge.app"}

// bridgeSite is where Proton Mail Bridge comes from.
const bridgeSite = "https://proton.me/mail/bridge"

// appPasswords is Google's page for making an app password.
const appPasswords = "https://myaccount.google.com/apppasswords"

func init() { built["mail"] = mailPlugin }

// mailPlugin is the mail plugin: Irie's own mail client, every account in
// one list, each in a colour of its own.
func mailPlugin() Plugin {
	return Plugin{
		Name:    "mail",
		Title:   "Mail",
		Summary: "Your Gmail and Proton Mail inboxes in one list inside " + brand.Name + ", each account in a colour of its own. What it reads stays in memory, and agents can't reach it.",
		key:     "plugins.mail",
		on:      func(c config.Config) bool { return c.Plugins.Mail },
		Parts: []Part{
			{
				Name:    MailGmail,
				Title:   "Gmail",
				Summary: "Your Gmail accounts, read and sent over IMAP and SMTP with an app password, kept in the Keychain.",
				Setup:   ":mail",
				Brings:  []string{":mail", "Mail in the right sidebar and its own window", brand.Slug + " mail"},
				own:     func(config.Config) bool { return true },
				check:   func(e Env, on bool) (bool, string) { return checkMail(e, config.MailGmail) },
			},
			{
				Name:    MailProton,
				Title:   "Proton Mail",
				Summary: "Your Proton Mail accounts, through Proton Mail Bridge on this computer, which keeps them end-to-end encrypted up to here.",
				Setup:   ":mail",
				Brings:  []string{":mail", "Mail in the right sidebar and its own window", brand.Slug + " mail"},
				own:     func(config.Config) bool { return true },
				check:   func(e Env, on bool) (bool, string) { return checkMail(e, config.MailProton) },
			},
		},
	}
}

// checkMail says how a kind's accounts stand: from config.toml, whether
// Proton Mail Bridge is installed, and the core's last look at each, which
// it keeps in memory. It reads no password and asks no server.
func checkMail(e Env, kind string) (bool, string) {
	var names []string
	for name, a := range e.Config.Mail.Accounts {
		if a.Kind == kind {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if kind == config.MailProton && !bridgeInstalled() {
		if len(names) == 0 {
			return false, "Proton Mail Bridge isn't installed: it comes from " + bridgeSite + ", for Proton's paid plans. Sign in to it, then Add Account in Mail with the Bridge password it shows."
		}
		return false, "Proton Mail Bridge isn't installed, and " + strings.Join(names, ", ") + " read through it: it comes from " + bridgeSite + "."
	}
	if len(names) == 0 {
		if kind == config.MailGmail {
			return false, "No Gmail account yet: Add Account in Mail, with an app password from " + appPasswords + "."
		}
		return false, "No Proton Mail account yet: sign in to Proton Mail Bridge, then Add Account in Mail with the Bridge password it shows."
	}
	var bad []string
	for _, a := range e.Live.Mail {
		if a.Kind != kind {
			continue
		}
		switch a.State {
		case ipc.MailNeeds:
			bad = append(bad, a.Name+" needs its password")
		case ipc.MailError:
			bad = append(bad, a.Name+": "+strings.TrimSuffix(a.Problem, "."))
		}
	}
	if len(bad) > 0 {
		return false, strings.Join(bad, "; ") + "."
	}
	what := "account"
	if len(names) > 1 {
		what = "accounts"
	}
	return true, fmt.Sprintf("Ready: %d %s (%s). :mail, or Mail in the right sidebar, reads them.", len(names), what, strings.Join(names, ", "))
}

func bridgeInstalled() bool {
	for _, app := range bridgeApps {
		if st, err := os.Stat(expand(app)); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}
