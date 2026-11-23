#!/bin/sh
# Holds a heavy run, a test or build run, back until the machine has room
# for it (DESIGN §2.9). tools/test.sh and tools/check.sh call it before
# they start anything, and tools/live.sh before its checks and builds. On
# 2026-11-23 three sessions ran internal/core's tests at once on an 18 GB
# Mac whose disk was 97% full: the memory ran out, the swap had no room
# to grow, and the kernel panicked.
#
#   tools/heavy.sh PID [NAME]     waits for room, then counts process PID
#                                 as a run until it ends; NAME starts the
#                                 lines it says
#   tools/heavy.sh --done PID     counts PID no more, as when an install is
#                                 through its tests and builds
#   tools/heavy.sh --room [NAME]  fails with the reason when the free disk
#                                 is under the floor, as tools/live.sh asks
#                                 before it tests or builds anything
#
# Two things hold a run back, and it says which in a line.
#
# The runs going: no more at once than one per 4 GB of free memory, and
# one always may. The free memory is what internal/localmodel reads: the
# share of the RAM the kernel counts as free on macOS
# (kern.memorystatus_level, the "memory free percentage" memory_pressure
# prints), MemAvailable on Linux. IRIE_HEAVY_SLOTS sets the number
# instead. Each run is a file in heavy/ in Irie's data folder, named by
# its pid and holding when that process started, so a run that ended, or
# whose pid a new process took, counts no more; nor does a stopped one,
# as in a frozen session, whose memory the free memory leaves out already.
# A run inside another, as check.sh's tests or an install's checks, is
# part of it and waits for nothing.
#
# The free disk where the swap grows, /System/Volumes/VM on macOS and /
# elsewhere: under the floor, half the RAM and at least 10 GiB, as
# sysload.Room.Floor reckons it (internal/sysload/room.go), a run waits up
# to IRIE_HEAVY_WAIT seconds (600) for room, then stops with the reason.
# A CI runner, thrown away after its run, only says so and goes on.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
eval "$(sh "$root/tools/brand.sh")"
case ${XDG_DATA_HOME:-} in
/*) dir=$XDG_DATA_HOME/$BRAND_SLUG/heavy ;;
*) dir=${HOME:?}/.local/share/$BRAND_SLUG/heavy ;;
esac

name=heavy
say() { echo "$name: $*" >&2; }
usage() {
	echo "usage: tools/heavy.sh PID [NAME] | --done PID | --room [NAME]" >&2
	exit 2
}

# whole says whether $1 is a whole number.
whole() {
	case $1 in '' | *[!0-9]*) return 1 ;; esac
}

# size says bytes as sysload.Size does: 8.2 GB, 640 MB.
size() {
	awk -v n="$1" 'BEGIN {
		if (n >= 1e9) printf "%.1f GB", n / 1e9
		else if (n >= 1e6) printf "%d MB", (n + 5e5) / 1e6
		else printf "%d KB", (n + 500) / 1e3
	}'
}

# ram prints the bytes of memory, or nothing when it can't tell. A sysctl
# that answers comes first, as on macOS; Linux's has no hw.memsize.
ram() {
	n=$(sysctl -n hw.memsize 2>/dev/null || true)
	if whole "$n"; then
		echo "$n"
		return
	fi
	n=$(awk '/^MemTotal:/ {print $2; exit}' /proc/meminfo 2>/dev/null || true)
	if whole "$n"; then echo $((n * 1024)); fi
}

# memfree prints the bytes of free memory, or nothing when it can't tell.
memfree() {
	level=$(sysctl -n kern.memorystatus_level 2>/dev/null || true)
	if whole "$level"; then
		total=$(ram)
		if whole "$total"; then
			if [ "$level" -gt 100 ]; then level=100; fi
			echo $((total / 100 * level))
			return
		fi
	fi
	n=$(awk '/^MemAvailable:/ {print $2; exit}' /proc/meminfo 2>/dev/null || true)
	if whole "$n"; then echo $((n * 1024)); fi
}

# turns sets slots, how many runs may go at once now, and why, in words.
per=4000000000
turns() {
	if whole "${IRIE_HEAVY_SLOTS:-}" && [ "$IRIE_HEAVY_SLOTS" -gt 0 ]; then
		slots=$IRIE_HEAVY_SLOTS
		why="IRIE_HEAVY_SLOTS makes room for $slots"
		return
	fi
	m=$(memfree)
	slots=1
	if whole "$m"; then
		if [ $((m / per)) -gt 1 ]; then slots=$((m / per)); fi
		why="$(size "$m") of free memory makes room for $slots, at 4 GB a run"
	else
		why="with the free memory unknown there is room for one"
	fi
}

# disk sets free, the bytes free where the swap grows, and floor, under
# which no run starts. free is empty when df can't tell, and then nothing
# waits for it, as with Room.Known.
disk() {
	vol=/System/Volumes/VM
	if [ ! -d "$vol" ]; then vol=/; fi
	free=$(df -Pk "$vol" 2>/dev/null | awk 'NR == 2 {print $4}' || true)
	if whole "$free"; then free=$((free * 1024)); else free=; fi
	floor=$((10 * 1024 * 1024 * 1024))
	r=$(ram)
	if whole "$r" && [ $((r / 2)) -gt "$floor" ]; then floor=$((r / 2)); fi
}
starved() { [ -n "$free" ] && [ "$free" -lt "$floor" ]; }
short() { echo "only $(size "$free") of the disk is free, under the $(size "$floor") floor that leaves the swap room to grow"; }
clearing() { echo "go clean -cache empties Go's build cache, and $BRAND_SLUG ctl raphael names the biggest caches safe to clear"; }
# refuse stops the run with the reason, after $1 when it waited.
refuse() {
	say "${1:-}$(short), so no test or build starts; clear $(size $((floor - free))) or more, then run it again: $(clearing)"
	exit 1
}
# span says seconds as 10m or 90s.
span() {
	if [ "$1" -gt 0 ] && [ $(($1 % 60)) -eq 0 ]; then echo "$(($1 / 60))m"; else echo "${1}s"; fi
}

# look prints what the runs' files and the process table say: "inside"
# when a forebear of process $1 is a run, which $1 is then part of; "gone
# PID" for each file whose process ended or is another by now; and
# "going PID..." with the runs going but $1, less the stopped ones. A
# process is told by its pid and when it started, read in UTC and the C
# locale so that every reader writes it alike; "unknown" when ps lists
# nothing.
look() {
	{
		for f in "$dir"/*; do
			case ${f##*/} in *[!0-9]*) continue ;; esac
			printf 'run %s %s\n' "${f##*/}" "$(cat "$f" 2>/dev/null || true)"
		done
		TZ=UTC LC_ALL=C ps -A -o pid= -o ppid= -o stat= -o lstart= 2>/dev/null || true
	} | awk -v me="$1" '
		$1 == "run" { s = $3; for (i = 4; i <= NF; i++) s = s " " $i; run[$2] = s; next }
		{ s = $4; for (i = 5; i <= NF; i++) s = s " " $i; up[$1] = $2; started[$1] = s; stat[$1] = $3; procs++ }
		END {
			if (!procs) { print "unknown"; exit }
			for (p = up[me]; p > 1 && !(p in seen); p = up[p]) {
				seen[p] = 1
				if ((p in run) && run[p] == started[p] && stat[p] !~ /^Z/) { print "inside"; exit }
			}
			going = "going"
			for (p in run) {
				if (run[p] != started[p] || stat[p] ~ /^Z/) print "gone " p
				else if (p != me && stat[p] !~ /^T/) going = going " " p
			}
			print going
		}'
}

