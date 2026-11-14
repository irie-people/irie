# Faults to try

Pick by the kind of thing you test, and try the likeliest first. Each fault says how to cause it on macOS and Linux. Aim each one at your own copy only, and undo it when its experiment ends.

## Inputs

- **Edges:** empty, missing or null; one character; very long, 1 MB or 100 MB; the wrong type; numbers at their edges (0, -1, 2147483648, 9007199254740993, NaN, Infinity, 1e309); dates at theirs (29 February, 31 December 23:59:59, 1970-01-01, 2038-01-19).
- **Text:** emoji, joined emoji such as a family, combining accents, right-to-left text, zero-width spaces, NUL bytes, invalid UTF-8, a 10,000-character word, only spaces, spaces at either end, quotes of every kind.
- **Input it should never trust:** `'; --`, `" OR 1=1 --`, `<script>alert(1)</script>`, `{{7*7}}`, `${7*7}`, `../../etc/passwd`, `$(id)`, `; ls`, `%00`, and a URL to 169.254.169.254 for a feature that fetches what it is given.
- **Files:** empty, cut short, in another format than its name says, huge, with a byte-order mark, with CRLF line ends, a name with spaces, unicode or a leading dash, a symlink to itself or out of the folder, a folder where a file should be.
- **Repeats:** the same request or click twice at once, twice in a row, a hundred times; fields left out; fields it has never heard of.
- **A fuzzer:** Go's `go test -fuzz=FuzzName -fuzztime=60s` keeps each input that fails in `testdata/fuzz/FuzzName/`, where plain `go test` runs it again; Hypothesis in Python and fast-check in JavaScript, when the project has them. Installing one is the person's call.

## Timing and concurrency

- **Two at once:** two clients editing one record, two copies of the program on one data folder, a save racing a delete.
- **Out of order:** a reply that arrives after a newer one, an event before the one it follows, a retry after a timeout that went through after all. Is the work done twice?
- **Cut off mid-way:** `kill -INT`, `kill -TERM` and `kill -KILL` during a write, a migration, an upload or a commit; then start it again and check the data.
- **A stall:** `kill -STOP <pid>`, then `kill -CONT <pid>` some seconds later, freezes a process as a slow disk or a debugger would.
- **Go:** `go test -race -count=50 -cpu=1,4,16 -run TestName`. A test that passes and fails at random is a finding too.
- **Clocks:** another time zone (`TZ=Pacific/Chatham`, `TZ=Asia/Kathmandu`), across a daylight-saving change, on 29 February, in 2038; a fake clock where the code takes one. On Linux, libfaketime when it is installed: `faketime '2038-01-19 03:14:00' <command>`.
- **Locales:** `LC_ALL=tr_TR.UTF-8`, whose i has no dot in capitals, and `LC_ALL=de_DE.UTF-8`, whose decimals take a comma.

## Processes

- **At random moments,** from the seed. In bash, with `PID` a process of the target:

  ```bash
  RANDOM=$SEED
  for i in $(seq 1 20); do
    sleep $((RANDOM % 10 + 1))
    kill -STOP "$PID"; sleep $((RANDOM % 5 + 1)); kill -CONT "$PID"
  done
  ```

  Stop only what you started or the target's own processes (`pgrep -P <pid>` lists a process's children), never one of the person's or the system's.
- **Parents and children:** kill the parent and look for children left holding ports or locks; kill a child and see whether the parent notices.
- **Starts:** start it twice; with its port taken (`python3 -m http.server <port>` holds one); after a crash that left its lock and pid files behind.
- **Restarts:** in the middle of each kind of work, then check that it comes back to the same state.

## Resources

- **A full disk.** On macOS, with no administrator needed, a small disk image that Finder doesn't show:

  ```bash
  hdiutil create -size 20m -fs APFS -volname chaos chaos.dmg
  mkdir -p mnt && hdiutil attach -nobrowse -mountpoint "$PWD/mnt" chaos.dmg
  # point the target's data or temp folder at mnt, then fill it:
  dd if=/dev/zero of=mnt/fill bs=1m count=100   # stops at "No space left on device"
  hdiutil detach mnt
  ```

  On Linux a small tmpfs or loop device needs root: ask, or use the file size limit below. Never fill the real disk.
