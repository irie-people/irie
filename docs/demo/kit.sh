#!/usr/bin/env bash
# kit.sh makes a release's launch kit, what to show and what to say when the
# release goes out (docs/proposals/launch-kits.md), as each of Higgsfield's
# feature launches came with a campaign. It reads the release's features
# from CHANGELOG.md, what their commits say, and the reels, and writes
# dist/kits/VERSION/:
#
#   brief.md        each feature, what its commit says, and the reel that
#                   films it or that it needs; the clips ready to post; the
#                   rules every post keeps
#   post.md         drafts for Threads and Instagram; kept once it is there,
#                   so your edits stay
#   NAME.phone.mp4  each phone cut that record.sh and cut.sh have made
#
# "check" goes over a kit before anything is posted, as Higgsfield's review
# goes over a submission before it pays. A draft must keep to its
# platform's limits and link only to the project's own pages. Nothing may
# name a person or a machine, and each clip must be 1080×1920 with nothing
# in its metadata either. kit.sh never posts anything.
#
#   docs/demo/kit.sh [VERSION]      default: the newest release
#   docs/demo/kit.sh check [DIR]    default: the newest release's kit
#
# It needs git, awk and perl, and ffprobe to check a kit's clips.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
changelog=$repo/CHANGELOG.md
films=$repo/dist/demo
kits=$repo/dist/kits

# The product's names come from internal/brand, so a copy under its own
# name (DESIGN §2.10) gets kits under that name.
eval "$("$repo/tools/brand.sh")"
product=$BRAND_NAME
tagline="$product runs your AI coding agents side by side, in one calm view. Free and open source."
hashtags="#opensource #codingagents #aiagents #devtools #macos"

# What each platform takes, from its own pages (PROVENANCE.md). Threads: 500
# characters, an emoji counting as its UTF-8 bytes, so every character past
# ASCII is counted that way here to be safe; five links; one topic tag.
# Instagram: a 2,200-character caption with five hashtags (since 2025-12-18),
# and a reel of 3 seconds or more.
threads_bytes=500 threads_links=5 threads_topics=1
instagram_chars=2200 instagram_tags=5 reel_min=3

# Fields go out split by the unit separator, which read keeps empty, as it
# would not a tab.
sep=$'\037'

die() { echo "kit.sh: $*" >&2; exit 1; }

[ -f "$changelog" ] || die "no CHANGELOG.md in $repo"
for t in git awk perl; do
	command -v "$t" >/dev/null || die "$t is not installed"
done

# releases prints CHANGELOG.md's releases, newest first, one a line: the
# version, the date and the compare link, which the first release lacks.
releases() {
	awk -v s="$sep" '/^## / {
		v = ""; url = ""; date = ""
		if (match($0, /^## \[[^]]+\]/)) v = substr($0, 5, RLENGTH - 5)
		else if (match($0, /^## [0-9][^ ]*/)) v = substr($0, 4, RLENGTH - 3)
		if (match($0, /\]\(https:[^)]+\)/)) url = substr($0, RSTART + 2, RLENGTH - 3)
		if (match($0, /\([0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]\)/)) date = substr($0, RSTART + 1, 10)
		if (v != "") print v s date s url
	}' "$changelog"
}

# newest prints the newest release's version.
newest() { releases | awk -F "$sep" 'NR == 1 { print $1 }'; }

# repo_url prints the repository's address on GitHub: the brand's, else
# the one CHANGELOG.md's compare links go to.
repo_url() {
	if [ -n "${BRAND_REPO:-}" ]; then echo "${BRAND_REPO%/}"; return; fi
	releases | awk -F "$sep" '$3 != "" { sub(/\/compare\/.*/, "", $3); print $3; exit }'
}

# features prints release $1's features in the changelog's order, one a
# line: the scope, the words, the commit and its link.
features() {
	awk -v want="$1" -v s="$sep" '
	/^## / { rel = (index($0, "## [" want "]") == 1 || index($0, "## " want " ") == 1); feat = 0; next }
	rel && /^### / { feat = ($0 ~ /^### Features/); next }
	rel && feat && /^\* / {
		line = substr($0, 3); scope = ""; hash = ""; url = ""
		if (match(line, /^\*\*[^*]+:\*\* /)) { scope = substr(line, 3, RLENGTH - 6); line = substr(line, RLENGTH + 1) }
		if (match(line, / \(\[[0-9a-f]+\]\([^)]+\)\)$/)) {
			link = substr(line, RSTART + 2, RLENGTH - 3); line = substr(line, 1, RSTART - 1)
			if (match(link, /^\[[0-9a-f]+\]/)) hash = substr(link, 2, RLENGTH - 2)
			if (match(link, /\(.*\)$/)) url = substr(link, RSTART + 1, RLENGTH - 2)
		}
		print scope s line s hash s url
	}' "$changelog"
}

