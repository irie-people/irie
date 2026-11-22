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
#   tools/rebrand.sh [--tagline TEXT] NAME [SLUG]
#
# NAME is letters, digits, spaces, hyphens and full stops, as brand.Name
# allows; SLUG is NAME in lower case with hyphens for spaces unless given.
# The copy's addresses are example.com's. go test ./internal/brand checks
# the result.
set -eu
export LC_ALL=C
cd "$(dirname "$0")/.."

tagline="Your agents, at work."
if [ "${1:-}" = --tagline ]; then
	tagline=${2:?--tagline needs a text}
	shift 2
fi
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
