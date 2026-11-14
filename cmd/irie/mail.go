//go:build !nomail

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/mail"
)

// The mail plugin's command (docs/proposals/mail.md): irie mail, which asks
// the core, where the accounts' connections are. go build -tags nomail
// leaves this file out.

// mailUsage is the mail plugin's lines in the usage.
const mailUsage = `  ` + brand.Slug + ` mail [<command>]   your mail: the newest messages of every account,
                          one message, adding and removing accounts
                          (` + brand.Slug + ` mail help)
`

const mailHelp = `usage:
  ` + brand.Slug + ` mail [--folder F] [--account NAME] [--search WORDS] [--limit N] [--refresh]
                       the newest messages of every account, or one's,
                       newest first; F is inbox, starred, sent, drafts,
                       archive, junk or trash
  ` + brand.Slug + ` mail accounts        each account and how it stands
  ` + brand.Slug + ` mail read ID         a message, whole (IDs come from --ids)
  ` + brand.Slug + ` mail add gmail|proton|imap ADDRESS [--name NAME] [--username U]
                       [--imap HOST:PORT] [--smtp HOST:PORT]
                       add an account, once its password signs in: a Gmail
                       app password, made at https://myaccount.google.com/apppasswords,
                       or the Bridge password Proton Mail Bridge shows; the
                       password goes to the Keychain, never config.toml.
                       For an address already added, it takes a new password
  ` + brand.Slug + ` mail remove NAME     remove an account and its password
  ` + brand.Slug + ` mail test NAME       sign in to an account again
  ` + brand.Slug + ` mail trust NAME      trust the certificate Proton Mail Bridge shows now,
                       after it was installed again
`

func init() {
	pluginCmds["mail"] = pluginCmd{run: func(args []string) error { return mailCmd(args, os.Stdin, os.Stdout) }}
}

func mailCall(a ipc.MailArgs, wait time.Duration) (ipc.MailResult, error) {
	_, p, err := load()
	if err != nil {
		return ipc.MailResult{}, err
	}
	var r ipc.MailResult
	if err := ipc.Call(p.Socket(), wait, ipc.OpMail, a, &r); err != nil {
		return ipc.MailResult{}, err
	}
	return r, nil
}

func mailCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "", "list":
		fs := flag.NewFlagSet("mail", flag.ContinueOnError)
		folder := fs.String("folder", "inbox", "the folder")
		account := fs.String("account", "", "only this account")
		search := fs.String("search", "", "only messages with these words")
		limit := fs.Int("limit", 0, "how many of each account's newest")
		refresh := fs.Bool("refresh", false, "read the accounts again now")
		ids := fs.Bool("ids", false, "print each message's ID, for mail read")
		if err := fs.Parse(args); err != nil {
			return err
		}
		a := ipc.MailArgs{Folder: *folder, Query: *search, Limit: *limit, Refresh: *refresh}
		if *account != "" {
			a.Accounts = []string{*account}
		}
		r, err := mailCall(a, 60*time.Second)
		if err != nil {
			return err
		}
		if len(r.Accounts) == 0 {
			fmt.Fprintln(stdout, mail.Summary(r, time.Now()))
			return nil
		}
		fmt.Fprint(stdout, mail.Table(r, time.Now()))
		if *ids {
			for _, m := range r.Messages {
				fmt.Fprintf(stdout, "%s\t%s\n", m.ID, m.Subject)
			}
		}
		return nil
	case "accounts":
		r, err := mailCall(ipc.MailArgs{Limit: 1}, 60*time.Second)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, mail.Summary(ipc.MailResult{Accounts: r.Accounts}, time.Now()))
		return nil
	case "read":
		if len(args) != 1 {
			return errors.New("usage: " + brand.Slug + " mail read ID")
		}
		r, err := mailCall(ipc.MailArgs{Do: ipc.MailRead, ID: args[0], Seen: true}, 60*time.Second)
		if err != nil {
			return err
		}
		writeMessage(stdout, *r.Message)
		return nil
	case "add":
		return mailAdd(args, stdin, stdout)
	case "remove", "test", "trust":
		if len(args) != 1 {
			return fmt.Errorf("usage: %s mail %s NAME", brand.Slug, sub)
		}
		do := map[string]string{"remove": ipc.MailRemove, "test": ipc.MailTest, "trust": ipc.MailTrust}[sub]
		r, err := mailCall(ipc.MailArgs{Do: do, Name: args[0]}, 60*time.Second)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, r.Text)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(stdout, mailHelp)
		return nil
	}
	return fmt.Errorf("unknown mail command %q\n\n%s", sub, mailHelp)
}

