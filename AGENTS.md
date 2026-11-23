# Working on Irie

These rules apply to every agent and every contributor.

## Public sources only

- Build from public documentation, the tools' own `--help`, and public open-source code whose license allows it.
- Record every source that shapes the code in `PROVENANCE.md` (date, URL, what it was used for) in the same change. Third-party code also gets its origin and license listed there. `tools/provenance.sh add SOURCE "USED FOR"` adds the row, dated in UTC, without reading the whole file; `tools/provenance.sh find TEXT` says whether a source is there already.
- Never open, search or copy from private codebases. If a local guard blocks something, stop and say what you needed. Don't work around it.

## No personal data

Nothing in the repository should identify a person: no names, usernames, emails, home paths, hostnames or employers in code, tests, fixtures, docs, logs or screenshots. Use `alice`, `/home/alice` and `example.com`. Examples use widely known model names.

## Commits

Commit in UTC: `TZ=UTC git commit`. Sign off every commit (`git commit -s`, the DCO).

Write every commit subject and pull request title as a [Conventional Commit](https://www.conventionalcommits.org/en/v1.0.0/): `type(scope): description`. `type` is one of `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`, in lower case; the scope is optional; a `!` before the colon marks a breaking change. `tools/live.sh` and the `commits` workflow refuse anything else, and `.github/conventional-commit.sh` prints the nearest subject that passes (`sh .github/conventional-commit.sh --title "<subject>"` checks one first; as your `commit-msg` hook it checks each commit as you make it); a merge keeps whatever message it has. release-please makes the next version and CHANGELOG.md from these commits, so a change never edits `Base` in `internal/version` or CHANGELOG.md; a release is a commit of its own.

## Stake

Contributions earn Steak under [irie-people/stake](https://github.com/irie-people/stake). Agree anything sizeable in an issue first, and say when an agent wrote part of a change.

## Stack

Go. The client runs every agent in tmux and draws its UI with Bubble Tea. Check a change with `tools/check.sh`: gofmt, `go vet`, the tests and PROVENANCE.md's rows, as the install and CI check them, and two guards against drift (DESIGN §2.12). Every repository path a doc names in backquotes must exist: mark one not built yet "(planned)". No package's test coverage may fall under its floor in `tools/coverage/floor.txt`: bring tests, and never lower a floor to let a change through. When the check raises a floor, commit the file with your change. The tests run through `tools/test.sh`, a runner in Rust (`tools/testrunner`) that spreads every package's tests over the CPUs and reruns only what changed; without cargo it is `go test`. Name packages, such as `tools/check.sh ./internal/core`, to check fewer while the machine is busy.

Read a doc by its sections: `tools/doc.sh DESIGN 4.8` prints §4.8 of DESIGN.md, `tools/doc.sh DESIGN 4.8 6.4` both in one call, and `tools/doc.sh DESIGN` its outline, so you read only the part you need.

Find code with `tools/code.sh` rather than a grep and then a sed for each hit: every call reads your whole context again, so a call saved is the saving. `tools/code.sh grep PATTERN [PATH...]` lists each matching line under the Go or Swift declaration, or doc section, it sits in, and `--show` prints those declarations whole; `def NAME` (or `TYPE.NAME`) prints a declaration with its doc comment; `outline FILE` gives each declaration's lines, and a folder's files with what they declare; `refs NAME` lists the uses in code, never in comments or strings. `tools/habits.sh` ranks what agents did in Irie's journal by calls, the next things worth a script, and what failed most, with what worked next, the next things worth a line here or a fix at their source.

## Design

Every client draws from Roots, Irie People's design library, a repository of its own: [irie-people/roots](https://github.com/irie-people/roots) (checked out beside Irie, at `../roots` from the main checkout). It holds the palettes and numbers (`palettes/`, `tokens.json`), the rules (`principles.md`) and the parts (`components.md`). Change a colour or a number there, never in a client's copy; then here `go get github.com/irie-people/roots@main` and `tools/design.sh sync` write it into Irie's copies (DESIGN §2.11). When a change teaches something lasting about how Irie should look or behave, add it to the library, with its source and, where one can be written, a check.

## Trying a change

If you run Irie, put a finished change into it at once: commit, then run `tools/live.sh`, in the background if you like, since it waits its turn. It runs `tools/check.sh`, builds `irie`, and also `Irie.app` when `mac/` changed and the app is installed or running (alongside the tests), then swaps the build into the running Irie. The tests and builds run before it takes the install lock, two installs at a time (`IRIE_LIVE_TEST_SLOTS`), so installs test side by side and queue only for the swap, which waits its turn rather than fail; a change to docs alone (docs/, site/, top-level Markdown) installs without the tests. While the owner uses Irie.app, the new app waits beside it and swaps in once they look away, so an install never takes their focus. When the Go code changed, the core and the sidebar restart. Restarting any part of Irie must never disturb an agent or anything it started (a hard requirement, DESIGN §2.1 principle 6); the script checks every agent's process afterwards and fails loudly if one changed. Start Irie from that build, `~/.local/share/irie/bin/irie`, and never build over it by hand. An install ends by saying how long it took and where the time went, phase by phase.

Before you say a change makes something faster, or leaves its output alone, show it. `irie ab GOLD NEW -- ARGS` runs the build you started from and your new one on the same input, taking turns. It says whether the output is the same and whether the difference is more than noise (DESIGN §4.24).

While you change the Mac app, run `tools/hot.sh` in the background. It opens a preview of your checkout's app on a desktop the owner is not looking at, and shows each change a few seconds after you save it, with nothing installed. The owner can go to that desktop to watch the change take shape, and you get the compiler's errors and the app's output as they come. The preview's sessions are the real ones. `tools/hot.sh --snapshot DIR` also draws the fixture into DIR after each build, for you to look at. A finished change still goes in with `tools/live.sh`.

To see the views as pictures instead, `mac/snapshot.sh DIR` draws the app's windows into DIR from the same quick build, without showing a window or taking the owner's focus.

## Windows stay out of sight

A hard rule, for Irie and anything else you test: nothing you run puts a window where the owner works, in front of what they look at or on the desktop they are on, even behind, even for a moment. Work without one (`mac/snapshot.sh`, `--snapshot`, a browser headless). A window you must open goes through `irie away -- COMMAND`, which puts each window the command opens on a desktop they are not looking at; never `open` without `-g`, never `open -g` outside `irie away`, and never a tab in the owner's own browser. Irie's own tools keep to it (DESIGN §4.11, "Out of sight"), and the `out-of-sight` skill that comes with Irie says how.

The branch `live` records what is installed. When the checkout lacks it, or lacks the build the running core came from, the script merges that in and starts again, so one change never drops another; only a merge that conflicts stops it, for you to merge by hand and run it again. `tools/live.sh --rollback` puts the previous build back.

A maintainer who publishes Irie can set `git config irie.push origin`. Each install then also moves origin's main to the live build, only as a fast-forward and only after the repository's pre-push hook passes. It refuses a commit whose author or committer is not a GitHub noreply address. A push that fails leaves the install in place and prints why: when origin has commits the live build lacks, fetch and merge `origin/main`, then install again.

A maintainer lands a pull request with `tools/land-pr.sh N` rather than GitHub's merge button: it checks the pull request's commits as the push would, merges it into the checkout, caught up with `live`, and installs it, and the push then moves main.
