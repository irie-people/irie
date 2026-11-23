#!/bin/sh
# Fails when a doc names, in backquotes, a repository path that isn't
# there (tools/docpaths, DESIGN §2.12): docs/*.md and the Markdown files
# at the top, but PROVENANCE.md and CHANGELOG.md. A path not built yet
# takes "(planned)" after it, and an example "for example" before it.
# tools/check.sh runs it; it takes a few milliseconds.
#
#   tools/docpaths.sh
set -eu
exec sh "$(dirname "$0")/gotool.sh" docpaths "$@"
