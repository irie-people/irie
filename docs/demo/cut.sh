#!/usr/bin/env bash
# cut.sh cuts the films record.sh made into what the site and the social
# posts show (docs/demo/README.md, "Cuts"). A reel's "# cut:" lines say how:
#
#   # cut: site FROM TO [POSTER]   the site's 16:10 cut: site/media/NAME.mp4
#                                  and NAME.webm, and NAME.jpg, its frame at
#                                  the time POSTER, else its first
#   # cut: phone FROM TO           the 9:16 cut, 1080×1920 with captions:
#                                  dist/demo/NAME.phone.mp4
#   # cut: shot FROM WIDTH X Y [X2 Y2 PAN]
#                                  from FROM until the next shot, the phone
#                                  cut shows a 9:16 crop of the film WIDTH
#                                  pixels wide, its top-left corner at X Y;
#                                  with X2 Y2 it eases there over the second
#                                  up to the time PAN
#   # cut: caption FROM TO top|bottom TEXT
#                                  a caption on the phone cut, its lines
#                                  balanced; " | " in TEXT breaks a line
#   # cut: sheet TIME TIME TIME TIME TIME
#                                  the five frames of the phone cut's contact
#                                  sheet, dist/demo/NAME.sheet.png; evenly
#                                  spread when the reel names none
#   # cut: readme FROM TO          this reel's part of docs/media/irie.gif,
#                                  which joins every reel's part in turn
#   # cut: jump FROM TO            leave film time FROM to TO out of every
#                                  cut, as a long wait while the window
#                                  connects; times inside it land at its end
#
# A time is seconds of the film, or an event of its NAME.events.json, as
# reached:needs_you, answer, close or opened, with #N for its Nth time and
# +S or -S after it, or end. So a film recorded again is cut at the same
# moments of what it shows.
#
#   docs/demo/cut.sh [REEL...]      default: every reel in docs/demo
#   docs/demo/cut.sh --gif          docs/media/irie.gif from every reel
#
# It needs ffmpeg and ffprobe, perl, and a Chrome or Chromium headless shell
# for the captions (IRIE_CUT_CHROME, else Playwright's or Chrome's own).
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
films=$repo/dist/demo
media=$repo/site/media

die() { echo "cut.sh: $*" >&2; exit 1; }

for t in ffmpeg ffprobe perl; do
	command -v "$t" >/dev/null || die "$t is not installed"
done

# chrome finds a headless Chrome to draw the captions with.
chrome() {
	local c
	for c in "${IRIE_CUT_CHROME:-}" "$HOME"/Library/Caches/ms-playwright/chromium_headless_shell-*/chrome-mac/headless_shell \
		"$HOME"/.cache/ms-playwright/chromium_headless_shell-*/chrome-linux/headless_shell \
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" "$(command -v chromium 2>/dev/null || true)"; do
		if [ -n "$c" ] && [ -x "$c" ]; then echo "$c"; return; fi
	done
	die "no headless Chrome for the captions: set IRIE_CUT_CHROME"
}

# lines prints a reel's "# cut: KIND" lines, without the kind.
lines() { sed -n "s/^# cut: $2 \{1,\}//p" "$1"; }

