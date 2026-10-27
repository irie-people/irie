#!/usr/bin/env bash
# setup-irie.sh TRUSTED
#
# Builds Irie from the checkout of main at TRUSTED and sets it up on a CI
# runner for one agent (docs/proposals/self-maintaining.md): its own
# directories under $IRIE_CI, the agent workflows' config, and irie on the
# PATH of later steps. Irie is always built from main, never from the pull
# request under review.
set -euo pipefail

trusted=$1
: "${IRIE_CI:?}" "${GITHUB_ENV:?}" "${GITHUB_PATH:?}"
home=$IRIE_CI/home

install -d -m 0700 "$IRIE_CI/run" "$home/config/irie" "$home/state" "$home/data" "$home/cache"
install -d "$IRIE_CI/bin" "$IRIE_CI/context" "$IRIE_CI/out"
go -C "$trusted" build -o "$IRIE_CI/bin/irie" ./cmd/irie
cp "$trusted/.github/irie/config.toml" "$home/config/irie/config.toml"

# The socket lives under XDG_RUNTIME_DIR, which must stay short.
cat >>"$GITHUB_ENV" <<EOF
XDG_CONFIG_HOME=$home/config
XDG_STATE_HOME=$home/state
XDG_DATA_HOME=$home/data
XDG_CACHE_HOME=$home/cache
XDG_RUNTIME_DIR=$IRIE_CI/run
EOF
echo "$IRIE_CI/bin" >>"$GITHUB_PATH"

# Irie refuses a config it cannot read; better here than at the spawn.
XDG_CONFIG_HOME=$home/config "$IRIE_CI/bin/irie" config print >/dev/null
"$IRIE_CI/bin/irie" version
