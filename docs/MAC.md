# Irie for Mac

Date: 2026-10-26. Status: first build. Read with [DESIGN.md](DESIGN.md).

Irie for Mac is a native app for the people who would never open a terminal, and it stays useful for developers. It is a SwiftUI front end to the same core as the terminal sidebar. The core still runs every agent in tmux, so agents keep working when the app quits. Linux, and anyone who prefers the terminal, keeps the terminal sidebar.

## 1. What it shows

- **A sidebar of sessions** grouped into Needs you, Working and Done, with counts in the header. Rows are one line. A session that needs you adds what it asks, in red. The selected row opens up to show its current step, profile, branch, folder, warnings and the last screen of a spawn that went wrong.
- **The shown agent's live terminal** on the right. Selecting a row swaps that agent in, and Return (or **Type to it**) moves the keyboard there.
- **A task bar** at the foot of the sidebar. It takes the same `@repo`, `@repo!` and `+profile` words as the terminal sidebar (DESIGN §4.2).
- **Right-click on a row:** show and type to it, open it in Ghostty, show the folder in Finder, copy the folder path, branch name or session ID.

## 2. How the terminal gets into the window

The app adds no state of its own to the core. It uses two additions to the socket API (DESIGN §2.4):

- `subscribe` with `view: "app"`. While at least one such subscription is open, the core keeps the slot zoomed (`resize-pane -Z`), so the slot fills `irie:0`. Swaps keep the zoom (`swap-pane -Z`, `select-pane -Z`). Agent windows take the zoomed slot's size, so a swap still never resizes an agent. When the last app view closes, the zoom ends and the sidebar is back.
- `attach-info`, which returns the tmux binary, the server's socket path and the session to attach to.

The app runs `tmux -S <socket> attach-session -t irie` inside its terminal view. Because the slot is zoomed, that client shows only the shown agent, and `show` swaps agents in place.

Two details of the generated tmux.conf follow from this. The `window-resized` hook leaves a zoomed window alone, because resizing the sidebar would end the zoom. The focus key goes to the agent while the slot is zoomed, because the app has no sidebar pane to move to. While the app is open, a terminal attached with `irie attach` also sees only the slot.

## 3. Build and run

`mac/build.sh` builds `mac/build/Irie.app` with Apple's frameworks only. It calls `swiftc` directly because SwiftPM 5.9, the version in the Command Line Tools for macOS 14.1, stops at `unable to lookup item 'PlatformPath'`. In this build the terminal opens in a Ghostty window, which follows the selected session.

`mac/Package.swift` adds [SwiftTerm](https://github.com/migueldeicaza/SwiftTerm) v1.18.0 (MIT), the newest release whose manifest builds with Swift 5.9, and draws the terminal in the window. It needs a toolchain whose SwiftPM works: Xcode, or newer Command Line Tools. `IRIE_MAC_NO_SWIFTTERM=1` builds the package without it.

The app finds the core the way `irie` does (`internal/paths`). `--socket PATH`, or `IRIE_CORE_SOCKET`, points it at another Irie. It needs a core with `attach-info`; an older one gets a message to run the new build's `irie up`.

## 4. Not yet

These come with the core features they need, or after:

- pause, freeze, rename, park, close, undo and priority controls (M4 to M7);
- buttons that answer a question or approve a command;
- choosing a local model for small jobs;
- an icon, and a signed, notarized app.
