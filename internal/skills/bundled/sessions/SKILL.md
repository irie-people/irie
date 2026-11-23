---
name: sessions
description: Find, bring back, sort and start up Irie's sessions. Use after a restart or power cut emptied the list ("restore my sessions", "my cards are gone"), to reopen a closed one, to put cards in a space or group of their own, or to start cut-off cards again.
license: Apache-2.0
---

# Irie's sessions: find, bring back, sort, start up

Each card in Irie's list is a session: an agent in a tmux pane. Everything here goes through `irie ctl`, which talks to the running Irie, so nothing needs a window. Leave the cards the person didn't name alone: only they move, park or close a card.

## Find

- `irie ctl list` shows the open sessions, Parked last. With `--json` each row also gives its `space`, `group`, `pinned` and `parked`.
- `irie ctl history [WORDS...]` lists the closed ones, newest first, whose name or id has every word. `lost` after the time marks the sessions a restart took. Only the newest 100 are there.
- `irie ctl conversation ID | tail` shows how a session's last turn ended. A turn that failed ("Login expired", "Credit balance is too low") never did its task, and one cut off mid-turn sits idle.

## After a restart

A restart or a power cut ends Irie's tmux server, and every card goes with it. The next start records each of them as closed on its own, marked `lost`.

1. Check that the list is empty and history shows the `lost` ones: `irie ctl list`, then `irie ctl history`.
2. Look in the core's log (`core.log` in Irie's state folder, `~/.local/state/irie` by default) for the `msg=lost` lines. One with `resume=false` never had a conversation, so reopening it starts its task over and spends tokens. Name it to the person rather than reopening it.
3. Bring the rest back with `irie ctl reopen --lost`, in the background, since it takes a few seconds a session. It reopens one at a time, each once the one before has started, and only while a quarter of the memory is free. Each comes back with its conversation, in its place, its space and its group, pinned, parked or frozen as it was. If it stops for memory, run it again later and it goes on with the rest.
4. Tell the person how many came back and what is left.

To bring back one closed session, use `irie ctl reopen ID` or its name. Reopening costs nothing until the session's next turn.

## Start them up again

Only start up what the person asked for. A session's next turn may first rebuild its prompt cache, and `irie ctl thaw` prints what that costs.

- `irie ctl thaw ID` lets a frozen one go on.
- `irie ctl continue ID` sends one on, as typing "continue" would. Use it for a card cut off mid-turn, or one whose turn failed for a reason that is fixed now, such as a sign-in.
- When several cards wait on the same job, as a daily retry makes, send one on. Tell the person about the others and let them close those.

## Spaces and groups

- `irie ctl spaces` lists the spaces and how many sessions each holds. `irie ctl spaces new NAME`, `rename SPACE NAME`, `move SPACE [BEFORE]` and `remove SPACE` change them.
- `irie ctl space NAME [SESSION...]` moves sessions, by id or name, to the space of that name, and makes the space when none has that name. `--group GROUP` moves a whole group. It is one step of the history, so one undo takes it back.
- `irie ctl group NAME [SESSION...]`, `irie ctl ungroup [SESSION...]` and `irie ctl nest GROUP [INTO]` do the same for groups. A path such as `Work/Irie` makes a group inside another.

## Start new work

`irie ctl spawn --name NAME [@REPO] [+PROFILE] - < brief.md` starts a session and prints its id. Make the brief stand on its own. Give the person's words as they said them, where to start reading, what to leave alone, and how to finish and report. `--group NAME` puts the new session in a group.

## Limits

- Closed sessions older than the newest 100 can't be reopened. `irie ctl fork ID` carries a conversation on for 30 days after its last turn.
- A session whose folder was deleted can't come back.
