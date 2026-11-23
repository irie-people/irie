#!/bin/sh
# Holds each package's test coverage to its floor in
# tools/coverage/floor.txt, a floor that only rises (tools/coverage,
# DESIGN §2.12). It reads what tools/test.sh --coverout=FILE measured: a
# package under its floor fails; a package whose coverage rose a point
# gets a higher floor, and a new package its first, which it writes for
# the change to commit. Never lower a floor to let a change through: bring
# tests. tools/check.sh runs it after the tests pass.
#
#   tools/coverage.sh FILE
set -eu
# The program runs from the top of the repository.
case ${1:-} in /* | '' | -*) ;; *) set -- "$PWD/$1" ;; esac
exec sh "$(dirname "$0")/gotool.sh" coverage "$@"
