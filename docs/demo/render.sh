#!/usr/bin/env bash
# render.sh renders Irie's demo recordings with vhs into dist/demo/
# (docs/demo/README.md). Each tape gets a fresh sandbox, and vhs, ttyd, the
# browser and the recorded shell run in the sandbox's environment only.
# Each video then loses its metadata, gains the chapters its cues logged,
# and is scanned for anything from outside the sandbox.
#
#   docs/demo/render.sh [TAPE...]   default: every tape in docs/demo
#
# It needs vhs, ttyd, ffmpeg and ffprobe, a Chrome or Chromium for vhs,
# and tmux, go, git and perl. Without vhs, ttyd or ffmpeg it says what is
# missing and renders nothing: docs/demo/rehearse.sh needs only tmux.
# Nothing is uploaded: vhs publishes only when asked to, and is not.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
out=$repo/dist/demo

die() { echo "render.sh: $*" >&2; exit 1; }

# chapters turns the cue log into chapters for ffmpeg (FFMETADATA) and a
# list to read (M:SS title). vhs drops frames it cannot capture in time, so
# its video can run shorter than the clock did: times are scaled to the
# video's length, from the first cue to the end cue.
chapters() {
	local log=$1 length=$2
	awk -F'\t' -v dur="$length" -v meta="$3" -v list="$4" '
		function stamp(t) { t = int(t + 0.5); return sprintf("%d:%02d", t / 60, t % 60) }
		function escape(s) { gsub(/[=;#\\]/, "\\\\&", s); return s }
		NR == 1 { t0 = $1 }
		$2 == "end" { end = $1 }
		$2 == "caption" && $4 != "" { n++; at[n] = $1; title[n] = $4 }
		END {
			if (!end || !n) exit 1
			scale = dur / (end - t0)
			print ";FFMETADATA1" >meta
			for (i = 1; i <= n; i++) {
				from = (at[i] - t0) * scale
				to = i < n ? (at[i + 1] - t0) * scale : dur
				printf "[CHAPTER]\nTIMEBASE=1/1000\nSTART=%d\nEND=%d\ntitle=%s\n", from * 1000, to * 1000, escape(title[i]) >meta
				printf "%s %s\n", stamp(from), title[i] >list
			}
		}
	' "$log" || die "$log has no chapters or no end cue"
}

# scan fails when a video carries the user's names or paths, or a sandbox
# path, in its metadata or anywhere as text.
scan() {
	local f=$1 tags word hits=0
	tags=$(ffprobe -v error -show_entries format_tags:stream_tags:chapter_tags -of default=noprint_wrappers=1 "$f")
	for word in "$real_user" "$real_host" "$real_home" /Users/ "$root"; do
		[ -n "$word" ] || continue
		if grep -qwF -- "$word" <<<"$tags"; then
			echo "render.sh: $(basename "$f")'s metadata names $word" >&2
			hits=1
		fi
	done
	# Short words can turn up by chance in compressed video, so only paths
	# are looked for in the whole file. grep reads to the end: with -q it
	# would stop early, and pipefail would take strings' SIGPIPE for a miss.
	for word in "$real_home" /Users/ "$root"; do
		if strings -n 6 "$f" | grep -F -- "$word" >/dev/null; then
			echo "render.sh: $(basename "$f") contains $word" >&2
			hits=1
		fi
	done
	return "$hits"
}

render() {
	local tape name raw length
	tape=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
	name=$(basename "$tape" .tape)
	echo "rendering $name"
	root=$("$here/setup.sh" "$tape")
	trap '"$here/setup.sh" down "$root" || true' EXIT

	# The tape's Output is relative, so the raw video lands in the sandbox.
	(cd "$root" && env -i /bin/bash --noprofile --norc -c 'source ./env.sh && exec vhs "$1"' _ "$tape")
	raw=$root/$name.mp4
	[ -s "$raw" ] || die "vhs wrote no $name.mp4"

	length=$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$raw")
	chapters "$root/cues.log" "$length" "$root/chapters.ffmeta" "$out/$name.chapters.txt"
	# Global tags come from the chapters file, which has none, and the
	# video stream keeps none of its own; bitexact keeps ffmpeg from writing
	# its encoder tag. What is left is the muxer's neutral defaults.
	ffmpeg -v error -y -i "$raw" -f ffmetadata -i "$root/chapters.ffmeta" \
		-map 0 -map_metadata 1 -map_metadata:s:v -1 -map_chapters 1 -c copy \
		-fflags +bitexact -movflags +faststart "$out/$name.mp4"
	scan "$out/$name.mp4" || status=1

	"$here/setup.sh" down "$root"
	trap - EXIT
	printf '%s: %s, %s, %s\n' "${out#"$repo"/}/$name.mp4" \
		"$(ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of csv=s=x:p=0 "$out/$name.mp4")" \
		"$(awk -v s="$length" 'BEGIN { printf "%d:%02d", s / 60, s % 60 }')" \
		"$(du -h "$out/$name.mp4" | cut -f1 | tr -d ' ')"
}

missing=()
for t in vhs ttyd ffmpeg ffprobe; do
	command -v "$t" >/dev/null || missing+=("$t")
done
if [ ${#missing[@]} -gt 0 ]; then
	echo "render.sh: ${missing[*]} missing, so nothing was rendered." >&2
	echo "render.sh: install them (vhs's README lists how), or play the tapes with docs/demo/rehearse.sh." >&2
	exit 3
fi
for t in tmux go git perl strings; do
	command -v "$t" >/dev/null || die "$t is not installed"
done

real_user=$(id -un)
real_host=$(hostname -s 2>/dev/null || true)
real_home=$HOME

mkdir -p "$out"
status=0
if [ $# -eq 0 ]; then set -- "$here"/*.tape; fi
for t in "$@"; do render "$t"; done
exit "$status"
