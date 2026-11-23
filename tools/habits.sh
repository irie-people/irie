#!/bin/sh
# Ranks what agents did in Irie's journal by how many calls it took them,
# the measure of what a script could take over, since each call reads the
# agent's whole context again (AGENTS.md, "Stack"). It counts a search
# followed by a read of what it found, hand-written waits and the same
# command run again and again, and how much the repository's own tools
# are used, so that a new tool's effect shows in the next run, and what
# failed most, with what the agent did next that worked: the next lines
# worth writing into the instructions agents read.
#
#   tools/habits.sh             the last 14 days
#   tools/habits.sh --days 30   the last 30
#   tools/habits.sh --json      the same as JSON
set -eu
cd "$(git rev-parse --show-toplevel)"
exec go run ./tools/habits "$@"
