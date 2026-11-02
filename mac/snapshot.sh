#!/bin/sh
# Draws Irie.app's windows into DIR from this checkout, without installing
# anything or taking focus (docs/MAC.md): a quick unoptimized build
# (build.sh --quick), then each offscreen drawing the app has: the window
# (--snapshot), the conversation view (--render-conversation) and a first
# run (--render-onboarding). It is how to see a change to the views before
# committing it; tools/live.sh builds and installs the real app.
#
#   mac/snapshot.sh DIR
set -eu
dir=${1:?usage: mac/snapshot.sh DIR}
mkdir -p "$dir"
dir=$(cd "$dir" && pwd)
cd "$(dirname "$0")"

./build.sh --quick >&2
bin=build/quick/Irie.app/Contents/MacOS/Irie
# Each drawing exits once its pictures are written, so each runs alone.
for mode in --snapshot --render-conversation --render-onboarding; do
	"$bin" "$mode" "$dir" >&2
done
ls "$dir"
