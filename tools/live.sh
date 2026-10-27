#!/bin/sh
# Installs this checkout's build into the Irie you run, so a finished change
# is live at once (AGENTS.md, "Trying a change").
#
# An install only moves forward. The checkout must be committed and must
# contain the build that is live, which the branch `live` records, so an
# install never drops another finished change. Everything is vetted, tested
# and built before the running Irie is touched. The new irie is renamed into
# place and its `irie up` replaces the core, the sidebar and the placeholder.
# Agents keep running, and each agent's host keeps the build it started with.
# If `irie up` fails, the previous build goes back.
#
# On macOS, when Irie.app is installed or running and mac/ changed since the
# installed app was built, it builds the app, installs it in ~/Applications
# and relaunches it if it was running. It never starts Irie or the app.
#
#   tools/live.sh             install this checkout
#   tools/live.sh --rollback  swap the previous irie back in
set -eu
cd "$(git rev-parse --show-toplevel)"

# Inside an Irie session IRIE_BIN names the build that started the session,
# and `irie up` would start that one again instead of the new one.
unset IRIE_BIN

# Irie's data directory, found the way internal/paths finds it.
case ${XDG_DATA_HOME:-} in
/*) data=$XDG_DATA_HOME/irie ;;
*) data=$HOME/.local/share/irie ;;
esac
bin=$data/bin/irie
app=${HOME:?}/Applications/Irie.app
appstamp=$data/bin/app.commit

say() { echo "live: $*" >&2; }

# rev prints the commit a Go binary was built from.
rev() { go version -m "$1" 2>/dev/null | sed -n 's/^[[:space:]]*build[[:space:]]*vcs\.revision=//p'; }

# running says whether a core answers.
running() { "$1" ctl list >/dev/null 2>&1; }

mkdir -p "$data/bin"
lock=$data/bin/live.lock
if ! mkdir "$lock" 2>/dev/null; then
	say "another install is running; if none is, rmdir $lock"
	exit 1
fi
trap 'rmdir "$lock"' EXIT
trap 'exit 1' HUP INT TERM

if [ "${1:-}" = --rollback ]; then
	if [ ! -e "$bin.prev" ]; then
		say "there is no previous install"
		exit 1
	fi
	mv "$bin" "$bin.swap" && mv "$bin.prev" "$bin" && mv "$bin.swap" "$bin.prev"
	if running "$bin"; then "$bin" up; fi
	r=$(rev "$bin")
	git update-ref refs/heads/live "$r"
	say "$(git log -1 --format='%h %s' "$r") is live again"
	exit 0
fi

if [ -n "$(git status --porcelain)" ]; then
	say "commit first, so that what is live is a commit"
	exit 1
fi
head=$(git rev-parse HEAD)
live=$(git rev-parse -q --verify refs/heads/live || true)
if [ -z "$live" ] && [ -e "$bin" ]; then
	# No branch yet: the build already installed is what is live.
	live=$(rev "$bin")
	if [ -n "$live" ]; then git update-ref refs/heads/live "$live"; fi
fi
if [ -n "$live" ] && ! git merge-base --is-ancestor "$live" HEAD; then
	say "this checkout lacks the live build, $(git log -1 --format='%h %s' "$live")"
	say "merge it, then run this again: git merge live"
	exit 1
fi

go vet ./...
go test ./...
go build -o "$bin.new" ./cmd/irie

newapp=
if [ "$(uname)" = Darwin ] && [ -d mac ] && { [ -d "$app" ] || pgrep -x Irie >/dev/null; }; then
	built=$(cat "$appstamp" 2>/dev/null || true)
	if [ ! -d "$app" ] || [ -z "$built" ] || ! git diff --quiet "$built" HEAD -- mac 2>/dev/null; then
		mac/build.sh
		newapp=mac/build/Irie.app
	fi
fi

# The rename leaves running processes on the old file, which .prev keeps.
up=
if running "$bin.new"; then up=1; fi
if [ -e "$bin" ]; then ln -f "$bin" "$bin.prev"; fi
mv -f "$bin.new" "$bin"
if [ -n "$up" ] && ! "$bin" up; then
	say "the new build did not come up, so the previous one goes back"
	if [ -e "$bin.prev" ]; then
		mv -f "$bin.prev" "$bin"
		"$bin" up || true
	fi
	exit 1
fi

pids=
if [ -n "$newapp" ]; then
	# The app keeps no state of its own (docs/MAC.md), so it is stopped, not
	# asked to quit. Launch Services would bring back an Irie that is still
	# running instead of starting the new one, so it waits for the exit.
	pids=$(pgrep -x Irie || true)
	front=
	if [ -n "$pids" ]; then
		if lsappinfo list 2>/dev/null | grep -q '"Irie" ASN:[^ ]* (in front)'; then front=1; fi
		kill $pids 2>/dev/null || true
		n=0
		while pgrep -x Irie >/dev/null && [ $n -lt 30 ]; do
			sleep 0.1
			n=$((n + 1))
		done
		pkill -9 -x Irie || true
	fi
	mkdir -p "$(dirname "$app")"
	rm -rf "$app"
	ditto "$newapp" "$app"
	echo "$head" >"$appstamp"
	if [ -n "$pids" ]; then
		if [ -n "$front" ]; then open "$app"; else open -g "$app"; fi
	fi
fi

git update-ref refs/heads/live "$head"
what=$(git log -1 --format='%h %s')
if [ -n "$up" ]; then
	say "$what is live; every agent kept running"
else
	say "$what is installed; Irie is not running, and $bin starts it"
fi
if [ -n "$pids" ]; then
	say "Irie.app is rebuilt and relaunched"
elif [ -n "$newapp" ]; then
	say "Irie.app is rebuilt in $app"
fi
