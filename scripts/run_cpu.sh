#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
export NO_COLOR=1
export CLICOLOR=0
export TERM=dumb
export QCTX_DIR="${ROOT_DIR}/ctx"
export QLOG_DIR="${ROOT_DIR}/logs"
export QDATA_DIR="${ROOT_DIR}/data"
export QWORKDIR="${ROOT_DIR}"
RUNNER_BIN="${RUNNER_BIN:-${ROOT_DIR}/bin/qproxy-runner}"
ALERT_FILE="${ROOT_DIR}/alerts/cpu_resolved.json"
if [[ ! -x "$RUNNER_BIN" ]]; then
  echo "Runner not found or not executable: $RUNNER_BIN" >&2
  exit 1
fi
if [[ ! -f "$ALERT_FILE" ]]; then
  echo "Alert file not found: $ALERT_FILE" >&2
  exit 1
fi
echo ">> Running qproxy-runner with CPU resolved alert"
cat "$ALERT_FILE" | "$RUNNER_BIN"
echo ">> done. Check logs/ and data/ctx/"
