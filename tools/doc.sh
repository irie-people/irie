#!/bin/sh
# Prints one section of a doc, or its outline, so that reading a doc costs
# only the part needed: DESIGN.md alone is over 200 KB.
#
#   tools/doc.sh                  the docs there are
#   tools/doc.sh DOC              DOC's outline: each heading, with its line
#   tools/doc.sh DOC SECTION...   these sections, each with its subsections:
#                                 name several to read them in one call
#
# DOC is a path, or a name such as DESIGN, MAC or skills, looked for as
# docs/DOC.md, docs/proposals/DOC.md and DOC.md, in any case. SECTION is a
# number, such as 4.8 or §4.8, or words from its heading, such as "jot dog".
# Lines in code blocks are never taken for headings.
set -eu
cd "$(git rev-parse --show-toplevel)"

if [ $# -eq 0 ]; then
	ls docs/*.md docs/proposals/*.md ./*.md
	exit 0
fi

doc=
if [ -f "$1" ]; then
	doc=$1
else
	want=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
	for f in docs/*.md docs/proposals/*.md ./*.md; do
		if [ "$(basename "$f" .md | tr '[:upper:]' '[:lower:]')" = "$want" ]; then
			doc=$f
			break
		fi
	done
fi
if [ -z "$doc" ]; then
	echo "doc: no doc named $1; tools/doc.sh lists them" >&2
	exit 2
fi

if [ $# -eq 1 ]; then
	awk '/^(```|~~~)/ { code = !code } !code && /^#+ / { printf "%5d  %s\n", NR, $0 }' "$doc"
	exit 0
fi

name=$1
shift
st=0
for want in "$@"; do
	# A number is matched at the start of a heading; words anywhere in it.
	num=
	case $want in
	[0-9]* | §[0-9]*) num=${want#§} ;;
	esac
	s=0
	WANT=$want NUM=$num awk '
		BEGIN { want = tolower(ENVIRON["WANT"]); num = ENVIRON["NUM"] }
		/^(```|~~~)/ { code = !code }
		!code && /^#+ / {
			level = index($0, " ") - 1
			text = substr($0, level + 2)
			if (on && level <= at) exit
			if (!on) {
				if (num != "") hit = index(text, num " ") == 1 || index(text, num ". ") == 1
				else hit = index(tolower(text), want) > 0
				if (hit) { on = 1; at = level }
			}
		}
		on { print }
		END { if (!on) exit 1 }' "$doc" || s=$?
	# Only 1 means no such section; the others go on. A reader that stops
	# early, such as head, ends awk another way, and then so does this.
	case $s in
	0) ;;
	1)
		echo "doc: no section $want in $doc; tools/doc.sh $name shows its outline" >&2
		st=1
		;;
	*) exit "$s" ;;
	esac
done
exit "$st"