# plural prints count $1 with word $2, an s on it unless the count is 1.
plural() { if [ "$1" = 1 ]; then echo "1 $2"; else echo "$1 $2s"; fi; }

# cap prints $1 with its first letter in capitals.
cap() { awk '{ print toupper(substr($0, 1, 1)) substr($0, 2) }' <<<"$1"; }

# about prints the first paragraph of commit $1's message body on one line,
# or nothing when the commit is not in this clone.
about() {
	[ -n "$1" ] || return 0
	{ git -C "$repo" log -1 --format=%b "$1" 2>/dev/null || true; } |
		awk 'NF == 0 { if (n) exit; next } !n && /^[A-Za-z-]+: / { next } { printf "%s%s", (n++ ? " " : ""), $0 } END { if (n) print "" }'
}

# films_of prints the reels that film commit $1, as their "# feature: HASH"
# lines say, a hash of seven characters or more matching either way.
films_of() {
	local reel h
	[ -n "$1" ] || return 0
	for reel in "$here"/*.reel; do
		[ -e "$reel" ] || continue
		while IFS= read -r h; do
			[ ${#h} -ge 7 ] || continue
			case $1 in "$h"*) basename "$reel" .reel; continue ;; esac
			case $h in "$1"*) basename "$reel" .reel ;; esac
		done < <(awk '$1 == "#" && $2 == "feature:" { for (i = 3; i <= NF; i++) print $i }' "$reel")
	done | sort -u
}

# captions prints reel $1's phone captions in quotes, one after another.
captions() {
	awk '$1 == "#" && $2 == "cut:" && $3 == "caption" {
		text = ""
		for (i = 7; i <= NF; i++) text = text (text == "" ? "" : " ") $i
		gsub(/ \| /, " ", text)
		if (!(text in seen)) printf "%s\"%s\"", (n++ ? " " : ""), text
		seen[text] = 1
	} END { if (n) print "" }' "$1"
}

# phone_reels prints the reels that have a phone cut.
phone_reels() {
	local reel
	for reel in "$here"/*.reel; do
		if [ -e "$reel" ] && grep -q '^# cut: phone ' "$reel"; then basename "$reel" .reel; fi
	done
}

# make_kit writes release $1's kit, from its date $2 and compare link $3.
make_kit() {
	local version=$1 date=$2 url=$3 dir=$kits/$1 base tag notes total more lines
	local n=0 filmed=0 scope text hash link where para reels name caps clips=""
	base=$(repo_url)
	[ -n "$base" ] || die "neither the brand nor CHANGELOG.md names the repository"
	tag=v$version
	[ -z "$url" ] || tag=${url##*...}
	notes=$base/releases/tag/$tag
	total=$(features "$version" | grep -c . || true)
	[ "$total" -gt 0 ] || die "release $version adds no features, so it needs no kit"
	mkdir -p "$dir"

	for name in $(phone_reels); do
		if [ -f "$films/$name.phone.mp4" ]; then
			cp "$films/$name.phone.mp4" "$dir/"
			clips="$clips $name.phone.mp4"
		fi
	done

	{
		echo "# $product $version: launch kit"
		echo
		if [ -n "$date" ]; then echo "Released $date. Release notes: $notes"; else echo "Release notes: $notes"; fi
		echo
		echo "Made by \`docs/demo/kit.sh\` from CHANGELOG.md, the commits and the reels. Nothing in it has been posted. Film what it asks for, edit \`post.md\`, run \`docs/demo/kit.sh check\`, then post by hand."
		echo
		echo "## What to show"
		echo
		echo "Each feature, with what its commit says. A clip of one shows it in the sandbox in under 30 seconds, with captions that say only what the picture shows. A reel that films a feature names its commit on a \`# feature: HASH\` line (docs/demo/README.md, \"Launch kits\"), and the brief then points to it."
		while IFS="$sep" read -r scope text hash link; do
			n=$((n + 1))
			echo
			echo "### $n. $(cap "$text")"
			echo
			where=
			[ -z "$scope" ] || where='`'$scope'` · '
			if [ -n "$hash" ]; then echo "$where[$hash]($link)"; else echo "${where}no commit named"; fi
			para=$(about "$hash")
			if [ -n "$para" ]; then
				echo
				echo "> $para"
			fi
			echo
			reels=$(films_of "$hash")
			if [ -z "$reels" ]; then
				echo "**Reel:** none yet."
				continue
			fi
			filmed=$((filmed + 1))
			for name in $reels; do
				if [ -f "$dir/$name.phone.mp4" ]; then
					echo "**Reel:** \`$name.reel\`. Its phone cut, \`$name.phone.mp4\`, is in this kit."
				else
					echo "**Reel:** \`$name.reel\`, not cut yet: \`docs/demo/record.sh docs/demo/$name.reel\`, then \`docs/demo/cut.sh docs/demo/$name.reel\`."
				fi
			done
		done < <(features "$version")

		echo
		echo "## Clips any post can use"
		echo
		if [ -z "$(phone_reels)" ]; then echo "None: no reel has a phone cut yet."; fi
		for name in $(phone_reels); do
			caps=$(captions "$here/$name.reel")
			if [ -f "$dir/$name.phone.mp4" ]; then
				echo "- \`$name.phone.mp4\`, in this kit: ${caps:-no captions}"
			else
				echo "- \`$name\`, not cut yet: ${caps:-no captions}"
			fi
		done
		echo
		echo "A clip not cut yet comes from \`docs/demo/record.sh\`, then \`docs/demo/cut.sh\`, and the next run of \`kit.sh\` puts it here."
		echo
		echo "## Rules every post keeps"
		echo
		echo "1. Film only the sandbox, with \`record.sh\`, never a real desktop, so nothing names a person or a machine."
		echo "2. Show what a user gets: real captures of $product, never a mock-up, a polished fake or stock footage."
		echo "3. Say only what the picture shows, with no claims of speed, money or results that the clip doesn't prove."
		echo "4. Nothing goes out until the maintainer has read it, and they post it by hand."
		echo "5. Never buy views, likes or follows, and never pay for reach."
		echo "6. Anyone who earns Steak and posts about $product says so, such as \"I contribute to $product\"."
		echo "7. Run \`docs/demo/kit.sh check\` first."
	} >"$dir/brief.md"

	if [ -e "$dir/post.md" ]; then
		echo "kit.sh: kept ${dir#"$repo"/}/post.md as it was"
	else
		lines=$(features "$version" | awk -F "$sep" 'NR <= 3 { print "- " toupper(substr($2, 1, 1)) substr($2, 2) }')
		more=$((total - 3))
		{
			echo "# $product $version: drafts"
			echo
			echo "Drafts from the release's first three features, for you to edit: \`kit.sh\` keeps this file once it is here. Run \`docs/demo/kit.sh check\`, then post by hand."
			echo
			echo "## Threads"
			echo
			echo '```text'
			echo "$product $version is out."
			echo
			echo "$lines"
			echo
			if [ "$more" -gt 0 ]; then echo "And $more more: $notes"; else echo "Release notes: $notes"; fi
			echo '```'
			echo
			echo "## Instagram"
			echo
			echo '```text'
			echo "$product $version is out."
			echo
			echo "$lines"
			echo
			if [ "$more" -gt 0 ]; then echo "And $more more in the release notes."; echo; fi
			echo "$tagline"
			echo
			echo "$hashtags"
			echo '```'
			echo
			if [ -n "$clips" ]; then echo "Clips in this kit to attach:$clips"; else echo "No clip in this kit yet: see brief.md."; fi
		} >"$dir/post.md"
	fi

	echo "kit.sh: wrote ${dir#"$repo"/}: $(plural "$total" feature), $filmed filmed by a reel, $(plural "$(echo $clips | wc -w | tr -d ' ')" clip)"
	echo "kit.sh: next, film what brief.md asks for, edit post.md, then run docs/demo/kit.sh check"
}

# The checks look for what no post may show, as record.sh does in a film:
# the user's names, matched as whole words, and paths outside the
# sandbox's home, matched anywhere.
real_user=$(id -un)
real_name=$(id -F 2>/dev/null || true)
real_host=$(hostname -s 2>/dev/null || true)
names() { printf '%s\n' "$real_user" "$real_name" "$real_host" | awk 'length($0) >= 3 && $0 != "alice"'; }
paths() { printf '%s\n' "$HOME" /Users/ /private/tmp/ /tmp/irie-demo | awk '$0 != "/home/alice" && length($0) > 1'; }

# found prints whether TEXT holds a name or a path, and which, each once.
found() {
	local word
	{
		while IFS= read -r word; do
			if grep -qiwF -- "$word" <<<"$1"; then echo "$word"; fi
		done < <(names)
		while IFS= read -r word; do
			if grep -qiF -- "$word" <<<"$1"; then echo "$word"; fi
		done < <(paths)
	} | awk '!seen[tolower($0)]++'
}

# urls, tags and emails print what the text on their input holds.
urls() { grep -Eo 'https?://[^[:space:]<>"()]+' | sed 's/[].,;:!?]*$//' || true; }
tags() { grep -Eo '(^|[[:space:]])#[[:alnum:]_]+' | sed 's/^[[:space:]]*//' || true; }
emails() {
	grep -Eo '[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}' |
		grep -Eiv '@(example\.(com|org|net)|users\.noreply\.github\.com)$' || true
}

# draft prints the text of the draft under post.md's "## $2", from its fence.
draft() {
	awk -v want="## $2" '
		$0 == want { on = 1; next }
		on && /^## / { exit }
		on && /^```/ { if (fence) exit; fence = 1; next }
		on && fence { print }' "$1"
}

problems=0
problem() { echo "kit.sh: $*" >&2; problems=$((problems + 1)); }

# count prints how many lines its input has.
count() { grep -c . || true; }

# check_kit goes over the kit in $1 before anything in it is posted.
check_kit() {
	local dir=$1 text n base owner u word f clip name size length drafts=0 clips=0
	[ -d "$dir" ] || die "no kit in $dir: make one with docs/demo/kit.sh"
	if [ ! -f "$dir/post.md" ]; then
		problem "$dir has no post.md"
	else
		text=$(draft "$dir/post.md" Threads)
		if [ -z "$text" ]; then
			problem "post.md has no Threads draft in a fence"
		else
			drafts=$((drafts + 1))
			n=$(printf '%s' "$text" | LC_ALL=C wc -c | tr -d ' ')
			[ "$n" -le "$threads_bytes" ] || problem "the Threads draft is $n characters, counting each past ASCII by its UTF-8 bytes; Threads takes $threads_bytes"
			n=$(urls <<<"$text" | sort -u | count)
			[ "$n" -le "$threads_links" ] || problem "the Threads draft has $n links; Threads takes $threads_links"
			n=$(tags <<<"$text" | count)
			[ "$n" -le "$threads_topics" ] || problem "the Threads draft has $n topic tags; Threads takes $threads_topics"
		fi
		text=$(draft "$dir/post.md" Instagram)
		if [ -z "$text" ]; then
			problem "post.md has no Instagram draft in a fence"
		else
			drafts=$((drafts + 1))
			n=$(printf '%s' "$text" | perl -CS -0777 -ne 'print length')
			[ "$n" -le "$instagram_chars" ] || problem "the Instagram draft is $n characters; Instagram takes $instagram_chars"
			n=$(tags <<<"$text" | count)
			[ "$n" -le "$instagram_tags" ] || problem "the Instagram draft has $n hashtags; Instagram takes $instagram_tags"
		fi
		base=$(repo_url)
		owner=${base#https://github.com/}
		owner=${owner%%/*}
		if [ -z "$owner" ]; then
			problem "neither the brand nor CHANGELOG.md names the repository, so post.md's links can't be checked"
		else
			while IFS= read -r u; do
				case $u in
				"https://github.com/$owner/"* | "https://$owner.github.io/"* | "https://$owner.github.io") ;;
				"${BRAND_SITE:-https://github.com/$owner/}"*) ;;
				*) problem "post.md links to $u, which is not one of the project's own pages" ;;
				esac
			done < <(urls <"$dir/post.md" | sort -u)
		fi
	fi
	for f in "$dir/brief.md" "$dir/post.md"; do
		[ -f "$f" ] || continue
		while IFS= read -r word; do
			problem "$(basename "$f") names $word"
		done < <(found "$(cat "$f")")
		while IFS= read -r word; do
			problem "$(basename "$f") has the email address $word"
		done < <(emails <"$f")
	done
	for clip in "$dir"/*.mp4; do
		[ -e "$clip" ] || continue
		command -v ffprobe >/dev/null || die "ffprobe is not installed, and the kit has clips to check"
		clips=$((clips + 1))
		name=$(basename "$clip")
		size=$(ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of csv=s=x:p=0 "$clip" || true)
		[ "$size" = 1080x1920 ] || problem "$name is ${size:-unreadable}, not 1080x1920"
		length=$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$clip" || true)
		length=$(awk -v l="${length:-0}" 'BEGIN { printf "%.1f", l + 0 }')
		awk -v l="$length" -v m="$reel_min" 'BEGIN { exit !(l + 0 >= m) }' ||
			problem "$name runs $length seconds; Instagram takes a reel of $reel_min or more"
		while IFS= read -r word; do
			problem "$name's metadata names $word"
		done < <(found "$(ffprobe -v error -show_entries format_tags:stream_tags:chapter_tags -of default=noprint_wrappers=1 "$clip" || true)")
	done
	[ "$problems" -eq 0 ] || die "$(plural "$problems" problem) in ${dir#"$repo"/}: fix them before anything is posted"
	echo "kit.sh: ${dir#"$repo"/} passes: $(plural "$drafts" draft), $(plural "$clips" clip)"
}

case ${1:-} in
-h | --help)
	sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'
	;;
check)
	check_kit "${2:-$kits/$(newest)}"
	;;
*)
	version=${1:-$(newest)}
	version=${version#v}
	line=$(releases | awk -F "$sep" -v v="$version" '$1 == v { print; exit }')
	[ -n "$line" ] || die "CHANGELOG.md has no release $version"
	IFS="$sep" read -r _ date url <<<"$line"
	make_kit "$version" "$date" "$url"
	;;
esac
