# Reversible Irie: what the Toffoli gate teaches

Date: 2026-11-07. Status: step 1 built (d03b8d1, 9231cf3); steps 2 to 6 are proposals. An agent (Claude Code) wrote it after researching the Toffoli gate, reversible computing and quantum computers for the maintainer. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

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
2. **A rebuild meter.**
   - Each turn's usage says how much it wrote to the cache. A turn whose write is most of its context rebuilt the cache, after an expiry, a reopen or a compaction.
   - Count those rebuilds per session.
   - Before Irie wakes a session idle past its cache's life, say what the rebuild costs, the way the spawn list says what a session costs (§6.3). Irie wakes sessions with a send, the jot dog's sending back (§4.8), a thaw, or a reopen.
   - Irie causes rebuilds of its own: a freeze of more than an hour (§4.10), close and reopen, and a mass thaw.
3. **The rest of undo.** Pause, notes, and freezing or thawing them all as one step that names its sessions.
4. **Inside and outside.** Mark each op by whether its effect leaves Irie. Inside ops never ask, since undo is there. Outside ops are the ones to confirm or have Mikael weigh. A mass thaw is an inside action with outside effects: each agent signs in again.
5. **Uncompute merged worktrees.** A closed session whose branch is in `live`, and whose worktree is clean, needs only its branch. Remove the checkout, and let reopen add it back. This changes "closing keeps the worktree" (§4.5), so the maintainer decides. It saves about 9 MB a session.
6. **Quantum watch.** The key exchange is post-quantum. The certificates (ECDSA P-256) and SSH keys (Ed25519) are not, which matters only against an attacker who has a quantum computer at the time of connecting.
   - Move Irie's authority to ML-DSA once phones accept such certificates. Go 1.27's `crypto/mldsa` and `crypto/x509` can already make them.
   - Keep `go.mod`'s `go` line at 1.24 or later, since a lower line turns the hybrid off.

## Measured

On the maintainer's Mac on 2026-11-07, the 27 open sessions had:

- read 819 million tokens from the cache
- written 17 million to it
- produced 3.8 million of output
- sent 8,600 uncached

At Opus 5.5's list prices with a one-hour cache, that is 44% of spend re-reading context, 36% writing it and 20% output. Part of the writing is each turn's new tool output, which has to be written once; the rest is rebuilds, which the meter in step 2 would tell apart.

The newest estimates for breaking 256-bit elliptic curves:

- under 500,000 superconducting qubits for minutes (arXiv:2603.28846)
- 26,000 neutral atoms for days (arXiv:2603.28627)

Surveyed experts give a 28–49% chance of such a machine within 10 years.
