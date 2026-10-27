# Proposal: demo footage

Date: 2026-10-26. Status: proposed, with a first cut in [docs/demo/](../demo/README.md). There is no issue tracker yet, so this file stands in for the issue that AGENTS.md asks for. An agent (Claude Code) wrote this proposal and the first cut.

## Why

Clips of Irie in use for the README, posts and talks. They must be **reproducible**, so they can be made again after any UI change, and **anonymous**, so nothing in them names a person or a machine. So they are scripted recordings of a sandboxed Irie, never a capture of a real desktop.

## What each recording shows

Long-form videos with chapters, from which short clips are cut. Captions are text on screen; there is no voice.

**Walkthrough** (16:9, 1920×1080, about six minutes):

| Chapter | Shows |
|---|---|
| Launch | `irie` from a shell; the sidebar and the slot with its keys |
| A task | Typing a task; the row going from starting to working to ✓; Space showing the agent in the slot |
| A worktree | `@acme-` and Tab listing repos; `@acme-api` making a worktree on its own branch; `@acme-billing!` running in the repo's own checkout |
| A profile | `+so` and Tab completing `+sonnet`; what a profile is |
| Many agents | Four more tasks; the header's counts; Up, Down and Space through the list |
| The slot | Enter puts you in an agent's terminal; typing to it; Ctrl+] back |
| Needs you | A question turns a row red; Enter, answer, it carries on; a permission prompt does the same |
| A big paste | A 381 KB log becomes a chip; the task reaches the agent as a file |
| A failed start | A profile whose CLI is missing fails at once, with exit 127 and the reason |
| Done, then idle | ✓ until you look, then ○ |
| Close the window | Detach; `irie ctl list`; `irie ctl spawn` with no window; `irie` again |

**Short** (9:16, 1080×1920, under a minute): nine agents in the list, one typed on camera, which turns red, is answered and finishes.

## How the sandbox works

`docs/demo/setup.sh` builds `/tmp/irie-demo.XXXXXX`: its own HOME (`home/alice`), XDG directories, runtime directory and tmux sockets, three neutral repos committed by Alice on a fixed date, and `irie` and the fakeagent built from the checkout with `-trimpath`. Every profile runs the fakeagent, so agents change state on cue with no model, no tokens and no network. A stand-in `claude` picks each task's step script from `cues.txt`, so the task on screen is the one that was typed. A second tmux around Irie, the presenter, draws the caption bar and takes cue keys that Irie never sees: next caption, paste, detach.

`render.sh` runs vhs in the sandbox's environment only, strips each MP4's metadata, embeds chapters from the logged cues, and scans the result. `rehearse.sh` plays the same tapes through tmux without vhs and saves the screen every second. Both fail when anything from outside the sandbox shows.

## Re-rendering after a UI change

Run `docs/demo/rehearse.sh`, read each `story.txt`, fix the tapes, captions and `cues.txt` timings it shows to be stale, then run `docs/demo/render.sh` and watch. `Wait` lines tie the tapes to what the screen shows, so a changed UI fails loudly instead of recording the wrong thing.

## Does Irie's layout read at 9:16?

Only partly. The sidebar reads well at a phone-sized font: the rows, glyphs, red rows and header counts are all clear in 60 columns. The slot does not. At 28 px, a 1080-pixel-wide frame holds 60 columns, and with a 30-column sidebar the slot has 29, so an agent's lines wrap mid-word, and a real agent's screen, laid out for a full terminal, would fare worse. A smaller font makes the slot usable but the text too small for a phone. The tall frame is also mostly empty below the list. The short is therefore about the list, not the slot. A stacked layout for tall windows, with the list above the slot, would fix this; see below.

## Open decisions

1. **Where the videos live.** `dist/` is ignored, so renders are built, not committed. Should a release attach them, or should they sit in git LFS or a separate repo?
2. **Formats.** MP4 only for now. Do we also want GIF or WebM for the README?
3. **Font.** Menlo, or DejaVu Sans Mono on Linux, which the rehearsal's size maths assumes. JetBrains Mono, vhs's default, would need the maths changed and the font installed.
4. **The short's frame.** Captions sit at the top because platforms cover the bottom of a 9:16 frame. Do we want a list-only short, or wait for a stacked layout?
5. **CI.** Rehearse on every UI change (it needs only tmux), and render on release?

## What's left

- **Render and watch.** vhs and ttyd were not installed on the machine that made the first cut, so nothing has been rendered with vhs yet; the tapes have only been rehearsed. Still unchecked: fonts and glyphs in the browser, vhs's frame drops (which `render.sh` corrects chapters for), the exact terminal size (the rehearsal's sums may be a column out), and how the 256-colour palette looks.
- **Irie: colours.** The sidebar picks its colour profile once, when it starts. Under tmux, Bubble Tea ignores COLORTERM and reads `tmux info`. That lists no terminal before a client attaches, and after one attaches it lists only terminfo flags. Irie never asks the terminal for `RGB`. So the sidebar draws in 256 colours: the sun (#F2B544) as #FFAF5F, the deep green (#1E5A42) as #005F5F. In the sandbox this held with a client attached too. A fix in `internal/ui`: request the capability, or set the profile from what the core knows of the client (`client_termfeatures` lists RGB).
- **Irie: a fixed clock or demo mode.** Ages ("now", "42s") and ids come from the real clock, so renders match in content but not frame for frame.
- **Irie: a stacked layout for tall windows**, for 9:16.
- **Irie: `~` in tool details.** The status strip shows a tool's file path in full. The demo's scripts avoid `Read` and `Edit` for that reason.
- **Irie: a spawn's summary hides the selected row's detail** until a key is pressed, so a tape cannot wait on a new session's detail. The tapes wait on the header's counts instead.
- **Irie: the strip cuts the spawn summary at the sidebar's width**, so "copied .env" is never seen at 34 columns.
- **fakeagent:** tools that take time (each finishes at once, so the strip never shows a running tool), a way to take the step script from the environment so the stand-in is not needed, and fuller output.
