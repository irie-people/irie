# Proposal: Irie for writers, with Variorum

Date: 2026-11-09. Status: the first slice is built; the rest is below. The owner asked for an Irie plugin for writers built on Variorum, with its maker credited. There is no issue tracker yet, so this file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote this file and the change it describes. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

## Credit

[Variorum](https://stargazing.tech/variorum) is a writing app for the Mac by Lillian Duarte. The ideas this extension stands on are hers:
- Every paragraph has a lasting identity, so it can hold **takes**: several versions of itself, side by side.
- **Version control made for writing.** Her letter says she "took some of the best parts of git, like commits and traceable lineage, and used block identity to sidestep the line-based diffs that make git itself unsuitable" for writing, so every take keeps its own history.
- **Lenses** are saved readings made of chosen takes, and **Compare** works like a pull request for prose.
- Its **door for AI** has levels: an AI can be allowed to suggest beside a writer's words without being able to change one of them.

Irie adds none of these ideas. It runs agents that use her door well, several at once. The skill follows her guide to the door, [Variorum and AI](https://stargazing.tech/variorum/mcp), and when its text and Variorum's differ, Variorum is right.

## Goal

A writer types `:write` and what they want, and an agent helps with their writing in Variorum without writing over it. Editor's notes arrive as comments, rewrites and translations as takes beside their paragraphs. The agent can also save lenses, keep books tidy and put research in notes of its own. Several agents can work at once, one job each, and the writer chooses what stays.

## What it is

Irie's second plugin, **Writing** (DESIGN §4.17), with one part, Writing in Variorum. `[plugins] writing` is on by default, and `irie plugins off writing` turns it off; then the skill leaves every session, and `:write` says why. The plugin shows in Settings › Plugins and the Plugins menu, in `irie plugins` and in `:plugins`, which say whether Variorum is in an Applications folder and where it comes from if not. In the sense of [extensions.md](extensions.md) §1.1, the part is:

| Part | What | Where |
|---|---|---|
| The plugin | Its switch, and a check for Variorum in `/Applications` or `~/Applications` | `internal/plugins/writing.go` |
| A skill | `/irie:variorum`: Variorum's words, nine rules, how to start, eight jobs, the setup in `reference/setup.md`, and the credit | `internal/skills/bundled/variorum/` |
| Words | `:write [TASK]` starts a writing session: Claude Code on `+claude`, in `<data>/guides/variorum`, running `/irie:variorum TASK`. The task names the session | `core/read.go`, `internal/core/guide.go` |
| Controls | **File › Write with Variorum…** in the Mac app sends `:write` | `mac/Sources/Irie/MenuBar.swift` |
| Helpers | `irie variorum door`, the door as each writing session's own MCP server (below). Variorum keeps the record: a receipt for each change, ⌥⌘Z, the Activity tab | `internal/variorum`, `cmd/irie` |

The skill runs only when the writer starts it (`disable-model-invocation: true`), so it costs nothing in other sessions.

**The writer's words stay theirs.** Variorum enforces most of this already: at Suggest, an AI "cannot change a word you wrote". The skill holds the same line even when a writer grants Edit: every rewrite is a new take, every remark is a comment, and the agent never chooses a take for the writer. It asks for the lowest level and the shortest time a job needs, and says exactly what to share. Its jobs follow the patterns from real use that Variorum's guide lists.

**Why Irie hands out the door.** The Claude Code setup Variorum copies adds its door for every folder (`claude mcp add -s user`). Then every Claude Code session on the Mac connects to Variorum when it starts, while Variorum is open, and keeps its connection for as long as it runs, whether it ever writes or not. Variorum serves at most 16 connections, and Irie often runs more sessions than that, so a writing session started later would find no room and get no Variorum tools. So the writer gives that setup to Irie instead of running it: Irie keeps its key and adds Variorum's door to its writing sessions alone, as an MCP server for that session only (`--mcp-config`), the way it adds its browser. No other session connects, and the key never goes into a chat. All writing sessions also run in `<data>/guides/variorum`, so Claude Code asks only once whether to trust the folder.

**Why `+claude`.** The session reads what the writer shares, and that goes to the model's company, as Variorum's guide says plainly. The skill says so once, before it reads anything.

## Variorum's source

Variorum has been open source since 12 November 2026: [github.com/lduarte-io/Variorum](https://github.com/lduarte-io/Variorum), under the AGPL-3.0. Three things in it shape Irie's part:
- **Closed contribution.** Its maker takes no pull requests, patches or code from anyone, and closes pull requests by themselves. Bugs and ideas go to her as issues, security problems by email (its SECURITY.md). So Irie's fixes for Variorum are issues that describe the problem, never code.
- **Her name and logo** aren't licensed for derived products. Irie names Variorum only to say what it works with and whose work it is.
- **No code of hers in Irie.** Irie is Apache-2.0; linking her code into Irie.app would bring the AGPL with it, and her code needs Swift 6.2 and macOS 26 besides. Irie reaches Variorum only as any other program does, through its door, a standard MCP server over stdio. The skill was checked against the door's code on 14 November 2026.

## The Variorum tab

The owner asked on 14 November 2026 for "an embedded type plugin in Irie with Variorum": Variorum's writing in Irie itself, beside the sessions that work on it. So the Writing plugin gets a second part, **the Variorum tab**, in the Mac app's right sidebar. It shows the note a writing session works on, as Variorum holds it at that moment:

- The note's title, then each paragraph as it reads (its chosen take), with the other takes under it, each with its label, and its comments. A take or comment that came since the tab last read the note has a mark.
- **Show in Variorum** beside a paragraph opens that paragraph's takes in Variorum, where the writer chooses; beside the title, it opens the note. Variorum offers what Irie asks it to show, or moves its view at once if the writer set it to; it never raises a window.
- **Which note:** the one the shown session's agent last named in a call to Variorum's door, which the core sees in its hooks. A menu lists the notes shared with Irie, to pick another.
- **When it reads:** when the tab opens or shows another session, when the writer presses its refresh button, and two seconds after the session's agent finishes a call to Variorum's door, at most once in that time. Never on a timer. Variorum logs every call to its door, reads too, and keeps only the last 500 lines across all its clients, so a tab that read on a timer would push the agents' changes out of the writer's Activity tab.
- **It never writes.** Irie's connection needs only Read, and the tab has nothing that changes the note. Choosing a take, and everything else, stays the writer's, in Variorum.

**Setting it up.** Irie keeps two of Variorum's setups, one for each client: **Claude Code**'s, for the writing sessions, and **Any MCP client**'s, for the tab. The tab connects as a client of its own, `irie`, never as Claude Code, so Variorum's Activity tab tells the two apart and the writer shares with each on its own.
1. In Variorum: Settings ▸ MCP ▸ Connect a client, choose **Claude Code** and copy. Each copy holds a fresh key.
2. In Irie: the tab's **Connect** button reads the setup from the clipboard and keeps it. It never shows it, and it never goes into a chat. It tells the two setups apart by their shape: a `claude mcp add` line is Claude Code's, a JSON `mcpServers` stanza is Any MCP client's. `pbpaste | irie variorum connect` does the same in a terminal.
3. Back in Variorum, choose **Any MCP client**, copy, and press **Connect** again.
4. Variorum asks the writer, in its Settings ▸ MCP, to let each client in: `claude-code` when the first writing session starts, `irie` when the tab first reads. Then the writer shares what each needs: the job's note, book or wing with `claude-code` at the level the job needs (the skill says which), and the same with `irie` at Read.

The writer doesn't run either setup in Terminal. Variorum lets `claude-code` in with one key at a time: a newer copy takes the name over, and it then turns the older key away. So Irie's copy is the writer's one Claude Code setup. A setup for every folder left from before would fill the 16 places, and a new one run in Terminal would shut Irie's sessions out. The plugin's check notices one in Claude Code's config and says that `claude mcp remove -s user variorum` removes it.

`irie variorum forget` deletes both setups. Irie keeps them in `<data>/variorum/door.json`, readable by the user alone, and never writes a key anywhere else: not in config.toml, a session's files, the journal, a log or an answer to a client.

**How the door reaches a session.** A session started by `:write`, while Irie keeps Claude Code's setup, gets the MCP server `variorum` for that session only: `irie variorum door`, which starts the door's own program, `variorum-mcp` inside Variorum.app, with the key in its environment, and then stays out of the way, every byte going straight through. So the agent's tools are Variorum's own (`mcp__variorum__read_note` and the rest), and its config names only `irie variorum door`, never the key. A session sees Variorum's door when it starts, so the skill's setup ends with `:reopen`.

**How a read works.** For each read the core starts the same program with the tab's key, says `initialize` as `irie` (the brand's slug), makes one call and ends; a call waits at most ten seconds. Irie runs only a `variorum-mcp` inside a Variorum.app (bundle `io.lduarte.Variorum`) in an Applications folder, so a pasted setup can't name another program, and it checks that again before each start. When Variorum is closed or locked, hasn't let `irie` in yet, or hasn't shared the note, the tab says so in the door's own words.

| Piece | Where |
|---|---|
| The setups (both shapes), the checks on the door's program, one call over stdio | `internal/variorum` |
| The `variorum` op (`status`, `connect`, `forget`, `notes`, `read`, `show`) and each session's note from its hooks; the door for `:write` sessions | `internal/core/variorum.go`, `internal/core/guide.go` |
| `irie variorum [status\|connect\|forget\|door]` | `cmd/irie` |
| The plugin's second part, ready once Variorum is installed and both setups are kept | `internal/plugins/writing.go` |
| The tab | `mac/Sources/Irie/VariorumTab.swift` and `RightSidebar.swift` |

`go build -tags nowriting` leaves all of it out, with the rest of the plugin.

## Not yet

- **Trying it here.** Variorum needs macOS 26 on Apple silicon, and the owner's Mac runs macOS 14. The skill now matches the door's code, and the tab is tested against a stand-in for the door built from that code, but neither has been tried against the app.
- **Words that stay on the Mac.** A writer may want a local profile, so that what they share never leaves the machine. `:write` runs on `+claude` for now.
- **A count on the cards.** The tab shows each take that is waiting to be chosen; the session's card could carry a count of them too.
