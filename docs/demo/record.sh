#!/usr/bin/env bash
# record.sh films Irie's Mac app from reels into dist/demo/
# (docs/demo/README.md). Each reel gets a fresh sandbox (setup.sh, which
# reads its "# irie-demo:" lines as a tape's) with a core of its own and
# the sessions its Spawn lines start, and a copy of
# Irie.app with a bundle id of its own, so neither the user's Irie nor the
# app's defaults, which hold their unsent task, are touched. The copy films
# its own window, which stands off any screen: nothing takes the focus, and
# macOS asks for no Screen Recording permission. Each video then loses its
# metadata, and its file and its frames, read with Apple's Vision
# framework, are scanned for anything from outside the sandbox.
#
#   docs/demo/record.sh [REEL...]   default: every reel in docs/demo
#
# It needs the app built (mac/build.sh, or IRIE_RECORD_APP=path/to/Irie.app),
# ffmpeg and ffprobe, swiftc, and what setup.sh needs: tmux, go and git.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
out=$repo/dist/demo
app=${IRIE_RECORD_APP:-$repo/mac/build/Irie.app}

die() { echo "record.sh: $*" >&2; exit 1; }

[ -x "$app/Contents/MacOS/Irie" ] || die "no app at $app: build it with mac/build.sh, or set IRIE_RECORD_APP"
for t in ffmpeg ffprobe swiftc strings plutil codesign defaults perl; do
	command -v "$t" >/dev/null || die "$t is not installed"
done

# The copy's bundle id: the app's own and .recording, which --record wants.
bundle=$(plutil -extract CFBundleIdentifier raw "$app/Contents/Info.plist").recording
real_user=$(id -un)
real_name=$(id -F 2>/dev/null || true)
real_host=$(hostname -s 2>/dev/null || true)
real_home=$HOME
root=

# in_sandbox runs a command in the sandbox's environment and nothing of the
# user's: an IRIE_BIN, say, would point irie at the user's own Irie.
in_sandbox() {
	env -i /bin/bash --noprofile --norc -c 'source "$0/env.sh" && exec "$@"' "$root" "$@"
}

cleanup() {
	if [ -n "$root" ]; then "$here/setup.sh" down "$root" || true; fi
	root=
	defaults delete "$bundle" >/dev/null 2>&1 || true
}

# names and paths list what no video may show: the user's names, matched as
# whole words, and paths outside the sandbox's ~, matched anywhere.
names() { printf '%s\n' "$real_user" "$real_name" "$real_host" | awk 'length($0) >= 3'; }
paths() { printf '%s\n' "$real_home" /Users/ /private/tmp/ /tmp/irie-demo irie-demo. "$root"; }

# found prints whether TEXT holds a name or a path, and which.
found() {
	local word
	while IFS= read -r word; do
		if grep -qiwF -- "$word" <<<"$1"; then echo "$word"; fi
	done < <(names)
	while IFS= read -r word; do
		if grep -qiF -- "$word" <<<"$1"; then echo "$word"; fi
	done < <(paths)
}

