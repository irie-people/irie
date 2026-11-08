# Demo recordings

Scripted, anonymous recordings of Irie in use, made so that short clips can be cut from them later. Each one is rendered from a tape or a reel against a throwaway sandbox, never captured from a real desktop. Why and what's next: [the proposal](../proposals/demo-footage.md).

| Recording | Script | Frame | Length | Shows |
|---|---|---|---|---|
| Walkthrough | `walkthrough.tape` | 1920×1080, 16:9 | about 6 minutes | launch, a task, `@repo` worktrees, `+profile`, many agents, the slot, sessions that need you, a big paste, a failed start, done and idle, closing the window, `irie ctl` |
| Short | `short.tape` | 1080×1920, 9:16 | about 45 seconds | nine agents in the list, one typed on camera, which needs you and is answered |
| First look | `first-look.reel` | 2048×1280, 16:10 | about 14 seconds | the Mac app: three agents at work, a fourth task typed into the task bar, starting and going to work, then two cards shown in turn |
| Needs you | `needs-you.reel` | 2048×1280, 16:10 | about half a minute | three agents at work; one asks a question and its card turns red; it is shown, answered in its terminal, and goes back to work |
| Close the window | `close-the-window.reel` | 2048×1280, 16:10 | about 20 seconds | three agents at work; the window closes and opens again, and the cards are where they were, still working |
| Local | `local.reel` | 2048×1280, 16:10 | about 25 seconds | a hard job on `+claude`; a small one typed for `+local`, which starts on Irie's own Ollama server and finishes |

