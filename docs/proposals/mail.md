# Proposal: Mail, a plugin that reads Gmail and Proton Mail in one list

Date: 2026-11-14. Status: built; what is left is under "Not yet". The owner asked for "an email client that integrates my Gmail inboxes with my Proton boxes, easily distinguishable and embeddable within Irie, as an email plugin". There is no issue tracker yet, so this file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote this file and the change it describes. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

## Goal

Every account's mail in one list inside Irie, beside the sessions: the newest first, whatever account it came to, and at a glance which account each message is in. Reading, answering, archiving and deleting happen there too, without a browser tab per account.

## What it is

Irie's third plugin, **Mail** (DESIGN §4.17), on by default: `[plugins] mail`, and `irie plugins off mail` turns it off. It shows nothing until an account is added. Its two parts are kinds of account:

| Part | How Irie reads it | Its check |
|---|---|---|
| Gmail | IMAP at `imap.gmail.com:993` and SMTP at `smtp.gmail.com:465`, both TLS from the start, signed in with an app password | ready once a Gmail account is added and the core's last look at it didn't fail |
| Proton Mail | Proton Mail Bridge on this computer, IMAP at `127.0.0.1:1143` and SMTP at `127.0.0.1:1025` with STARTTLS, signed in with the Bridge password | Bridge in an Applications folder, then as for Gmail |

Any other IMAP account works too (`kind = "imap"` with its own servers); it shows under neither part.

### Accounts

An account is a table in config.toml, by the short name every message of it shows:

```toml
[mail.accounts.personal]
kind = "gmail"                 # gmail, proton or imap
address = "alice@example.com"

[mail.accounts.proton]
kind = "proton"
address = "alice@proton.me"
# imap = "127.0.0.1:1143"     # only when Bridge's ports were changed
# colour = 3                  # 1 to 6; 0 or none = Irie picks
```

Add Account (in Mail) or `irie mail add gmail alice@example.com` adds one, and only once its password signs in: the core logs in, finds the inbox, and only then writes the table and keeps the password. The same address added again takes a new password. Removing an account removes its table whole and its password.

**Passwords** never touch config.toml. On macOS they are generic passwords in the login Keychain, service "Irie mail", account the address, written through `security -i` with the password as hex on its input, never on a command line where `ps` would show it. Elsewhere, and in test Iries (`IRIE_KEYCHAIN=off`), they are files of the user's alone in `<state>/mail/keys`. The core reads one only to open a connection.

- **Gmail** takes an app password, made at <https://myaccount.google.com/apppasswords> with 2-Step Verification on; Google doesn't offer one with security-key-only 2-Step Verification or Advanced Protection. IMAP has been always on in Gmail since January 2025.
- **Proton Mail** needs Proton Mail Bridge, which Proton offers on paid plans: Bridge signs in to Proton, keeps the mail end-to-end encrypted up to this computer, and serves it over IMAP on 127.0.0.1. Its password is the one in Bridge's Mailbox details, not the Proton password. In Bridge's split address mode each address is an account of its own.

### Telling accounts apart

Each account wears one of the agents' six colours (Theme.agent), which the core picks so accounts side by side are near opposite on the wheel, as cards are: Gmail accounts first, then Proton Mail's, each kind by name, taking colours in the order 0, 3, 1, 4, 2, 5; an account's own `colour` wins. A message shows its account's colour down its edge, as an unread dot, and in a badge under its subject with the account's name and its provider's mark (an envelope for Gmail, a shield for Proton Mail), so colour never tells accounts apart alone. The strip of accounts above the list, and the Mail window's sidebar, show one account's mail alone.

### Where it shows

