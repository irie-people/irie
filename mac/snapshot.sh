#!/bin/sh
# Draws Irie.app's windows into DIR from this checkout, without installing
# anything, opening a window or taking focus (docs/MAC.md). It builds the
# way a preview does (build.sh --hot: unoptimized and incremental, with its
# objects kept in build/snapshot, so a run after an edit compiles only the
# files it reaches), then runs each offscreen drawing the app has: the
# window (--snapshot), the conversation view (--render-conversation) and a
# first run (--render-onboarding). It is how to see a change to the views
# before committing it; tools/live.sh builds and installs the real app.
#
#   mac/snapshot.sh DIR
set -eu
dir=${1:?usage: mac/snapshot.sh DIR}
mkdir -p "$dir"
dir=$(cd "$dir" && pwd)
cd "$(dirname "$0")"
# The product's names, from internal/brand (DESIGN §2.10).
eval "$(../tools/brand.sh)"

app="$PWD/build/snapshot/$BRAND_NAME Preview.app"
./build.sh --hot build/snapshot/obj "$app" >&2
# Each drawing exits once its pictures are written, so each runs alone. One
# that fails, such as a title bar that put a button in its overflow menu,
# fails the script once every drawing has run.
failed=
for mode in --snapshot --render-conversation --render-onboarding; do
	"$app/Contents/MacOS/${BRAND_EXE}Preview" "$mode" "$dir" >&2 || failed="$failed $mode"
done
ls "$dir"
if [ -n "$failed" ]; then
	echo "snapshot.sh: failed:$failed" >&2
	exit 1
fi
