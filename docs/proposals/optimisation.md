# Proposal: Irie, optimised to the ceiling

Date: 2026-10-31. Status: proposed, for the owner to decide. There is no issue tracker yet, so this file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote it from `live` at `7035eaf`, after measuring the Irie running on the owner's machine. It is the plan only and builds no code. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

## Goal

Irie should cost next to nothing while nothing happens and add no delay an agent can feel when something does. Its memory should stay flat, it should spend no token it does not need, and a change should reach the running Irie quickly. Here "the ceiling" means meeting a fixed target for each of these, measured the same way every time, rather than being faster than before.

**Today and the ceiling.** Measured on 2026-10-31 with 38 to 40 sessions (§1):

| Measure | Today | Ceiling |
|---|---|---|
| Mac app CPU, with nobody touching it | 37–98% of a core, mostly above 90%; the main thread is about 80% busy | under 2% |
| Mac app memory | 742 MB after 1 h; 1,316 MB after 4.6 h and still growing, by 70–150 MB an hour | flat (under 5 MB an hour), and under 400 MB with 40 sessions |
| Core, tmux sidebar and placeholder | 2–8%, 2% and 2–4% of a core | under 1% together while nothing changes |
| Listing sessions outside Irie | `claude agents --json` every 3 s while the app is open. One run takes 0.4–0.6 s of CPU and 3–4 s, so it costs 13–20% of a core | it runs only when Claude Code's session files change |
| Status line | `irie statusline` starts every 2 s in every session: about 19 starts a second at about 16 ms of CPU each | at most one start per session every 5 s, at most 3 ms each, and no work in the core when nothing changed |
| Hooks | 2 blocking process starts per tool call at about 16 ms of CPU each, plus up to 20–70 ms waiting on the core's lock while it runs tmux | at most 3 ms per start, no wait on the lock, and the events that only report taken off the critical path |
| Frames to the app | about 5.7 a second. A 46 KB snapshot of every session went out 12 times in 30 s, and a plan figure that keeps flipping sends 3.3 headers a second | frames only for real changes, and a snapshot only on subscribe |
| The agents' own terminals | 60 `claude` processes use about 135% of a core between them | measured with Claude Code's reduced motion on (§2.9) |
| Routing | "Fix the failing test" is classed hard and runs on Opus at max effort; "Review the PR and summarize the risks" is classed medium | classes checked against every past task, and $ per landed jot tracked |
| Tests | `go test ./...` takes 646 s under load. `internal/core` is killed at 10 min with 90 of its 168 tests run. 41 test tmux servers have leaked, the oldest 4 days ago | under 3 min under the same load, and no leaks, even after a timeout |

**In short.** Twelve iterations (§3). Each one lands on its own with `tools/live.sh` and has a gate it must pass.
- **Iteration 1** builds the yardstick.
- **Iterations 2–4** are small changes that remove most of the waste measured here, and make the tests finish.
- **Iterations 6 and 7** change how the app redraws and stop its memory growth.
- **Iterations 9 and 10** take hooks off the critical path and let the core sleep while nothing happens.
- **Iterations 5 and 8** are the token work.
- **Iteration 11** makes the agents draw less and the app build faster.
- **Iteration 12** adds a budget that keeps the gains.

---

## 1. How it was measured

- **Machine.** 12-core Apple silicon with 18 GiB, macOS 14.1, Go 1.27.3, tmux 3.7b and Claude Code 2.1.316, with 38 to 40 sessions in Irie. The agents' own work kept the load average between 25 and 150. So wall times here are upper bounds; compare CPU shares instead.
- **Tools.**
  - `ps`, `sample` for 5 s on the app, `footprint`, and `/usr/bin/time` over 50 runs.
  - `GODEBUG=inittrace=1` for package init.
  - A probe that subscribes to the core's socket the way a client does and counts frames by type, size and changed field for 30 s.
  - `go test -json -count=1 ./...`, the journal, and `irie ctl route` for the classifier.

  Iteration 1 turns these into one repeatable tool.

## 2. Findings

### 2.1 The Mac app redraws every card, several times a second

