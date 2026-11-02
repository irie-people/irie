#!/bin/sh
# The checks a change must pass, in one place (AGENTS.md, "Stack"):
# tools/live.sh, the test workflow and the CI author agent all run it.
#
#   tools/check.sh            every package
#   tools/check.sh PKG...     go vet and go test for these packages only,
#                             such as ./internal/worktree, the way to check
#                             one package while the machine is busy
#
# It fails on a Go file gofmt would change, a go vet finding, a failing test
# or a row listed twice in PROVENANCE.md. gofmt and the PROVENANCE.md check
# always cover the whole checkout; they take a second.
set -eu
cd "$(git rev-parse --show-toplevel)"
if [ $# -eq 0 ]; then set -- ./...; fi

# Tracked files only: other checkouts can sit inside this one, as Claude
# Code's worktrees do under .claude/worktrees.
unformatted=$(git ls-files -z -- '*.go' | xargs -0 gofmt -l)
if [ -n "$unformatted" ]; then
	echo "check: gofmt would change these files; gofmt -w them:" >&2
	printf '%s\n' "$unformatted" | sed 's/^/check:   /' >&2
	exit 1
fi
sh tools/provenance.sh check
go vet "$@"
go test "$@"
