#!/usr/bin/env bash
# rehearse.sh plays the demo tapes against a sandboxed Irie through tmux
# instead of vhs: the same keys, pauses and waits, in a terminal the size
# vhs would draw. It saves the screen once a second and logs each cue, so a
# tape can be checked, and its chapters timed, on a machine without vhs,
# ttyd or a browser (docs/demo/README.md).
#
#   docs/demo/rehearse.sh [TAPE...]   default: every tape in docs/demo
#
# For each tape it writes dist/demo/rehearsal/NAME/: frames/ (the screen,
# with its colours, once a second), story.txt (each caption and the screen
# just after it), chapters.txt and timing.txt. It fails when a Wait times
# out, and when a frame shows a path or a name from outside the sandbox.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
out=$repo/dist/demo/rehearsal

die() { echo "rehearse.sh: $*" >&2; exit 1; }
now() { perl -MTime::HiRes=time -e 'printf "%.3f", time'; }

# setting prints a tape's "Set NAME value".
setting() { sed -n "s/^Set $2 //p" "$1" | tail -n 1; }

# size prints the terminal vhs would draw as "COLS ROWS". xterm.js's canvas
# renderer floors a cell's width and rounds its height up from the font's
# advance and line height, here Menlo's (1233 and 2384 of 2048 units a
# size); letter spacing adds whole pixels. vhs's defaults fill in what a
# tape does not set.
size() {
	awk -v fs="$(setting "$1" FontSize)" -v w="$(setting "$1" Width)" -v h="$(setting "$1" Height)" \
		-v pad="$(setting "$1" Padding)" -v ls="$(setting "$1" LetterSpacing)" -v lh="$(setting "$1" LineHeight)" '
		function or(v, d) { return v == "" ? d : v }
		BEGIN {
			fs = or(fs, 22); w = or(w, 1200); h = or(h, 600); pad = or(pad, 60); ls = or(ls, 1); lh = or(lh, 1)
			cw = int(fs * 1233 / 2048) + int(ls + 0.5)
			ch = fs * 2384 / 2048
			if (ch > int(ch)) ch = int(ch) + 1
			ch = int(ch * lh)
			print int((w - 2 * pad) / cw), int((h - 2 * pad) / ch)
		}'
}

# secs turns a vhs duration (500ms, 2s, 1m, or bare seconds) into seconds.
secs() {
	awk -v d="$1" 'BEGIN {
		if (d ~ /ms$/) { sub(/ms$/, "", d); print d / 1000 }
		else if (d ~ /m$/) { sub(/m$/, "", d); print d * 60 }
		else { sub(/s$/, "", d); print d + 0 }
	}'
}

viewer() { tmux -S "$root/run/viewer" "$@"; }
send() { viewer send-keys -t v "$@"; }

# key maps a vhs key name to tmux's.
key() {
	case $1 in
	Enter | Tab | Space | Escape | Up | Down | Left | Right) echo "$1" ;;
	Backspace) echo BSpace ;;
	PageUp) echo PPage ;;
	PageDown) echo NPage ;;
	Delete) echo DC ;;
	Insert) echo IC ;;
	*) return 1 ;;
	esac
}

# type_text types like vhs: a key, then the typing speed's pause. A lone
# ";" would end a tmux command, so it goes as hex.
type_text() {
	local text=$1 delay=$2 i c
	for ((i = 0; i < ${#text}; i++)); do
		c=${text:i:1}
		if [ "$c" = ";" ]; then send -H 3b; else send -l -- "$c"; fi
		sleep "$delay"
	done
}

# wait_screen waits until the screen matches an extended regular expression
# (vhs's Wait+Screen; tapes keep to what both RE2 and grep -E read alike).
wait_screen() {
	local timeout=$1 re=$2 deadline
	deadline=$(awk -v n="$(now)" -v t="$timeout" 'BEGIN { printf "%.3f", n + t }')
	# grep reads to the end, so pipefail never sees capture-pane's SIGPIPE.
	until viewer capture-pane -t v -p | grep -E -- "$re" >/dev/null; do
		if awk -v n="$(now)" -v d="$deadline" 'BEGIN { exit !(n > d) }'; then
			die "$name.tape:$lineno: Wait timed out after ${timeout}s for /$re/"
		fi
		sleep 0.1
	done
}

# record saves the screen once a second while the tape is shown.
record() {
	local n=0
	while [ -e "$dir/.recording" ]; do
		if [ -e "$dir/.shown" ]; then
			n=$((n + 1))
			printf '%04d\t%s\n' "$n" "$(now)" >>"$dir/frames/index"
			viewer capture-pane -t v -e -p >"$dir/frames/$(printf %04d "$n").txt" 2>/dev/null || true
		fi
		sleep 1
	done
}

play() {
	local tape=$1 line cmd rest at speed timeout text count k n
	# vhs's defaults: 50ms a key, and 15s for a Wait.
	speed=0.05
	timeout=15
	lineno=0
	while IFS= read -r line || [ -n "$line" ]; do
		lineno=$((lineno + 1))
		line=${line#"${line%%[![:space:]]*}"}
		case $line in '' | '#'*) continue ;; esac
		cmd=${line%% *}
		rest=${line#"$cmd"}
		rest=${rest# }
		at=
		case $cmd in *@*) at=$(secs "${cmd#*@}") cmd=${cmd%%@*} ;; esac
		case $cmd in
		Output | Require) ;;
		Set)
			case ${rest%% *} in
			TypingSpeed) speed=$(secs "${rest#* }") ;;
			WaitTimeout) timeout=$(secs "${rest#* }") ;;
			esac
			;;
		Hide) rm -f "$dir/.shown" ;;
		Show) touch "$dir/.shown" ;;
		Type)
			k=${rest:0:1}
			text=${rest:1}
			text=${text%"$k"*}
			type_text "$text" "${at:-$speed}"
			;;
		Sleep) sleep "$(secs "$rest")" ;;
		Ctrl+*)
			k=${cmd#Ctrl+}
			send "C-$(printf '%s' "$k" | tr '[:upper:]' '[:lower:]')"
			;;
		Wait+Screen)
			text=${rest#/}
			wait_screen "${at:-$timeout}" "${text%/}"
			;;
		*)
			k=$(key "$cmd") || die "$name.tape:$lineno: rehearse.sh does not play \"$cmd\""
			count=${rest:-1}
			for ((n = 0; n < count; n++)); do
				send "$k"
				sleep "${at:-$speed}"
			done
			;;
		esac
	done <"$tape"
}

