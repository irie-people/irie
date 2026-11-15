# Proposal: agent locks

Date: 2026-11-14. Status: built 2026-11-15, as DESIGN §4.21 describes; the owner approved all four steps that day. There is no issue tracker yet, so this file stood in for the issue that AGENTS.md asks for before a sizeable change.

## Goal

Irie can say when an agent it runs is set up in a way known to be vulnerable, and what to update. The idea comes from a public talk-to-camera video (lizthedeveloper on Instagram, 2026-09-19), offered freely: we have CVEs for packages but no registry of agent configurations known to be vulnerable, so record each agent's exact setup, as a lock file records exact versions, and check it.

## What exists today

- Public advisories already cover the parts. OSV.dev lists 30 advisories for the `@anthropic-ai/claude-code` npm package (2026-11-14). Two concern git worktrees, which Irie makes for each repo task: a trust dialog bypass through a spoofed worktree (GHSA-q5hj-mxqh-vv77, fixed in 2.1.84) and a sandbox escape through worktree path confusion (GHSA-7835-87q9-rgvv, fixed in 2.1.163). The newest, GHSA-5xjx-9qx6-jcrv (2026-11-09), lets the Edit tool write outside the workspace, fixed in 2.1.251.
- MCP servers and plugins ship as npm or PyPI packages, so OSV covers them by name and version too.
- Nothing lists combinations or settings, such as one MCP server together with automatic approval.

## What Irie would do

1. **A lock per session.** At spawn, the core writes `sessions/<id>/lock.json`: the harness and its version (`claude --version`, cached per binary), the profile and model, each MCP server with its package and version where it has one, Irie's plugins and the session's skills with a hash of each, the hooks, and the permission mode. A resumed or forked session gets a fresh lock.
2. **`irie lock [SESSION]`** prints the locks, one line per part.
3. **`irie lock --check`** asks OSV's batch API (`api.osv.dev/v1/querybatch`) about every harness and package version in the locks, and prints each advisory that affects one: its id, summary and fixed version. A query sends a package name and version and nothing else. It runs only when asked; `[lock] check = "daily"` would let the core run it once a day, off by default, since OSV is a Google service.
4. **A mark.** A session whose harness has a known advisory shows it in `ctl list` and on its card, and a peer's sessions (docs/proposals/multi-host.md) are checked the same way, since an unattended machine needs it most.

## Not yet

- A shared registry of vulnerable combinations, the other half of the idea, needs many people's locks; Irie's could feed one later, by explicit export only.
- Advisories for models themselves (a model's known jailbreaks) have no public feed to check against.

## Tests

A lock written for a fake harness and a fake MCP server; `--check` against an `httptest` stand-in for OSV that answers with an advisory for one version and none for the next; no test reaches the network.
