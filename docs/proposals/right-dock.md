# Proposal: the right sidebar and its dock

Date: 2026-11-01. Status: being built, with the recommendations of §10 taken. Built: the right sidebar with its Dock, Helpers and Review tabs (§7 steps 1 and 2, and Review from step 4). Not yet: Changes, Plan, and the Settings sections of §5.7. On 2026-11-02 the owner chose the sidebar inline, after the terminal, rather than a peek over it (decision 2): the peek and the pin are gone, and the text below says so where it changed. On 2026-11-08 they asked for it to slide in as a drawer, which it now does before it settles after the terminal. It answers the owner's jot "deep research a dock right pane for irie". An agent (Claude Code) wrote it, from `live` at `831c1c2`. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

**In short.** The jot most likely means cmux's **Dock**. cmux has a right sidebar, toggled with ⌥⌘B, and its Dock tab holds controls you define: live terminals running commands such as a git view, a log or a dev server, and web pages such as a dashboard. They are listed per repo in a small JSON file. cmux turned the Dock on for everyone on 2026-10-05, and the owner has asked Irie to copy cmux before (§1).

Irie's version is a **right sidebar** with five tabs:
- **Dock:** your controls, as cmux has them. Irie reads cmux's file too, and runs each control in the folder of the session on show.
- **Changes**, **Helpers**, **Plan** and **Review:** Irie's own tabs about the session on show. Irie knows its sessions, so these need no setting up.

The sidebar opens and closes as a whole. It keeps its tab and width as you go from session to session, and it never opens or switches tab by itself.

The hard part is the terminal. The app's terminal is a client of the slot, and every agent's window has the slot's size (DESIGN §2.3). So a sidebar that narrows the terminal resizes all 24 agents at once, and each of them redraws. A narrower terminal also changes what Claude Code draws: its own diff panel, which opens inside the terminal from 144 columns, the owner's exact width, stops opening. And under Claude Code's older renderer a resize can copy lines into its history, an open bug (§3).

The research proposed a **peek**, sliding over the terminal's right edge so nothing is resized, and a **pin** to keep it beside the terminal (§3). The owner chose inline instead: "i dont want modal i want inline on right side of terminal (after term)". So the sidebar always sits after the terminal, and opening or closing it resizes every agent once, just as resizing the window does. Later they asked for it to "slide in as a drawer" (2026-11-08): it slides in from the window's right edge, over the terminal's edge, and the terminal makes room only once it is in, so the slide never adds a resize.

| | Today on `live` | Proposed |
|---|---|---|
| Commands you keep an eye on: git, a log, a dev server, a test watcher | A terminal of your own, outside Irie | Dock controls, each running in the shown session's worktree |
| A web page you check, such as the app an agent serves | A browser tab outside Irie | A Dock control showing it |
| A helper, while the terminal shows | A click flips to the conversation | It opens in the sidebar, over or beside the terminal |
| What a session changed | Its card says "Not through: 2 commits not in live" | Changes lists its files and commits; each file opens to its diff |
| Its plan | Only in its terminal (Ctrl+T), and only when it has Claude Code's task tools | Plan shows its checklist, or the plan it had approved |
| What is ready to review | Ready to review, above the mode switch | The same list, plus a Review tab that opens each session's changes in place |
| The terminal's width | Changes only with the window | Also when you open or close the sidebar, or drag its edge (once, on release) |
| tmux | One slot; every agent at its size | Unchanged; Dock terminals run on a server of their own |

---

## 1. What the request asks

**cmux's Dock** is the likeliest source of the words.
- **The sidebar.** cmux's right sidebar has five modes: files, find, sessions, feed and dock. ⌥⌘B shows and hides it, which is also the key VS Code uses for its secondary side bar.
- **The Dock.** In cmux's words, it is "cmux's right-sidebar terminal and browser control area". Terminal controls run commands in Ghostty-backed sections; browser controls embed web pages. cmux suggests it for "feeds, logs, queues, git status, dev servers, dashboards, or reference pages".
- **The file.** A control is listed in `.cmux/dock.json` in a repo, which a team commits, or in `~/.config/cmux/dock.json` for yourself. The repo's file wins, and cmux asks you to trust a repo's file before it starts its commands.
- **When.** cmux 0.65.0 turned the Dock on for every user on 2026-10-05, and lets you hide or reorder the right sidebar's tabs. That was four weeks before the jot. The owner asked Irie to copy cmux once before, for agent teams ([split-pane.md](split-pane.md)), and still keeps cmux knowledge at hand.

**The general meaning** fits as well. A dock is a side area that holds tool windows: Zed's right dock, VS Code's secondary side bar, JetBrains' tool window bars, and Xcode's inspector (§4). So this proposal builds a right sidebar of tabs, and makes cmux's Dock its first tab.

