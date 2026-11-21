#!/bin/sh
# The checks a change must pass, in one place (AGENTS.md, "Stack"):
# tools/live.sh, the test workflow and the CI author agent all run it.
#
#   tools/check.sh            every package
#   tools/check.sh PKG...     go vet and go test for these packages only,
#                             such as ./internal/worktree, the way to check
#                             one package while the machine is busy
#
# It fails on a Go file gofmt would change, a go vet finding (for every
# package, also in a build without the plugins), a failing test or a row
# listed twice in PROVENANCE.md. gofmt and the PROVENANCE.md check
# always cover the whole checkout; they take a second. The tests run
# through tools/test.sh, which splits every package's tests over the CPUs.
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
# go vet runs beside the tests, which take the longest, and a finding
# fails the check once they are through.
vetdir=$(mktemp -d)
trap 'rm -rf "$vetdir"' EXIT
go vet "$@" >"$vetdir/1" 2>&1 &
vets=$!
# A build that leaves the plugins out (DESIGN §4.17), as a company's copy
# may, must still compile, its tests too: the stand-ins and tags can't
# drift from the code they stand in for.
if [ "$*" = ./... ]; then
	# A company's copy keeps the VPN and leaves the rest out; a build may
	# leave out every plugin (DESIGN §4.17, §4.18).
	go vet -tags noprivacy,nowriting,nomarkets,nomail,noterms,noextension,noyoink,nojammin ./... >"$vetdir/2" 2>&1 &
	vets="$vets $!"
	go vet -tags novpn,noprivacy,nowriting,nomarkets,nomail,nobitwarden,noterms,noextension,noyoink,nochat,noobsidian,nojammin ./... >"$vetdir/3" 2>&1 &
	vets="$vets $!"
fi
# The tests, in the least wall time (tools/test.sh; go test without cargo).
tested=0
sh tools/test.sh "$@" || tested=$?
vetted=0
for pid in $vets; do
	wait "$pid" || vetted=1
done
cat "$vetdir"/* >&2
if [ "$vetted" -ne 0 ]; then
	echo "check: go vet failed" >&2
fi
[ "$tested" -eq 0 ] && [ "$vetted" -eq 0 ]