# hold writes process $1's file, through a rename so that no one reads
# half of it.
hold() {
	s=$(TZ=UTC LC_ALL=C ps -o lstart= -p "$1" 2>/dev/null | awk '{$1 = $1; print}')
	if [ -z "$s" ]; then
		say "there is no process $1"
		exit 2
	fi
	mkdir -p "$dir"
	echo "$s" >"$dir/.$1.$$"
	mv -f "$dir/.$1.$$" "$dir/$1"
}

case ${1:-} in
--done)
	whole "${2:-}" || usage
	rm -f "$dir/$2"
	exit 0
	;;
--room)
	name=${2:-$name}
	disk
	if starved; then refuse; fi
	exit 0
	;;
esac
whole "${1:-}" || usage
pid=$1
name=${2:-$name}
wait=${IRIE_HEAVY_WAIT:-600}
whole "$wait" || wait=600
waited=0
tries=0
said_disk=
said_turn=
while :; do
	out=$(look "$pid")
	case $out in
	unknown)
		say "ps listed no process, so this run takes no turn"
		exit 0
		;;
	inside*) exit 0 ;;
	esac
	for p in $(printf '%s\n' "$out" | sed -n 's/^gone //p'); do rm -f "$dir/$p"; done

	disk
	if starved; then
		if [ "${CI:-}" = true ]; then
			if [ -z "$said_disk" ]; then say "$(short); going on, as a CI runner is thrown away after its run"; fi
			said_disk=1
		elif [ "$waited" -ge "$wait" ]; then
			if [ "$waited" -gt 0 ]; then refuse "after a wait of $(span "$waited"), "; fi
			refuse
		else
			if [ -z "$said_disk" ]; then say "$(short); waiting up to $(span "$wait") for room: $(clearing)"; fi
			said_disk=1
			sleep 5
			waited=$((waited + 5))
			continue
		fi
	fi

	# Every newcomer writes its file, then counts the others: two that come
	# at once each count the other, so they never go past the limit
	# together, though both may wait.
	hold "$pid"
	going=$(look "$pid" | sed -n 's/^going//p')
	turns
	# shellcheck disable=SC2086 # the pids, one word each
	set -- $going
	if [ $(($# + 1)) -le "$slots" ]; then exit 0; fi
	rm -f "$dir/$pid"
	if [ -z "$said_turn" ]; then
		if [ $# -eq 1 ]; then
			runs="1 test or build run is going (pid $1)"
		else
			runs="$# test or build runs are going (pids $(echo "$*" | sed 's/ /, /g'))"
		fi
		say "waiting for a turn: $runs, and $why"
		said_turn=1
	fi
	# Two that wait wake at times of their own, two to three seconds on,
	# so that they don't keep meeting and both wait again.
	tries=$((tries + 1))
	sleep "$(awk -v seed=$((pid * 1000 + tries)) 'BEGIN { srand(seed); printf "%.2f", 2 + rand() }')"
done
