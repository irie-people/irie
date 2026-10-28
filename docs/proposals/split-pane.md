# Proposal: helpers beside their session (the split pane)

Date: 2026-10-27. Status: option C is being built (see "Built so far" below); decisions 2 to 4 are still the owner's. It answers two open questions: [DESIGN](../DESIGN.md) §10 decision 4, which left "selecting an orchestrator splits the slot" for after v1, and where a subagent shows at all, which M8 never settled once the conversation view and the Mac app arrived. An agent (Claude Code) wrote it, from `live` at `cb29dff`. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

**In short.** A Claude Code subagent is headless: it has no terminal of its own. It runs inside its session's `claude` process, and all there is to see of it is its transcript, its hook events and its row in Claude Code's subagent status line. So a split pane that puts a helper beside its session is a view Irie draws, not a tmux pane: Irie draws the helper from its transcript, the way it already draws the session's conversation. In the Mac app, opening a helper splits the conversation view: the session's conversation stays where it is, and the helper's own conversation sits beside it under a strip of its siblings. The terminal sidebar's slot does the same inside its one pane. Nothing is added to tmux, so no agent is resized or signalled, and the mirror windows M8 planned are not needed. The raw terminal does not split, because a narrower terminal resizes every agent window, 59 of them on the day this was written.

| | Today on `live` | Proposed |
|---|---|---|
| A running helper | One line under its card, and a chip in the conversation | The same, and a click opens its pane beside the conversation |
| What it says and does | Nowhere: subagents' lines are skipped | Its own conversation, live, in plain words |
| A helper that finished | Gone from the card at once | Stays, marked done, until you drop it or the session's next prompt plus 10 minutes |
| A delegated session | Its own card; only the terminal sidebar marks it `↳ delegated` | Under its parent's card, and opens in the same split |
| tmux | One pane in the slot | Unchanged |