- **One store for everything.** `Store` is one `ObservableObject` (`mac/Sources/Irie/Store.swift:8`) with about 40 `@Published` properties. About 40 views read it through `@EnvironmentObject`, including every `SessionCard` (`SidebarView.swift:174`). A change to any property re-runs all of those views.
- **Frames are applied whether or not anything changed.** Each frame gets its own main-actor task (`Store.swift:171`). An upsert assigns `sessions[i] = s` even when the row is unchanged (`Store.swift:212`), and a snapshot replaces the whole array (`Store.swift:207`).
- **The core sends whole snapshots for small changes.** A subscriber gets a single upsert only when exactly one session changed within the 100 ms frame gap. If two or more changed, it gets every session (`internal/core/subscribe.go:315`).
  - In 30 s that was 12 snapshots of 46 KB, 60 upserts and 98 headers.
  - The upserts carried `detail` 32 times, `command` 31, `cost_usd` 15, `tokens` 12, `subagents` 4, `context` 2 and `state` 1.
- **Colours are rebuilt on every call.** `Theme.dynamic` and `Theme.computed` wrap a new `NSColor(name:dynamicProvider:)` each time (`Theme.swift:181–217`), so every redraw hands AppKit new colour objects. That fits the profile's `-[NSView setAppearance:]` and `NSDP_setPropertyValue` cascades; iteration 6 confirms it.
- **The age clock covers the whole list.** One 30 s `TimelineView` wraps every card (`SidebarView.swift:90`).
- **Profile.** About 380 of 490 main-thread samples were working:
  - 223 in SwiftUI's graph update;
  - 142 in layout, a deep `_layoutSubtreeWithOldSize:` recursion;
  - 31 in a recursive `setAppearance:`;
  - 27 in `NSDP_setPropertyValue`.

  Irie's own code is a small share (`SessionCard.body`, `Store.drawnGroups`, `WholeLines`, `Store.canMove` and the `Theme` closures). The cost is how often SwiftUI is asked to redo everything.

### 2.2 The app's memory grows

After 4.6 h, `MALLOC_TINY` held 663 MB and `MALLOC_NANO` 323 MB, all of it dirty. Graphics memory (IOAccelerator and IOSurface) was only about 200 MB. So the growth is in small objects, not terminals.

Which type grows is not known yet. A heap dump pauses the app, so none was taken of the live app; iteration 7 takes one of a build fed by a fixture core. The suspects:
- colour providers made on every redraw and kept in AppKit's dependency caches (the profile's `__reusableDependencyContextForKey` recursion);
- per-frame task closures and copies of the session array;
- conversation items that are never trimmed.

### 2.3 Listing the sessions outside Irie

While anyone is looking, the core runs `claude agents --json` every 3 s, and the Mac app always counts as looking (`internal/core/outside.go:40, 84, 240`). The comment there says this costs about 4% of a core. Here one run took 0.4–0.6 s of CPU and 3–4 s of wall time, so a run is nearly always in flight.

Claude Code rewrites `~/.claude/sessions/<pid>.json` whenever a session's status changes; the file has `updatedAt` and `statusUpdatedAt` fields. A cheap look at that directory's names, sizes and times tells the core when a run is worth it. That means reading no file Claude Code does not document.

### 2.4 The status line

- **A process every 2 s in every session.** Irie's status line re-runs every `ownRefresh = 2` seconds (`internal/harness/claude/statusline.go:76`), because Claude Code's own updates stop while a session is idle. With 38 sessions that is about 19 starts a second. Under load, one start took 15.8 ms of CPU with no core to talk to, against 3 ms for `/usr/bin/true`.
- **Every update makes the core diff everything again.** `status` runs `changes()`, a `reflect.DeepEqual` over every session, then `publishHeader`, all under the core's lock. It does so even when the update changed nothing (`internal/core/status.go:43–51`).
- **The plan's use flips.** Every update overwrites the plan's five-hour and weekly use with the figures that session last saw (`status.go:40–41`). Idle sessions re-run every 2 s with old figures and overwrite the fresh ones. In 20 s the five-hour figure changed 39 times and the weekly one 20 times, and a header frame went out 3.3 times a second. Users can see this bug, and it costs CPU too.

### 2.5 Hooks

