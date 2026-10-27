#!/usr/bin/env bash
# setup.sh builds a throwaway sandbox for Irie's demo recordings
# (docs/demo/README.md): its own HOME and XDG directories, its own tmux
# sockets, three dummy git repos, and profiles whose agent is the repo's
# fakeagent, so agents change state on cue with no model, no tokens and no
# network. Nothing in the sandbox points back at this checkout.
#
#   docs/demo/setup.sh [TAPE]     make a sandbox and print its root
#   docs/demo/setup.sh down ROOT  stop its tmux servers and delete it
#
# To look around by hand: root=$(docs/demo/setup.sh), then in a new shell
# `source $root/env.sh` and run `irie`.
#
# A tape's "# irie-demo:" lines set ui.sidebar_width and where the captions
# go; its "# chapter:" and "# caption:" lines become the captions.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)

die() { echo "setup.sh: $*" >&2; exit 1; }

# A sandbox lives in /tmp, never under the user's home: its paths stay short
# enough for unix sockets (about 104 bytes on macOS) and name nobody.
is_sandbox() {
	case $1 in
	/tmp/irie-demo.* | /private/tmp/irie-demo.*) [ -f "$1/env.sh" ] && [ -d "$1/home/alice" ] ;;
	*) return 1 ;;
	esac
}

down() {
	local root=$1
	is_sandbox "$root" || die "$root is not a demo sandbox"
	(
		# shellcheck source=/dev/null
		source "$root/env.sh"
		for s in "$root/run/viewer" "$root/run/presenter"; do
			if [ -S "$s" ]; then tmux -S "$s" kill-server 2>/dev/null || true; fi
		done
		# TMUX_TMPDIR points into the sandbox, so this reaches its own server only.
		tmux -L "$DEMO_SOCKET" kill-server 2>/dev/null || true
	)
	rm -rf "$root"
}

# directive prints a tape's "# irie-demo: key = value" setting.
directive() {
	[ -n "$tape" ] || return 0
	sed -n "s/^# irie-demo: $1 = //p" "$tape" | tail -n 1
}

# utf8_locale prints a UTF-8 locale this machine has. grep reads to the end:
# with -q, pipefail would take locale's SIGPIPE for a miss.
utf8_locale() {
	if locale -a 2>/dev/null | grep -ix 'c\.utf-\{0,1\}8' >/dev/null; then echo C.UTF-8; else echo en_US.UTF-8; fi
}

# commit makes a repo's one commit, the same on every run.
commit() {
	git -C "$1" add -A
	GIT_AUTHOR_DATE=2026-10-01T09:00:00Z GIT_COMMITTER_DATE=2026-10-01T09:00:00Z \
		git -C "$1" commit -q -m "$2"
}

make_repos() {
	local code=$HOME/code r
	for r in acme-api acme-web acme-billing; do
		git init -q "$code/$r"
		printf '.env\n.env.local\n.claude/settings.local.json\n' >"$code/$r/.gitignore"
		mkdir -p "$code/$r/.claude"
		printf '{\n  "permissions": { "allow": ["Bash(make test:*)"] }\n}\n' >"$code/$r/.claude/settings.local.json"
	done

	r=$code/acme-api
	printf '# acme-api\n\nThe Acme orders API.\n' >"$r/README.md"
	printf 'module example.com/acme-api\n\ngo 1.22\n' >"$r/go.mod"
	cat >"$r/main.go" <<'EOF'
package main

import "net/http"

func main() {
	http.HandleFunc("/login", login)
	http.HandleFunc("/v1/orders", orders)
	http.ListenAndServe(":8080", nil)
}
EOF
	printf '.env\n' >"$r/.worktreeinclude"
	printf 'DATABASE_URL=postgres://localhost:5432/acme_dev\n' >"$r/.env"
	commit "$r" "Serve orders and login"

	r=$code/acme-web
	mkdir -p "$r/src"
	printf '# acme-web\n\nThe Acme storefront.\n' >"$r/README.md"
	printf '{\n  "name": "acme-web",\n  "private": true,\n  "scripts": { "test": "vitest" }\n}\n' >"$r/package.json"
	printf 'export function SignupButton() {\n  return <button className="signup">Sign up</button>;\n}\n' >"$r/src/SignupButton.tsx"
	printf 'test("checkout totals the cart", () => {});\n' >"$r/src/checkout.test.ts"
	printf '.env.local\n' >"$r/.worktreeinclude"
	printf 'API_URL=http://localhost:8080\n' >"$r/.env.local"
	commit "$r" "Storefront with signup and checkout"

	r=$code/acme-billing
	printf '# acme-billing\n\nInvoices and payment webhooks.\n' >"$r/README.md"
	printf 'def deliver(event):\n    """Send one webhook event."""\n' >"$r/webhooks.py"
	commit "$r" "Deliver payment webhooks"
}

