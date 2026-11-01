# Proposal: a dead man's switch, Bitwarden and Mullvad in Irie

Date: 2026-11-01. Status: proposed, for the owner to decide. There is no issue tracker yet, so this file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote it, from `live` at `bddef62`. It is the design only and builds no code. It builds on the skills proposal ([skills.md](skills.md), M12, decided but not built). Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

## Goal

An agent helped the owner set up a dead man's switch for their family in one session: a letter in the password manager, emergency access for the people who would need it, a check-in every two weeks that warns the owner first and the family a fortnight later, and a one-tap "I'm OK" on the phone. The owner wants every Irie user to be able to do the same, easily, and asked for three more extensions around the tools that made it work.

| Extension | What it lets an Irie user do |
|---|---|
| **Dead man's switch** (§2) | Set up their own switch in one guided session: any family size, any password manager, any phone |
| **Bitwarden for agents** (§3) | Let an agent use or save a login, with the user's approval each time, without the secret ever reaching the model, a transcript or a log |
| **Mullvad VPN control** (§4) | See and change the VPN from Irie's task bar, the Mac app and the phone apps |
| **Mullvad Browser for agents** (§5) | Have agents browse in Mullvad Browser instead of Chrome, where that is worth what it costs |

**In short.**
- An extension is up to four parts: an Irie skill, helper commands in the `irie` binary, words in the core's language, and controls in the clients (§1.1). Each extension has one switch.
- Irie's own skills are compiled into `irie` and reach Claude Code through M12's session-only plugin (`--plugin-dir`), so they need no download (§1.2).
- One rule runs through all four, learned the hard way: **an agent prepares, the user clicks** (§1.3). Anything that grants someone access, writes to another person, sets a standing rule, signs in or spends money is the user's own click, and Irie makes it one step.
- A secret never passes through a model. An agent names a login; Irie fetches it, hands it to the one command that needs it and scrubs it from what comes back (§3).
- **Build the dead man's switch first** (§6). It is what the owner wants everyone to have, it needs the least new code, and its delivery is M12's first slice.

---

## 1. What an extension is

### 1.1 The parts

| Part | Where it lives | What it is for | Dead man's switch | Bitwarden | Mullvad VPN | Mullvad Browser |
|---|---|---|---|---|---|---|
| A skill | `internal/skills/builtin/<name>/`, embedded in `irie` | What the agent should do, step by step, and the vendor pages it needs | `dead-mans-switch` | `vault` | — | a recipe, later |
| Helper commands | `irie <name> …` in `cmd/irie`, logic in `internal/<name>` | Work that must be exact, or must keep a secret from the agent: Go, tested with `go test` | `irie dms` | `irie vault` | `irie vpn` | — |
| Words | The core's language (CORE.md §3) | One reading in every client: the terminal sidebar, the Mac app, the phone app and `irie ctl input` | `:setup switch` | approvals | `:vpn …` | — |
| Controls | The Mac app, the phone app | Menus, a Settings tab, a status mark | a menu item | Allow and Deny, with Touch ID | a status mark, a menu | — |

A helper is a subcommand of `irie` rather than a script in the skill's folder, for three reasons: it is tested with the rest of Irie, it needs no Python or Node on the user's machine, and the skill's folder stays Markdown only, which keeps the M12 review flag for scripts quiet. A skill calls its helpers through `$IRIE_BIN`, which every Irie session already has (DESIGN §3.3).

### 1.2 Irie's own skills and M12

M12 fetches third-party skills, each pinned to a commit and checked by hash. Irie's own skills are its own Apache-2.0 text, so none of that applies to them:
- They are compiled into `irie` with `go:embed`, so a release carries them and the first run needs no network.
- They go into the same plugin folder as M12's, `<data>/skills/plugins/<digest>/`, under the plugin name `irie`, so `/irie:dead-mans-switch` runs one. The folder rules of M12 §1.1 hold: real copies, read-only files, one folder per set named by its digest, hashed again before each launch.
- A guide is started by its skill's full name, `/irie:<name>`, so Irie's own copy is the one that runs, whatever skill of the same name the user has. M12 §1.2's rules for skills an agent may pick itself come with M12; until then a guide's session is the only one with the plugin (decision 3), and a user's own plugin named `irie` would be replaced in that one session, as Claude Code replaces a same-named plugin for a `--plugin-dir` one.
- `[skills] enabled` and `disabled` (M12 §5.1) switch them like any other skill.

A skill that only runs when the user types it, with `disable-model-invocation: true` in its frontmatter, puts nothing in the agent's context: Claude Code's skills docs give it "Description not in context, full skill loads when invoked". Irie's guides are all of that kind. They cost no tokens in a session that doesn't use them, and the agent never starts one on its own.

M12's first slice landed in `live` beside this work (`aa041dd`, from another session): every Claude Code session gets Irie's skills in that plugin, once a probe has seen the CLI load a plugin passed with `--plugin-dir` (`claude --plugin-dir <probe> plugin list --json`), which also catches managed settings that refuse the flag and a plugin of the user's own named `irie`. The dead man's switch skill is one of those skills. It is user-only, so other sessions don't pay for it; `:setup switch` checks first that it will reach the guide's session, and says why when it won't.

### 1.3 An agent prepares, the user clicks

When the agent set up the owner's switch, Claude Code's auto mode refused every change it tried to make to an account on the owner's behalf: adding emergency contacts and organization invites ("Permission Grant"), Healthchecks email integrations ("Account & Standing-Rule Changes", even after the owner said go ahead), and an iMessage nobody asked for. What worked was the agent preparing everything and the owner making the one click, or running the one `!` line the agent wrote. That was the classifier doing its job, and the extensions are built around it.

