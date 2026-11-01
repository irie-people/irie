#!/bin/sh
# Builds the downloads of a release into DIR (DESIGN §2.7):
#
#   irie-darwin-arm64.tar.gz  irie-darwin-amd64.tar.gz
#   irie-linux-amd64.tar.gz   irie-linux-arm64.tar.gz
#   Irie-macos-arm64.zip      Irie.app, with irie inside, for Apple silicon
#   SHA256SUMS                the SHA-256 of each
#
# The site's install.sh picks among them. Their names carry no version, so
# github.com/irie-people/irie/releases/latest/download/<name> always
# finds the latest. It runs on a Mac with Xcode, as the release workflow
# does; elsewhere it builds the irie downloads only.
#
#   tools/release.sh 0.2.0 dist
set -eu

version=${1:?usage: tools/release.sh VERSION DIR}
out=${2:?usage: tools/release.sh VERSION DIR}
version=${version#v}
root=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

say() { printf 'release.sh: %s\n' "$*" >&2; }

base=$(sed -n 's/^const Base = "\([^"]*\)".*/\1/p' "$root/internal/version/version.go")
if [ "$base" != "$version" ]; then
	say "internal/version says $base, not $version"
	exit 1
fi

cd "$root"
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do
	os=${target%/*}
	arch=${target#*/}
	dir=$work/irie-$os-$arch
	mkdir -p "$dir"
	say "irie for $os/$arch"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "$dir/irie" ./cmd/irie
	cp LICENSE "$dir/"
	# COPYFILE_DISABLE keeps macOS's tar from adding ._ files.
	COPYFILE_DISABLE=1 tar -czf "$out/irie-$os-$arch.tar.gz" -C "$dir" irie LICENSE
done

if [ "$(uname -s)" = Darwin ] && [ "$(uname -m)" = arm64 ]; then
	say "Irie.app"
	mac/build.sh
	app=$work/Irie.app
	ditto mac/build/Irie.app "$app"
	# irie inside the app, so the app alone can start Irie (docs/MAC.md).
	# Contents/Helpers, not MacOS: on a disk that ignores case, irie and
	# the app's own Irie would be one file.
	mkdir -p "$app/Contents/Helpers"
	cp "$work/irie-darwin-arm64/irie" "$app/Contents/Helpers/irie"
	plutil -replace CFBundleShortVersionString -string "$version" "$app/Contents/Info.plist"
	# Signed inside out, ad hoc: a signature of the app covers its helper.
	codesign --force --sign - "$app/Contents/Helpers/irie"
	codesign --force --sign - "$app"
	codesign --verify --strict "$app"
	ditto -c -k --keepParent "$app" "$out/Irie-macos-arm64.zip"
else
	say "not a Mac with Apple silicon: no Irie.app"
fi

cd "$out"
sums=$(for f in irie-*.tar.gz Irie-*.zip; do [ -e "$f" ] && printf '%s\n' "$f"; done)
if command -v sha256sum >/dev/null 2>&1; then
	echo "$sums" | xargs sha256sum >SHA256SUMS
else
	echo "$sums" | xargs shasum -a 256 >SHA256SUMS
fi
say "done: $(echo "$sums" | wc -l | tr -d ' ') downloads in $out"
