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
# On macOS, when Irie.app is installed or running and mac/ or its icon
# (tools/mkicon) changed since the installed app was built, it builds the
# app, installs it in ~/Applications and relaunches it if it was running. It
# never starts Irie or the app.
#
# With `git config irie.push <remote>` set, a finished install also moves
# that remote's main to the live build: a fast-forward or nothing, never a
# force, and only after the repository's own pre-push hook passes. A push that
# fails leaves the install in place and says why.
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
case ${XDG_CONFIG_HOME:-} in
/*) conf=$XDG_CONFIG_HOME/irie/config.toml ;;
*) conf=$HOME/.config/irie/config.toml ;;
esac
bin=$data/bin/irie
app=${HOME:?}/Applications/Irie.app
appstamp=$data/bin/app.commit

say() { echo "live: $*" >&2; }

# rev prints the commit a Go binary was built from.
rev() { go version -m "$1" 2>/dev/null | sed -n 's/^[[:space:]]*build[[:space:]]*vcs\.revision=//p'; }

# running says whether a core answers.
running() { "$1" ctl list >/dev/null 2>&1; }

# agents lists every agent pane with its process, so that an install can
# show it disturbed none (DESIGN §2.1, principle 6). The name comes last and
# only names the agent in a report: a row's name may change meanwhile, as
# when Claude Code titles a new session (DESIGN §4.2).
agents() {
	tmux -L "${sock:-irie}" list-panes -a -f '#{==:#{@irie_role},agent}' \
		-F '#{pane_id} #{pane_pid} #{pane_dead} #{@irie_name}' 2>/dev/null | sort || true
}

mkdir -p "$data/bin"
# The lock holds the pid of the install that took it, so one whose install
# died can be told from one still running, and a bare rmdir cannot remove a
# running install's lock: two at once would leave `live` with the later one,
# dropping the other's change.
lock=$data/bin/live.lock
if ! mkdir "$lock" 2>/dev/null; then
	pid=$(cat "$lock/pid" 2>/dev/null || true)
	if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
		say "the install that took the lock, pid $pid, is gone; rm -r $lock, then run this again"
	else
		say "another install is running${pid:+, pid $pid}; wait for it to finish, then run this again"
	fi
	exit 1
fi
echo $$ >"$lock/pid"
trap 'rm -f "$lock/pid"; rmdir "$lock"' EXIT
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

# A build put in by hand skips the branch, so the running core's own build
# must be in this checkout as well.
# The tmux socket: from the installed build, or before the first install
# from the config file itself.
sock=$({ "$bin" config print 2>/dev/null || cat "$conf" 2>/dev/null || true; } |
	sed -n '/^\[tmux\]/,/^\[/s/^[[:space:]]*socket[[:space:]]*=[[:space:]]*"\(.*\)"$/\1/p' | head -1)
core=$(tmux -L "${sock:-irie}" list-panes -a -f '#{==:#{@irie_role},core}' -F '#{pane_pid}' 2>/dev/null | head -1 || true)
corebin=
if [ -n "$core" ]; then
	corebin=$(ps -o command= -p "$core" | cut -d' ' -f1)
	corerev=$(rev "$corebin")
	if [ -n "$corerev" ] && ! git merge-base --is-ancestor "$corerev" HEAD; then
		say "the running Irie was built from $(git log -1 --format='%h %s' "$corerev" 2>/dev/null || echo "$corerev"), which this checkout lacks"
		say "merge it, then run this again: git merge $corerev"
		exit 1
	fi
fi

go vet ./...
go test ./...

# A change outside the Go code needs no new core, so the running one stays,
# unless it runs from somewhere else.
gonew=1
installed=$(rev "$bin")
if [ -n "$installed" ] && { [ -z "$corebin" ] || [ "$corebin" = "$bin" ]; } &&
	git diff --quiet "$installed" HEAD -- go.mod go.sum cmd internal ':(exclude)*_test.go' 2>/dev/null; then
	gonew=
fi
if [ -n "$gonew" ]; then go build -o "$bin.new" ./cmd/irie; fi

newapp=
if [ "$(uname)" = Darwin ] && [ -d mac ] && { [ -d "$app" ] || pgrep -x Irie >/dev/null; }; then
	built=$(cat "$appstamp" 2>/dev/null || true)
	if [ ! -d "$app" ] || [ -z "$built" ] || ! git diff --quiet "$built" HEAD -- mac tools/mkicon 2>/dev/null; then
		mac/build.sh
		newapp=mac/build/Irie.app
	fi
fi

before=$(agents)

# The rename leaves running processes on the old file, which .prev keeps.
up=
if [ -n "$gonew" ]; then
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
	# Launch Services keeps the icon it last read; registering the app again
	# makes Finder and the Dock show a new one.
	/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$app" 2>/dev/null || true
	echo "$head" >"$appstamp"
	if [ -n "$pids" ]; then
		if [ -n "$front" ]; then open "$app"; else open -g "$app" --args --background; fi
	fi
fi

git update-ref refs/heads/live "$head"

# Every agent must still run the process it ran before the install: the
# same pane, pid and dead flag, whatever its name is now.
after=$(agents)
changed=$(printf '%s\n' "$before" | while read -r pane pid dead name; do
	if [ -n "$pane" ] && ! printf '%s\n' "$after" | grep -qE "^$pane $pid $dead( |\$)"; then echo "$pane $pid $dead $name"; fi
done)
if [ -n "$changed" ]; then
	say "an agent's process changed during the install, which must never happen (DESIGN §2.1, principle 6):"
	printf '%s\n' "$changed" | while read -r pane pid dead name; do say "  $name: pane $pane, pid $pid, dead $dead before"; done
	exit 1
fi

what=$(git log -1 --format='%h %s')
if [ -z "$gonew" ]; then
	say "$what is live; the Go code did not change, so the core kept running"
elif [ -n "$up" ]; then
	say "$what is live; every agent kept running"
else
	say "$what is installed; Irie is not running, and $bin starts it"
fi
if [ -n "$pids" ]; then
	say "Irie.app is rebuilt and relaunched"
elif [ -n "$newapp" ]; then
	say "Irie.app is rebuilt in $app"
fi

# The push comes last, so that a slow or refused one never holds back the
# install. Every commit going out must carry a GitHub noreply address, as the
# commit hooks require (AGENTS.md, "No personal data"): a merge, or a commit
# made where the hooks did not run, never passed them. GIT_TERMINAL_PROMPT=0
# makes a missing login fail instead of wait.
remote=$(git config --get irie.push || true)
if [ -n "$remote" ]; then
	strangers=$(git log --format='%h %ae %ce' "$head" --not --remotes="$remote" |
		awk '{ for (i = 2; i <= 3; i++) if ($i !~ /@users\.noreply\.github\.com$/ && $i != "noreply@github.com") { print $1 " " $i; next } }')
	if [ -n "$strangers" ]; then
		say "not pushed: these commits carry an address that is not a GitHub noreply one:"
		printf '%s\n' "$strangers" | sed 's/^/live:   /' >&2
	elif out=$(GIT_TERMINAL_PROMPT=0 git push "$remote" "$head:refs/heads/main" 2>&1); then
		case $out in
		*"Everything up-to-date"*) say "$remote's main already has it" ;;
		*) say "$remote's main is the live build now" ;;
		esac
	else
		say "not pushed, so $remote's main stays where it was:"
		printf '%s\n' "$out" | grep -v -E '^(To |hint:)' | sed 's/^/live:   /' >&2
		case $out in
		*"fetch first"* | *non-fast-forward*)
			say "it has commits the live build lacks: git fetch $remote, merge $remote/main, then install again"
			;;
		esac
	fi
fi
