#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"
export Q_BIN="${Q_BIN:-$HERE/scripts/mock-q}"
export QCTX_DIR="${QCTX_DIR:-$HERE/ctx}"
export QLOG_DIR="${QLOG_DIR:-$HERE/logs}"
export QWORKDIR="${QWORKDIR:-$HERE}"
export NO_COLOR=1
export CLICOLOR=0
export TERM=dumb
BIN="$HERE/bin/qproxy-runner"
if [ ! -x "$BIN" ]; then
printf "ERROR: %s not found or not executable. Build first: go build -o bin/qproxy-runner ./cmd/runner\n" "$BIN" >&2
exit 1
fi
ALERT="${1:-$HERE/alerts/latency_firing.json}"
printf ">> Running qproxy-runner with %s (mock q)\n" "$ALERT"
cat "$ALERT" | "$BIN"
