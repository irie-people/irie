# Proposal: Irie's website

Date: 2026-10-26. Status: proposed. There is no issue tracker yet, so this file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote this proposal and the site it describes.

## Goal

A small website, served by GitHub Pages from `site/`, that explains Irie so plainly that someone who has never opened a terminal gets it. It takes cues from go.dev: one clear line at the top, a short path to getting started, short pages and plain words. It copies none of go.dev's code, layout or wording.

## Pages

Every page opens with its title and then its picture, before any text. Home puts the definition of the word above its title, as the README does.

| Page | File | Picture | What it says |
|---|---|---|---|
| Home | `index.html` | The Irie window: the sidebar beside the slot | The definition of "irie". The tagline, "If you could do anything, what would you do first?". What Irie is, in one sentence. Then the four reasons from the README, each with a line on what is true today |
| What Irie is | `what-irie-is.html` | Your computer holding all of Irie, with one arrow out: from the agents to their AI companies | Added on 2026-10-27, first in the navigation so it is one click from every page. Everything, in full: what Irie does and never does, its parts, the programs it runs, every file it keeps and for how long, what it sends and where, what it does to agents and the three `[Irie]` notes word for word, what isn't built yet, who makes it, and commands to check each claim |
| How it works | `how-it-works.html` | You, Irie, tmux, agents and worktrees | You type a task; Irie starts an agent in tmux; Enter shows it live; hooks tell Irie how it is doing; what the colours mean; worktrees and profiles; what stays on your computer |
| Get started | `get-started.html` | How a spawn line reads | There is nothing to install yet. What you will need, and four steps of what using Irie will look like |
| Roadmap | `roadmap.html` | The eleven milestones | The milestones of DESIGN §8 as done, in progress or planned; what was built beside them (Irie for Mac, the conversation view, sessions outside Irie); then what comes after v1 (DESIGN §1.3) |
| FAQ | `faq.html` | Close the window and the agents keep going | Short answers to a newcomer's questions: what an agent is, the Mac app, which agents, local models, how a model is picked, cost, the name |
| Contribute | `contribute.html` | From an issue to Steak | The four steps of CONTRIBUTING, "Free, and paid back" and the Steak links from the README, and the house rules from AGENTS.md |

### Docs

Added on 2026-10-26, for using Irie rather than learning about it. A "Docs" item in the navigation leads to `docs.html`, which lists the pages in order; each docs page ends with the next page and a list of all of them. They follow the same rules as the rest of the site.

| Page | File | Picture | What it covers |
|---|---|---|---|
| Docs | `docs.html` | Ten stones on a path, Themes' stone empty because it is planned | The pages in order, and what the marks mean |
| Install | `install.html` | A terminal building Irie | Building from source with Go; what `irie` does on its first run; updating, keeping and removing it |
| Your first session | `first-session.html` | The window on a first run | Typing a task, reading the row, looking in, answering an agent, a finished task |
| Tasks, repos and worktrees | `tasks.html` | Two `@shop` tasks in worktrees | The task box, `@`, `@!` and `+`, how `@` finds a repo, worktrees, Tab completion, pastes, names |
| Profiles and models | `profiles.html` | `+local` picking a profile | The settings file and when a change takes effect, profiles, local models, generic agents, every setting |
| Other agents | `agents.html` | The two harnesses and the rows each gives | Added on 2026-10-27. What Irie sees of a `claude-code` and a `generic` agent, any program as a generic profile, a team's own agent: a launcher that starts Claude Code and what it must pass on, or a program of its own, trying a profile, and what an adapter needs |
| Themes | `themes.html` | Four themes as tiles, light beside dark | Planned. Rosewater (the default), Terracotta and Lilac in the colours the owner approved from warm-look's palettes, and Evergreen from `mac/Sources/Irie/Theme.swift`, all copied exactly: for each, a light and a dark sidebar, every state in order, and each colour's lowest contrast |
| Keys and commands | `keys.html` | Irie's key guide as keycaps | Every key, the keys to come, every command, `irie ctl` |
| Recipes | `recipes.html` | Three branches merged into main | Seven recipes, each with what works today and what is planned |
| Irie at work | `work.html` | Three laptops, each with its own Irie, and no Irie server | Added on 2026-10-27. Short answers for whoever decides, a company's managed Claude Code settings and what each does to Irie, its AI provider, proxy and launcher, the team's own agents, a team's settings file, and what isn't there |
| Troubleshooting | `troubleshooting.html` | Rows in trouble | Each state, message and warning, what to do, and where Irie keeps things |

