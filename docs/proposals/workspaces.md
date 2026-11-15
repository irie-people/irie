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
- **Spaces** (`spaces`, `space`, `ui.spaced`; `space` on rows, in `input`, `spawn` and `close`): chips under the list in the Mac app, each space with its own Pinned and Parked.
- Closing an orchestrator tab already ended its subs: helpers end with its agent, and its teammates' terminals end with it (internal/core/team.go).
- **Your groups** (`group`, `ui.grouped`, `@irie_groups`; `group` on rows, `groups` in the header; `:group [NAME]`, `:ungroup`; `irie ctl group`, `irie ctl spawn --group`), asked on 14 Nov as "an option on session tabs that is create a new group". Rebuilt on today's order rather than merging the 1 Nov work: a group has no entry in the order, and a sidebar draws it where the first of its cards is, which the core keeps together. **New Group** in a card's menu makes one and opens its name to edit; Move to Group and Remove from Group; drops in and out; a header that renames, folds, ungroups or closes; one undo per change. A pinned or parked card keeps its group, and joining a group unpins and unparks. Each space has its own groups, and a reopened session comes back in its group and space. An agent's new session goes in the agent's group, as asked on 1 Nov.
- **Groups inside groups** (`group` with `move` and `groups`, `parent` on each of the header's groups; `:nest [NAME]`; `irie ctl nest`; paths such as `Work/Irie` for `:group`, `irie ctl group` and `--group`), asked on 14 Nov as "add hierarchy and drag and drop groups into other groups and organize it like a mind file system". A group holds groups as a folder holds folders, eight deep at most, each drawn inside the one around it with its own folder and rail. Drag a header onto another header to put the group inside it, onto a header's top edge to put it just before, onto a card to put it in that card's place, or onto the strip under the list for the top. The header's menu puts a group in a new group, moves it into another or out of the one around it; Move to Group lists the groups as a tree. Names are unique beside each other, as in a folder. The core keeps each group's cards together as that tree draws them, and every move is one step that ⌘Z takes back.
- **The panes, as a rolodex** (2026-11-14, `mac/Sources/Irie/Rolodex.swift`, `RolodexBrowser.swift`; core op `pane`). The owner's picture: "within a session workspace you will have a term, then a split pane to the right, whether that's a term or a browser or a plugin pane", built "as a rolodex", with ⌥Tab and ⌥⇧Tab to walk it, starting at the terminal and going left to right, "fun and inventive". What it does:
  - **A wheel of cards** between the terminal (or the conversation) and the right sidebar, opened with View > Show Panes (⌥⌘P) or the toolbar's stack button. Each card has an index tab; the tabs stand left to right in the cards' order. Since 15 Nov (asked as "lets refine this irie pane rolladex thing, i want to revisit its design", the owner's pick of three directions: a calm tab row) they stand in one row level with the terminal's top, the front one joined to its card and kept wide enough for its name, the cards behind as thin edges above the front one's; the front card's foot is level with the terminal's and its pane fills it, with no rail, knob or side column, and the + and fold sit at the row's end.
  - **Kinds:** Terminal, a login shell of your own in the session's folder (the core's `pane` op runs it on the Dock's tmux server, so it never resizes an agent, outlives a restart and ends with the session; when it exits, its last screen stays with New Shell); Browser, an address bar over a page that takes an address, a port such as 3000, or words to search, keeps its page as the wheel turns and keeps no data after the app quits; Code (15 Nov, asked as "a new pane - IDE a light weight code editor ... a user can ask to pull up code and it would open a file in right pane ide ... their code syntax highlighting preference ... edit it manually and save automatically"), a native editor of the session's files in the code colours picked, saved as you type, that takes the agent's changes on disk as they land, and that `irie ctl open FILE[:LINE]` fills (MAC.md, Code cards); and a plugin's page, Markets while that plugin is on.
  - **⌥Tab** goes from the terminal to the first card, then to each next card, and back to the terminal after the last; **⌥⇧Tab** the other way. The wheel turns the short way round, so after the last card the first comes up next, one notch forward.
  - **Fun:** a turn flips the front card over its foot toward you, in 3D, while the next card comes up behind it and the cards behind settle forward on a spring; a turn back brings the card before up over it, landing with a small bounce. A click on a far tab spins the wheel there, each notch quicker. A scroll over the tabs turns the wheel a notch at a time, with a tick on a Force Touch trackpad (the rail and its knob went with the 15 Nov calm row). An empty wheel shows a blank index card, ruled, asking "What goes on this card?", whose kinds answer to T, B, C and M. With Reduce Motion on, a turn is one step.
  - **Per session:** each session keeps its own cards across launches. Whether the wheel shows, and its width (dragged at its edge, applied once on release), is one choice for every session, so moving between sessions resizes no agent; opening or closing it resizes them once, as the right sidebar does.
  - **Folding** (14 Nov, asked as "how do i minimize the right split pane"): View > Fold Panes (⇧⌥⌘P), the fold button beside the +, or a drag well past the wheel's narrowest folds the wheel down to a strip at the window's edge, its tabs standing on their side, the front one out. A click on a tab unfolds the wheel at that card; ⌥Tab, the strip's button or a drag out from it unfolds it too. Folding and unfolding resize the agents once each, and whether it is folded is one choice for every session.
  - **Over the terminal** (15 Nov, asked as "an overlap feature for the term rolodex, when they resize it and if it goes over what the min of the term i want it to overlap like windows, and then focus controls which lays on top"): beside the terminal the wheel still stops where the terminal would have less than its narrowest, about 60 columns. A drag on past that point holds there for 16 points, with a tick, then goes on over the terminal, up to leaving 120 points of it, and a band shows the part of the terminal the panes will cover. Let go there, and the terminal keeps its narrowest while the wheel lies over it like a window over another, on a ground of its own with a shadow on the terminal. Whichever has the keyboard lies on top: a card taking it (⌥Tab, a click in it, a tab) brings the wheel up, and the terminal or the message box taking it brings that up; while the keyboard is elsewhere, such as in the list, the last one stays on top. A click on what shows of the other brings it up, as a click on a window does, and on the wheel's own parts gives its front card the keyboard. Nothing changes size as they swap, so the agents never resize for it, and a drag from one width over the terminal to another resizes nothing of the terminal either. Over it, the wheel's tabs start where the terminal ends, so they show whichever is on top. The divider is the edge that shows between them, the wheel's or the terminal frame's, and a drag of it puts the wheel's edge where you let go: short of the terminal's narrowest, the wheel stands beside it again, and a double click puts back the first width beside it. Opening, closing or unfolding a wheel that lies under the terminal slides it under the terminal's edge, the terminal making room before it comes and taking it back after it goes. Going over the terminal is one choice for every session, kept across launches, as the width is.

## 4. Next

In this order, each shipped live on its own:

1. **Your groups** (built 14 Nov, above, groups inside groups too). Left for later: a colour per group, joining and leaving groups by drag in the terminal sidebar (it has `:group`, `:ungroup` and `:nest`), Move Group to Space in the header's menu (a header dropped on a space's chip moves it now), and the Move keys stepping over a group inside a group as one. The 1 Nov work stays on branch `irie/tab-groups-wip` (`f3d7645`) for reference.
2. **Spaces** (built 9 Nov; the terminal sidebar still shows every space). A space is a named set of cards with its own groups and Parked. The core keeps `space` on each row and a list of spaces (`@irie_spaces`); a new session takes the space it was started from. The Mac app draws chips under the list, ⌃1–9 switches, ⌃⌥1–9 sends the selected card, and a card dragged onto a chip moves there. The title bar's counts stay global; a red dot on a chip is for later. The terminal sidebar shows one space at a time, with `[` and `]` to switch.
3. **The workspace pane.** A split beside the chat or terminal, per card, with tabs: Writing (an editor on a Markdown file in the card's folder, where the agent's edits come as suggestions you accept or reject), Browser (WKWebView, as the Dock's pages), Files (open and edit any file in the folder, the agent's changes marked), and your own Terminal (a shell in the folder, apart from the agent's). Variorum stays its own app (it needs macOS 26), reached through its door (`:write`). **Built 2026-11-14 as the panes' rolodex (§3)**, with Terminal, Browser and Markets cards; Files came on 15 Nov as the Code card. Writing, with the agent's edits as suggestions, is its next kind, and cards of a session on another machine (its shell and files there) after it.

## 5. Open questions

- Should Close All Finished take a space or the whole list, once spaces exist? The plan: the space you're in.
- Should a pinned card be pinned in every space, or in its own? The plan: its own.
- Suggesting mode needs a way for the agent to propose edits to a file the pane has open: a tool of Irie's own (an MCP server, as the browser's) that the pane draws as suggestions. Its shape is the first step of item 3.
- Spaces could come before groups: they stand alone (a `space` on each row, chips, ⌃1–9), and groups then live inside a space. Which goes first is the owner's call.
