# Demo recordings

Scripted, anonymous recordings of Irie in use, made so that short clips can be cut from them later. Each one is rendered from a tape against a throwaway sandbox, never captured from a real desktop. Why and what's next: [the proposal](../proposals/demo-footage.md).

| Recording | Tape | Frame | Length | Shows |
|---|---|---|---|---|
| Walkthrough | `walkthrough.tape` | 1920×1080, 16:9 | about 6 minutes | launch, a task, `@repo` worktrees, `+profile`, many agents, the slot, sessions that need you, a big paste, a failed start, done and idle, closing the window, `irie ctl` |
| Short | `short.tape` | 1080×1920, 9:16 | about 45 seconds | nine agents in the list, one typed on camera, which needs you and is answered |

Captions are text on screen; there is no voice.

## Render

```sh
docs/demo/render.sh                        # every tape
docs/demo/render.sh docs/demo/short.tape   # one tape
```

Each tape becomes `dist/demo/NAME.mp4`, with chapters embedded, and `dist/demo/NAME.chapters.txt`. `dist/` is ignored by git. `render.sh` prints each video's frame size, length and file size. A video can come out shorter than its rehearsal, because vhs drops the frames it cannot capture in time.

It needs [vhs](https://github.com/charmbracelet/vhs) with its ttyd and ffmpeg (ffprobe too), a Chrome or Chromium for vhs to drive, and tmux, Go, git and perl. Without vhs, ttyd or ffmpeg it says what is missing and renders nothing. vhs uploads only when asked to (`--publish` or `VHS_PUBLISH=true`), and `render.sh` runs it in the sandbox's environment, without either.

## Rehearse

```sh
docs/demo/rehearse.sh            # every tape
```

`rehearse.sh` plays a tape through tmux instead of vhs: the same keys, pauses and waits, in a terminal the size vhs would draw. It needs only tmux, Go, git and perl, so it runs where vhs does not. It writes `dist/demo/rehearsal/NAME/`:

- `frames/`: the screen once a second, with its colours (`less -R` shows them).
- `story.txt`: each caption, then the screen a moment after it.
- `chapters.txt` and `timing.txt`: when each chapter starts, and the length.

It fails when a `Wait` times out, and when a frame shows the user's name, host name or home, `/Users/`, or a sandbox path that was not shortened to `~`.

## How it works

- **The sandbox.** `setup.sh` makes `/tmp/irie-demo.XXXXXX` with its own `HOME` (`home/alice`), XDG directories, runtime directory and tmux sockets. Irie's server is `[tmux] socket = "irie-demo-XXXXXX"`, and `TMUX_TMPDIR` points into the sandbox, so not even the socket's directory is shared with a real Irie. It builds `irie` and the fakeagent from this checkout with `-trimpath`, makes three repos (`acme-api`, `acme-web`, `acme-billing`) committed by Alice on a fixed date, and links tmux, git and the render tools into its own `bin`, so `PATH` names no user directory. `setup.sh down ROOT` stops its servers and deletes it. To look around by hand: `root=$(docs/demo/setup.sh)`, then in a new shell `source $root/env.sh` and `irie`.
- **The agents.** Every profile runs the repo's fakeagent (`internal/testutil/fakeagent`), which fires Claude Code's hooks with no model, no tokens and no network. The default profile `claude` (Opus 5.5, max effort) and `+sonnet` go through `claude`, a stand-in that picks the fakeagent's step script for the task from `cues.txt` and shows the task as it was typed. `+fake` takes the step script itself as its task, as in `+fake prompt,ask,stop`. `+local` names a CLI that is not installed, for the walkthrough's failed start.
- **The presenter.** A tmux around Irie whose status line is the caption bar. `Ctrl+G` then a letter is a cue that Irie never sees, run by `cue`: `n` shows the next caption, `p` pastes `paste.txt` (a 381 KB log) as one bracketed paste, `d` detaches Irie's client as closing the window would, and `e` marks the end. vhs's own `Paste` types the clipboard key by key, which would never make a chip.
- **Captions and chapters.** A tape's `# caption:` lines are its captions, in order, and a `# chapter:` line names the chapter its caption opens. Each cue is logged with its time. vhs drops frames it cannot capture in time, so `render.sh` scales the logged times to the video's length.
- **Nothing that names anyone.** The recorded prompt is `alice$`, `HOME` is `home/alice`, `TZ` is UTC, and vhs, ttyd, the browser and the recorded shell see only the sandbox's environment. `render.sh` keeps only the muxer's neutral tags and the chapter titles in each MP4, then scans it for the user's name, host name and home, `/Users/` and sandbox paths.

## Walkthrough chapters

From the rehearsal, which runs 6:10 from the first caption to the end. Each render writes its own `walkthrough.chapters.txt`, timed to the video.

```
0:00 Launch
0:32 A task
1:10 A worktree
1:50 A profile
2:08 Many agents
3:01 The slot
3:30 Needs you
4:13 A big paste
4:44 A failed start
5:01 Done, then idle
5:19 Close the window
6:00 End
```

## After a UI change

1. Run `docs/demo/rehearse.sh`. A `Wait` that times out means the screen no longer shows what the tape waits for: update its pattern, or the keys before it.
2. Read each `story.txt`: every caption should still describe the screen under it. Adjust `Sleep`s and captions. A task's script in `cues.txt` runs on its own clock from its spawn, so keep its timing in step with the tape.
3. Run `docs/demo/render.sh` and watch the result.