# at prints a time as seconds of the film: plain seconds, end, or an event
# of the film's events.json with :N and +S or -S. With a reel's jumps in
# CUT_JUMPS, as pairs of film times, it is the time in the film with the
# jumps left out: a time inside a jump is where the jump lands.
at() {
	local time=$1 events=$2 length=$3
	perl -MJSON::PP -e '
		my ($time, $file, $length) = @ARGV;
		my $t;
		if ($time =~ /^[0-9.]+$/) {
			$t = $time;
		} else {
			my ($name, $n, $sign, $s) = $time =~ /^([a-z_]+(?::[a-z_0-9]+)?)(?:#(\d+))?(?:([+-])([0-9.]+))?$/
				or die "cut.sh: no time $time\n";
			if ($name eq "end") {
				$t = $length;
			} else {
				open my $f, "<", $file or die "cut.sh: no $file\n";
				my $events = decode_json(do { local $/; <$f> });
				my ($do, $text) = split /:/, $name, 2;
				my @hits = grep { $_->{do} eq $do && (!defined $text || ($_->{text} // "") eq $text) } @$events;
				my $hit = $hits[($n // 1) - 1] or die "cut.sh: no event $name" . ($n ? " #$n" : "") . " in $file\n";
				$t = $hit->{ms} / 1000;
			}
			$t += ($sign eq "-" ? -$s : $s) if defined $sign;
		}
		$t = 0 if $t < 0;
		$t = $length if $t > $length;
		my @jumps = split " ", $ENV{CUT_JUMPS} // "";
		my $gone = 0;
		for (my $i = 0; $i < @jumps; $i += 2) {
			my ($a, $b) = @jumps[$i, $i + 1];
			if ($t >= $b) { $gone += $b - $a } elsif ($t > $a) { $t = $a }
		}
		printf "%.3f", $t - $gone;
	' -- "$time" "$events" "$length"
}

# prepare finds a reel's film and events, and when the reel has jumps, makes
# the film with them left out in TMP and points film at it: name, film,
# events and length are set, and CUT_JUMPS for at.
prepare() {
	local reel=$1 tmp=$2 a b keep="" join="" n=0 last=0
	name=$(basename "$reel" .reel)
	film=$films/$name.mp4 events=$films/$name.events.json
	[ -s "$film" ] || die "no $film: film it with docs/demo/record.sh $reel"
	length=$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$film")
	export CUT_JUMPS=
	local jumps=""
	while read -r a b; do
		[ -n "$a" ] || continue
		a=$(at "$a" "$events" "$length") b=$(at "$b" "$events" "$length")
		perl -e 'exit !($ARGV[0] < $ARGV[1] && $ARGV[2] <= $ARGV[0])' -- "$a" "$b" "$last" ||
			die "$name: a jump from $a to $b s runs backwards or over the one before"
		jumps+="$a $b " last=$b
	done < <(lines "$reel" jump)
	[ -n "$jumps" ] || return 0
	# The parts kept, between the jumps, joined.
	local -a cuts
	read -r -a cuts <<<"0 $jumps$length"
	keep="[0:v]split=$((${#cuts[@]} / 2))"
	for ((n = 0; n < ${#cuts[@]}; n += 2)); do keep+="[s$n]"; done
	for ((n = 0; n < ${#cuts[@]}; n += 2)); do
		keep+=";[s$n]trim=start=${cuts[$n]}:end=${cuts[$((n + 1))]},setpts=PTS-STARTPTS[k$n]"
		join+="[k$n]"
	done
	ffmpeg -v error -y -i "$film" -filter_complex "$keep;${join}concat=n=$((${#cuts[@]} / 2)):v=1:a=0[out]" \
		-map "[out]" -an -c:v libx264 -preset veryfast -crf 12 "$tmp/$name.edited.mp4"
	film=$tmp/$name.edited.mp4
	export CUT_JUMPS=$jumps
}

# pill draws one caption, white on Irie's deep green, onto a clear
# 1080×1920 picture, at the top or the bottom.
pill() {
	local text=$1 where=$2 out=$3 page
	page=$(mktemp "${TMPDIR:-/tmp}/cut-caption.XXXXXX")
	mv "$page" "$page.html"
	page=$page.html
	TEXT=$text WHERE=$where perl -e '
		my ($t, $w) = @ENV{qw(TEXT WHERE)};
		$t =~ s/&/&amp;/g; $t =~ s/</&lt;/g; $t =~ s/>/&gt;/g;
		$t =~ s{ \| }{<br>}g;
		my $place = $w eq "top" ? "top: 150px" : "bottom: 230px";
		print <<"EOF";
<!doctype html><meta charset="utf-8">
<style>
html, body { margin: 0; width: 1080px; height: 1920px; background: transparent; overflow: hidden; }
p { position: absolute; left: 50%; $place; transform: translateX(-50%); box-sizing: border-box;
    width: max-content; max-width: 900px; margin: 0; padding: 26px 44px;
    border-radius: 30px; background: rgba(30, 90, 66, .93); color: #fff; text-align: center; text-wrap: balance;
    font: 700 58px/1.18 -apple-system, BlinkMacSystemFont, "Helvetica Neue", Arial, sans-serif; }
</style>
<p>$t</p>
EOF
	' >"$page"
	"$(chrome)" --headless --disable-gpu --hide-scrollbars --force-device-scale-factor=1 \
		--window-size=1080,1920 --default-background-color=00000000 \
		--screenshot="$out" "file://$page" >/dev/null 2>&1
	rm -f "$page"
	[ -s "$out" ] || die "the caption \"$text\" did not draw"
}

# pan prints an ffmpeg expression in t, seconds of a shot, that holds A
# until a second before T and then eases to B, which it holds from T.
pan() {
	perl -e '
		my ($a, $b, $t) = @ARGV;
		if (!defined $b || $a == $b) { print $a; exit }
		my $s = $t - 1;
		print "if(lt(t,$s),$a,if(lt(t,$t),$a+($b-$a)*(3*pow((t-$s),2)-2*pow((t-$s),3)),$b))";
	' -- "$@"
}

# since prints time A less time B, in seconds.
since() { perl -e 'printf "%.3f", $ARGV[0] - $ARGV[1]' -- "$1" "$2"; }

clean() { echo -map_metadata -1 -map_chapters -1 -fflags +bitexact -flags:v +bitexact -flags:a +bitexact; }

cut_reel() {
	local reel=$1 name film events length from to line a b where text tmp
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/cut.XXXXXX")
	prepare "$reel" "$tmp"

	if line=$(lines "$reel" site) && [ -n "$line" ]; then
		local poster
		read -r from to poster <<<"$line"
		from=$(at "$from" "$events" "$length") to=$(at "$to" "$events" "$length")
		poster=$(since "$(at "${poster:-$from}" "$events" "$length")" "$from")
		mkdir -p "$media"
		# 1344×840 is twice the box at its widest, 672 pixels.
		ffmpeg -v error -y -ss "$from" -to "$to" -i "$film" -an -vf "fps=30,scale=1344:840:flags=lanczos,format=yuv420p" \
			-c:v libx264 -preset slow -crf 24 -profile:v high -movflags +faststart $(clean) "$media/$name.mp4"
		ffmpeg -v error -y -ss "$from" -to "$to" -i "$film" -an -vf "fps=30,scale=1344:840:flags=lanczos,format=yuv420p" \
			-c:v libvpx-vp9 -crf 36 -b:v 0 -row-mt 1 -deadline good -cpu-used 2 $(clean) "$media/$name.webm"
		ffmpeg -v error -y -ss "$poster" -i "$media/$name.mp4" -frames:v 1 -q:v 3 $(clean) "$media/$name.jpg"
		printf '%s: %s to %s s\n' "${media#"$repo"/}/$name.{mp4,webm,jpg}" "$from" "$to"
	fi

	if line=$(lines "$reel" phone) && [ -n "$line" ]; then
		read -r from to <<<"$line"
		from=$(at "$from" "$events" "$length") to=$(at "$to" "$events" "$length")
		local graph last n=0 i shots=() inputs=() join=""
		# The shots, each from its time to the next one's, in the cut's time.
		local s w x y x2 y2 p
		while read -r s w x y x2 y2 p; do
			[ -n "$s" ] || continue
			shots+=("$(since "$(at "$s" "$events" "$length")" "$from") $w $x $y ${x2:-$x} ${y2:-$y} ${p:-0}")
		done < <(lines "$reel" shot)
		[ ${#shots[@]} -gt 0 ] || shots=("0 540 0 0 0 0 0")
		graph="[0:v]fps=30,split=${#shots[@]}"
		for ((i = 0; i < ${#shots[@]}; i++)); do graph+="[in$i]"; done
		for ((i = 0; i < ${#shots[@]}; i++)); do
			read -r s w x y x2 y2 p <<<"${shots[$i]}"
			local end h
			end=$(since "$to" "$from")
			if [ $((i + 1)) -lt ${#shots[@]} ]; then end=${shots[$((i + 1))]%% *}; fi
			h=$((w * 16 / 9))
			[ "$p" = 0 ] || p=$(since "$(at "$p" "$events" "$length")" "$from")
			p=$(since "$p" "$s")
			# A crop whose sides are even, so scaling it keeps 9:16 exactly.
			graph+=";[in$i]trim=start=$s:end=$end,setpts=PTS-STARTPTS,crop=$w:$((h / 2 * 2)):'$(pan "$x" "$x2" "$p")':'$(pan "$y" "$y2" "$p")'"
			graph+=",scale=1080:1920:flags=lanczos,setsar=1[shot$i]"
			join+="[shot$i]"
		done
		graph+=";${join}concat=n=${#shots[@]}:v=1:a=0[base]"
		last=base n=1
		while read -r a b where text; do
			[ -n "$a" ] || continue
			a=$(since "$(at "$a" "$events" "$length")" "$from")
			b=$(since "$(at "$b" "$events" "$length")" "$from")
			perl -e 'exit !($ARGV[0] < $ARGV[1])' -- "$a" "$b" || die "the caption \"$text\" ends before it starts"
			pill "$text" "$where" "$tmp/caption$n.png"
			inputs+=(-loop 1 -i "$tmp/caption$n.png")
			graph+=";[$n:v]format=rgba,fade=in:st=$a:d=0.25:alpha=1,fade=out:st=$(since "$b" 0.25):d=0.25:alpha=1[c$n]"
			graph+=";[$last][c$n]overlay=0:0:shortest=1:enable='between(t,$a,$b)'[v$n]"
			last=v$n n=$((n + 1))
		done < <(lines "$reel" caption)
		graph+=";[$last]format=yuv420p[out]"
		# Instagram and Threads want a sound track: a silent one.
		ffmpeg -v error -y -ss "$from" -to "$to" -i "$film" ${inputs[@]+"${inputs[@]}"} \
			-f lavfi -i "anullsrc=r=48000:cl=stereo" -filter_complex "$graph" -map "[out]" -map "$n:a" \
			-c:v libx264 -preset slow -crf 20 -profile:v high -r 30 -c:a aac -b:a 64k -shortest \
			-movflags +faststart $(clean) "$films/$name.phone.mp4"
		printf '%s: %s to %s s\n' "${films#"$repo"/}/$name.phone.mp4" "$from" "$to"

		# The contact sheet: five frames side by side, 360×640 each.
		local times=() select="" k
		if line=$(lines "$reel" sheet) && [ -n "$line" ]; then
			for t in $line; do times+=("$(since "$(at "$t" "$events" "$length")" "$from")"); done
		else
			for k in 1 3 5 7 9; do times+=("$(perl -e 'printf "%.3f", $ARGV[0] * $ARGV[1] / 10' -- "$(since "$to" "$from")" "$k")"); done
		fi
		for t in "${times[@]}"; do
			t=$(perl -e 'printf "%.3f", $ARGV[0] < 0.05 ? 0.05 : $ARGV[0]' -- "$t")
			select+="${select:++}lt(prev_t,$t)*gte(t,$t)"
		done
		ffmpeg -v error -y -i "$films/$name.phone.mp4" -vf "select='$select',scale=360:640:flags=lanczos,pad=372:652:6:6:color=white,tile=5x1" \
			-fps_mode vfr -frames:v 1 $(clean) "$films/$name.sheet.png"
		printf '%s: frames at %s s\n' "${films#"$repo"/}/$name.sheet.png" "${times[*]}"
	fi
	rm -rf "$tmp"
}

# gif joins every reel's readme part into docs/media/irie.gif, 800×500 at
# 12 frames a second, its colours chosen for the whole clip.
gif() {
	local reel name film events length line from to parts=() n=0 tmp
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/cut.XXXXXX")
	for reel in "$here"/first-look.reel "$here"/needs-you.reel "$here"/close-the-window.reel "$here"/local.reel; do
		line=$(lines "$reel" readme) && [ -n "$line" ] || continue
		prepare "$reel" "$tmp"
		read -r from to <<<"$line"
		from=$(at "$from" "$events" "$length") to=$(at "$to" "$events" "$length")
		ffmpeg -v error -y -ss "$from" -to "$to" -i "$film" -an -vf "fps=12,scale=800:500:flags=lanczos" \
			-c:v ffv1 "$tmp/$n.mkv"
		parts+=(-i "$tmp/$n.mkv") n=$((n + 1))
	done
	[ "$n" -gt 0 ] || die "no reel has a readme cut"
	mkdir -p "$repo/docs/media"
	local join="" i
	for ((i = 0; i < n; i++)); do join+="[$i:v]"; done
	# In two passes, so no pass holds the whole clip: the colours, then
	# the frames in them.
	ffmpeg -v error -y "${parts[@]}" -filter_complex "${join}concat=n=$n:v=1:a=0" -c:v ffv1 "$tmp/all.mkv"
	ffmpeg -v error -y -i "$tmp/all.mkv" -vf "palettegen=max_colors=128:stats_mode=diff" "$tmp/palette.png"
	ffmpeg -v error -y -i "$tmp/all.mkv" -i "$tmp/palette.png" -lavfi \
		"paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle" -loop 0 $(clean) "$repo/docs/media/irie.gif"
	rm -rf "$tmp"
	printf 'docs/media/irie.gif: %s, %s\n' \
		"$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$repo/docs/media/irie.gif" | awk '{ printf "%.1f s", $1 }')" \
		"$(du -h "$repo/docs/media/irie.gif" | cut -f1 | tr -d ' ')"
}

if [ "${1:-}" = --gif ]; then
	gif
	exit
fi
if [ $# -eq 0 ]; then set -- "$here"/*.reel; fi
for r in "$@"; do cut_reel "$(cd "$(dirname "$r")" && pwd)/$(basename "$r")"; done