- **A file too large:** `ulimit -f 1024` before starting the target, in KiB in bash. A write past it fails with "File too large", or SIGXFSZ ends a program that doesn't ignore it.
- **No file handles left:** `ulimit -n 64` before starting it.
- **Read-only or forbidden:** `chmod a-w` its data folder, `chmod 000` one of its files; put them back after.
- **Memory:** on Linux, `ulimit -v <KiB>`. macOS refuses that limit, so feed it big inputs and watch its memory (`ps -o rss= -p <pid>` every few seconds) for growth that never comes back.
- **A starved CPU:** `taskpolicy -b <command>` on macOS, or `taskpolicy -b -p <pid>`, runs it in the background state, slowly; `nice -n 19` on Linux. Busy loops of your own only a few at a time, each with an end.

## Dependencies and the network

- **Down:** stop its database or service. Point it at a closed port, refused at once, and at one that accepts and never answers, a hang, which is worse.
- **Slow, cut off, garbled:** a small proxy of your own between it and the dependency, a few dozen lines, that delays, drops, cuts a reply half-way or answers with noise, from the seed. Toxiproxy does this when it is installed.
- **Errors:** a fake server that answers 500, 502, 429 with Retry-After, 401 after a while as an expired token does, a redirect loop, an empty body, invalid JSON, a huge body.
- **Names:** a host that never resolves (names under `.invalid` never do, by RFC 2606), and TLS errors from a self-signed certificate on your fake.
- **Offline, in a web app:** Playwright's `browserContext.setOffline(true)`, and `browserContext.route(url, handler)` to delay, fail or rewrite the requests you choose.
- macOS shapes traffic only as root (`dnctl`, `pfctl`): leave that to the person.

## Load

- **Ramp up,** against your copy only: 1, then 10, then 100 at once. `ab` comes with macOS: `ab -n 2000 -c 50 http://127.0.0.1:<port>/`. `hey`, `oha`, `wrk` or `k6` when installed, or a small script of your own.
- **Watch** the 50th, 95th and 99th percentile times, the error rate, and memory and open files over time.
- **Soak:** a steady, modest load for 10 to 30 minutes. Memory, files or goroutines that only grow are a leak.
- **Size:** 10,000 rows in a list, a 1 GB file, a folder of 100,000 files; and none at all, as on a first run.

## Saved state

- **Damage its files between runs:** empty one (`: > state.json`), cut one short (`head -c 100`), fill one with noise (`head -c 4096 /dev/urandom`), write an older version's format, delete it, put a folder in its place.
- **Versions:** upgrade with old data, go back with new data, run two versions on one data folder.
- **Undo:** restore a backup; delete something, then take it back.

## User interfaces

- **Headless only:** a browser with no window, never brought to the front.
- **A seeded monkey:** with Playwright, click, type into and scroll random visible elements a few hundred times, chosen by the seed, logging each step so it replays; collect `pageerror` and console errors, failed requests and 5xx answers.
- **Flows:** back, forward and reload in the middle; two tabs on one account; a double click on every button that sends something.
- **Shapes:** phone width (390 by 844), 200% zoom, dark mode, reduced motion, keyboard only, a right-to-left language, long German words.
- **Too much:** paste 100,000 characters; drop a file of the wrong type or size.
- **A Mac app:** drive it without taking the person's focus, and draw its windows to files when it can.

## Agents and model features

- **Orders in what it reads:** text in files, pages, emails and tool output that tells the agent to do something else. It should treat it as data.
- **A model or tool that fails:** a timeout, a 429, an answer that isn't valid JSON, an empty answer, one cut off mid-stream, a huge one, a tool that fails every time.
- **No end:** loops that never stop and costs that only grow. Is there a cap on turns, tokens and money?
- Use a fake model server or recorded answers; never loop against a paid API.
