# Proposal: learning in Irie

Date: 2026-10-31. Status: step 1 is built, at the owner's word (2026-11-01); see "Built so far" below. The rest is proposed. There is no issue tracker yet, so this file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote it, from `live` at `d92721f`. It is the design only and builds no code. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

## Goal

Irie helps the person at the keyboard learn while their agents work. They learn to use Irie itself, to read and steer what an agent does, and, when they want to, the craft underneath. None of it slows down someone who only wants the work done. Every part has its own switch, and none of it writes to the user's own agent config.

**In short.**

| Part | What it is | Default | Tokens |
|---|---|---|---|
| Tutor mode (§4) | `learn.tutor = off \| explain \| teach`. Claude Code sessions run in Claude Code's own **Explanatory** or **Learning** output style, which Irie passes per launch in the settings file it already writes. `+explain` and `+teach` set it for one task | off | more: longer replies |
| The walkthrough (§5) | On the first run, a short welcome, then a first real task, with short steps that follow its card from starting to done. Help > Show the Walkthrough Again replays it | on, once | a few cents for the first task, or none on `+local` |
| Tips (§6) | One tip at a time, where it applies, until you have done the thing it teaches: in both sidebars, and as "Irie tip" lines under Claude Code's spinner | on | none |
| Help and resources (§7) | A Help menu: Irie's own guide pages, a glossary in plain words, and a short list of free learning resources, plus your own links | always there | none |
| Pictures (§4.8) | In a tutor mode, the agent draws a small diagram where a picture helps (`learn.visual`), and the Mac app draws it in the conversation. Later, explainers the agent makes, in a sandboxed pane | on, in a tutor mode | a little more |
| Learning from your own sessions (§8) | Later, each opt-in: the Insight notes of a session gathered in one place, Explain This on a call, a short lesson when a session finishes, and questions to check yourself, spaced over days | off | none for the notes; the rest on `+local` first |

The first build (§11) is tutor mode with its pictures, the Help menu and the agent tips: mostly core work, plus a Settings tab.

