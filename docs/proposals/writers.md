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
| Helpers | None. Variorum's door does the work and keeps its own record: a receipt for each change, ⌥⌘Z, the Activity tab | — |

The skill runs only when the writer starts it (`disable-model-invocation: true`), so it costs nothing in other sessions.

**The writer's words stay theirs.** Variorum enforces most of this already: at Suggest, an AI "cannot change a word you wrote". The skill holds the same line even when a writer grants Edit: every rewrite is a new take, every remark is a comment, and the agent never chooses a take for the writer. It asks for the lowest level and the shortest time a job needs, and says exactly what to share. Its jobs follow the patterns from real use that Variorum's guide lists.

**Why one folder.** Claude Code keeps an MCP server added with `claude mcp add` for the folder it was added in, unless the setup says `--scope user`. All of Irie's writing sessions run in `<data>/guides/variorum`. So the setup has the writer run Variorum's copied setup from that folder, in Terminal, which works with either scope and keeps the setup's key away from the model. With one folder, Claude Code also asks only once whether to trust it.

**Why `+claude`.** The session reads what the writer shares, and that goes to the model's company, as Variorum's guide says plainly. The skill says so once, before it reads anything.

## Not yet

- **Trying it here.** Variorum needs macOS 26 on Apple silicon, and the owner's Mac runs macOS 14. The skill follows Variorum's published guide, but none of it has been tried against the app.
- **Variorum's source.** On 7 November 2026 its maker announced that Variorum will be open source. Once it is, check the skill against the door's own code, and send any fixes for Variorum to her repository as pull requests.
- **Words that stay on the Mac.** A writer may want a local profile, so that what they share never leaves the machine. `:write` runs on `+claude` for now.
- **Irie's view of it.** Variorum's receipts and Activity tab already say what each session did. Irie's cards could show the same, such as a count of takes waiting to be chosen.
