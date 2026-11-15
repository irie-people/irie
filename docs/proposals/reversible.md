# Reversible Irie: what the Toffoli gate teaches

Date: 2026-11-07. Status: step 1 built (d03b8d1, 9231cf3); step 2, the rebuild meter, on 2026-11-14 (DESIGN §6.3); step 5, dropping merged worktrees, on 2026-11-15, at the maintainer's word (DESIGN §4.5); steps 3, 4 and 6 are proposals. An agent (Claude Code) wrote it after researching the Toffoli gate, reversible computing and quantum computers for the maintainer. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

**In short.** The Toffoli gate, or controlled-controlled-NOT, flips its target bit only when both control bits are 1. It keeps its inputs, so no information is lost, and run twice it undoes itself. Any computation can be built from such gates (Toffoli, 1980). Bennett (1973) showed such a computation need not pile up garbage either: compute, copy the answer, then run the steps backwards to clean the scratch.

Neither reversible chips nor quantum computers will make Irie faster before 2030. Irie's costs are tokens, time, memory and disk, not the chip's energy. No one has shown a quantum speedup for language models, scheduling or coding. Two things do carry over:

- **The gate's rules, as design.**
- **A threat.** Quantum computers are measured in Toffoli gates, and they will break the elliptic-curve keys under Irie's links.

## The rules

1. **Every action is its own undo.** An action that can be undone needs no "are you sure?". The history is DESIGN §4.5.
2. **An inverse puts back all the action changed,** not only the field it names. Promote moves a session to the top and unparks it. So undoing a promote puts back its place and its park too.
3. **What leaves the machine can't be undone.** A quantum gate stays reversible only while nothing outside learns of it. Irie's equivalent is a note an agent has read, a text, a push, a pull request or a login. Such effects deserve two controls, as the gate has two: the user's word and a second check (Mikael). Everything else can simply happen, since undo is there.
4. **Compute, copy, uncompute.** Scratch should go once its result is out. A subagent works this way: it reads widely in its own context, hands back an answer, and its context is dropped, so the parent re-reads only the answer on every later turn.
5. **Count the expensive gate.** Quantum algorithms are priced in Toffoli gates and nothing else. Irie's expensive gate is a cache rebuild.
   - When a session's prompt cache has expired, its next turn writes the whole context again, at 2× base input on a one-hour cache (1.25× on a five-minute one).
   - Reading it costs 0.05× on Opus 5.5 (0.1× on most models).
   - So one rebuild costs what 25 to 40 turns of reading do.

## Steps

1. **Built.** Rename, reorder, the freeze and thaw of one session, and promote and stop promoting are steps of the history, with labels that name their sessions (`rename "a" to "b"`). Undoing a promotion puts the session back in its place and parks it again if it was parked. `core.Title` and the Mac app's Edit menu put curly quotes on each quoted name.

   A test in `internal/peer` checks that the phone's and the peers' links agree their keys with the hybrid post-quantum X25519MLKEM768. It also checks that a client with X25519 alone still gets in ([PHONE.md](../PHONE.md)).
2. **A rebuild meter. Built** (DESIGN §6.3).
   - Each turn's usage says how much it wrote to the cache. A turn whose write is most of its context rebuilt the cache, after an expiry or a reopen. A compaction starts a new context, so the meter does not count it.
   - Count those rebuilds per session: the journal's `cache.rebuilt`, with what each cost and what woke the session, each row's count and cost today and this week, and `irie ctl rebuilds`.
   - Before Irie wakes a session idle past its cache's life, say what the rebuild costs, the way the spawn list says what a session costs (§6.3). Irie wakes sessions with a send, the jot dog's sending back (§4.8), a thaw, or a reopen. The trod bot leaves a cold session be when waking it costs more than `usage.wake_most`; the jot dog's nudge comes while the cache is warm.
   - Irie causes rebuilds of its own: a freeze of more than an hour (§4.10), close and reopen, and a mass thaw.
3. **The rest of undo.** Pause, notes, and freezing or thawing them all as one step that names its sessions.
4. **Inside and outside.** Mark each op by whether its effect leaves Irie. Inside ops never ask, since undo is there. Outside ops are the ones to confirm or have Mikael weigh. A mass thaw is an inside action with outside effects: each agent signs in again.
5. **Uncompute merged worktrees. Built** (DESIGN §4.5), approved on 2026-11-15. A closed session whose branch is in its repo's land branch (`live` for Irie's own), and whose worktree is clean, needs only its branch.
   - Once its agent has ended, Irie removes the checkout with `git worktree remove` and keeps the branch, while nothing runs in the folder: no process works there, runs a program from it or has a file in it open.
   - Reopen, from history or from undo, adds it back at the same path with `git worktree add`, which runs the repo's hooks.
   - The journal's `worktree.dropped` keeps the path, branch and commit, to put it back by hand. `irie ctl worktrees` is a dry run of the sweep of the sessions closed before; `--drop` does it.
   - It saves the checkout's room: about 9 MB a session, more with build output.
6. **Quantum watch.** The key exchange is post-quantum. The certificates (ECDSA P-256) and SSH keys (Ed25519) are not, which matters only against an attacker who has a quantum computer at the time of connecting.
   - Move Irie's authority to ML-DSA once phones accept such certificates. Go 1.27's `crypto/mldsa` and `crypto/x509` can already make them.
   - Keep `go.mod`'s `go` line at 1.24 or later, since a lower line turns the hybrid off.

## Measured

On the maintainer's Mac on 2026-11-07, the 27 open sessions had:

- read 819 million tokens from the cache
- written 17 million to it
- produced 3.8 million of output
- sent 8,600 uncached

At Opus 5.5's list prices with a one-hour cache, that is 44% of spend re-reading context, 36% writing it and 20% output. Part of the writing is each turn's new tool output, which has to be written once; the rest is rebuilds, which the meter of step 2 now tells apart (`irie ctl rebuilds`).

The newest estimates for breaking 256-bit elliptic curves:

- under 500,000 superconducting qubits for minutes (arXiv:2603.28846)
- 26,000 neutral atoms for days (arXiv:2603.28627)

Surveyed experts give a 28–49% chance of such a machine within 10 years.