func mailAdd(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) < 2 {
		return errors.New("usage: " + brand.Slug + " mail add gmail|proton|imap ADDRESS [--name NAME]")
	}
	kind, address := args[0], args[1]
	fs := flag.NewFlagSet("mail add", flag.ContinueOnError)
	name := fs.String("name", "", "the account's short name")
	username := fs.String("username", "", "its login, when not the address")
	imapAddr := fs.String("imap", "", "its IMAP server, host:port")
	smtpAddr := fs.String("smtp", "", "its SMTP server, host:port")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	switch kind {
	case config.MailGmail:
		fmt.Fprintln(stdout, "Gmail takes an app password: make one at https://myaccount.google.com/apppasswords (2-Step Verification must be on).")
	case config.MailProton:
		fmt.Fprintln(stdout, "Proton Mail takes the Bridge password: open Proton Mail Bridge, pick the account, and copy the password from its mailbox details.")
	case config.MailIMAP:
	default:
		return fmt.Errorf("%q is no kind of account: gmail, proton or imap", kind)
	}
	password, err := readMailPassword(stdin, stdout, "Password for "+address)
	if err != nil {
		return err
	}
	r, err := mailCall(ipc.MailArgs{Do: ipc.MailAdd, New: &ipc.MailNew{
		Name: *name, Kind: kind, Address: address, Username: *username, IMAP: *imapAddr, SMTP: *smtpAddr, Password: password,
	}}, 90*time.Second)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, r.Text)
	return nil
}

// readMailPassword reads a password from the input: from a terminal without
// showing it, after a prompt.
func readMailPassword(stdin io.Reader, stdout io.Writer, prompt string) (string, error) {
	f, _ := stdin.(*os.File)
	if isTerminal(stdin) {
		fmt.Fprintf(stdout, "%s (it won't show): ", prompt)
		if err := mailStty(f, "-echo"); err == nil {
			defer func() {
				mailStty(f, "echo")
				fmt.Fprintln(stdout)
			}()
		}
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func mailStty(tty *os.File, arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = tty
	return cmd.Run()
}

// writeMessage prints a message whole, headers first.
func writeMessage(w io.Writer, m ipc.MailBody) {
	fmt.Fprintf(w, "From:    %s <%s>\n", m.From, m.FromAddr)
	if len(m.To) > 0 {
		fmt.Fprintf(w, "To:      %s\n", strings.Join(m.To, ", "))
	}
	if len(m.Cc) > 0 {
		fmt.Fprintf(w, "Cc:      %s\n", strings.Join(m.Cc, ", "))
	}
	fmt.Fprintf(w, "Date:    %s\n", m.Date.Local().Format("Mon 2 Jan 2006 15:04"))
	fmt.Fprintf(w, "Subject: %s\n", m.Subject)
	fmt.Fprintf(w, "Account: %s\n", m.Account)
	for _, a := range m.Attachments {
		fmt.Fprintf(w, "File:    %s (%s, %d KB)\n", a.Name, a.Type, (a.Size*3/4+1023)/1024)
	}
	fmt.Fprintf(w, "\n%s\n", m.Text)
}