# scan fails when the video's metadata or bytes name the user or a path
# outside the sandbox, or when its frames, read once a second, show one.
scan() {
	local f=$1 frames=$2 word hits=0
	while IFS= read -r word; do
		echo "record.sh: $(basename "$f")'s metadata names $word" >&2
		hits=1
	done < <(found "$(ffprobe -v error -show_entries format_tags:stream_tags:chapter_tags -of default=noprint_wrappers=1 "$f")")
	# Short words can turn up by chance in compressed video, so only paths
	# are looked for in the whole file. grep reads to the end: with -q it
	# would stop early, and pipefail would take strings' SIGPIPE for a miss.
	for word in "$real_home" /Users/ "$root"; do
		if strings -n 6 "$f" | grep -F -- "$word" >/dev/null; then
			echo "record.sh: $(basename "$f") contains $word" >&2
			hits=1
		fi
	done
	mkdir -p "$frames"
	ffmpeg -v error -i "$f" -vf fps=1 "$frames/%03d.png"
	if ! "$root/ocr" "$frames"/*.png >"$frames/text.tsv"; then
		echo "record.sh: could not read $(basename "$f")'s frames" >&2
		return 1
	fi
	# Every line read against every name, as a whole word, and every path,
	# anywhere, in one pass, as found does. A shell loop a line, with its
	# process substitutions, made bash 3.2, macOS's own, abort on a corrupted
	# heap partway through a film's thousand lines, which left the film
	# unscanned and its sandbox running.
	names >"$frames/names.txt"
	paths >"$frames/paths.txt"
	local frame text
	while IFS=$'\t' read -r frame word text; do
		echo "record.sh: $(basename "$f") shows $word in frame $(basename "$frame" .png) of one a second: $text" >&2
		hits=1
	done < <(perl -e '
		sub list { open my $h, "<", $_[0] or die "record.sh: $_[0]: $!\n"; grep { length } map { chomp; $_ } <$h> }
		my @names = list($ARGV[0]);
		my @paths = list($ARGV[1]);
		open my $t, "<", $ARGV[2] or die "record.sh: $ARGV[2]: $!\n";
		while (<$t>) {
			chomp;
			my ($frame, $text) = split /\t/, $_, 2;
			next unless defined $text;
			for my $w (@names) { print "$frame\t$w\t$text\n" if $text =~ /(?<!\w)\Q$w\E(?!\w)/i }
			for my $w (@paths) { print "$frame\t$w\t$text\n" if index(lc $text, lc $w) >= 0 }
		}
	' -- "$frames/names.txt" "$frames/paths.txt" "$frames/text.tsv")
	return "$hits"
}

record() {
	local reel name raw line length
	local -a spawn
	reel=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
	name=$(basename "$reel" .reel)
	echo "recording $name"
	root=$("$here/setup.sh" "$reel")
	trap cleanup EXIT
	swiftc -O -o "$root/ocr" "$here/ocr.swift" 2>/dev/null || die "ocr.swift did not build"
	in_sandbox irie up >/dev/null
	# The reel's Spawn lines start its sessions, in order, before filming.
	while IFS= read -r line; do
		read -r -a spawn <<<"$line"
		in_sandbox irie ctl spawn --wait "${spawn[@]}" >/dev/null
	done < <(sed -n 's/^Spawn[[:space:]]\{1,\}//p' "$reel")

	cp -R "$app" "$root/Irie.app"
	plutil -replace CFBundleIdentifier -string "$bundle" "$root/Irie.app/Contents/Info.plist"
	codesign --force --sign - "$root/Irie.app" 2>/dev/null
	defaults delete "$bundle" >/dev/null 2>&1 || true
	raw=$root/$name.mp4
	# A wall-clock limit, so a reel that waits forever cannot stall the shell.
	in_sandbox perl -e 'alarm 600; exec @ARGV' "$root/Irie.app/Contents/MacOS/Irie" \
		--record "$raw" --reel "$reel" --socket "$root/run/irie/core.sock"
	[ -s "$raw" ] || die "Irie --record wrote no $name.mp4"

	# The muxer's neutral tags only, as render.sh keeps.
	ffmpeg -v error -y -i "$raw" -map 0 -map_metadata -1 -map_metadata:s:v -1 -c copy \
		-fflags +bitexact -movflags +faststart "$out/$name.mp4"
	cp "$root/$name.events.json" "$out/$name.events.json"
	scan "$out/$name.mp4" "$root/frames" || status=1

	cleanup
	trap - EXIT
	length=$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$out/$name.mp4")
	printf '%s: %s, %s, %s\n' "${out#"$repo"/}/$name.mp4" \
		"$(ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of csv=s=x:p=0 "$out/$name.mp4")" \
		"$(awk -v s="$length" 'BEGIN { printf "%d:%02d", s / 60, s % 60 }')" \
		"$(du -h "$out/$name.mp4" | cut -f1 | tr -d ' ')"
}

mkdir -p "$out"
status=0
if [ $# -eq 0 ]; then set -- "$here"/*.reel; fi
for r in "$@"; do record "$r"; done
exit "$status"
