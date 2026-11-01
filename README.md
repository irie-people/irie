# Irie

> **i·rie** /ˈaɪ.ri/ · *adjective, Jamaican Patois*
> Good, pleasing, at peace. Everything's all right.
> *"Everything irie."*

**If you could do anything, what would you do first?**

Run every coding agent you have, from any supplier, on any model, cloud or local. Run them side by side and keep track of all of them in one calm view.

## Why

**Take back your time.** Hand the grind to a crew of agents working in parallel, each in its own git worktree. Spend your hours on the parts only you can do.

**Power to the people.** Your workflow shouldn't belong to one AI company. Irie works with any agent and any model supplier, including models running on your own machine. If one raises prices, rate-limits you or goes away, swap it out.

**Local when you can.** A model on your own hardware costs nothing per token and keeps your code at home. Let it take the small stuff.

**Many models, one love.** A fast local model for the small jobs, a strong cloud model for the hard ones, and a second opinion from a different supplier before anything reaches you.

## What it is

One Go program, `irie`, that runs on your computer, and an optional Mac app. It starts each agent in a private tmux server, follows Claude Code through hooks it adds to each session, and keeps its records in folders only you can open.

It has no server, account or telemetry, and sends nothing over the network of its own: your agents talk to their own providers, as they would without it. [What Irie is](https://irie-people.github.io/irie/what-irie-is.html) lists every part, file, connection and word it says to an agent, and how to check each one yourself.

## Install

On a Mac or Linux, paste this into a terminal:

```sh
curl -fsSL https://irie-people.github.io/irie/install.sh | sh
```

It downloads the latest release for your computer, checks its SHA-256, and installs `irie`, and on a Mac with Apple silicon Irie.app too; a release with no download for your computer is built from source instead, which needs Go. Then `irie setup` checks what else Irie needs, tmux, Claude Code and a sign-in, and offers to install each part that is missing, asking first. Open Irie from Spotlight, or type `irie`: its welcome offers a first task to try. [Install](https://irie-people.github.io/irie/install.html) says what each step does, and how to build Irie from source.

## Status

Early: see the [roadmap](https://irie-people.github.io/irie/roadmap.html).

## Docs

The website has the docs, from installing Irie to a merged change. Its pages are in [`site/`](site/), and open from disk too.

- [Install](https://irie-people.github.io/irie/install.html): one line, what `irie setup` checks, and what Irie does on its first run
- [Your first session](https://irie-people.github.io/irie/first-session.html): type a task, read its row, look in, and answer an agent that needs you
- [Tasks, repos and worktrees](https://irie-people.github.io/irie/tasks.html): `@repo`, `@repo!`, `+profile`, Tab completion, pastes, and what a worktree gets
- [Profiles and models](https://irie-people.github.io/irie/profiles.html): the config file, every setting with its default, and profiles for local models
- [Other agents](https://irie-people.github.io/irie/agents.html): any agent that runs in a terminal, including a launcher or harness your own team built, and what Irie sees of each
- [Themes](https://irie-people.github.io/irie/themes.html): the four planned colour themes, Rosewater, Terracotta, Lilac and Evergreen, in light and dark
- [Keys and commands](https://irie-people.github.io/irie/keys.html): every key, and `irie ctl` for scripts
- [Recipes](https://irie-people.github.io/irie/recipes.html): from an idea to a merged change, with what works today and what is planned
- [Irie at work](https://irie-people.github.io/irie/work.html): Irie at a company: cost, data, managed Claude Code settings, AI providers, proxies, and what isn't there yet
- [Troubleshooting](https://irie-people.github.io/irie/troubleshooting.html): each stall, failure and message, and what to do about it

## Free, and paid back

**Get the value you'd pay for, free, today.** If Irie ever makes money (donations, sponsorships, grants), it all goes back to the people who build it, after running costs, in proportion to the value of what they contributed. The founder started with zero Steak and earns it the same way as everyone else. The record is public: [irie-people/stake](https://github.com/irie-people/stake). It runs on [Steak](https://github.com/irie-people/steak) v0.2.0.

## Help

[SUPPORT.md](SUPPORT.md) says where to ask, and [SECURITY.md](SECURITY.md) how to report a vulnerability privately. Everyone here follows the [Code of Conduct](CODE_OF_CONDUCT.md), and [GOVERNANCE.md](GOVERNANCE.md) says how decisions are made.

## License

[Apache-2.0](LICENSE). Built from public documentation; every source is listed in [PROVENANCE.md](PROVENANCE.md).
