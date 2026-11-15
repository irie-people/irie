#!/bin/sh
# Keeps the files that copy Irie's design library in step with it
# (design/README.md).
#
#   tools/design.sh         says which copies are not the library's
#   tools/design.sh sync    writes the library's values into them, and makes
#                           design/web/tokens.css and tokens.js again
set -eu
cd "$(dirname "$0")/.."
exec go run ./tools/design "$@"
