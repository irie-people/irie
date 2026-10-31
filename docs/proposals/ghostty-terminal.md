# Proposal: Ghostty as the app's terminal

Date: 2026-10-28. Status: steps 0 to 3 built on 2026-10-31, with Ghostty's split keys for a session and its teammates (see "Built so far"); step 4, splits for any pane, and step 5 are not. It answers the owner's request "research and plan using ghostty instead as in app term so we can get split pane easier". An agent (Claude Code) wrote it, from `live` at `d30fbe0`. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

**In short.** The Mac app can draw its terminals with libghostty, Ghostty's own engine, instead of SwiftTerm: Ghostty's renderer, fonts and input, the owner's Ghostty config, and Ghostty's split keys, which reach the app as actions. It does not by itself make splits easier, because what makes a split hard in Irie is tmux, not the terminal widget: every pane the app shows is a tmux client of one target, and the slot's size is every agent's size. Splits did get easy for agent teams in `d30fbe0`, on SwiftTerm, by giving each pane a target of its own (a view session, or a teammate's session). The plan is therefore two tracks that meet: swap the engine for libghostty 1.3.1, pinned, behind a setting with SwiftTerm as the fallback; and make the split general, a tree of panes where each leaf is any session, teammate or shell, driven by Ghostty's split actions. The step that makes splits truly easy, one tmux control-mode connection whose layout the app mirrors, waits for upstream Ghostty.

**Built so far** (2026-10-31, at the owner's request "make the ghostty term split integration in irie"):
- **The spike** passed on this Mac, with two findings. Zig's own fetch stalled on Ghostty's dependency mirror (the tarballs came back unread), while curl fetched every one in seconds, so the build fetches them with curl and files them with `zig fetch`. And the macOS 14 SDK in Xcode 15.0.1 lacks one constant Ghostty declares and never uses, `kCVPixelFormatType_30RGB_r210`, which the build replaces with its value when the SDK lacks it. The rest of 1.3.1 builds with Xcode 15 as it is.
- **The build**: `mac/ghostty.sh`, cached as `ghostty-1.3.1-<arch>`, linked by `mac/build.sh`, with terminfo and shell integration in the app's resources. A first build takes a few minutes, and the app grows from 7.7 MB to about 21 MB. Without Xcode or a network the app builds with SwiftTerm alone.
- **The view**: `GhosttyTerminal.swift`. Its surface's command is the same tmux client. Two things differ from what §4.2 expected: on macOS Ghostty runs a command as `exec -l <command>` in a login bash, so the command is a single `env … sh -c '…'`; and a surface starts at 800x600 before its size is set, so the client starts a tenth of a second later. An offscreen harness against a private tmux server checked that tmux's client always has the terminal's grid, at attach, after a resize and after a reattach (tmux never saw another size), that typing and Control-C reach the program, and that a client that ends attaches again.
- **The setting**: Settings > Appearance > Terminal drawn by, Ghostty when this build has it.
- **The split**: Ghostty draws the session and its teammates too, and Ghostty's split keys work there: `goto_split` moves the keyboard between the panes, `resize_split` and `equalize_splits` move the bar, and `toggle_split_zoom` makes one pane fill the split, by laying it over the others, so no terminal ends for a zoom. `new_split` does nothing yet: it waits for splits for any pane (§4.4).

| | Before (SwiftTerm) | With libghostty |
|---|---|---|
| Renderer | SwiftTerm's Metal view, shaders compiled at run time | Ghostty's Metal renderer, the one the owner's Ghostty runs |
| Look | Irie's colours and the app's font size | The owner's Ghostty config (font, theme, cursor), with Irie's ground and size on top |
| A pane | A SwiftTerm view running `tmux attach-session -t <target>` | A Ghostty surface running the same command |
| Split keys | None in the terminal | Ghostty's (⌘D, ⇧⌘D, ⌥⌘arrows, ⌃⌘arrows, ⇧⌘↩) arrive as actions the app carries out |
| Split layout | `TeamSplit.swift`: the session left, teammates in a column | A split tree adapted from Ghostty's own (MIT), any leaf |
| tmux side | One client per pane; view sessions; the team server | Unchanged |
| Build | `swiftc` and SwiftTerm's source, cached | Plus GhosttyKit, built once from a pinned tag with Zig 0.15.2 and Xcode, cached |
| Binary | About 7.7 MB (the installed app) | Larger: GhosttyKit's static archive is about 141 MB before dead-stripping (Calyx's figure) |

---

## 1. What makes a split hard in Irie

A split is several terminals side by side, each showing a different program. In cmux every pane runs its own shell, so a split is just another terminal view. Irie's agents live in tmux, so that a restart of the app or the core never touches them (DESIGN §2.1, principle 6), and a terminal view can only show one by being a tmux client:

- **One window per client.** A tmux client shows one window of one session. A pane the app shows on its own must be alone in a window and current in a session that this client alone attaches to. `d30fbe0` built that twice: the view session `irie-view` links the shown agent's window alone, and each teammate is a session of its own on the team server.
- **The slot's size is every agent's.** The main view is a client of the zoomed slot. Agent windows are kept at the slot's size, so a swap never resizes an agent. A split that narrowed the main view would resize every agent, so the split shows its session through the view session instead, and only that agent takes the split's size.
- **Who makes the pane.** Teammates' panes are made by Claude Code through the tmux shim; anything else (a second session, a shell) has to be made by the core.

None of these change with the widget. A Ghostty surface, like a SwiftTerm view, runs a command in a pty, and the command is the same tmux client.

## 2. libghostty, as of 1.3.1

**What it is.** Ghostty's core (Zig) built as `GhosttyKit.xcframework`: a C API in `include/ghostty.h` (1,178 lines at v1.3.1) for an app (`ghostty_app_new`, `ghostty_app_tick`), its config (`ghostty_config_*`), and surfaces (`ghostty_surface_new` with a host `NSView`, then key, text, preedit, mouse, scroll, pressure, focus, size and scale calls). A surface's config carries its `command`, `working_directory`, `env_vars`, `font_size`, `wait_after_command` and context (window, tab or split). The engine reports through six runtime callbacks (wakeup, action, clipboard read, confirm and write, close surface), and 65 action tags, five of them for splits: `NEW_SPLIT`, `GOTO_SPLIT`, `RESIZE_SPLIT`, `EQUALIZE_SPLITS`, `TOGGLE_SPLIT_ZOOM`. libghostty draws a surface; laying out splits is the host app's work, as Ghostty's own app does in Swift (`macos/Sources/Features/Splits`, MIT).

**Status.** The embedding API is used by Ghostty's own macOS app and is not yet a general-purpose API; breaking changes are expected, and releases ship no xcframework. Others embed it anyway: cmux (on a fork, with its own split library), Calyx, and several SwiftPM wrappers that publish prebuilt frameworks. What upstream offers for general use is libghostty-vt, a terminal-state library with no renderer, which does not fit a GPU view.

**Building it.**

| Item | Fact |
|---|---|
| Zig | 1.3.x builds only with Zig 0.15.2 (`minimum_zig_version` in `build.zig.zon`). A downstream project reports `main` moving to Zig 0.16 in July 2026, so a later pin may need another Zig |
| Command | `zig build -Demit-xcframework=true -Dxcframework-target=native -Demit-macos-app=false -Doptimize=ReleaseFast` |
| Xcode | Required for the SDK, `xcodebuild -create-xcframework` and the Metal compiler; `xcode-select` must point at it (or `DEVELOPER_DIR`). Xcode 26 dropped `metallib`, which the build expects (a two-line shim fixes it); Zig 0.15.2 cannot link the macOS 26.4+ SDK |
| Optimise | ReleaseFast: debug builds are over 100x slower, and one embedder saw heavy output crawl without it |
| Output | arm64 only with `native`; a static archive of about 141 MB |
| Resources | `terminfo` (`xterm-ghostty`) and `shell-integration` must ship where libghostty finds them (`GHOSTTY_RESOURCES_DIR`), or TERM falls back to `xterm-256color` and programs lose synchronized output and styled underlines |
| Licence | MIT (Ghostty and its Swift app code) |

**This Mac.** macOS 14.1 on arm64. `/Applications/Xcode.app` is Xcode 15.0.1 with the Metal compiler and `xcodebuild`, but `xcode-select` points at the Command Line Tools, so the build would run with `DEVELOPER_DIR` set. The app itself builds with the Command Line Tools' Swift 5.9. Zig is not installed. 14.7 GB of disk is free; a build takes a few GB of Zig cache. Whether Ghostty 1.3.1's Zig code builds against Xcode 15's macOS 14 SDK is the open question: the spike was readied (Zig 0.15.2 from ziglang.org with its published SHA-256, Ghostty's v1.3.1 source) and then stopped, because running a downloaded build here needs the owner's go-ahead.

