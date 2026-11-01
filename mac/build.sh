#!/bin/sh
# Builds mac/build/Irie.app (docs/MAC.md).
#
# It calls swiftc rather than `swift build`: with only the Command Line Tools
# installed, SwiftPM 5.9 stops at "unable to lookup item 'PlatformPath'". The
# terminal view is SwiftTerm (MIT), fetched at the pinned release and checked
# against its SHA-256, then compiled once into a static library in the user's
# cache, which every checkout and worktree shares. Ghostty's own engine,
# GhosttyKit, draws the terminals when it builds (ghostty.sh, cached the same
# way); without it the app uses SwiftTerm alone. The icon is drawn in Go.
# Mermaid (MIT) draws the diagrams agents write in tutor mode; its script is
# fetched at the pinned release and checked against its SHA-256 the same
# way, and without it a diagram shows as its text.
# IRIE_MAC_NO_SWIFTTERM=1 builds with Apple's frameworks only; the terminal
# then opens in Ghostty. IRIE_MAC_NO_GHOSTTY=1 leaves GhosttyKit out.
# IRIE_SIGN_IDENTITY signs it with your own identity (below).
# IRIE_MAC_UNIVERSAL=1 on Apple silicon adds an Intel slice, drawn with
# SwiftTerm, as a release's app has (tools/release.sh).
set -eu
cd "$(dirname "$0")"

target="$(uname -m)-apple-macos14.0"
sdk=$(xcrun --sdk macosx --show-sdk-path)
app=build/Irie.app
term=1
[ "${IRIE_MAC_NO_SWIFTTERM:-}" = 1 ] && term=0

v=1.18.0
sum=57fb6256525f725913469e002c9d62084b01fff53b24faa700b79c1830700d48
# swiftterm TARGET builds SwiftTerm for TARGET once and sets deps to it.
# One copy for every checkout and worktree, so SwiftTerm compiles once per
# release, target and compiler, not once per worktree. A module loads only
# into the compiler that built it, so the compiler is in the name.
swiftterm() {
	cc=$(swiftc --version 2>&1 | shasum -a 256 | cut -c1-12)
	cache=${XDG_CACHE_HOME:-$HOME/Library/Caches}/irie
	deps="$cache/swiftterm-$v-$1-$cc"
	if [ ! -f "$deps/libSwiftTerm.a" ]; then
		# Built aside and renamed into place, so a build running alongside
		# never sees half a library.
		mkdir -p "$cache"
		tmp=$(mktemp -d "$cache/.swiftterm.XXXXXX")
		trap 'rm -rf "$tmp"' EXIT
		curl -fsSL -o "$tmp/src.tar.gz" "https://github.com/migueldeicaza/SwiftTerm/archive/refs/tags/v$v.tar.gz"
		if [ "$(shasum -a 256 "$tmp/src.tar.gz" | cut -d' ' -f1)" != "$sum" ]; then
			echo "SwiftTerm $v: the download's SHA-256 does not match; refusing it" >&2
			exit 1
		fi
		tar -xzf "$tmp/src.tar.gz" -C "$tmp"
		# SwiftTerm's own file names have no spaces, so the list splits safely.
		(cd "$tmp/SwiftTerm-$v/Sources/SwiftTerm" &&
			swiftc -O -wmo -parse-as-library -target "$1" -sdk "$sdk" \
				-module-name SwiftTerm -emit-module -emit-module-path "$tmp/SwiftTerm.swiftmodule" \
				-emit-library -static -o "$tmp/libSwiftTerm.a" \
				$(find . -name '*.swift' -not -path './iOS/*'))
		# A build alongside may have finished first; either copy will do.
		if [ ! -e "$deps" ]; then mv "$tmp" "$deps"; fi
	fi
}
if [ "$term" = 1 ]; then
	swiftterm "$target"
fi

# Mermaid's script and license, for the diagrams in the conversation view
# (docs/proposals/learning.md §4.8). One copy in the cache for every
# checkout; a download that fails or does not match leaves diagrams as text.
mv=11.4.1
msum=a43bc1afd446f9c4cc66ac5dd45d02e8d65e26fc5344ec0ef787f88d6ddb6f9e
lsum=ec9fb67dcb25eccc416ed56e1aab819222c805a2a4bfe4cb19e7556bf2ffde80
mcache=${XDG_CACHE_HOME:-$HOME/Library/Caches}/irie
mermaid="$mcache/mermaid-$mv"
if [ ! -f "$mermaid/mermaid.min.js" ]; then
	mkdir -p "$mcache"
	mtmp=$(mktemp -d "$mcache/.mermaid.XXXXXX")
	if curl -fsSL --max-time 120 -o "$mtmp/mermaid.min.js" "https://cdn.jsdelivr.net/npm/mermaid@$mv/dist/mermaid.min.js" &&
		curl -fsSL --max-time 60 -o "$mtmp/LICENSE" "https://cdn.jsdelivr.net/npm/mermaid@$mv/LICENSE" &&
		[ "$(shasum -a 256 "$mtmp/mermaid.min.js" | cut -d' ' -f1)" = "$msum" ] &&
		[ "$(shasum -a 256 "$mtmp/LICENSE" | cut -d' ' -f1)" = "$lsum" ]; then
		if [ ! -e "$mermaid" ]; then mv "$mtmp" "$mermaid"; else rm -rf "$mtmp"; fi
	else
		rm -rf "$mtmp"
		echo "build.sh: Mermaid $mv did not download or its SHA-256 did not match, so diagrams show as text" >&2
	fi
