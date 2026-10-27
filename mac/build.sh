#!/bin/sh
# Builds mac/build/Irie.app with Apple's frameworks only (docs/MAC.md).
#
# It calls swiftc rather than `swift build`: with only the Command Line Tools
# installed, SwiftPM 5.9 stops at "unable to lookup item 'PlatformPath'". The
# SwiftTerm build, which draws the terminal in the window, goes through
# Package.swift instead.
set -eu
cd "$(dirname "$0")"

app=build/Irie.app
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
swiftc -O -parse-as-library \
	-target "$(uname -m)-apple-macos14.0" \
	-sdk "$(xcrun --sdk macosx --show-sdk-path)" \
	-o "$app/Contents/MacOS/Irie" \
	Sources/Irie/*.swift
cp Info.plist "$app/Contents/Info.plist"
codesign --force --sign - "$app" 2>/dev/null
echo "built $(pwd)/$app"
