#!/bin/sh
# Finds code and prints it by its declarations, so that finding a thing
# and reading it take one call, not a grep and then a sed for each hit
# (AGENTS.md, "Stack"). Each call an agent makes reads its whole context
# again, so a call saved is worth more than the bytes it prints.
#
#   tools/code.sh def NAME... [PATH...]    each declaration named NAME, whole;
#                                          NAME may be TYPE.NAME
#   tools/code.sh outline PATH...          a file's declarations with their
#                                          lines, or a folder's files with
#                                          what each declares
#   tools/code.sh grep [-i] [-F] [-w] [--show[=LINES]] PATTERN [PATH...]
#                                          the matching lines under the
#                                          declaration each sits in; --show
#                                          prints those declarations whole
#   tools/code.sh refs [--show] NAME [PATH...]
#                                          where NAME is used in code, never
#                                          in a comment or a string
#
# tools/code.sh --help says more. The program, tools/code, is built once
# for each version of it and kept in the repository's git folder, which
# every worktree shares; a checkout that changes it builds its own.
set -eu
root=$(git rev-parse --show-toplevel)
state=$(git rev-parse --path-format=absolute --git-common-dir)/irie-tools
src=tools/code
if git -C "$root" diff --quiet HEAD -- "$src" 2>/dev/null &&
	[ -z "$(git -C "$root" ls-files --others --exclude-standard -- "$src")" ] &&
	tree=$(git -C "$root" rev-parse -q --verify "HEAD:$src" 2>/dev/null); then
	bin=$state/code-$tree
else
	# A changed program is built again for each change to its source.
	bin=$state/code-dev-$(cat "$root/$src"/*.go | cksum | tr -d ' ')
fi
if [ ! -x "$bin" ]; then
	mkdir -p "$state"
	go build -C "$root" -o "$bin.$$" ./$src
	mv -f "$bin.$$" "$bin"
	# Builds no commit has used for a month go.
	find "$state" -maxdepth 1 -name 'code-*' -mtime +30 -exec rm -f {} + 2>/dev/null || true
fi
exec "$bin" "$@"