Tapes film the terminal sidebar with vhs; reels film the Mac app itself ([Mac app recordings](#mac-app-recordings)). Captions are text on screen; there is no voice.

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
- **A local model.** A tape or reel with the line `# irie-demo: local = ollama` gets a `+local` that runs instead. Irie's own Ollama server is then the repo's fakeollama (`internal/testutil/fakeollama`) with one model, `qwen3.5:9b`, at a free port. Irie starts it as it would Ollama, and a `+local` spawn takes Irie's own path: the preflight checks the model's context and the memory it takes, and the agent is pointed at the server. The agent is still the stand-in, so no model runs. The core reads a fixed machine, 16 GiB with 10 free (`IRIE_FAKE_MEMORY`), so the memory check comes out the same on every Mac.
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

## Mac app recordings

```sh
docs/demo/record.sh                             # every reel
docs/demo/record.sh docs/demo/first-look.reel   # one reel
```

Each reel becomes `dist/demo/NAME.mp4`, filmed at 2x (a 1024×640 window gives 2048×1280) at 30 frames a second, and `dist/demo/NAME.events.json`: what the reel did, and when, in milliseconds from the first frame, to place zooms and captions on later. It needs the app built (`mac/build.sh`, or `IRIE_RECORD_APP` naming another `Irie.app`), ffmpeg and ffprobe, swiftc, and what `setup.sh` needs.

A reel reads much as a tape does, one step a line:

| Step | Does |
|---|---|
| `Set Size 1024x640` | the window, in points; at least 820×500 |
| `Set Appearance light` | light or dark |
| `Set Palette evergreen` | rosewater, terracotta, lilac or evergreen |
| `Set Backdrop E9EBDF` | the colour in the window's rounded corners, and in its place while it is closed; black unless set |
| `Set TypingSpeed 55ms` | the pause between typed keys |
| `Spawn @acme-web +sonnet fix the flaky checkout test` | a session `record.sh` starts with `irie ctl spawn --wait` before filming, in order |
| `Sleep 1.5s` | waits; `500ms` works too |
| `Type "@acme-billing retry failed webhooks"` | types into the task bar |
| `Enter` | starts what the task bar holds |
| `Down 2`, `Up` | shows the next or previous card, as many times as asked |
| `Show 3` | shows the third card in the list |
| `Answer "Green"` | types into the shown session's terminal at the typing speed, then Return, as a person answering there would; the keys go through the core, as the phone's do |
| `Close` | closes the window, which quits the app; the film shows the backdrop where the window was, and the agents run on in the core's tmux |
| `Open` | opens the window again with nothing kept from before, as a fresh launch does: what it shows comes from the core |
| `Wait working 20s` | waits until the newest session, started since the last `Enter`, is in that state |

Tasks follow `cues.txt`, as in the tapes, so a typed task does what its words say there.

How it works:

- **The sandbox and the copy.** `record.sh` makes a sandbox with `setup.sh`, starts its Irie with `irie up` and the reel's sessions with `irie ctl spawn`, all in the sandbox's environment only. It films with a copy of `Irie.app` whose bundle id ends in `.recording`, so the copy's defaults are its own: the reel types through the defaults that hold the task bar's text, and the installed app's hold the user's unsent task. `Irie --record` refuses to run under any other bundle id. The copy's defaults are deleted after each reel.
- **Filming.** `Irie --record OUT.mp4 --reel FILE --socket PATH` opens the app's window on the sandbox's core, far off any screen as `--snapshot` does, never takes the focus, and plays the reel. The strip under the terminal keeps to one phrase, "Take back your time. ⏳", rather than one picked at random, so a film is the same on every run. Each frame is the window read back from the window server, its terminal and animations included, which macOS allows an app for its own windows without the Screen Recording permission. The window is read back on the beat, and each picture converted to sRGB with Accelerate and encoded on a queue of its own, so a slow frame never holds up the next reading. On a quiet Mac a frame at 2x takes about 30 ms in all, and the film runs at 30 frames a second; `--fps 60` asks for more. A busy Mac gives fewer, so the line it prints says how many came and what a frame took to read and to encode. `--size`, `--appearance`, `--palette` and `--backdrop RRGGBB`, the colour in the window's rounded corners, override the reel.
- **Nothing that names anyone.** As with the tapes, the video keeps only the muxer's neutral tags, and its file is scanned for the user's home, `/Users/` and the sandbox's paths. Its frames are scanned too: once a second, Apple's Vision framework reads their text on the Mac (`ocr.swift`), and `record.sh` fails when it shows the user's name, full name or host name, or a path from outside the sandbox's `~`.

## Cuts

```sh
docs/demo/cut.sh                              # every reel's cuts
docs/demo/cut.sh docs/demo/needs-you.reel     # one reel's
docs/demo/cut.sh --gif                        # the README's clip
```

`cut.sh` cuts each film `record.sh` made, as the reel's `# cut:` lines say, into:

- **The site's cut**, `site/media/NAME.mp4` and `NAME.webm`, 1344×840 (twice the widest box) at 30 frames a second with no sound, and `NAME.jpg`, the frame its box shows before it plays. Each box's `<video>` plays only when asked.
- **The phone cut**, `dist/demo/NAME.phone.mp4`, 1080×1920 for Instagram Reels and Threads: shots that crop the film to 9:16, the cards at twice their size or the shown agent's terminal below its first line, with captions drawn by a headless Chrome and a silent sound track. The terminal's first line, the fakeagent's banner, stays out of every shot.
- **The README's clip**, `docs/media/irie.gif`: each reel's `readme` part in turn, 800×500 at 12 frames a second.

A reel's cut lines name moments by what the film did, such as `reached:needs_you-2.5` or `opened+1`, from its `NAME.events.json`, so a film recorded again after a UI change cuts the same way. A `jump` leaves time out of every cut, such as a long wait while a reopened window connects on a busy Mac, or most of a stand-in agent's run, whose length its script sets; it never leaves out what a caption speaks of. The lines are listed at the top of `cut.sh`. Every caption must be true of the picture under it, and uses Irie's fixed lines (docs/PHRASES.md) only whole, word for word.

| Reel | Site box | Phone captions |
|---|---|---|
| `first-look` | Home | "Irie runs your AI coding agents side by side, in one calm view." "Type a task. It gets its own worktree and card." |
| `needs-you` | Get started, "a card turns red" | "A card turns red when an agent needs you." "Answer it. The rest stay calm." |
| `close-the-window` | How it works, "Each agent runs in tmux" | "Close the window. They keep working." "Open it again: nothing stopped." |
| `local` | Get started, a first task | "Small job. Local model. $0 per token." "Hard job: the strongest model." |
