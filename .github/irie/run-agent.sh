#!/usr/bin/env bash
# run-agent.sh PROFILE DIR NAME TASK MINUTES
#
# Runs one agent through Irie on a CI runner and waits for it to exit
# (docs/proposals/self-maintaining.md). `irie ctl spawn --wait` fails at once
# if the agent never comes up. irie ctl has no wait-until-done yet, so the
# loop polls `irie ctl list` until Irie has the agent's exit code from its
# pane host. The session's events stream into the job log meanwhile.
#
# Exits with the agent's code; 1 if it never came up, which irie ctl spawn
# reports with the last lines of its screen, or if Irie lost the session; or
# 124 when MINUTES pass first. An agent still running then stops when the job
# ends, because the runner ends every process the job started.
set -euo pipefail

profile=$1 dir=$2 name=$3 task=$4 minutes=$5

id=$(irie ctl spawn --wait --cwd "$dir" --name "$name" "+$profile" - <"$task")
echo "irie: $name is session $id (+$profile) in $dir"
irie ctl watch --session "$id" &
watcher=$!
trap 'kill "$watcher" 2>/dev/null || true' EXIT

# row prints the session's state, its exit code or -, and its reason or -.
row() {
	irie ctl list --json | python3 -c '
import json, sys
for s in json.load(sys.stdin)["sessions"]:
    if s["id"] == sys.argv[1]:
        code = s.get("exit_code")
        print(s["state"], "-" if code is None else code, s.get("reason") or "-")
        break
else:
    print("gone - -")
' "$id"
}

deadline=$((SECONDS + minutes * 60))
waited=0 said=
while :; do
	if ! read -r state code reason < <(row); then
		state=unknown code=- reason=-
	fi
	case $state in
	failed)
		echo "irie: $name failed to start (exit $code, $reason)" >&2
		irie ctl list --json | python3 -c '
import json, sys
for s in json.load(sys.stdin)["sessions"]:
    if s["id"] == sys.argv[1]:
        print("\n".join(s.get("tail") or []))
' "$id" >&2 || true
		[ "$code" != - ] && [ "$code" != 0 ] || code=1
		exit "$code"
		;;
	exited)
		if [ "$code" != - ]; then
			echo "irie: $name exited with $code"
			exit "$code"
		fi
		# A SessionEnd hook can mark the session exited before its pane dies.
		waited=$((waited + 1))
		if [ "$waited" -ge 12 ]; then
			echo "irie: $name ended, but Irie never got its exit code" >&2
			exit 1
		fi
		;;
	gone)
		echo "irie: session $id is gone" >&2
		exit 1
		;;
	needs_you | stalled)
		# Nobody can answer on a runner. A failed turn, such as a dry key,
		# shows here first; the agent then exits on its own.
		if [ "$state $reason" != "$said" ]; then
			echo "irie: $name is $state: $reason"
			said="$state $reason"
		fi
		;;
	esac
	if [ "$SECONDS" -ge "$deadline" ]; then
		echo "irie: $name ran past $minutes minutes" >&2
		exit 124
	fi
	sleep 5
done
