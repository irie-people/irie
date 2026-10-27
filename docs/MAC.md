# Irie for Mac

Date: 2026-10-26. Status: first build. Read with [DESIGN.md](DESIGN.md).

Irie for Mac is a native app for the people who would never open a terminal, and it stays useful for developers. It is a SwiftUI front end to the same core as the terminal sidebar. The core still runs every agent in tmux, so agents keep working when the app quits. Linux, and anyone who prefers the terminal, keeps the terminal sidebar.

## 1. What it shows

- **A sidebar of sessions** grouped into Needs you, Working and Done, with counts in the header. Rows are one line. A session that needs you adds what it asks, in red. The selected row opens up to show its current step, profile, branch, folder, warnings and the last screen of a spawn that went wrong.
- **Helpers under their session.** A session's running subagents are listed under its card, each with its kind and current tool, and in red when it needs you. The core reports them in each row's `subagents`, kept from the agent's SubagentStart and SubagentStop hooks.
- **Tokens per session.** Each card shows the session's token count beside its age; hovering, or selecting the card, gives the split into input, output, cache read and cache write. The core reads each row's `tokens` from the session's transcript and its finished subagents' transcripts. It counts each reply once, because a reply spans several lines that all repeat its usage.
- **The shown agent's live terminal** on the right. Selecting a row swaps that agent in, and Return (or **Type to it**) moves the keyboard there.
- **A task bar** at the foot of the sidebar. It takes the same `@repo`, `@repo!` and `+profile` words as the terminal sidebar (DESIGN §4.2).
- **Right-click on a row:** show and type to it, open it in Ghostty, show the folder in Finder, copy the folder path, branch name or session ID.

## 2. How the terminal gets into the window

The app adds no state of its own to the core. It uses two additions to the socket API (DESIGN §2.4):

- `subscribe` with `view: "app"`. While at least one such subscription is open, the core keeps the slot zoomed (`resize-pane -Z`), so the slot fills `irie:0`. Swaps keep the zoom (`swap-pane -Z`, `select-pane -Z`). Agent windows take the zoomed slot's size, so a swap still never resizes an agent. When the last app view closes, the zoom ends and the sidebar is back.
- `attach-info`, which returns the tmux binary, the server's socket path and the session to attach to.

The app runs `tmux -S <socket> attach-session -t irie` inside its terminal view. Because the slot is zoomed, that client shows only the shown agent, and `show` swaps agents in place.

Two details of the generated tmux.conf follow from this. The `window-resized` hook leaves a zoomed window alone, because resizing the sidebar would end the zoom. In a terminal attached to the zoomed slot, the focus key goes to the agent, because there is no sidebar pane to move to. The app catches Ctrl+] itself (§4), so from the app it never reaches tmux. While the app is open, a terminal attached with `irie attach` also sees only the slot.

## 3. Build and run

`mac/build.sh` builds `mac/build/Irie.app`. It calls `swiftc` directly because SwiftPM 5.9, the version in the Command Line Tools for macOS 14.1, stops at `unable to lookup item 'PlatformPath'`.

The terminal view is [SwiftTerm](https://github.com/migueldeicaza/SwiftTerm) v1.18.0 (MIT), the newest release whose manifest builds with Swift 5.9. The script fetches it, refuses the download unless its SHA-256 matches, and compiles it once into a static library under `mac/.build`. SwiftTerm draws with Metal: its renderer compiles `Shaders.metal`, which the script copies into the app, when the app runs, so no Xcode is needed. Where Metal is missing it draws with Core Graphics.

`IRIE_MAC_NO_SWIFTTERM=1 mac/build.sh` builds with Apple's frameworks only. The terminal then opens in a Ghostty window that follows the selected session. Where SwiftPM works (Xcode, or newer Command Line Tools), `mac/Package.swift` builds the same app.

The app finds the core the way `irie` does (`internal/paths`). `--socket PATH`, or `IRIE_CORE_SOCKET`, points it at another Irie. It needs a core with `attach-info`; an older one gets a message to run the new build's `irie up`.

## 4. Keys

The **Go**, **View** and **Edit** menus list them all.

| Keys | What they do |
|---|---|
| Ctrl+Tab, Ctrl+Shift+Tab | Show the next or the previous session in the sidebar's order: by group, without collapsed groups or rows the filter hides, wrapping at the ends. The keyboard stays where it was. The app catches these keys before the terminal or AppKit's key-view loop sees them |
| Ctrl+] | Move the keyboard between the session list and the terminal |
| ⌘1 to ⌘9 | Show the first to ninth session in the sidebar's order |
| ⌘F | Find a session. Esc clears the search and goes back to the list |
| ⌘N | New task |
| ↑ and ↓, then Return | Pick a session in the list, then type to it |
| ⌘C, ⌘V, ⌘A | Copy, paste and select all in the terminal. A mouse drag selects in tmux, which puts the text on the pasteboard when you let go (tmux's `set-clipboard external` and OSC 52, which SwiftTerm writes to the pasteboard). ⌘C copies a selection made with Shift-drag, which bypasses tmux |
| ⌘=, ⌘-, ⌘0 | Make the terminal's text bigger, smaller or the default size |

## 5. Not yet

These come with the core features they need, or after:

- pause, freeze, rename, park, close, undo and priority controls (M4 to M7);
- buttons that answer a question or approve a command;
- choosing a local model for small jobs;
- an icon, and a signed, notarized app.