# make_paste writes the big log a tape pastes: 5,000 request lines, one of
# them slow, the same on every run.
make_paste() {
	# Park and Miller's generator stays exact in awk's doubles.
	awk 'BEGIN {
		paths[0] = "/v1/orders"; paths[1] = "/v1/orders/search"; paths[2] = "/login"; paths[3] = "/v1/cart"
		seed = 42
		for (i = 1; i <= 5000; i++) {
			seed = (seed * 16807) % 2147483647
			ms = 4 + seed % 60
			path = paths[int(seed / 7) % 4]
			if (i == 3121) { ms = 4871; path = "/v1/orders/search" }
			printf "2026-10-26T%02d:%02d:%02d.%03dZ INFO acme-api GET %s 200 %dms req=%08x\n", 13 + int(i / 3600), int(i / 60) % 60, i % 60, seed % 1000, path, ms, seed
		}
	}' >"$1"
}

# make_captions turns a tape's "# chapter:" and "# caption:" lines into one
# line per caption: the chapter it opens, if any, a tab, then the caption.
make_captions() {
	if [ -z "$tape" ]; then : >"$1"; return; fi
	awk '
		/^# chapter: / { chapter = substr($0, 12); next }
		/^# caption: / { print chapter "\t" substr($0, 12); chapter = ""; next }
	' "$tape" >"$1"
}

make_presenter() {
	local root=$1 position
	position=$(directive captions)
	# tmux starts a pane's shell through \$SHELL -c, and a bash that is not
	# interactive drops PS1, so the shell sets it again from env.sh.
	cat >"$root/bashrc" <<EOF
# The presenter's shell, written by docs/demo/setup.sh.
source '$root/env.sh'
unset TMUX TMUX_PANE
EOF
	cat >"$root/presenter.conf" <<EOF
# The presenter: a tmux around Irie for the demo recordings, written by
# docs/demo/setup.sh. Its status line is the caption bar. Ctrl+G then a
# letter runs a cue: n shows the next caption, p pastes the big log, d
# detaches Irie's client as closing the window would, e marks the end.
# Irie never sees these keys.
set -g prefix None
set -g status on
set -g status-position ${position:-bottom}
set -g status-interval 1
set -g status-style "bg=#16231D,fg=#F2B544,bold"
set -g status-format[0] "#[align=centre]#{@caption}"
set -g @caption ""
set -g mouse off
set -s escape-time 0
set -g default-terminal tmux-256color
set -as terminal-features ",xterm*:RGB"
set -g default-command "/bin/bash --noprofile --rcfile '$root/bashrc'"
set -g set-titles off
set -g bell-action none
set -g visual-bell off
bind -n C-g switch-client -T cue
bind -T cue n run-shell "'$root/bin/cue' next" \; refresh-client -S
bind -T cue p run-shell "'$root/bin/cue' paste"
bind -T cue d run-shell "'$root/bin/cue' detach"
bind -T cue e run-shell "'$root/bin/cue' end"
EOF
}

