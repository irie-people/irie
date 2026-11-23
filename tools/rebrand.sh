#!/bin/sh
# Renames this checkout to a company's copy, to test how Irie ports to one
# (DESIGN §2.10, docs/proposals/company-copies.md): internal/brand/brand.go
# takes NAME's names and a company's defaults, tips in the bar, the
# personal plugins left out and Cisco Secure Client at vpn.example.com as
# its VPN, and mac/Sources/Irie/Brand.swift is written from it. It is for a
# clone of your own, a scratch copy or CI, never Irie itself: it refuses a
# checkout whose installs push to Irie's own repository (git config
# irie.push), as tools/live.sh would push the rename there.
#
#   tools/rebrand.sh [--tagline TEXT] [--arch NAMES] [--pronoun he|she|it] NAME [SLUG]
#
# NAME is letters, digits, spaces, hyphens and full stops, as brand.Name
# allows; SLUG is NAME in lower case with hyphens for spaces unless given.
# The copy's addresses are example.com's. Its arch agents are named after
# the work they do, Guard, Priority, Medic and Planner for Mikael,
# Gabriel, Raphael and Raguel, and spoken of as "it": --arch names them
# otherwise, four comma-separated names in that order, and --pronoun
# speaks of them as he or she. go test ./internal/brand checks the result.
set -eu
export LC_ALL=C
cd "$(dirname "$0")/.."

tagline="Your agents, at work."
arch="Guard,Priority,Medic,Planner"
pronoun=it
while :; do
	case ${1:-} in
	--tagline)
		tagline=${2:?--tagline needs a text}
		shift 2
		;;
	--arch)
		arch=${2:?--arch needs four names}
		shift 2
		;;
	--pronoun)
		pronoun=${2:?--pronoun needs he, she or it}
		shift 2
		;;
	*) break ;;
	esac
done
name=${1:?usage: tools/rebrand.sh [--tagline TEXT] NAME [SLUG]}
case $name in
'' | *[!A-Za-z0-9\ .-]*)
	echo "rebrand.sh: a name is letters, digits, spaces, hyphens and full stops" >&2
	exit 2
	;;
esac
case $tagline in
*[\"\\\|\&]*)
	echo "rebrand.sh: a tagline holds no \", \\, | or &" >&2
	exit 2
	;;
esac
slug=${2:-$(printf '%s' "$name" | tr 'A-Z ' 'a-z-' | tr -cd 'a-z0-9-')}
case $slug in
[a-z]*) ;;
*)
	echo "rebrand.sh: a slug is lower-case letters, digits and hyphens, starting with a letter" >&2
	exit 2
	;;
esac
case $slug in *[!a-z0-9-]*)
	echo "rebrand.sh: a slug is lower-case letters, digits and hyphens, starting with a letter" >&2
	exit 2
	;;
esac
env=$(printf '%s' "$slug" | tr 'a-z-' 'A-Z_')
case $arch in
*,*,*,*,* | *[!A-Za-z0-9\ .,-]*)
	echo "rebrand.sh: --arch is four names, comma-separated, of letters, digits, spaces, hyphens and full stops" >&2
	exit 2
	;;
*,*,*,*) ;;
*)
	echo "rebrand.sh: --arch is four names, comma-separated: for Mikael, Gabriel, Raphael and Raguel" >&2
	exit 2
	;;
esac
mikael=${arch%%,*} rest=${arch#*,}
gabriel=${rest%%,*} rest=${rest#*,}
raphael=${rest%%,*} raguel=${rest#*,}
case $pronoun in
he) him=him his=his ;;
she) him=her his=her ;;
it) him=it his=its ;;
*)
	echo "rebrand.sh: --pronoun is he, she or it" >&2
	exit 2
	;;
esac

remote=$(git config --get irie.push 2>/dev/null || true)
if [ -n "$remote" ] && git remote get-url --push "$remote" 2>/dev/null | grep -Eq 'irie-people/irie(\.git)?$'; then
	echo "rebrand.sh: this checkout's installs push to Irie's own repository ($remote, git config irie.push), so it stays Irie; rename a clone of your own" >&2
	exit 1
fi

f=internal/brand/brand.go
# -i.bak works with both BSD and GNU sed.
sed -i.bak \
	-e "s/^	Name = \".*\"\$/	Name = \"$name\"/" \
	-e "s/^	Slug = \".*\"\$/	Slug = \"$slug\"/" \
	-e "s/^	EnvPrefix = \".*\"\$/	EnvPrefix = \"$env\"/" \
	-e "s/^	BundleID = \".*\"\$/	BundleID = \"com.example.$slug\"/" \
	-e "s|^	Site = \".*\"\$|	Site = \"https://$slug.example.com/\"|" \
	-e "s|^	Repo = \".*\"\$|	Repo = \"https://example.com/$slug\"|" \
	-e "s|^	Tagline = \".*\"\$|	Tagline = \"$tagline\"|" \
	-e 's/^	Bar = ".*"$/	Bar = "tips"/' \
	-e 's/^	Tags = ".*"$/	Tags = "noprivacy,nowriting,nomarkets,nomail,noterms,noextension,noyoink,nofounding,nojammin"/' \
	-e 's/^	VPNs = ".*"$/	VPNs = "cisco"/' \
	-e 's/^	VPNServer = ".*"$/	VPNServer = "vpn.example.com"/' \
	-e "s/^	Mikael\([[:space:]]*\)= \".*\"\$/	Mikael\1= \"$mikael\"/" \
	-e "s/^	Gabriel\([[:space:]]*\)= \".*\"\$/	Gabriel\1= \"$gabriel\"/" \
	-e "s/^	Raphael\([[:space:]]*\)= \".*\"\$/	Raphael\1= \"$raphael\"/" \
	-e "s/^	Raffi\([[:space:]]*\)= \".*\"\$/	Raffi\1= \"$raphael\"/" \
	-e "s/^	Raguel\([[:space:]]*\)= \".*\"\$/	Raguel\1= \"$raguel\"/" \
	-e "s/^	Ragu\([[:space:]]*\)= \".*\"\$/	Ragu\1= \"$raguel\"/" \
	-e "s/^	He\([[:space:]]*\)= \".*\"\$/	He\1= \"$pronoun\"/" \
	-e "s/^	Him\([[:space:]]*\)= \".*\"\$/	Him\1= \"$him\"/" \
	-e "s/^	His\([[:space:]]*\)= \".*\"\$/	His\1= \"$his\"/" \
	"$f"
rm -f "$f.bak"
for want in "Name = \"$name\"" "Slug = \"$slug\"" "EnvPrefix = \"$env\"" 'Bar = "tips"' 'VPNs = "cisco"'; do
	if ! grep -qF "	$want" "$f"; then
		echo "rebrand.sh: $f has no line $want: has brand.go changed its form?" >&2
		exit 1
	fi
done
sh tools/brand.sh --swift >mac/Sources/Irie/Brand.swift
sh tools/brand.sh
