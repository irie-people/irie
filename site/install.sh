#!/bin/sh
# Irie's installer, for macOS and Linux:
#
#   curl -fsSL https://irie-people.github.io/irie/install.sh | sh
#
# It downloads the latest release of irie for this computer from GitHub,
# checks its SHA-256, and puts it in ~/.local/share/irie/bin, with a link
# in ~/.local/bin. On a Mac with Apple silicon it puts Irie.app in
# ~/Applications, where Spotlight finds it. Then `irie setup` checks for
# tmux, Claude Code and a sign-in, and offers to install each one that is
# missing; nothing is installed without a yes. Running it again updates
# Irie, and agents that are running carry on.
#
# What it reads from the environment:
#
#   IRIE_VERSION=0.2.0   install that release, not the latest
#   IRIE_NO_APP=1        leave Irie.app out
#   IRIE_NO_SETUP=1      don't run irie setup at the end
#   IRIE_DOWNLOADS=URL   where the release files are, such as a mirror
#
# The source of this file is site/install.sh in github.com/irie-people/irie.
set -eu

repo=https://github.com/irie-people/irie
docs=https://irie-people.github.io/irie/install.html

say() { printf '%s\n' "$*"; }
step() {
	if [ -t 1 ]; then
		printf '\033[1m%s\033[0m\n' "$*"
	else
		say "$*"
	fi
}
fail() {
	printf 'irie install: %s\n' "$*" >&2
	exit 1
}

