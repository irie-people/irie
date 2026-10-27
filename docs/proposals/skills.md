# Proposal: skills that come with Irie

Date: 2026-10-27. Status: proposed, for the maintainer to decide. There is no issue tracker yet, so this file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote this proposal. It is the design only and builds no code. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

## Goal

A new user gets a small set of open-source skills, installed and ready the first time Irie runs. Irie hands them to each session at launch and never writes into the user's own skills, settings or repositories. Each skill can be switched off, and so can the whole set. The set is kept up to date, but a pin moves only through a reviewed pull request.

A **skill** is a folder with a `SKILL.md`: a name and a short description that the agent sees in every session, instructions it reads only when it uses the skill, and sometimes scripts or reference files. The format is the open [Agent Skills](https://agentskills.io/specification) standard, which every harness named in DESIGN §3.2 except aider now reads.

**In short.**
- Claude Code gets the set through `--plugin-dir`, one flag per launch, as a plugin named `irie`. Nothing is written to its settings or to the user's skills folders.
- The repository tracks a list, not files: name, repo, path, commit, license and a sha256 for every file. Irie fetches exactly those files from GitHub into its data directory and refuses any that don't match.
- The first run downloads about 170 KB from `raw.githubusercontent.com`, and nothing else. It can be skipped, and Irie works the same without the skills.
- A job in the self-maintaining loop proposes new pins as pull requests, and a maintainer approves them. A user can opt in to following upstream, with a 14-day cooldown and the same hash checks.
- The default set has five skills, chosen against written criteria (§6).

---

## 1. Delivery: inject, never edit

Principle 4 of DESIGN §2.1 covers skills too: they go in per launch, through argv, and no user file is touched.

### 1.1 Claude Code: a session-only plugin

Each Claude Code launch gets one more flag (`<data>` is `$XDG_DATA_HOME/irie`):

```
claude --model claude-opus-5-5 --effort max --permission-mode auto \
       --settings <state>/sessions/<id>/claude-settings.json \
       --plugin-dir <data>/skills/plugins/<digest> \
       --session-id <uuid> [--name <slug>] "<task>"
```

Claude Code's docs say a plugin loaded this way "loads for that session only, and nothing is written to your settings for it", and that a `--plugin-dir` directory "loads in place and is never copied". `claude --help` lists the flag (checked on 2.1.310), so Irie finds it the way it finds every optional flag (DESIGN §3.3). A CLI without it gets no skills and a warning.

The folder is Irie's own:

```
<data>/skills/plugins/<digest>/
  .claude-plugin/plugin.json    written by Irie: {"name": "irie", "description": "…", "version": "…"}
  skills/<name>/…               each enabled skill's files, copied from the store, read-only
  skills/<name>/LICENSE…        the skill's license, beside it
  SOURCES.md                    written by Irie: each skill's repo, path, commit and license
```

| Rule | Why |
|---|---|
| Only `skills/`. Irie writes the manifest itself and never adds hooks, MCP servers, agents, commands or LSP servers | The list can name only files inside a skill's folder and its license file, so a skill can't bring other plugin parts with it |
| One folder per set, named by a digest of the skills, commits and hashes it holds. Files are 0444, and the folder never changes while a session uses it | A running session never sees a skill change under it, which would also break its prompt cache. Sessions with the same set share a folder |
| Real copies, not symbolic links into the store | Claude Code rejects a symbolic link that leads outside the plugin directory |
| Before each launch the core hashes the folder it is about to pass again, and rebuilds it from the store on a mismatch | The folder loads in place, and some agents edit skills where they find them. The default set is 22 files, so this is cheap |
| Only enabled skills go in the folder | Claude Code's `skillOverrides` setting doesn't apply to plugin skills, so leaving a skill out is the only per-skill switch |
| A changed set takes effect at the next launch or reopen | The same rule as for profiles |
| A folder that no live session uses is removed at the next core start | It is Irie's cache, not the user's data |

### 1.2 Names and clashes

Plugin skills are namespaced: Irie's `frontend-design` runs as `/irie:frontend-design`. The bare `/frontend-design` runs it too, unless something else already uses that name. So a user's own skill of the same name keeps the bare name, and Claude Code loads both, because "plugin skills are namespaced". Both would then sit in the model's skill listing, competing for the same job and costing context twice. Irie's rule is that the user's own wins.

| Case | What Claude Code does | What Irie does |
|---|---|---|
| The user has their own skill with that name, in a personal, project or enterprise location | Loads both; the bare name runs theirs | Leaves its copy out of that session. `irie skills` shows "yours is used" |
| The user has a plugin also named `irie` | The session-only plugin replaces theirs, silently for a marketplace plugin | Leaves its plugin out and says so. Irie never replaces a user's plugin |
| A plugin of the user's has a skill with that name (`tools:frontend-design`) | Loads both, each in its own namespace | Leaves both. Irie doesn't look inside other plugins; `irie skills disable` covers it |
| The user turned Irie's plugin off in their own settings, `"enabledPlugins": {"irie@inline": false}` | Doesn't load it | Nothing: it is the user's switch |
| Managed settings set `disableSideloadFlags` | Exits at start with an error naming the flag | Irie checks once per Claude Code version and set, with `claude --plugin-dir <dir> plugin list --json`, which also shows whether `irie@inline` loaded. If the flag is refused, Irie leaves it out on that machine, and the status strip says why |
| A profile passes `--safe-mode` | Loads no plugins | Nothing: the user asked for none |

To spot a skill of the same name, the core reads the folder names, and the `name` in each `SKILL.md`'s frontmatter, in the locations Claude Code's skills docs list for the session's directory ("Choose where skills load"). It only reads them.

### 1.3 Other harnesses

What differs between harnesses is not the format but whether a launch can add a folder without editing or moving the user's own configuration.

| Harness | Per-launch route that edits nothing | Plan |
|---|---|---|
| Claude Code | `--plugin-dir`, or `CLAUDE_CODE_PLUGIN_DIRS` from 2.1.280 | v1 (§1.1) |
| pi | `--skill <path>`, repeatable. A name clash "keeps the first discovered skill", with a warning; the order is not documented | With pi's adapter: one `--skill` per enabled skill, once a test confirms the user's own skill wins |
| opencode | `OPENCODE_CONFIG_CONTENT`, inline config that takes a `skills` list of paths. Later configs override earlier ones for the same key, so it may replace the user's own `skills` list, and the v2 docs name no such variable | Check when its adapter is built |
| Amp | Only `--settings-file`, which replaces the user's settings file. `amp.skills.path` in it adds folders, but workspace settings override it | Only through a generated copy of the user's settings. Check when its adapter is built |
| crush | `$CRUSH_SKILLS_DIR` is one of its global skill paths (v0.30.0 and later). Whether setting it hides the user's other global paths is not documented | Check before use. crush is FSL: Irie runs it and copies nothing |
| hermes-agent | None of its own: `-s` preloads skills by name, and `-p` or `HERMES_HOME` swap the whole home. A user can opt in once: `external_dirs` in their own config expands `${VAR}`, and those folders rank after the user's own skills | Document the one-line opt-in; Irie sets the variable per launch. hermes may edit a skill where it finds it, which the read-only files and the re-hash catch |
| Codex CLI | None: `[[skills.config]]` only turns a skill on or off, and `CODEX_HOME` would move the user's login and config | None until Codex offers one |
| Gemini CLI | None: `GEMINI_CLI_HOME` moves the whole home, and a system settings file can only disable skills | None until it offers one |
| goose | None: `GOOSE_PATH_ROOT` moves all of goose's config and data | None until it offers one |
| aider | No skills. `--read` puts whole files into every turn, with no index and no loading on demand | None |
| Any other CLI | — | None |

Several of these also read `.agents/skills` inside a repository. Irie never puts skills there, or anywhere else in a worktree: the worktree is the user's checkout, and an agent could commit them.

---

## 2. The tracked list

### 2.1 What is tracked

The repository holds one list, `internal/skills/list.toml`, embedded in the `irie` binary. It holds names, paths, commits and hashes, and no third-party text, so PROVENANCE's "No third-party code is copied into the repository" stays true. One real entry:

```toml
version = 1

[[skill]]
name = "frontend-design"            # the SKILL.md frontmatter name; runs as /irie:frontend-design
repo = "anthropics/skills"          # on github.com
path = "skills/frontend-design"     # the skill's folder in that repo
commit = "dbd4588f9e1033efb41dad4bef2f7947c8993d44"
license = "Apache-2.0"              # SPDX id
license_file = "skills/frontend-design/LICENSE.txt"
reason = "Gives agents a design brief for UI work, so it doesn't look templated."
files = [
  { path = "skills/frontend-design/LICENSE.txt", size = 10174, mode = "644", sha256 = "0d542e0c8804e39aa7f37eb00da5a762149dc682d7829451287e11b938e94594" },
  { path = "skills/frontend-design/SKILL.md",    size = 9390,  mode = "644", sha256 = "d91970639e9f5c37682ac7ab60094d35f1c7c1f38d731bd56396563aee10c1d3" },
]
```

| Field | Rule |
|---|---|
| `name` | Equals the `name` in the pinned `SKILL.md`, and meets the Agent Skills rule: at most 64 characters, lowercase letters, digits and hyphens |
| `repo`, `path`, `commit` | A full 40-character commit, never a branch or a tag |
| `license`, `license_file` | An OSI-approved license by SPDX id, and the file that holds its text: inside the skill's folder when it has one, otherwise the repository's own (Storybook's and superpowers' are at the root), which Irie keeps as `LICENSE` beside the skill. Source-available licenses are refused |
| `files` | Every file in the folder, plus the license file, each by its path in the repository, with its size, mode and sha256. Nothing else is fetched |
| `reason` | The line `irie skills` shows |

### 2.2 Fetching and checking

1. For each enabled skill missing from the store, the core fetches each listed file from `https://raw.githubusercontent.com/<repo>/<commit>/<file path>`. The URL names the commit, so what comes back can't change with the branch.
2. Each download must have the listed size and sha256 before it is written. A mismatch, a 404 or a timeout keeps that whole skill out and is reported with the file's name; the other skills are unaffected.
3. Verified files go to `<data>/skills/store/<repo>/<commit>/`, with their listed modes.
4. The core builds the plugin folders from the store (§1.1).

For this proposal, the 22 files of the default set were fetched this way at the commits in §6 and hashed. Each also matched the blob id in git's own tree for its commit. The appendix lists them.

### 2.3 Licenses

Every skill keeps its license file beside it, in the store and in each plugin folder. Irie never hands the files on: each user's machine fetches them from upstream, under the upstream license. `SOURCES.md` in each plugin folder lists each skill's origin and license, and `irie skills show` prints them. Four skills in `anthropics/skills` are source-available, and two candidates have no license file at all; none of them is in the set (§6.2).

### 2.4 Why not git submodules

| | Submodules | The list |
|---|---|---|
| Source downloads | GitHub's source archives are made by `git archive`, which writes a submodule as an empty folder (checked with git 2.43) | Irie fetches at run time, so every download of Irie works the same |
| `go install` | Go modules don't include submodules and won't: "Submodules will not be supported" (golang/go#24094) | As above |
| What gets pulled | A gitlink records one commit of a whole repository, and `git submodule update` clones it and checks out its whole tree. Storybook is over 1 GB on GitHub; its skill is one 1.7 KB file | Exactly the listed files: about 170 KB for the default set |
| What a pin covers | One commit per repository | One commit per skill, and a hash for every file |
| When it is checked | At checkout, by git's object ids | At every fetch, and before each launch |
| What Irie's repository holds | A pointer, with the third-party code in every recursive clone | Paths and hashes only |

A blobless, sparse `git fetch` can get one folder without the rest; that is how the research for this proposal read the repositories. It needs partial-clone support from both git and the server, and it still fetches commits and trees. Plain HTTPS downloads of known files are simpler, and they are easy to test against a local server.

---

## 3. Network and privacy

### 3.1 What is contacted

| When | Host | What is sent |
|---|---|---|
| The first run, and when a new Irie version changes the list | `raw.githubusercontent.com` (GitHub), over HTTPS | One GET per file: 22 files, about 170 KB, for the default set. A request carries the public path and `User-Agent: irie/<version>`: no token, no cookie, and nothing about the user's code, repos, tasks or agents |
| `irie skills update` | The same | The same, for missing or changed files |
| Daily, only with `follow_upstream = true` | `api.github.com` and `raw.githubusercontent.com` | Unauthenticated reads of each skill repository's public commits, trees and push history, then the files |

GitHub sees the machine's IP address and the public paths. `mirror` points the downloads at another host that serves the same paths, such as a company mirror, and the hashes are checked the same way. A mirror must use HTTPS, except on loopback for tests.

### 3.2 Skipping it

- **Before the first run.** `irie skills off` works without a running core and makes no network call. So does `[skills] enabled = false` in `config.toml`, or `IRIE_SKILLS=off` in the environment of the `irie` or `irie up` that starts the core, for CI and tests.
- **At the first run.** The status strip says what is being fetched, from where, and how to turn it off.
- **Never on its own.** `fetch = "manual"` fetches only on `irie skills update`.
- **Offline.** `irie skills update --from <dir>` installs from a copy made elsewhere, checked against the same hashes.

### 3.3 Irie without the skills

Nothing in Irie depends on the skills. With them off, missing or refused, a spawn just leaves out `--plugin-dir`. A spawn never waits on the network: if the first fetch is still running, that session starts without skills, and the status strip says so. Irie's own tests never fetch; they point `mirror` at a local test server.

### 3.4 What the skills themselves do

Skills are instructions to an agent, and they act only through the agent's own tools and permissions. Some send the agent online: `mcp-builder` reads the public MCP docs, and `handle-pr-comments` uses the user's `gh` to talk to GitHub. That traffic is the agent's, as it would be without Irie. A default skill may not send the user's code or questions to a third-party service on its own, which is one reason Context7's `find-docs` is left out (§6.2).

### 3.5 The FAQ answer

When this ships, in the same change, the answer to "Does Irie send my code anywhere?" in the site's FAQ (`site/faq.html` on the `irie/docs-site` branch) becomes:

> No. Irie runs on your computer and never sends your code anywhere. Each agent talks to its own AI company, just as it would without Irie.
>
> Irie goes online for one thing: it downloads a few open-source skills from GitHub, about 170 KB of files, the first time it runs and after an update that changes them. That download carries nothing about you or your code. To skip it, run `irie skills off` before you start Irie. Everything else works the same.
>
> Irie keeps a log of what each agent did, such as the commands it ran, on your computer for 30 days.

---

## 4. Updates

A skill is instructions to an agent that holds tools, so a new upstream commit is treated like a dependency update: pinned, hashed, reviewed, and never `@latest`.

### 4.1 Reviewed pins, the default

1. **Watch.** A scheduled job finds, for each skill, the newest commit that changed its folder or its license file and reached the branch at least 14 days ago. The age comes from `pushed_at` in GitHub's repository activity list, the time of the push that put the commit on the branch, and never from the date inside the commit, which its author sets.
2. **Propose.** If anything changed, the job fetches the new files, hashes them and opens a draft pull request that changes only `internal/skills/list.toml`. The pull request links GitHub's compare view and raises the flags below, and the reviewers get the full diff of the skill's files as a context file.
3. **Review.** Reviewer agents read that diff as untrusted text, since a skill is a prompt and may try to steer them, as in the self-maintaining proposal. A maintainer approves, and GitHub merges.
4. **Ship.** New pins reach users with the next Irie build. At its next start the core fetches the changed files and builds new plugin folders; running sessions keep theirs.

This is the shape of the self-maintaining proposal (`docs/proposals/self-maintaining.md` on the `irie/self-maintaining` branch): a job that runs no agent proposes, agents review, a person decides. The job's logic lives in `tools/skillpin`, so a maintainer can run the same thing by hand until the loop is switched on. The list joins the loop's protected paths: only this job may change it, never an author agent.

Each flag blocks the pull request until a maintainer accepts it in a review:

| Flag | Why it matters |
|---|---|
| The license changed, or its file is gone | The skill may no longer be open source |
| A file was added or removed, a file other than Markdown changed, or a mode became executable | Scripts are code the agent will run |
| The frontmatter adds or changes `allowed-tools`, `disallowed-tools`, `hooks`, `shell`, `model`, `effort`, `context` or `agent` | They change what the agent may do. `allowed-tools` grants tools without asking during the turn, and a skill's `hooks` stay registered for the rest of the session |
| A `` !`command` `` line appears | Claude Code runs it before the skill's text reaches the model |
| New hosts, or install and fetch commands such as `npx`, `pip install`, `curl … \| sh` or `@latest` | Network use and unpinned code |
| The name changed, or the description grew | Users' switches name skills, and every description costs context in every session |
| It names other skills by a plugin namespace (`other:skill`) | Those names break under `irie:` |

**Gone upstream.** If a skill's folder, repository or pinned commit disappears, the job proposes removing it. Users keep the copy in their store until then.

### 4.2 Following upstream, if a user opts in

`follow_upstream = true` trades review for freshness:
- Once a day the core asks GitHub for the newest commit that changed each skill's folder, and uses it once it reached the branch at least `cooldown` ago: 14 days or more, by `pushed_at` as above.
- Every file must match the blob id in GitHub's tree for that commit. Irie then writes a local lock with each file's sha256 and checks it before each launch, as for reviewed pins.
- A commit later force-pushed off the branch is dropped, and the skill goes back to the last good one.
- New `allowed-tools`, `hooks`, `shell` and injected commands are refused unless the reviewed pin already had them; such a commit is skipped until a reviewed pin catches up.
- `irie skills` marks these skills "unreviewed", with their commit and age. Turning the option off returns to the reviewed pins at the next core start.

---

## 5. Settings

### 5.1 Config keys

In `$XDG_CONFIG_HOME/irie/config.toml`. The values shown are the defaults.

```toml
[skills]
enabled = true                # false = never fetch, never pass skills to agents
disabled = []                 # names to leave out, e.g. ["mcp-builder"]
fetch = "auto"                # auto: the first run and when Irie's list changes | manual: only `irie skills update`
mirror = ""                   # "" = https://raw.githubusercontent.com; another host that serves the same paths
follow_upstream = false       # true = newer commits after the cooldown, hash-checked but unreviewed
cooldown = "336h"             # 14 days; shorter values are refused

[profiles.claude]
skills = true                 # false leaves the set out of this profile's sessions, e.g. a small local model
```

### 5.2 Commands

| Command | What it does |
|---|---|
| `irie skills` | Lists each skill: on or off, and why (config, a command, the user's own skill of that name, not fetched yet, a refused hash); its repo and commit; its license; its folder on disk |
| `irie skills on`, `irie skills off` | Turns the whole set on or off. `off` works before the first run and makes no network call |
| `irie skills enable <name>`, `irie skills disable <name>` | Turns one skill on or off |
| `irie skills update [--dry-run] [--from <dir>]` | Fetches what the current pins need and checks every hash; with `follow_upstream`, also looks for newer commits past the cooldown. `--dry-run` prints every URL it would fetch |
| `irie skills show <name>` | The skill's files and hashes, its license, its upstream URL at the pinned commit, and its reason |

Choices made with commands are kept in `$XDG_STATE_HOME/irie/skills.json`. `config.toml` wins over them: a command that would contradict it says so and changes nothing. The commands work with the core down; when it is up, they tell it over the socket, so the status strip agrees.

### 5.3 What a setting never touches

Irie writes only in its own places: the list inside its binary, the store and the plugin folders under `<data>/skills/`, and its choices in `$XDG_STATE_HOME/irie/`. It reads the names of the user's own skills only to step aside (§1.2). Turning a skill or the whole set off changes what Irie passes at launch and nothing else. The user's own skills, plugins and settings stay exactly as they were.

---

## 6. The default set

### 6.1 The choice

A skill is in the default set only if it:
1. helps with work Irie's users hand to agents, whatever their language or supplier;
2. is open source under an OSI-approved license, with a license file to keep beside it;
3. stands alone, without naming other skills by a plugin namespace;
4. adds a capability or a check, and doesn't change an agent's style or scope for the whole session;
5. runs no unpinned code and sends nothing to a third party on its own;
6. declares no `allowed-tools`, `hooks`, `shell` or injected commands.

| Skill | Source and commit | License | Size | Why |
|---|---|---|---|---|
| `frontend-design` | anthropics/skills, `skills/frontend-design` @ `dbd4588` | Apache-2.0 | 2 files, 19.6 KB | Agents' UI work tends to look templated; this gives them a design brief to meet and a critique step |
| `webapp-testing` | anthropics/skills, `skills/webapp-testing` @ `dbd4588` | Apache-2.0 | 6 files, 22.4 KB | Lets an agent check its web change in a headless browser before it says it's done. It needs Python and Playwright, which it doesn't install |
| `mcp-builder` | anthropics/skills, `skills/mcp-builder` @ `dbd4588` | Apache-2.0 | 10 files, 121.8 KB | MCP servers are how agents from any supplier get new tools, and this is a careful guide to building one. Its optional evaluation script calls Anthropic's API with the user's own key |
| `handle-pr-comments` | storybookjs/storybook, `.agents/skills/handle-pr-comments` @ `13c99b1` | MIT | 1 file and the repository's license, 2.8 KB | Works through the review comments on an agent's pull request one at a time and asks before each change, so its row turns red exactly when a person is needed |
| `verification-before-completion` | obra/superpowers, `skills/verification-before-completion` @ `bb92a77` | MIT | 1 file and the repository's license, 4.7 KB | A ✓ in Irie means an agent says it's done; this makes it run the check and show the output first |

Together they are 22 files and 171,256 bytes. Their names and descriptions add about 1,200 characters to every Claude Code session's skill listing, whose budget is 1% of the model's context window.

Each pin is the newest commit on its repository's default branch that was at least 14 days old on 2026-10-27. For this first list, age was counted by commit date; the watch job will use `pushed_at` (§4.1). Storybook marks its skill `internal` in its metadata, so it may move without notice; the pin and the watch job absorb that.

### 6.2 Left out

| Candidate | Why it is left out |
|---|---|
| `find-docs` (upstash/context7, MIT) | It runs `npx ctx7@latest` for every lookup, which is unpinned code from npm that no review sees, and it sends the agent's questions to Context7's service. Its description also asks for it "even when you think you know the answer". A version pinned to one CLI release would answer the first point |
| `vercel-composition-patterns` (vercel-labs/agent-skills, `skills/composition-patterns`) | Its README and frontmatter say MIT, but the repository has no license file to keep beside it. It is also only for React. Worth adding once upstream adds the file |
| `skill-creator` (anthropics/skills, Apache-2.0) | Its evaluation scripts start nested `claude -p` runs, which Irie can't see, pause or count; they unset `CLAUDECODE` to allow the nesting |
| `ponytail` (DietrichGebert/ponytail, MIT) | A working-style mode: its description says to use it "on any coding task", and it stays "active for the whole session". That is each user's choice, not a default |
| `codebase-onboarding` (ECC, MIT) | It writes a `CLAUDE.md` into the repository, and overlaps Claude Code's own `/init` |
| `context-budget` (ECC, MIT) | It mostly points to `/context` and `/status`, and 11 commits changed it up to its pin. Irie's own context gauges come after v1 (DESIGN §1.3) |
| `agent-harness-construction` (ECC, MIT) | Short, general advice that helps only people building agent harnesses |
| `systematic-debugging` (obra/superpowers, MIT) | It names other skills as `superpowers:…`, which break under `irie:` |
| `docx`, `pdf`, `pptx`, `xlsx` (anthropics/skills) | Source-available ("All rights reserved"), not open source |
| `doc-coauthoring` (anthropics/skills) | No license file |
| `claude-api` (anthropics/skills, Apache-2.0) | Claude Code already bundles its own, and the skill is about one supplier |
| `brand-guidelines`, `internal-comms`, `academy-guide`, `discernment-nudge` (anthropics/skills, Apache-2.0) | Anthropic's brand, company announcements, Anthropic's courses and follow-up questions on chat answers: not work in a repository |
| `algorithmic-art`, `canvas-design`, `slack-gif-creator`, `theme-factory`, `web-artifacts-builder` (anthropics/skills, Apache-2.0) | Art, GIFs, themes and claude.ai artifacts rather than code |

ECC is the MIT repository formerly called everything-claude-code.

---

## 7. Where it fits

- **DESIGN §2.1.** Principle 4 reads: "Hooks, settings, env and skills go in per launch, through `--settings`, `--plugin-dir`, argv and env." "Editing the user's agent config files" stays on §1.2's list of what v1 doesn't do.
- **DESIGN §8: a new milestone, M12. Skills.** It needs only what M1–M3 built: the spawn pipeline, profiles, and optional-flag detection from `claude --help`. It can be built any time after M3, and it must land before the first release, since that is when "the first time Irie runs" begins.
  - **Build.** `internal/skills` (the list, fetch, verify, the store, plugin folders, the check for the user's own skills); `--plugin-dir` in the Claude adapter; the `[skills]` config and the profile key; `irie skills`; `tools/skillpin`.
  - **Tests.** List validation. Fetches from an `httptest` server, with a bad hash, a 404 and a timeout; no test touches the network. The plugin folder's layout and digest. Argv goldens with and without skills. The user's-own-skill rule against a synthetic `/home/alice`. The fake agent records `--plugin-dir`.
  - **Manual check.** On a fresh account, run `irie`, spawn a task and type `/irie:` in Claude Code: the five skills are offered. Run `irie skills off`, spawn again, and they are gone.
- **DESIGN §1.3, item 1.** Each new harness adapter adds its route from §1.3: pi's `--skill` and opencode's inline config first, the others when their CLIs offer one. Codex, first in that list, has none yet.
- **DESIGN §2.6, §5.4, §5.5 and §9.** `internal/skills/` and `tools/skillpin/`; rows for the store, the plugin folders and `skills.json`; the `[skills]` section and the profile key; and three risks: a skill as a supply-chain path, a pinned commit that vanishes, and the first run's network call.
- **The self-maintaining loop.** The watch job (§4.1) is one more workflow behind `IRIE_AGENTS`, and the list joins its protected paths. Its CI profiles set `skills` on purpose, since a fresh runner would otherwise fetch the set for every job.
- **PROVENANCE.** When M12 lands, each listed skill goes under "Third-party code" as fetched at run time and never copied, with its license.
- **The site.** The FAQ answer of §3.5 and the roadmap's M12, in the same change as M12.

---

## Open decisions

| # | Decision | Options | Recommendation |
|---|---|---|---|
| 1 | The default set | a. The five in §6.1. b. a, plus `vercel-composition-patterns` once it has a license file. c. Fewer: drop `mcp-builder` | **a** now, then **b** |
| 2 | Where reviewed pins come from | a. Only from the `irie` binary, so they change with Irie. b. Also a reviewed list fetched from main | **a**: no extra network call, and the release stays the one thing to trust |
| 3 | The first run | a. Fetch by default, with the status-strip notice. b. Ask once in the sidebar first. c. Release archives carry the pinned files, so the first run needs no network | **a**, as asked. Weigh **c** at the first release: Irie would then hand the files on itself |
| 4 | A user's own skill of the same name | a. Irie's copy steps aside. b. Load both, namespaced | **a** |
| 5 | The plugin's name | a. `irie`. b. `irie-skills` | **a**: short commands, `/irie:<name>` |
| 6 | How a commit's age is measured | a. GitHub's `pushed_at`. b. When Irie first saw it. c. The commit's own date | **a**, falling back to **b** where the activity list has no entry; never **c** |
| 7 | Following upstream and new grants | a. Refuse new `allowed-tools`, `hooks`, `shell` and injected commands. b. Allow them | **a** |
| 8 | When to build it | a. M12, any time after M3 and before the first release. b. After v1 | **a** |
| 9 | Talking to upstream | Ask Vercel for a license file, Upstash for a pinned CLI version in `find-docs`, and Storybook whether `handle-pr-comments` stays where it is | Open the three issues before M12 |
| 10 | The next harness | a. pi, with its adapter. b. opencode | Whichever adapter lands first |

## Checks for this proposal

Every file in the appendix was downloaded at its commit from `raw.githubusercontent.com`, hashed, and checked against the blob id in that commit's tree, read with a blobless `git fetch`. Every candidate was read in full, and its frontmatter checked for the fields in §4.1. The scripts are not part of the change.

## Appendix: the default set's files

Paths are relative to each skill's folder; "the repository's" `LICENSE` is at the repository's root.

| Skill | File | Mode | Bytes | sha256 |
|---|---|---|---|---|
| frontend-design | `LICENSE.txt` | 644 | 10174 | `0d542e0c8804e39aa7f37eb00da5a762149dc682d7829451287e11b938e94594` |
| frontend-design | `SKILL.md` | 644 | 9390 | `d91970639e9f5c37682ac7ab60094d35f1c7c1f38d731bd56396563aee10c1d3` |
| webapp-testing | `LICENSE.txt` | 644 | 11345 | `bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362` |
| webapp-testing | `SKILL.md` | 644 | 3913 | `51b7349e77ec63b7744a6f63647e7566a0b4d2e301121cc10e8c2113af6556a2` |
| webapp-testing | `examples/console_logging.py` | 644 | 1027 | `ea46877289acb82da7e7ce59d0bc37c8977cd57e2a006d0c88d7a1c625bf95da` |
| webapp-testing | `examples/element_discovery.py` | 644 | 1463 | `d63c89604a22f8845d724e95dda45db49b1bf57c25ce0a83afbb7b8da3d402f0` |
| webapp-testing | `examples/static_html_automation.py` | 644 | 953 | `9d533aafb875ee3ab8b8ebf8f5b9003ac8d999da3d09b285cce252e623140064` |
| webapp-testing | `scripts/with_server.py` | 755 | 3693 | `b0dcf4918935b795f4eda9821579b9902119235ff4447f687a30286e7d0925fd` |
| mcp-builder | `LICENSE.txt` | 644 | 11345 | `bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362` |
| mcp-builder | `SKILL.md` | 644 | 9092 | `0f4592dcb53cf2b5d6b7febee6b4152018b565551a1c29e3c612f57b218ab295` |
| mcp-builder | `reference/evaluation.md` | 644 | 21663 | `8c99479f8a2d22a636c38e274537aac3610879e26f34e0709825077c4576f427` |
| mcp-builder | `reference/mcp_best_practices.md` | 644 | 7330 | `80fb4369a349447cf18ecdd7494fe7938b6065377e9f08c077cec411093a3007` |
| mcp-builder | `reference/node_mcp_server.md` | 644 | 28550 | `c3ba35a4f599dd53be9c6555ae72c19a7bf412cd5426576c2c08d42755482c66` |
| mcp-builder | `reference/python_mcp_server.md` | 644 | 25099 | `2da52f77e675191014ca2e146a4b95aa04d0ca7dd7e2b100322df15ade685e80` |
| mcp-builder | `scripts/connections.py` | 644 | 4875 | `9403668a2041568772082a8b334122c1f88daf0541fb393af4522d0094a47a6e` |
| mcp-builder | `scripts/evaluation.py` | 644 | 12579 | `49ed1d17cdce5da101b210197740713f49b935c29d4f339542a14b132658e6f7` |
| mcp-builder | `scripts/example_evaluation.xml` | 644 | 1194 | `9272b348ddcc4b06ba562367ccd0770e018158c0068ac5116d5e34aaeff8777a` |
| mcp-builder | `scripts/requirements.txt` | 644 | 29 | `d5d7558b2368ecea9dfeed7d1fbc71ee9e0750bebd1282faa527d528a344c3c7` |
| handle-pr-comments | `SKILL.md` | 644 | 1749 | `458b662e88d5c33ab7859862c882fabd53d6fc1f9075e77430cdc6fa9d327521` |
| handle-pr-comments | `LICENSE` (the repository's) | 644 | 1077 | `bc90586179d44dcb313a9a289d687a2f26226303b2066bd21dbefd2378e894e5` |
| verification-before-completion | `SKILL.md` | 644 | 3646 | `2befe7fc55bcadaa3d97dd9e8efeb633d2561c0ebe74c5a8b17c4d9e7e4520b3` |
| verification-before-completion | `LICENSE` (the repository's) | 644 | 1070 | `a37e0e9697144819e1d965176ac4ae5bc3fa02d11e7812036bbcadf6dafe2400` |

The full commits: anthropics/skills `dbd4588f9e1033efb41dad4bef2f7947c8993d44`, storybookjs/storybook `13c99b12126666cde465516d7a0227f07368eca9`, obra/superpowers `bb92a77741419a4ab5f06e711a283343f1ada0c3`.
