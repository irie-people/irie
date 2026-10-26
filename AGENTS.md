# Working on Irie

These rules apply to every agent and every contributor.

## Public sources only

- Build from public documentation, the tools' own `--help`, and public open-source code whose license allows it.
- Record every source that shapes the code in `PROVENANCE.md` (date, URL, what it was used for) in the same change. Third-party code also gets its origin and license listed there.
- Never open, search or copy from private codebases. If a local guard blocks something, stop and say what you needed. Don't work around it.

## No personal data

Nothing in the repository should identify a person: no names, usernames, emails, home paths, hostnames or employers in code, tests, fixtures, docs, logs or screenshots. Use `alice`, `/home/alice` and `example.com`. Examples use widely known model names.

## Commits

Commit in UTC: `TZ=UTC git commit`.

## Stack

Go. The client runs every agent in tmux and draws its UI with Bubble Tea. Test with `go test ./...`.
