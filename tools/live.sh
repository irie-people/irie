#!/bin/sh
# Installs this checkout's build into the Irie you run, so a finished change
# is live at once (AGENTS.md, "Trying a change").
#
# An install only moves forward. The checkout must be committed and must
# contain the build that is live, which the branch `live` records, so an
# install never drops another finished change: when it lacks that build, the
# install merges it in and starts again, and a merge that conflicts stops it. Everything is vetted, tested
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
locked=
slot=
tmpbin=
macpid=
work=$(mktemp -d "${TMPDIR:-/tmp}/irie-live.XXXXXX")
cleanup() {
	if [ -n "$locked" ]; then
		rm -f "$lock/pid"
		rmdir "$lock" 2>/dev/null || true
	fi
	if [ -n "$slot" ]; then
		rm -f "$slot/pid"
		rmdir "$slot" 2>/dev/null || true
	fi
	if [ -n "$macpid" ]; then kill "$macpid" 2>/dev/null || true; fi
	if [ -n "$tmpbin" ]; then rm -f "$tmpbin"; fi
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

# take_lock waits up to $1 seconds for the lock. Only the swap holds it, a
# minute or so, so installs test side by side and queue only to swap.
take_lock() {
	waited=0
	while ! mkdir "$lock" 2>/dev/null; do
		pid=$(cat "$lock/pid" 2>/dev/null || true)
		if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
			say "the install that took the lock, pid $pid, is gone; rm -r $lock, then run this again"
			exit 1
		elif [ -n "$pid" ] && ps -o stat= -p "$pid" 2>/dev/null | grep -q '^T'; then
			# A stopped install, as when Irie freezes the session that ran
			# it, holds the lock until it is let go on.
			say "the install that holds the lock, pid $pid, is stopped, as when its session is frozen;"
			say "it goes on when that session is thawed, and no other install can run until it ends"
			exit 1
		elif [ "$waited" -ge "$1" ]; then
			say "another install is running${pid:+, pid $pid}; wait for it to finish, then run this again"
			exit 1
		fi
		if [ "$waited" = 0 ]; then say "another install is swapping its build in${pid:+, pid $pid}; waiting for it"; fi
		sleep 2
		waited=$((waited + 2))
	done
	echo $$ >"$lock/pid"
	locked=1
}

# take_slot waits for one of IRIE_LIVE_TEST_SLOTS (2) places to test and
# build in: each install's tests start test cores, tmux servers and agents,
# and more at once than the machine has memory for fail on time alone. A
# place whose install is gone is taken over.
take_slot() {
	n=${IRIE_LIVE_TEST_SLOTS:-2}
	said=
	while :; do
		i=1
		while [ "$i" -le "$n" ]; do
			s=$data/bin/live.test.$i
			if mkdir "$s" 2>/dev/null; then
				echo $$ >"$s/pid"
				slot=$s
				return
			fi
			pid=$(cat "$s/pid" 2>/dev/null || true)
			if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
				rm -f "$s/pid"
				rmdir "$s" 2>/dev/null || true
				continue
			fi
			i=$((i + 1))
		done
		if [ -z "$said" ]; then
			say "$n other installs are testing; waiting for a place"
			said=1
		fi
		sleep 5
	done
}

# catch_up REV WHAT merges REV, the live build or the running core's, into
# the checkout when the checkout lacks it, then starts the install again on
# the merged checkout, whose tests are cached for every package the merge
# left alone. A conflict stops the install, for whoever ran it to merge by
# hand. git writes a merged file anew rather than over the old one, so this
# script reads on undisturbed until the exec runs the merged copy.
catch_up() {
	if git merge-base --is-ancestor "$1" HEAD; then return; fi
	n=${IRIE_LIVE_MERGES:-0}
	if [ "$n" -ge 5 ]; then
		say "$2 moved five times during this install; run it again"
		exit 1
	fi
	say "this checkout lacks $2, $(git log -1 --format='%h %s' "$1" 2>/dev/null || echo "$1"); merging it"
	if ! TZ=UTC git merge --quiet --signoff --no-edit "$3" >&2; then
		files=$(git diff --name-only --diff-filter=U | paste -sd ' ' -)
		git merge --abort 2>/dev/null || true
		say "the merge conflicts${files:+ in $files}; merge it by hand, resolving each conflict against what you merge now, then run this again: TZ=UTC git merge --signoff $3"
		exit 1
	fi
	cleanup
	trap - EXIT
	IRIE_LIVE_MERGES=$((n + 1)) exec sh tools/live.sh
}

if [ "${1:-}" = --rollback ]; then
	take_lock 600
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
if [ -n "$live" ]; then catch_up "$live" "the live build" live; fi

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
	if [ -n "$corerev" ]; then catch_up "$corerev" "the running Irie's build" "$corerev"; fi
fi

# Every commit going into live needs a Conventional Commit subject
# (AGENTS.md, "Commits"): release-please reads them from main, where they
# never change again. The commits workflow sees pull requests only.
if [ -n "$live" ]; then
	for c in $(git rev-list --no-merges "$live..HEAD"); do
		if ! sh .github/conventional-commit.sh --rev "$c"; then
			say "reword it as above, then run this again"
			exit 1
		fi
	done
fi

# Only docs changed since the live build, which passed these checks: the
# tests and builds would only run again on the same code.
code=1
if [ -n "$live" ] && git diff --quiet "$live" HEAD -- . ':(exclude)docs' ':(exclude)site' ':(exclude,glob)*.md' 2>/dev/null; then
	code=
fi

if [ -n "$code" ]; then take_slot; fi

# Irie.app builds while the tests run, not after them.
newapp=
if [ "$(uname)" = Darwin ] && [ -d mac ] && { [ -d "$app" ] || pgrep -x Irie >/dev/null; }; then
	built=$(cat "$appstamp" 2>/dev/null || true)
	if [ ! -d "$app" ] || [ -z "$built" ] || ! git diff --quiet "$built" HEAD -- mac tools/mkicon 2>/dev/null; then
		mac/build.sh >"$work/mac.log" 2>&1 &
		macpid=$!
		newapp=mac/build/Irie.app
	fi
fi

if [ -z "$code" ]; then
	say "only docs changed since the live build, so the tests are skipped"
	sh tools/provenance.sh check
else
	# A session in macOS's background state (another one promoted, or Gabriel
	# holding it back while the user works on something else, DESIGN §4.11)
	# runs the tests slowly: give the slowest package time rather than fail the
	# install. A -timeout of your own in GOFLAGS wins.
	case " ${GOFLAGS:-} " in
	*" -timeout="*) ;;
	*) GOFLAGS="${GOFLAGS:+$GOFLAGS }-timeout=40m" && export GOFLAGS ;;
	esac
	# gofmt, go vet, go test and PROVENANCE.md's rows, as CI runs them.
	sh tools/check.sh
