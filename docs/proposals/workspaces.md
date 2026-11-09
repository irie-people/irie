# Tabs, groups, spaces and a workspace pane

Date: 2026-11-09. Status: decisions, what is built, and the plan for the rest. An agent (Claude Code) wrote it with the owner, who asked for "different options with closing sub tabs or tabs, and moving tabs and workspaces to be customizable to user" and wanted to run the ideas and decisions together.

**In short.** The sidebar's cards (tabs) close and move your way: pin a card to the top, close several at once with one undo, pick cards with ⌘ and ⇧, close from under the pointer, move a card with the keys, and choose where new cards go. Next come your own groups (the 1 Nov tab groups), switchable spaces that each hold their own groups and Parked, and a workspace pane beside each agent where you type, edit and browse while the agent helps.

| | Before | Now |
|---|---|---|
| Close several cards | One at a time, one undo each | Close Others, Close Below, Close All Finished, a group's Close, or pick with ⌘/⇧ and ⌘W; one ⌘Z brings them all back |
| Keep a card at hand | Promote (which also gives it the machine first) | Pin to the Top: a Pinned group, which no bulk close or close-when-done reaches |
| Close from the mouse | The card's menu | A × in place of the mark on hover, and a middle click |
| Move a card | Drag, Move Up and Down in its menu | Also ⌥⌘↑/↓, and ⇧⌥⌘↑/↓ to its group's top or bottom |
| Where a new card goes | Last | Settings › Tabs (`ui.new_tabs`): last, first, or next to the card that started it |

---

## 1. The words

The owner's words, as the app uses them:

- **Tab**: a session's card in the sidebar (an "orchestrator tab").
- **Sub tab**: what hangs under a card: its helpers (subagents) and teammates ("subs"), and, with the workspace pane, the pane's own tabs.
- **Workspace**: both of these, the owner said: groups of cards in the one list, and spaces you switch between. And, from the owner's own picture, the pane beside an agent where you work yourself.

## 2. Decisions (2026-11-09)

| Question | The owner's pick |
|---|---|
| What a workspace is | Both: tab groups in the one list, and switchable spaces. "Leave for now" a red dot on a space whose hidden card needs you; the title bar's counts still count every space |
| What a space holds | Its own groups and its own Parked; a new task starts in the space you're in |
| Closing tabs | Close many (Others, Below, All Finished, a group's header), × on hover, select several; and closing an orchestrator tab closes all its subs. Not picked: asking before closing a busy card |
| Closing sub tabs | × hides one from the card, and it keeps working; right-click › Stop ends it |
| Moving tabs | Keys, where new tabs go, pin to the top, ⌃⌥1–9 to send a card to a space, and drag and drop everywhere |
| The workspace pane | A split beside the chat or terminal, with a divider you drag either way |
| The pane's sub tabs | Writing, Browser, Files and your own Terminal |
| The agent and your pane | It suggests and you accept, like a document's suggesting mode; it never types into your text unasked |
| Budget | Up to 85% of the week's plan (it was at 74%) |

## 3. Built

On `irie/can-we-do-some-different`, live once installed:

- **Pinned** (core `pin`, `unpin`; `ui.pinned`, `ui.unpinned`; `irie ctl pin`; `:pin`): Parked's opposite, at the top in both sidebars. Pinning a parked card unparks it, parking a pinned one unpins it, and undo puts both back.
- **Closing several** (`close` with `sessions` or `finished`): one batch in the history, which one undo or redo takes (`close 6 sessions`). `:close others|below|finished` for the text clients.
- **New tabs** (`ui.new_tabs` = last, first or next; `after` in `spawn`, from `IRIE_SESSION` for `irie ctl spawn` and from the selected card for a typed task).
- **The Mac app**: the card's and the group header's menus, File's items and Move Session keys, ⌘/⇧ picking, the hover × and middle click, Settings › Tabs.
- **Sub tabs**: a × on a helper's or teammate's row hides it from the card while it keeps working, and "2 hidden · Show" brings it back; Stop in its menu (`stop_sub`, `ui.sub_stopped`) turns a helper's tool calls away with word to end, or ends a teammate's terminal.
- Closing an orchestrator tab already ended its subs: helpers end with its agent, and its teammates' terminals end with it (internal/core/team.go).

## 4. Next

In this order, each shipped live on its own:

1. **Your groups.** The 1 Nov work, which the tab-groups card left uncommitted in `~/.local/share/irie/worktrees/irie/tab-groups`, is kept whole as commit `f3d7645` on branch `irie/tab-groups-wip`: groups as entries of the one order, `@irie_groups`, `irie ctl group`, `spawn --group`, menus and drags in both sidebars. A trial merge onto this branch conflicts in 21 files (59 places), and beyond the text it predates undoable reorders, today's park (which moves a card to the end of the order), the On Asahi group, the core's timed lock and pins. So it is a rework, not a merge: take its design and its sidebar pieces, and fit them to today's order, with a group's own Close Group and Pinned above every group. Start it with a fresh week's budget, so it lands whole rather than half done again.
2. **Spaces.** A space is a named set of cards with its own groups and Parked. The core keeps `space` on each row and a list of spaces (`@irie_spaces`); a new session takes the space it was started from. The Mac app draws chips under the list, ⌃1–9 switches, ⌃⌥1–9 sends the selected card, and a card dragged onto a chip moves there. The title bar's counts stay global; a red dot on a chip is for later. The terminal sidebar shows one space at a time, with `[` and `]` to switch.
3. **The workspace pane.** A split beside the chat or terminal, per card, with tabs: Writing (an editor on a Markdown file in the card's folder, where the agent's edits come as suggestions you accept or reject), Browser (WKWebView, as the Dock's pages), Files (open and edit any file in the folder, the agent's changes marked), and your own Terminal (a shell in the folder, apart from the agent's). Variorum stays its own app (it needs macOS 26), reached through its door (`:write`).

## 5. Open questions

- Should Close All Finished take a space or the whole list, once spaces exist? The plan: the space you're in.
- Should a pinned card be pinned in every space, or in its own? The plan: its own.
- Suggesting mode needs a way for the agent to propose edits to a file the pane has open: a tool of Irie's own (an MCP server, as the browser's) that the pane draws as suggestions. Its shape is the first step of item 3.
- Spaces could come before groups: they stand alone (a `space` on each row, chips, ⌃1–9), and groups then live inside a space. Which goes first is the owner's call.