| What | Who | How Irie makes it easy |
|---|---|---|
| Read vendor docs, ask the user questions, work out the steps, write files in a folder the user chose | The agent | — |
| Create or change things inside the user's own account with a key the user made for it, such as Healthchecks checks | The agent, then it shows what it made | The helper does it exactly, and prints what changed |
| Grant a person access: emergency access, an organization invite, a legacy contact | The user | The exact page, who to add, which level, which wait, what to tick |
| Send to another person, or set a standing rule: an email integration, an alert, a message, an auto-reply | The user | The same |
| Sign in, make an API key, pay, anything behind two-factor | The user | One `!` line, or one page to open |
| Check what the user did | The agent | Reads it back: a read-only key, a CLI's status, a helper's `status` |

Irie never argues its way past Claude Code's own checks. It doesn't answer a permission hook with `allow` (DESIGN §3.3), doesn't send `classifierContext` to talk the classifier round, and its skills declare no `allowed-tools`.

### 1.4 Switches

| Extension | Switch | Default |
|---|---|---|
| Dead man's switch | `[skills] disabled = ["dead-mans-switch"]`; every skill: `[skills] enabled = false`; one profile: `profiles.<name>.skills = false` | On. It does nothing until the user asks for it |
| Bitwarden for agents | `[vault] on` | Off |
| Mullvad VPN control | `[vpn] on` | Off. Settings offers it when the `mullvad` CLI is found |
| Mullvad Browser for agents | A profile the user adds (§5.4) | None |

---

## 2. Dead man's switch

### 2.1 What the user ends up with

| Piece | What it is | Where it lives |
|---|---|---|
| **A letter** | "READ ME FIRST": what to do first, who to call, where the papers are, which accounts exist and how to reach them, and what the user wants. No passwords: those stay in the password manager | A secure note in the password manager, or on paper |
| **Emergency access** | The people who would need it can get into the password manager after a wait the user picks, and the user can turn them down during the wait | The password manager (§2.5) |
| **A check-in** | Every 14 days the user taps "I'm OK". Two days late, the user gets a reminder. Sixteen days after that, the people the user chose get an email that says what has happened and what to do | Two checks at Healthchecks.io (§2.3) |
| **A tap** | One tap on the phone that checks in, and a calendar reminder every other Sunday | The phone (§2.4) |
| **Backups** | Google's Inactive Account Manager and Apple's Legacy Contact, for those who have the accounts | Google, Apple (§2.6) |
| **A record** | `switch.md`: what was set up, where, when, and how to turn each part off. No secrets | The folder the user picks |
| **A yearly review** | A calendar event once a year: the people, the letter and the tap, checked again | The calendar |

The rhythm is the user's: 14, 2 and 16 days are what the guide suggests.

### 2.2 The session

The user types `:setup switch` in any task bar, or picks **File › Set Up a Dead Man's Switch…** in the Mac app. The core starts a Claude Code session called "Dead Man's Switch" on `+claude`, in a folder of its own, `<data>/guides/dead-mans-switch`, with `/irie:dead-mans-switch` as its task. The folder is the same every time, so Claude Code asks only once whether to trust it, and the reply says to choose Yes; the guide's files go in the folder the user picks. `+claude` is the most careful profile, and this session decides who can reach everything a person owns. The skill then takes the user through it, a step at a time, and they can stop after any step and reopen the session later:

1. **Ask.** Who should be told, and for each of them an email address and whether they get full access or only a view; any number of people. Which password manager, and its plan. Which phone. Whether they have Google and Apple accounts. The rhythm. Where to keep the files, by default `~/Documents/Dead Man's Switch`.
2. **Plan.** The agent writes `switch.md`, every step marked *agent* or *you*, and shows it.
3. **The letter.** The agent writes the outline (§2.7) and goes through it with the user, who fills it in. The user pastes it into a secure note called READ ME FIRST. The agent then offers to delete the plain copy.
4. **Emergency access.** For the user's password manager, the exact steps, one person at a time (§2.5).
5. **The check-in.** Two checks, made by the agent with a key the user gave Irie, and the emails the user adds (§2.3).
6. **The tap.** The phone's one-tap check-in, and the calendar file (§2.4).
7. **Backups.** Google and Apple, step by step (§2.6).
8. **Test.** A check-in from the phone, read back by the agent; a test email to the user only.
9. **Tell the people.** A short message the user may send each person in their own words. The agent never sends it.
10. **Done.** `switch.md` marked up to date, with how to turn each part off.

What the user tells the agent goes to its model's company, like any conversation. The guide says so at the start, and keeps secrets out: the letter says where things are, never what the passwords are.

### 2.3 The check-in, at Healthchecks.io

Healthchecks.io is a monitoring service whose code is open source (BSD-3-Clause) and can be run at home. A check expects a ping every *period*; when the period and then its *grace time* pass without one, the check goes down and emails the integrations assigned to it, once. Its free plan, Hobbyist, has 20 checks, and email alerts. A check notifies all its integrations at once, and the two groups must hear at different times, so the switch uses two checks:

| Check | Period | Grace | Emails | When |
|---|---|---|---|---|
| **Reminder**: "Dead man's switch: tap I'm OK" | 14 days | 2 days | the user only | 16 days after the last check-in |
| **Alarm**: in the user's words, such as "Alice has not checked in for 30 days: please read this" | 14 days | 16 days | each person the user chose | 30 days after the last check-in |