Their facts come from the code and its `--help` output, checked by running Irie in a private session with its own folders and tmux socket.

## Pictures

All pictures are inline SVG drawn for this site. Each has `role="img"`, a `<title>` that names it and a `<desc>` that describes it, so it has alt text. Terminal pictures stay dark in both themes, as most terminals are; diagrams follow the page's theme.

| Picture | Page | Shows |
|---|---|---|
| The Irie window | Home | The sidebar as `internal/ui/view.go` draws it at its default 34 columns: header and counts, rows with glyphs and ages, the selected row's deep-green bar, the status strip and the command bar. Beside it, the slot with an agent at work. On a phone it crops to the sidebar |
| Everything on your computer | What Irie is | Irie, its tmux with three agents, its files and Ollama on 127.0.0.1, all inside your computer; the one arrow out runs from the agents to their AI companies |
| How it works | How it works | You, Irie, tmux, three agents and their worktrees, with hooks running back up to Irie |
| What the colours mean | How it works | Every state of DESIGN §5.3 with its glyph and colour; paused and frozen marked planned |
| A spawn line | Get started | `@shop +local fix the flaky checkout test`: where, who, what |
| Milestones | Roadmap | Eleven stones over two waves: done, in progress, planned |
| The window closes | FAQ | The window is only a view; the agents live in tmux and keep going |
| From an issue to Steak | Contribute | Issue, signed-off pull request, merge, a `steak:N` size, the public record |
| Four themes | Themes | Rosewater, Terracotta, Lilac and Evergreen as tiles, light on the left and dark on the right |
| A theme's sidebar | Themes | For each theme, in light and dark: the Mac app's sidebar as planned, with cards in every kind of state, then every state's mark in order. Each picture keeps its own colours in both of the site's themes |

Recordings come later. Each one has an empty box now, sized 16:10, whose caption doubles as its alt text and names the recording to come.

| Box | Page | Recording to come |
|---|---|---|
| `first-look` | Home | Opening Irie, typing a task and watching its row |
| `swap-in` | How it works | Pressing Enter on a row; the agent's screen moves into the slot |
| `first-task` | Get started | Typing a first task and watching it start |
| `needs-you` | Get started | A row turns red, and answering the agent |

## Look

- **Colours.** Irie's own, from `internal/ui/view.go`: sun `#F2B544`, deep green `#1E5A42`, on-green `#EAF6EF`, rule green `#3D5247`, sea `#2E9E77` and deep sea `#1D7357`. The state colours in the pictures stand in for the terminal's own red, green, yellow and cyan.
- **Themes.** Light and dark, following the system setting through `prefers-color-scheme`. Sun is too light for text on a light page (1.8:1), so there it only decorates and deep green carries text and links. On the dark page, sun carries links.
- **Contrast.** Text meets WCAG 2.2 AA, 4.5:1, in both themes; large text and meaningful graphics meet 3:1. So "done" is deep green in the light theme and sea in the dark one, and a sun-coloured shape that means something gets a dark outline.
- **Type.** The system's own fonts, sans for text and monospace for terminal text.
- **Nothing third-party.** Static HTML and one stylesheet. No JavaScript, no build step, no web fonts, no trackers or analytics.
- **Status marks.** "Built" for what is on main, which is not the same as released: there is no release yet, only building from source. "Planned" for the rest, linking to its milestone on the roadmap.

## Writing

- One idea per paragraph, two sentences at most.
- A word a newcomer would not know gets a one-line meaning where it first appears on each page, marked with `<dfn>`: terminal, coding agent, model, tmux, git, worktree, branch, hook, harness, profile, spawn line, tool call, subagent, Ghostty, Spotlight, issue, commit, pull request and Steak. Headings may leave the meaning to the paragraph under them.
- Only what is true of main today. Everything else is marked planned.
- Irie's fixed lines, such as the tagline, the Why lines and "Free, and paid back", come from [docs/PHRASES.md](../PHRASES.md) word for word.
- Examples use `alice`, `/home/alice`, `example.com`, a `shop` repo and widely known model names. No personal data.

## Deploy

`.github/workflows/pages.yml` publishes `site/` as it is, with GitHub's own Pages actions: `checkout` v7, `configure-pages` v6, `upload-pages-artifact` v5 and `deploy-pages` v5, the latest majors on 2026-10-26. It runs on pushes to main that touch `site/` or the workflow, and by hand.

