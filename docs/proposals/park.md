# Park: product requirements

Date: 2026-10-27. Status: requirements, with what this change builds and what is left. It covers park and unpark from end to end: what they are for, every way in, what the core keeps, undo, and the edge cases. An agent (Claude Code) wrote it, from `live` at `c1f6dc5`, after the owner reported that unpark did not work. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

**In short.** Park puts a session out of the way, in a Parked group at the end of the list, without closing it: its agent keeps running, its conversation and worktree stay, and it still turns red when it needs you. Unpark puts it back where it was. Both sidebars, the core and its socket had park; what broke was taking a park back with the mouse. A card parked from its hover button jumped to the end of the list at once, and whatever slid under the pointer took the next click: Parked's header, which folds, or the next card's park button. This change keeps such a card where it is until the pointer leaves it, so a second click on the same spot unparks it. It also adds undo and redo for park, ⌘Z in the Mac app for the whole history, `irie ctl park` and `unpark`, parked sessions in `irie ctl list`, and "parked" on Irie's status line.

| | Before this change | After |
|---|---|---|
| Park from a card's Ⓟ, then click the same spot | The card is gone; the click folds Parked or parks the next card | The card is still there, parked; the click unparks it |
| Take back a park | Find the card in Parked and unpark it there, or drag it out | The same, or Undo in the notice, or ⌘Z, or `u` in the terminal sidebar |
| Undo in the Mac app | None | Edit > Undo and Redo for the core's history: park, unpark, close, reopen, start |
| From a script or an agent | Not possible | `irie ctl park [SESSION]`, `irie ctl unpark [SESSION]` |
| `irie ctl list` | Parked sessions mixed in, unmarked | Listed last, under PARKED |
| Irie's status line, parked and finished | `✓ to verify` | `✓ parked` |

---

## 1. Why park

The list never reorders itself: only you move a card, by dragging, promoting, parking or closing it (DESIGN §4.1). A state change changes a card's mark and colours and never its place. So finished work stays where it is, marked *to verify*, until you have checked it. Then you either close it, which ends its agent, or park it, which only moves it out of the way. Park is for:

- a finished task you have checked but want to keep at hand: its conversation, its worktree, its terminal;
- a session that waits on something slow, such as a review or a deploy, until it needs you again;
- anything you want out of sight without losing it.

An earlier State view filed cards into Needs you, Working and To verify by itself, which moved a card whenever its state changed. The owner asked for a list that holds still and for parking done by hand instead, and the List view and Parked replaced it (d7a2406). A second request made a parked card go out of sight at once and out of the To verify count, with a hover button (daac737).

## 2. What went wrong with unpark

On 2026-10-27, Irie's journal and the Mac app's log showed one attempt from start to finish. A card was parked from its hover button at 19:42:36 UTC. Two clicks followed in the next six seconds, then a menu item; none of them reached the core. A drag out of Parked at 19:42:51 finally unparked it. An earlier park, at 18:41 UTC, was never taken back at all.

The core's `park` and `unpark`, the card's menu, `z` and the drag all worked. The trouble was where things were after the first click. Measured in the app with fixture sessions:

| Parked from its Ⓟ | What is under the pointer straight after |
|---|---|
| The last card of the list | Parked's header: a click folds Parked, and the card just parked goes out of sight |
| A card in the middle | The next card's Ⓟ: once the pointer moves, a click parks that card instead |
| A parked card, unparked from Parked | Another parked card, while the unparked one jumps back to its place far up the list |

So "click it again to undo" did the wrong thing every time, and a card's own button was never under the pointer when it was wanted.

## 3. Requirements

**Status:** *done* was on `live` before this change; *new* comes with it; *later* is not built.