- **13 events, one process each.** Every Claude Code session runs `irie hook` on 13 events (`internal/harness/claude/hooks.go`). `PreToolUse` and `PostToolUse` fire on every tool call, and Claude Code waits for both.
- **Most of a start goes to code the hook never uses.** Under load, package init took about 35 ms of wall-clock time. About 10 ms of that went to `internal/ui` (5.0 ms, 560 allocations), `internal/core` (4.3 ms, 311 KB, 3,298 allocations), `ultraviolet`, `net/http`, `crypto/x509` and `lipgloss`. Every subcommand is the same binary, so every start runs every package's init.
- **The core's lock is held across tmux.** `tick()` holds `c.mu` while `refresh()` runs `tmux list-panes` (`internal/core/core.go:285–307`). That took 20–70 ms here, and a hook that arrives meanwhile waits for it. The hook handler itself records the event, makes its phrase, follows the conversation and diffs every session, all under the lock (`internal/core/hook.go:44–93`).
- **What Claude Code offers.**
  - HTTP hooks (`type: "http"`) POST the event to a URL and take headers filled from `allowedEnvVars`. A failed connection is a non-blocking error, so the action goes ahead.
  - Async command hooks (`async: true`) run without blocking.

  A hook that holds a tool call, for a pause or a frozen teammate, must survive a core restart. A command hook that waits and retries survives one; an HTTP request does not.

### 2.6 The core's tick and the tmux UIs

Every second the core:
- runs `tmux list-panes`, a fork and exec that returns 42 lines;
- opens and stats every session's transcripts to keep the token tally (`internal/core/tokens.go:42–63`). The reading is incremental, but every check opens the file;
- reads the process table while any session is promoted (`priority.go:65`).

Every 5 s it also runs `ps` to measure memory (`memory.go:81`).

The tmux sidebar and the placeholder subscribe to every frame, and they keep drawing even while no tmux client is attached (`internal/ui/run.go:110`, `internal/ui/slot.go:59`).

tmux 3.7b's control mode can report changes instead of being polled. Format subscriptions (`refresh-client -B`) are checked by the server once a second and reported with `%subscription-changed`, and the `no-output` flag leaves out the panes' output. DESIGN M11 already plans control-mode events.

### 2.7 Tokens

Irie itself spends few tokens. Its notes are appended late in the context, so the prompt cache survives them, and phrases and titles come from rules, not a model (`internal/core/phrase.go`, `title.go`). The waste is elsewhere:

- **The classifier only adds up words.** A hard word counts +3, a medium word +1 and a small word −2, and a score of 3 or more is hard (`internal/route/route.go:63–114`).
  - The live router classes "Fix the failing test" and "Fix the bug in the parse function" as hard, so they run on Opus at max effort.
  - It classes "Review the PR and summarize the risks", "Explain why the sidebar flickers" and "Quick fix for the race in the sidebar" as medium.
- **Outcomes lean good.**
  - A finished turn counts as good once it settles, unless someone rated it or it was done again (`internal/core/route.go:97–109`).
  - A profile below the floor for a class never gets that class, so it cannot earn its way back.
- **Delegation.**
  - Each `ctl delegate` starts a whole new session.
  - Its result is the child's whole last message, uncapped, and the parent reads it again on every later request (`internal/hook/hook.go:141`, `internal/core/route.go:538`, `cmd/irie/route.go:155`).
  - A delegate that outlives the caller's Bash timeout cannot be fetched later.
- **Fixed overhead.**
  - With `spawn.teammates = panes`, every Claude Code session gets the agent-teams environment, delegated and Haiku ones included (`internal/core/team.go:33–35, 80–82`).
  - The delegate note (545 characters) goes out on every `SessionStart`, resumes included (`internal/core/hook.go:78–79`).
- **A moved session resumes on the strongest profile** (`internal/core/move.go:115–121`). A Sonnet conversation moved into Irie is then cached again at Opus prices.
- **The dog says "wait for it and try again"** (`internal/core/dog.go:305`), which invites a polling loop.
- **Not measured yet:** how many tokens a session's first request carries, and how big the team tools are.

### 2.8 Tests and builds

- **The core's tests are serial and slow.**
  - `go test ./...` took 646 s with a load average around 150.
  - `internal/core` hit `go test`'s 10-minute default after 90 of its 168 tests. None of them calls `t.Parallel`.
  - The 427 top-level tests add up to 767 s.
  - Three timing tests failed under that load, as noted in earlier sessions.
