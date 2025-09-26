#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"
rm -f "$HERE/ctx/".txt "$HERE/ctx/".md "$HERE/logs/"*.json 2>/dev/null || true
mkdir -p "$HERE/ctx" "$HERE/logs"
printf "State cleaned.\n"
