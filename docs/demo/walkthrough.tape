# Irie walkthrough: about six minutes at 16:9, to cut clips from later.
# Render it with docs/demo/render.sh, or rehearse it without vhs with
# docs/demo/rehearse.sh. The agents are the repo's fakeagent, and cues.txt
# says what each task does and when (docs/demo/README.md).
#
# Ctrl+G then a letter is a presenter key, which Irie never sees: n shows
# the next "# caption:" below, p pastes the big log, d closes Irie's window
# (detaches its client), e marks the end. A "# chapter:" line names the
# chapter that its caption opens.
#
# irie-demo: sidebar_width = 34
# irie-demo: captions = bottom

Output walkthrough.mp4

Require tmux
Require irie

Set Shell "bash"
Set FontFamily "Menlo, DejaVu Sans Mono, monospace"
Set FontSize 28
Set LetterSpacing 1
Set LineHeight 1.0
Set Width 1920
Set Height 1080
Set Padding 28
Set Framerate 25
Set TypingSpeed 45ms
Set CursorBlink false
Set Theme { "name": "Irie", "background": "#0F1714", "foreground": "#DCE5E0", "selection": "#1E5A42", "cursor": "#F2B544", "cursorAccent": "#0F1714", "black": "#1A2420", "red": "#F25F5C", "green": "#4CC38A", "yellow": "#F2B544", "blue": "#5AA9E6", "magenta": "#C38FD9", "cyan": "#45C8C0", "white": "#DCE5E0", "brightBlack": "#5B6B63", "brightRed": "#FF7B78", "brightGreen": "#6FDBA6", "brightYellow": "#FFD27F", "brightBlue": "#84C2F0", "brightMagenta": "#D7AFE8", "brightCyan": "#74E0D8", "brightWhite": "#F5FAF7" }

Hide
Type@10ms `source "$DEMO_ROOT/env.sh" && exec tmux -S "$DEMO_ROOT/run/presenter" -f "$DEMO_ROOT/presenter.conf" new-session -s demo`
Enter
Sleep 1s
# chapter: Launch
# caption: Irie runs your coding agents side by side, in one calm view.
Ctrl+G
Type "n"
Sleep 200ms
Show

Sleep 6s
# caption: The agents here are scripted stand-ins: no model, no tokens, no network.
Ctrl+G
Type "n"
Sleep 6s
# caption: One command starts it.
Ctrl+G
Type "n"
Sleep 1s
Type "irie"
Sleep 700ms
Enter
Sleep 3s
# caption: Your sessions go on the left. The slot on the right shows one agent's live terminal.
Ctrl+G
Type "n"
Sleep 8s
# caption: Until you pick a session, the slot lists the keys.
Ctrl+G
Type "n"
Sleep 7s

# chapter: A task
# caption: Type a task and press Enter. Irie starts an agent for it.
Ctrl+G
Type "n"
Sleep 2s
Type "research rate limiting libraries for Go"
Sleep 1s
Enter
Sleep 3s
# caption: Its row follows the agent's own hooks: starting, then working.
Ctrl+G
Type "n"
Sleep 6s
# caption: Space shows the selected session in the slot, and you stay in the sidebar.
Ctrl+G
Type "n"
Sleep 1s
Space
Sleep 7s
# caption: The agent's tool calls scroll by in its own terminal.
Ctrl+G
Type "n"
Wait+Screen@60s /✓1/
Sleep 1s
# caption: A tick: it finished its turn. The row stays green until you look again.
Ctrl+G
Type "n"
Sleep 7s

# chapter: A worktree
# caption: @repo runs the task in a new git worktree of that repo.
Ctrl+G
Type "n"
Sleep 2s
Tab
Sleep 500ms
Type "@acme-"
Sleep 500ms
Tab
Sleep 1s
# caption: Tab completes repo names and profiles.
Ctrl+G
Type "n"
Sleep 3s
Type "a"
Sleep 300ms
Tab
Sleep 1s
Type "add rate limiting to the login endpoint"
Sleep 1s
Enter
Sleep 2s
# caption: It gets its own branch, irie/ and the task's name, with the repo's untracked .env copied in.
Ctrl+G
Type "n"
Sleep 7s
Space
Sleep 7s
# caption: @repo! runs it in the repo's own checkout instead, with no worktree.
Ctrl+G
Type "n"
Sleep 1s
Tab
Type "@acme-billing! fix a typo in the readme"
Sleep 700ms
Enter
Sleep 7s

# chapter: A profile
# caption: +profile picks the agent: harness, model, effort and permission mode.
Ctrl+G
Type "n"
Sleep 2s
Tab
Sleep 500ms
Type "@acme-web +so"
Sleep 500ms
Tab
Sleep 1s
Type "fix the flaky checkout test"
Sleep 1s
Enter
Sleep 2s
# caption: Profiles are yours to define. Here the default is Opus 5.5 on max effort; +sonnet is Sonnet 5.5.
Ctrl+G
Type "n"
Sleep 8s