- **The right sidebar's Mail tab** (⌥⌘7, or `:mail`): the list beside the sessions, with the folder (Inbox, Starred, Sent, Drafts, Archive, Junk, Trash, the same for every account), search across every account, and the strip of accounts. A message opens in place of the list. The tab's count is the unread mail, never red: unread mail is news, not something that needs you.
- **The Mail window** (Plugins › Mail › Open Mail Window, ⇧⌘M): folders and accounts, the list and the message side by side.
- **Reading**: the message's text, or its HTML read as text when that is all it has; or, with **As Sent**, the HTML as the sender laid it out, in a web view that runs no script, keeps no data and loads nothing from elsewhere (a content security policy allows only the page's own styles and inline pictures), so no tracking picture reports that it was read. A link opens in the browser. Its files are listed by name and size.
- **Acting**: Reply, Reply All and Forward open the composer, From set to the account the message came to; Send sends at once, threaded under the message answered, which is then marked answered. Gmail and Proton keep the copy in Sent themselves. Star, Mark as Unread, Archive (Gmail's All Mail), Move to Inbox, Junk and Move to Trash.
- **Plugins**: Settings › Plugins, the Plugins menu, `irie plugins` and `:plugins` show each part's state and what it lacks.
- **Words and commands**: `:mail` in any task bar says how the accounts stand and lists the newest unread; the Mac app opens the Mail tab instead. `irie mail` lists, `irie mail read`, `add`, `remove`, `test` and `trust` do the rest from a terminal (`irie mail help`).

### How it works

The core owns the mail, as it owns every rule ([CORE.md](../CORE.md)); the clients only draw. `internal/mail` keeps one IMAP connection per account (go-imap v2), opened at the first look, kept between looks so a message opens at once, and closed after five idle minutes. A list reads every account at once, each given at most 20 seconds: an account that fails, or takes longer, says why in its own row, and the others' mail shows. A folder's newest messages stay in the core's memory for `mail.fresh` (a minute), which the Mail views' once-a-minute look rides on, so the servers see no more than a look a minute while Mail shows, and none while it doesn't. Nothing a message holds is written to a file.

Each message's ID carries its account, its mailbox, its UID and the mailbox's UIDVALIDITY, so an ID can't name another message once a server renumbers a folder. The folders are found by the servers' special-use marks (RFC 6154), else by their usual names at the top or under `[Gmail]` or `INBOX`, never in a folder of the person's own. A move uses MOVE, and is refused on a server with neither MOVE nor UIDPLUS, where it would expunge every message marked deleted. A message is read part by part from its structure, the text and HTML parts only, decoded to UTF-8 from their character sets; a part over 2 MB is read to 2 MB.

**Security.**
- Every connection is TLS: a server that offers neither TLS nor STARTTLS is refused, and so is SMTP AUTH before STARTTLS.
- A server elsewhere must show a certificate the system trusts. A server on this computer, such as Bridge, signs its own, so the core trusts the first certificate it shows and only that one after (`<state>/mail/pins.json`), so another program that takes Bridge's port can't collect the password. After reinstalling Bridge, `irie mail trust NAME`, or the account's Trust Bridge's New Certificate, trusts the new one.
- **Mail is the person's alone.** The core refuses the mail op from any process inside an agent's pane, a look as much as a send, typed `:mail` included, as it refuses an agent's change to the VPN. Mail is where prompt injection arrives, and an agent that read it could be turned against its user or made to send.
- The Keychain item trusts `security(1)`, so another program of the user's that runs `security` can read it, as with any item `security` makes; the Mac app and core are not signed, so an item they made would ask again after each install.

**A build without it.** `go build -tags nomail` leaves the plugin out with its IMAP and MIME libraries: `internal/mail`, `internal/core/mail.go` (`mail_off.go` stands in), `cmd/irie/mail.go`, and the plugin's entry. `hello` leaves out the `mail` op, so the Mac app shows no Mail tab, window or menu items. `internal/plugins`' build test checks that `irie` links none of it.

**Tests.** `internal/mail`: against go-imap's in-memory server over STARTTLS with a self-signed certificate, as Bridge serves, and a small SMTP server of the test's own: the list across accounts, newest first, with the unread count; reading a multipart message in another character set with its HTML and its file; an HTML-only message as text, without its script; star, archive, the archive found at the top and not in a folder of the person's; search; staying fresh, then reading again after a dropped connection; accounts that fail saying why (a wrong password, none, Bridge not running) while the others show; the colours; names and IDs; the folders by mark and by name; the pins; composing an answer; sending through STARTTLS with Bcc; Gmail's and Bridge's refusals in words; the password stores, the Keychain in a temporary keychain of its own. `internal/core`: an account added only once its password signs in, listed, read, and removed whole; `:mail`; the plugin's parts; agents refused; the plugin off. `internal/config`: the settings checked, an account's table in and out whole.

## Not yet

- **Files**: an attachment can't be saved from Irie yet; Open in Gmail opens the message there. Proton's web mail has no link to one message.
- **New mail as it comes** (IMAP IDLE) and a notification for it: Mail reads the list each minute while it shows.
- **Drafts** saved to the server, a signature, and HTML messages written.
- **Gmail with "Sign in with Google"** (OAuth and XOAUTH2), which needs a Google Cloud project of the person's own; app passwords work until then.
- The terminal sidebar and the phone app show `:mail`'s words, not a list.
- **Mail for agents**, with the person's OK for each message: not until Irie can show an agent mail without the mail's words reaching it as orders.
