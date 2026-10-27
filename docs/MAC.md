# Irie for Mac

Date: 2026-10-26. Status: first build. Read with [DESIGN.md](DESIGN.md).

Irie for Mac is a native app for the people who would never open a terminal, and it stays useful for developers. It is a SwiftUI front end to the same core as the terminal sidebar. The core still runs every agent in tmux, so agents keep working when the app quits. Linux, and anyone who prefers the terminal, keeps the terminal sidebar.

## 1. What it shows

- **A card per session**, in the order the sessions started. As in the terminal sidebar, a state change never reorders the list (DESIGN §4.1); it only changes the card's mark and colours. Each card is rounded and edged in its state's colour: red, blue or green, or a warm hairline once it is quiet. The header counts how many need you, are working or are done. Each count jumps to the first such session.
- **Cards that never move.** Every card is two lines: the name, then what the session needs from you (in red, on a red tint), what it is doing, or how it ended. Its age and token count sit in a trailing slot of fixed width. Hovering swaps that slot for quick actions, so the name never re-wraps and no card changes height under the pointer.
- **A details panel**, a card of fixed height above the task bar, for the selected session: its state, current step, profile and branch, folder, tokens, helpers, warnings, and the last screen of a spawn that went wrong. Long lines are cut off, and the full text shows on hover.
- **Helpers.** A session's running subagents show as a count on its card and as a line each in the panel, red when one needs you. The core reports them in each row's `subagents`, kept from the agent's SubagentStart and SubagentStop hooks.
- **Tokens per session.** Each card shows the session's token count, and the panel splits it into input, output, cache read and cache write. The core reads each row's `tokens` from the session's transcript and its finished subagents' transcripts. It counts each reply once, because a reply spans several lines that all repeat its usage.
- **Accessibility.** VoiceOver reads each card as one element: name, state, what it needs or does, when it started ("started 3 minutes ago") and its tokens. The card's actions offer what the hover buttons and the right-click menu do. Notices are announced, and errors stay until the next notice or until you edit or submit. Text colours keep 4.5:1 contrast, with stronger ones for Increase Contrast. The selected card shows a full fill and sun edge while the list has the keyboard, and a faint one otherwise.
- **The shown agent's live terminal** on the right, in a rounded frame edged in that session's colour, or sun while it is quiet. The edge brightens while the terminal has the keyboard. Selecting a row swaps that agent in, and Return (or **Type to it**) moves the keyboard there.
- **A task bar** at the foot of the sidebar, always three lines tall; longer text scrolls inside it. It takes the same `@repo`, `@repo!` and `+profile` words as the terminal sidebar (DESIGN §4.2). It keeps the keyboard while a task starts; once the task has started, the app shows the new agent and gives its terminal the keyboard. Notices float above it rather than pushing the list.
- **Warm, solid colours:** sand in light mode and cocoa in dark mode, with the terminal sidebar's sun, sea and state colours (`Theme.swift`). Nothing behind the window shows through.
- **Right-click on a row:** show and type to it, open it in Ghostty, show the folder in Finder, copy the folder path, branch name or session ID.
- **Outside Irie.** Claude Code sessions that run in other terminals or in the background follow the cards, under a header with their count (DESIGN §4.6). Each is one line, in the order it started, with the mark of the state it matches, so a state change never moves or resizes it. A click or the arrow keys select one, and the details panel shows what it is doing, where it runs and its folder, with **Move into Irie** and **Copy Session ID**. While the session is busy or waits for you, the button is off and the panel says why. A move asks first, because the session's copy outside Irie ends, and anything it runs in the background stops. Then the conversation carries on in an Irie pane, and its terminal takes the keyboard. Right-click a row for the same, and to show or copy its folder. The keys that show a session skip these rows, since Irie can't show them.

## 2. How the terminal gets into the window

The app adds no state of its own to the core. It uses two additions to the socket API (DESIGN §2.4):

