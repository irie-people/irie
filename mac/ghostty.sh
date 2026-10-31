#!/bin/sh
# Builds GhosttyKit, Ghostty's embedding library (MIT), which draws the
# app's terminals (docs/proposals/ghostty-terminal.md), and prints the
# folder that holds it: include/ (ghostty.h and a module map), lib/
# (libghostty.a) and share/ (terminfo and shell integration). It builds once
# per Ghostty release and architecture into the user's cache, which every
# checkout and worktree shares, and prints the folder at once after that.
#
# Ghostty's source and Zig, the compiler Ghostty builds with, are fetched
# at pinned releases and checked against their SHA-256. Ghostty's build
# needs Xcode for its Metal compiler and xcodebuild, which the Command Line
# Tools lack: Xcode as xcode-select names it, else /Applications/Xcode.app.
set -eu

v=1.3.1
sum=265837d3026b433f0e6b4e49d43153b915b0a19513f7edd8a8e693c559bd415b
zig=0.15.2 # the one Zig that builds Ghostty 1.3.x
case $(uname -m) in
arm64) arch=aarch64 zigsum=3cc2bab367e185cdfb27501c4b30b1b0653c28d9f73df8dc91488e66ece5fa6b ;;
x86_64) arch=x86_64 zigsum=375b6909fc1495d16fc2c7db9538f707456bfc3373b14ee83fdd3e22b3d43f7f ;;
*)
	echo "ghostty.sh: no Zig for $(uname -m)" >&2
	exit 1
	;;
esac
cache=${XDG_CACHE_HOME:-$HOME/Library/Caches}/irie
out="$cache/ghostty-$v-$arch"
if [ -f "$out/lib/libghostty.a" ]; then
	echo "$out"
	exit 0
fi

dev=$(xcode-select -p)
if ! DEVELOPER_DIR=$dev xcrun --find metal >/dev/null 2>&1; then
	dev=/Applications/Xcode.app/Contents/Developer
fi
if ! DEVELOPER_DIR=$dev xcrun --find metal >/dev/null 2>&1; then
	echo "ghostty.sh: Ghostty's build needs Xcode (its Metal compiler); none is installed" >&2
	exit 1
fi
export DEVELOPER_DIR=$dev

# Built aside and renamed into place, so a build running alongside never
# sees half a library. Zig's own cache stays inside, and goes with it.
mkdir -p "$cache"
tmp=$(mktemp -d "$cache/.ghostty.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
export ZIG_GLOBAL_CACHE_DIR="$tmp/zig-cache"

fetch() { # url file sha256
	curl -fsSL --retry 3 -o "$2" "$1"
	if [ "$(shasum -a 256 "$2" | cut -d' ' -f1)" != "$3" ]; then
		echo "ghostty.sh: $1: the download's SHA-256 does not match; refusing it" >&2
		exit 1
	fi
}
fetch "https://ziglang.org/download/$zig/zig-$arch-macos-$zig.tar.xz" "$tmp/zig.tar.xz" "$zigsum"
tar -xJf "$tmp/zig.tar.xz" -C "$tmp"
zigbin="$tmp/zig-$arch-macos-$zig/zig"
fetch "https://github.com/ghostty-org/ghostty/archive/refs/tags/v$v.tar.gz" "$tmp/ghostty.tar.gz" "$sum"
tar -xzf "$tmp/ghostty.tar.gz" -C "$tmp"
src="$tmp/ghostty-$v"

# Zig's own fetch stopped part way on Ghostty's dependency mirror here, so
# curl fetches each dependency and `zig fetch` files it in Zig's cache by
# its hash, which the build then finds. Each is checked against the hash
# its build.zig.zon names.
mkdir "$tmp/deps"
grep -rh --include=build.zig.zon '\.url = "https://' "$src" | sed 's/.*"\(https:[^"]*\)".*/\1/' | grep -v '/COMMIT\.tar\.gz$' | sort -u |
	while read -r url; do
		f="$tmp/deps/$(basename "$url")"
		curl -fsSL --retry 3 -o "$f" "$url"
		"$zigbin" fetch "$f" >/dev/null
	done

# The macOS 14 SDK lacks kCVPixelFormatType_30RGB_r210, which Ghostty
# declares but never uses; newer SDKs define it as the code 'r210'.
pf="$src/pkg/macos/video/pixel_format.zig"
if ! grep -q kCVPixelFormatType_30RGB_r210 "$(xcrun --sdk macosx --show-sdk-path)/System/Library/Frameworks/CoreVideo.framework/Headers/CVPixelBuffer.h"; then
	sed -i '' 's/c\.kCVPixelFormatType_30RGB_r210,/0x72323130,/' "$pf"
fi

(cd "$src" && "$zigbin" build -Demit-xcframework=true -Dxcframework-target=native -Demit-macos-app=false -Doptimize=ReleaseFast)

kit="$src/macos/GhosttyKit.xcframework/macos-$(uname -m)"
mkdir -p "$tmp/out/include" "$tmp/out/lib" "$tmp/out/share"
cp "$kit/libghostty-fat.a" "$tmp/out/lib/libghostty.a"
cp "$kit/Headers/ghostty.h" "$tmp/out/include/"
# The framework's own map makes ghostty.h an umbrella over headers it does
# not include; the app needs only ghostty.h.
printf 'module GhosttyKit {\n    header "ghostty.h"\n    export *\n}\n' >"$tmp/out/include/module.modulemap"
cp -R "$src/zig-out/share/terminfo" "$tmp/out/share/"
mkdir "$tmp/out/share/ghostty"
cp -R "$src/zig-out/share/ghostty/shell-integration" "$src/zig-out/share/ghostty/themes" "$tmp/out/share/ghostty/"
cp "$src/LICENSE" "$tmp/out/LICENSE"
# A build alongside may have finished first; either copy will do.
if [ ! -e "$out" ]; then mv "$tmp/out" "$out"; fi
echo "$out"
