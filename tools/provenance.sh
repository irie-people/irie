#!/bin/sh
# Keeps PROVENANCE.md (AGENTS.md, "Public sources only") without reading it
# whole: it is the repository's largest file, and nearly every change adds
# to it.
#
#   tools/provenance.sh add SOURCE USED_FOR   add a row dated today in UTC at
#                                             the end of the Sources table
#   tools/provenance.sh find TEXT             the rows that mention TEXT
#   tools/provenance.sh check                 fail when a row is listed twice
#
# Branches all add their rows at the same place, so .gitattributes merges
# the file as a union: both sides' rows stay, and check finds a row that
# both sides added.
set -eu
cd "$(git rev-parse --show-toplevel)"
file=PROVENANCE.md
usage="usage: tools/provenance.sh add SOURCE USED_FOR | find TEXT | check"

case ${1:-} in
add)
	if [ $# -ne 3 ] || [ -z "$2" ] || [ -z "$3" ]; then
		echo "$usage" >&2
		exit 2
	fi
	# A row is one line, and a | would end its cell.
	cell() { printf '%s' "$1" | tr '\n' ' ' | sed 's/|/\\|/g'; }
	src=$(cell "$2")
	use=$(cell "$3")
	if grep -qF -- "| $src | $use |" "$file"; then
		echo "provenance: already listed: $src" >&2
		exit 0
	fi
	row="| $(date -u +%Y-%m-%d) | $src | $use |"
	# ENVIRON, not awk -v, which would read the row's backslashes as escapes.
	if ! ROW=$row awk '
		/^## / { sources = ($0 == "## Sources") }
		{ line[NR] = $0; if (sources && /^\|/) last = NR }
		END {
			if (!last) exit 1
			for (i = 1; i <= NR; i++) { print line[i]; if (i == last) print ENVIRON["ROW"] }
		}' "$file" >"$file.tmp"; then
		rm -f "$file.tmp"
		echo "provenance: $file has no Sources table" >&2
		exit 1
	fi
	mv "$file.tmp" "$file"
	echo "$row"
	;;
find)
	if [ $# -ne 2 ] || [ -z "$2" ]; then
		echo "$usage" >&2
		exit 2
	fi
	grep -inF -- "$2" "$file" || st=$?
	# grep's 1 is no match; a reader that stops early ends it another way.
	if [ "${st:-0}" = 1 ]; then echo "provenance: no row mentions $2" >&2; fi
	exit "${st:-0}"
	;;
check)
	twice=$(grep -E '^\| [0-9]{4}-[0-9]{2}-[0-9]{2} ' "$file" | sort | uniq -d)
	if [ -n "$twice" ]; then
		echo "provenance: listed twice in $file:" >&2
		printf '%s\n' "$twice" >&2
		exit 1
	fi
	;;
*)
	echo "$usage" >&2
	exit 2
	;;
esac