- `subscribe` with `view: "app"`. While at least one such subscription is open, the core keeps the slot zoomed (`resize-pane -Z`), so the slot fills `irie:0`. Swaps keep the zoom (`swap-pane -Z`, `select-pane -Z`). Agent windows take the zoomed slot's size, so a swap still never resizes an agent. When the last app view closes, the zoom ends and the sidebar is back.
- `attach-info`, which returns the tmux binary, the server's socket path and the session to attach to.

The app runs `tmux -S <socket> attach-session -t irie` inside its terminal view. Because the slot is zoomed, that client shows only the shown agent, and `show` swaps agents in place.

Two details of the generated tmux.conf follow from this. The `window-resized` hook leaves a zoomed window alone, because resizing the sidebar would end the zoom. In a terminal attached to the zoomed slot, the focus key goes to the agent, because there is no sidebar pane to move to. The app catches Ctrl+] itself (§4), so from the app it never reaches tmux. While the app is open, a terminal attached with `irie attach` also sees only the slot.

## 3. Build and run

`mac/build.sh` builds `mac/build/Irie.app`. It calls `swiftc` directly because SwiftPM 5.9, the version in the Command Line Tools for macOS 14.1, stops at `unable to lookup item 'PlatformPath'`. `tools/live.sh` (AGENTS.md) runs it when `mac/` changed, installs the app in `~/Applications` and relaunches it if it was running.

The terminal view is [SwiftTerm](https://github.com/migueldeicaza/SwiftTerm) v1.18.0 (MIT), the newest release whose manifest builds with Swift 5.9. The script fetches it, refuses the download unless its SHA-256 matches, and compiles it once into a static library under `mac/.build`. SwiftTerm draws with Metal: its renderer compiles `Shaders.metal`, which the script copies into the app, when the app runs, so no Xcode is needed. Where Metal is missing it draws with Core Graphics.

`IRIE_MAC_NO_SWIFTTERM=1 mac/build.sh` builds with Apple's frameworks only. The terminal then opens in a Ghostty window that follows the selected session. Where SwiftPM works (Xcode, or newer Command Line Tools), `mac/Package.swift` builds the same app.

The app finds the core the way `irie` does (`internal/paths`). `--socket PATH`, or `IRIE_CORE_SOCKET`, points it at another Irie. It needs a core with `attach-info`; an older one gets a message to run the new build's `irie up`.

## 4. Keys

The **Go**, **View** and **Edit** menus list them all.

| Keys | What they do |
|---|---|
| Ctrl+Tab, Ctrl+Shift+Tab | Show the next or the previous session in the sidebar's order: the order they started, without the rows the filter hides, wrapping at the ends. The keyboard stays where it was. The app catches these keys before the terminal or AppKit's key-view loop sees them |
| Ctrl+] | Move the keyboard between the session list and the terminal |
| ⌘1 to ⌘9 | Show the first to ninth session in the sidebar's order |
| ⌘F | Find a session. Esc clears the search and goes back to the list |
| ⌘N | New task |
| ↑ and ↓, then Return | Pick a session in the list, then type to it. The arrows go on into Outside Irie, where they only select a row, and Return says that it runs outside Irie |
| ⌘C, ⌘V, ⌘A | Copy, paste and select all in the terminal. A mouse drag selects in tmux, which puts the text on the pasteboard when you let go (tmux's `set-clipboard external` and OSC 52, which SwiftTerm writes to the pasteboard). ⌘C copies a selection made with Shift-drag, which bypasses tmux |
| ⌘=, ⌘-, ⌘0 | Make the terminal's text bigger, smaller or the default size |

## 5. Not yet

These come with the core features they need, or after:

- pause, freeze, rename, park, close, undo and priority controls (M4 to M7);
- buttons that answer a question or approve a command;
- choosing a local model for small jobs;
- an icon, and a signed, notarized app.
