# Company copies: product requirements

Date: 2026-11-14, with R20 (the company's VPN) added the same day. Status: requirements, with what is built and what is next. An agent (Claude Code) wrote it, from `live` at `6410c2f`, after the owner asked to port Irie for use inside a company: a copy under the company's own name, with Irie's personal plugins left out and its core running on the company's servers. It draws on Irie's own code and docs only, so PROVENANCE.md gains no rows.

**In short.** A company may run Irie as a product of its own. Its copy renames Irie by changing one file, carries none of the plugins that are personal, fits its own name wherever the name shows, greets people with tips rather than Irie's cheer, and runs its core on a Linux server as well as on a Mac. The first five are built (DESIGN §2.10, §4.17); running on a server is the next work, in the order of §3.

| | Before | Now |
|---|---|---|
| Rename Irie | About 2,000 places, by hand | One file, `internal/brand/brand.go`, and `tools/brand.sh --swift` |
| The privacy, writing and markets plugins in a copy | Compiled in, with their commands, menus and settings | `Tags = "noprivacy,nowriting,nomarkets"` leaves every line of them out |
| The company's VPN | None: VPN control was Mullvad's alone, and left with the privacy plugin | The VPN plugin stays in a copy and drives the VPN the company approves, named in `VPNs`, such as Cisco Secure Client, GlobalProtect, FortiClient, Cloudflare WARP, Tailscale or NetBird, or any VPN through its own commands; `VPNServer` presets what it connects to |
| A long name | Pushed the title bar's counts out, wrapped buttons | Up to 30 characters fit: it shrinks, trails off or is left out of a tight button |
| The strip under the terminal | Irie's phrases of encouragement | `Bar = "tips"`: tips for those new to it |
| Two copies on one Mac | Shared folders, socket, tmux server and variables | Each has its own |

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
| R16 | A renamed copy's own test suite passes: the tests that expect Irie's names take them from the brand | Next | §3, 6 |
| R17 | The iPhone app takes its name from the same file | Next | §3, 7 |
| R18 | Irie ships neutral defaults that a copy keeps as they are: no owner's preference among `spawn.preferences` | Next | §3, 8; open question 2 |
| R19 | The icon comes from the brand: a copy draws its own mark without editing `tools/mkicon` | Later | |

## 3. Next, in order

1. **tmux in `irie up` (R11).** The check is `internal/ready`'s, now run only by `irie setup`, the first run and the `ready` op. On Ubuntu 20.04 (tmux 3.0a) and Debian 11 (3.1c), `up` fails without saying why.
2. **A service (R12).** A systemd user unit that runs `irie up` and keeps the core, `loginctl enable-linger` so systemd keeps `/run/user/UID` after the last logout, and a socket override for the CLI: a core started by systemd and a shell over ssh may disagree on `XDG_RUNTIME_DIR`.
3. **Containers (R13).** `sysload` and `localmodel` read `/proc/meminfo`, `/proc/loadavg` and the CPU count; read cgroup v2's `memory.max` and `cpu.max` first.
4. **Who may call (R14).** `core.sock` is mode 0600 with no token: any process of the user, agents included, may call every op. The caller is already known (`LOCAL_PEERPID`, `SO_PEERCRED`) and checked for the VPN's and Raphael's ops; extend that to `config`, `spawn`, `send`, `close` and `keys` where the caller is an agent, or give the clients a token. The command VPN (R20) makes this more pressing: its `vpn.status`, `vpn.connect` and `vpn.disconnect` are programs the core runs, and today an agent can set them through the `config` op, though it could run the same programs from its own shell. The check has to tell an agent from a setup session such as `:setup vpn`, which also runs in a pane of its own.
5. **Quiet on a server (R15).** The focus guard refuses `open` and `osascript` in a Mac's words on Linux too; the trod bot reads a missing `ioreg` as the person away and has no `caffeinate`.
6. **Tests (R16).** Renamed to "Acme Agent Workbench Pro" and built without the plugins, the copy printed no "Irie" anywhere, but about 40 tests in 11 of 17 packages failed: they expect `~/.config/irie`, the `irie/` branch prefix, "Outside Irie" and the like, and `internal/hook`'s set `IRIE_SESSION` by hand and wait out their time.
7. **The iPhone app (R17)** on `brand.go`, as the Mac app's `Brand.swift` is.
8. **Defaults (R18).** `config.Preferences()` holds the owner's own wish that texts read at a glance on a phone; it belongs in the owner's config.toml, and Irie's default empty.

## 4. Open questions

1. **The arch agents in a copy.** Mikael's text goes into every session, and his door weighs risky calls with Claude Code's small model: real protection a company may want to keep. Their names and charges are Irie's own. Should a copy rename and reword them through the brand, or turn them off by their switches?
2. **Irie's default preferences.** Empty, with the owner's in their own config (R18)?
3. **The phone gateway in a company.** It is off by default and refuses clients outside private networks; should a copy be able to leave it out with a tag, as it does the plugins?
4. **More than one person on a server.** One core serves one user, and a team runs one core each. Is that enough, or does a company need a shared core?

## 5. Checks

- `internal/brand`: the names' form and length, `tools/brand.sh` agrees with the package, `Brand.swift` is current, `Exe` for long and non-Latin names, the build tags are Irie's own, and no Go or Swift string names the product itself.
- `internal/plugins`: every package builds with `-tags novpn,noprivacy,nowriting,nomarkets,nomail`, and that `irie` links none of `internal/vpn`, `anon`, `browser`, `dms`, `htmltext`, `mcp` and `mail`, nor the mail plugin's IMAP and MIME libraries; a copy's own tags, `noprivacy,nowriting,nomarkets,nomail`, build `irie` with `internal/vpn` and none of the others. A copy that wants Mail drops `nomail` from `Tags`. `tools/check.sh` vets every package with both sets.
- `internal/vpn` and `internal/config`: the VPNs a copy names are VPNs Irie knows, and the config refuses a `vpn.provider` the copy doesn't offer.
- `internal/config`: `ui.bar`.
- By hand, before a release of a copy: build a renamed copy with its tags, run `help`, `config print`, `plugins`, `skills`, `ctl` and `setup --check` in an empty home, and look for Irie's name and the plugins' words; draw the Mac app with `mac/snapshot.sh` and look at the title bar and the welcome.
