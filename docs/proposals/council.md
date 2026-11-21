# Proposal: Raguel's council

Date: 2026-11-21. Status: the owner asked for it ("can we start self tracking and doing arch angel council review on our workflows and what we are doing and drafting plans to bring to me to offload cost or be more efficient or make any improvements anywhere"), and the first part comes with this proposal. An agent (Claude Code) wrote it and the first cut. It stands in for the issue [AGENTS.md](../../AGENTS.md) asks for before anything sizeable.

**In short.** Irie already writes down everything its agents do, in the journal (DESIGN §5.2), and nobody reads it. Raguel, Ragu 🤝, the arch agent the owner named to start an agent council, reads it for them. His tally of a stretch of work is plain code and costs no tokens; each part of it sits with the arch agent whose charge it touches. Every `arch.raguel.every`, while the user is away, or whenever they ask, he holds a council: one session that runs as him reads the tally, looks closer where a number needs it, and drafts a few plans, each with its evidence, what it saves, what it costs and a brief an agent can build from. The user says yes or no to each. A yes starts the plan as a jot, and the next tally shows what came of it.

## 1. Why

Each call an agent makes reads its whole context again from the prompt cache, so the cost of agents' work is mostly round trips: a call saved is the saving, and a fact looked up in every session is paid for in every session. The journal shows where the round trips go. Over 19 days to 2026-11-21 it held 47,093 calls by 260 sessions; 59% of the 6,233 greps were followed within two calls by a read of what they found, and 964 calls held a hand-written sleep, 811 minutes in all. Cache writes were 36% of spend ([reversible.md](reversible.md)).

Two audits so far found such savings by hand: the tool-scripts audit of 2026-11-01 (tools/land-pr.sh, tools/doc.sh, tools/provenance.sh, mac/snapshot.sh, tools/check.sh) and the offload round of 2026-11-21 (tools/code.sh, tools/habits.sh). Each took an agent about an hour of mining the journal, and each found more than it cost. The council makes it a habit: measured every week, for the price of one session, with the owner deciding every change.

## 2. The tally

`irie ctl council tally [--days N] [--json]` (op `council`) counts a stretch of the journal, 7 days by default, and the stretch before it, so every number comes with the one it is rising or falling from. Nothing in it costs a token.

| Seat | What it counts |
|---|---|
| Ragu 🤝, cost | What the sessions cost, by repository and by profile, and the costliest sessions; prompt-cache rebuilds and their price, by what woke the session (§6.3); calls by tool, the commands agents run most by kind (as `tools/habits.sh` names them) with two examples each, searches followed by a read, runs of the same command, delegations |
| Gabriel 🪽, time | Hand-written waits; the longest tools by the time they took; the user's asks and how long they waited for an answer |
| Raffi ❤️‍🩹, what breaks | Tools that failed, by kind; turns that failed, by error; test runs that failed; jots the dog sent back, by what was left; usage-limit failovers and fallbacks |
| Mikael ⚔️🛡️ | No numbers: he weighs the plans |

It ends with the plans of earlier councils: what the user said, and for a yes, the jot's state and its watch line, then and now.

## 3. The council

- **When.** Each `arch.raguel.every` (off until the user sets it), at a quiet time: the user away from the computer and the plan's weekly use with room left, as `council.Due` weighs it; or at once with `irie ctl council convene`, or Hold a council now in the Arch Agents tab. One council at a time.
- **Who.** `+raguel`, a built-in profile (Opus 5.5 at high effort) whose session runs as him (`--agent raguel`), so his charge (`internal/arch/agents/raguel.md`) is its whole method. The task holds the tally, so his first turn needs no call to read it. The session starts in the council folder, `arch.raguel.folder`.
- **What he may do.** Read, run the tally, and `irie ctl council propose` and `done`. His charge forbids edits, commits, settings, spawns, messages and installs, and the op takes a plan only from the council's own session.
- **The seats.** He weighs each part of the tally as its arch agent would. Mikael weighs every plan: none may make an agent less safe, less honest or less careful with the user's data, or take away a guard, a check or a question the user asked for.
- **The plans.** At most `arch.raguel.plans` (5), best first. Each has its seat, a title to take in at a glance, the evidence, the change, what it saves a week and how that was counted, the effort (small, medium, large), the risk, the line of the tally to watch, where it changes, and a brief that stands alone. The op refuses a plan with a field missing or wrong, and says which, so he can put it in again.
- **The minutes.** `done` takes his summary; the core then writes `<date> Council.html`, for a phone, and `<date> Council.md`, for an agent, into the folder: his summary, the plans, the tally and what the council cost.

## 4. Bringing the plans to the user

The council's card ends with the plans at a glance. The user answers in any of three places: the Arch Agents tab, where each waiting plan is an item of Ragu's with Yes and No; `irie ctl council yes N...` or `no N --why ...` in their own terminal; or telling Ragu in the council's card, which he does for them. Only the user's word counts: from inside an agent, the op refuses a yes or a no, but for the council's own session within `arch.gabriel.word` of a message the user typed there. A new council lets the last one's unanswered plans lapse, and may bring them again.

A yes starts the plan's brief as a jot (§4.8) in its folder, in the group Council, so the jot dog follows it until its work lands.

## 5. What came of it

Every plan names its watch line. Each tally lists the earlier councils' plans with what the user said and, for a yes, where its jot stands, and the council's summary says, by the watch lines, whether the plans the user took paid off. The council's own cost is in the next tally, like any session's: if it costs more than it finds, the numbers say so.

## 6. Settings

| Key | Default | What it does |
|---|---|---|
| `arch.raguel.on` | on | the tally, the op and his row; off, he does nothing |
| `arch.raguel.every` | `0s` | how often he holds a council by himself; `0s` for only when asked |
| `arch.raguel.plans` | 5 | the most plans a council brings |
| `arch.raguel.folder` | `~/Documents/Irie Council` | where the council works and the minutes go |

## 7. Later

- Each seat as a reviewer of its own, if one session's plans come out anchored on its first idea.
- Seats for Uri, Azra and Izzy as they land: lessons, leftovers and notices.
- Other harnesses' calls, once their hooks reach the journal.
- Small plans built without asking, but only behind a setting the user turns on, and only once earlier councils' small plans have paid off.