**Built so far** (2026-11-01, step 1's core): the `[learn]` table (`tutor`, `visual`, `agent_tips`, `links`); `outputStyle` and Irie's tips in a session's settings file, and the ask for diagrams through `--append-system-prompt`; `+explain` and `+teach` in both task parsers and the `+` list; the tutor mode in `spec.json`, on the row and in the terminal sidebar's words, kept by reopen, failover and a restarted core; your turn in teach mode (§4.4), which the jot dog leaves be and `send` still reaches; the note for generic agents; and the `learn` op over `internal/learn/learn.toml`, with 14 tips, 23 resources and 31 words. Then the Mac app's part: the Learning tab, the New Task sheet's Tutor menu, the Help menu with the site's pages and a Learn window for the resources and the glossary, Mermaid diagrams drawn in the conversation (§4.8), the tutor mode in the details panel, and the message box kept in your turn.

---

## 1. Who learns what

Irie for Mac is for "the people who would never open a terminal", and it stays useful for developers ([MAC.md](../MAC.md)). Three people learn different things in it:

| Who | What they need to learn | What helps |
|---|---|---|
| Someone new to all of this | Irie: cards, colours, the task bar, needs you, to verify, park and close. What an agent is doing, in plain words. The few ideas underneath: a folder and a repo, a branch and a worktree, a commit, a test, landing | The walkthrough, tips, the glossary, and the **explain** level of tutor mode |
| A developer new to a codebase, a language or a framework | Why the agent made the changes it made | **explain** |
| A developer who hands more and more to agents | To keep their own skill, and to understand what they merge | **teach** now and then, the Insight notes, and questions spaced over days (§8) |

Everyone also learns to write a good task, pick an agent for it, and check the work. Irie's site already teaches that (§3).

## 2. What the research says

**Help that does the work can stop the learning.**
- **Anthropic's own trial** (January 2026). Fifty-two mostly junior engineers learned a Python library new to them, Trio; half had an AI assistant. On a quiz afterwards, without it, the AI group averaged 50% and the others 67%, and the AI group was not significantly faster. The widest gap was in debugging. Anthropic's summary: AI help "lowered comprehension scores, except when used for explanations rather than delegation". Those who asked for explanations or asked conceptual questions kept most of the learning; those who handed over the work scored lowest. It was a chat assistant, not an agent.
- **Bastani et al.** (PNAS, 2025). About a thousand high-school students practised maths with GPT-4. Plain access raised their practice grades by 48%, but on an exam without it they did 17% worse than students who never had it. A tutor version, which gave hints and not answers, raised practice grades by 127%, and its students did no worse on the exam.
- **Lehmann et al.** (2024, revised 2025). Beginners learned Python with ChatGPT. Those who asked it for solutions covered more topics with less understanding; asking it for explanations raised understanding.
- **Kestin et al.** (Scientific Reports, 2025). A physics tutor on GPT-4, which took students through each problem a step at a time from solutions written in advance, more than doubled their learning gains compared with an active-learning class.

Both tutors that helped had a solution to steer by, and an agent's open-ended work has none. What is left to work with is what the person does: write a part, explain it back, predict before they look.

**What helps people learn.** Dunlosky et al. (2013) rated ten study techniques. Practice testing and spacing practice out over time came out high; self-explanation and mixing kinds of problems moderate; rereading, highlighting and summarising low. Novices learn more from studying worked examples than from solving problems alone (Sweller and Cooper, 1985), but help that suits a novice gets in an expert's way, the expertise reversal effect (Kalyuga et al., 2003). Good learners explain worked examples to themselves (Chi et al., 1989). Being tested beats rereading on a later test, though rereading feels better (Roediger and Karpicke, 2006), and Bastani's students thought they had learned more than they had.

**First runs and tips.** Apple's guidelines ask for an onboarding flow that is "fast, fun, and optional", say "Teach through interactivity", and suggest "a collection of context-specific tips instead of a single onboarding flow". A tutorial someone skipped should not come back on later launches, but should be easy to find. Apple's TipKit docs add: "Don't use tips to guide people through your app". Nielsen Norman Group (Laubheimer, 2023) found that tutorials shown out of context, "push revelations", get skipped and don't help, and that help shown when it is needed works.

**Pictures.** Students given an explanation in words and pictures together did better at using what they learned than students given words alone, across the studies Mayer reviewed (1997). That holds for nearly everyone: the claim that teaching should match a person's preferred style, such as "visual learner", has no good evidence behind it (Pashler et al., 2008). So pictures come with every tutor-mode session, not as a mode for some.

**Learning modes elsewhere.** Claude's learning mode and Claude Code's Explanatory and Learning styles arrived in 2025, as did ChatGPT's study mode (July) and Gemini's Guided Learning (August). Each is an instruction to the model that a person switches on, and so is tutor mode.

**What it means for Irie.**
- Watching an agent is reading worked examples. They teach when the person explains them, predicts or writes a part, which is what explain and teach ask for. They teach less as the person grows expert, so tutor mode is chosen per task and is off by default (§4).
- Teach mode is the guarded tutor: the agent leaves the decisions to the person (§4.4).
- Questions that come back over days are practice testing and spacing, the two techniques rated high (§8). They are answered without help, because people misjudge what they have learned.
- The walkthrough is short, optional, done on a real task, and easy to find again; everything after it is a tip that shows when it applies (§5, §6).

## 3. What Irie has today

- **The Mac app.** An empty list shows the mark, "Everything irie." and "No agents yet. Type a task below to start one." (`Welcome` in SidebarView.swift). The task bar's hint is "What should we work on?". The phrase bar under the terminal shows one of 18 phrases every 30 seconds. Every tool call has plain words in the conversation view and on the card (`core.Phrase`, DESIGN §4.7). There is no help: the Help menu has only macOS's own item, and the app ships no help book.
- **The terminal sidebar.** With no session shown, the slot lists the keys. The status strip says what the selected session is doing.
- **The site.** It has a path for beginners: Get started, Your first session, Tasks, repos and worktrees, Keys and commands, Troubleshooting, and Recipes. Your first session starts with a task chosen to make the agent ask something, so the reader sees a card turn red: "Write haiku about my city. Ask me which city first." The pages describe the terminal sidebar of the first milestones. Some of it is out of date: Your first session still says paused and frozen "stay at 0 until Irie can pause and freeze agents", and both have shipped.
- **Claude Code** (2.1.316) has four output styles besides Default: Proactive, Concise, Explanatory and Learning. It also has tips under its spinner (since 2.1.311, one suggests the Explanatory style when you ask for explanations), a session recap when you come back, `/btw` for a side question that stays out of the conversation's history, and `/powerup`, interactive lessons on its features with animated demos (since 2.1.90). All of these are in the agent's terminal, out of sight of someone who stays in the conversation view.
- **The skills Irie plans to bring** ([skills.md](skills.md), decided for M12) include `codebase-onboarding`, which writes a guide to an unfamiliar codebase.

## 4. Tutor mode

### 4.1 The levels

| `learn.tutor` | Claude Code gets | What you see | Cost |
|---|---|---|---|
| `off` | nothing: your own output style, if you set one, stands | as today | as today |
| `explain` | `"outputStyle": "Explanatory"` | The work as usual, plus short "★ Insight" notes with the reasons behind its choices | longer replies |
| `teach` | `"outputStyle": "Learning"` | The same notes. It also leaves small decisions for you to write, at a `TODO(human)` comment, and waits for you | longer replies, and your own time |

The styles are Claude Code's own, and its output-styles docs describe both. Irie writes no instructions of its own for them. Irie already passes a settings file to every Claude Code session it starts (DESIGN §3.3). A style is one more key in that file, so nothing is written to the user's files (DESIGN §2.1, principle 4). The docs say the Explanatory and Learning styles "produce longer responses than Default by design, which increases output tokens". So the Settings window and the `+` list say so next to them.

**Checked on Claude Code 2.1.316**, in a throwaway session with no prompt sent:
- `claude --settings <file>` with `{"outputStyle": "Concise"}` started in that style: `/output-style` marked it current, and listed `default`, Proactive, Concise, Explanatory and Learning.
- An edit to that file while the session ran changed nothing. The file is read once: the docs' reload of changed settings covers the user, project, local and managed files, and the `ConfigChange` hook names only those.
- With the file's style set, `/output-style Learning` typed in the session answered "Output style set to Learning", but the session kept the file's style, because `--settings` sits above the project's own files. It also wrote `.claude/settings.local.json` in the session's folder. In a jot's worktree that is an untracked file, which the jot dog would count.

So Irie never types `/output-style` into a session. While tutor mode is on for a session, Irie's control is the way to change it (§4.3), and the docs for tutor mode say so.

Styles have moved before. Claude Code deprecated output styles in 2.0.30 and brought them back in 2.0.32, and a name it does not know quietly gives the Default style. So when M11's smoke tests against the real Claude Code arrive, one checks that `/output-style` still lists Explanatory and Learning.

### 4.2 Choosing it

- **For every task:** Settings > Learning > Tutor mode (`learn.tutor`), with the three levels in words: **Off**, **Explain as it goes**, **Teach me**.
- **For one task:** `+explain` or `+teach` in the task bar or the command bar. Either can stand beside a profile word: `+sonnet +teach build the upload form`. The New Task sheet gets a picker with the same three levels. `irie ctl spawn +teach …` takes the word, and the `spawn` op takes `tutor`.
- **The words are reserved.** A profile named `explain` or `teach` is refused when the config loads, with the key named, as any bad key is.
- **It shows.** The `+` list offers both words, each with what it does and that it costs more. A session's row carries `tutor`, so the card's details and the conversation's header say **Explain mode** or **Teach mode**.

### 4.3 Changing it mid-session

The style is fixed when Claude Code starts (§4.1). A change from a card's menu (**Tutor Mode ▸ Off, Explain, Teach**) or `:tutor <level>` is recorded at once, and the card shows the new mode. It reaches the agent in two ways:

- **Now, as a note.** At its next step or prompt, the agent gets a note in Irie's own words (DESIGN §4.3). The note says to explain its choices from now on, or to leave the key decisions to the user at `TODO(human)` and wait, or to stop doing either. The templates sit in `[notes]` with the others. Claude Code does much the same when you switch style mid-session: its prompt-caching docs say it "delivers the new style's instructions as a message in the conversation", so the cache is kept. A note keeps it too.
- **In full, from the next reopen or move.** The session's settings file carries the new style from then on.

Irie could instead restart the agent at its next wait on the same conversation (`claude --resume`), with the new style in its settings. The style would then be exact at once. But the restart changes the system prompt, so the whole conversation would go through the prompt cache again, and it would end whatever the agent runs in the background. So Irie does not do it.

### 4.4 When the agent waits for you to write

In teach mode, a turn can end on a "Learn by Doing" request: what is built, what to write, and what to weigh, and then the agent waits. Claude Code's docs show such a request ending in a full stop, not a question mark. Two of Irie's rules read a finished turn by that mark, and both would get it wrong:
- the jot dog sees the `TODO(human)` comment, an edit not committed, and sends the jot back to finish (DESIGN §4.8). The agent could then write your part itself;
- close-when-done closes a quiet finished session unless its task asks for something to read, its last message asks something, or the dog found work left (`awaitsLook` and `finished` in close.go). With `spawn.close_when_done` on, a session in a scratch folder, where the dog watches nothing, would close `close_delay` (30 seconds) after it asked you to write.

So in a session in teach mode, a turn whose last message holds `TODO(human)` ends as **your turn**. Its card turns red, as for any question, and says "Your turn: …" with the request's task line. The dog leaves it be, and close-when-done leaves it open. The conversation view shows the request as a card with **Open the File**, which opens the file at the `TODO(human)` line in the user's editor. Your next message ("done") hands the turn back.

Teach mode expects you to edit code. Someone who has never seen code starts with **explain**. The Settings window says this in its one line under Teach me.

### 4.5 Insight notes in the conversation view

Both styles write their notes as text in the reply, between a `★ Insight ─────` line and a closing rule. The core's conversation reader marks them, and the Mac app and the slot draw each as a quiet callout instead of a run of box-drawing characters. Nothing changes for a session without notes.

### 4.6 Other harnesses

Other harnesses have no output styles. On a generic profile, `explain` and `teach` add one paragraph before the task (DESIGN §3.3, the task argument): explain your choices as you go, or leave the key decisions to the user, mark them `TODO(human)` and wait. The `+` list says that this is only the instruction, not a style. When an adapter for Codex CLI, Gemini CLI or opencode lands, it can use that harness's own way to add instructions.

### 4.7 Later: Irie's own styles

The session plugin of [skills.md](skills.md) (`--plugin-dir`, M12) can carry output styles too: Claude Code's plugin reference lists an `output-styles/` folder. Irie could then offer a style of its own, written for Irie and copied from nowhere. One idea is **Newcomer**: plain words, every term explained the first time it appears, what it will do before it does it, and what to check at the end. It would set `keep-coding-instructions: true`, so Claude Code keeps its coding instructions, and never `force-for-plugin`, which would put it on every session. `learn.tutor = "newcomer"` would pick it.

### 4.8 Pictures

Words and a picture teach better than words alone (§2), so in a tutor mode the agent draws.

- **The ask.** With `learn.visual` on, the default, a session in a tutor mode gets one paragraph through `--append-system-prompt`: when a picture would help the user follow an explanation, such as how parts of the code connect, what happens in what order, or how branches and commits relate, draw a small Mermaid diagram in a `mermaid` code block, and go on explaining in words beside it. The flag appends to Claude Code's instructions and its output style and takes nothing away, and it goes per launch, like the settings file. A generic agent gets the same words after its tutor note (§4.6).
- **Why Mermaid.** It is text, so the agent writes it like any reply and the transcript keeps it. It is short, models write it well, and it has the shapes teaching needs: flowcharts, sequences, states, and git graphs for branches, commits and merges. A diagram holds no script or markup of its own, so drawing one runs nothing the agent wrote.
- **Drawing it.** The Mac app's conversation view draws each `mermaid` block as a diagram in the palette's colours, with a button to show its source. It draws it with Mermaid's own library, MIT-licensed, bundled with the app at a pinned version and checked by its hash at build time, as the terminals' libraries are. It draws in a web view with no network and Mermaid's strict security level, so a diagram can neither fetch nor run anything: the page's policy loads scripts only from Irie's own scheme and images only from `data:` addresses, and any navigation is cancelled. That closes the way a diagram leaked data in Cursor (CVE-2025-54132, cited in [generative-ui.md](generative-ui.md) §1), through images it fetched. That proposal left Mermaid as plain code because the app had no renderer; this is the renderer, inside that sandbox. In the slot and in the agent's terminal the block stays text.
- **Later: explainers the agent makes.** For what a diagram cannot show, such as a step-by-step animation of an algorithm, the agent could write one self-contained HTML page into the session's folder and say so. The app would show it in a pane beside the conversation, as it shows a helper: sandboxed, with no network, no access to files, and nothing that reaches the app. That is step 3.

## 5. The walkthrough

### 5.1 In the Mac app

*Built in brief* (2026-11-01, DESIGN §2.7): the welcome lists what Irie needs with a button for what is missing, and **Try a First Task** puts this task in the task bar; a **Your first task** card above the cards then follows it through steps 2 to 4. The rest of this section, with Show Me Around, Help > Show the Walkthrough Again and `:tour`, is still proposed.

The first time the app opens with no sessions, the list shows a welcome in place of today's empty list. It has the mark and the motto, one line on what Irie does, and two buttons: **Show Me Around** and **Skip**. Then the walkthrough teaches by doing, with one real task:

1. **The task bar.** It is filled in with the site's first task, "Write haiku about my city. Ask me which city first.", on `+haiku`, at a fortieth of Opus's price. The task bar's menu offers `+local` instead when a local model is set up, for no tokens. The step above it says what the task bar takes, and that this task will ask you something. The buttons are **Start It** and **Type My Own**. If `claude` is not found, the step says so first and links to Claude Code's Quickstart. If the session stalls, which on a first run is usually a sign-in or a question whether to trust the folder, the step says to answer it in its terminal.
2. **Starting and working.** A step on the new card: each task gets a card, its mark says how the agent is doing, and the line under the name says what it does now.
3. **Needs you.** When the card turns red: red means it needs you, and here it asks which city. The step points at where to answer.
4. **Done.** When it finishes: ✓ means to verify. Read the haiku, then park the session (Ⓟ) or close it (⌘W), and ⌘Z takes either back.
5. **The conversation and the terminal.** The conversation shows the work in plain words, and ⌥⌘T shows the agent's live terminal.

The steps move on when the card does, not on a Next button. Esc or **Skip** ends the walkthrough at any step, and the task, if started, runs on as any other. **Help > Show the Walkthrough Again** replays it.

This task needs no repo: it runs in a scratch folder of its own. It makes the agent ask before it works, so in about a minute the newcomer sees a card start, work, turn red and finish. The walkthrough is what Apple's guidelines and Nielsen Norman Group's findings ask for (§2): fast, optional, taught on a real task, and easy to find again after a skip. A skipped walkthrough never comes back by itself. Apple says tips should not guide people through an app, so the walkthrough is its own short flow, and tips come only after it (§6).

### 5.2 In the terminal sidebar

`:tour` starts the same walkthrough, and so does the first run when there are no sessions. The slot's placeholder shows the steps as a short list that ticks as they happen: a task typed, its row picked, the question answered, the turn done, the session parked. The keys stay below it.

### 5.3 A practice agent with no model

Later, a **Practice** choice could run Irie's own scripted agent instead of a real one. That is the test suite's fake agent, which fires Claude Code's hooks with no model and no network and already drives the demo recordings (docs/demo). It would go through the same states for free. It needs work first: today it writes no conversation the app could show. The real task teaches more, so it comes first.

## 6. Tips

### 6.1 In the sidebars

A tip appears where it applies, once the thing it teaches is near. It stops once you have done that thing, or closed the tip. Only one tip shows at a time, and the walkthrough's steps come before any tip. Examples:

| When | Where | What it says, in short |
|---|---|---|
| The first card that needs you | the card | Red means it needs you; show it and answer |
| The first finished turn | the card | ✓ is to verify: check the work, then park or close |
| Five sessions or more, none parked yet | the Ⓟ button | Park puts a checked session away; it keeps running |
| The first close | the title bar's counts | ⇧⌘T or ⌘Z brings it back |
| The first helper | the helper's line | ⇧⌘H shows its work beside the conversation |
| The plan's five hours past `route.pressure` | the task bar | `+local` runs on this Mac with no tokens, and `+haiku` costs a fortieth of Opus |
| The tenth task with no `+` or `@` words | the task bar | `@repo` runs it in a worktree, and `+explain` tells you why it does what it does |

In the Mac app a tip is a small card in the palette's colours, with the same contrast rules as the rest of the app. It reads as one VoiceOver element and is announced like a notice. Return follows it and Esc closes it. The terminal sidebar shows a tip in its status strip.

Apple's TipKit was weighed. The macOS 14 SDK the build uses has it, and it offers inline and popover tips, display frequency and a datastore of what was shown. But its rule macros (`#Rule`, `@Parameter`) need a compiler plugin that ships only with Xcode, and the Command Line Tools that build Irie lack it (checked: `libTipKitMacros.dylib` is only inside Xcode.app). Tips without macros do build. Its way to show tips again, `Tips.resetDatastore()`, is meant for testing; the per-tip reset needs macOS 26. Its tips would also look like the system's, not like Irie's palettes, and the terminal sidebar could not share what was seen. So Irie draws its own tips, and `--snapshot` can draw them for review.

### 6.2 In Claude Code's spinner

Claude Code shows a tip under its spinner while the agent works. Its `spinnerTipsOverride` setting adds tips to that rotation, and the docs say the setting is read from `--settings`, with a label of up to 40 characters, an `id` per tip and a `cooldownSessions` count. With `learn.agent_tips` on, Irie adds a dozen tips about Irie to the session's settings file, under the label **Irie tip**. Examples: Ctrl+] moves between the list and this terminal; your agents keep running when the window closes; `+local` runs a task with no tokens. Each has a cooldown of several sessions, so they come seldom, and Claude Code's own tips stay. A user who set `spinnerTipsEnabled` to `false` sees no tips at all, Irie's included, as Claude Code's docs say.

**Not used:** `companyAnnouncements`. It shows a line when a session starts, but Claude Code presents it as "Message from <your account>'s Organization" (checked on 2.1.316), which would not be true.

### 6.3 In the phrase bar

With `learn.phrase_tips` on, every other phrase under the terminal is a tip. It is off by default, so the bar stays as it is.

## 7. Help and resources

### 7.1 The Help menu

The Mac app gets a Help menu of its own:

- **Irie Guide**, **Your First Session**, **Tasks, Repos and Worktrees**, **Keys and Commands** and **Troubleshooting**: the site's pages, opened in the browser.
- **Glossary…**: the words Irie and its agents use, each in a sentence or two (§7.3).
- **Learning Resources…**: the list in §7.2.
- **Show the Walkthrough Again** and **Show Tips Again**.

The terminal sidebar gets `:help` for the keys, `:learn` for the resources, numbered so that a number opens one, and `:glossary <word>`.

### 7.2 Learning resources

A short list of free resources, grouped, each with what it teaches. Irie links to them and never copies or caches what they say:

| Group | Resource | Notes |
|---|---|---|
| Irie | the site's guide pages | |
| Working with Claude Code | its docs' Quickstart, Best practices, Common workflows and Glossary pages, and the Output styles page that explains tutor mode; `/powerup` in an agent's terminal | `/powerup` is Claude Code's own lessons, with animated demos |
| Courses | Anthropic Academy: Claude 101, Claude Code 101, AI Fluency | free with a sign-in; Claude Code 101 assumes you know the command line |
| Prompting | Anthropic's prompt engineering overview and its prompting best practices | |
| Git | Pro Git; Learn Git Branching, a game in the browser; GitHub Skills | Pro Git is CC BY-NC-SA 3.0; GitHub Skills needs a GitHub account |
| The terminal | MIT's The Missing Semester of Your CS Education | CC BY-NC-SA 4.0; its 2026 edition has a lecture on agentic coding |
| The language at hand | the official tutorial for each language the shown session's repo is written in, found from its files with no model: A Tour of Go, The Swift Programming Language, the Python tutorial, the Rust book, the TypeScript handbook, MDN's web development course | |

Every link was checked live on 2026-10-31 (PROVENANCE.md). Irie only links, so there is nothing to keep in step with the sources and no license to carry.

The list lives in one file in the repo, which the core serves to both sidebars and the site can show too. Each entry has a title, a link, what it teaches and its level. `learn.links` adds the user's own, such as a team handbook, at the top.

### 7.3 The glossary

About thirty words, each defined in plain words: session, agent, helper (subagent), task, repo, folder, worktree, branch, commit, merge, land, to verify, park, pause, freeze, promote, profile, model, local model, token, context, permission, tool call, output style, `TODO(human)`, and so on. It is Irie's own writing, kept in one file, and shared by the Help menu, `:glossary` and a site page, so the words are the same everywhere, as the phrasebook asks of Irie's fixed lines ([PHRASES.md](../PHRASES.md)). Later, with `learn.glossary_hints` on, a word from the glossary in an agent's reply gets a dotted underline, and the definition shows on hover.

## 8. Learning from your own sessions (later)

Each part is opt-in, and none sends anything anywhere.

- **The notes, gathered.** In explain and teach modes the Insight notes are already in the transcript. The core collects them per session, with no model and no tokens, into a **What it explained** list in the details panel, and keeps every session's notes in one local file, so they can be read again after the session is closed.
- **Explain This.** A call's line in the conversation opens to its exact command; **Explain** beside it asks for two to four plain sentences on what that command does and why an agent on this task would run it. The router answers on the cheapest profile accurate enough, `+local` first, as `irie ctl delegate` does (DESIGN §6.4), and the answer shows under the call. In the terminal, Claude Code's own `/btw` answers such side questions from the session's context and leaves its history alone, but only in its terminal.
- **A lesson when a session finishes** (`learn.lessons`): what changed, why, and the glossary's words that came up, from the agent's last message and its notes.
- **Check yourself** (`learn.review`): two or three questions about what the session changed, written by `+local` when it is set up, with the answers a click away. They come back after one, three, seven and twenty-one days in a **Review** sheet, because spacing practice out is one of the two techniques rated high (§2). Nothing is scored anywhere but on this Mac.

## 9. Settings

### 9.1 Config keys

```toml
[learn]                      # what helps you learn while your agents work (docs/proposals/learning.md)
tutor = "off"                # off | explain | teach: Claude Code's Explanatory or Learning output style; +explain and +teach set it for one task
visual = true                # in a tutor mode, the agent draws a small diagram where a picture helps (§4.8)
walkthrough = true           # show the walkthrough on the first run; Help > Show the Walkthrough Again replays it
tips = true                  # one tip at a time in the sidebars, where it applies, until you have done what it teaches
agent_tips = true            # Irie tips under Claude Code's spinner (spinnerTipsOverride)
phrase_tips = false          # every other phrase under the terminal is a tip
links = {}                   # your own resources, by title, first in the list: { "Team handbook" = "https://example.com/handbook" }
# glossary_hints = false     # later (§8)
# lessons = false            # later
# review = false             # later
```

`links` is a table by title, as `repos.aliases` is, so the Settings window can edit it through the `config` op. Every key is read when a session starts or a view draws, so a change applies to the next task without a restart (DESIGN §5.5). What has been seen, the walkthrough's step and the tips already shown, is kept by the core in `$XDG_STATE_HOME/irie/learn.json`, mode 0600, so both sidebars share it. **Show Tips Again** and `irie ctl learn reset` clear it.

### 9.2 The Learning tab

Settings gets a sixth tab, **Learning**:

- **Tutor mode**: Off, Explain as it goes, Teach me, each with one line on what it does, and that the last two make longer replies.
- **Walkthrough**: show it on the first run, and **Show It Now**.
- **Tips**: in the sidebar, under Claude Code's spinner, in the phrase bar, and **Show Tips Again**.
- **Your links**: add, edit and remove.

### 9.3 The socket API

- `spawn` takes `tutor` (`off`, `explain` or `teach`); a row carries `tutor`, and `your_turn` while a teach-mode request waits on you (§4.4).
- `tutor` with `session` and `level` changes a running session's mode (§4.3).
- `learn` returns the tips, the walkthrough's state, the resources and the glossary; with `seen` it marks a tip or a step seen, and with `reset` it clears them.

## 10. Cost, privacy and accessibility

- **Tokens.** Only tutor mode, the walkthrough's first task, Explain This, lessons and questions use a model. Tutor mode is off by default and says it costs more where it is chosen. The first task is a few cents of tokens on `+haiku` and nothing on `+local`. The later parts run on `+local` first. Tips, the Help menu, the glossary, the resources and the gathered notes use no model at all.
- **Privacy.** Nothing about what you learn leaves the machine. `learn.json` is local, links open in your browser, and Irie sends no telemetry, now or for this.
- **Accessibility.** The walkthrough works from the keyboard: Return follows a step and Esc ends it. Tips and steps are VoiceOver elements, announced as notices are, with 4.5:1 text in every palette, and nothing moves when Reduce Motion is on.

## 11. Build order

1. **Tutor mode, the Help menu, agent tips.**
   - Core (built, 2026-11-01): the `[learn]` table, with the reserved profile names, and `visual` with its `--append-system-prompt` (§4.8). `outputStyle` and `spinnerTipsOverride` in the session's settings file. `tutor` on `spawn` and on rows. The words in both task parsers and in `complete`. The paragraph for generic harnesses. Your turn in teach mode (§4.4). Insight notes marked in the conversation (§4.5).
   - Mac app: the Learning tab, the New Task sheet's picker, the Help menu with the site's pages, sheets for the resources and the glossary, Mermaid diagrams drawn in the conversation (§4.8), and the message box in your turn.
   - Tests: settings-file goldens with and without a style and tips; the parsers' words; reserved names refused; a teach-mode stop with `TODO(human)` left alone by the dog and kept open by close-when-done, with the fake agent.
   - Docs: DESIGN §3.3, §4.2, §5.5; MAC.md; the site's Profiles, Tasks and Keys pages.
2. **The walkthrough and tips.** `learn.json` and the `learn` op, the Mac app's walkthrough and tips, `:tour`, `:help` and the status strip's tips in the terminal sidebar, the mid-session change (§4.3), and the phrase bar's tips. Bring the site's Your first session up to date, so it tells the walkthrough's story.
3. **Learning from your sessions.** The notes gathered, Explain This, lessons, questions and the review sheet, glossary hints, the practice agent, explainers the agent makes (§4.8), and Irie's own styles once M12's plugin lands.

## 12. Not doing

- **Points, streaks and badges.** Irie is meant to be calm, and nothing here should nag.
- **A course of Irie's own.** Good free courses exist (§7.2). Irie's material is the user's own work.
- **Editing the user's files** to set a style: no `~/.claude/settings.json`, no `.claude/output-styles/`, and no `/output-style` typed into a session (§4.1).
- **`companyAnnouncements`** (§6.2).

## Open decisions

| # | Decision | Options | Recommendation |
|---|---|---|---|
| 1 | Tutor mode's default | a. off. b. explain for new installs | **a**: it costs tokens, and the walkthrough offers it |
| 2 | The walkthrough's first task | a. A real task on `+haiku`, or `+local` when it is set up. b. The practice agent, free | **a** now; **b** in step 3 |
| 3 | Tips in the Mac app | a. Irie's own tip cards. b. TipKit without rules | **a** (§6.1) |
| 4 | The phrase bar | a. Vibes only, tips opt-in. b. Tips mixed in by default | **a** |
| 5 | A mid-session change | a. A note now, the style in full from the next reopen (§4.3). b. Restart the agent at its next wait | **a**: no second pass through the cache, and nothing the agent runs is cut off |
| 6 | Irie's own Newcomer style | a. After M12's plugin. b. Never: only Claude Code's styles | **a** |
| 7 | Pictures in a tutor mode | a. On by default. b. Off until asked for | **a**: words and pictures teach nearly everyone better (§2), and they come only where a picture helps |
