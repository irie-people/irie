# When the target is Irie itself

The Irie the person is using holds their work: every agent in it is a task in flight, theirs or someone's.

- **Never touch the live Irie:** not its core, its tmux server, its sidebars, Irie.app, its hosts or any session's agent. Don't stop, freeze, kill, restart or install over any of them. Don't run `irie`, `irie up` or `go run ./cmd/irie` from your checkout inside a session: it finds the live core and can replace it.
- **Attack a test Irie.** Irie's Go tests start an Irie of their own, with its own tmux server, socket and folders (internal/testutil), and stop it after. Write each experiment as a Go test beside the code it attacks: `internal/core/steady_test.go` plays a chaos monkey that way, stopping each of Irie's processes and checking that they come back.
- **Start from DESIGN §2.9,** which lists the threats Irie already handles, with the test for each. Look for what it lacks: a call that can hang with no deadline, a file a crash can leave half-written, a lock two sessions can both take, a restart that loses a row.
- **One package at a time,** such as `tools/check.sh ./internal/core`, while the machine is busy. Gabriel slows sessions the person isn't looking at while it is, which slows your timings too: say so beside any time you report.
- **A finding as a test:** a red test must not reach the branch Irie installs from, since `tools/live.sh` runs every test. Write it with `t.Skip("chaos finding N: …")` until the fix lands, or leave it out and give the steps in your report.
- **The Mac app:** `mac/snapshot.sh DIR` draws its windows into DIR without opening one. Never take the person's focus.
