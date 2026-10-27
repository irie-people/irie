#!/bin/sh
# Builds mac/build/Irie.app (docs/MAC.md).
#
# It calls swiftc rather than `swift build`: with only the Command Line Tools
# installed, SwiftPM 5.9 stops at "unable to lookup item 'PlatformPath'". The
# terminal view is SwiftTerm (MIT), fetched at the pinned release and checked
# against its SHA-256, then compiled once into a static library under .build.
# IRIE_MAC_NO_SWIFTTERM=1 builds with Apple's frameworks only; the terminal
# then opens in Ghostty.
set -eu
cd "$(dirname "$0")"

target="$(uname -m)-apple-macos14.0"
sdk=$(xcrun --sdk macosx --show-sdk-path)
app=build/Irie.app
term=1
[ "${IRIE_MAC_NO_SWIFTTERM:-}" = 1 ] && term=0

if [ "$term" = 1 ]; then
	v=1.18.0
	sum=57fb6256525f725913469e002c9d62084b01fff53b24faa700b79c1830700d48
	deps="$PWD/.build/swiftterm-$v"
	if [ ! -f "$deps/libSwiftTerm.a" ]; then
		mkdir -p "$deps"
		curl -fsSL -o "$deps/src.tar.gz" "https://github.com/migueldeicaza/SwiftTerm/archive/refs/tags/v$v.tar.gz"
		if [ "$(shasum -a 256 "$deps/src.tar.gz" | cut -d' ' -f1)" != "$sum" ]; then
			echo "SwiftTerm $v: the download's SHA-256 does not match; refusing it" >&2
			exit 1
		fi
		tar -xzf "$deps/src.tar.gz" -C "$deps"
		# SwiftTerm's own file names have no spaces, so the list splits safely.
		(cd "$deps/SwiftTerm-$v/Sources/SwiftTerm" &&
			swiftc -O -wmo -parse-as-library -target "$target" -sdk "$sdk" \
				-module-name SwiftTerm -emit-module -emit-module-path "$deps/SwiftTerm.swiftmodule" \
				-emit-library -static -o "$deps/libSwiftTerm.a" \
				$(find . -name '*.swift' -not -path './iOS/*'))
	fi
fi

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
if [ "$term" = 1 ]; then
	swiftc -O -parse-as-library -target "$target" -sdk "$sdk" \
		-I "$deps" -L "$deps" -lSwiftTerm \
		-o "$app/Contents/MacOS/Irie" Sources/Irie/*.swift
	# SwiftTerm's Metal renderer compiles these shaders at run time.
	cp "$deps/SwiftTerm-$v/Sources/SwiftTerm/Apple/Metal/Shaders.metal" "$app/Contents/Resources/"
else
	swiftc -O -parse-as-library -target "$target" -sdk "$sdk" \
		-o "$app/Contents/MacOS/Irie" Sources/Irie/*.swift
fi
cp Info.plist "$app/Contents/Info.plist"
codesign --force --sign - "$app" 2>/dev/null
echo "built $(pwd)/$app"
