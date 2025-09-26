
#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/bin/qproxy-runner"

export QWORKDIR="${QWORKDIR:-$ROOT}"
export QCTX_DIR="${QCTX_DIR:-$ROOT/ctx}"
export QLOG_DIR="${QLOG_DIR:-$ROOT/logs}"
export QDATA_DIR="${QDATA_DIR:-$ROOT/data}"
export Q_BIN="${Q_BIN:-/usr/local/bin/q}"

mkdir -p "$ROOT/bin" "$QCTX_DIR" "$QLOG_DIR" "$QDATA_DIR/ctx"

cmd="${1:-}"
shift || true

build() {
  echo ">>> building $BIN"
  (cd "$ROOT" && go mod tidy && go build -o "$BIN" ./cmd/runner)
  echo ">>> built $BIN"
}

run() {
  "$BIN" "$@"
}

case "${cmd}" in
  build) build;;
  run) build; run "$@";;
  *) echo "usage: $0 {build|run -- -alert alert.json -meta meta.json [-final_ctx ctx.txt]}"; exit 1;;
esac