**Built so far** (2026-10-27, at the owner's request, with a style setting):
- the core: a helper's conversation through `conversation` and `subscribe` with `agent: "sub:<id>"`, chips that name their helper, and `ui.helper_style` in the header (DESIGN §4.4);
- the Mac app: the helper pane beside the conversation, or in its place in a narrow window, with the strip, the clickable lines under a card, View > Show Helpers (⇧⌘H) and Go > Next and Previous Helper (⌘] and ⌘[) ([MAC.md](../MAC.md));
- two looks, which Settings > Appearance > Helpers picks: `browser`, cards and plain words as in §6.2, and `terminal`, a terminal's lines on its dark ground, still drawn by the app, so no agent is resized.

**Then the terminal split** (2026-10-27, at the owner's request after the terminal look "did not work": "copy cmux with this functionality"). The terminal view never split, because a helper has no terminal. An agent team's teammates do have terminals, so §6.5 went the other way from what it proposed: with `spawn.teammates = "panes"` Claude Code runs with `teammateMode: "tmux"`, and its teammates open as terminals beside their session in the Mac app's terminal view, as cmux shows them (DESIGN §2.3, [MAC.md](../MAC.md)). The tmux calls Claude Code makes were read off Claude Code 2.1.310 with a logging `tmux` in a throwaway server, so the shim follows what it does rather than any docs: `split-window -d -t <pane> -h -l 70% -P -F '#{pane_id}' -- cat` for the first teammate, `-v` from the first for the rest with `select-layout main-vertical` and `resize-pane -x 30%` on the lead, then pane options, `select-pane -T`, `respawn-pane -k` with the teammate's own `claude` command line, and `kill-pane` for each as the lead exits. They go to a team server of Irie's, where each teammate gets a tmux session of its own, and the session's own terminal shows through a view of its window, so no other agent is resized. Off by default: teammates are Claude Code sessions of their own and cost tokens like one.

Not built yet: the subagent status line feed (§6.1), finished helpers kept on the card itself, delegated sessions under their parent (§6.4), the terminal sidebar's split (§6.3), in-process teammates as helpers (§6.5), and pause and notes per helper (§6.6).

---

## 1. A session's children

Four kinds of agent can work for a session. Only the first is common today.

| Kind | Started by | Where it runs | Own terminal | What Irie can see | Shown on `live` |
|---|---|---|---|---|---|
| Subagent, forks included | The model, with the Agent tool | Inside the session's `claude` process | No | SubagentStart and SubagentStop with `agent_id` and `agent_type`; tool hooks fired inside it carry its `agent_id`; its transcript `<session id>/subagents/agent-<agent id>.jsonl` beside the session's; its row in `subagentStatusLine` | A line under its card; a chip in the conversation |
| Agent-team teammate | The model, only with `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` | In-process inside the lead by default; in split-pane mode a separate `claude` in a tmux or iTerm2 pane | Only in split-pane mode | In-process: SubagentStart for each message it is sent, TeammateIdle, TaskCreated and TaskCompleted, with `agent_id` from 2.1.290 | As a subagent, when in-process |
| Delegated session | `irie ctl delegate`, or `spawn` with `parent` (DESIGN §6.4) | A session of its own, in its own pane | Yes | Everything a session has, and `parent` on its row | Its own card. The terminal sidebar marks it `↳ delegated`; the Mac app ignores `parent` |
| Background session | `claude --bg` in an agent's shell | Under Claude Code's own supervisor, one process each | Yes, through `claude attach` | `claude agents --json` | Under Outside Irie (DESIGN §4.6) |

What Claude Code's docs say about subagents, as of 2.1.311:
- **Background by default.** "With nothing configured, a subagent that Claude spawns from your main conversation starts in the background." Such a launch returns at once with `status: "async_launched"` and the subagent's `agentId`, and the session goes on working. Fork mode is on by default too, in a terminal session since 2.1.232: Claude starts forks on its own, subagents that inherit the whole conversation.
- **One id.** The `agent_id` of the hooks, the `agentId` of the Agent call's result and the transcript's file name are the same id. The Agent SDK's docs give a subagent's task id as that id too, which makes it the status line's `id`. An `agent-<id>.meta.json` beside the transcript holds the helper's name.
- **Depth and number.** Up to three layers below the main conversation (`CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH`), and up to 20 at once (`CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS`). Claude Code deletes their transcripts after `cleanupPeriodDays`, 30 days by default.
- **Their own TUI.** Claude Code lists running helpers in a panel under the prompt. ↑ and ↓ pick one; Enter opens its transcript, and what you type there goes to it; `x` stops it. A helper that finished well loses its row at once, and one that failed or was stopped keeps it for 30 seconds.
- **From outside.** Nothing documented lets another program type to a running subagent. The tool hooks it fires carry its `agent_id`, and a hook's `additionalContext` answers that call, so a note should reach the subagent at its next tool call. The docs do not say outright that it lands in the subagent's context rather than the session's, so the smoke test checks it (§8).
- **Starts and stops.** SubagentStart fires when Claude spawns a subagent, again when it resumes one, and each time an in-process teammate handles a new message. SubagentStop also fires when one of Claude Code's own internal agents finishes, such as a prompt suggestion or a `/btw` answer, with no SubagentStart before it. Such an internal agent carries the session's own agent name as its `agent_type`, or an empty one. It is nobody's helper.

**The subagent status line.** The `subagentStatusLine` setting, like `statusLine`, runs a command "once per refresh tick" with every visible subagent row on stdin as JSON, and the command may print a row body in place of Claude Code's own. Each row carries `id`, `name`, `type` (`local_agent`), `agentType`, `status` (`running`, `completed`, `failed`, `killed`), `description`, `label` ("short progress summary of the task when Claude Code has one"), `startTime`, `model`, `effort`, `contextWindowSize`, `tokenCount`, `tokenSamples` and `cwd`. A command that prints nothing leaves every row as Claude Code draws it. It is the only documented feed of each subagent's progress, tokens and model; the hooks give its tool calls.

## 2. What `live` has

- **Tracking.** The core keeps each session's running subagents from SubagentStart and SubagentStop (`track.subs`, `internal/core/state.go`), each with its kind, start time, and what its newest tool call does or what it needs. A subagent is dropped the moment it stops.
- **The Mac app.** Up to three helper lines under the card, then "and N more" (`HelperRows`, `SidebarView.swift`), and the same in the details panel. In the conversation, each Agent call is a `HelperChip`, and it shows what its helper does now only while no other helper of the same kind runs, because nothing ties a chip to its helper (`ConversationView.swift`, `live(for:)`).
- **The conversation reader** drops every line marked `isSidechain` and every transcript under `subagents/` (`internal/core/conversation.go`). Finished subagents' transcripts count only toward the token tally.
- **The wire.** `show` refuses any agent but `main` (`internal/core/core.go`). `subscribe` already takes an `agent` and sends only that agent's events (`internal/core/subscribe.go`), and `RoleMirror` exists (`internal/tmux/tmux.go`), but nothing makes a mirror.
- **tmux.** `Plan` keeps `irie:0` at two panes with one pane in the slot, zooms the slot while the Mac app is open, and keeps every agent window at the slot's size (`internal/tmux/layout.go`). The app runs one SwiftTerm view, a tmux client of the zoomed slot, so it can show only one pane.
- **Not built.** Pause (M5) and notes (M7), which per-helper pause and notes need.

## 3. How helpers are used

One day of one maintainer's journal, 2026-10-27, with 110 sessions:

| Measure | Value |
|---|---|
| Sessions that started any subagent | 13 (12%) |
| Subagents started | 26; at most 5 in one session |
| Most at once in a session | 1 in 10 sessions, 2 in 2, 3 in 1 |
| How long one ran | median 21 min; 10% under 5 min; 10% over 2.1 h; longest 3.1 h |
| Kinds | `deep` 15, `general-purpose` 5, `dev` 5, `fork` 1 |
| Tool calls made inside subagents | 1,906 of 10,646 (18%) |
| Permission prompts from subagents | 0 |

So a split has to show one to three helpers that run for a long time, well. It does not have to tile twenty. Nearly a fifth of all tool calls are subagents', and today they show only as the one line that says what each is doing now.

## 4. What other tools do

Read from public docs, changelogs and issues; ideas only.

| Pattern | Who does it | What users say |
|---|---|---|
| A card in the parent's transcript | Claude Code, Cursor, VS Code, Zed, Gemini CLI, goose, Kiro, Amp | Cheap and keeps the story. Collapsed it is a black box ("no visibility"); expanded it buries the parent |
| Drill into the child, with a way back | Claude Code's panel, opencode, Cursor, VS Code, Warp's pills, the Codex CLI | Detail on demand. Users lose track of where their typing goes, and read-only children are resented |
| A live strip or panel of children | Claude Code, Warp, Kiro, the Codex IDE extension | Glanceable, but rows vanish (Claude Code clears a finished one at once) or show the wrong state |
| A lead pane with the children's terminals stacked | Claude Code's split-pane teammates, cmux | Praised for watching work live. Panes get too short at four or five, the lead's width resets, keystrokes race |
| A grid of peers | Cursor 3.1, VS Code's agents window, Claude Code's desktop app, Warp | For comparing peer sessions, not a parent and its children |

Lessons for Irie:
1. The parent's conversation stays home. A child is a quiet card there with one live line, and its own lines never pour into the parent's.
2. A split is the parent and one child, opened from the child's card, with a strip for its siblings. Keep the layout the user made, and show one pane when narrow.
3. Say where typing goes. Never send text to a child silently, or to the parent when the user meant the child.
4. A child stays reachable after it finishes, with how it ended.
5. Children nest. Show depth in the child's own pane, not as a deeper and deeper tree.

## 5. Options

| | A. Split the slot in tmux (decision 4b as written) | B. Mirror windows swapped in (decision 4a, M8 as written) | C. Draw helpers in Irie's own views (proposed) | D. Children's terminals side by side |
|---|---|---|---|---|
| What it shows of a subagent | A mirror process per subagent, drawing its transcript as terminal text | The same, one at a time in the slot | Its conversation, drawn by the app's SwiftUI or the slot's Bubble Tea | Nothing: a subagent has no terminal. Only delegated sessions and split-pane teammates |
| Agents resized | The orchestrator and its mirrors at every split change, and every other agent window, which keeps the slot's size | None | None | None, if each tile scales its text to the child's own grid. A tile that does not gets a cropped corner, or dots round the grid |
| tmux layout | `irie:0` gets more panes than its invariants allow; in the Mac app the zoom must end, and the app then shows a tmux split | A window per subagent in `irie-agents` | Unchanged | Per tile, a grouped session of `irie-agents` and a read-only client (`attach -r`, which also leaves the window's size alone) |
| In the Mac app | A terminal look, the one the owner turned down | Terminal text | Native, like the conversation | One SwiftTerm view per tile |
| Extra processes | One mirror per subagent | One mirror per subagent | None | One tmux client per tile, 1 to 3 MB each |
| Effort | Large | Medium | Medium | Large |

**A** breaks the layout's invariants and the rule that showing never resizes an agent, and draws subagents as terminal text, which the Mac app was built to avoid. **B** was planned when the slot showed raw terminals. With the conversation view the default, a mirror pane would draw terminal text where Irie already draws conversations. It also costs a process and a window per subagent. **D** is the only way to see live terminals side by side, and it cannot show the common child at all. tmux draws one window per client, and a pane lives in one window, so N live terminals take N clients. Checked on tmux 3.7b: with the agents' windows at `window-size manual`, attaching and resizing clients of other sizes sent no SIGWINCH. A smaller client saw a corner of the window that follows the cursor, and a larger one saw dots round it. D fits a later grid of peer sessions better than a parent and its children.

**Headless runs.** Claude Code's print mode (`claude -p --output-format stream-json`) streams every message as JSON. It marks a subagent's messages with `parent_tool_use_id`, and with `--forward-subagent-text` it includes their text. That would give Irie a typed feed instead of a transcript to follow, but only for a session Irie starts headless. A subagent of an interactive session cannot be made headless. A headless session has no terminal to flip to, and no person at a terminal answers its prompts. They go to an SDK host or a `--permission-prompt-tool`, and with `--permission-prompts none` whatever would ask is denied. It fits one kind of child, later: a delegated subtask could run headless and show in its parent's strip from its stream. It could then open in a terminal with `claude --resume <id>`, which the docs allow for print-mode sessions.

## 6. The proposal: C

### 6.1 The core: helpers as data

- **A helper record per subagent, kept after it stops.** From SubagentStart: its id (`sub:<agent id>`), kind and start. From the subagent status line: its name, description, `label`, status, model, effort, tokens, context window and folder. From SubagentStop: when it ended and how (`completed`, `failed` or `killed`, as the status line says), and its transcript path, which confirms the one derived at SubagentStart. A row gains `helpers`, every helper with its state. `subagents` stays as it is for older clients: the running ones only.
- **Kept until dropped.** A finished helper stays until a view drops it, or until `subagents.keep_finished` (10 minutes, named in DESIGN §5.5 but not yet read by the config) after the session's next prompt. A turn's helpers therefore stay while you read what the turn did, and a finished session keeps the helpers of its last turn.
- **Born at SubagentStart only.** A SubagentStop for an agent that never started, an internal one, makes no helper. A SubagentStart for a helper Irie already has, as when Claude resumes it or a teammate takes a new message, makes it run again in the same place.
- **Nested helpers.** A helper that another helper started is an `agent` item in that helper's conversation, and its record names that helper as `by` once the transcript says so. The strip of §6.2 lists every helper of the session, nested ones too, flat and in the order they started.
- **The status line feed.** Irie's settings file adds `subagentStatusLine` running `irie statusline --subagents`, as it adds `statusLine` today (DESIGN §6.3). It forwards each tick's rows to the core (op `status`, with `tasks`), then runs the user's own `subagentStatusLine` on the same input, or prints nothing, which leaves Claude Code's rows as they were. The docs do not say whether `refreshInterval` paces this command while the orchestrator idles and waits on its helpers, so the smoke test measures the ticks. Without the feed, as on an older Claude Code, the hooks' newest tool call stands in, as on `live` today.
- **A helper's conversation.** `conversation` and `subscribe` with `conversation: true` take `agent: "sub:<id>"`. The core reads that helper's transcript with the reader it has, keeping the sidechain lines it skips for the session. The helper's first message is its task from the orchestrator, so it is a `user` item marked `from: "parent"`, not a message from you. So are later messages the orchestrator sends it with SendMessage. Its own Agent calls are `agent` items as in any conversation, which is how nested helpers show. `needs` is the helper's own: a permission prompt fired with its `agent_id`.
- **Chips know their helper.** An `agent` item carries the helper's id once the Agent call's result has `agentId`, which a background launch, the default, returns at once. A foreground call that is still running is matched to the helper whose status line `description` is the call's `description`, and else to the only running helper of its kind, which is what the Mac app does now.
- **`show` with `agent`.** `show {session, agent: "sub:<id>"}` picks the helper the views show. A helper is always drawn as a conversation, so in the terminal view it flips the slot to the conversation view.

### 6.2 The Mac app: the split

A wide window, with a helper open:

```
┌ Sidebar ─────────────┐ ┌ Fix the login test · Working · claude ─────────────────────────────────┐
│ ◐ Fix the login test │ │ ┌ Conversation ──────────────────┐ ┌ ◐ deep 18m · ✓ Explore ─────────┐ │
│   ↳ deep  Running…   │ │ │ You  fix the flaky login test  │ │ deep · Find the race            │ │
│   ✓ Explore  Done    │ │ │ Claude  I'll look at two…      │ │ Working · 22k tokens · 11%      │ │
│ ○ Rename the docs    │ │ │ ↳ deep  Find the race  ◌ 18m   │ │                                 │ │
│                      │ │ │ ↳ Explore  Map auth    ✓ 1m    │ │ From the session  Find out…     │ │
│                      │ │ │                                │ │ Ran the tests         ✓ 40s     │ │
│                      │ │ │                                │ │ Edited 2 files       +14 −3     │ │
│                      │ │ │                                │ │                                 │ │
│                      │ │ │ Message box                    │ │ No messages yet · Terminal ›    │ │
│                      │ │ └────────────────────────────────┘ └─────────────────────────────────┘ │
└──────────────────────┘ └────────────────────────────────────────────────────────────────────────┘
```

- **Opening.** Click a helper's chip in the conversation, or its line under the card, or the toolbar's helper button, which shows how many there are. View > Helpers toggles the split. A helper starting never opens it, so the layout changes only when you ask.
- **Where.** The window opens 1200 points wide, which leaves the right side about 910. From about 900 points the two sit side by side: the conversation keeps at least 520 points, the helper column takes the rest up to 480, and the bar between them can be dragged, with the app keeping where it was left. A narrower right side shows one at a time: the helper's pane takes the conversation's place, under a line that leads back ("‹ Fix the login test").
- **The strip.** The top of the helper pane is a strip of the session's helpers, one pill each, in the order they started. A pill holds its kind, its state's glyph from DESIGN §5.3 and its elapsed time, says which helper started it when another did, and turns red while that helper needs you. A finished helper keeps its pill and its place until it is dropped (× on the pill), so nothing moves on its own.
- **The pane.** A header with the kind, name or description, state, elapsed time, tokens with the context share, and model. Under it, the helper's conversation, drawn by the same rows as the session's (`FeedContent`): the task as a card from the session, its replies, its calls in plain words, its own helpers as chips that open in this pane. A finished helper ends with how it went: "Done in 21 min · 48 tool calls", and its report.
- **Typing.** The pane's foot says plainly that a helper can't take messages from Irie yet, and offers the terminal, where Claude Code's panel can type to it. Once notes exist (M7), the foot becomes a box that sends a note the helper gets at its next tool call (§6.6).
- **The terminal view.** It does not split. Clicking a helper while the terminal shows flips to the conversation view with that helper open, as the click asks. The split lives in the conversation's layer, and the terminal stays attached under it at its full size, in the same place in the view tree. Were SwiftUI to make the terminal view again, its tmux client would restart.
- **The divider.** SwiftUI's `HSplitView` is an `NSSplitView` that keeps each side's minimum width but no position (it has no autosave name), so the app stores where the divider was left.
- **Delegated sessions** sit under their parent's card like helpers, as full cards of their own (§6.4), and open in the same split.
- **Keys.** ⌘[ and ⌘] step back and forth through the strip, as Back and Forward do in other Mac apps, and Esc in the helper pane closes it. Ctrl+] still moves the keyboard between the list and the session's message box.
- **Accessibility.** VoiceOver reads a pill as its kind, task, state and time, and reads the conversation and the helper pane as two separate regions.

### 6.3 The terminal sidebar

- **Rows.** A helper's line under its card becomes a child row that can be selected, as DESIGN §4.1 and §4.4 intended, with `ui.max_sub_rows` (6) before `+N more`. Enter or a click shows that helper.
- **The slot.** `irie placeholder` already draws the shown session's conversation. Showing a helper splits that drawing inside the same pane: the session's conversation above and the helper's below, at least 8 rows each, or the helper alone when the pane is shorter than 20 rows. `[` and `]` in the slot step through the session's helpers, and Esc closes the helper. It is one pane, so tmux does not change.

### 6.4 Delegated sessions

A delegated session already has everything the split needs: its conversation comes from the `conversation` op as any session's does. Today a new session goes last in the list's one order (`internal/core/order.go`). A delegated one would go instead right after its parent and the parent's earlier children, once, when it is spawned. The Mac app reads `parent` and draws such a card a step in, under its parent's helpers. Each child keeps its own card, marks and actions. It also gets a pill in its parent's strip: opening it puts its conversation in the helper pane, and its raw terminal is a flip away, as for any session. A child the user drags elsewhere stays where it is put, by the list's one rule: only you move a card.

### 6.5 Agent teams

Teams are experimental and off by default, and their default display mode is in-process: every teammate runs inside the lead, where the hooks see it. A user may have set `teammateMode` to `auto`, `tmux` or `iterm2`. `auto` uses split panes "when you're already running inside a tmux session". An Irie agent does run inside tmux, but Irie hides `TMUX` from it. The docs do not say how Claude Code tells, so whether `auto` falls back to in-process inside Irie is for the smoke test. `tmux` asks for panes outright. By public issue reports, a lead outside tmux then starts a tmux server of its own, which no Irie view shows. The docs also list Ghostty, the other terminal Irie supports, as one where split panes don't work.

So Irie's settings file sets `"teammateMode": "in-process"`, unless the profile's own arguments pass `--teammate-mode`. A `--settings` payload applies over the user's own settings. In-process teammates then show as helpers. The other way would show teammates in panes Irie draws itself, as cmux does by turning Claude Code's tmux calls into native splits. That rests on which tmux commands Claude Code runs, which it does not document, so it is left out.

*Since built the other way* (see "Built so far"): the commands were read off Claude Code itself, and Irie's settings file sets `"teammateMode": "tmux"` when `spawn.teammates` is `panes`.

### 6.6 Acting on a helper

| Action | How | When |
|---|---|---|
| Watch | This proposal | First |
| Pause one helper | M5's gate for `sub:<id>`: its next tool call is held | With M5 |
| Send it a note | M7's notes for `sub:<id>`, delivered as `additionalContext` at its next tool call, the one documented way in | With M7 |
| Type to it | In the terminal, through Claude Code's panel | Now, by hand |
| Stop it | Claude Code's panel (`x`) | Now, by hand. Irie adds no stop: holding or denying its calls is not stopping it |

## 7. What DESIGN would change

- **§2.2 and §2.3.** No `irie mirror` and no mirror windows. The slot holds the placeholder or one agent pane. `RoleMirror` goes.
- **§2.6.** No `internal/mirror`. `internal/transcript` is the reader `internal/core/conversation.go` already has.
- **§4.4** is rewritten as §6.1 to §6.3 here. `subagents.keep_finished` keeps a finished helper listed instead of a mirror window open.
- **§4.7.** A helper's conversation, `from: "parent"` on its task, and `agent` on the ops.
- **§6.3.** `subagentStatusLine` beside `statusLine`.
- **M8** becomes: helper records, the status line feed, helper conversations, the Mac app's split and the slot's split, child rows, delegated sessions under their parent. Per-helper pause and notes move to M5 and M7.
- **§10 decision 4** is closed as C, with A and B dropped.

## 8. Order of work

1. **The core** (medium). Helper records kept after they stop, and woken by a repeated start; internal agents left out; `irie statusline --subagents` and `status` with `tasks`; `conversation` and `subscribe` with `agent`; chips carrying their helper's id; `show` with `agent`. Tests with the fake agent, which names a subagent transcript today but does not write one: it writes the transcript and fires SubagentStart, two PreToolUse calls with that `agent_id` and SubagentStop. The helper's items stream, the finished helper stays, a SubagentStop with no start makes no helper, a second start revives the first record, a fake status line tick fills in the label, and the chip's helper id comes from the Agent result.
2. **The Mac app** (medium). A helper model beside `ConversationModel`, the split and its strip, opening from chips and lines, the narrow layout, children under their parent's card. `--render-conversation` gains a fixture with two helpers, one running and one done, wide and narrow.
3. **The terminal sidebar** (small to medium). Child rows, and the slot's split and keys.
4. **Agent teams** (small). `teammateMode` in the settings file, and its golden.
5. **Smoke test** (M11), against the real Claude Code. A session with a background subagent: when its transcript appears, that its lines parse, that the subagent status line ticks while the orchestrator idles, and that the hook, result, file and status line ids agree. A fork: where its transcript starts. A session with `teammateMode: auto`: that its teammates stay in-process inside Irie. Once M7 has notes: that a note given at a helper's tool call reaches the helper and not the session.

## 9. Risks

| Risk | Effect | Mitigation |
|---|---|---|
| The transcript format is Claude Code's own and changes between versions | A helper's pane misses lines | The reader skips what it does not understand; the status line and hooks still give the live line; the smoke test |
| A helper's transcript does not exist yet at SubagentStart | An empty pane | The pane shows hook events and the status line until the file appears, and retries (DESIGN §4.4) |
| A fork's transcript may begin with the conversation it inherited | The pane repeats the session's conversation | The pane starts at the fork's own task; the smoke test runs a fork |
| Which helper started which is read from transcripts | A nested helper's pill lacks its parent for a while | The strip is flat anyway; the pill gains "from deep" once the parent's transcript names it, and keeps its place |
| The status line ticks rarely while the orchestrator is idle | A stale label | `refreshInterval`; the hooks' newest tool call |
| Many helpers, up to 20 at once | A crowded strip | The strip scrolls; the sidebar shows three, then "and N more" |
| A user's own `subagentStatusLine` | Irie's would hide it | Irie runs theirs on the same input, as for `statusLine` |
| Opening the split in the terminal view | Every agent window resized | The terminal view never splits (§6.2) |

## 10. Decisions for the owner

| # | Decision | Options | Recommendation |
|---|---|---|---|
| 1 | How a helper shows | A, B, C or D (§5) | **C** |
| 2 | How many helper panes at once | a. One, with a strip of its siblings. b. Up to two side by side | **a** for the first cut. The journal never had more than three helpers at once, and a strip of three reads at a glance |
| 3 | Delegated sessions under their parent's card | a. Yes, as children. b. No, where they are | **a**, since only the user moves a card and a child is placed once, under its parent, when it starts |
| 4 | `teammateMode: in-process` in Irie's settings file | a. Yes. b. Leave the user's | **a**, because a teammate's pane would land where no view shows it |

## 11. Found on the way: a swap into the zoomed slot signals agents

DESIGN §2.3 promises that a swap never resizes an agent. While the Mac app is open the slot is zoomed, and `SwapIn` adds `-Z` to keep it zoomed (`keepZoom`, `internal/tmux/layout.go`). tmux 3.7b's `swap-pane` unzooms the window before it swaps, with or without `-Z`; `-Z` only zooms it again afterwards (`window_push_zoom` in `cmd-swap-pane.c`, `window_unzoom` in `window.c`). So inside the one command, both panes are resized to the unzoomed slot and back.

On a private test server, an agent pane swapped into a zoomed slot got a SIGWINCH, then read its old size. A TUI that reads its size after both resizes sees no change; one that reads between them draws twice. Every session switch in the Mac app therefore signals the two agents it moves. In the same test, an equal-size `swap-pane` in a window that was not zoomed resized nothing. A fix outside this proposal is to give the app a window that holds only the slot, so it needs no zoom.
