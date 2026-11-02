#!/bin/sh
# Hot loading for the Mac app (docs/MAC.md, "Hot loading"). It keeps a
# preview of this checkout's app open beside Irie.app, and a few seconds
# after each save under mac/ the preview shows the change: the build is
# incremental and unoptimized (mac/build.sh --hot), and each new build takes
# over the window of the build before once it has drawn, so the window seems
# to change in place. Nothing is installed and `live` does not move, so
# every checkout can keep a preview of its own; tools/live.sh still installs
# the finished change.
#
# A preview is a client of the running core, as Irie.app is: its sessions
# are the real ones, and what you do in it happens to them. Its terminals
# never size a window, so it never resizes an agent. It opens behind the app
# in front and never takes the focus, though it keeps the focus it has.
#
#   tools/hot.sh                 open the preview, and build it again on each change
#   tools/hot.sh --snapshot DIR  also draw the fixture into DIR after each build
#                                (the app's --snapshot), to look at without a screen
#   tools/hot.sh --no-open       build, and draw with --snapshot, with no preview
#   tools/hot.sh --once          build once, draw with --snapshot, and stop
#
# Closing the preview stops hot.sh. A preview that crashes, or that the
# freeze dog stops, opens again with the next build. What the preview
# prints shows here.
set -eu

say() { echo "hot: $*" >&2; }

snap= show=1 once=
while [ $# -gt 0 ]; do
	case $1 in
	--snapshot)
		if [ $# -lt 2 ]; then
			say "--snapshot needs a folder"
			exit 2
		fi
		snap=$2
		shift 2
		;;
	--no-open)
		show=
		shift
		;;
	--once)
		once=1 show=
		shift
		;;
	-h | --help)
		sed -n '2,/^set -eu/{/^set -eu/d;s/^# \{0,1\}//;p;}' "$0"
		exit 0
		;;
	*)
		say "unknown argument $1 (tools/hot.sh --help)"
		exit 2
		;;
	esac
done
case $snap in "" | /*) ;; *) snap=$PWD/$snap ;; esac

if [ "$(uname)" != Darwin ]; then
	say "the Mac app builds on macOS only"
	exit 1
fi
cd "$(git rev-parse --show-toplevel)"
root=$PWD
dir=$root/mac/build/hot
mkdir -p "$dir"

# One hot.sh per checkout: they would build into the same folder.
lock=$dir/lock
if ! mkdir "$lock" 2>/dev/null; then
	pid=$(cat "$lock/pid" 2>/dev/null || true)
	if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
		say "hot.sh already runs for this checkout, pid $pid"
		exit 1
	fi
	rm -rf "$lock"
	mkdir "$lock"
fi
echo $$ >"$lock/pid"

# The phrase bar names the checkout by its branch. The identifier, one per
# checkout, keeps each preview's window and choices apart from the app's and
# from other checkouts' previews.
name=$(git symbolic-ref -q --short HEAD || git rev-parse --short HEAD)
name=${name#irie/}
id=io.github.irie-hq.irie.preview.$(printf %s "$root" | shasum | cut -c1-10)

# A first preview takes the app's choices (palette, view, folds, sidebar
# width, font size) but not its window's frame, so it opens beside the app
# rather than over it. From then on it keeps its own, build after build.
if [ -n "$show" ] && ! defaults read "$id" >/dev/null 2>&1 &&
	defaults export io.github.irie-hq.irie "$dir/seed.plist" 2>/dev/null; then
	plutil -remove 'NSWindow Frame main' "$dir/seed.plist" 2>/dev/null || true
	defaults import "$id" "$dir/seed.plist"
	rm -f "$dir/seed.plist"
fi

# The preview that shows, and its slot. Builds take turns between two
# slots, A and B, so a new build never writes over the one that runs.
cur= slot= echoed=0

app() { echo "$dir/$1/Irie Preview.app"; }

# pids SLOT prints the processes that run the build in SLOT.
pids() {
	exe="$(app "$1")/Contents/MacOS/IriePreview"
	ps -axo pid=,comm= | while read -r pid comm; do
		if [ "$comm" = "$exe" ]; then echo "$pid"; fi
	done
}

alive() { [ -n "$1" ] && kill -0 "$1" 2>/dev/null; }

# status writes the line the preview shows in its phrase bar.
status() { printf '%s\n' "$*" >"$dir/status.new" && mv -f "$dir/status.new" "$dir/status"; }

# output prints what the shown preview wrote since the last look.
output() {
	if [ -z "$slot" ] || [ ! -f "$dir/$slot.log" ]; then return 0; fi
	size=$(wc -c <"$dir/$slot.log" | tr -d ' ')
	if [ "$size" -gt "$echoed" ]; then
		tail -c +$((echoed + 1)) "$dir/$slot.log" | head -c $((size - echoed)) >&2
		echoed=$size
	fi
}

# why PID says why a preview that is gone went: a crash report or a freeze
# dog report written since it opened. Nothing means it was closed.
why() {
	r=$(find "$HOME/Library/Logs/DiagnosticReports" -name 'IriePreview*' -newer "$dir/launched" 2>/dev/null | head -1)
	if [ -z "$r" ]; then
		r=$(find "${XDG_STATE_HOME:-$HOME/.local/state}/irie/freezes" -name "*-$1.txt" 2>/dev/null | head -1)
	fi
	if [ -n "$r" ]; then echo ": it crashed or froze, see $r"; fi
}

cleanup() {
	output
	if alive "$cur"; then kill "$cur" 2>/dev/null || true; fi
	rm -f "$dir/status"
	rm -rf "$lock"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

# build SLOT builds the preview into SLOT and says how it went, in the
# preview's phrase bar and here, with the compiler's errors when it failed.
build() {
	status building
	t0=$(date +%s)
	if IRIE_MAC_HOT_ID=$id mac/build.sh --hot "$dir/obj" "$(app "$1")" >"$dir/build.log" 2>&1; then
		status "ok $(date +%H:%M:%S)"
		say "built in $(($(date +%s) - t0)) s"
		return 0
	fi
	# The first error, as FILE:LINE: MESSAGE, for the phrase bar.
	err=$(grep -m1 'error:' "$dir/build.log" | sed -E 's|^([^:]*/)?([^/:]+):([0-9]+):[0-9]+: error: |\2:\3: |' || true)
	status "failed ${err:-see $dir/build.log}"
	cat "$dir/build.log" >&2
	if [ -n "$cur" ]; then
		say "the build failed; the preview keeps the build before it"
	else
		say "the build failed"
	fi
	return 1
}