- **Leaks.** 41 test tmux servers were still running, the oldest from 4 days ago. Many of them still run a test core, sidebar and placeholder that keep polling. A test killed by its timeout never runs its cleanup, and the timed-out run left 4 more.
- **The app builds with `-O` but not `-wmo`** (`mac/build.sh:76`). A build takes 10–20 minutes; a type-check takes about 8 s.

### 2.9 The agents' own terminals

Claude Code's terminal UI uses more CPU than anything else on the machine: 60 `claude` processes at about 135% of a core between them. Much of that redraws spinners in panes nobody is looking at.

Claude Code has a `prefersReducedMotion` setting, which "reduces or turns off interface animations such as the spinner, shimmer, and flash effects". Irie could set it in the settings file it already writes for each session. How much it saves is not measured yet.

## 3. The iterations

**Rules for every iteration.**
- It lands in `live` on its own with `tools/live.sh`. Its commit message carries a before and after from the yardstick.
- DESIGN §2.1 principle 6 holds: nothing disturbs an agent.
- A protocol change is negotiated: a client says what it can read, so an older app keeps working with a newer core, and the other way round.
- An iteration that misses its gate is reworked or reverted before the next one starts.

### Iteration 1. The yardstick (S)

- **`tools/perf`.** A Go program in this module, using `internal/ipc`, that prints one report after 30–60 s:
  - frames by type, size and changed field, and how often the header changes;
  - CPU and memory of the core, sidebar, placeholder, hosts and app;
  - how many `claude agents`, `irie statusline` and `irie hook` processes started;
  - the app's main-thread busy share from `sample`, and its `footprint`.
- **Counters in the core**, read with `irie ctl perf --json`:
  - request count and handling time per op (p50 and p99);
  - time spent waiting on `c.mu`;
  - frames sent, by type;
  - how long each tick takes.
- **Go benchmarks** for `changes()`, `list()`, `status()` and frame encoding, with 40 sessions.
- **Token figures:**
  - $ and tokens per landed jot, by class and profile, from `status.json` cost, transcript tallies and the dog's landed state;
  - cache-read share per request;
  - tokens in a session's first request.
- **The baseline:** the numbers in the table at the top go into an appendix of this file.

**Gate:** two runs ten minutes apart agree within 15%.

### Iteration 2. Stop the churn (S, four commits)

1. **The plan's use** comes from the newest window (the latest `resets_at`), and within a window from the highest `used`, instead of from whichever session reported last (`status.go:40`).
2. **A status update that changed nothing does nothing in the core:** no diff and no publish (`status.go:34–51`). An update that did change something diffs only its own session.
3. **No snapshot for two changes.** Send one `upsert` or `remove` per changed session instead of a snapshot of all of them (`subscribe.go:312–331`). Every client already reads those frames; check that the tmux sidebar re-sorts on an upsert the same way it does on a snapshot.
4. **The app applies only real changes.** It skips rows that are unchanged, compares a snapshot with what it has before assigning it, and applies every frame that arrived during one main-actor turn together (`Store.swift:170–241`).

Expected: headers fall from 3.3 a second to almost none, snapshots go out only on subscribe, and the app redraws only as often as something really changes. **Gate:** in `tools/perf`, at most 0.1 headers a second, no snapshot after subscribe, and app CPU at least halved before iteration 6 starts.

### Iteration 3. Stop the polling (S)

1. **Outside sessions.**
   - On the 500 ms ticker, look at `~/.claude/sessions`: names, sizes and modification times.
   - Run `claude agents --json` only when something there changed, at most every 3 s, plus once a minute regardless.
   - Run it only while the app shows the outside list.
   - Correct the 4% in the comment at `outside.go:34–40`.
2. **The status line every 5 s** (`ownRefresh`), set in the config as `usage.status_refresh`. While a finished session is idle, its own line can then lag by up to 5 s; the cards stay live.
3. **Memory sampling** every 30 s while nobody is looking, and every 5 s while someone is.

Expected:
- `claude agents` drops from 13–20% of a core to under 1%.
- Status line starts fall 2.5 times.

