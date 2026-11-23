#!/bin/sh
# Runs the Go tests in the least wall time (DESIGN §2.6), as tools/check.sh
# and so tools/live.sh and CI do. tools/testrunner, in Rust, builds every
# test binary and the programs the tests start (irie and the fakes) once,
# then splits each package's tests over as many processes as the machine
# has CPUs, the longest first by earlier runs' times, so internal/core's
# tests no longer run one after another while the other CPUs wait. A test
# that passed is not run again until its binary or what it read changes,
# as go test's cache does for a whole package. It measures each package's
# coverage as go test -cover does, its processes' merged, and keeps it with
# the results, so a cached package says its coverage too. Without cargo, or
# with a flag only go test takes (-run, -json, -coverprofile and the like),
# it is go test.
#
#   tools/test.sh               every package
#   tools/test.sh PKG...        these packages
#   tools/test.sh -count=1      every test, whatever passed before
#   tools/test.sh -p 6          six test processes at once (default: one per
#                               CPU, half as many under 8 CPUs)
#   tools/test.sh --plan        the plan and its predicted time, running nothing
#   tools/test.sh --coverout=FILE
#                               also writes each package's coverage to FILE,
#                               for tools/coverage.sh; go test writes none
#
# The times and the kept results live in the repository's git folder, in
# testrunner/, which every worktree shares.
set -eu
cd "$(git rev-parse --show-toplevel)"
# A turn among the machine's test and build runs, and room on the disk.
sh tools/heavy.sh $$ test.sh
if [ $# -eq 0 ]; then set -- ./...; fi

# go test, for a machine without cargo or a flag the runner leaves to it:
# --coverout is the runner's alone.
gotest() {
	for a; do
		shift
		case $a in --coverout=*) ;; *) set -- "$@" "$a" ;; esac
	done
	exec go test "$@"
}

# rustup's place; no shell profile adds it here.
PATH=${CARGO_HOME:-$HOME/.cargo}/bin:$PATH
if ! command -v cargo >/dev/null 2>&1; then
	gotest "$@"
fi
state=$(git rev-parse --path-format=absolute --git-common-dir)/testrunner

# One build of each version of the runner serves every worktree: a
# worktree whose runner is as committed takes the build of that commit's
# tools/testrunner, made once.
runner=tools/testrunner
if git diff --quiet HEAD -- "$runner" 2>/dev/null && tree=$(git rev-parse -q --verify "HEAD:$runner" 2>/dev/null); then
	target=$state/build/$tree
else
	target=$runner/target
fi
if [ ! -x "$target/release/testrunner" ] || [ "$target" = "$runner/target" ]; then
	# A Rust too old for the runner, or a runner that does not build, leaves
	# the tests to go test rather than fail them.
	if ! cargo build --quiet --release --locked --manifest-path "$runner/Cargo.toml" --target-dir "$target"; then
		echo "test.sh: the test runner did not build, so go test runs the tests" >&2
		gotest "$@"
	fi
fi

# Builds of runners no commit has used for a month go.
find "$state/build" -mindepth 1 -maxdepth 1 -type d -mtime +30 -exec rm -rf {} + 2>/dev/null || true

set +e
"$target/release/testrunner" --state="$state" -cover \
	--helpers=./cmd/irie,./internal/testutil/... \
	--helpers-if=github.com/irie-people/irie/internal/testutil \
	--helpers-env=IRIE_TEST_BINS "$@"
code=$?
set -e
# 3: a flag the runner leaves to go test.
if [ "$code" -eq 3 ]; then
	gotest "$@"
fi
exit "$code"