| Step | Detail |
|---|---|
| One-time setup | The owner sets Settings → Pages → Build and deployment → Source to "GitHub Actions". The workflow cannot do this with the default token |
| Permissions | `contents: read`, `pages: write`, `id-token: write`; nothing else |
| Concurrency | One deployment at a time; a running one is never cancelled |
| URL | `https://irie-people.github.io/irie/` until a domain is chosen (open decision 1). Links between pages are relative, so the site works at any path and from disk |
| Artifact | `site/` alone, without dotfiles. A workflow deploy serves what it uploads; `.nojekyll` only matters when publishing from a branch |

## Checks before each change

Open every page from disk in a browser at desktop width (1280 px), phone width (390 px) and the narrowest width WCAG asks for (320 px), in both themes. Look for horizontal overflow, contrast below AA, missing alt text and broken links. This change was checked with headless Chrome driven through the DevTools protocol, which also measured the contrast of each picture's text against the shape behind it, followed every link and `#anchor` between pages, and confirmed that no page asks for anything outside `site/`. The script is not part of the change.

## Keeping it true

- **When a milestone merges.** The same pull request updates `site/roadmap.html` and the milestones picture, and removes that milestone's "Planned" marks. Each mark carries `data-m="<N>"`, so `grep -rn 'data-m="3"' site/` finds them, and `grep -rn milestone site/` finds the sentences that name one.
- **Milestone 3.** This branch was made on top of the M3 commit, which was not on main yet. The docs change that builds on it carries the M3 commit too, and marks M3 done, so M3 reaches main first or with it (open decision 4).
- **Milestones 6 and 10, and the rest of live.** On 2026-10-27 the site was merged with live, so it carries the code it describes: M6 and M10 are built there and marked done, M4 and M7 are in progress (rename, close and reopen, reorder and promote are built; park, jump, undo and notes are not), and M5, M8, M9 and M11 are planned, each checked against live's code. The site reaches main with that code, so "done means added to main" holds wherever the site is published. The themes stay Planned, as agreed with the session that asked for them.
- **When behaviour changes.** The same pull request updates the docs: keys are in `keys.html`, settings in `profiles.html`, and messages in `troubleshooting.html`.
- **What Irie is stays complete.** A change that adds or removes a part, a file Irie keeps, a connection, a program it runs, or a word it says to an agent updates `what-irie-is.html` in the same pull request. The page quotes every `[Irie]` note word for word, so `grep -rn '\[Irie\]' internal` finds what to compare.
- **When the themes are built.** The marks with `data-m="themes"` go, Themes' stone on the docs page fills, and the other pictures move to the default theme, Rosewater.
- **When the first release ships.** Install gains the download, and the "no download yet" lines on Home, Get started, the FAQ and the README go.
- **Links out.** The site links to `github.com/irie-people/irie`, `irie-people/stake` and `irie-people/steak`. None of them was public on 2026-10-26, so those links fail until they are.

## Open decisions

| # | Decision | Options | Recommendation |
|---|---|---|---|
| 1 | The site's address | a. `irie-people.github.io/irie`. b. A project domain, set in the Pages settings | **a** until the first release; choose a domain before announcing it |
| 2 | What Get started says at the first release | a. One install command, then the four steps already there. b. A page per platform | **a**: one command for macOS and Linux, and a line on what to do if tmux is missing |
| 3 | Recordings | a. Short MP4 or WebM files in `site/`, played with `<video>`, no autoplay. b. Animated GIF. c. Terminal recordings as text | **a**, made with a fresh `alice` account and the `shop` example, with captions |
| 4 | Order of merges | a. Merge M3 first, then this branch with M3 marked done. b. Rebase this branch onto main without M3 | **a**: this branch already sits on M3 |
| 5 | A theme switch | a. Follow the system only. b. A switch, which needs JavaScript | **a** |
| 6 | Guarding the site in CI | a. By hand, as above. b. A small static check in `go test ./...` for links, alt text and scripts | **a** for now; **b** once pages change often |
| 7 | Social preview card and a 404 page | Both need absolute URLs | After decision 1 |
| 8 | Pinning the Pages actions | a. Major tags, like `steak.yml`. b. Full commit SHAs | **a** for GitHub's own actions, matching `steak.yml` |