**Gate:**
- At most 2 `claude agents` runs a minute while no session changes.
- At most 0.2 status line starts a second per session.

### Iteration 4. Tests that finish and clean up (M)

1. **Test servers that cannot leak.** Name them `irie-test-<pid>-<random>`. Before any test starts, `TestMain` kills the servers whose test process is gone, which also covers runs killed by a timeout. Kill today's 41 once, with the owner's go-ahead.
2. **Parallel core tests.** Add `t.Parallel()` to every core test that owns its tmux socket and directories. The tests that touch process priority, the machine's memory or wall-clock bounds (promote, freeze, background) stay serial, behind one lock.
3. **A `go test` timeout in `tools/live.sh` that fits**, until step 2 is in, so an install under load does not fail on time alone.

Expected: `internal/core` drops from over 600 s to about 2 min under load, and every later iteration installs faster. **Gate:** `go test ./...` finishes in under 3 min at a load average around 50, and no `irie-test-*` server is left after a run, even one killed by its timeout.

### Iteration 5. Route by the right class (S)

1. **The classifier.** Medium words on their own cannot make a task hard: together they add at most +2. Small words lower a task only when it has no hard word. Replay every past `task.md` through the old and new rules, read the differences by hand, and land the change only if no hard or orchestration task moves down a class.
2. **Outcomes from landing.** A jot counts as good when its work landed, which the dog knows, and bad when it was redone or rated down. Today it counts as good once it settles.

Expected: routine fixes run on Sonnet, and reviews and "why" questions go back to the strongest profile. **Gate:** the replay passes, and two weeks after landing, $ per landed jot is down while the landing rate is not.

### Iteration 6. The app redraws only what changed (M)

1. **Observation.** Move `Store` to `@Observable`. The app already targets macOS 14 (`mac/build.sh:16`). A view then re-runs only when something it read has changed.
2. **One row object per session,** kept by id and updated in place by each frame. The list holds only the ids, so an upsert redraws one card.
3. **Colours made once** per palette and key (`Theme.swift:181–217`).
4. **Derived lists worked out once per change,** not in every view body: `drawnGroups`, `drawnOrder` and `canMove` (`Store.swift:340–390`).
5. **The 30 s clock moves into the age label** (`SidebarView.swift:90`).
6. **A card whose inputs are unchanged is not redrawn** (`Equatable`).

To check, count `Self._printChanges()` calls in a debug build fed by a fixture core that sends 2 upserts a second, and run `sample`. **Gate:** with 40 sessions and 2 upserts a second, the app stays under 3% CPU and its main thread under 5% busy, and one upsert redraws one card.

### Iteration 7. The app's memory stays flat (S–M)

1. **Find the type that grows,** with Instruments' Allocations generations or `heap`/`leaks`, on a build fed by the fixture core. Do it on the live app only with the owner's go-ahead, since it pauses the app.
2. **Fix it,** starting with the suspects in §2.2, and free the terminal surfaces of closed sessions.
3. **`tools/perf --soak 1h`** prints how fast the footprint grows.

**Gate:** under 5 MB an hour over 8 h with a steady set of sessions, and under 400 MB with 40 sessions.

### Iteration 8. Cheaper delegation, less fixed overhead (S)

1. **Delegation.**
   - Ask the child for a result of at most 10 lines.
   - Print at most 2,000 characters, with a pointer to `result.md`.
   - Add `ctl result SESSION` for a delegate that outlived its caller.
   - Start delegated sessions without the agent-teams environment.
2. **Agent teams only for sessions that may lead one:** not delegated, Haiku or local ones (`team.go:33–35`).
3. **The delegate note only on `startup`, `clear` and `compact`,** not on `resume` (`hook.go:78`).
4. **A move keeps the conversation's own model** when it can tell what that was (`move.go:115–121`).
5. **The dog says to wait in the background,** not to try again (`dog.go:305`).

**Gate:** iteration 1's figures for the tokens in a delegated session's first request, and for the parent's cache reads after a result, both go down.

### Iteration 9. Hooks without the wait (M)

1. **No tmux under the lock.**
   - Take the snapshot unlocked and apply it locked (`core.go:285–307`).
   - In `hook`, make the phrase outside the lock and diff only the session the hook is for.
