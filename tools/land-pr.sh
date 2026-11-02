#!/bin/sh
# Lands pull request N the way every change lands (AGENTS.md, "Trying a
# change"): it fetches the pull request's head, checks its commits as the
# install's push and the commits workflow would, merges it into this
# checkout, caught up with `live`, and runs tools/live.sh. With irie.push
# set, that install then moves the remote's main, and GitHub marks the pull
# request merged. GitHub's merge button would put the merging account's
# name and address on main, and land the change there without installing it.
#
#   tools/land-pr.sh N
set -eu
n=${1:-}
case $n in
'' | *[!0-9]*)
	echo "usage: tools/land-pr.sh N, where N is the pull request's number" >&2
	exit 2
	;;
esac
cd "$(git rev-parse --show-toplevel)"
say() { echo "land-pr: $*" >&2; }

if [ -n "$(git status --porcelain)" ]; then
	say "commit first, so that the merge is all that changes"
	exit 1
fi
remote=$(git config --get irie.push || echo origin)
git fetch --quiet "$remote" "refs/pull/$n/head"
pr=$(git rev-parse FETCH_HEAD)

if git merge-base --is-ancestor "$pr" HEAD; then
	say "#$n is in this checkout already"
else
	say "#$n brings:"
	git log --format='  %h %s' "HEAD..$pr" >&2
	# The same address rule as the install's push, before anything merges.
	strangers=$(git log --format='%h %ae %ce' "HEAD..$pr" |
		awk '{ for (i = 2; i <= 3; i++) if ($i !~ /@users\.noreply\.github\.com$/ && $i != "noreply@github.com") { print $1 " " $i; next } }')
	if [ -n "$strangers" ]; then
		say "not landed: these commits carry an address that is not a GitHub noreply one:"
		printf '%s\n' "$strangers" | sed 's/^/land-pr:   /' >&2
		exit 1
	fi
	for c in $(git rev-list --no-merges "HEAD..$pr"); do
		sh .github/conventional-commit.sh --rev "$c"
	done
fi

# merge REV MESSAGE merges with the commit rules, or stops at a conflict.
merge() {
	if ! TZ=UTC git merge --quiet --signoff -m "$2" "$1"; then
		say "resolve each conflict, commit with TZ=UTC git commit -s --cleanup=strip, then run tools/live.sh"
		exit 1
	fi
}
if git rev-parse -q --verify refs/heads/live >/dev/null && ! git merge-base --is-ancestor live HEAD; then
	merge live "Merge branch 'live'"
fi
if ! git merge-base --is-ancestor "$pr" HEAD; then
	merge "$pr" "Merge pull request #$n"
fi
exec tools/live.sh