# chapter: Many agents
# caption: Start as many as you like. Each runs in its own window, out of sight.
Ctrl+G
Type "n"
Sleep 1s
Tab
Type "@acme-web restyle the signup button"
Sleep 700ms
Enter
Sleep 1500ms
Tab
Type "@acme-billing retry failed webhooks with backoff"
Sleep 700ms
Enter
Sleep 1500ms
Tab
Type "@acme-api bump the go version"
Sleep 700ms
Enter
Sleep 1500ms
Tab
Type "@acme-web write tests for the cart"
Sleep 700ms
Enter
Sleep 3s
# caption: The header counts them: working, needs you, done.
Ctrl+G
Type "n"
Sleep 6s
# caption: Up and Down pick a session, Space peeks at it. The order never changes under you.
Ctrl+G
Type "n"
Sleep 1s
Up
Space
Sleep 6s
Up
Space
Sleep 6s
Up
Space
Sleep 6s
Up
Space
Sleep 6s

# chapter: The slot
# caption: Enter shows the agent and puts you in the slot: your keys go to the agent.
Ctrl+G
Type "n"
Sleep 1s
Up
Up
Up
Sleep 1s
Enter
Sleep 7s
# caption: It is the agent's own terminal. Type to it as you would anywhere.
Ctrl+G
Type "n"
Sleep 1s
Type "now compare the two best ones"
Sleep 700ms
Enter
Sleep 6s
# caption: Ctrl+] takes you back to the sidebar. The agents keep going.
Ctrl+G
Type "n"
Sleep 2s
Ctrl+]
Sleep 5s
Down
Down
Down
Down
Space
Sleep 2s

# chapter: Needs you
# caption: When an agent needs you, its row turns red.
Ctrl+G
Type "n"
Wait+Screen@90s /asks: Which colour/
Sleep 6s
# caption: Enter takes you to it. Answer in its own terminal, and it carries on.
Ctrl+G
Type "n"
Sleep 1s
Enter
Sleep 3s
Type "Green"
Sleep 700ms
Enter
Sleep 3s
Ctrl+]
Sleep 5s
# caption: A permission prompt turns its row red the same way.
Ctrl+G
Type "n"
Sleep 1s
Down
Wait+Screen@90s /needs permission/
Sleep 6s
Enter
Sleep 3s
Enter
Sleep 5s
Ctrl+]
Sleep 3s

# chapter: A big paste
# caption: Paste anything. A paste over 8 KiB becomes a chip.
Ctrl+G
Type "n"
Sleep 1s
Tab
Type "@acme-api find the slow request in this log"
Sleep 500ms
Ctrl+J
Sleep 500ms
Ctrl+G
Type "p"
Sleep 7s
# caption: Enter sends all of it, byte for byte.
Ctrl+G
Type "n"
Sleep 1s
Enter
Sleep 3s
Enter
Sleep 2s
# caption: A task is never typed into the agent's screen. Over 100 KiB, it arrives as a file.
Ctrl+G
Type "n"
Sleep 11s
Ctrl+]
Sleep 2s

# chapter: A failed start
# caption: If an agent cannot start, its row says so, and why.
Ctrl+G
Type "n"
Sleep 1s
Tab
Type "+local summarise the open issues"
Sleep 700ms
Enter
Sleep 6s
# caption: Here the profile's CLI is not installed: exit 127, command not found.
Ctrl+G
Type "n"
Sleep 7s

# chapter: Done, then idle
# caption: A finished row stays green until you look at it. Then it goes quiet.
Ctrl+G
Type "n"
Sleep 1s
Up@150ms 14
Sleep 2s
Space
Sleep 5s
Down@300ms 6
Space
Sleep 6s

# chapter: Close the window
# caption: Close the window and every agent keeps running.
Ctrl+G
Type "n"
Sleep 2s
Ctrl+G
Type "d"
Sleep 3s
# caption: irie ctl drives the same core from scripts.
Ctrl+G
Type "n"
Sleep 1s
Type "irie ctl list"
Sleep 500ms
Enter
Sleep 10s
Type "irie ctl spawn @acme-api update the changelog"
Sleep 500ms
Enter
Sleep 5s
# caption: Run irie again: it is all still there, and the new task too.
Ctrl+G
Type "n"
Sleep 1s
Type "irie"
Sleep 500ms
Enter
Sleep 3s
Wait+Screen@40s /✓ update-the-changelog/
Sleep 5s

# chapter: End
# caption: Irie. Everything irie.
Ctrl+G
Type "n"
Sleep 11s
Ctrl+G
Type "e"