The alarm's email has the subject `DOWN | <the check's name>`, so the name says what to do, and its body carries the check's description word for word, which holds the steps: open READ ME FIRST, request emergency access, call someone. The email also shows the time and the IP address of the last check-in, so the people see roughly where it came from (a Mullvad address, with the VPN on). When check-ins start again, the people get an `UP` email too, which tells them a false alarm is over.

| Step | Who | How |
|---|---|---|
| Sign up, and make a project | The user | healthchecks.io sends a sign-in link by email. A second link voids the first, so the guide asks for one and waits |
| Make an API key | The user | On the project's Settings page, a read-write key, not the read-only one. "By default, a project on Healthchecks.io doesn't have an API key", and no API makes one |
| Give it to Irie | The user | Copy it, then type `! "$IRIE_BIN" dms key --paste`: Irie reads the clipboard, keeps the key 0600 in its state folder, empties the clipboard, and prints its last four characters. A `!` line can't prompt, and anything typed on it lands in the conversation; the clipboard keeps the key out of both |
| Make the two checks | The agent | `irie dms checks`, through the Management API with `unique: ["name"]`, so running it again changes nothing. It sends no `channels`, so the new checks email nobody yet |
| Add the email integrations | The user | Integrations › Email, one per person, each named after them, and the user's own address named Me, unless the list has it already. Each new address gets an email "Verify email address on Healthchecks.io" and receives nothing until its owner clicks it; the user's own sign-in address needs no confirmation. The API can't make integrations at all (`/api/v3/channels/` is read-only), and they are standing rules that write to other people (§1.3) |
| Tick who hears what | The user | Checks the API made start with no integrations, so the user ticks Me on both. Healthchecks attaches an integration added in the web page to every check that already exists, so the user unticks each person on the reminder, or the people would hear at 16 days instead of 30 |
| Check | The agent | `irie dms status`: both checks, their state, and the integrations each one emails, by name; a reminder that emails more than one integration is flagged |
| Arm it | The user | One check-in. A new check waits for its first ping, and until then it never goes down |

**Ping URLs are keys.** Whoever has one can check in for the user, and adding `/fail` to it sets the alarm off at once. So the helpers keep them 0600 in Irie's state folder and never print them, and they go only into the phone's shortcut. The checks' email addresses can only check in, unless the user sets keywords for a failure, and they go into the contact card. The Healthchecks password, if the user sets one, goes in the password manager, so the people can pause the checks after a false alarm.

**Own server.** `irie dms key --api https://hc.example.com/api/v3/` points every helper at the user's own Healthchecks.

### 2.4 The tap

| Phone | The one-tap check-in | Made by |
|---|---|---|
| **iPhone** | A shortcut, "I'm OK", that pings both checks, on the Home Screen | `irie dms shortcut` writes it in Shortcuts' older file format, two URL actions each followed by Get Contents of URL, and signs it with `shortcuts sign --mode people-who-know-me` on the Mac. The user AirDrops or messages it to the phone and taps Add Shortcut |
| **Any phone** | An email "I'm OK" to a contact of that name, which holds both checks' addresses, `<uuid>@hc-ping.com` | `irie dms contact` writes the contact card (vCard, RFC 6350), which any phone imports. Healthchecks takes a check-in by email, and its own code pings every address one email goes to; that the hosted service does the same is not confirmed, so the guide checks the first one with `irie dms status`. The email becomes the last check-in, which the alarm attaches, so it stays short |
| **Any computer** | `irie dms ping` | — |

Signing matters: Apple's WWDC21 talk on Shortcuts for macOS says shortcut files "are notarized by Apple … signed with the identity of the person who sent them", and `shortcuts sign` "also supports signing a shortcut in the old format", the format of the one permissively licensed description of it (python-shortcuts, MIT). Apple says of *people who know me*: "Only people who have you in their contacts will be able to run your shortcut. Your contact info will be included in the shortcut file." That suits a shortcut for one's own phone, which has its owner's card; the phone also needs Settings › Shortcuts › Private Sharing on. `--mode anyone` sends Apple a copy, and from a background session such as Irie's tmux it failed, asking for iCloud; the guide doesn't use it. If the shortcut won't import, the contact works on an iPhone too.

**The calendar.** `irie dms calendar` writes one .ics file with two events: "Check in: tap I'm OK", every other Sunday at 10:00 (`RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=SU`), and a yearly review with a checklist (`RRULE:FREQ=YEARLY`), each with an alarm at its start. Times are floating, so the reminder stays at ten wherever the phone is. The file holds no ping URL. iCloud Drive on a busy Mac can be stuck for a day, so the guide sends the files over AirDrop or Messages.

### 2.5 Any password manager

The guide asks which one the user has, and follows that vendor's own page, read when the step comes:

