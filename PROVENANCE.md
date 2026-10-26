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

## Third-party code

No third-party code is copied into the repository. These Go modules are compiled into `irie`, fetched by the Go toolchain at the versions pinned in `go.mod` and checked against `go.sum` (`go mod verify`). Bubble Tea, Bubbles, Lip Gloss and TOML are direct dependencies (DESIGN §8.1); the rest come in through them.

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
