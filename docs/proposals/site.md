# Proposal: Irie's website

Date: 2026-10-26. Status: proposed. There is no issue tracker yet, so this file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote this proposal and the site it describes.

## Goal

A small website, served by GitHub Pages from `site/`, that explains Irie so plainly that someone who has never opened a terminal gets it. It takes cues from go.dev: one clear line at the top, a short path to getting started, short pages and plain words. It copies none of go.dev's code, layout or wording.

## Pages

Every page opens with its title and then its picture, before any text.

| Page | File | Picture | What it says |
|---|---|---|---|
| Home | `index.html` | The Irie window: the sidebar beside the slot | What Irie is, in one sentence. Then the four reasons from the README, each with a line on what is true today |
| How it works | `how-it-works.html` | You, Irie, tmux, agents and worktrees | You type a task; Irie starts an agent in tmux; Enter shows it live; hooks tell Irie how it is doing; what the colours mean; worktrees and profiles; what stays on your computer |
| Get started | `get-started.html` | How a spawn line reads | There is nothing to install yet. What you will need, and four steps of what using Irie will look like |
| Roadmap | `roadmap.html` | The eleven milestones | The milestones of DESIGN §8, marked done only once merged on main, then what comes after v1 (DESIGN §1.3) |
| FAQ | `faq.html` | Close the window and the agents keep going | Short answers to a newcomer's questions: what an agent is, which agents, local models, cost, the name |
| Contribute | `contribute.html` | From an issue to Steak | The four steps of CONTRIBUTING, "Free, and paid back" and the Steak links from the README, and the house rules from AGENTS.md |

## Pictures

All pictures are inline SVG drawn for this site. Each has `role="img"`, a `<title>` that names it and a `<desc>` that describes it, so it has alt text. Terminal pictures stay dark in both themes, as most terminals are; diagrams follow the page's theme.

| Picture | Page | Shows |
|---|---|---|
| The Irie window | Home | The sidebar as `internal/ui/view.go` draws it at its default 34 columns: header and counts, rows with glyphs and ages, the selected row's deep-green bar, the status strip and the command bar. Beside it, the slot with an agent at work. On a phone it crops to the sidebar |
| How it works | How it works | You, Irie, tmux, three agents and their worktrees, with hooks running back up to Irie |
| What the colours mean | How it works | Every state of DESIGN §5.3 with its glyph and colour; paused and frozen marked planned |
| A spawn line | Get started | `@shop +local fix the flaky checkout test`: where, who, what, with where and who marked planned |
| Milestones | Roadmap | Eleven stones over two waves: done, in progress, planned |
| The window closes | FAQ | The window is only a view; the agents live in tmux and keep going |
| From an issue to Steak | Contribute | Issue, signed-off pull request, merge, a `steak:N` size, the public record |

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
- **Status marks.** "Built" for what is on main, which is not the same as released: there is nothing to install yet. "Planned" for the rest, linking to its milestone on the roadmap.

## Writing

- One idea per paragraph, two sentences at most.
- A word a newcomer would not know gets a one-line meaning where it first appears on each page, marked with `<dfn>`: terminal, coding agent, model, tmux, git, worktree, branch, hook, harness, profile, spawn line, tool call, subagent, Ghostty, Spotlight, issue, commit, pull request and Steak. Headings may leave the meaning to the paragraph under them.
- Only what is true of main today. Everything else is marked planned.
- Examples use `alice`, `/home/alice`, `example.com`, a `shop` repo and widely known model names. No personal data.

## Deploy

`.github/workflows/pages.yml` publishes `site/` as it is, with GitHub's own Pages actions: `checkout` v7, `configure-pages` v6, `upload-pages-artifact` v5 and `deploy-pages` v5, the latest majors on 2026-10-26. It runs on pushes to main that touch `site/` or the workflow, and by hand.

| Step | Detail |
|---|---|
| One-time setup | The owner sets Settings → Pages → Build and deployment → Source to "GitHub Actions". The workflow cannot do this with the default token |
| Permissions | `contents: read`, `pages: write`, `id-token: write`; nothing else |
| Concurrency | One deployment at a time; a running one is never cancelled |
| URL | `https://irie-hq.github.io/irie/` until a domain is chosen (open decision 1). Links between pages are relative, so the site works at any path and from disk |
| Artifact | `site/` alone, without dotfiles. A workflow deploy serves what it uploads; `.nojekyll` only matters when publishing from a branch |

## Checks before each change

Open every page from disk in a browser at desktop width (1280 px), phone width (390 px) and the narrowest width WCAG asks for (320 px), in both themes. Look for horizontal overflow, contrast below AA, missing alt text and broken links. This change was checked with headless Chrome driven through the DevTools protocol, which also measured the contrast of each picture's text against the shape behind it, followed every link and `#anchor` between pages, and confirmed that no page asks for anything outside `site/`. The script is not part of the change.

## Keeping it true

- **When a milestone merges.** The same pull request updates `site/roadmap.html` and the milestones picture, and removes that milestone's "Planned" marks. Each mark carries `data-m="<N>"`, so `grep -rn 'data-m="3"' site/` finds them, and `grep -rn milestone site/` finds the sentences that name one.
- **Milestone 3.** This branch was made on top of the M3 commit, which is not on main yet. Merging it as it is would bring M3 along; then M3 flips to done in the same change.
- **When the first release ships.** Get started gains the install steps, and the "nothing to install yet" lines on Home, Get started and the FAQ go.
- **Links out.** The site links to `github.com/irie-hq/irie`, `irie-hq/stake` and `steak-hq/steak`. None of them was public on 2026-10-26, so those links fail until they are.

## Open decisions

| # | Decision | Options | Recommendation |
|---|---|---|---|
| 1 | The site's address | a. `irie-hq.github.io/irie`. b. A project domain, set in the Pages settings | **a** until the first release; choose a domain before announcing it |
| 2 | What Get started says at the first release | a. One install command, then the four steps already there. b. A page per platform | **a**: one command for macOS and Linux, and a line on what to do if tmux is missing |
| 3 | Recordings | a. Short MP4 or WebM files in `site/`, played with `<video>`, no autoplay. b. Animated GIF. c. Terminal recordings as text | **a**, made with a fresh `alice` account and the `shop` example, with captions |
| 4 | Order of merges | a. Merge M3 first, then this branch with M3 marked done. b. Rebase this branch onto main without M3 | **a**: this branch already sits on M3 |
| 5 | A theme switch | a. Follow the system only. b. A switch, which needs JavaScript | **a** |
| 6 | Guarding the site in CI | a. By hand, as above. b. A small static check in `go test ./...` for links, alt text and scripts | **a** for now; **b** once pages change often |
| 7 | Social preview card and a 404 page | Both need absolute URLs | After decision 1 |
| 8 | Pinning the Pages actions | a. Major tags, like `steak.yml`. b. Full commit SHAs | **a** for GitHub's own actions, matching `steak.yml` |
