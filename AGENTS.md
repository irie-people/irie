# Working on Irie

These rules apply to every agent and every contributor.

## Public sources only

- Build from public documentation, the tools' own `--help`, and public open-source code whose license allows it.
- Record every source that shapes the code in `PROVENANCE.md` (date, URL, what it was used for) in the same change. Third-party code also gets its origin and license listed there.
- Never open, search or copy from private codebases. If a local guard blocks something, stop and say what you needed. Don't work around it.

## No personal data

Nothing in the repository should identify a person: no names, usernames, emails, home paths, hostnames or employers in code, tests, fixtures, docs, logs or screenshots. Use `alice`, `/home/alice` and `example.com`. Examples use widely known model names.

## Commits

Commit in UTC: `TZ=UTC git commit`. Sign off every commit (`git commit -s`, the DCO).

Write every commit subject and pull request title as a [Conventional Commit](https://www.conventionalcommits.org/en/v1.0.0/): `type(scope): description`. `type` is one of `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`, in lower case; the scope is optional; a `!` before the colon marks a breaking change. The commit-msg hook and the `commits` workflow refuse anything else and print the nearest subject that passes (`sh .github/conventional-commit.sh --title "<subject>"` checks one first); a merge keeps whatever message it has. release-please makes the next version and CHANGELOG.md from these commits, so a change never edits `Base` in `internal/version` or CHANGELOG.md; a release is a commit of its own.

## Stake

Contributions earn Steak under [irie-hq/stake](https://github.com/irie-hq/stake). Agree anything sizeable in an issue first, and say when an agent wrote part of a change.

## Stack

Go. The client runs every agent in tmux and draws its UI with Bubble Tea. Test with `go test ./...`.

## Trying a change

If you run Irie, put a finished change into it at once: commit, then run `tools/live.sh`. It runs `go vet` and `go test`, builds `irie`, and also `Irie.app` when `mac/` changed and the app is installed or running, then swaps the build into the running Irie. When the Go code changed, the core and the sidebar restart. Restarting any part of Irie must never disturb an agent or anything it started (a hard requirement, DESIGN §2.1 principle 6); the script checks every agent's process afterwards and fails loudly if one changed. Start Irie from that build, `~/.local/share/irie/bin/irie`, and never build over it by hand.

The branch `live` records what is installed. The script refuses a checkout that lacks it, or lacks the build the running core came from, so one change never drops another: run the `git merge` it prints and try again. `tools/live.sh --rollback` puts the previous build back.
