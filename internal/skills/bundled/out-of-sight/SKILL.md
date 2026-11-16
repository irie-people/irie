---
name: out-of-sight
description: The user's hard rule for windows, when testing Irie or anything else. Read it before you run anything that may open a window on their Mac (an app, a preview, a UI test, a browser that is not headless, a simulator, open(1)). No window may land on the desktop the user is on or in front of what they look at; it says how to work without one, and how irie away opens one on a desktop they are not looking at.
license: Apache-2.0
---

# Out of sight

The user's hard rule: **a window you open never appears on the desktop they are on, and never in front of what they look at.** Not for a moment, not behind the app in front, not just to check. It holds when you test Irie and when you test anything else. Gabriel turns away the calls he can read that break it, but most windows come from scripts and tests he cannot see, so keeping it is yours.

## 1. Work without a window

Most checks need none. Reach for these first:

- **The Mac app's views:** `mac/snapshot.sh DIR`, or Irie.app's `--snapshot DIR`, `--render-conversation DIR` and `--render-onboarding DIR`, which draw out of sight and exit.
- **Web pages:** a browser headless (`--headless=new` keeps a profile and its logins), Playwright or Puppeteer headless, pages opened in the background with no `bringToFront`, or Irie's browser tools, which run without a window.
- **A window that is already open,** such as the user's Irie.app: `screencapture -x -o -l WINDOW_ID FILE` takes a picture of that one window, wherever it is, with no flash.
- **Test runners:** their headless mode, never `--headed`.

## 2. A window you must open goes through irie away

When the check needs a real window, run the command through `"$IRIE_BIN" away`:

```sh
"$IRIE_BIN" away -- open -g -n -a "Google Chrome" --args --user-data-dir=/tmp/test-profile http://localhost:3000
"$IRIE_BIN" away --for 30s --app Simulator -- xcrun simctl boot "iPhone 15"
"$IRIE_BIN" away -- python3 app.py
```

It runs the command. Each window of a process the command starts, of an app it opens with `open`, or of an app named with `--app` goes to a desktop the user is not looking at, while the command runs and for `--for` after it ends (10 s). An app the command started that cannot move is hidden instead. It says on standard error where each window went, such as desktop 2, and exits with the command's status, or with 3 when a window stayed in sight: close that window at once.

- Inside it, open apps with `open -g`, never in front.
- For pages, use a browser of your own (`-n` and a `--user-data-dir` of its own), never a tab in the user's browser: a new tab shows in their window, on their desktop.
- Give an app that starts slowly, or that the command starts some other way, `--app NAME` and `--for 30s`.
- If the user should look, tell them in one line which desktop the window is on. Close it when you are done.

## 3. Irie's own tools keep it

- `tools/hot.sh` opens Irie Preview on a desktop the user is not looking at (`--away`), and each new build takes over the window of the one before where it is.
- `tools/live.sh` and the freeze dog open Irie.app again with `--background`, which puts its window back on the desktop it was on.
- `--snapshot` and `--bench` draw in a see-through window on another desktop. `--record` films on another desktop, and does not film at all when the user can see every desktop.

## 4. Never

- `open` without `-g`, which brings the app to the front, or `open -g` outside `irie away`, which opens it on the user's desktop, behind.
- AppleScript that activates an app or types into one, `NSApp.activate`, `bringToFront`, a `--headed` run.
- An app's executable started straight, short of a headless or out-of-sight flag such as `--headless` or `--snapshot`.
- A server in a test or a scratch run that listens on every address, such as `:8080` or `0.0.0.0`. With the macOS firewall on, each new program that does brings up a dialog asking whether it may accept incoming connections, and every ad hoc build, such as each `go build`, is a new program to it. Listen on 127.0.0.1, which it asks nothing about.
- Moving, hiding or closing the user's own windows. `irie away` leaves alone every window that was open before it ran, and an app the user has in front.

If the user must see something, say so in one line and let them open it.