**tmux control mode.** Ghostty has a fairly complete control-mode parser (issue #1935, open), but not the surfaces it needs: the blocker, in Mitchell Hashimoto's words, is creating "new apprt things (windows/splits/tabs)" that are not tied to an exec-based Termio. Work continues in small steps (commits in October 2026), and a community fork has an iTerm2-style version in its macOS app. Until it lands upstream, a libghostty surface needs a process of its own.

## 3. What Ghostty gives splits, and what stays

**Gives.**
- **Keys and actions.** The owner's Ghostty keybinds work in Irie's terminals, and the split ones arrive as actions for the app to carry out. macOS defaults:

| Keys | Action |
|---|---|
| ⌘D, ⇧⌘D | `new_split:right`, `new_split:down` |
| ⌘[, ⌘] | `goto_split:previous`, `goto_split:next` |
| ⌥⌘ arrows | `goto_split` up, down, left, right |
| ⌃⌘ arrows | `resize_split` by 10 |
| ⇧⌘↩ | `toggle_split_zoom` |

- **A split tree to adapt.** Ghostty's `SplitTree.swift` (1,413 lines), `SplitView.swift`, its divider and `TerminalSplitTreeView.swift` are MIT: a codable tree of leaves and splits with ratios, paths, directional focus, zoom and equalize. A quick search found no syntax newer than Swift 5.9 in it, so adapting it for the app's compiler looks possible, though the app target builds it in Swift 6 mode upstream.
- **One engine for many panes.** Every surface shares one app and its font atlases.

**Stays.** A tmux client per pane, the view sessions, the team server, the shim, and the core's layout rules.

## 4. Design

### 4.1 Build

- `mac/build.sh` builds GhosttyKit once per Ghostty version, target and compiler, into the cache it already keeps for SwiftTerm: `~/Library/Caches/irie/ghostty-1.3.1-<target>-<cc>/`. It fetches Zig 0.15.2 and Ghostty's v1.3.1 source by URL and SHA-256, both pinned in the script, and builds with `ZIG_GLOBAL_CACHE_DIR` inside that folder, so nothing lands elsewhere.
- The app links the static library with `swiftc` as now, plus the frameworks it needs (Metal, MetalKit, QuartzCore, CoreText, Carbon, IOSurface and the C++ runtime), importing the module map's `GhosttyKit`. No Swift 6 is needed: the wrapper is Irie's own.
- `terminfo` and `shell-integration` go into `Irie.app/Contents/Resources/ghostty`, and the app sets `GHOSTTY_RESOURCES_DIR` before `ghostty_init`.
- PROVENANCE's third-party code lists Ghostty (MIT) and what was adapted from its Swift app.
- **Fallback for the toolchain.** If Xcode 15's SDK will not build 1.3.1: Xcode 16 (needs macOS 14.5 or later on this Mac), or a CI job on a macOS runner once the repo is pushed. A prebuilt framework from a third party is the last resort: it is a 141 MB binary nobody here built.

### 4.2 The terminal view

`GhosttyTerminal.swift`, one file for every `ghostty_*` call, as Calyx keeps its FFI in one place:

- **App and config.** One `ghostty_app_t` per process. Config loads the owner's Ghostty files (`ghostty_config_load_default_files`), then Irie's overrides in a file of its own: the terminal's ground and ink from `Theme`, the app's font size, and no window decorations. libghostty does not watch files, so the Settings change and View > Theme reload it.
- **Callbacks.** C functions with the view passed through `userdata`. Wakeup can come on any thread and hops to the main queue to tick; action runs on the main thread and returns whether it did the action, so an action Irie does not carry out returns `false` and the key reaches the program. Clipboard read and write go to `NSPasteboard`; close surface ends the view.
- **The surface.** Its command is `tmux -S <socket> attach-session -t <target>`, with `TERMINFO` at the bundled terminfo, so tmux knows Ghostty's features. The view passes its laid-out size when it makes the surface: a surface born at Ghostty's default grid would start the main client at that size, and Irie keeps every agent window at the slot's size, so every agent would be resized once at launch.
- **Input and drawing.** Key down, key up and modifiers go to `ghostty_surface_key`, and text and marked text through `NSTextInputClient` for IME and dead keys; mouse buttons, position, scroll (with precision and momentum) and pressure; focus on becoming first responder; size and scale on layout and on screen changes. Ghostty's `SurfaceView_AppKit.swift` (MIT, 2,341 lines) is the reference.
- **Two lessons from other embedders.** Never put a live surface inside a SwiftUI `if`/`else`: flipping the branch rebuilds it and kills its process (Calyx). And host surfaces in plain AppKit split views, not nested SwiftUI hosting views, whose focus walk grows with every level (cmux).

### 4.3 One setting, two engines

`terminalEngine` in the app's defaults, `ghostty` or `swiftterm`, under Settings > Appearance. `TerminalHost` and `SplitTerminal` pick the view; everything around them (the frame, the lines from the card, focus tokens, reattach after the core restarts) stays. SwiftTerm stays for a release as the fallback, then goes with its build step.

### 4.4 Splits for any pane

- **The tree.** The terminal area holds a split tree (adapted from Ghostty's) instead of `TeamSplit`'s fixed layout. A leaf is a session, a teammate, or a shell. A session with teammates opens with them in a column, as today; Ghostty's split actions from the focused pane edit the tree; the app keeps a tree per session, so a session comes back as it was left.
- **New leaves.** `new_split` asks what goes there: another session (the list, as in the find field), or a shell in the focused session's folder. A card dragged onto the terminal splits it too.
- **The core.** `Want.Split` becomes `Want.Views`: one view session per shown session (`irie-view-<n>`), each linking that agent's window alone at `window-size latest`, and the slot shows the conversation of the selected one. A shell is a session of its own on the team server, beside the teammates. `show` takes the list of leaves.
- **Keys.** The app's own menu keys win: ⌘[ and ⌘] stay Next and Previous Helper, so moving between panes is ⌥⌘ arrows.

### 4.5 tmux

Every tmux client then runs in a Ghostty surface, so the main server's `terminal-features` gain `xterm-ghostty` (RGB, sync, styled underlines), as tmux reads them from the client's TERM.

### 4.6 Later: control mode

Once libghostty can host a surface with no process of its own (issue #1935), the app can keep one control-mode connection per server and draw every tmux pane it wants as a surface, at the size of its pane. The view sessions, the zoom and the slot's single size then go: tmux's own layout becomes the app's, and agent teams' real `split-window` calls could stay where Claude Code puts them.

## 5. What it costs

| Item | Cost |
|---|---|
| Build | Zig and a pinned Ghostty source in the build; a first build of several minutes and a few GB of cache; Xcode selected for it |
| Binary | Tens of MB more after dead-stripping |
| API drift | The C API changes between versions: pin one, and on each update diff `GHOSTTY_ACTION_` against the action router (Swift sees the enum as non-exhaustive, so the compiler cannot) |
| Input | IME, dead keys, Option as Meta and key repeat must match Ghostty's own app; worth a checklist |
| Two engines | For one release, both paths need testing |

## 6. Order of work

0. **Spike** (small; needs the owner's go-ahead). Build GhosttyKit 1.3.1 with Zig 0.15.2 and Xcode 15.0.1 (`DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer`), link it with `swiftc` 5.9 into a scratch app with one surface running `tmux -L irie attach-session -t irie`, and measure the build, the binary, typing, and drawing heavy output. If the SDK is too old, stop and take §4.1's fallback.
1. **Build** (small). `mac/build.sh` and the cache; resources into the bundle; PROVENANCE.
2. **The view** (medium). `GhosttyTerminal.swift` with input, drawing, focus, clipboard and config; `--snapshot` gains a Ghostty frame.
3. **The setting** (small). Ghostty for the main terminal and the team split, SwiftTerm as the fallback.
4. **Splits for any pane** (medium). The tree and its keys in the app; `Want.Views` and shells in the core; tests with private tmux servers, as `TestSplitSizesOnlyTheLead` does for one.
5. **Control mode** (large, upstream-gated). When libghostty can host process-less surfaces.

Steps 1 to 3 do not depend on 4, and 4 does not depend on Ghostty: it can start on SwiftTerm today.

## 7. Decisions for the owner

| # | Decision | Options | Recommendation |
|---|---|---|---|
| 1 | Run the spike here | a. Yes, as in §6 step 0. b. Build elsewhere | **a**: Xcode 15 is already installed; it decides whether this Mac can build Ghostty at all |
| 2 | Whose look | a. The owner's Ghostty config, with Irie's ground and size on top. b. Irie's own only | **a**, so Irie's terminals look and type like the owner's Ghostty |
| 3 | Toolchain if Xcode 15 fails | a. Update macOS to 14.5+ and install Xcode 16. b. CI on a macOS runner. c. A third-party prebuilt framework | **a**, then **b** once the repo is pushed; **c** never for a release |
| 4 | Order | a. Engine first, then splits. b. Splits first, on SwiftTerm | **b** if splits are what matters most, since it needs no new toolchain; **a** if the look and feel of Ghostty does |
| 5 | SwiftTerm | a. Keep one release as the fallback. b. Remove at once | **a** |
