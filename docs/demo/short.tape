# Irie short: under a minute at 9:16, from its own tape rather than a crop
# of the walkthrough. Render it with docs/demo/render.sh, or rehearse it
# without vhs with docs/demo/rehearse.sh. Presenter keys and captions work
# as in walkthrough.tape.
#
# A tall frame gives the sidebar room and the slot little, so this one is
# about the list: eight agents start before the recording does, through the
# command bar so their windows fit the slot, and one task is typed on
# camera. The sidebar is 30 columns, which leaves the slot 29, and lines
# are 1.3 high so the list fills more of the frame.
#
# irie-demo: sidebar_width = 30
# irie-demo: captions = top

Output short.mp4

Require tmux
Require irie

Set Shell "bash"
Set FontFamily "Menlo, DejaVu Sans Mono, monospace"
Set FontSize 28
Set LetterSpacing 1
Set LineHeight 1.3
Set Width 1080
Set Height 1920
Set Padding 28
Set Framerate 25
Set TypingSpeed 40ms
Set CursorBlink false
Set Theme { "name": "Irie", "background": "#0F1714", "foreground": "#DCE5E0", "selection": "#1E5A42", "cursor": "#F2B544", "cursorAccent": "#0F1714", "black": "#1A2420", "red": "#F25F5C", "green": "#4CC38A", "yellow": "#F2B544", "blue": "#5AA9E6", "magenta": "#C38FD9", "cyan": "#45C8C0", "white": "#DCE5E0", "brightBlack": "#5B6B63", "brightRed": "#FF7B78", "brightGreen": "#6FDBA6", "brightYellow": "#FFD27F", "brightBlue": "#84C2F0", "brightMagenta": "#D7AFE8", "brightCyan": "#74E0D8", "brightWhite": "#F5FAF7" }

Hide
Type@10ms `source "$DEMO_ROOT/env.sh" && exec tmux -S "$DEMO_ROOT/run/presenter" -f "$DEMO_ROOT/presenter.conf" new-session -s demo`
Enter
Sleep 1s
Type "irie"
Enter
Sleep 2s
Type@5ms "research rate limiting libraries for Go"
Enter
Sleep 300ms
Tab
Type@5ms "@acme-api add a health check endpoint"
Enter
Sleep 300ms
Tab
Type@5ms "@acme-web +sonnet fix the flaky checkout test"
Enter
Sleep 300ms
Tab
Type@5ms "@acme-billing tidy the webhook retries"
Enter
Sleep 300ms
Tab
Type@5ms "@acme-api add rate limiting to the login endpoint"
Enter
Sleep 300ms
Tab
Type@5ms "@acme-web write tests for the cart"
Enter
Sleep 300ms
Tab
Type@5ms "@acme-api bump the go version"
Enter
Sleep 300ms
Tab
Type@5ms "@acme-web translate the signup page"
Enter
Sleep 1s
Up@10ms 8
Space
Sleep 1s
# chapter: Short
# caption: Every coding agent, side by side.
Ctrl+G
Type "n"
Sleep 200ms
Show

Sleep 4s
# caption: Scripted stand-ins here: no model, no tokens.
Ctrl+G
Type "n"
Sleep 3s
# caption: Type a task. It gets its own agent and worktree.
Ctrl+G
Type "n"
Sleep 1s
Tab
Type "@acme-web pick a colour for the signup button"
Sleep 600ms
Enter
Sleep 3s
# caption: Every row follows its agent, live.
Ctrl+G
Type "n"
Wait+Screen@30s /●[0-9]+ ●[1-9]/
Sleep 1s
# caption: Red: an agent needs you.
Ctrl+G
Type "n"
Sleep 3s
Enter
Sleep 2s
# caption: Answer it, and it carries on.
Ctrl+G
Type "n"
Sleep 1s
Type "Green"
Sleep 500ms
Enter
Sleep 4s
Ctrl+]
Wait+Screen@30s /✓ pick-a-colour/
Sleep 3s
# caption: Irie. Everything irie.
Ctrl+G
Type "n"
Sleep 4s
Ctrl+G
Type "e"