fi

# GhosttyKit, when it builds: a Ghostty that will not build, as without Xcode
# or a network on the first build, leaves the app on SwiftTerm.
ghostty=
if [ "$term" = 1 ] && [ "${IRIE_MAC_NO_GHOSTTY:-}" != 1 ]; then
	if ! ghostty=$(./ghostty.sh); then
		ghostty=
		echo "build.sh: GhosttyKit did not build, so the app's terminals use SwiftTerm" >&2
	fi
fi

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
if [ "$term" = 1 ]; then
	set --
	if [ -n "$ghostty" ]; then
		# The module map makes `canImport(GhosttyKit)` true, which compiles
		# the Ghostty terminal in (GhosttyTerminal.swift).
		set -- -I "$ghostty/include" -L "$ghostty/lib" -lghostty -lc++ \
			-framework Carbon -framework CoreText -framework CoreVideo -framework IOSurface \
			-framework Metal -framework OpenGL -framework QuartzCore -Xlinker -dead_strip
	fi
	swiftc -O -parse-as-library -target "$target" -sdk "$sdk" \
		-I "$deps" -L "$deps" -lSwiftTerm "$@" \
		-o "$app/Contents/MacOS/Irie" Sources/Irie/*.swift
	# The Intel slice draws with SwiftTerm: GhosttyKit builds for this
	# Mac's own processor only. Without its module, `canImport(GhosttyKit)`
	# is false in that slice.
	if [ "${IRIE_MAC_UNIVERSAL:-}" = 1 ] && [ "$(uname -m)" = arm64 ]; then
		native=$deps
		swiftterm x86_64-apple-macos14.0
		swiftc -O -parse-as-library -target x86_64-apple-macos14.0 -sdk "$sdk" \
			-I "$deps" -L "$deps" -lSwiftTerm \
			-o build/Irie-x86_64 Sources/Irie/*.swift
		lipo -create "$app/Contents/MacOS/Irie" build/Irie-x86_64 -output build/Irie-universal
		mv build/Irie-universal "$app/Contents/MacOS/Irie"
		rm -f build/Irie-x86_64
		deps=$native
	fi
	# SwiftTerm's Metal renderer compiles these shaders at run time.
	cp "$deps/SwiftTerm-$v/Sources/SwiftTerm/Apple/Metal/Shaders.metal" "$app/Contents/Resources/"
	# Ghostty finds terminfo and its shell integration beside its
	# resources, as in Ghostty's own app.
	if [ -n "$ghostty" ]; then
		cp -R "$ghostty/share/ghostty" "$ghostty/share/terminfo" "$app/Contents/Resources/"
	fi
else
	swiftc -O -parse-as-library -target "$target" -sdk "$sdk" \
		-o "$app/Contents/MacOS/Irie" Sources/Irie/*.swift
fi
cp Info.plist "$app/Contents/Info.plist"
if [ -f "$mermaid/mermaid.min.js" ]; then
	cp "$mermaid/mermaid.min.js" "$app/Contents/Resources/"
	cp "$mermaid/LICENSE" "$app/Contents/Resources/mermaid-LICENSE.txt"
fi
# Irie's icon (DESIGN §7.2): tools/mkicon draws the iconset, iconutil packs it.
rm -rf build/Irie.iconset
go run ../tools/mkicon build/Irie.iconset
iconutil -c icns -o "$app/Contents/Resources/Irie.icns" build/Irie.iconset
rm -rf build/Irie.iconset
# Signed ad hoc, which runs on this Mac only, unless IRIE_SIGN_IDENTITY
# names a code signing identity in the keychain: "Apple Development: …",
# which Xcode makes once you sign in with your Apple ID, or "Developer ID
# Application: …", the paid program's, for other Macs. A Developer ID build
# takes the hardened runtime and a secure timestamp from Apple, as
# notarization wants. A new identity is a new app to macOS's privacy
# settings, which ask again for what they had allowed.
# `git config irie.sign "<identity>"` keeps the choice for every install.
identity=${IRIE_SIGN_IDENTITY:-$(git config --get irie.sign 2>/dev/null || echo -)}
case "$identity" in
-) codesign --force --sign - "$app" 2>/dev/null ;;
"Developer ID"*) codesign --force --options runtime --timestamp --sign "$identity" "$app" ;;
*) codesign --force --sign "$identity" "$app" ;;
esac
echo "built $(pwd)/$app"
