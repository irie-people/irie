# Security

## Reporting a vulnerability

Report it privately, with GitHub's private vulnerability reporting. Don't open an issue, a pull request or a discussion about it.

1. Open the [Security tab](https://github.com/irie-people/irie/security) of irie-people/irie.
2. Choose **Report a vulnerability**.
3. Say what you found, how to reproduce it, and what it lets someone do.

The report goes to the project's maintainers through its account on GitHub, and to nobody else. There is no security email address.

If the button isn't there, open an issue that asks for a private way to report, and keep every detail out of it.

## What happens next

A maintainer looks at the report and works on a fix with you in a private security advisory. When the fix is out, the advisory is published, with credit to you unless you'd rather not be named. There are no promised response times.

## Supported versions

Irie releases from its main branch as changes land (release-please, tags `vX.Y.Z`). Fixes go into the next release. Only the latest release is supported.

## Scope

In scope is Irie's own code: the `irie` program, its core and the socket it serves, the pages and gateway it serves, the hook it adds to agents, Irie.app, and the website in `site/`.

The core talks over a socket file that only your user can open, in a folder only your user can read. It also serves one page on the loopback address, and the phone gateway listens on the local network once you turn it on: see [Network](#network).

The agents Irie starts, such as Claude Code, and tmux are their own projects. Report problems in them to those projects.

## For security and IT teams

Irie runs AI coding agents (Claude Code by default) for one person on their own machine. Each agent runs in a tmux pane, and the sidebar and Irie.app show them. It is open source under the Apache License 2.0. This section says what Irie runs, touches and listens on, so you can recognise it rather than guess. Everything is as it is by default unless it says otherwise.

### Processes

| As `ps` shows it | What it is |
|---|---|
| `tmux -L irie -f …/tmux.conf` | Irie's own tmux server. The agents run in its panes. `irie-teams` and `irie-dock` are two smaller ones. |
| `irie core --supervise`, and its child `irie core` | The core, which keeps the state and answers the socket. |
| `irie ui --supervise` | The sidebar in the terminal. |
| `irie host --spec …/sessions/<id>/spec.json` | One per agent. It starts the agent in a process group of its own. |
| `claude --model … --settings …/sessions/<id>/claude-settings.json --session-id <uuid>` | The agent, under its host. |
| `irie hook`, `irie statusline` | Started by Claude Code for each hook event, and for its status line. |
| `irie runtime -- <program>` | A background program Irie keeps while it's used: a local model server (Ollama, llama-server), an `ssh` tunnel, Tor or SearXNG. |
| `irie remote serve --supervise` | The phone gateway, only after `irie remote on`. |
| `Irie`, `Irie --away-helper` | Irie.app, and its helper that keeps windows an agent opens off the desktop the person is on. |

### Telling an agent's commands apart

An agent's commands are ordinary shell commands, and a tool that watches command lines sees them as the person's own. Three things tell them apart:

- **Ancestry.** `tmux -L irie` → `irie host --spec …` → `claude …` → the shell that runs the command.
- **Environment.**
  - Every agent and every command it runs carries `IRIE_SESSION` (the session's id), `IRIE_SOCKET` and `IRIE_BIN`.
  - An agent's teammate carries `IRIE_TEAM_OF`, and a helper carries `IRIE_HELPER`.
  - An anonymous research session carries `IRIE_ANON` and proxy settings.
  - A session on a local model carries `ANTHROPIC_BASE_URL` on 127.0.0.1.
- **The journal.** `~/.local/state/irie/journal/` records each agent's tool calls by session and time, for 30 days. With `journal.tool_input = "none"` it leaves out commands' text.

An agent runs with the person's own rights, and with whatever they gave the program it runs in, such as Full Disk Access granted to Irie.app or a terminal.

### Network

| What listens | Where | When | Protection |
|---|---|---|---|
| The Terms in short page, served by the core | 127.0.0.1:7393, HTTP | On by default, while the Terms plugin is on | Answers only to a Host of 127.0.0.1, localhost or [::1], and to its own origin. No token. |
| Local model servers | 127.0.0.1:11435 (Ollama), :11436 (llama-server), :11437 and up (`ssh` tunnels to another machine's) | While a session uses a local model | Loopback only. |
| The browser connector (WebDriver BiDi) | A random loopback port | After `irie setup browser`, while an agent browses | Refuses a request that carries an Origin. No token. |
| Anonymous research | 127.0.0.1 and ::1 from port 9250 (a proxy per session), Tor from 9150, SearXNG on 8888 | After `irie setup anon` | Loopback only. No token. |
| Spotify sign-in | A random 127.0.0.1 port, for 10 minutes | While linking Spotify | Checks the Host and the OAuth state. |
| The phone gateway | Every address of the machine unless `remote.listen` names one: HTTPS on 7387, a plain-HTTP setup page on 7386, and an HTTPS port for each pane it shares | After `irie remote on` | TLS 1.2 or later with Irie's own name-constrained certificate authority. A Bearer token, issued through a six-digit pairing code. A 403 to any address off the local network. The pane ports carry no Irie token. macOS's firewall asks once whether `irie` may accept connections. |

Irie makes no telemetry and no update checks. It connects out only for these:

- **Installing or updating:** its releases on GitHub (irie-people/irie).
- **`irie setup`, each part after a yes:** the Homebrew and Claude Code installers, package managers, and a model download through Ollama.
- **Features the person turns on:**
  - another machine running Irie, with its certificate authority pinned, and `ssh` for `git fetch`;
  - web push services for the phone (Apple, Google, Mozilla, Microsoft);
  - OSV.dev for `irie lock --check`;
  - the services a plugin names, such as Spotify, Discord, Slack, a mail server, Tor or Mullvad's relays.

The agents connect to their own services (Claude Code to Anthropic's API) and to whatever their tasks need.

### On macOS

- **Privacy prompts.** Irie asks for none of its own, except Automation for Music or Spotify, when its music plugin controls them.
- **`irie setup lid`.** This step keeps the Mac awake with its lid closed while agents work.
  - It adds `/private/etc/sudoers.d/irie-lid` (checked with `visudo`), which lets the user run only `pmset -a disablesleep 1` and `pmset -a disablesleep 0` as root, without a password.
  - It installs one LaunchAgent, `io.github.irie-hq.irie.lid`, which runs `irie lid check` at login and every 2 minutes.
- **Keychain.** Items go in the login keychain: "Irie vault" is made automatically, to seal the sidebar's and zen's history. "Irie jammin", "Irie mail" and "Irie chat" are made only when those accounts are added.
- **Gabriel**, on by default, puts heavy apps outside Irie into macOS's background state (`setpriority` with `PRIO_DARWIN_BG`) while the person works, so the app in front gets the machine first. An endpoint tool may see one process lower another's priority.
- **Window moves.** Irie.app loads a private macOS framework (SkyLight) to move windows between desktops, so windows an agent opens never land where the person works.
- **Writes outside Irie's folders:**
  - `~/.local/bin/irie` (a link) and `~/Applications/Irie.app`;
  - `~/.claude/themes/irie.json`;
  - branches named `irie/…` in the person's repositories;
  - the browser extension's native messaging host files, once it is set up;
  - a PATH line in the shell's startup file, after a yes.

### Files

- `~/.config/irie`: the config.
- `~/.local/state/irie`: sessions, the journal and conversations' history (30 days), logs, and the phone gateway's certificate authority.
- `~/.local/share/irie`: the installed build, worktrees and models.
- `$TMPDIR/irie-<uid>`: the core's socket.

Folders are 0700 and files 0600, owned by the user.

### Verifying a release

Releases are built by GitHub Actions from their tags. Each holds `SHA256SUMS`, and the installer checks every download against it. To check one by hand, take both from the same release:

```sh
grep ' irie-darwin-arm64.tar.gz$' SHA256SUMS | shasum -a 256 -c
```

Release builds are signed ad hoc and not notarized, so Gatekeeper and endpoint tools see them as from an unidentified developer. A build someone makes for themselves can be signed with their own identity (`git config irie.sign`).