# report turns the cue log into chapters.txt, timing.txt and story.txt.
report() {
	local log=$dir/cues.log
	[ -s "$log" ] || die "$name.tape: no cues were logged"
	awk -F'\t' '
		function stamp(t) { t = int(t + 0.5); return sprintf("%d:%02d", t / 60, t % 60) }
		NR == 1 { t0 = $1 }
		$2 == "caption" && $4 != "" { printf "%s %s\n", stamp($1 - t0), $4 }
	' "$log" >"$dir/chapters.txt"
	awk -F'\t' '
		NR == 1 { t0 = $1 }
		$2 == "end" { end = $1 }
		$2 == "caption" { captions++ }
		END {
			printf "length %d:%02d from the first cue to the end\n", (end - t0) / 60, (end - t0) % 60
			printf "%d captions\n", captions
		}
	' "$log" >"$dir/timing.txt"
	: >"$dir/story.txt"
	local t0 t kind n chapter caption frame
	t0=$(head -n 1 "$log" | cut -f1)
	# Tabs are whitespace to read, which would merge an empty chapter field
	# away, so the fields are split on another character.
	tr '\t' '\037' <"$log" | while IFS=$'\037' read -r t kind n chapter caption; do
		[ "$kind" = caption ] || continue
		# The screen a second and a half after the caption changed.
		frame=$(awk -F'\t' -v t="$t" '$2 >= t + 1.5 { print $1; exit }' "$dir/frames/index")
		{
			printf '=== %s  %s%s\n' "$(awk -v t="$t" -v t0="$t0" 'BEGIN { t = int(t - t0 + 0.5); printf "%d:%02d", t / 60, t % 60 }')" "${chapter:+[$chapter] }" "$caption"
			if [ -n "$frame" ]; then sed $'s/\x1b\\[[0-9;:]*[A-Za-z]//g' "$dir/frames/$frame.txt"; fi
		} >>"$dir/story.txt"
	done
}

# scan fails when a frame shows the user's names or paths, or a sandbox
# path that was not shortened to ~.
scan() {
	local hits=0 word
	for word in "$real_user" "$real_host"; do
		[ -n "$word" ] || continue
		if grep -rlw -F -- "$word" "$dir/frames" >/dev/null; then
			echo "rehearse.sh: $name: a frame shows a real user or host name" >&2
			hits=1
		fi
	done
	for word in "$real_home" /Users/ "$root"; do
		if grep -rl -F -- "$word" "$dir/frames" >/dev/null; then
			echo "rehearse.sh: $name: a frame shows $( [ "$word" = "$root" ] && echo "a sandbox path" || echo "a path outside the sandbox")" >&2
			hits=1
		fi
	done
	return "$hits"
}

rehearse() {
	local tape cols rows
	tape=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
	name=$(basename "$tape" .tape)
	dir=$out/$name
	read -r cols rows <<<"$(size "$tape")"
	rm -rf "$dir"
	mkdir -p "$dir/frames"
	echo "rehearsing $name at ${cols}x$rows"

	root=$("$here/setup.sh" "$tape")
	trap 'rm -f "$dir/.recording"; "$here/setup.sh" down "$root" || true' EXIT
	cat >"$root/viewer.conf" <<-'EOF'
		set -g default-terminal xterm-256color
		set -g status off
		set -g prefix None
		set -g mouse off
		set -s escape-time 0
		set -g remain-on-exit on
	EOF
	# The viewer stands in for vhs's terminal, and starts vhs's shell in the
	# sandbox's environment, as render.sh starts vhs.
	env -i /bin/bash --noprofile --norc -c '
		source "$1/env.sh"
		export TERM=xterm-256color
		tmux -S "$1/run/viewer" -f "$1/viewer.conf" new-session -d -s v -x "$2" -y "$3" \
			"PS1=\"> \" /bin/bash --noprofile --norc --login +o history"
	' _ "$root" "$cols" "$rows"

	touch "$dir/.recording"
	record &
	play "$tape"
	rm -f "$dir/.recording"
	wait
	cp "$root/cues.log" "$dir/cues.log"
	report
	scan || status=1
	"$here/setup.sh" down "$root"
	trap - EXIT
	cat "$dir/timing.txt"
	echo "frames, story and chapters: ${dir#"$repo"/}/"
}

command -v tmux >/dev/null || die "tmux is not installed"
command -v perl >/dev/null || die "perl is not installed"
real_user=$(id -un)
real_host=$(hostname -s 2>/dev/null || true)
real_home=$HOME

status=0
if [ $# -eq 0 ]; then set -- "$here"/*.tape; fi
for t in "$@"; do rehearse "$t"; done
exit "$status"
