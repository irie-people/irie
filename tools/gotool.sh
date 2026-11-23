#!/bin/sh
# Runs one of the Go programs in tools/ that use the standard library
# alone, built once for each version of its source and kept in the
# repository's git folder, which every worktree shares. A check that runs
# one (tools/check.sh) then takes milliseconds rather than go run's build
# and link each time.
#
#   tools/gotool.sh NAME [ARGS...]   runs tools/NAME from the top of the
#                                    repository
set -eu
name=${1:?usage: tools/gotool.sh NAME [ARGS...]}
shift
root=$(git rev-parse --show-toplevel)
state=$(git rev-parse --path-format=absolute --git-common-dir)/irie-tools
# The source, not the folder: a file beside it, such as the coverage
# floors in tools/coverage, changes nothing to build.
key=$(cat "$root/tools/$name"/*.go | git hash-object --stdin)
bin=$state/$name-$key
if [ ! -x "$bin" ]; then
	mkdir -p "$state"
	go build -C "$root" -o "$bin.$$" "./tools/$name"
	mv -f "$bin.$$" "$bin"
	# Builds no run has made for a month go.
	find "$state" -maxdepth 1 -name "$name-*" -mtime +30 -exec rm -f {} + 2>/dev/null || true
fi
cd "$root"
exec "$bin" "$@"
