# Proposal: generative UI, or what an agent may draw in Irie

Date: 2026-11-01. Status: research and a proposal; nothing is built. It answers a request to research generative UI for Irie, and takes up what MAC.md §6 leaves for later, "buttons that answer a question or approve a command". An agent (Claude Code) wrote it, from `live` at `9235848`. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

**Built since** (2026-11-01): the skills infrastructure that carries the agent's side of this proposal (docs/proposals/skills.md, "Built so far"). Every Claude Code session now gets `/irie:present`, a skill that tells the agent how to show its work in what Irie draws today: decisions through `AskUserQuestion`, with previews; results as tables, lists and `diff` blocks; no HTML or remote images. The rest of the proposal is not built.

**In short.** Generative UI is an interface a model makes for the moment. It comes in three kinds: the model picks a component the host already has (Claude Code's question form), composes a tree from the host's catalog (Google's A2UI), or writes a page of HTML (MCP Apps, Claude's artifacts). The evidence favours generated pages for explaining things, at a cost of a minute or two, thousands of tokens and a look that is not the host's. It shows nothing yet for approvals, and the published attacks on this kind of UI leak data through what the client fetches to draw, or forge the approval itself. Irie's own gap is the first kind. Claude Code already hands Irie its questions, plans, permission prompts and MCP forms as data, and the Mac app draws each as a red card whose one button is **Answer in its terminal**. Over four days of one maintainer's journal, agents asked 29 questions in 24 of 150 sessions, and each waited a median of a minute. Claude Code's hooks can now answer all four for the user, while nothing of the other two kinds reaches Irie through Claude Code: it draws no MCP App and passes none on. So the proposal is, in order: answer here, drawn by Irie from Claude Code's own data (§7.1); richer rows from what the transcript already holds (§7.2); then cards an agent composes from a small catalog with A2UI's names, through one MCP tool, drawn natively in both views, with no HTML and buttons that only send text (§7.3). HTML comes later, if at all, as a viewer for a page an agent built (§7.4).

| | Today on `live` | Proposed |
|---|---|---|
| A question | A red card listing the first question's choices, and **Answer in its terminal** | Every question as buttons, or toggles when several may be picked, with descriptions, previews and **Other**; answered in Irie |
| A plan | The plan, and the same button | **Approve**, or **Keep planning** with what to change |
| A permission prompt | What it asks, and the same button | The exact command or path; **Allow**, **Always allow …** and **Deny** with a reason |
| An MCP server's form | The same button | A native form from its schema |
| An edit | "Edited main.go +14 −3" | The same, opening to its diff |
| Results, a comparison, progress | Markdown | A card the agent composes and Irie draws (off until turned on) |
| A page the agent built | Nothing | Later: a preview, on a click |

---

## 1. What generative UI is

An interface a model makes for the moment, in place of one a developer drew in advance. Three kinds recur in every product and framework read for this proposal (the names are CopilotKit's):

| Kind | The model makes | Who draws it | Examples |
|---|---|---|---|
| Static | A pick of a component the host already has, and its data | The host | Claude Code's `AskUserQuestion` and `ReportFindings`; the Vercel AI SDK's tool parts; Adaptive Cards response templates in Microsoft 365 Copilot |
| Declarative | A tree of components from the host's catalog, as data | The host, from its catalog | A2UI (Google) and Flutter's GenUI SDK; json-render (Vercel Labs); OpenUI Lang (Thesys); assistant-ui's single `present` tool |
| Open-ended | A page: HTML, CSS and script | A sandboxed web view | Gemini's dynamic view; Claude's artifacts and custom visuals; MCP Apps in Claude, ChatGPT, VS Code and Microsoft 365 Copilot |

The further down the table, the more a model can show, and the more it costs: seconds and tokens, a look that is no longer the host's, and more to secure.

**What the evidence says.**
- **People like generated pages better than text, for explaining.** Google's raters preferred a generated page to the same answer in Markdown in 82.8% of pairs, and in 90.5% on information-seeking prompts, though a page made by a human expert was still better in about half the comparisons. A generated page takes "a minute or two", which streaming roughly halves; the raters saw pages made in advance, so the wait did not count. Older, smaller models did worse: 29% and 60% of the pages from Gemini 2.0 Flash and Flash-Lite had errors, against none from 2.5 and 3. A second study (ACL 2026 Findings) found most raters preferred generated interfaces to chat, but among real users asking their own questions they won 50.8% of comparisons and lost 41.1%, with waits "up to several minutes".
- **Nothing measured shows it helps decisions.** No controlled study of forms, previews or diffs for an agent's approvals was found. What Anthropic measured for Claude Code: people approve 97% of its permission prompts but reject 39% of its plans; on a clearly dangerous command slipped into a run of prompts, people blocked 13.6%: about 17% early on, about 5% after 50 prompts. Claude Code's team tried questions as an array on the plan tool and as custom Markdown before a dedicated question tool worked.
- **It changes under people's feet.** Nielsen Norman Group: "constantly changing UIs will cause usability problems". In a study of model-made controls, users found each control's effect hard to predict, and across 120 interfaces from five generative UI tools over a quarter of the design reasons shown to users were not in what was made.
- **What gets attacked is the drawing.** Several published incidents leaked data through something the client fetched while drawing a model's output: an image address carrying the data (EchoLeak in Microsoft 365 Copilot, CVE-2025-32711; CamoLeak in GitHub Copilot Chat; ForcedLeak in Agentforce through an expired allowed domain), or a Mermaid diagram's images (Cursor, CVE-2025-54132). Others forged the approval itself: Claude Code's and Copilot Chat's dialogs padded or styled until the dangerous part was out of view ("Lies-in-the-Loop"), a chained command hidden by whitespace in Gemini CLI's approval, terminal escape codes that hid or rewrote text in a tool's output. OWASP's advice for the last kind: draw approval dialogs from structured data, never from a model's Markdown or HTML.
- **Specs are cheaper, not free.** A vendor's benchmark (Thesys, the seller of one format) put a whole generated screen at about 1,400 output tokens in its own compact language, 2,800 as A2UI and 3,300 as json-render, and 3% to 17% of specs failed a structure check. A card in Irie is smaller than a screen: the example in §7.3 is 458 characters, about 150 tokens.

## 2. What `live` has

- **Questions, plans, permissions and forms are seen, not answered.** The hooks of DESIGN §3.3 tell the core when an agent waits on you: `AskUserQuestion` and `ExitPlanMode` at PreToolUse, the permission prompt at PermissionRequest and Notification, an MCP server's form at Elicitation. The row turns red and `needs` carries the phrase, the first question's option labels (`questionOptions`, `internal/core/conversation.go`) or the plan. DESIGN §3.3 says of PermissionRequest "Never answered", and the shim's replies "Never `allow`: that would bypass the user's permission rules" (`Respond`, `internal/harness/claude/hooks.go`).
- **The Mac app** draws `needs` as a red card at the end of the conversation (`NeedsCard`, `ConversationView.swift`): the question, the options as plain rings that do nothing, the plan in Markdown, and one button, **Answer in its terminal**. The message box gives way to a banner, because `send` refuses with `busy` while the agent waits on a prompt (DESIGN §4.7).
- **The terminal slot** (`irie placeholder`) draws the same card in Lip Gloss.
- **Rows.** Each tool call is one line in plain words, with how long it took and the lines an edit changed. Replies are Markdown, drawn by the app's own renderer (`Markdown.swift`): headings, lists, code blocks, quotes, tables and rules. A fenced block is drawn as plain code whatever its language.
- **Nothing an agent can draw.** No tool, file or message lets an agent put anything in Irie's views but text. Irie passes Claude Code a `--settings` file (hooks, the status line, teammate mode) and no MCP server or plugin.
- **Holding.** When a session asks, the core decides who answers it. It holds the hook while a view is open and the session is on show in the conversation view, or not on show at all: then its card answers it, in a popover, so that answering does not bring its terminal up. It lets go at once, printing nothing, when no view is open, or while that session's raw terminal is on show, so a person at the terminal sees Claude Code's own dialog as today. Flipping to the terminal (⌥⌘T, **Answer in its terminal**, the flip key) lets go of a held prompt there and then. Once let go, the prompt is Claude Code's and the card says so.

## 3. When agents wait on a person

Every day of one maintainer's journal on this machine from 2026-10-27 to 2026-10-31 (four files: none for 10-29, and the last day in part), counted locally on 2026-10-31:

| Measure | Value |
|---|---|
| Sessions | 150 |
| Questions (`AskUserQuestion`) | 29, in 24 sessions |
| Time a question waited for its answer | median 59 s; 10% over 4.5 min; longest 81 min |
| Permission prompts, questions left out | 4, in 4 sessions (the profile runs in `auto` mode) |
| Plans to approve (`ExitPlanMode`) | 0 |
| MCP forms (elicitations) | 0 |
| Stalls: a new session held at a first-run prompt, such as trust | 14 |
| Tool calls | 14,354: Bash 9,889 (69%), Edit 1,393, Read 1,000, WebFetch 912, Write 415, WebSearch 297, Agent 53, Artifact 4 |
| Task lists (`TodoWrite`, `TaskCreate`) | 0: Claude Code leaves them out on current models |

So the one kind of generated interface this maintainer meets every day is Claude Code's own question form, and in the Mac app each one meant leaving the conversation for a terminal. Each question also fired PermissionRequest, so the journal records it as `input.needed` twice, once as a question and once as a permission.

## 4. What others do

Read from public specs, docs, changelogs and papers; ideas only, no code.

### 4.1 Wire formats

| | What it carries | Who keeps it | State on 2026-10-31 | For Irie |
|---|---|---|---|---|
| MCP elicitation | A flat form (text, number, boolean, one or several of a list) or a web address to open | The MCP spec | Since 2025-06-18; URL mode and titled choices 2025-11-25; carried in multi-step requests from 2026-07-28 | Claude Code draws it and a hook can answer it: §7.1 |
| MCP Apps | An HTML page in a sandboxed iframe, speaking JSON-RPC to its host over `postMessage` | MCP, Apache-2.0 | Stable 2026-01-26. In Claude on the web, desktop and mobile, ChatGPT, VS Code (experimental), Microsoft 365 Copilot, Goose and others. Not in Claude Code, which "calls the tool as text and doesn't render the UI"; an issue asking for it is open | Nothing reaches Irie through Claude Code, and a native host needs a web view anyway |
| A2UI | A flat list of components with ids, from a catalog the client owns, with a data model and actions back | Google, Apache-2.0 | 0.9.1 the current release; 1.0 a release candidate, due late 2026. The basic catalog has 18 components and no table, chart or progress. A SwiftUI renderer is in alpha (macOS 14+, not published); the Bubble Tea ones are early | The names of §7.3's catalog |
| AG-UI | Events between an agent and its front end: text, tool calls, state as JSON Patch, interrupts | CopilotKit's organisation, MIT | 1.0 on 2026-09-30 | A transport, not a format. Irie's socket already carries this |
| ACP | Tool calls with text, diffs (`path`, `oldText`, `newText`) or a terminal; a plan of entries with a status; permission options (`allow_once`, `allow_always`, `reject_once`, `reject_always`); elicitation since July 2026 | The Agent Client Protocol project, Apache-2.0 | Version 1 | The shape of §7.1's buttons and §7.2's rows, so a later ACP adapter (DESIGN §1.2) maps straight in |
| Adaptive Cards | A card from a JSON schema, with actions that post back | Microsoft, MIT | 1.5 in Teams and Copilot; native renderers since 2017 | Prior art: "Purely declarative - No code is needed or allowed"; the host owns the look |

Two more are formats one vendor controls: OpenAI's ChatKit widgets (a card JSON the developer designs and the agent fills), and Slack's Block Kit, which has added blocks for agents (`task_card`, `data_visualization`).

### 4.2 Products

- **Google.** The Gemini app's dynamic view is Gemini 3 writing a page of HTML per prompt, patched by post-processors, in Labs since 2025-11-18. Generated layouts in Search's AI Mode went worldwide in English on 2026-08-19. Flutter's GenUI SDK (alpha) draws real Flutter widgets from A2UI, and warns that its catalog prompt runs to "3,000-5,000+ tokens".
- **Anthropic.** Artifacts are pages at a private claude.ai address, which Claude Code can publish since 2026-09-16. Custom visuals in chat (beta since 2026-03-12) are HTML drawn inline, and "clicking inside a visual sends a follow-up prompt to Claude". MCP Apps run in Claude since 2026-01-26 as "interactive connectors".
- **OpenAI.** ChatGPT's apps are MCP Apps since 2026-02-22. Its guidelines: at most two actions on a card, the system's fonts and colours, WCAG AA, and a card always before the model's reply. Agent Builder closes on 2026-11-30.
- **Microsoft.** Microsoft 365 Copilot's agents answer with Adaptive Cards that the developer writes and the model fills, or with MCP Apps in a sandboxed frame.
- **Apple.** Nothing that makes UI at run time, through WWDC 2026. The Foundation Models framework's guided generation fills a struct the app defines, for views the app wrote.
- **Frameworks.** The Vercel AI SDK (version 7, 2026-06-25) maps each tool's call to a component, streams its arguments as they come, and no longer recommends its earlier `streamUI`. json-render and OpenUI have the model write a tree from a catalog typed with Zod, which also writes the prompt. assistant-ui has one `present` tool whose input is a tree from 35 components, and its docs say: "Treat props as untrusted input." Tambo's hosted service closed on 2026-10-31; its post says models now write front-end code faster than expected, and users prefer the assistants they already use.

### 4.3 Coding agents

| Tool | Questions | MCP forms | MCP Apps | Plans | A preview of what it built |
|---|---|---|---|---|---|
| Claude Code | `AskUserQuestion`, with previews | Form and URL (2.1.76) | No | Approve, three ways, or keep planning | The desktop app's browser pane; artifacts on claude.ai |
| Cursor | A question tool (2.1; any chat from 2.4) | Yes, though misrouted with several windows open | Yes, in a sandboxed iframe (2.6) | Editable Markdown, then **Build** | A browser pane; canvases of tables, charts and diffs drawn by its own React components (3.1) |
| Codex | Option cards with **Other**; questions that do not block in the CLI | Form and URL | Only the app, behind a flag | A plan item | An in-app browser |
| VS Code | Questions as a carousel | Listed | Yes (1.109) | A `/plan` agent | Mermaid; canvases in preview (1.142) |
| Zed | `ask_user`; ACP's forms | Through ACP, not MCP | No | ACP's plan | No |
| Goose | Through elicitation | Form only, 5 minutes | Experimental | Not checked | HTML apps |

opencode, Cline and Warp have question tools too. None of the terminal tools draws MCP Apps; only those built on a web view do.

What they teach:
- **A question tool is now standard,** and each draws its questions as choices to pick. A row that only says "answer in the terminal" is behind every one of them.
- **Task lists are on the way out.** Amp removed its to-dos in 2026-01, Cline deprecated its Focus Chain, Codex made `update_plan` opt-in, and Claude Code leaves its task tools out on current models (§3). A progress bar built on them would rarely show.
- **Key every prompt to its session.** Cursor's MCP forms went to the wrong window when several were open. Irie's prompts carry the session and agent they came from.
- **Previews sit beside the conversation.** Claude Code's desktop app, Codex, Cursor and Amp all put a browser next to the conversation for the page an agent built, with the page's content treated as untrusted.
- **Draw it yourself.** The community renderers for SwiftUI and Bubble Tea are young (A2UI-Swift, MIT; a2ui-tea, MIT; a2tea, an early A2UI-over-MCP terminal renderer under Charm's organisation since 2026-08-15). Irie's roughly fifteen views are less work than following any of them.

### 4.4 Lessons for Irie

1. **Only a catalog draws everywhere.** SwiftUI and Bubble Tea can both draw a tree of known components. HTML needs a web view, and a terminal can only link to it.
2. **The host owns the look.** Adaptive Cards, ChatGPT's guidelines and Google's style instructions all keep the host's fonts and colours. A card in Irie should look like Irie.
3. **A click becomes a message.** Claude's custom visuals, MCP Apps' `ui/message` and ChatGPT's follow-up messages all turn an action into the user's next prompt. Irie already types messages to an agent through `send`.
4. **Keep it small.** Two actions per card in ChatGPT; a closed set of components; validate, and show text when a spec fails.
5. **Approvals are the host's.** The forged-dialog attacks of §1 work by mixing the model's text into the approval. An approval drawn from Claude Code's structured input, by Irie, with nothing the model wrote styled into it, is the defence.
6. **Nothing is fetched while drawing.** Every leak of §1 went out through an address the client fetched to draw.
7. **This field moves monthly.** Borrow names and ideas, not code, and keep Irie's own format small enough to map onto whichever standard wins.

## 5. What Claude Code gives a host (2.1.316)

Read from Claude Code's docs on 2026-10-31.

**Hooks that answer for the user.** Four documented replies let a program that is not the terminal answer what Claude Code would ask:

| Prompt | Hook | Reply | Notes |
|---|---|---|---|
| A question | PreToolUse on `AskUserQuestion` | `permissionDecision: "allow"` with `updatedInput`: the `questions` echoed and `answers`, each question's text mapped to the label chosen (several joined by commas) | "Returning `allow` alone is not sufficient." No dialog appears |
| A plan | PreToolUse on `ExitPlanMode` | `allow` with `updatedInput` (the injected `plan` and `planFilePath`); `deny` with a reason to keep planning | The tool "still runs without a prompt" in plan mode |
| A permission | PermissionRequest | `decision: {behavior: "allow"}`, optionally with `updatedPermissions` (one of the `permission_suggestions` it was given, such as an allow rule for the session); or `{behavior: "deny", message}` | "Deny and ask rules are still evaluated", so an allow never beats a deny rule |
| An MCP form | Elicitation | `action: "accept"` with `content` keyed by the `requested_schema`'s properties; `decline`; `cancel` | URL mode too: `accept` skips the dialog, so the URL never opens |

An answer from a hook means no dialog appears, and a hook that prints nothing leaves the prompt to the dialog. So while a host holds the hook, it is the only place the prompt can be answered: for a question and a plan this follows from PreToolUse running before the tool, and for a permission prompt and an MCP form the docs imply it without saying so, which the smoke test checks (§9).

**MessageDisplay.** A hook that rewrites the text of a reply as it streams to the screen; the transcript and what Claude reads keep the original. It could show a one-line stand-in in the terminal for a block that Irie draws.

**Mods.** A mod is a plugin of JavaScript that runs inside Claude Code (on by default from 2.1.286; one loads for one session with `--plugin-dir`, as Irie passes `--settings`). It can:
- draw a pane beside the transcript or a band above the prompt, from elements: `Box`, `Text`, `Button`, `Link`, `Code`, `Markdown`, `Input`, `Select`, `Image` and `Raster` in the terminal, and `Svg` in the desktop app and VS Code;
- redraw what Claude Code draws: messages, tool call rows, the spinner and the question dialog. The permission prompt is not a render site;
- add a tool for Claude (`$.tool.register`, seen as `mcp__<plugin>__<name>`), and answer its calls in a `tool.call` hook;
- hold any tool call until the user decides: time spent inside a mods API call does not count against the hook's 10 s;
- reach files, processes and the network through `$.fs`, `$.process` and `$.http`, with the user's permissions: so it can run `$IRIE_BIN ctl` to talk to the core.

A mod draws in the terminal, the desktop app's Code tab and the VS Code extension, not in `claude -p`. Organisations can turn mods off or refuse `--plugin-dir` (`disableSideloadFlags`), and no mod loads in a folder until its trust prompt is answered.

**MCP.** `--mcp-config` adds MCP servers for one session. A stdio server's tool call may run up to about 28 hours, with an idle limit of 30 minutes without progress, and one in the main conversation that runs past two minutes moves to a background task: Claude goes on working and gets the result as a notification. Claude Code reads MCP Apps' `ui://` resources only when asked by URI, lists none and draws none: Anthropic's own guide says it "calls the tool as text and doesn't render the UI".

**Other tools that are UI.** `ReportFindings` hands Claude Code a structured list "so Claude Code can render them instead of printing them as text"; `SendUserFile` sends a report, diagram or screenshot to a Remote Control client; `Artifact` publishes an HTML page to claude.ai. Channels, MCP servers that push messages into a session and relay permission prompts, are a research preview and need a `--dangerously-load-development-channels` flag for a server of one's own.

## 6. Options

| | A. Answer here | B. Richer rows | C. Cards from Irie's catalog | D. Pages the agent writes | E. Host MCP Apps |
|---|---|---|---|---|---|
| What the model makes | Nothing new: Claude Code's questions, plans, prompts and MCP forms | Nothing new; at most a fenced block | A small JSON tree from a closed catalog | A whole page of HTML | Nothing: an MCP server ships the page |
| Who draws it | Irie, natively | Irie, natively | Irie, natively; Claude Code's terminal through an optional mod | A web view | A web view, with the MCP Apps bridge |
| Looks like Irie | Yes | Yes | Yes | No | No |
| In the terminal slot | Yes, with keys | Yes | Yes, with text stand-ins | A link | The server's text fallback |
| Tokens | None | None | About 150 a card, and two lines of instructions | Thousands a page, a minute or two each | The server's |
| What could go wrong | An answer on the user's behalf: only from Irie's own controls | Nothing new | Bounded: data in a closed catalog; buttons only send text | A sandbox to keep shut: script, network, look-alike prompts | As D, plus calls back to the server |
| Other harnesses | Each has its own prompts and hooks; ACP's map onto the same cards | Claude Code's transcript; ACP's diffs and plans later | Any harness with MCP | Any | Not through Claude Code, which draws none and passes none on |
| Effort | Medium | Small | Medium to large | Medium | Large, and blocked |

**A** is where people wait today (§3), costs no tokens, and builds what MAC.md §6 lists as not yet done: "buttons that answer a question or approve a command". **B** costs nothing either and makes rows people already read clearer. **C** is the generative part proper, the one kind of generated UI that both of Irie's views can draw in Irie's own look; it is worth building once A shows how people answer in Irie, and it is off until turned on. **D** is the most expressive and the most expensive in seconds, tokens and risk, and it cannot be drawn in a terminal; it fits only as a viewer for what an agent built (§7.4). **E** waits on Claude Code: it neither draws MCP Apps nor hands their pages on, so Irie would have nothing to draw.

## 7. The proposal: A, then B, then C

### 7.1 Answer here (first)

The four prompts of §5, drawn by Irie and answered through Claude Code's hooks. The agent's model makes nothing new; Claude Code already gave the prompt its shape.

- **Holding.** When a session asks, the core decides who answers it. It holds the hook while the session is on show in the conversation view, or not on show at all (its card can answer it, in a popover, so answering does not bring its terminal up); with no view open at all, it lets go at once, and it lets go at once, printing nothing, while that session's raw terminal is on show, so a person at the terminal sees Claude Code's own dialog as today. Flipping to the terminal (⌥⌘T, **Answer in its terminal**, the flip key) lets go of a held prompt there and then. Once let go, the prompt is Claude Code's and the card says so.
- **Through a file, as a freeze is.** The shim waits on an answer file in the session's folder, polled as a freeze's gate is, so a held prompt outlives a restart of the core or the app, and every install (DESIGN §2.1 principle 6). The new core finds it there and the views draw it again. The shim lets go if its agent is gone, if it cannot reach the core for 30 s, or after `ui.answer_wait` (default 30 min), well under each hook's timeout, which grows from 10 s to `hold.max` plus 120 s for PermissionRequest and Elicitation. A question let go after the wait still waits in the terminal: nothing is answered on the user's behalf.
- **Questions.** All of them, not only the first: one to four, each a group of buttons, or of toggles when it is `multiSelect`, with each option's description, its preview when it has one (Markdown, beside the options when wide), and an **Other** field. Number keys pick; Return sends when every question has an answer. The reply echoes the questions and maps each question's text to the label, the labels joined by commas, or the text typed. A question Irie lets go is not held again at the PermissionRequest that follows it.
- **Plans.** The plan in Markdown, then **Approve** and **Keep planning**, the latter with a field for what to change, which goes back as the deny's reason. Which permission mode Claude Code takes after a hook's approval is not documented; the smoke test finds out, and the card then says which mode follows.
- **Permissions.** What it wants to do, drawn by Irie from the tool's input and nothing else: the exact command or path in a monospaced block, with control characters shown as `^X`, and the model's own description of it set apart and labelled as the model's. Then **Allow**, the suggestions Claude Code gave as **Always allow …** (each one's rule and where it is saved, shown before it is chosen), and **Deny**, with a field for why. Return alone never allows. Claude Code's own deny and ask rules still apply. People approve nearly every prompt (§1), so the card makes the command plain; it does not make approving easier.
- **MCP forms.** The `requested_schema` as a native form: text, number, a toggle for a boolean, a menu or buttons for an enum, each with its title, description and default; **Submit**, **Decline** and Esc to cancel. A URL request names the site in plain words and its full address; **Open** opens it in the browser and accepts.
- **Who answered.** The conversation keeps a line under the prompt, "You answered in Irie: Postgres", and the journal records `ui.answered` with the session, the kind and the choice, but not typed text.
- **The terminal slot** draws the same card with keys: `1`–`9` pick, Space toggles, Tab moves between questions, Return sends, `t` lets go to the terminal.
- **Rules.** The shim's rule "never `allow`" becomes "never `allow` what the user did not answer in Irie's own drawing". An answer comes only from a click or key in Irie's chrome, never from a card an agent drew (§7.3), a message or a note. `ui.answer_here` (on by default) turns it off.

### 7.2 Richer rows (second)

What the transcript already holds, drawn better. The model writes nothing new, so it costs no tokens.

- **Edits open to their diff.** The reader already walks each edit's `structuredPatch` to count lines (`internal/core/conversation.go`). It keeps the hunks too, up to a cap, as the item's `diff`; an edit's row opens to the changed lines, red and green with line numbers, and a run of edits to a list of files that each open. The terminal slot draws the same in Lip Gloss.
- **Fenced blocks by language.** A ```` ```diff ```` block draws as a diff. A ```` ```chart ```` block holding the chart of §7.3's catalog draws as a native chart, and in the terminal slot as bars of block characters, so a model told about it can chart without a tool call. Any other language stays plain code. Mermaid, which models write often, needs a renderer the app does not have: it stays code, with a **Copy** button. (Built since, 2026-11-01: [learning.md](learning.md) §4.8 draws a `mermaid` block as a diagram, with Mermaid's own library in a web view that loads nothing else and allows images only from `data:` addresses, which closes the way CVE-2025-54132 leaked; `/irie:present` now suggests a small diagram where a picture helps.)
- **Images.** A screenshot the agent read or wrote in its folder shows as a thumbnail on its row, opened full size with a click. Nothing is fetched from the network.
- **Tests.** A run of a test command whose output names passes and failures (`go test`, `swift test`, `pytest`, `npm test`) says so on its row: "Ran the tests: 212 passed, 1 failed", with the failure's name. It is read from the output, never run.

### 7.3 Cards the agent composes (third)

One tool lets an agent put a card in its conversation: results, a comparison, progress, a choice. Irie draws it from its own catalog in its own look, in the Mac app and the terminal slot. The model writes a small JSON tree, never code.

- **The tool.** `card`, served by `irie mcp`, a stdio MCP server in the `irie` binary that the core passes to each Claude Code session with `--mcp-config` (§5), as it passes `--settings`. Any harness that speaks MCP can get it the same way once it has an adapter (DESIGN §1.3). The server's instructions, which load at session start while the tool itself waits for tool search, say in two lines when a card is worth it: something a person should see at a glance or choose from, not prose. `ui.cards` turns it on.
- **It returns at once.** "Shown as card `c3`." A card never holds the agent. Pressing one of its buttons sends a message through `send`, as typing would, which the agent reads at its next tool boundary or as its next prompt: `[Card “Three ways to keep sessions”] Use SQLite.`, then the values of any fields on the card, one per line. The conversation shows it as yours, marked as sent from the card, and the card keeps what was chosen and dims its buttons. A card that asks tells the agent, in the tool's result, that the answer comes as the person's next message.
- **Same id, same card.** A card sent again with its id is redrawn in place, so progress can tick without a new card each time. A card can be pinned to the details panel while its session is selected.
- **The catalog.** Named after A2UI's basic catalog where the two overlap, so A2UI can map onto it later:

| Element | What it draws | In the terminal slot |
|---|---|---|
| `Text` | A line or paragraph; `title`, `body`, `caption` or `mono`; inline Markdown | Text |
| `Row`, `Column` | Children side by side (wrapping when narrow) or stacked | Stacked |
| `List` | Items with a title, a detail and a status mark | Lines with marks |
| `Facts` | Label and value pairs | Two columns |
| `Table` | Columns and rows, up to 100 rows | A ruled table, cut to the width |
| `Progress` | A bar with a label: value of total | A bar of block characters |
| `Chart` | Bars or lines, up to 6 series (Swift Charts) | Bars or a sparkline of block characters |
| `Code`, `Diff` | Code in a language; a unified diff in red and green | The same in Lip Gloss |
| `Image` | A PNG or JPEG from the session's own folders | Its alt text and path |
| `Link` | A file path or a web address, with its host shown | The address |
| `Divider` | A rule | A rule |
| `ChoicePicker`, `TextField`, `CheckBox` | Fields whose values go with a button's message | Numbered fields |
| `Button` | Up to 4: a label and the message it sends; `primary` or not | `[1] Use SQLite` |

A card that compares three stores and asks which to use is 458 characters as compact JSON, about 150 tokens:

```json
{"id": "store", "title": "Three ways to keep sessions",
 "body": [
   {"type": "Table", "columns": ["Store", "Writes/s", "Setup", "Risk"],
    "rows": [["SQLite (WAL)", "~5k", "none", "one writer"],
             ["Postgres", "~20k", "a server", "ops"],
             ["JSON files", "~200", "none", "torn writes"]]},
   {"type": "Text", "style": "caption", "text": "Rough figures; measure before choosing."}],
 "actions": [{"label": "Use SQLite", "send": "Use SQLite.", "primary": true},
             {"label": "Use Postgres", "send": "Use Postgres."}]}
```

The slot draws no images: Ghostty has neither Sixel nor iTerm2's images, and the Kitty protocol's images need tmux passthrough and break easily, so charts are block characters there and images are their alt text.

- **Limits.** 32 KB of JSON, 200 elements, nesting 6 deep. Over a limit, or with an element the catalog lacks, the tool says what to fix and nothing is drawn. The views skip any property they do not know, so a newer core's cards still draw in an older app.
- **Safety.** No HTML, script, style or font. Colours come from four roles (accent, good, warning, quiet) that each palette maps; the red of what needs you is Irie's own and no card can use it. A card sits in a frame headed with its agent's name, so it never passes for Irie's own prompts, and its buttons only send text to that agent: they never approve a tool call, answer a prompt (§7.1) or run anything. Every string is drawn as text: a link shows its address, and the terminal slot shows control characters as `^X`, so a card cannot hide or rewrite what is around it. Images are read from disk, never fetched, so a card cannot leak what it shows through an address. Links open in the browser on a click, after showing where they go.
- **In Claude Code's own terminal** the call is a tool row with its JSON. An optional Irie mod, loaded with `--plugin-dir`, redraws that row (the `ToolUse` render site) as the card's title and a text drawing of it, for whoever sits in the raw terminal. The mod is Claude Code's only; the card works without it.
- **Tokens.** The tool's definition loads only when the model searches for it, and the two lines of server instructions sit in every session's cached prompt. A card is its JSON: the example above is about 150 output tokens.

### 7.4 Later: a preview of what the agent built

A web view in the Mac app for a page an agent built: an HTML file in its folder, or a page it serves on `localhost`. It opens only on a click on a card's `Link` or a row's path, as the browsers beside the conversation in Claude Code's desktop app, Codex, Cursor and Amp do. It is a viewer, not part of Irie's chrome:
- a frame headed "Page made by <agent>", outside the conversation's flow;
- a non-persistent website data store, set before the view is made;
- a content rule list that blocks every load but the file's folder and `localhost`;
- no script message handler, so the page cannot reach Irie; new windows refused; the inspector off;
- WebKit's own sandboxed content process.

The terminal slot shows the path. MCP Apps stay out until Claude Code passes them on; a native host would have to draw them in the same kind of view.

## 8. What DESIGN would change

- **§3.3.** PermissionRequest and Elicitation wait up to `hold.max` plus 120 s, like the tool hooks. The replies gain `updatedInput` for `AskUserQuestion` and `ExitPlanMode`, `decision` for PermissionRequest and `action` for Elicitation. "Never `allow`" becomes "never `allow` what the user did not answer in Irie's own drawing". With `ui.cards`, `--mcp-config` adds `irie mcp`; with `spawn.mod`, `--plugin-dir` adds the Irie mod.
- **§2.4.** An `answer` op; `needs` with an id and everything a prompt holds (every question with its options, descriptions, previews and `multiSelect`; a permission's exact input and suggestions; an elicitation's schema); `card`, which `irie mcp` sends to the core.
- **§4.7.** `needs` answerable; `card` items; an edit's `diff`; fenced `diff` and `chart` blocks.
- **§5.2.** `ui.answered`, `card.shown` and `card.pressed` events.
- **§5.5.** `ui.answer_here` (on), `ui.answer_wait` (30 m), `ui.cards` (off) and `spawn.mod` (off).
- **MAC.md §1.** The needs card becomes a form; cards in the conversation.

## 9. Order of work

1. **Answer here, the core** (medium). `needs` ids and the prompts' whole data; the shim's wait on an answer file and its letting go; the four replies; the `answer` op; letting go when the raw terminal shows the session. Tests with the fake agent, taught to take an `updatedInput` answer, a PermissionRequest decision and an Elicitation action as Claude Code does: an answer lands, a flip lets go, a restart of the core keeps the prompt, the wait runs out, the agent ends while held.
2. **Answer here, the views** (medium). The Mac app's form at the end of the conversation and from the card of a session not on show; the slot's keys. `--render-conversation` gains fixtures of each prompt, wide and narrow.
3. **Smoke test** (small, M11), against the real Claude Code: a question answered by the hook with no dialog and no PermissionRequest after it; a plan approved, and which mode follows; a permission allowed with a suggestion, and a deny rule that still wins; an elicitation from a test MCP server; that no dialog shows while a PermissionRequest or an Elicitation is held, and how a held prompt looks in the terminal.
4. **Richer rows** (small). Diff hunks, fenced `diff` and `chart`, thumbnails, test counts.
5. **Cards** (medium to large). `irie mcp` and its `card` tool, the `card` op and item, the catalog in SwiftUI and Lip Gloss, the button's message, limits and errors; fixtures of every element; `ui.cards`.
6. **The mod** (small, optional). The tool row redrawn in Claude Code's terminal; tested with `claude plugin test`.
7. **Previews** (later, medium).

## 10. Risks

| Risk | Effect | Mitigation |
|---|---|---|
| A held prompt hides Claude Code's dialog | Someone at the raw terminal sees a hook running, not the question | Hold only while that session's raw terminal is not on show; let go on a flip, after `ui.answer_wait`, or when the core has been gone 30 s |
| Claude Code changes how a hook answers | Answers stop landing | Documented fields only; the smoke test on each new version; an answer that fails lets go to the terminal |
| An answer the user did not mean | A tool runs, a plan goes ahead | Answers only from Irie's own controls; Return alone never allows; **Always allow** shows its rule first; Claude Code's deny rules still win |
| A forged approval | A dangerous command looks harmless | The permission card draws the exact command from the tool's input, in a monospaced block, with control characters shown as `^X` and nothing the model wrote styled into it; the model's description sits apart, labelled as its own |
| Easier approving means more approving | Habituation, as measured in §1 | Irie answers what Claude Code asks; fewer prompts come from containment (`auto` mode, sandboxing), not from Irie |
| A restart while a prompt is held | A lost question | The answer file in the session's folder; the new core draws it again |
| A card passes for one of Irie's prompts | A person clicks what they take for Irie | A frame headed with the agent's name; no needs red; its buttons only send text |
| A prompt injection writes a card | The card says what a web page wanted | A card cannot act; what its button sends shows in the conversation as sent from the card |
| Tokens | Cards cost output | Off by default; the tool waits for tool search; about 150 tokens a card |
| A local model writes a bad card | Nothing shows | The tool says what to fix; nothing is half drawn |
| A2UI moves | Names drift | Only names are borrowed; an adapter, if wanted, after A2UI 1.0 |
| An organisation turns mods off or refuses `--plugin-dir` | No card in Claude Code's own terminal | The mod is optional; the card is Irie's |

## 11. Decisions for the owner

| # | Decision | Options | Recommendation |
|---|---|---|---|
| 1 | Answer prompts from Irie (§7.1), which ends "never `allow`" | a. Questions, plans, MCP forms and permissions. b. As a, without permissions. c. No | **a**: people who never open a terminal meet permission prompts too, and the card is drawn from Claude Code's structured input |
| 2 | Where a prompt goes while its session's raw terminal shows | a. Claude Code's own dialog. b. Irie's card over the terminal | **a**: you answer where you look |
| 3 | Cards (§7.3) | a. After A and B, off by default. b. On by default. c. Not now | **a** |
| 4 | How cards reach the agent | a. `irie mcp`, and an optional mod for Claude Code's terminal. b. A mod only | **a**: one Go server any MCP harness can use |
| 5 | The card format | a. Irie's own catalog with A2UI's names. b. A2UI's wire format | **a**, looked at again at A2UI 1.0 |
| 6 | Previews of HTML (§7.4) | a. Later, local files and `localhost` only. b. Never | **a** |