# shot SLOT draws the fixture with the build in SLOT into the snapshot
# folder, offscreen.
shot() {
	if "$(app "$1")/Contents/MacOS/IriePreview" --snapshot "$snap" >"$dir/snapshot.log" 2>&1; then
		say "drew the fixture into $snap"
	else
		cat "$dir/snapshot.log" >&2
		say "the snapshot failed"
	fi
}

# launch SLOT opens the build in SLOT as a preview, behind the app in front,
# and waits for it to take over from the build before.
launch() {
	log=$dir/$1.log
	: >"$log"
	touch "$dir/launched"
	/usr/bin/open -n -g -F --stdout "$log" --stderr "$log" "$(app "$1")" --args \
		--background --preview "$name" --hot "$dir" --hot-pid $$ ${cur:+--replace "$cur"}
	new= n=0
	while [ -z "$new" ] && [ $n -lt 100 ]; do
		new=$(pids "$1" | head -1)
		if [ -z "$new" ]; then sleep 0.1; fi
		n=$((n + 1))
	done
	if [ -z "$new" ]; then
		say "the preview did not start; see $log"
		return 1
	fi
	output
	old=$cur oldslot=$slot oldechoed=$echoed
	cur=$new slot=$1 echoed=0
	if [ -z "$old" ]; then
		say "the preview is open"
		return 0
	fi
	# The new build ends the old one once it has drawn; one that has not
	# within 15 s is ended here.
	n=0
	while alive "$old" && alive "$cur" && [ $n -lt 150 ]; do
		sleep 0.1
		n=$((n + 1))
	done
	if ! alive "$cur"; then
		output
		say "the new build quit as it started$(why "$cur"); the preview keeps the build before it"
		cur=$old slot=$oldslot echoed=$oldechoed
		return 1
	fi
	if alive "$old"; then kill "$old" 2>/dev/null || true; fi
	n=0
	while alive "$old" && [ $n -lt 30 ]; do
		sleep 0.1
		n=$((n + 1))
	done
	rm -rf "${dir:?}/$oldslot"
	say "the preview shows the new build"
}

# sig changes whenever a source file, or the app's Info.plist, does.
sig() { stat -f '%Fm %z %i %N' mac/Info.plist mac/Sources/Irie/*.swift 2>/dev/null | cksum; }

last=
while :; do
	now=$(sig)
	if [ "$now" != "$last" ]; then
		# An edit often comes as several writes: build once they settle.
		sleep 0.3
		if [ "$(sig)" != "$now" ]; then continue; fi
		last=$now
		next=A
		if [ "$slot" = A ]; then next=B; fi
		# Nothing may still run from the slot a build goes to.
		for p in $(pids "$next"); do kill "$p" 2>/dev/null || true; done
		if build "$next"; then
			if [ -n "$snap" ]; then shot "$next"; fi
			if [ -n "$show" ]; then launch "$next" || true; fi
		elif [ -n "$once" ]; then
			exit 1
		fi
		if [ -n "$once" ]; then exit 0; fi
	fi
	output
	if [ -n "$cur" ] && ! alive "$cur"; then
		w=$(why "$cur")
		cur=
		if [ -z "$w" ]; then
			say "the preview was closed, so hot.sh stops"
			exit 0
		fi
		say "the preview is gone$w; the next build opens it again"
	fi
	sleep 0.5
done