make_sandbox() {
	local root home locale tool width
	[ -z "$tape" ] || [ -f "$tape" ] || die "no tape $tape"
	command -v go >/dev/null || die "go is not installed"
	command -v tmux >/dev/null || die "tmux is not installed"
	command -v git >/dev/null || die "git is not installed"

	root=$(mktemp -d /tmp/irie-demo.XXXXXX)
	root=$(cd "$root" && pwd -P) # macOS: /tmp is /private/tmp; Irie and git see real paths
	# Nothing runs in the sandbox yet, so a setup that fails just removes it.
	# The path is expanded now: root is local, and mktemp's names hold no quote.
	trap "rm -rf '$root'" EXIT
	home=$root/home/alice
	mkdir -p "$root/bin" "$root/run" "$root/tmp" "$home/code" "$home/.config/irie"
	chmod 700 "$root/run"

	# -trimpath keeps this checkout's path out of the binaries, and so out
	# of any stack trace that could reach the screen.
	(cd "$repo" && go build -trimpath -o "$root/bin/irie" ./cmd/irie)
	(cd "$repo" && go build -trimpath -o "$root/bin/fakeagent" ./internal/testutil/fakeagent)
	cp "$here/claude" "$here/cue" "$root/bin/"
	cp "$here/cues.txt" "$root/"
	# Tools join the sandbox's PATH as links, so PATH names no user directory.
	for tool in tmux git vhs ttyd ffmpeg ffprobe chromium chromium-browser google-chrome google-chrome-stable; do
		if command -v "$tool" >/dev/null; then ln -s "$(command -v "$tool")" "$root/bin/$tool"; fi
	done

	locale=$(utf8_locale)
	cat >"$root/env.sh" <<EOF
# The demo sandbox's environment, written by docs/demo/setup.sh.
export DEMO_ROOT='$root'
export DEMO_SOCKET='irie-demo-${root##*.}'
export HOME='$home' USER=alice LOGNAME=alice SHELL=/bin/bash
export PATH='$root/bin:/usr/bin:/bin:/usr/sbin:/sbin'
export XDG_CONFIG_HOME='$home/.config' XDG_STATE_HOME='$home/.local/state'
export XDG_DATA_HOME='$home/.local/share' XDG_CACHE_HOME='$home/.cache'
export XDG_RUNTIME_DIR='$root/run' TMUX_TMPDIR='$root/run' TMPDIR='$root/tmp'
export LANG=$locale LC_ALL=$locale TZ=UTC
export COLORTERM=truecolor
export GIT_CONFIG_NOSYSTEM=1
export PS1='alice\$ ' HISTFILE=
export BASH_SILENCE_DEPRECATION_WARNING=1
EOF
	# shellcheck source=/dev/null
	source "$root/env.sh"

	width=$(directive sidebar_width)
	cat >"$home/.config/irie/config.toml" <<EOF
# Irie's demo sandbox, written by docs/demo/setup.sh. The profiles run the
# repo's fakeagent: claude and sonnet through the stand-in, which picks the
# step script for a task from cues.txt; fake takes the script as its task,
# as in +fake prompt,ask,stop. local names a CLI that is not installed.

[ui]
sidebar_width = ${width:-34}

[profiles.claude]
command = "$root/bin/claude"
model = "claude-opus-5-5"
effort = "max"
permission_mode = "auto"

[profiles.sonnet]
command = "$root/bin/claude"
model = "claude-sonnet-5-5"
effort = "high"
permission_mode = "auto"

[profiles.fake]
command = "$root/bin/fakeagent"

# A profile whose CLI is not installed, for a spawn that fails.
[profiles.local]
command = "local-agent"

[repos]
roots = ["~/code"]

[tmux]
socket = "$DEMO_SOCKET"
EOF
	printf '[user]\n\tname = Alice\n\temail = alice@example.com\n[init]\n\tdefaultBranch = main\n' >"$home/.gitconfig"

	make_repos
	make_paste "$root/paste.txt"
	make_captions "$root/captions.tsv"
	make_presenter "$root"
	trap - EXIT
	echo "$root"
}

tape=
case ${1:-} in
down)
	[ $# -eq 2 ] || die "usage: setup.sh down ROOT"
	down "$2"
	;;
-h | --help)
	sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
	;;
*)
	[ $# -le 1 ] || die "usage: setup.sh [TAPE]"
	if [ $# -eq 1 ]; then tape=$(cd "$(dirname "$1")" && pwd)/$(basename "$1"); fi
	make_sandbox
	;;
esac
