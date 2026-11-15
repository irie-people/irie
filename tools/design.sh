#!/bin/sh
# Keeps Irie's copies of its design library's values in step with the
# library, Roots: github.com/irie-people/roots (internal/designcheck, DESIGN §2.11).
#
#   tools/design.sh         says which copies are not the library's
#   tools/design.sh sync    writes the library's values into them, and the
#                           site's themes page's tables
#
# A new value comes from the library with
#   go get github.com/irie-people/roots@main
set -eu
cd "$(dirname "$0")/.."
exec go run ./tools/design "$@"