2. **Cheap starts.** Measure two options and keep the better one:
   - Move package-level init in `internal/core`, `internal/ui`, `internal/config` and `internal/localmodel` behind `sync.OnceValue`.
   - Or build a small second binary, `cmd/irie-hook`, with only the hook and the status line (no Bubble Tea, no `net/http`), and name it in the settings file.

   Also try `-ldflags=-s -w` for installed builds.
3. **Off the critical path.**
   - First check how each event's answer and any hold are used.
   - Make `SubagentStart`, `SubagentStop`, `Notification`, `StopFailure`, `SessionEnd` and `Elicitation` `async: true` command hooks. Do the same for `PostToolUse` in sessions that cannot have teammates.
   - The events that can hold a tool call or carry an answer stay blocking command hooks: `PreToolUse`, `PermissionRequest`, `SessionStart`, `UserPromptSubmit`, `Stop`, and `PostToolUse` in sessions with teammates.

Expected: a start falls from about 16 ms of CPU to about 3, and no hook waits for a tick. **Gate:** on an idle machine, `PreToolUse` adds under 5 ms at p99 by the core's own count from iteration 1, and no hook waits on `c.mu` for more than 1 ms.

### Iteration 10. The core sleeps when nothing happens (M–L)

1. **tmux control mode (M11).** One `tmux -C` client with `no-output` and a format subscription on the pane fields the core reads replaces `list-panes` every second. A full `list-panes` still runs every 30 s, and after a reconnect.
2. **Tick on events, not on the clock.** The events are hooks, status updates, tmux notifications and transcript writes; for the transcripts, use kqueue, which `golang.org/x/sys/unix` provides. A 5 s tick stays only for timers such as `ready_timeout` and the dog's sweep.
3. **The tmux UIs rest while no tmux client is attached.** The header says so, the sidebar and placeholder stop drawing, and the core builds no frames for them.
4. **A row's wire form is built only when its session changed,** and compared without `reflect.DeepEqual` (`core.go:466`).

**Gate:**
- With 40 idle sessions and the app closed:
  - core, sidebar and placeholder stay under 0.5% of a core between them;
  - no processes start in a minute beyond the safety checks.
- With the app open, they stay under 1%.

### Iteration 11. Agents that draw less, and faster app builds (S)

1. **Reduced motion.** Pass `prefersReducedMotion: true` in the per-session settings, set in the config as `spawn.agent_motion = false`. Measure the `claude` processes' CPU with it on and off, both idle and working. Keep it only if it saves at least 20%.
2. **The Mac build.** Try `-wmo`, and find slow type-checks with `-Xfrontend -warn-long-function-bodies=200`. Keep whatever builds fastest without running slower.

**Gate:** the measured saving, and a Mac build at least twice as fast.

### Iteration 12. Hold the line (S)

- **Budget tests that fail the build when a change makes things worse:**
  - `changes()` with 40 sessions stays under a set number of microseconds;
  - frame bytes per change stay under a set size;
  - a tick with no change sends no frame;
  - a status update that changed nothing sends nothing.
- **A one-hour soak** with the fixture core before each release, recording how fast the app's memory grows and its idle CPU.
- **Optional, and only by A/B:** effort below max for hard work that is not orchestration, judged by ratings and by what lands.

## 4. Order and sizes

| # | Iteration | Area | Size | Needs | Biggest win |
|---|---|---|---|---|---|
| 1 | The yardstick | all | S | — | the numbers every later gate needs |
| 2 | Stop the churn | core, app | S | 1 | most frames, and the flipping plan figure |
| 3 | Stop the polling | core | S | 1 | 13–20% of a core; status line starts fall 2.5 times |
| 4 | Tests that finish and clean up | tests | M | — | installs in minutes; no leaked servers |
| 5 | Route by the right class | tokens | S | 1 | Opus at max effort on routine fixes |
| 6 | The app redraws only what changed | app | M | 2 | about 90% of the app's CPU |
| 7 | The app's memory stays flat | app | S–M | 6 | 70–150 MB an hour |
| 8 | Cheaper delegation, less fixed overhead | tokens | S | 1 | tokens per delegate and per session |
| 9 | Hooks without the wait | core, hook | M | 1 | latency on every tool call |
| 10 | The core sleeps when nothing happens | core, UI | M–L | 9 | the rest of the idle CPU |
| 11 | Agents that draw less, faster builds | agents, Mac app | S | 1 | possibly the most CPU of all |
| 12 | Hold the line | all | S | 2–10 | keeping the gains |