| Password manager | What the people get | Plan | The user's steps |
|---|---|---|---|
| **Bitwarden** | Emergency access: *View* (read every item of the user's own vault) or *Takeover* (set a new master password, which removes two-step login), after a wait of 1, 2, 7, 14, 30 or 90 days; the user can approve or reject a request during the wait | Premium, or a member of a paid organization such as Families. Each contact needs a Bitwarden account, free is enough, on the same server | Settings › Emergency access › Add emergency contact, for each person: email, level, wait. The invitation lasts five days; once a person accepts, the user confirms them by their fingerprint phrase. Neither the CLI nor a public API can do any of it |
| **Bitwarden Families** | A shared vault for up to six | Families | Admin Console › Members › Invite member; the Public API is only for Teams and Enterprise |
| **1Password** | No emergency access. The Emergency Kit (a PDF with the sign-in address, email and Secret Key, and a space for the password), and on a Families plan the organizer can recover a member's account | Any | Print the Emergency Kit, write the password in, and keep it where the letter says |
| **Proton Pass** | Emergency access to the whole Proton Account, up to five contacts, after 1 to 30 days | A paid Proton plan; both need Proton Mail addresses | mail.proton.me › Settings › Recovery › Emergency access |
| **KeePassXC, KeePass** | Nothing built in. KeePass 2 prints an Emergency Sheet | — | The database file, the key file and the password, written down, kept where the letter says |
| **Apple Passwords** | Not through a Legacy Contact: Apple says it can't reach "Information stored in the account holder's iCloud Keychain" | — | A shared password group (iOS 17, macOS 14) with the people, for what they need |
| **Google Password Manager** | Not confirmed that the Inactive Account Manager hands over passwords | — | Share the passwords that matter with the family group |
| **LastPass** | Emergency access, at once or after a wait | Premium or Families | The vendor's page |
| **Dashlane** | No emergency contacts any more | — | Its suggestion: an encrypted export, and its password in the letter's place |
| **None** | — | — | Bitwarden's free plan holds the letter as a secure note; emergency access needs Premium |

### 2.6 Backups from Google and Apple

| Service | What it does | The user's steps |
|---|---|---|
| **Google Inactive Account Manager** | After a waiting period the user picks (Google's own posts say up to 18 months), it can send chosen data to up to 10 people, who confirm with a phone number; it can also set a Gmail auto-reply and delete the account | myaccount.google.com/inactive |
| **Apple Legacy Contact** | With the access key the user gives them and a death certificate, a Legacy Contact gets iCloud Photos, Notes, Mail, Messages in iCloud, iCloud Drive, backups and more, for three years; not the Keychain, purchases or payment details. More than one can be added | iPhone: Settings › [name] › Sign-In & Security › Legacy Contact. Mac: System Settings › [name] › Sign-In & Security. Needs iOS 15.2 or macOS 12.1, and two-factor authentication |

### 2.7 What ships

| Part | Contents |
|---|---|
| The skill, `internal/skills/builtin/dead-mans-switch/` | `SKILL.md`: the rules of §1.3 for this guide and the steps of §2.2, `disable-model-invocation: true`, no grants. `reference/`: `letter.md` (the outline of READ ME FIRST), `healthchecks.md`, `phone.md`, `password-managers.md`, `backups.md`, `helpers.md`. Markdown only, no personal data, and every vendor page by its URL |
| `irie dms` | `key [--paste] [--api URL]`; `checks --alarm-name … [--alarm-desc-file …] [--every 14] [--remind 2] [--alarm 16]`; `status`; `ping`; `pause`, `resume`; `calendar --out FILE`; `shortcut --out FILE [--sign]`; `contact --out FILE` |
| The core | `:setup switch`, a tool of its own that starts a Claude Code session on `+claude` with `/irie:dead-mans-switch` and Irie's plugin folder, again when it is reopened (§1.2) |
| The Mac app | File › Set Up a Dead Man's Switch…, which sends `:setup switch` |
| The phone apps | Nothing new: their task bars send `:setup switch` too, and the session is theirs to follow and answer |

### 2.8 What it needs from the user

An Irie with Claude Code whose `--help` lists `--plugin-dir`; about an hour; a password manager (any plan, though emergency access is often a paid feature); a free Healthchecks.io account or their own server; each person's email address, and their help confirming it; for the iPhone shortcut, a Mac signed in to iCloud; and the clicks of §2.3, §2.5 and §2.6.

### 2.9 Switching it off

- **In Irie.** `[skills] disabled = ["dead-mans-switch"]` takes the skill out, and `:setup switch` then says why; `[skills] enabled = false` takes out every skill of Irie's, and `profiles.claude.skills = false` those of the guide's profile.
- **The switch itself.** `irie dms pause` stops both checks from emailing anyone until the next check-in. Deleting the checks in Healthchecks ends it; then the emergency contacts in the password manager, the shortcut, the calendar events, and Google's and Apple's settings. `switch.md` lists each, with its page.

### 2.10 Tests

- `internal/skills`: every bundled skill grants no tools, holds no injected `` !`command` `` and no personal data, and is Markdown only; the guide is user-only (`content_test.go`). The plugin folder's own tests came with M12's first slice.
- `internal/core`: `:setup switch` starts the session with the skill's command on `+claude`; the fake agent, which lists its plugins as Claude Code does when `IRIE_FAKE_PLUGINS` is set, finds the skill in Irie's plugin folder, and a reopen passes the folder again; every way the guide can be off says why: skills off, the skill off, a profile without skills, no `+claude`, and a CLI that can't load the plugin.
- `internal/dms`: against a fake of the Management API, two checks with the right periods and graces, made again by name, never with `channels`; the key never in an error, even when the server echoes it; a read-only key refused in words; pause and ping; the status report; the calendar against RFC 5545 (CRLF, folding at 75 octets outside characters and escapes, TEXT escapes); the shortcut's property list through `plutil -lint` where there is one.
- **Manual check.** In the live Irie: `:setup switch`, the guide's first questions, `irie dms key --paste` with a test project's key, `irie dms checks`, `irie dms status`, a shortcut signed and opened on an iPhone, and the calendar file opened. Then pause the checks, and delete the test project.

---

## 3. Bitwarden for agents

### 3.1 What it lets a user do

An agent sometimes needs a login: an API token to deploy, the password of a test account, a key to call a service. Today the user pastes it into the chat, where it stays in the transcript, or leaves it in an environment variable that every agent can read. With this extension the agent names the login and Irie does the rest:
- **Use a login.** `irie vault run "example.com deploy token" --env TOKEN=password -- ./deploy.sh` asks the user, then runs `./deploy.sh` with the secret in `TOKEN`. The agent sees the command's output with the secret replaced by `[secret]`.
- **Save a login.** `irie vault save "example.com test account" --uri https://example.com --username alice --generate 24` asks the user, then saves a new login with a password Irie generated, which the agent never sees. A script that made an account can hand its password over on standard input instead.
- **Find a login.** `irie vault find example.com` lists the names, URIs and usernames of matching logins, and never a password, a note, a card or a TOTP code.

### 3.2 Why not just give an agent `bw`, or Bitwarden's MCP server

- **`bw` itself.** Once unlocked, `bw get password <id>` prints the password to whoever runs it. An agent that runs it puts the password in its transcript, and the session key in `BW_SESSION` decrypts the whole vault until `bw lock`: Bitwarden's CLI docs say "Session keys are valid until invalidated using bw lock or bw logout".
- **`bw serve`.** A REST server on `localhost:8087` that answers any local request while the vault is unlocked; it only blocks requests with an `Origin` header. Every process on the machine, every agent included, could read the vault through it.
- **Bitwarden's MCP server** (bitwarden/mcp-server, GPL-3.0). It is careful about the master password: its `unlock` tool opens a native password dialog, and "The password is never sent through the MCP protocol or seen by the LLM". Its `get` and `list` tools return vault items to the model, though, and its own README warns that it gives an assistant the ability to "Expose credentials and vault contents through AI responses". That is the wrong side of the brief's line: a secret must never land in a transcript.

So the agent never holds the key, and never sees a secret. Irie does both jobs for it.

### 3.3 The broker

| Piece | Rule |
|---|---|
| Unlock | The user runs `irie vault unlock` in their own terminal (one `!` line the agent writes), or clicks Unlock in the Mac app. Irie asks for the master password without echo and passes it to `bw unlock --raw` through `--passwordenv` with a one-time variable name, never in argv. The session key lives in the core's memory only: never on disk, never in an agent's environment |
| Lock | After `vault.idle` (default 15 minutes) without a request, when the Mac sleeps or its screen locks, on `irie vault lock`, and when the core stops. Locking runs `bw lock`, which ends the key |
| A request | `irie vault run`, `save` and `find` are clients of the core's socket. Each sends what it wants (the login's words, the fields, the command and its arguments); the core finds the item with `bw list items --search` and asks the user (§3.4). Only then does it fetch the secret with `bw get` |
| Running | The core starts the command itself, in the agent's working directory, with the secret in the named environment variables only. Never in argv, where `ps` would show it |
| What comes back | The command's output streams back to `irie vault run`, and so to the agent, with each secret replaced by `[secret]`, also in its base64, URL-encoded and JSON-escaped forms. The exit status passes through |
| Saving | `bw get template item` and `item.login`, filled in, then `bw encode` and `bw create item`. A generated password comes from `bw generate` and goes straight to `bw create`; the agent gets the new item's name and id |
| Which `bw` | The user's own login by default. `vault.appdata` points `BITWARDENCLI_APPDATA_DIR` at another one, so a careful user can give Irie a separate Bitwarden account, or an organization collection, holding only the logins agents may use |

Every Claude Code session also gets one more check: Irie's PostToolUse hook, which already sees every tool call, replaces any secret it handed out this session with `[secret]` through `updatedToolOutput`, which "Replaces the tool's output with the provided value before it is sent to Claude". That catches a secret that a command printed in a form the broker didn't know. The docs don't say what the transcript keeps, so this was checked with Claude Code 2.1.316: a hook that replaced the output of `echo` with `[secret]` left `[secret]` in the transcript, both in the tool result and in its `toolUseResult`, and the original output nowhere. The docs do warn that "Telemetry such as OpenTelemetry tool spans and analytics events also captures the original output before the hook runs". That is why the hook is only the second layer: the broker scrubs first, so the original output a tool returns already has `[secret]` in it.

### 3.4 Approval each time

A request turns the agent's card red, like a question, and says what it wants: *"api-fix wants the password of "example.com deploy token" (https://example.com) for `./deploy.sh`"*. The user answers **Allow once** or **Deny**, and Deny is the default after `vault.ask_timeout` (2 minutes). Nothing is remembered, so the next request asks again.

The answer must come from the user, not from an agent: agents run as the same user and can reach Irie's socket with `irie ctl`. So:
- **The Mac app** asks for Touch ID, or the Mac's password, before it sends Allow. It signs the answer with a key it keeps in the Secure Enclave, whose public half it gave the core at first run; the core accepts no Allow without that signature. No agent can make one.
- **The terminal sidebar** has no such key. It takes Allow only when typed in full, and only when `vault.terminal_approve` is on, off by default.
- **The phone apps** show the request and can Deny it, but can't Allow it in the first version.
- **The core** refuses an answer from a connection whose peer process is inside an agent's process tree (`LOCAL_PEERPID` on macOS, `SO_PEERCRED` on Linux). That stops the easy way round, not a determined one.

### 3.5 Where a secret never goes

| Place | How it stays out |
|---|---|
| The model's context and the transcript | The agent's tool calls carry names and variable names only; output is scrubbed twice (§3.3) |
| Irie's journal and logs | They record the request (the session, the item's name and id, the fields' names, the command) and the answer, never a value |
| Hook payloads | A Bash command holds `$TOKEN`, not its value |
| Process listings | Never in argv. An environment is readable by processes of the same user while the command runs; that limit is in §3.7 |
| Files | Nothing written. The master password goes through a one-time environment variable; the session key stays in memory |
| The approval prompt, the phone and push notifications | The item's name and URI, never a value |

### 3.6 What it needs from the user, and switching it off

- The Bitwarden CLI (`brew install bitwarden-cli`, or `npm install -g @bitwarden/cli`), signed in once with `bw login` in their own terminal: email, master password and two-factor are theirs to type.
- `[vault] on = true`, and an unlock when a request comes.
- Off: `[vault] on = false` (the default). `irie vault lock` ends the session key at once. Without the extension, Irie also takes `BW_SESSION` out of every agent's environment, so a key the user exported in their own shell never reaches an agent.

**What an agent may do alone:** `find`, which shows no secrets, and only while the vault is unlocked for Irie. Everything else waits for the user.

### 3.7 Limits

On one Mac account every agent runs as the user. A process of the same user can read another's environment while it runs, and can read `bw`'s encrypted data file. Irie's broker keeps secrets out of transcripts, logs and argv, and stops an agent that uses Irie's own path, including one a prompt injection steers. It doesn't stop a determined program running as the user. For that, run agents as another user, or in Claude Code's sandbox, which can keep a command away from `bw`'s files and from the network.

---

## 4. Mullvad VPN control

### 4.1 What it lets a user do

Mullvad's own app and its `mullvad` CLI drive the VPN. With this extension Irie shows the VPN's state where the user already looks, and changes it from any Irie client, the phone included, so a phone can turn the Mac's VPN on or move it to another country.

| Typed (any client) | What happens |
|---|---|
| `:vpn` | One line: *VPN on: Gothenburg, Sweden (se-got-wg-001), lockdown on, 41 days left* |
| `:vpn on`, `:vpn off` | `mullvad connect --wait`, `mullvad disconnect --wait` |
| `:vpn se`, `:vpn se got`, `:vpn se-got-wg-001` | `mullvad relay set location …`, then `mullvad reconnect --wait` if it was on |
| `:vpn lockdown on`, `:vpn lockdown off` | `mullvad lockdown-mode set on\|off`, which blocks the network while the VPN is off |
| `:vpn time` | The account's time left |

The status strip and the Mac app show a small mark: green with the city when connected, amber while connecting, red when the tunnel is in error, plain when off. The Mac app adds a VPN menu (Connect, Disconnect, Location, Lockdown) and an Extensions tab in Settings. The phone apps show the mark, and their task bars take the same words.

### 4.2 How the core reads and drives it

| Item | Rule |
|---|---|
| Finding it | `vpn.mullvad`, else `mullvad` on the PATH, else `/usr/local/bin/mullvad` (macOS) and `/usr/bin/mullvad` (Linux) |
| State | `mullvad status listen --json`, kept running by the core while `[vpn] on`, prints the state, then a JSON line for each event of the daemon: each change of the tunnel's state, and also new settings, a new relay list (a long line), device and version events, which Irie skips, as it does the warning line printed when the device is logged out. The core restarts it with backoff if it ends |
| Account time | `mullvad account get`, once an hour. Only its `Expires at:` line is read |
| Lockdown | `mullvad lockdown-mode get`, at start and after each change |
| Changes | Each word runs its command with a 30-second limit, and the reply is the command's own message when it fails |
| Version | `mullvad --version`. The words and JSON above are those of mullvad-cli 2026.3; an older CLI without `--json` gets a warning and no mark |

The JSON is the daemon's `TunnelState`: `{"state": "connected", "details": {"endpoint": …, "location": {…}, "feature_indicators": […]}}`, with `disconnected`, `connecting`, `disconnecting` and `error` as the other states.

### 4.3 Privacy

- **The account number is the account's only secret**: Mullvad has no username or password besides it. `mullvad account get` prints it on its first line. Irie reads only the line that starts `Expires at:` and drops the rest unread; nothing of the output is logged.
- **The real address.** While the VPN is off, the `location` in the status is the machine's own public address and its place on the map. Irie keeps only the state and, when connected, the relay's city, country and host name. It never shows, journals or logs an address or coordinates.
- **Nothing goes anywhere.** Irie talks to the local daemon through its CLI, and to no Mullvad server.

### 4.4 Who does what, and switching it off

The user changes the VPN, from any of their clients. Agents don't: the `vpn` op refuses a change from a connection inside an agent's process tree, the same check as §3.4, and Irie's skills tell agents never to touch the network's settings. An agent could still run `mullvad` itself; lockdown mode is the protection that holds whatever runs.

It needs only the Mullvad app installed and logged in. `[vpn] on = false`, the default, stops the listener and hides every mark; the words then say the extension is off.

---

## 5. Mullvad Browser for agents

### 5.1 What it would give

Agents that browse, to research, to check a site they built or to fill a form, do it in Chromium today: Claude in Chrome works with Chrome, Edge and other Chromium browsers; Chrome DevTools MCP "officially supports Google Chrome and Chrome for Testing only"; Playwright brings its own browsers. Mullvad Browser is the Tor Project's Firefox ESR without the Tor network, made with Mullvad "for all its users to appear as one": private browsing always on, `privacy.resistFingerprinting` locked on, letterboxing, uBlock Origin and NoScript bundled as part of its fingerprinting defence, DNS over HTTPS, and telemetry compiled out. Its code is Firefox's, under MPL-2.0. With Mullvad VPN on, an agent browsing in it would leave less of a trail than one in Chrome.

### 5.2 Can agents drive it?

Yes. Mullvad Browser 16.0 (Firefox 153.6.0esr) keeps both of Firefox's remote protocols: Marionette, which geckodriver speaks, and WebDriver BiDi through the Remote Agent. CDP is gone from every Firefox since 141. Neither the Mullvad build's mozconfigs nor its 189 patches on Firefox touch them: none passes `--disable-webdriver`, and no patch changes `remote/` or `testing/geckodriver/`. The Tor Project runs Marionette tests of its own.

**Checked on this Mac.** Headless, on a fresh profile in a scratch folder, with `--remote-debugging-port` and no prefs at all, an 80-line Node client on Node's own WebSocket ran `session.new`, `browsingContext.getTree`, `navigate`, `script.evaluate` and `captureScreenshot`, then `session.end`; `--marionette` worked too. The server listened on 127.0.0.1 only, wrote `WebDriverBiDiServer.json` into the profile when it was ready, and kept no history or cookies on disk afterwards. Two things to know: a `data:` page can't be opened from the first tab, a privileged page, until the tab has gone to `about:blank`; and on this loaded machine every step was slow, from 0.5 to 54 seconds.

| Client | Works with Mullvad Browser | How |
|---|---|---|
| Mozilla's Firefox DevTools MCP (MIT or Apache-2.0) | Yes, by its docs; not tried here | `--firefox-path`, or `--connect-existing --marionette-port` to a browser already running with both `--marionette` and `--remote-debugging-port` |
| Puppeteer (Apache-2.0) | Yes, through BiDi | `executablePath`, or `connect` with `protocol: 'webDriverBiDi'` to a browser already running |
| Selenium or WebdriverIO with geckodriver | Yes | `moz:firefoxOptions.binary` |
| Playwright, Playwright MCP | No | "Playwright doesn't work with the branded version of Firefox since it relies on patches" |
| Claude in Chrome, Chrome DevTools MCP | No | Chromium only |

### 5.3 What it costs

| Cost | Why |
|---|---|
| Sites can tell | `navigator.webdriver` is `true` on every page while the browser is under remote control, as the WebDriver spec requires. A Mullvad Browser that says so is no longer one of the crowd it was built to hide in |
| Weaker defaults, unless set | On start the Remote Agent applies about 120 "recommended" prefs for testing: the popup blocker, tracking protection and extension updates off, among them. They are reset at a clean shutdown, and not when the browser is killed. `remote.prefs.recommended = false` in the profile keeps the browser's own |
| The bundled add-ons, only if Irie launches it | geckodriver, Puppeteer and Playwright write `extensions.installDistroAddons = false` into the profiles they make, which leaves out uBlock Origin and NoScript. A browser Irie starts on a fresh profile keeps them, and a client then connects to it |
| No logins | Private browsing is always on, and Mullvad's FAQ says staying signed in between sessions is "not possible". Good for privacy; useless for a task that needs the user's accounts |
| Anyone local can drive it | The port answers only on loopback and refuses requests with an `Origin` header, so web pages can't use it, but any process of the user's can, without a password, while it runs |
| One client at a time | Firefox accepts one, though a new session can start after the last ends |

### 5.4 What it would take, and the recommendation

For Irie to offer it, a session option such as a profile's `browser = "mullvad"` would have Irie:
1. make a fresh profile in the session's state folder, with `remote.prefs.recommended = false`;
2. hand Claude Code an MCP server through `--mcp-config`, per launch like `--settings`: a small `irie browser mcp` that starts Mullvad Browser on that profile with `--marionette` and `--remote-debugging-port` on free loopback ports, waits for `WebDriverBiDiServer.json`, then runs Mozilla's server, pinned to a version, with `--connect-existing`. As the agent's own child it freezes, thaws and ends with the session;
3. say on the card that the session browses in Mullvad Browser, and whether Mullvad VPN is on, since the browser alone hides no address.

**Recommendation:** not to build it yet. Write it up as a recipe in the docs instead: a user who wants it can run the steps above by hand with Mozilla's server. An automated Mullvad Browser gives up the thing that makes it Mullvad Browser, a fingerprint shared with everyone else, and it can't use the user's logins. What most users want from it, an agent's traffic away from their own address, Mullvad VPN gives every browser (§4). Build `irie browser mcp` when users ask for it.

---

## 6. Which to build first

| Order | Extension | Why here | What it builds |
|---|---|---|---|
| **1** | **Dead man's switch** | It is what the owner wants every user to have. It needs no new kind of trust in Irie: the agent prepares, the user clicks, and Irie keeps one key. Its delivery is M12's first slice, which Bitwarden's skill reuses | `internal/skills` and the plugin folder, `--plugin-dir` in the Claude adapter, `[skills] enabled` and `disabled`, the guide's skill, `internal/dms` and `irie dms`, `:setup switch`, File › Set Up a Dead Man's Switch… |
| 2 | Mullvad VPN control | Small and on its own, and useful every day. It reads one secret, the account number, only to drop it | `internal/vpn`, the `vpn` op and the `:vpn` words, the header's mark, the Mac app's menu and mark, the phone apps' mark |
| 3 | Bitwarden for agents | Irie's first approval of its own, with Touch ID and the Secure Enclave, and the largest security surface of the four. It deserves a review of its own design before code | `internal/vault`, `irie vault`, approvals in the core, the Mac app's key, the hook's scrub |
| 4 | Mullvad Browser for agents | A recipe and its docs, no code, until a maintained browser server takes a custom Firefox binary (§5) | Docs, and a profile in the examples |

**Recommendation:** the dead man's switch first, shipped live, then Mullvad VPN control in the same way. Bitwarden for agents after its approval design (§3.4) is agreed.

---

## 7. Where it fits

- **DESIGN §2.1, principle 4.** "Hooks, settings, env and skills go in per launch, through `--settings`, `--plugin-dir`, argv and env", as skills.md §7 already proposes.
- **DESIGN §2.6.** `internal/skills/` (Irie's own skills and the plugin folder), `internal/dms/` (the switch's helpers); later `internal/vpn/` and `internal/vault/`.
- **DESIGN §3.3.** `--plugin-dir` in the launch line of every Claude Code session whose CLI loads it, as M12's first slice has it; `:setup switch` refuses, with the probe's reason, where it doesn't.
- **DESIGN §4.2 and CORE.md §3.** `:setup switch` among the words; later `:vpn …`.
- **DESIGN §5.4.** `<data>/skills/plugins/<digest>/`, removed when no session uses it; `<state>/dms/healthchecks.key` and `switch.json`, both 0600.
- **DESIGN §5.5.** `[skills] enabled` and `disabled`; later `[vpn]` and `[vault]`.
- **DESIGN §8.** M12's first slice is in, and the dead man's switch is its second skill; M12's list, fetch and store follow. Each later extension is its own step, each with its tests and a manual check.
- **DESIGN §9, risks.** A guide that tells people how to give others access to their accounts: it reads each vendor's own page, and says when it can't see what the user sees. A secret broker on a machine where agents run as the user (§3.7). A VPN changed under agents at work (§4.4).
- **MAC.md.** File › Set Up a Dead Man's Switch…; later the VPN menu and an Extensions tab in Settings.
- **The site.** A page for the guide, from this proposal, once it ships.

---

## Open decisions

| # | Decision | Options | Recommendation |
|---|---|---|---|
| 1 | What to build first | a. Dead man's switch. b. Mullvad VPN control. c. Bitwarden for agents. d. Mullvad Browser | **a** (§6) |
| 2 | How far to go now | a. The dead man's switch, then Mullvad VPN control. b. The dead man's switch only. c. Those two and Bitwarden | **a**: Bitwarden waits for its approval design to be agreed |
| 3 | Which sessions get Irie's plugin | a. Only a guide's own session, until M12 checks for managed settings that refuse `--plugin-dir`. b. Every Claude Code session now | **a**: a machine whose managed settings set `disableSideloadFlags` would otherwise fail every spawn |
| 4 | The phone's tap | a. An iPhone shortcut, signed on the Mac, and the "I'm OK" contact for any other phone. b. The contact for every phone: one way, no signing, but two taps and an email in the alarm. c. The iPhone shortcut only | **a** |
| 5 | The check-in service | a. Healthchecks.io's hosted free plan, with `--api` for one's own Healthchecks. b. Self-hosted only | **a** |
| 6 | The guide's profile | a. `+claude`. b. `spawn.default_profile` | **a**: routing would read `/irie:dead-mans-switch` as a small task |
| 7 | The VPN extension's default | a. Off, offered in Settings when `mullvad` is found. b. On when found | **a** |
| 8 | Vault approvals from a phone | a. Deny only, at first. b. Allow, signed by the native iPhone app's own Secure Enclave key after Face ID | **a**, then **b** with the native app |
| 9 | Mullvad Browser | a. A recipe in the docs, for now (§5.4). b. `irie browser mcp` now | **a** |

**Decided 2026-11-01:**
- **Decision 1:** a, the dead man's switch first.
- **Decision 2:** a, then Mullvad VPN control; Bitwarden for agents waits for its approval design.
- **Decision 3:** a, only a guide's own session gets Irie's plugin. Superseded the same day: M12's first slice, built alongside, hands every Claude Code session the plugin once a probe has seen the CLI load it, which answers the risk behind the decision, so the guide's session gets it that way too.
- **Decision 4:** the user's choice: the guide offers the iPhone shortcut and the contact card, and each user takes either or both.
- **Decisions 5 to 9:** the recommendations stand.

**Built 2026-11-01:** the dead man's switch (§2) and Mullvad VPN control (§4). For the VPN: the `:vpn` words and the `vpn` op; the header's mark in the terminal sidebar, the Mac app (with File › VPN and a switch in Settings › General) and the phone web app's machine cards; and the core's check that refuses a change from a process whose parents include an agent's host, with the caller's process id read from the socket. The native iPhone app doesn't show the VPN yet.

## Checks for this proposal

- `mullvad`, `bw` and `claude` were read through their own `--help` only. Nothing read an account, a vault or the VPN's state, and nothing changed a setting.
- With Claude Code 2.1.316 in a scratch folder, on Haiku 5.5: a read-only plugin folder named `irie` ran its user-only skill from `/irie:<skill>` as the prompt; and a PostToolUse hook's `updatedToolOutput` replaced the tool's output in the transcript too (§3.3).
- Healthchecks: its API, ping, email and pricing docs, and its BSD-3-Clause code at `ab4b5c4` for what the docs leave out: integrations are read-only in the API, sign-in tokens, email verification, an integration added in the web page going to every check, the alert email's contents, and a check that waits for its first ping. A few requests went to hc-ping.com for UUIDs that are no check, to read its answers: `200 OK (not found)`, with `access-control-allow-origin: *`; none reached a check.
- Bitwarden, 1Password, Proton, KeePass, LastPass, Dashlane, Apple and Google: their own help pages, and Bitwarden's web app at `b793bdc` (GPL-3.0, read only) for the list of wait times. What a page didn't say is marked not confirmed.
- Mullvad Browser 16.0 on this Mac, headless on fresh profiles in a scratch folder: WebDriver BiDi and Marionette as in §5.2. Every browser started was ended by its own process id, and no profile of the user's was opened. Its sources at `a95cf21` on gitlab.torproject.org, through the GitLab API.
- `shortcuts --help` and `shortcuts sign --help`, macOS 14.1. No shortcut was signed for this proposal.
