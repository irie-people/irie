#!/usr/bin/env bash
# check-commits.sh BASE HEAD
#
# Refuses an agent's commits before they are pushed
# (docs/proposals/self-maintaining.md). They must build on BASE without
# merges, be authored by the bot and committed and signed off by the
# maintainer who accepted the issue, stay within the size caps, add no
# submodule, symlink or anything that looks like a key, and leave alone every
# path that steers CI or agents. Each commit is checked on its own, so a later
# one cannot hide an earlier one.
#
# Environment: BOT_NAME, BOT_EMAIL, COMMITTER_NAME, COMMITTER_EMAIL, and
# optionally MAX_COMMITS (20) and MAX_LINES (3000).
set -euo pipefail

base=$1 head=$2
author="${BOT_NAME:?} <${BOT_EMAIL:?}>"
committer="${COMMITTER_NAME:?} <${COMMITTER_EMAIL:?}>"
max_commits=${MAX_COMMITS:-20}
max_lines=${MAX_LINES:-3000}
keys='sk-ant-[A-Za-z0-9_-]{16,}|sk-(proj-)?[A-Za-z0-9_-]{32,}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|AIza[0-9A-Za-z_-]{30,}|-----BEGIN [A-Z ]*PRIVATE KEY-----'

refuse() {
	echo "refused: $*" >&2
	exit 1
}

# protected reports whether a path steers CI or the agents themselves.
protected() {
	case $1 in
	.github | .github/* | .claude | .claude/* | */.claude | */.claude/* | .mcp.json | */.mcp.json | .gitmodules) return 0 ;;
	CLAUDE.md | */CLAUDE.md | CLAUDE.local.md | */CLAUDE.local.md | AGENTS.md | */AGENTS.md) return 0 ;;
	esac
	return 1
}

git merge-base --is-ancestor "$base" "$head" || refuse "the commits do not build on $base"
[ "$(git rev-parse "$base")" != "$(git rev-parse "$head")" ] || refuse "there are no commits"
[ -z "$(git rev-list --merges "$base..$head")" ] || refuse "there are merge commits"
n=$(git rev-list --count "$base..$head")
[ "$n" -le "$max_commits" ] || refuse "$n commits, more than $max_commits"

for c in $(git rev-list --reverse "$base..$head"); do
	short=$(git rev-parse --short "$c")
	a=$(git show -s --format='%an <%ae>' "$c")
	m=$(git show -s --format='%cn <%ce>' "$c")
	[ "$a" = "$author" ] || refuse "$short is authored by $a, not $author"
	[ "$m" = "$committer" ] || refuse "$short is committed by $m, not $committer"
	# grep reads all its input in these pipes: with -q it could stop early and
	# fail the writer, and pipefail would then read a match as none.
	git show -s --format=%B "$c" | grep -Fx "Signed-off-by: $committer" >/dev/null ||
		refuse "$short has no Signed-off-by: $committer"
	while IFS= read -r -d '' p; do
		if protected "$p"; then
			refuse "$short changes $p"
		fi
	done < <(git diff-tree -r --no-commit-id --name-only -z "$c")
	if git diff-tree -r --no-commit-id "$c" | grep -E '^:(160000 |[0-7]{6} 160000 )' >/dev/null; then
		refuse "$short changes a submodule"
	fi
	# A symlink could plant agent settings under another name, or lead a
	# reviewer's file tools out of its directory.
	if git diff-tree -r --no-commit-id "$c" | grep -E '^:[0-7]{6} 120000 ' >/dev/null; then
		refuse "$short adds a symlink"
	fi
done

read -r changed binary < <(git diff --numstat "$base" "$head" |
	awk '{ if ($1 == "-") b++; else s += $1 + $2 } END { print s + 0, b + 0 }')
[ "$changed" -le "$max_lines" ] || refuse "$changed changed lines, more than $max_lines"
[ "$binary" -eq 0 ] || refuse "$binary binary files"

if git diff "$base" "$head" | grep '^+' | grep -E -- "$keys" >/dev/null; then
	refuse "an added line looks like a key or a token"
fi
if git log --format=%B "$base..$head" | grep -E -- "$keys" >/dev/null; then
	refuse "a commit message looks like it holds a key or a token"
fi

echo "ok: $n commits, $changed changed lines"
