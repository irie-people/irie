# Provenance

Irie is written from public documentation and public open-source projects. Every source that shaped the code is listed here: what it is, when it was read, and what it was used for.

## Sources

| Date | Source | Used for |
|---|---|---|
| 2026-10-26 | https://code.claude.com/docs/en/hooks.md | Hook events and payload fields (`agent_id`, `transcript_path`, `notification_type`), `additionalContext`, hook timeouts and `onFailure: "block"`: the hook shim, hold at the next tool call, context notes, state mapping (DESIGN §3.3, §4.3) |
| 2026-10-26 | https://code.claude.com/docs/en/settings.md | `--settings` as a per-launch layer whose hook lists merge with the user's: per-session hook injection without editing user files (DESIGN §3.3) |
| 2026-10-26 | https://code.claude.com/docs/en/cli-reference.md | Launch flags (`--model`, `--effort`, `--permission-mode`, `--session-id`, `--resume`): default profile argv and reopen (DESIGN §3.3) |
| 2026-10-26 | https://code.claude.com/docs/en/interactive-mode.md | Enter queues for the next tool boundary, Ctrl+X Ctrl+S sends now, Ctrl+Z behaviour: notes go through hook context; freeze uses signals (DESIGN §3.2, §4.3) |
| 2026-10-26 | https://code.claude.com/docs/en/terminal-config.md | tmux options Claude Code needs (`allow-passthrough`, `focus-events`, `extended-keys`): generated tmux.conf (DESIGN §2.3) |
| 2026-10-26 | https://code.claude.com/docs/en/claude-directory.md ; https://code.claude.com/docs/en/sessions.md | Transcript and subagent transcript locations: transcript tailer and subagent mirrors (DESIGN §4.4) |
| 2026-10-26 | https://code.claude.com/docs/en/worktrees.md | `.worktreeinclude` convention for copying untracked files into worktrees: worktree rules (DESIGN §4.2) |
| 2026-10-26 | https://code.claude.com/docs/en/env-vars.md | `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_AUTO_COMPACT_WINDOW`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`: local-model profiles (DESIGN §6.1) |
| 2026-10-26 | https://code.claude.com/docs/en/llm-gateway | Anthropic does not support Claude Code on non-Claude models: local sessions are tagged best effort (DESIGN §6.1) |
| 2026-10-26 | https://code.claude.com/docs/en/statusline | Status-line JSON with `rate_limits`: input for post-v1 budget-aware routing (DESIGN §6.2) |
| 2026-10-26 | https://code.claude.com/docs/en/legal-and-compliance | Credential-use policy: Irie launches the unmodified CLI under the user's own login and never relays credentials (DESIGN §9) |
| 2026-10-26 | https://github.com/anthropics/claude-code/issues/101603 ; https://github.com/anthropics/claude-code/issues/97583 | Ctrl+Z wedge in orphaned process groups and Ctrl+Z regression: the pane host owns the foreground process group, continues stops it did not cause, and freezes with SIGSTOP (DESIGN §4.3) |
| 2026-10-26 | https://github.com/anthropics/claude-code/issues/96232 | Flag-file PreToolUse pause pattern and its limits (typed input waits while a hook holds): hold design and gate files (DESIGN §4.3) |
| 2026-10-26 | tmux(1) manual for tmux 3.7b, https://github.com/tmux/tmux | Private `-L` server, `swap-pane -d`, `select-pane -d/-e`, `remain-on-exit`, `window-size`/`resize-window`, control mode, `set-titles`, formats: layout, swap-in, read-only mirrors, observation (DESIGN §2.3, §2.5) |
| 2026-10-26 | https://ghostty.org/docs/features/applescript | AppleScript dictionary (Ghostty 1.3.0+): open or focus the Irie window (DESIGN §7.1) |
| 2026-10-26 | https://ghostty.org/docs/config/reference | `title` and `-e` options: window title and the launch fallback (DESIGN §7.1) |
| 2026-10-26 | https://github.com/YoanWai/agent-manager (README, docs/usage.md; Apache-2.0) | Ideas only, no code: private `-L` tmux server, per-session generated `--settings` hooks, hook-first state with pane fallback (DESIGN §2) |
| 2026-10-26 | https://github.com/OndrejDrapalik/gmux (README; MIT) | Idea only: a placeholder pane swapped into the main slot so nothing reflows (DESIGN §2.3) |
| 2026-10-26 | https://github.com/cyclops-team/cyclops (README, findings.md; MIT) | Control-mode costs (`ignore-size`, non-UTF-8 output, once-a-second subscriptions, pane death): polling first, control mode last (DESIGN §2.5, M11) |
| 2026-10-26 | https://github.com/OrchestratorInc/agent-orchestrator (README, docs/architecture.md; Apache-2.0) | Idea only: state derived from events at read time (DESIGN §2.1) |
| 2026-10-26 | https://herdr.dev/docs/agents/ | Layered state detection with hooks first, and "done" kept visible until viewed: observation order, done and idle states (DESIGN §2.5, §5.3) |
| 2026-10-26 | https://github.com/asheshgoplani/agent-deck (README, docs; MIT) | Queue-versus-interrupt steering; its status detector credits an AGPL source, so it is excluded from reuse (DESIGN §8.1) |
| 2026-10-26 | https://mitchellh.com/writing/program-status-osc7501 | OSC 7501 state vocabulary: event-kind mapping (DESIGN §5.2) |
| 2026-10-26 | https://agentclientprotocol.com/protocol/overview | ACP `session/update`, tool-call statuses and stop reasons: event-kind mapping; ACP driving deferred (DESIGN §1.2, §5.2) |
| 2026-10-26 | https://developers.openai.com/codex/hooks ; https://developers.openai.com/codex/app-server | Codex hooks, trust gate, timeouts and `turn/steer`: capability matrix and the first post-v1 adapter (DESIGN §3.2) |
| 2026-10-26 | https://geminicli.com/docs/hooks/reference/ | BeforeTool and its 60 s timeout that lets calls through: capability matrix (DESIGN §3.2) |
| 2026-10-26 | https://opencode.ai/docs/plugins/ | Awaited `tool.execute.before` plugin hook: capability matrix (DESIGN §3.2) |
| 2026-10-26 | https://goose-docs.ai/docs/guides/context-engineering/hooks/ | goose hook timeout and `on_failure`: capability matrix (DESIGN §3.2) |
| 2026-10-26 | https://raw.githubusercontent.com/earendil-works/pi/main/packages/coding-agent/docs/extensions.md ; https://raw.githubusercontent.com/earendil-works/pi/main/packages/coding-agent/docs/rpc-commands.md | pi `tool_call` blocking and RPC `steer`: capability matrix (DESIGN §3.2) |
| 2026-10-26 | https://hermes-agent.nousresearch.com/docs/user-guide/features/hooks | `pre_tool_call` blocks the call on timeout (fails closed): capability matrix (DESIGN §3.2) |
| 2026-10-26 | https://raw.githubusercontent.com/charmbracelet/crush/main/docs/hooks/README.md | PreToolUse-only hooks that let calls through on timeout: capability matrix; crush is FSL, so Irie runs it and copies nothing (DESIGN §3.2) |
| 2026-10-26 | https://ampcode.com/docs/plugin-api | `tool.call` plugin and steering: capability matrix (DESIGN §3.2) |
| 2026-10-26 | https://aider.chat/docs/usage/commands.html ; https://aider.chat/docs/config/options.html | No hooks, only a notifications command: aider stays in the generic tier (DESIGN §3.2) |
| 2026-10-26 | https://docs.ollama.com/context-length.md ; https://docs.ollama.com/api/ps.md | Default context by VRAM, `OLLAMA_CONTEXT_LENGTH`, `context_length` in `/api/ps`: local-model preflight (DESIGN §6.1) |
| 2026-10-26 | https://raw.githubusercontent.com/ollama/ollama/main/docs/api/anthropic-compatibility.mdx | Ollama's `/v1/messages` for Claude Code, without `tool_choice`: local profiles (DESIGN §6.1) |
| 2026-10-26 | https://lmstudio.ai/docs/developer/rest/list ; https://raw.githubusercontent.com/lmstudio-ai/docs/main/1_developer/4_anthropic-compat/index.mdx | LM Studio loaded-instance context and its Anthropic-compatible endpoint: preflight (DESIGN §6.1) |
| 2026-10-26 | https://raw.githubusercontent.com/ggml-org/llama.cpp/master/tools/server/README.md | llama-server `/props`, `/slots`, and `-c` divided across slots: preflight (DESIGN §6.1) |
| 2026-10-26 | https://raw.githubusercontent.com/steipete/CodexBar/main/docs/cli.md | `usage --format json` and `guard`: subscription-window input for post-v1 routing (DESIGN §6.2) |
| 2026-10-26 | https://github.com/NousResearch/hermes-agent/issues/142082 | Request for a pre-load memory admission check: post-v1 RAM admission (DESIGN §6.2) |
| 2026-10-26 | https://github.com/charmbracelet/bubbletea (README, UPGRADE_GUIDE_V2.md) ; https://github.com/charmbracelet/bubbles ; https://github.com/charmbracelet/lipgloss | Bubble Tea v2 stack and module paths: UI dependencies (DESIGN §8.1) |
| 2026-10-26 | Source of Bubble Tea v2.1.0, Bubbles v2.2.1, Lip Gloss v2.0.6 and ultraviolet (Go module cache; https://github.com/charmbracelet/bubbletea, https://github.com/charmbracelet/bubbles, https://github.com/charmbracelet/lipgloss, https://github.com/charmbracelet/ultraviolet; MIT) | API only, no code copied: `Model`/`View`, key and mouse messages and key names, Lip Gloss styles. Textarea: `CharLimit` 0, `DynamicHeight`, `MaxHeight` (which also stops input unless `MaxContentHeight` is set) and its 10,000-line cap, hence refusing longer pastes: the sidebar and command bar (M1) |
| 2026-10-26 | Source and docs of github.com/BurntSushi/toml v1.6.0 (MIT) | API only: `MetaData.Undecoded` to name unknown keys; `Primitive` and `PrimitiveDecode` so a partial profile keeps its defaults (maps decode into fresh values); duration strings: config (M1) |
| 2026-10-26 | tmux 3.7b manual page (local `man tmux`), https://github.com/tmux/tmux | Exact window targets (`session:=name`), commands given as argv without `sh -c`, command lists, `default-size`, `resize-window` setting `window-size manual`, `join-pane`, `break-pane`, `respawn-pane -k`, `set-option -p`, the `window-resized` hook, `list-panes -a -F`: runner, layout repair, swap-in and the spawn command list (M1) |
| 2026-10-26 | Go standard library docs and source, go1.27.3 (https://go.dev/src/syscall/ ; BSD-3-Clause) | `SysProcAttr.Foreground` and `Ctty`, `ForkExec`, `Wait4`; `syscall_bsd.go` reports a SIGSTOP stop as `Continued`, not `Stopped`, so the pane host reads the wait status bits itself (M1) |
| 2026-10-26 | https://specifications.freedesktop.org/basedir/latest/ | XDG defaults, relative values ignored, a runtime dir owned by the user with mode 0700: paths (M1) |
| 2026-10-26 | https://www.rfc-editor.org/rfc/rfc9562.html | UUID version 4 layout and text form: pre-assigned Claude Code session ids (M1) |
| 2026-10-26 | https://www.crockford.com/base32.html | Base32 alphabet, written in lower case, for time-sortable session ids (M1) |
| 2026-10-26 | https://code.claude.com/docs/en/hooks.md (read again for M2) | The settings shape (event, matcher groups, command handlers), `timeout` in seconds, `onFailure: "block"` from 2.1.295, shell form run with `sh -c`, matcher rules, hooks run without a terminal, plain stdout on SessionStart and UserPromptSubmit becoming context (so the shim prints nothing unless it has something to say), `hookSpecificOutput` with `additionalContext` and `permissionDecision`, and each event's input: PermissionRequest without `tool_use_id`, `duration_ms`, PostToolUseFailure's and StopFailure's `error`, the Notification types, SubagentStop's `agent_transcript_path` and its firing for Claude Code's own agents, SessionEnd's `reason` and 1.5 s budget: the settings file, `Normalize`, `Respond`, the shim and the fake agent (M2) |
| 2026-10-26 | https://code.claude.com/docs/en/cli-reference.md ; https://code.claude.com/docs/en/settings.md (read again for M2) | `--settings` takes a path or inline JSON and sits above the user's files, whose hook lists merge with it; `-v, --version`; `-n, --name`; `--help` does not list every flag: the settings flag, the version check, optional flags, and the flags the fake agent accepts (M2) |
| 2026-10-26 | Output of `claude --version` and `claude --help` (Claude Code 2.1.310, run locally; neither starts a session) | The version line `2.1.310 (Claude Code)` and `-n, --name <name>` in the options: parsing the version and finding optional flags (M2) |
| 2026-10-26 | tmux 3.7b manual page (local `man tmux`), https://github.com/tmux/tmux | `capture-pane -p -J -S`, which also reads a dead pane kept by `remain-on-exit`, and its "Pane is dead" line: the screen tail of a stalled or failed spawn (M2) |
| 2026-10-26 | https://pubs.opengroup.org/onlinepubs/9799919799/utilities/V3_chap02.html (§2.2.2 Single-Quotes) | Single quotes keep every character but a single quote: quoting the irie path in hook commands (M2) |
| 2026-10-26 | https://www.rfc-editor.org/rfc/rfc3339 | Timestamp format: the envelope's `ts`, in UTC with milliseconds (M2) |
| 2026-10-26 | Go standard library docs, go1.27.3 (https://pkg.go.dev/os/exec ; https://pkg.go.dev/encoding/json ; https://pkg.go.dev/syscall ; BSD-3-Clause) | `Cmd.Cancel` and `Cmd.WaitDelay` with `Setpgid` and `Setsid` to time out a process group; `Decoder.UseNumber` and `DisallowUnknownFields`: the version probe, the shim, the fake agent's hook runs and strict journal checks in tests (M2) |
| 2026-10-26 | https://code.claude.com/docs/en/worktrees.md (read again for M3) | `.worktreeinclude` sits at the repo root, uses `.gitignore` syntax, and copies only files that match a pattern and are also gitignored: which untracked files a worktree gets. Claude Code's own rule for `**/` patterns inside wholly ignored directories is not copied; git's matching decides (M3) |
| 2026-10-26 | https://code.claude.com/docs/en/permissions.md (section "Project allow rules and workspace trust") ; https://code.claude.com/docs/en/security.md | Workspace trust is keyed on the git repository root, and in a worktree on the main checkout's root: trusting a repo once covers its worktrees, which answers DESIGN §4.2's check (M3) |
| 2026-10-26 | git 2.43.0 manual pages (local `man git`, `git-worktree`, `git-ls-files`, `git-check-ignore`, `git-rev-parse`, `git-status`, `git-submodule`, `git-show-ref`, `gitignore`) and `git rev-parse --local-env-vars`, https://git-scm.com/docs | `worktree add --no-track -b` and `worktree list --porcelain`; `ls-files --others --ignored --exclude-from` and `:(literal)` pathspecs; `check-ignore --stdin -z` and its exit code 1; `rev-parse --path-format=absolute --show-toplevel --git-common-dir` and `--verify --end-of-options`; `status --porcelain=v1 -z`; `submodule update --init --recursive`; `show-ref --verify --quiet`; `GIT_TERMINAL_PROMPT`, `GIT_OPTIONAL_LOCKS`, `GIT_CEILING_DIRECTORIES`, `GIT_CONFIG_GLOBAL` and the variables that point git at another repo: repo lookup, worktrees and test isolation (M3) |
| 2026-10-26 | tmux 3.7b manual page (local `man tmux`), checked on a private test server | `load-buffer`, `paste-buffer -p -d` (bracketed only when the program asked for it, LF sent as CR, control characters other than tabs shown as `^X` unless `-S`), `send-keys Enter`, and the `pane_current_command` and `bracket_paste_flag` formats: pasting a generic agent's task once it runs (M3) |
| 2026-10-26 | Source of Bubbles v2.2.1 (textarea, internal/runeutil), Bubble Tea v2.1.0 and ultraviolet (Go module cache; MIT) | API only, no code copied: the textarea's `InsertString`, `Line`, `Column`, `MoveToBegin` and `SetCursorColumn` for Tab completion; its sanitizer, which turns tabs into spaces and CR into newlines, and `PasteMsg`, which carries a paste as sent: why a paste over 8 KiB becomes a chip outside the textarea (M3) |
| 2026-10-26 | https://en.wikipedia.org/wiki/Levenshtein_distance | Edit distance for the closest repo names when an @name fits none (M3) |
| 2026-10-26 | https://github.com/charmbracelet/vhs (README.md; MIT) | Tape commands and settings, vhs's need for ttyd and ffmpeg, and `vhs publish`: the demo tapes and `render.sh` (docs/demo) |
| 2026-10-26 | Source of vhs, main branch (command.go, parser/parser.go, lexer/lexer.go, shell.go, tty.go, vhs.go, video.go, style.go, themes.go, main.go; https://github.com/charmbracelet/vhs; MIT) | Behaviour only, no code copied. `Paste` types the clipboard key by key, so the big paste goes through tmux's `paste-buffer -p`. `Ctrl+` takes `]` and letters. `Wait+Screen` exists. Strings are raw, and a theme's JSON ends at the first `}`. bash runs with `--noprofile --norc` and vhs's own PS1. ttyd inherits vhs's environment, so `render.sh` scrubs it. Frames are paced by the clock and dropped when late, so chapters are scaled to the video's length. The terminal is the frame less its padding. The theme's keys. Uploads happen only with `--publish` or `VHS_PUBLISH`. Used for: the tapes, `render.sh` and `rehearse.sh` (docs/demo) |
| 2026-10-26 | Source of xterm.js (addons/xterm-addon-canvas/src/CanvasRenderer.ts at 5.3.0, src/browser/renderer/dom/DomRenderer.ts on master; https://github.com/xtermjs/xterm.js; MIT) | A cell's width is the font's advance floored plus the letter spacing, and its height is rounded up: the terminal size `rehearse.sh` works out for a tape |
| 2026-10-26 | Menlo's font tables (local /System/Library/Fonts/Menlo.ttc: head, hhea and hmtx, read with a short script) ; https://en.wikipedia.org/wiki/Menlo_(typeface) | An advance of 1233 and a line height of 2384, in 2048 units: `rehearse.sh`'s terminal size. Menlo is based on Bitstream Vera and DejaVu, so DejaVu Sans Mono is the tapes' font on Linux |
| 2026-10-26 | tmux 3.7b manual page (local `man tmux`), checked on private servers | Key tables (`switch-client -T`), `run-shell`, `load-buffer`, `paste-buffer -p` (bracketed only when the program asked for it), `detach-client -s`, `send-keys -l` and `-H` (ASCII only), `capture-pane -e -p`, `status-format`, `status-position` and `default-command`, sockets by `-S` and `TMUX_TMPDIR`, and `info`, which lists terminfo flags rather than a client's features: the presenter, `cue` and `rehearse.sh` (docs/demo), and the colour follow-up (docs/proposals/demo-footage.md) |
| 2026-10-26 | https://ffmpeg.org/ffmpeg.html (`-map_metadata`, `-map_chapters`) ; https://ffmpeg.org/ffmpeg-formats.html (the Metadata chapter; the mov muxer) | Global, per-stream and per-chapter metadata mappings; the FFMETADATA format and its escapes; MP4 chapters as a QuickTime chapter track: `render.sh` strips tags and embeds chapters |
| 2026-10-26 | Source of github.com/charmbracelet/colorprofile v0.4.3 and Bubble Tea v2.1.0 (Go module cache; MIT) | Behaviour only, no code copied. Under tmux, colour detection ignores COLORTERM and reads `tmux info` once, at start. It upgrades only on a requested `RGB` or `Tc` capability. Used for: the colour follow-up (docs/proposals/demo-footage.md) |
| 2026-10-26 | https://en.wikipedia.org/wiki/Lehmer_random_number_generator | Park and Miller's MINSTD (multiplier 16807, modulus 2^31 − 1), exact in awk's doubles: the demo's synthetic log is the same on every run (docs/demo/setup.sh) |

## Third-party code

No third-party code is copied into the repository. The demo recordings (docs/demo) run vhs (MIT) and the tools it drives, tmux, git and ffmpeg; none of them is copied or compiled in. These Go modules are compiled into `irie`, fetched by the Go toolchain at the versions pinned in `go.mod` and checked against `go.sum` (`go mod verify`). Bubble Tea, Bubbles, Lip Gloss and TOML are direct dependencies (DESIGN §8.1); the rest come in through them. M2 and M3 add no dependency: M3 runs the user's own `git` for repos and worktrees.

| Module | Version | License | Why |
|---|---|---|---|
| charm.land/bubbletea/v2 (github.com/charmbracelet/bubbletea) | v2.1.0 | MIT | Direct: sidebar and placeholder |
| charm.land/bubbles/v2 (github.com/charmbracelet/bubbles) | v2.2.1 | MIT | Direct: command bar textarea, key bindings |
| charm.land/lipgloss/v2 (github.com/charmbracelet/lipgloss) | v2.0.6 | MIT | Direct: styles and layout |
| github.com/BurntSushi/toml | v1.6.0 | MIT | Direct: config.toml |
| github.com/atotto/clipboard | v0.1.4 | BSD-3-Clause | Indirect, via the Bubbles textarea |
| github.com/charmbracelet/colorprofile | v0.4.3 | MIT | Indirect |
| github.com/charmbracelet/ultraviolet | v0.0.0-20261008173134-6b8d4baf91b4 | MIT | Indirect |
| github.com/charmbracelet/x/ansi | v0.11.9 | MIT | Indirect |
| github.com/charmbracelet/x/term | v0.2.2 | MIT | Indirect |
| github.com/charmbracelet/x/termios | v0.1.1 | MIT | Indirect |
| github.com/charmbracelet/x/windows | v0.2.2 | MIT | Indirect |
| github.com/clipperhouse/displaywidth | v0.11.0 | MIT | Indirect |
| github.com/clipperhouse/uax29/v2 | v2.7.0 | MIT | Indirect |
| github.com/lucasb-eyer/go-colorful | v1.4.1 | MIT | Indirect |
| github.com/mattn/go-runewidth | v0.0.30 | MIT | Indirect |
| github.com/muesli/cancelreader | v0.2.2 | MIT | Indirect |
| github.com/rivo/uniseg | v0.4.7 | MIT | Indirect |
| github.com/xo/terminfo | v1.0.0 | MIT | Indirect |
| golang.org/x/sync | v0.22.0 | BSD-3-Clause | Indirect |
| golang.org/x/sys | v0.47.0 | BSD-3-Clause | Indirect; Irie's own code imports it from M6 |

`go.sum` also lists modules that only those dependencies' own tests use (github.com/MakeNowJust/heredoc, github.com/aymanbagabas/go-udiff, github.com/charmbracelet/x/exp/golden, golang.org/x/exp and others). They are not compiled into `irie`.
