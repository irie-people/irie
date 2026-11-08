# Agents as processes, on git

Date: 2026-11-01. Status: proposal; step 1 is built (DESIGN §4.13). Sources are in PROVENANCE.md.

## The ask

Rebuild Irie on git, for context forking and git operations on the context itself. The user pauses processes and changes their priority on the fly, and the processes are agents. Orchestrators have small sidecar agents that handle the messages coming in from other agents and going out to them, offload tasks and queue them, so that nothing is wasted. Once the CLI controls everything, the UI has to feel the snappiest it can. All of it is a foundation for experiments: testing, racing, tuning, and publishing the numbers.

## What others do

| Work | What it does | What Irie takes, and where it differs |
|---|---|---|
| Git Context Controller (arXiv 2508.00031, 2025) | An agent's memory as a versioned file system with COMMIT (a milestone with a summary), BRANCH (an exploration), MERGE and CONTEXT (the log at some resolution); agents that commit summaries do better on long tasks | Commits at milestones and branches for explorations. Irie stores the real transcript rather than a narrative the model writes, so a fork carries the exact conversation on, and the model spends no tokens keeping its own log |
| Entire CLI (entireio/cli) | Captures agent sessions in git as checkpoints, linked to code commits, under refs of their own (`refs/entire/checkpoints/…`); resumes a branch's sessions | Contexts linked to code by commit. Irie keeps its store outside the user's repo, so no push of theirs carries a conversation away, and gives each session a branch and each turn a commit |
| Claude Code | A transcript is a tree of entries (`uuid`, `parentUuid`); `/branch` and `--fork-session` copy a conversation whole; `/rewind` goes back a turn; the Agent SDK's `forkSession(id, {upToMessageId})` copies a transcript up to a message into a new session that `resume` carries on | Irie's fork is `forkSession()`'s, from any turn of any session, with the code of that turn in a worktree of its own |
| AIOS: LLM Agent Operating System (COLM 2025) | An agent OS kernel: a scheduler (FIFO, round robin), a context manager that snapshots and restores a suspended agent, and calls to get and set an agent's priority, suspend and resume it | The process model. Irie cannot suspend a token stream it does not serve: it stops an agent at a tool boundary (a hook that holds) or stops its whole process tree (SIGSTOP), and its snapshot is a commit |

## The model

| An OS has | Irie has | State |
|---|---|---|
| A process and its pid | A session and its id; its agent's process tree under its host | Built |
| `ps`, `top` | `irie ctl ps`: one line per agent, process-style | Built (§4.14) |
| SIGSTOP and SIGCONT | Freeze and thaw: the whole tree at once (§4.3) | Built (M6) |
| Stopping at a safe point | Pause: held at the next tool call by its hook (§4.3) | M5, not built |
| `nice`, `renice` | Priority: 1 promoted (its tree gets the machine first), 0, and below 0 in the background | 0 and 1 built (M7) |
| A scheduler and its run queue | At most `sched.slots` agents at work; the rest wait, the highest priority first | Step 5 |
| `fork(2)` | `irie ctl fork`: a context carried on from any turn, with that turn's code | Built (§4.13) |
| A checkpoint | A commit per turn | Built (§4.13) |
| Pipes and signals between processes | Mailboxes, delivered at tool boundaries as notes | Step 6 |
| Daemons | Sidecars: a small model per orchestrator that reads and answers its mail | Step 7 |

## Steps

1. **Contexts in git, and fork.** Built: DESIGN §4.13. Every session a branch, every turn a commit with the code it recorded; `irie ctl fork`, `ctx log`, `show`, `diff`, `tag`, `sync`, `git`. First sync of a 300-turn session 0.65 s, each turn after it ~21 ms, a fork's transcript 0.1 s.
2. **Fork from the apps.** The conversation view's items carry their entry's uuid, so each gets "Fork from here"; a card's menu gets "Fork…", and the sidebar `:fork`. Both call `fork`, so the CLI and the apps do the same thing.
3. **`irie ctl ps`.** Built (DESIGN §4.14): a line per agent with its state as a letter (R, S, W, H, T, I, Z, X), priority, model, context used, tokens, cost, memory, age and name; `--json` for scripts, `--watch` for a live table. Q, queued, comes with the scheduler.
4. **Pause and notes (M5, §4.3).** The hold at the next tool call, and the notes channel that delivers text at a tool boundary. Mailboxes need the notes channel; the scheduler needs pause.
5. **The scheduler.** `sched.slots` (default: no limit). A spawn past the limit waits in a queued state, and starts when a slot frees, the highest priority first, then the oldest. `irie ctl renice SESSION LEVEL` changes a priority on the fly, a waiting spawn's too. Below 0, an agent's tree runs in the background whether or not another is promoted. Holding agents that are working when the slots shrink is pause's job.
6. **Mailboxes.** `irie ctl msg send TO TEXT` from the user or an agent (`"$IRIE_BIN" ctl msg send`), `msg inbox`. A message is delivered at its agent's next tool boundary as a note, or with its next prompt when it is idle, and stored in the context store as a commit on `refs/irie/mail/<session>`, so what was said between agents is in the same log as what each did.
7. **Sidecars.** A sidecar is a session on a cheap profile (`+haiku`, `+local`) started for an orchestrator. It drains the orchestrator's inbox, answers what it can from the orchestrator's context, which it reads from the store, and batches the rest into one digest note. Going out, it turns an orchestrator's queued requests into spawns through the router (§6.4). Measured by the orchestrator's tokens with and without it.
8. **Races.** A first cut is built (DESIGN §4.14): races from @REPO or from a fork, checked, ranked and kept as JSON and a page; not yet in the context store, and racers not yet exempt from the jot dog. `irie ctl race [--from SESSION@AT] [--check CMD] +a +b +c TASK` forks one point to N profiles, each in a worktree of its own. When each turn ends it runs CHECK there, and records wall time, turns, tool calls, tokens, cost and whether CHECK passed. The results are a commit on `refs/irie/races/<id>` (`results.json`, `results.md`), so a race is versioned like the contexts it compares. `irie ctl race report` prints the table; `--publish` writes a page for the site.
9. **The snappiest UI.** Every op the CLI has, the apps have, and each answers on screen before the core does: an optimistic change that the next frame confirms or takes back. A budget: an op's round trip under 5 ms at p50, a frame under 16 ms. `irie ctl bench` measures each op's round trip, and its numbers are published beside the races'.

## Decisions for the owner

1. **Retention.** Settled on 2026-11-02: the owner chose to clean contexts up after a time each user sets. `context.keep_days`, 30 by default, as Claude Code keeps its own transcripts, and 0 for good; the Mac app's Settings window sets it (DESIGN §4.13).
2. **Forks as jots.** A fork with a task and a worktree is a jot like any other, so the jot dog sees its work through to `live`. A race's forks should probably be exempt: only the winner lands.
3. **What is published.** Race numbers can go on the site without prompts or code (profile, task class, time, tokens, cost, pass), or with them for tasks that are public.
