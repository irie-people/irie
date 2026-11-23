# Company copies: product requirements

Date: 2026-11-14, with R20 (the company's VPN) added the same day, and R16, R21 and R22 from a test copy, Pesto, on 2026-11-22. Status: requirements, with what is built and what is next. An agent (Claude Code) wrote it, from `live` at `6410c2f`, after the owner asked to port Irie for use inside a company: a copy under the company's own name, with Irie's personal plugins left out and its core running on the company's servers. It draws on Irie's own code and docs only, so PROVENANCE.md gains no rows.

**In short.** A company may run Irie as a product of its own. Its copy renames Irie by changing one file, carries none of the plugins that are personal, fits its own name wherever the name shows, greets people with tips rather than Irie's cheer, and runs its core on a Linux server as well as on a Mac. The first five are built (DESIGN §2.10, §4.17); running on a server is the next work, in the order of §3.

| | Before | Now |
|---|---|---|
| Rename Irie | About 2,000 places, by hand | One file, `internal/brand/brand.go`, and `tools/brand.sh --swift` |
| The privacy, writing, markets, mail, Terms in short, browser extension, Yoink, founding documents and Jammin plugins in a copy | Compiled in, with their commands, menus and settings | `Tags = "noprivacy,nowriting,nomarkets,nomail,noterms,noextension,noyoink,nofounding,nojammin"` leaves every line of them out |
| The company's VPN | None: VPN control was Mullvad's alone, and left with the privacy plugin | The VPN plugin stays in a copy and drives the VPN the company approves, named in `VPNs`, such as Cisco Secure Client, GlobalProtect, FortiClient, Cloudflare WARP, Tailscale or NetBird, or any VPN through its own commands; `VPNServer` presets what it connects to |
| Chat in a copy | None | The chat plugin stays in a copy, so its people's Slack, and its Discord for a community, sit beside their sessions, agents writing drafts they send; `nochat` leaves it out |
| Bitwarden in a copy | Held, inside the privacy plugin | The Bitwarden plugin stays in a copy, so a company on Bitwarden fills its people's sign-ins in their panes too; `nobitwarden` leaves it out |
| A long name | Pushed the title bar's counts out, wrapped buttons | Up to 30 characters fit: it shrinks, trails off or is left out of a tight button |
| The strip under the terminal | Irie's phrases of encouragement | `Bar = "tips"`: tips for those new to it |
| Two copies on one Mac | Shared folders, socket, tmux server and variables | Each has its own; a few parts of a core still reach the other's agents (R21) |
| Testing a copy | A rename by hand, and about 40 tests that fail under another name | `tools/rebrand.sh` renames a clone; every test passes under another name, and CI runs them all as Pesto |

## 1. Who and why

- **An engineer at a company** builds the company's copy from Irie's source, as a fork or a wrapper, under the company's name. Irie's licence, Apache-2.0, allows it and grants no right to Irie's name or mark (its §6), so the copy renames. Its changed files say they were changed (§4b).
- **People at the company** use the copy as they would Irie: on their Macs, and on Linux servers for agents that run unattended.
- **Irie's owner** keeps Irie and its plugins as they are, and takes back from a company only changes that are general, never a company's own code or name.

## 2. Requirements

| # | Requirement | Status | Where |
|---|---|---|---|
| R1 | A copy renames Irie by changing one file: what people read (`Name`), what computers look up (`Slug`), the environment variables (`EnvPrefix`), the Mac app's identifier, the guide's and source's addresses, the welcome's tagline | Built | DESIGN §2.10 |
| R2 | Every surface takes the name from there: messages, help, both sidebars, the Mac app's windows, menus, Info.plist and executable, the phone app, the skills and arch agents' words, the tips, the build and install scripts | Built | §2.10 |
| R3 | Two copies run side by side on one computer: no folder, socket, tmux server or variable of one is the other's | Built | §2.10 |
| R4 | The name never creeps back: a string that names the product itself, in Go or in the Mac app, fails a test | Built | `internal/brand` |
| R5 | A name of up to 30 characters fits wherever it shows; the name is letters, digits, spaces, hyphens and full stops, so it is safe in scripts, tmux.conf, XML and HTML | Built | §2.10 |
| R6 | The app's process name stays findable whatever the name (`Exe`, 15 characters at most), so installs and the core still find the app | Built | §2.10 |
| R7 | A copy leaves a plugin out of the binary, not only switched off: no code, commands, menus, words or skills of it | Built | DESIGN §4.17 |
| R8 | The plugins a copy leaves out are named in the same one file (`Tags`), and the install and release scripts build accordingly | Built | §2.10 |
| R9 | A config.toml that still sets a left-out plugin's keys loads, and they do nothing; `config print` leaves them out | Built | §4.17 |
| R10 | The strip under the terminal shows tips for those new to it instead of words of encouragement, by the brand's default or `ui.bar` | Built | §2.10 |
| R20 | A copy drives the VPN its company approves, and no other: the VPN is a plugin of its own that a copy keeps without the privacy plugin; the brand file names the VPNs it offers (`VPNs`), the first the default, and what they connect to (`VPNServer`); the config, `:vpn use` and `irie setup vpn` refuse any other; a copy that names its VPNs shows the VPN from the start; a VPN Irie doesn't know by name works through commands the company names | Built | DESIGN §4.18 |
| R11 | `irie up` refuses a tmux older than 3.2 and says so, as `irie setup` does, rather than failing without a reason | Next | §3, 1 |
| R12 | The core runs as a service: a systemd user unit, lingering so it outlives a logout, and a socket every client of that user finds, with an override for the CLI | Next | §3, 2 |
| R13 | In a container, memory and CPU come from its cgroup limits, not the host's totals, for local models and placement | Next | §3, 3 |
| R14 | Only the person, not any process of theirs, changes the config or starts and steers sessions: a caller check or a token on those ops of the socket | Next | §3, 4 |
| R15 | Parts that are only for a Mac in front of a person stay quiet on a server: Gabriel's focus guard checks the OS, and the trod bot doesn't read a missing `ioreg` as the person being away | Next | §3, 5 |
| R16 | A renamed copy's own test suite passes: the tests that expect Irie's names take them from the brand, and a copy's test cores stay off the machine | Built | §5 |
| R17 | The iPhone app takes its name from the same file | Next | §3, 7 |
| R18 | Irie ships neutral defaults that a copy keeps as they are: no owner's preference among `spawn.preferences` | Next | §3, 8; open question 2 |
| R19 | The icon comes from the brand: a copy draws its own mark without editing `tools/mkicon` | Later | |
| R21 | Two copies side by side leave each other's agents alone: no part of one core slows, lists as its own, moves, stops or speaks for the other's sessions, and their servers keep apart | Next | §3, 6 |
| R22 | The Mac app takes the strip's default, cheer or tips, from the brand until the core answers, and its snapshots draw a copy's own plugins and VPN | Later | |
| R23 | A copy names its arch agents after their work: their names (Mikael, Gabriel, Raphael, Raguel and the short names Raffi and Ragu) and the pronoun their texts use come from the brand, in their texts, messages, the council and the Mac app; their keys stay Irie's. `tools/rebrand.sh` gives Guard, Priority, Medic and Planner, spoken of as it | Built | DESIGN §2.10 |
| R24 | A published copy carries none of the plugins it leaves out: their Go packages and files, the skills they embed and the Mac app's views for them are gone from its source, not only left out of its builds | Next | `tools/prune` |

## 3. Next, in order

1. **tmux in `irie up` (R11).** The check is `internal/ready`'s, now run only by `irie setup`, the first run and the `ready` op. On Ubuntu 20.04 (tmux 3.0a) and Debian 11 (3.1c), `up` fails without saying why.
2. **A service (R12).** A systemd user unit that runs `irie up` and keeps the core, `loginctl enable-linger` so systemd keeps `/run/user/UID` after the last logout, and a socket override for the CLI: a core started by systemd and a shell over ssh may disagree on `XDG_RUNTIME_DIR`.
3. **Containers (R13).** `sysload` and `localmodel` read `/proc/meminfo`, `/proc/loadavg` and the CPU count; read cgroup v2's `memory.max` and `cpu.max` first.
4. **Who may call (R14).** `core.sock` is mode 0600 with no token: any process of the user, agents included, may call every op. The caller is already known (`LOCAL_PEERPID`, `SO_PEERCRED`) and checked for the VPN's and Raphael's ops; extend that to `config`, `spawn`, `send`, `close` and `keys` where the caller is an agent, or give the clients a token. The command VPN (R20) makes this more pressing: its `vpn.status`, `vpn.connect` and `vpn.disconnect` are programs the core runs, and today an agent can set them through the `config` op, though it could run the same programs from its own shell. The check has to tell an agent from a setup session such as `:setup vpn`, which also runs in a pane of its own.
5. **Quiet on a server (R15).** The focus guard refuses `open` and `osascript` in a Mac's words on Linux too; the trod bot reads a missing `ioreg` as the person away and has no `caffeinate`. Pesto on the Asahi (Fedora, 2026-11-22) showed the plugins' words giving a Mac's advice on Linux: Bitwarden's "`brew install bitwarden-cli`" and "The Mac app unlocks Bitwarden", Chat's "Tokens stay in the Keychain", Obsidian's "a Vault card in the Mac app". Each should say what that system does, or the part should say it needs a Mac. A copy has no stop command either: stopping one is `tmux -L SLUG kill-server`.
6. **Side by side (R21).** Pesto, a copy installed beside Irie on the owner's Mac to test it (2026-11-22), showed where one core still reaches the other's sessions. Gabriel takes any busy process outside its own sessions for an app outside it, the other copy's agents and core included, and slows them (`internal/core/gabriel.go`); a copy beside another sets `arch.gabriel.outside = false` meanwhile. The list of sessions outside shows the other copy's agents, and a Move ends one there (`outside.go` drops only its own). Raphael offers to stop the other's busy processes. An agent of one copy counts as the person to the other's caller checks (`caller.go`). The lid's `pmset` is the whole Mac's. Both copies default `local.port` to 11435 and `local.llama_port` to 11436, so one's local models chat on the other's server; Pesto moves to 11445 and 11446. A copy started from a terminal of the other's session carries its `IRIE_SESSION`, `IRIE_SOCKET` and `IRIE_BIN` into its own agents (`host.go`, `up.go`). Each part should know every copy's sessions, by their hosts or by a mark each copy's host leaves on its tree, and a copy's default ports should differ by its slug.
7. **The iPhone app (R17)** on `brand.go`, as the Mac app's `Brand.swift` is.
8. **Defaults (R18).** `config.Preferences()` holds the owner's own wish that texts read at a glance on a phone; it belongs in the owner's config.toml, and Irie's default empty.

## 4. Open questions

1. **The arch agents in a copy.** Settled on 2026-11-23: the owner asked for a copy's arch agents to be renamed through the brand, to be more work related (R23). Their charges keep Irie's words, with the copy's names and pronoun in them.
2. **Irie's default preferences.** Empty, with the owner's in their own config (R18)?
3. **The phone gateway in a company.** It is off by default and refuses clients outside private networks; should a copy be able to leave it out with a tag, as it does the plugins?
4. **More than one person on a server.** One core serves one user, and a team runs one core each. Is that enough, or does a company need a shared core?

## 5. Checks

- `internal/brand`: the names' form and length, `tools/brand.sh` agrees with the package, `Brand.swift` is current, `Exe` for long and non-Latin names, the build tags are Irie's own, and no Go or Swift string names the product itself.
- `internal/plugins`: every package builds with `-tags novpn,noprivacy,nowriting,nomarkets,nomail,nobitwarden,noterms,noextension,noyoink,nochat,noobsidian,nofounding,nojammin`, and that `irie` links none of `internal/vpn`, `anon`, `browser`, `dms`, `htmltext`, `mcp`, `mail`, `extension`, `terms`, `yoink`, `chat`, `obsidian`, `founding` and `jammin`, nor the mail plugin's IMAP and MIME libraries; a copy's own tags, `noprivacy,nowriting,nomarkets,nomail,noterms,noextension,noyoink,nofounding,nojammin`, build `irie` with `internal/vpn`, `internal/chat` and `internal/obsidian` and none of the others. A copy that wants Mail drops `nomail` from `Tags`, and one that wants Terms in short, the browser extension, Yoink or Jammin `noterms`, `noextension`, `noyoink` or `nojammin`; one that wants no vault of notes adds `noobsidian`. `tools/check.sh` vets every package with both sets.
- `internal/vpn` and `internal/config`: the VPNs a copy names are VPNs Irie knows, and the config refuses a `vpn.provider` the copy doesn't offer.
- `internal/config`: `ui.bar`.
- `internal/brand`: `brand.Text` renames Irie, the command as a word, folders, `IRIE_` variables and the guide's address, and keeps ids, keys, tmux options, the source folders and others' links (`TestText`); each text names the command where the rewrite can tell (`TestTextsTellTheCommand`).
- `cmd/irie`: the help lines up for a command of any length (`TestHelpLinesUpForAnyName`); `internal/plugins`: a company's copy offers no card of a plugin it leaves out.
- Every test passes under another name (R16): `internal/testutil` switches a test core's Gabriel, Raphael, Raguel, lid and Keychain off by the brand's variables and writes its config to the brand's folder, and each test that expects a name takes it from `internal/brand`. CI's `copy` job renames the checkout to Pesto with `tools/rebrand.sh` and runs `tools/check.sh` on Linux. One package as a copy, without copying the tree: `GOFLAGS=-overlay=O.json go test ./internal/PKG`, where O.json swaps in a renamed `brand.go`. Before 2026-11-22 a renamed copy's test cores ran with Gabriel, the Keychain and the rest on, and without their config, so they started the real `claude`: never run an older copy's tests.
- On a Linux server: Pesto ran on the Asahi (Fedora, arm64, 2026-11-22) beside its Irie. It was built with `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags "$(tools/brand.sh --tags)"` and started on its own tmux server. A session with no model (a `generic` profile running `sh`) and a Claude Code session on the Asahi's own Dory, through the Agent SDK, both ran there, with `PESTO_` variables, `pesto hook`, `/pesto:` skills and states from working to done. Check a copy's slug against its servers' packages: Fedora's passt installs a `/usr/bin/pesto`, which setup now names as another program of that name.
- A copy installed beside Irie: `tools/rebrand.sh` in a clone of its own (it refuses a checkout that pushes to Irie's repository, and so does `tools/live.sh` for a renamed one), then `tools/live.sh` there. Pesto, the owner's, keeps its own script and notes in its clone (`PESTO.md`).
- By hand, before a release of a copy: build a renamed copy with its tags, run `help`, `config print`, `plugins`, `skills`, `ctl` and `setup --check` in an empty home, and look for Irie's name and the plugins' words; draw the Mac app with `mac/snapshot.sh` and look at the title bar and the welcome.