Two other readings, and where they end up:
- **The review dock, moved right.** Ready to review, at the sidebar's foot, is called the review dock in DESIGN §4.5. It becomes the Review tab (§5.3) and stays in the left sidebar too, unless the owner says otherwise (decision 6).
- **Irie itself docked at the screen's edge**, as a slim window beside other apps. That is another feature, a compact window, and comes later if wanted (decision 1).

## 2. What `live` has

The right side of the window today ([MAC.md](../MAC.md) §1):
- **The frame.** `TerminalArea` holds the shown session's live terminal, drawn by Ghostty or SwiftTerm as a tmux client of the slot, or its conversation; ⌥⌘T flips between them. Under the frame runs the phrase bar. The toolbar holds the name and subtitle, Show Helpers and Open in Ghostty.
- **The helper pane.** It sits beside the conversation only. The two go side by side from 840 points; the pane is 300 to 520 points wide, 380 at first, with a divider of its own (`PaneDivider`, its width kept in the app's defaults). While the terminal shows, a click on a helper flips to the conversation ([split-pane.md](split-pane.md) §6.2).
- **The team split.** A session's teammates show as live terminals in a column inside the frame. They run on a tmux server of their own, `irie-teams`, and the session's own terminal there is a client of `irie-view`, so only those agents take the split's size (DESIGN §2.3). The Dock's terminals can work the same way.
- **Elsewhere.** The details panel ("working area") and the review dock sit at the left sidebar's foot.

What the core already serves that tabs can use:
- **Rows:** `loose`, the files not committed and the commits the land branch lacks (`internal/worktree/loose.go`, the jot dog's reading); `subagents`; `team`; `tokens`; `warnings`; `tail`.
- **`conversation`:** a session's items and `needs`, or a helper's own conversation.
- **`closed` and `dog`.**
- **Each session's status-line usage:** cost, context share and the plan's use (`internal/core/status.go`).

The owner's own numbers, read locally on 2026-11-01:
- **Window:** 1509 × 944 points, on a screen 1512 points wide. The left sidebar is 296 points and the right side 1212.
- **The view:** the app shows the terminal (Settings > Appearance > On the right), at 144 × 49 cells in 13-point type, about 8 points a column.
- **Switching:** on the journal's five days (2026-10-27 to 11-01) the app showed a session 680, 14, 8, 543 and 794 times, across 213 sessions.
- **Agents:** tmux had 24 agent panes, every one 144 × 49, in the alternate screen with no history.
- **The machine:** a load of 17 to 27 on 12 CPUs, with memory under pressure.

## 3. The catch: the terminal's width is every agent's width

How the width travels:
1. The app's terminal is a tmux client of session `irie`, whose window takes `window-size latest`. The slot is zoomed while the app is open, so the client's size is the slot's.
2. Once a second the core repairs the layout. It gives every agent window but a split one the slot's size (the `resize-window` loop in `internal/tmux/layout.go`), so a swap never resizes an agent.
3. So anything that narrows the app's terminal resizes every agent: 24 SIGWINCHes today.
4. The terminal views hand every frame change to the terminal at once (`GhosttyTerminalView.setFrameSize` calls `ghostty_surface_set_size`). So an animated sidebar would resize the shown agent on every frame, and every agent at each tick the animation spans.

What a resize does to Claude Code depends on which of its two renderers draws it:
- **Fullscreen rendering** draws in the terminal's alternate screen and keeps nothing in the scrollback, so a resize only redraws the screen. It is the default for anyone who first used Claude Code on or after 2026-05-06. Every agent pane on the owner's Mac uses it: all 24 were in the alternate screen with no tmux history (`#{alternate_on}` and `#{history_size}`, read on 2026-11-01).
- **The classic renderer** writes into the scrollback, and its resizes have leaked there. #49086, fixed on 2026-04-21, left a whole frame in the scrollback at each resize. #52660, closed as a duplicate, left ghost copies of the input box in tmux's scrollback when a pane was resized. #84247, open and reproduced since 2026-08-05 (Claude Code 2.1.221), duplicates bands of lines already written, out of order, when the terminal is narrowed and widened while Claude Code writes. #43113, open: Claude Code breaks its lines at the width it had, so old output never rewraps.
- **With either renderer, the width changes what Claude Code draws.** In fullscreen rendering, `/diff` opens a diff panel beside the conversation, inside the terminal. It needs a terminal at least 110 columns wide, and it opens on its own once Claude edits files when the terminal is at least 144 columns wide (Claude Code 2.1.287 or later). The owner's slot is exactly 144 columns, so it opens by itself in their agents; this session's own pane showed it, with its `source: Current ▾` picker. Some users count that opening as a bug (#93160, open).

So the sidebar should change the terminal's width only when you mean it to. The ways to give it room:

| | A. Peek: over the terminal | B. A view session per shown session | C. Pin: the slot narrows | D. The window grows |
|---|---|---|---|---|
| How | The sidebar overlays the terminal's right edge, and the terminal keeps its size | While the sidebar is open, the shown agent's window is linked into `irie-view`, as the team split's lead is | The terminal narrows; at its next tick the core gives every agent the new size | The window widens by the sidebar's width when the screen has room |
| Agents resized | None | The shown one, as you show it and again as you leave it: on a day of 794 shows, about 1,600 resizes, each one while you look | Every agent, once per pin, unpin or drag | None |
| What it costs you | About 40 of the terminal's 144 columns hide while it is open: in the owner's agents, mostly Claude Code's own diff panel | Swaps that never resize | Each agent redraws once per layout change, as when the window is resized; under the classic renderer that may duplicate lines in its scrollback. Below 144 columns Claude Code's own diff panel stops opening by itself | Nothing where there is room. The owner's window fills the screen, so for them it never applies |
| Work | Small | Medium: `show` with a sidebar flag, and `planView` for it | Small | Small, but a window that moves on its own surprises people |

The proposal is **A by default and C when you pin it**. A peek is free, safe under both renderers, and always at hand. Pinning is a choice about the layout, made now and then, like resizing the window. In the owner's window a pinned sidebar leaves the terminal about 100 columns. Claude Code's own diff panel then stays shut, and Changes takes its place (decision 8). B would make the busiest action in the app, showing a session, the one that resizes, so it is out. D could come later as a setting.

**The owner's choice (2026-11-02): C always.** The sidebar sits inline after the terminal whenever it is open; there is no peek. Each open, close and release of its edge resizes every agent once. Since 2026-11-08 it comes and goes as a drawer, lying over the terminal's edge only while it slides.

## 4. What other tools do

Read from public docs, changelogs and issues on 2026-11-01; ideas only.

| Tool | What its right side holds | How it opens | Follows the session? |
|---|---|---|---|
| cmux | A right sidebar: Files, Find, Sessions (the Vault, which searches old agent sessions by transcript), Feed and Dock | ⌥⌘B, or the `cmux right-sidebar` command; no button for it yet, an open request (#7998) | Its visibility is per window; a repo's Dock file beats your own |
| Claude Code desktop, the Code tab | Panes you drag into any layout: chat, diff, browser, terminal, file, plan, tasks, subagent, iOS Simulator. Also a "session details" rail of cards: where it runs, what it changed, what it runs, context | A button per pane in the title bar; panes without one, such as Plan, are listed once they have something to show; ⌘\ closes the focused pane | Yes. A pane can pop out to its own window. Its terminal opens in the session's folder |
| Codex app | A review pane (unstaged, staged, commit, branch, last turn), files and a browser beside the chat; a terminal drawer under it | ⌃⇧G review, ⌘⇧E files, ⌃` terminal | The terminal per project; the browser's width is shared across projects, an open complaint |
| Cursor 3 | An agents window whose right panel has Changes, files, terminal, browser, output, plans and the pull request | Esc toggles the panel; a new project starts with it closed and opens it at the first task | Its tabs come back per agent |
| Conductor | Per workspace: all files, changes, checks (CI, the pull request, comments, to-dos), notes, and a run tab | ⌘⇧D diff | Per workspace, kept across restarts |
| Warp | A code review panel and a notifications mailbox | ⌘⇧+ or a button; it can open by itself at the first accepted diff, if you ask it to | Not stated |
| Zed | Docks left, right and bottom; each holds panels, one showing at a time; its defaults now put the project, outline and git panels on the right | Status-bar buttons; ⌘R or ⌥⌘B for the right dock; ⇧Esc zooms the active pane | Dock places are global settings |
| VS Code | The secondary side bar, opposite the primary one; chat lives there by default | ⌥⌘B; whether it shows is remembered per workspace | Its agents window's panels follow the active session |
| JetBrains IDEs | Tool window bars on each edge | A click on a bar's icon; ⇧⌘F12 hides them all. View modes: Dock Pinned (beside the editor), Dock Unpinned (only while in use), Undock (over the editor), Float, Window | Layouts keep positions and sizes |
| Xcode | Inspectors at the right, picked by a bar at their top | A toolbar button at the right; ⌥⌘0 | Not documented |

What users say about right panes:
- **Widths forgotten or taken.** Claude Code desktop's browser pane forgot its width when toggled (#85936, fixed). Codex's sidebar grows when an image opens (#46654, open), and its browser's width is shared across projects (#20236, open).
- **Opening unasked.** "Stop showing Copilot on launch" (VS Code #257329, fixed). Claude Code's diff panel opening in fullscreen without `/diff` (#93160, open).
- **Moved without asking.** An update moved Zed's panels to the right dock, and users spent hours putting them back (Zed discussion #54180).
- **State lost.** Claude Code desktop's file viewer lost its file and scroll when switching sessions and back (#83002, closed as not planned).

Lessons for Irie:
1. The right side is evidence for the session on show, with its changes first. The Dock holds what your own tools say.
2. It follows the session on show, and each tab keeps its state per session: its scroll, its open file.
3. It stays closed until you open it, and a close stays closed. Nothing opens on its own, as cmux's newest home view also chose (cmux pull request #22702).
4. It has a button and a key, and a key per tab. cmux's lack of a button drew a request.
5. Widths are yours and are kept. A big read zooms over the rest instead of squeezing it, and a tab can go to its own window later.
6. Its tabs can be hidden and reordered, and an update never moves them.

## 5. The proposal

### 5.1 The right sidebar

Inline, after the terminal, in a window as wide as the owner's:

```
┌ Sidebar ──────────────┐ ┌ Fix the login test · Working · claude · fix-login ─────────── ▤ ┐
│ ☀ Irie   ● 1  ◐ 3     │ │ ┌ terminal, about 100 columns ────────────┐ ┌ Dock Changes Helpers ┐ │
│ ┌ find ─────────────┐ │ │ │                                         │ │ Git            ⟳ ■ ⊼ │ │
│ ◐ Fix the login test ═╪═╪═│                                         │ │ (git's own view)     │ │
│ ● Rename the docs     │ │ │                                         │ ├──────────────────────┤ │
│ ✓ Tidy the README     │ │ │                                         │ │ Tests          ⟳ ■   │ │
│                       │ │ │                                         │ │ ok  ./auth  1.2s     │ │
│ ┌ details ──────────┐ │ │ │                                         │ ├──────────────────────┤ │
│ │ ◐ Working         │ │ │ │                                         │ │ App  localhost:8080  │ │
│ └───────────────────┘ │ │ └─────────────────────────────────────────┘ └──────────────────────┘ │
│ ┌ task bar ─────────┐ │ │   Running the tests                                                 │
└───────────────────────┘ └──────────────────────────────────────────────────────────────────────┘
```

▤ is the sidebar's toolbar button, ⊼ (in this drawing) the close button, and ⟳ and ■ restart and stop a control.

- **Opening it.** The toolbar's button (`sidebar.right`, at the toolbar's trailing end, where the HIG puts buttons that open an inspector), View > Show Right Sidebar (⌥⌘B, as in cmux and VS Code), or a tab's own key: ⌥⌘1 Dock, ⌥⌘2 Changes, ⌥⌘3 Helpers, ⌥⌘4 Plan, ⌥⌘5 Review. A click on a helper's line or chip opens Helpers. A click on a card's "Not through" line, or the details panel's, opens Changes. Nothing else opens it: a helper starting, a file changing or a control printing only marks a tab.
- **A drawer, inline.** It sits after the terminal or the conversation (the owner's choice; the research proposed a peek over the terminal and a pin), and comes and goes as a drawer, the owner's later ask. Opening it slides it in from the window's right edge, over the terminal's edge, and once it is in, the terminal narrows to make room, in one step; closing it widens the terminal first, under the drawer, then slides the drawer out. Every agent follows once each time, as when the window is resized. With Reduce Motion on, it opens and closes in one step. In JetBrains' words, it is always Dock Pinned.
- **Width.** It starts at 340 points and goes from 260 to 520 by its edge. In a window that would leave the terminal less than about 80 columns, it gives up width first, down to its narrowest. Dragging its edge draws a guide line, and the width applies on release: one resize, not one per frame.
- **Tabs.** A row of tabs runs across its top, each a short word, in your order; at its narrowest the words give way to icons, as Xcode's inspector bar does. A tab shows a count where it has one, red only when something in it needs you. One tab shows at a time. The tab and the width stay as you go from session to session: open Changes and Ctrl+Tab through your sessions to see each one's changes in turn. Each tab keeps its own state per session, such as its scroll and the file it has open.
- **Zoom.** A tab can fill the right side for a long read, such as a big diff or a busy log: the ⤢ button in its header, or ⌥⌘↩. A zoom lies over the terminal, so nothing is resized. Esc ends it.
- **The keyboard.** A click in the sidebar gives it the keyboard, and Esc hands it back to the terminal or the message box. Ctrl+] still moves between the list and the terminal or the message box.
- **What it keeps.** Whether it is open, its width, its tab and the tabs' order, in the app's defaults beside the right side's choice. A restarted app opens it as it was.
- **Its look.** It sits on the palette's panel ground, like the details panel, under a 1-point edge, square at the window's edge it comes out of and rounded where it faces the terminal. At its top is the shown session's name in that session's colour, as the colour follows the session everywhere ([MAC.md](../MAC.md) §5). It brings no new colours.
- **Accessibility.** The sidebar is a region, "Right sidebar", and its tabs a tab group with their counts. Each tab announces what changes in it, politely. Every action has a VoiceOver action, and text keeps 4.5:1.

### 5.2 The Dock: controls you define

cmux's model, kept close enough that cmux's own files work:

- **Controls.** A control is a live terminal running one command, such as `lazygit`, `tail -f log/development.log` or a test watcher, or a web page, such as a dev server's page or a local dashboard. Controls stack top to bottom in the file's order. Each has a header with its title, ⟳ to restart it, ■ to stop it, ⤢ to zoom it, and a fold. Its height comes from the file or shares what is left, and the rules between controls drag.
- **The files.** A repo lists its controls in `.irie/dock.json`, which the team commits. You list your own in `dock.json` in Irie's config folder, as cmux keeps its own in its config folder. When a repo has no `.irie/dock.json` but has cmux's `.cmux/dock.json`, Irie reads that one, so a team's cmux Dock works in Irie unchanged. cmux's own personal file is not read: it holds what you wanted beside cmux's workspaces, which may name another setup's tools. The repo's controls come first, then yours.
- **The fields** are cmux's: `id`, `title`, `command`, `type` (`terminal` or `browser`), `url`, `chrome`, `cwd`, `height` and `env`. Irie adds one, `scope`, and cmux has no use for it:
  - `session`, the default: one copy per session, run in its worktree;
  - `repo`: one copy in the repo's own folder, such as a dev server;
  - `global`: one copy for everything.
- **Where a control runs.** A `session` control starts in the shown session's worktree (`cwd` is relative to it), so `lazygit` shows the agent's own branch. Each session gets its copy the first time the Dock shows while it is on show, and keeps it until the session closes. So going back and forth between sessions is instant.
- **How a terminal control runs.** In your login shell, as cmux does, in tmux on a server of Irie's own, `irie-dock`, one tmux session per copy, as the team server holds teammates. The Dock draws each copy as a client of its session, with the engine the terminal uses. So a control never resizes an agent, and it outlives a restart of the app or the core. Gabriel can hold back the copies nobody is looking at, as he holds back sessions (DESIGN §4.11).
- **How a web control runs.** In a web view with a data store that keeps nothing, which loads only its page's own origin. It has no bridge to Irie, and links to other sites open in the browser. It shows its address bar and reload button unless `chrome` is false. The rules are those of [generative-ui.md](generative-ui.md) §7.4.
- **Trust.** A repo's file runs commands on your Mac, and agents write files in their worktrees. So Irie runs a repo's controls only after you have trusted that file: it shows each command and asks once, then remembers the file's hash. A changed file asks again and shows what changed, so an agent's edit to it never runs until you have looked. Your own controls in Irie's `dock.json` need no asking. cmux also asks before it starts a repo's controls.
- **Set Up My Dock…** An empty Dock says what the Dock is for and offers this button. It starts a session with a brief like the one cmux publishes:
  - read the repo's scripts, dev servers, logs and tests;
  - ask what to watch when unsure;
  - write `.irie/dock.json`, with no secrets in it.

  The trust prompt then shows what it wrote. Anyone can have a Dock without knowing the file exists (DESIGN §1.4, pillars 1 and 4).
- **Integrations.** Any command-line tool or local web page becomes a control, so the Dock is also where the owner's "integrate with anything" work can show itself to people.

### 5.3 Irie's own tabs

**Changes**, for the session on show. What it changed that has not landed:
- **Its line.** One line in plain words, the jot dog's reading (DESIGN §4.8): "2 files not committed, 2 commits not in live", or "Everything is in live".
- **Its lists.** Under **Not committed**, each file with its status, path and lines added and removed. Under **On fix-login, not in live**, the commits the land branch lacks, newest first, each opening to its own files.
- **A file** opens to its diff in place: unified, red and green, with line numbers, wrapped to the sidebar. ⤢ zooms it. An image or a binary file says so.
- **Actions:** Show in Finder and Copy Path. Later, Ask About It, which puts the file's diff in the message box, and Ask It to Commit, a message typed through `send`.
- **Data:** the core's `changes` and `diff` (§5.5). They are read again after the session's edits and commands, at most every 2 s, and when you show it; never while the tab is closed.

Claude Code's own diff panel (§3) shows the same kind of list inside the terminal. It has two ideas worth taking: an `ask` button that attaches a file's diff to your next prompt, and a view of one turn's edits (`T1`, `T2`, …). Changes adds what it lacks:
- it works in the conversation view and under the classic renderer;
- it keeps working after the session ends or closes;
- it says what has not reached the land branch, which is `live` here, not just the repo's default branch;
- it is the same for every harness Irie runs.

**Helpers**, for the session on show. This is the helper pane of [split-pane.md](split-pane.md) §6.2, moved into the sidebar as it is: the strip of the session's helpers, then the open helper's conversation.
- It opens from a helper's line or chip in either view, so the terminal view no longer flips to the conversation.
- The session's teammates are listed under the strip with their states, and a click on one shows the team split.
- ⇧⌘H, ⌘[ and ⌘] keep their jobs. In the conversation view the sidebar takes the place of today's split.

**Plan**, for the session on show. Its plan in plain words, from the first of these it has:
1. its task list, when it has Claude Code's task tools: each task's subject and whether it is pending, in progress or done, read from `TaskCreate` and `TaskUpdate` (or `TodoWrite`) in its transcript;
2. the plan it had approved in plan mode (`ExitPlanMode`'s plan, in Markdown);
3. a line saying it keeps its plan in its head, with the setting that turns task lists on (decision 5).

Claude Code provides the task tools by default only on Opus 4.7, Sonnet 4.6, Haiku 4.5 and earlier models. On newer ones it leaves them out unless `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` is set (Claude Code 2.1.233 or later), because "the tools' definitions and reminders take up context". Irie's `+claude` runs `opus-5-5`, so most sessions would show (2) or (3). Like Claude Code desktop's Plan pane, the tab can stay out of the row until a session has something to show in it.

**Review**, for all sessions. Everything ready for you to look at, in one list:
- the sessions to verify (done and not parked);
- those that closed on their own (the review dock's list);
- the jots that are not through (the jot dog's `dog`).

A row joins at the end and leaves only when you open it, park it or drop it, so nothing slides under the pointer. A click shows its session, and its row opens to its changes in place.

Later tabs, each waiting on its feature:

| Tab | What it shows | Waits on |
|---|---|---|
| History | Closed sessions found by what was said in them, reopened with a click, as cmux's Vault finds old agent sessions | A search over closed sessions' transcripts; `reopen` reaches the newest 100 today |
| Files and Find | The shown worktree's files, with a preview, and a search across them | Their own small proposal |
| Activity | One line per tool call across all sessions, the newest at the foot, as cmux's Feed | A `subscribe` with `events` and phrases, which exists |
| Preview | A page an agent built, in a sandboxed web view | [generative-ui.md](generative-ui.md) §7.4, and the browser connector if it lands |
| Cards | An agent's pinned cards | [generative-ui.md](generative-ui.md) §7.3 |
| Notes | Notes an agent gets at its next tool call | M7's `note` |
| Usage | The session's cost, context share, model and effort, and the plan's five-hour and weekly use | A row field for the status-line data the core keeps (`status.go`) |

### 5.4 The rules it keeps

From the UI and UX pillars (DESIGN §1.4) and the rule that only you move a card (DESIGN §4.1):
- **Nothing moves or opens by itself.** The sidebar never opens or switches tab on its own, and its tabs keep their order, through updates too. State shows as counts and colour.
- **Plain words first.** The exact thing is a click away: Changes' line, then its files, then a diff.
- **It opens small.** The sidebar starts closed, and a tab shows its summary first.
- **Yours to change** (§5.7). Every change shows at once and can be undone, and the defaults are a click away.
- **The terminal resizes only when you change the layout:** opening or closing the sidebar, letting go of its edge, or resizing the window.
- **Nothing runs that you did not trust.** A repo's Dock controls wait for your yes, and ask again when their file changes.
- **Free while closed.** No subscription, no git reads, no timers. Dock controls that are running keep running, and Gabriel can hold them back.

### 5.5 The core's part

The core owns every rule and clients only draw ([CORE.md](../CORE.md)). So each tab's data comes from the core, and the terminal sidebar, `irie ctl` and the phone can draw the same later.

| Op or frame | Args | Result |
|---|---|---|
| `dock` | `session`; `start?`, `stop?` or `restart?` with a control's `id`; `trust?` with a file's hash | `controls[]`, each with its fields, where it came from (`repo`, `cmux` or `config`), its state (`untrusted`, `stopped`, `running` or `exited` with a code), and for a terminal the tmux socket and target to attach to; `untrusted?`: the file's path, hash and commands, for the trust prompt |
| `changes` | `session` | `branch`; `land`, the branch it lands on (`live` in Irie's own repo, else the repo's default); `files[]`, each `path`, `status`, `added`, `removed`, and `commit` when committed; `commits[]`, each `sha`, `subject` and `time`; `more?` |
| `diff` | `session`, `path`, `commit?` | `text`, a unified diff cut at 200 KB or 5,000 lines; `binary?`; `more?` |
| `subscribe` with `session` and `changes: true` | | `{"t":"changes","changes":{…}}` frames after the session's edit and command hook events, debounced to 1 s, with one read in flight per session |
| `conversation` frames | | `plan`: `items[]` (`text`, `status`), `text?` (an approved plan), and `source` (`tasks`, `todos` or `plan`) |

- **The Dock's server.** `tmux -L irie-dock`, with a generated config like the team server's (DESIGN §2.3). The core starts a copy when `dock` asks, keeps a copy's tmux session named for its control and session, ends a `session` copy when its session closes, and never stops the server on a restart. Trusted hashes live in the state folder.
- **How Changes reads.** On `ReadLoose`'s git reads (`status --porcelain=v2` and the land branch's commits), plus `git diff --numstat -z` for both, within the same timeouts. A session outside a git repo says "Not in a repo". Claude Code's `bashEditDiffEnabled` could tell it which commands changed files (§9).
- **Costs.** No tokens. Git runs only for the shown session while its tab shows. A Dock control costs what its command costs. Step 3 measures Changes against the yardstick of [optimisation.md](optimisation.md) iteration 1.
- **Task lists.** `spawn.task_lists`, off by default, would put `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` in each Claude Code session's environment (decision 5).

### 5.6 How it is built

The candidates the Mac offers, on the macOS 14 SDK the app builds with:
- **SwiftUI's `.inspector`** (macOS 14) gives a trailing column that SwiftUI restores, with `inspectorColumnWidth(min:ideal:max:)`. Apple's WWDC23 talk says a width the user drags is kept across launches. It always takes a column of its own, so it cannot lie over the terminal. And on macOS 14 people have reported faults: it opens at its minimum width rather than its ideal one, and on a `NavigationSplitView` it adds the toolbar's height to the detail's minimum height, which an Apple engineer called a bug.
- **AppKit's inspector item**, `NSSplitViewItem(inspectorWithViewController:)` with `toggleInspector(_:)` and the toolbar's `inspectorTrackingSeparator` (macOS 14), would mean moving the window off `NavigationSplitView` into an `NSSplitViewController`. Its toggle animates too.
- **A third `NavigationSplitView` column** can't be hidden on macOS ("macOS always displays the content column"), and the sidebar is not a level of navigation.
- **`HSplitView`** keeps no divider position and has no holding priority.

So the sidebar is a column of the app's own, in `TerminalArea`, as the helper pane already is:
- **The terminal keeps its place.** The frame is always the first child, with the sidebar after it in an `HStack`. SwiftUI never makes the terminal view again, which would restart its tmux client ([split-pane.md](split-pane.md) §6.2).
- **One step for the terminal.** The terminal's room for the sidebar changes in a transaction with no animation: once the drawer is in, as a close starts, and on a release of the edge. Only the drawer animates, by a move transition, which offsets it and never changes the terminal's size.
- **The edge** works like `PaneDivider`, with a guide line while it is dragged and the width set on release, 1 point wide as the HIG asks of split views.
- **Under the conversation.** The terminal's layer and the conversation's both take the right side's width less the sidebar's.
- **Dock terminals** are `GhosttyTerminal` views (or SwiftTerm's), each attached to its copy's session on `irie-dock`, as the team split's panes are attached to theirs.
- **Tests.** `--snapshot` draws the sidebar in each tab, light and dark. A test in the in-process harness can toggle it and check that the terminal view is still the same object and that each open or close changes the grid exactly once.

### 5.7 Yours to change

- **Settings > Appearance > Right Sidebar:** which tabs show and in what order; the width to start at; which tab ⌥⌘B opens, the last one or a fixed one; and where Ready to review shows, the left sidebar, the right one or both.
- **Settings > Dock:** your own controls, a row each, to add, edit, remove or reorder; the repo files Irie found and whether each is trusted.
- **In the sidebar:** drag a tab to move it. Right-click a tab for Hide, Move Left, Move Right and Reset. Right-click a control for Edit, Restart, Stop and Remove.
- **In plain words**, later: `:dock` words in any task bar, such as `:dock changes`, `:dock close`, `:dock hide plan` and `:dock add lazygit`. The core reads them as it reads every `:` word, and answers with what the app should do. That kind of answer is new: today every `:` word is an action of the core's own, such as `:park` or `:vpn`. cmux's open request #8092 asks for the same, commands for every part of its window.

### 5.8 The terminal sidebar and the phone

- **The terminal sidebar** has one slot and no room for a right sidebar. `irie ctl changes [SESSION]` prints what Changes shows, and `irie ctl dock` lists the controls and attaches to one in the slot's terminal.
- **The phone apps** can draw Changes and Plan from the same ops, as they draw `screen` today ([PHONE.md](../PHONE.md)).

## 6. What DESIGN and MAC would change

- DESIGN §2.3: the Dock's server, `irie-dock`, beside the team server; and one more invariant: the slot's width changes only with the window or the right sidebar.
- DESIGN §2.4: `dock`, `changes`, `diff`, the `changes` frame, and `plan` in conversation frames.
- DESIGN §4.4: helpers show in the right sidebar, beside the terminal as well as the conversation.
- DESIGN §4.7: where a session's plan comes from.
- DESIGN §5.4: the user's `dock.json` and the trusted hashes.
- [MAC.md](../MAC.md) §1: the right sidebar and its tabs. §4: ⌥⌘B, ⌥⌘1 to ⌥⌘5 and ⌥⌘↩.

## 7. Order of work

1. **The sidebar and Helpers (M).** The column, inline after the terminal, the width, the keys and what it keeps, with the helper pane moved in. `--snapshot` draws each of its states, and tests check the resize rules (§5.6). Ship it, so the owner can try it live.
2. **The Dock (L).**
   - The `dock` op, the `irie-dock` server, the files (`.cmux/dock.json` too) and trust.
   - Terminal controls first, then web controls.
   - Then Set Up My Dock….
3. **Changes (M).** The core's ops and frames, then the tab and its diff, sharing their rows with [generative-ui.md](generative-ui.md) §7.2.
4. **Plan and Review (S each).**
5. **Making it yours (S).** The Settings sections, moving and hiding tabs, and the `:dock` words.
6. **Later tabs**, each with its feature (§5.3).

## 8. Risks

| Risk | What keeps it small |
|---|---|
| A repo's Dock file, or an agent's edit to it, runs a harmful command | Nothing runs before you trust that exact file. A changed file asks again and shows the change. Your own controls are yours |
| Dock controls pile up: a copy per session | A copy starts only when shown and ends with its session; `repo` and `global` controls run once; Gabriel holds back the unseen |
| Opening or closing the sidebar resizes every agent once; under the classic renderer, #84247 may duplicate lines in the scrollback of agents that were writing | Once per open or close, never per animation frame; the owner's agents use the fullscreen renderer, where a resize only redraws |
| SwiftUI makes the terminal view again, which restarts its tmux client | The frame keeps its place in the view tree, and a test checks the terminal view is the same object after every change (§5.6) |
| Too little room beside a team split and the left sidebar | The sidebar gives up width first, down to 260 points |
| Noise | Counts stay quiet, red is only for what needs you, and a control's output marks its tab only when you ask it to |
| Git reads on a busy machine | Only for the shown session while its tab shows, one in flight, within timeouts |

## 9. Found on the way

- **A window resize resizes the agents several times.** `GhosttyTerminalView.setFrameSize` passes every size of a live window resize to tmux, and the core gives every agent each slot size its once-a-second tick sees. So a three-second drag of the window's edge can redraw each agent three times, and under the classic renderer each resize while an agent writes may duplicate lines (#84247). Holding the terminal's size through a live resize (`inLiveResize`, then `viewDidEndLiveResize`) would make it once. It is worth a change of its own, apart from the sidebar.
- **Claude Code's diff panel opens by itself at exactly the owner's width.** Its rule is 144 columns or more, and the slot is 144. So in agents that have edited files, unless someone closed it, the terminal's right side is Claude Code's own panel: 23 of the 144 columns in this session's pane. Irie neither knows nor shows this. A pinned sidebar, or one column less, keeps it shut. The panel is a built-in mod, `cc-plugin-diff`, which `/plugin` can turn off, and `/diff` closes it for later sessions too.
- **The owner's agents all run Claude Code's fullscreen renderer.** All 24 panes were in the alternate screen, so tmux keeps no history for any of them. The terminal's own scrollback and tmux's copy mode show nothing of the conversation. Scrolling and selecting happen inside Claude Code, which copies on release.
- **Claude Code's own task list is empty in most of the owner's sessions.** The newer models get no task tools by default (§5.3), so Ctrl+T in their terminals shows nothing.
- **Claude Code writes a recap** of a session when you come back to it after three minutes away, up to 400 characters. Irie does not read it yet. Where it lands in the transcript is not documented, so check before Plan uses it.
- **`bashEditDiffEnabled`**, a Claude Code setting, gives PostToolUse Bash hooks the files a command changed. Changes could use it to know when a command touched files, rather than reading git after every command.

## 10. Decisions for the owner

| # | Decision | Options | Recommendation |
|---|---|---|---|
| 1 | What it is | a. A right sidebar of tabs inside Irie's window, with cmux's Dock as its first tab. b. Irie itself as a slim window at the screen's edge | **a**; b can come later as a compact window |
| 2 | How it meets the terminal | a. Peek over it, pin beside it. b. Always beside it, so every open and close resizes every agent. c. Always over it | The research recommended **a** (§3); the owner chose **b** on 2026-11-02, inline after the terminal, and on 2026-11-08 a drawer that slides in before it settles there |
| 3 | The Dock's files | a. `.irie/dock.json` in a repo, cmux's `.cmux/dock.json` read when there is none, and your own controls in Irie's own `dock.json`. b. Irie's own files only | **a**, so a cmux Dock works unchanged |
| 4 | The first tabs | Dock, Changes, Helpers, Plan and Review, or fewer | **All five**, in §7's order |
| 5 | Task lists on newer models | a. Off: Plan shows plan-mode plans and a note. b. On for every Claude Code session, at a cost in context every turn | **a**, with the setting there for b |
| 6 | Ready to review | a. In both sidebars. b. The right one only | **a**, so nothing the owner knows moves |
| 7 | Its name in the menus | Right Sidebar, Right Dock, Inspector or Side Panel | **Right Sidebar**, cmux's word and the match for Irie's left one, with **Dock** as its first tab; the Mac's own Dock stays out of the menus |
| 8 | Claude Code's own diff panel, inside the terminal | a. Leave it: the open sidebar takes the terminal under 144 columns, which keeps it shut. b. Turn the `cc-plugin-diff` mod off in Irie's sessions while Changes is on | **a** for now; b only if a documented setting can do it per session |