Iterations 2, 3, 4, 5 and 8 touch different files, so they can run as separate jots at the same time. Iterations 6 and 7 are one stream of work, in that order.

## 5. Not doing

- **Reading Claude Code's session files to list outside sessions.** Their format is not documented. Irie only checks whether the directory changed, and the CLI stays the source.
- **HTTP hooks for events that can hold a tool call.** A core restart would drop the request, and the tool would go ahead while the session is paused or frozen.
- **Moving orchestration or hard work off Opus, or lowering its effort, without A/B data.**
- **Rewriting the app in AppKit.** SwiftUI with Observation and one object per row should reach the target, and iteration 6's gate says whether it did.
- **Replacing JSON on the socket.** At the frame rate left after iteration 2, encoding is not where the time goes.

## Open decisions

- **The status line interval.** 5 s is proposed. 10 s halves the starts again, but a finished session's own line lags longer.
- **Killing today's 41 leaked test servers** (iteration 4). They are test leftovers on this machine, but only the owner should say so.
- **Heap dumps of the live app** (iteration 7). They pause the app for a while.
- **Reduced motion** (iteration 11). It changes how every agent's terminal looks.
- **Learning by exploration.** One small or medium task in N would go to a profile below the floor, so that profile can earn its way back. Off unless the owner wants it.

## Appendix: the baseline, 2026-10-31

| What | Measured |
|---|---|
| Irie.app | 92–98% CPU by `ps`, 37% over 3 s by `top`; footprint 742 MB (peak 818 MB) after 58 min; a later instance 1,316 MB after 4 h 37 min and 1,325 MB 8 min later |
| App memory by kind (4.6 h) | `MALLOC_TINY` 663 MB, `MALLOC_NANO` 323 MB, IOAccelerator 145 + 16 MB, `MALLOC_SMALL` 67 MB, IOSurface 45 MB |
| Main thread, 490 samples | 223 SwiftUI graph update, 142 layout, 31 `setAppearance:`, 27 `NSDP_setPropertyValue`, about 40 waiting |
| Frames, 30 s, 38 sessions | header 98 (300 B on average), upsert 60 (1,083 B), snapshot 12 (45,960 B), outside 1, closed 1 (10 KB), history 1; 5–13 frames a second |
| Header, 20 s | 49 headers: `plan.five_hour` changed 39 times, `plan.seven_day` 20, `memory.agents` 4, `memory.available` 3 |
| Core, sidebar, placeholder | 1.7–8.0%, 1.5–2.2%, 1.6–4.2% CPU; 23–33 MB, 17 MB, 21–22 MB |
| `irie host` (one per agent) | 0% CPU, 2–6 MB each |
| `claude agents --json` | 3.0–4.0 s wall, 0.41–0.60 s CPU (user and sys) per run |
| `irie hook`, `irie statusline` | 15.6 and 15.8 ms CPU per start over 50 starts each, with no core (`/usr/bin/true`: 3 ms) |
| Package init of one start | 110 packages; about 35 ms in total; `internal/ui` 5.0 ms, `internal/core` 4.3 ms, `ultraviolet` 1.5 ms, `net/http` 1.0 ms, `crypto/x509` 0.64 ms, `internal/config` 0.59 ms, `internal/localmodel` 0.54 ms |
| `tmux list-panes -a` | 20–70 ms with 42 panes at a load average of about 56 |
| Journal | 20,209 events on the day, 11 MB; `tool.started` 6.6 MB of it |
| `go test -json -count=1 ./...` | 646 s; `internal/core` 600.5 s (timed out at 10 min), `internal/host` 61.7 s, `internal/tmux` 56.0 s, the rest under 20 s each; 427 top-level tests, 767 s summed; slowest single test 25 s |
| Test tmux servers | 41 alive, the oldest 4 days; 4 more after the timed-out run |
| `claude` processes | 60, about 135% of a core between them |
| Load average | 25 to 150 during the measurements |