# All of it runs in main, so a download cut short runs nothing.
main() {
	case $(uname -s) in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) fail "Irie runs on macOS and Linux; $(uname -s) isn't one. See $docs" ;;
	esac
	case $(uname -m) in
	arm64 | aarch64) arch=arm64 ;;
	x86_64 | amd64) arch=amd64 ;;
	*) fail "there is no Irie for $(uname -m) yet. See $docs" ;;
	esac
	# A shell under Rosetta says x86_64 on Apple silicon.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = 1 ]; then
		arch=arm64
	fi

	if command -v curl >/dev/null 2>&1; then
		fetch() { curl -fsSL --retry 2 -o "$2" "$1"; }
	elif command -v wget >/dev/null 2>&1; then
		fetch() { wget -q -O "$2" "$1"; }
	else
		fail "it needs curl or wget to download Irie"
	fi
	if command -v sha256sum >/dev/null 2>&1; then
		sha256() { sha256sum "$1" | cut -d' ' -f1; }
	elif command -v shasum >/dev/null 2>&1; then
		sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
	else
		fail "it needs sha256sum or shasum to check the download"
	fi

	home=${HOME:?HOME is not set}
	case ${XDG_DATA_HOME:-} in
	/*) data=$XDG_DATA_HOME/irie ;;
	*) data=$home/.local/share/irie ;;
	esac
	bindir=$data/bin
	linkdir=$home/.local/bin

	if [ -n "${IRIE_DOWNLOADS:-}" ]; then
		from=${IRIE_DOWNLOADS%/}
	elif [ -n "${IRIE_VERSION:-}" ]; then
		from=$repo/releases/download/v${IRIE_VERSION#v}
	else
		from=$repo/releases/latest/download
	fi

	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t irie)
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 130' INT TERM

	asset=irie-$os-$arch.tar.gz
	step "Downloading Irie for $(pretty "$os") on $arch"
	# curl's own words are kept for when building from source can't
	# happen either: a release with no download for this computer is no
	# error by itself.
	if ! fetch "$from/SHA256SUMS" "$tmp/SHA256SUMS" 2>"$tmp/why" || ! fetch "$from/$asset" "$tmp/$asset" 2>>"$tmp/why"; then
		from_source
	else
		check "$asset"
		mkdir -p "$tmp/irie"
		tar -xzf "$tmp/$asset" -C "$tmp/irie"
		[ -x "$tmp/irie/irie" ] || fail "$asset has no irie in it"
		put "$tmp/irie/irie"
	fi
	link

	if [ "$os" = darwin ] && [ -z "${IRIE_NO_APP:-}" ]; then
		app
	fi

	version=$("$bindir/irie" version 2>/dev/null | cut -d' ' -f2 | cut -d+ -f1)
	say ""
	step "Irie ${version:-} is installed."
	# A release from before irie setup: say what it needs instead.
	if ! "$bindir/irie" help 2>/dev/null | grep -q 'irie setup \[--check'; then
		say "It needs tmux and Claude Code, signed in: see $docs#setup. Then type irie."
		return
	fi
	if [ -n "${IRIE_NO_SETUP:-}" ]; then
		say "Run irie setup to check that this computer has what Irie needs."
		return
	fi
	say ""
	# Its questions need the terminal: this script's own input is curl's.
	if (exec </dev/tty) 2>/dev/null; then
		env -u IRIE_BIN "$linkdir/irie" setup </dev/tty || true
	else
		env -u IRIE_BIN "$linkdir/irie" setup --check || true
	fi
}

pretty() {
	case $1 in
	darwin) echo macOS ;;
	linux) echo Linux ;;
	esac
}

# check fails unless the file's SHA-256 is the one SHA256SUMS gives it.
check() {
	want=$(awk -v f="$1" '$2 == f || $2 == "*" f { print $1 }' "$tmp/SHA256SUMS")
	[ -n "$want" ] || fail "SHA256SUMS has no line for $1"
	got=$(sha256 "$tmp/$1")
	[ "$got" = "$want" ] || fail "$1 is not the file that was released: its SHA-256 is $got, not $want. Nothing was installed"
}

# put moves a new irie into place in one step, so a running Irie and its
# agents keep the one they started with until they restart.
put() {
	mkdir -p "$bindir"
	chmod 755 "$1"
	mv -f "$1" "$bindir/irie.new"
	mv -f "$bindir/irie.new" "$bindir/irie"
}

# link puts irie in ~/.local/bin, where Claude Code's installer puts
# claude too.
link() {
	mkdir -p "$linkdir"
	if [ -e "$linkdir/irie" ] && [ ! -L "$linkdir/irie" ]; then
		mv -f "$linkdir/irie" "$linkdir/irie.old"
		say "An older irie in ~/.local/bin is now irie.old there."
	fi
	ln -sf "$bindir/irie" "$linkdir/irie"
}

# from_source builds irie with Go when a release has no download for
# this computer, as before the first release with downloads.
from_source() {
	if ! command -v go >/dev/null 2>&1; then
		why=$(tail -n 1 "$tmp/why" 2>/dev/null)
		fail "Irie for $(pretty "$os") on $arch couldn't be downloaded from $from${why:+ ($why)}, and building it from source needs Go. Install Go from https://go.dev/dl, then run this again, or see $docs"
	fi
	at=latest
	[ -n "${IRIE_VERSION:-}" ] && at=v${IRIE_VERSION#v}
	say "No download yet, so building it from source with $(go version | cut -d' ' -f3)…"
	GOBIN=$tmp/gobin GOFLAGS=-trimpath go install "github.com/irie-people/irie/cmd/irie@$at" || fail "the build failed. See $docs"
	put "$tmp/gobin/irie"
}

# app puts Irie.app in ~/Applications, or updates it where it is, and
# reopens it when it was open. Its agents run on without it.
app() {
	if [ "$arch" != arm64 ]; then
		say "Irie.app is for Apple silicon for now; on this Mac, type irie in a terminal."
		return
	fi
	zip=Irie-macos-arm64.zip
	if ! fetch "$from/$zip" "$tmp/$zip"; then
		say "This release has no Irie.app; type irie in a terminal."
		return
	fi
	check "$zip"
	ditto -x -k "$tmp/$zip" "$tmp/app"
	[ -d "$tmp/app/Irie.app" ] || fail "$zip has no Irie.app in it"
	apps=$home/Applications
	[ -d /Applications/Irie.app ] && [ -w /Applications ] && apps=/Applications
	mkdir -p "$apps"
	# Only the Irie.app being replaced is closed, and opened again after.
	open=
	if pgrep -f "^$apps/Irie.app/Contents/MacOS/Irie" >/dev/null 2>&1; then
		open=1
		osascript -e "tell application \"$apps/Irie.app\" to quit" >/dev/null 2>&1 || true
		for _ in 1 2 3 4 5 6 7 8 9 10; do
			pgrep -f "^$apps/Irie.app/Contents/MacOS/Irie" >/dev/null 2>&1 || break
			sleep 0.5
		done
	fi
	rm -rf "$apps/Irie.app.old"
	[ -d "$apps/Irie.app" ] && mv "$apps/Irie.app" "$apps/Irie.app.old"
	mv "$tmp/app/Irie.app" "$apps/Irie.app"
	rm -rf "$apps/Irie.app.old"
	# So Spotlight and Launchpad find it at once.
	lsregister=/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister
	[ -x "$lsregister" ] && "$lsregister" -f "$apps/Irie.app" 2>/dev/null || true
	mdimport "$apps/Irie.app" 2>/dev/null || true
	say "Irie.app is in $(echo "$apps" | sed "s#^$home#~#"): open it from Spotlight."
	[ -n "$open" ] && open "$apps/Irie.app"
	return 0
}

main "$@"