fi

# A change outside the Go code needs no new core, so the running one stays,
# unless it runs from somewhere else. Nor does one to the tests' helpers in
# internal/testutil, which no build of irie links.
gonew=1
installed=$(rev "$bin")
if [ -n "$installed" ] && { [ -z "$corebin" ] || [ "$corebin" = "$bin" ]; } &&
	git diff --quiet "$installed" HEAD -- go.mod go.sum cmd internal ':(exclude)*_test.go' ':(exclude)internal/testutil' 2>/dev/null; then
	gonew=
fi
# Built beside the install, not at $bin.new: other installs build at the same
# time, and only the swap below holds the lock.
if [ -n "$gonew" ]; then
	tmpbin=$bin.new.$$
	go build -o "$tmpbin" ./cmd/irie
fi
if [ -n "$macpid" ]; then
	if ! wait "$macpid"; then
		macpid=
		cat "$work/mac.log" >&2
		say "Irie.app did not build"
		exit 1
	fi
	macpid=
fi

if [ -n "$slot" ]; then
	rm -f "$slot/pid"
	rmdir "$slot" 2>/dev/null || true
	slot=
fi
take_lock 1800
# Another install may have landed while this one tested: the swap must not
# drop it. Its merge leaves the tests it did not touch cached.
live=$(git rev-parse -q --verify refs/heads/live || true)
if [ -n "$live" ]; then catch_up "$live" "an install that landed while this one tested" live; fi
core=$(tmux -L "${sock:-irie}" list-panes -a -f '#{==:#{@irie_role},core}' -F '#{pane_pid}' 2>/dev/null | head -1 || true)
if [ -n "$core" ]; then
	corerev=$(rev "$(ps -o command= -p "$core" | cut -d' ' -f1)")
	if [ -n "$corerev" ]; then catch_up "$corerev" "the running Irie's build" "$corerev"; fi
fi
if [ -n "$gonew" ]; then
	mv -f "$tmpbin" "$bin.new"
	tmpbin=
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
# While you use Irie.app (in front, with keyboard or mouse input in the last
# minute) an install never pulls the window from under you: the new app
# waits beside the old one, and the core swaps it in once another app is in
# front or you step away (Gabriel, DESIGN §4.11).
inuse=
if [ -n "$newapp" ] && pgrep -x Irie >/dev/null &&
	lsappinfo list 2>/dev/null | grep -q '"Irie" ASN:[^ ]* (in front)'; then
	idle=$(ioreg -c IOHIDSystem -d 4 -k HIDIdleTime 2>/dev/null | awk '/HIDIdleTime/ {print int($NF / 1000000000); exit}')
	if [ "${idle:-999}" -lt 60 ]; then inuse=1; fi
fi
if [ -n "$inuse" ]; then
	rm -rf "$app.next"
	ditto "$newapp" "$app.next"
	echo "$head" >"$appstamp.next"
	say "you are using Irie.app, so the new one waits beside it and swaps in once you look away"
	newapp=
fi
if [ -n "$newapp" ]; then
	rm -rf "$app.next" "$appstamp.next"
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
