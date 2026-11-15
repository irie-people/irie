#!/bin/sh
# Builds the downloads of a release into DIR (DESIGN §2.7):
#
#   irie-darwin-arm64.tar.gz  irie-darwin-amd64.tar.gz
#   irie-linux-amd64.tar.gz   irie-linux-arm64.tar.gz
#   irie-windows-amd64.zip    irie-windows-arm64.zip
#                             Irie.exe, Irie for Windows, with its icon and
#                             the notices of what it carries, and
#                             bin\irie.exe, the wrapper, which runs Irie
#                             inside WSL 2 (docs/WINDOWS.md)
#   Irie-macos.zip            Irie.app, with irie inside, for Apple silicon
#                             and Intel Macs
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
# The product's names, from internal/brand (DESIGN §2.10): irie-*.tar.gz,
# Irie-macos.zip and Irie.app unless a copy renamed it.
eval "$("$root/tools/brand.sh")"
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
	dir=$work/$BRAND_SLUG-$os-$arch
	mkdir -p "$dir"
	say "$BRAND_SLUG for $os/$arch"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -tags "$BRAND_TAGS" -ldflags "-s -w" -o "$dir/$BRAND_SLUG" ./cmd/irie
	cp LICENSE "$dir/"
	# COPYFILE_DISABLE keeps macOS's tar from adding ._ files.
	COPYFILE_DISABLE=1 tar -czf "$out/$BRAND_SLUG-$os-$arch.tar.gz" -C "$dir" "$BRAND_SLUG" LICENSE
done

# Irie for Windows (cmd/windows-app), the window, and the wrapper
# (cmd/windows) in bin beside it, which runs the Linux irie inside WSL 2,
# laid out as install.ps1 puts them in %LOCALAPPDATA%\irie. A zip, which
# Windows opens without anything more. Both are Go alone, so any computer
# builds them.
for arch in amd64 arm64; do
	dir=$work/$BRAND_SLUG-windows-$arch
	mkdir -p "$dir/bin"
	say "$BRAND_EXE.exe and $BRAND_SLUG.exe for windows/$arch"
	CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -tags "$BRAND_TAGS" -ldflags "-s -w" -o "$dir/bin/$BRAND_SLUG.exe" ./cmd/windows
	CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -tags "$BRAND_TAGS" -ldflags "-s -w -H windowsgui" -o "$dir/$BRAND_EXE.exe" ./cmd/windows-app
	cp cmd/windows-app/Irie.ico "$dir/$BRAND_EXE.ico"
	cp internal/winapp/web/vendor/NOTICES.txt "$dir/THIRD-PARTY-NOTICES.txt"
	cp LICENSE "$dir/"
	(cd "$dir" && zip -q -X -r "$out/$BRAND_SLUG-windows-$arch.zip" "$BRAND_EXE.exe" "$BRAND_EXE.ico" bin LICENSE THIRD-PARTY-NOTICES.txt)
done

if [ "$(uname -s)" = Darwin ] && [ "$(uname -m)" = arm64 ]; then
	say "$BRAND_NAME.app, for Apple silicon and Intel"
	# A build to install: its version is internal/version's, checked
	# above, and its build number orders it after earlier ones.
	IRIE_MAC_INSTALL=1 IRIE_MAC_UNIVERSAL=1 mac/build.sh
	app="$work/$BRAND_NAME.app"
	ditto "mac/build/$BRAND_NAME.app" "$app"
	# irie inside the app, so the app alone can start Irie (docs/MAC.md).
	# Contents/Helpers, not MacOS: on a disk that ignores case, irie and
	# the app's own Irie would be one file.
	mkdir -p "$app/Contents/Helpers"
	lipo -create "$work/$BRAND_SLUG-darwin-arm64/$BRAND_SLUG" "$work/$BRAND_SLUG-darwin-amd64/$BRAND_SLUG" -output "$app/Contents/Helpers/$BRAND_SLUG"
	# Signed again from the inside out, so the app's signature covers its
	# helper, with the identity mac/build.sh signs with: ad hoc unless
	# IRIE_SIGN_IDENTITY or `git config irie.sign` names one (docs/MAC.md).
	identity=${IRIE_SIGN_IDENTITY:-$(git config --get irie.sign 2>/dev/null || echo -)}
	for f in "$app/Contents/Helpers/$BRAND_SLUG" "$app"; do
		case "$identity" in
		-) codesign --force --sign - "$f" ;;
		"Developer ID"*) codesign --force --options runtime --timestamp --sign "$identity" "$f" ;;
		*) codesign --force --sign "$identity" "$f" ;;
		esac
	done
	codesign --verify --strict "$app"
	ditto -c -k --keepParent "$app" "$out/$BRAND_EXE-macos.zip"
else
	say "not a Mac with Apple silicon: no $BRAND_NAME.app"
fi

cd "$out"
sums=$(for f in "$BRAND_SLUG"-*.tar.gz "$BRAND_SLUG"-windows-*.zip "$BRAND_EXE"-*.zip; do [ -e "$f" ] && printf '%s\n' "$f"; done | sort -u)
if command -v sha256sum >/dev/null 2>&1; then
	echo "$sums" | xargs sha256sum >SHA256SUMS
else
	echo "$sums" | xargs shasum -a 256 >SHA256SUMS
fi
say "done: $(echo "$sums" | wc -l | tr -d ' ') downloads in $out"