| # | Requirement | Status | Where |
|---|---|---|---|
| R1 | Park and unpark any session. Parking leaves its place in the one order, so unpark brings it back there | done | core `park`, `unpark` (DESIGN §2.4, §4.1) |
| R2 | Parked is a group at the end of the list, after the List or the repo groups and before Outside Irie, in the list's order | done | both sidebars |
| R3 | Parked starts folded, so a session parked goes out of sight at once; once opened it stays open. Folded, its header says how many sessions it holds and how many need you, in red | done | both sidebars |
| R4 | A parked session keeps running and still turns red when it needs you, and so does Parked's header | done | DESIGN §10, decision 3 |
| R5 | A parked session is not *to verify*: it leaves the count, and a finished one says Parked under a quiet name | done | both sidebars |
| R6 | Park survives restarts of the app, the sidebar and the core, and installs | done | journal replay of `ui.parked`, `ui.unparked` |
| R7 | Promoting a parked session unparks it. A closed session comes back unparked when it is reopened | done | core |
| R8 | Every way in: Mac app, a card's Ⓟ on hover, its menu, the details panel's Ⓟ, File > Park Session, `z` and `⇧Z` while the list has the keyboard, a drag across Parked's edge, VoiceOver's actions; terminal sidebar, `z`, `Z`, `:park`, `:unpark` and the drag | done | |
| R9 | A park or unpark done on a card under the pointer leaves the card where it is, in its new state, until the pointer leaves it; a second click on the same spot takes it back | new | Mac app |
| R10 | Park and unpark are steps of the core's one history: undo and redo take them back, from either sidebar | new | core (DESIGN §4.5) |
| R11 | The notice after a park or unpark offers Undo, in the place that never moves | new | Mac app |
| R12 | Edit > Undo and Redo (⌘Z, ⇧⌘Z) step through the whole history, name the step, and ask the core's questions in an alert | new | Mac app |
| R13 | `irie ctl park [SESSION]` and `irie ctl unpark [SESSION]`, by id or name, or with none the session the command runs in, so an agent can park itself | new | CLI |
| R14 | `irie ctl list` lists parked sessions last, under PARKED, as both sidebars do | new | CLI |
| R15 | Irie's status line says `✓ parked` for a parked session whose turn is over, not `✓ to verify` | new | status line |
| R16 | The details panel says a session is parked, and Parked's header says on hover how to unpark | new | Mac app |
| L1 | Promote has the same slide under the pointer: the card jumps to the top and the next card's ↑ arrives under the pointer. The same lingering would fix it | later | |
| L2 | Park several at once, such as every finished session you have seen, as one step you take | later | |
| L3 | How long a session has been parked, in the details | later | |
| L4 | A shortcut that parks while the terminal has the keyboard; `z` needs the list to have it | later | |
| L5 | Rename, freeze, priority and reorder in the history, as DESIGN §4.5 plans | later | |

## 4. Behaviour

**Lingering (R9).** The card the pointer is on is the one the user is looking at, so it must not move under them.
- A park or unpark from a card's Ⓟ button or the card's own menu, while the pointer is on that card, changes the session at once, in the core and on the card: its Ⓟ fills or empties, a finished one says Parked, the To verify count follows. The card stays in the group it was drawn in.
- When the pointer leaves the card, the card goes where it now belongs, with a short animation. It also goes when a drag starts and when the app stops being the front app.
- Clicking the same Ⓟ again before the pointer leaves takes it back: the card is where it is drawn, so nothing moves at all.
- From the keyboard (`z`), the menu bar, the details panel or VoiceOver, the card moves at once, as before. The selection goes with it.

**Undo (R10 to R12).** The core records `park` and `unpark` steps, labelled `park "api-fix"`, when the op changed something. Parking a parked session records nothing; nor does promote's unpark, or what undo and redo do themselves. Undoing a park unparks; redoing it parks again. The core asks first when the step no longer fits:
- the session is closed, or gone: skip is recommended;
- the session is that way already, such as a park undone after the session was unparked by hand: settle is recommended, which moves the history on and changes nothing.

In the Mac app, ⌘Z and ⇧⌘Z go to the history unless a text field has the keyboard and has typing to take back. The menu reads **Undo Park “api-fix”**. A question comes as an alert, one button per answer, the recommended one first.

**The CLI (R13, R14).** `irie ctl park` takes a session by id or name, as `ctl close` does; with none, it parks the session it runs in (`$IRIE_SESSION`). `ctl list` keeps the core's order and prints the parked sessions after the rest, under a PARKED heading; `--json` is unchanged and carries `parked` on each row.

## 5. Acceptance

- **Core.** Park, undo, redo; undo of a park after the session was unparked asks, and settle moves on; a park that changes nothing records no step (`internal/core`). Park keeps the order through a restart and promote unparks (`TestParkKeepsThePlaceThroughARestartAndPromoteUnparks`).
- **Mac app.** Checked in-process with fixture sessions: parking the last card or a middle one from under the pointer leaves the card in place, parked; a second park or unpark there takes it back with no move; once the pointer leaves, the card goes to Parked, and an unparked one back to its place; `z` moves at once; the notice offers Undo, which calls the core's `undo`.
- **By hand, in the running Irie.** Park the last card with its Ⓟ, then click the same spot: it is unparked, in place. Park it again and move the pointer away: it goes to Parked. ⌘Z: it comes back. `irie ctl park` from an agent's own terminal: its card goes to Parked.

## 6. Open questions

1. Unpark brings a card back to its old place, which may be far up a long list. Should it come back last instead, beside Parked, where the eye already is?
2. Should parking also freeze, to give a long-parked session's CPU back? Freeze exists on its own today.
3. Which shortcut, if any, should park while the terminal has the keyboard (L4)?
