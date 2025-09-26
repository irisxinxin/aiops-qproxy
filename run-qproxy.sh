#!/usr/bin/env bash
set -euo pipefail

# --- 进入仓库根目录 ---
cd "$(dirname "$0")"

# --- 载入用户自定义环境（如有）---
if [[ -f .env.local ]]; then
  # shellcheck disable=SC1091
  source .env.local
fi

# --- 默认路径（若用户未覆盖）---
QWORKDIR="${QWORKDIR:-$PWD}"
QCTX_DIR="${QCTX_DIR:-$PWD/ctx}"
QLOG_DIR="${QLOG_DIR:-$PWD/logs}"
QDATA_DIR="${QDATA_DIR:-$PWD/data}"
BIN="${BIN:-$PWD/bin/qproxy-runner}"
ALERT_FILE="${ALERT_FILE:-}"

# --- 关闭 ANSI/颜色（保持输出可解析）---
export NO_COLOR="${NO_COLOR:-1}"
export CLICOLOR="${CLICOLOR:-0}"
export TERM="${TERM:-dumb}"

# --- 确保目录存在（不破坏你已有 logs/ctx）---
mkdir -p "$QCTX_DIR" "$QLOG_DIR" "$QDATA_DIR" "$PWD/bin" "$PWD/scripts"

# --- 选择 Q_BIN：优先用户配置 -> which q -> mock ---
if [[ -z "${Q_BIN:-}" ]]; then
  if command -v q >/dev/null 2>&1; then
    Q_BIN="$(command -v q)"
  else
    Q_BIN="$PWD/scripts/mock-q"
  fi
fi
export Q_BIN QCTX_DIR QLOG_DIR QDATA_DIR QWORKDIR

# --- 自动构建 runner（若缺失）---
if [[ ! -x "$BIN" ]]; then
  echo ">>> building $BIN"
  go mod tidy
  go build -o "$BIN" ./cmd/runner
fi

# --- 使用方式 ---
usage() {
  cat <<'USAGE'
用法:
  1) 从文件喂入：
     ALERT_FILE=./alerts/latency_firing.json ./run-qproxy.sh

  2) 或者 stdin 管道：
     cat ./alerts/latency_firing.json | ./run-qproxy.sh

  3) 指定 Q_BIN（可在 .env.local 中设置，或临时环境变量）：
     Q_BIN=/usr/local/bin/q ALERT_FILE=./alerts/latency_firing.json ./run-qproxy.sh

说明:
  - 默认读取 ALERT_FILE；未设置则从 stdin 读取。
  - 输出日志写入 $QLOG_DIR/qio-*.jsonl
  - 可复用 context 写入 $QCTX_DIR/reusable/<fingerprint>/final_ctx.md
  - 中间/历史数据写入 $QDATA_DIR
USAGE
}

# --- 确定输入来源 ---
if [[ -t 0 && -z "$ALERT_FILE" ]]; then
  usage
  echo
  echo "提示：未检测到 stdin，且未设置 ALERT_FILE"
  exit 1
fi

# --- 开始执行 ---
echo "== ENV =="
echo "Q_BIN     = $Q_BIN"
echo "QCTX_DIR  = $QCTX_DIR"
echo "QLOG_DIR  = $QLOG_DIR"
echo "QDATA_DIR = $QDATA_DIR"
echo "QWORKDIR  = $QWORKDIR"
echo "BIN       = $BIN"
echo

set -o pipefail
if [[ -n "$ALERT_FILE" ]]; then
  if [[ ! -f "$ALERT_FILE" ]]; then
    echo "ERR: ALERT_FILE 不存在: $ALERT_FILE" >&2
    exit 2
  fi
  cat "$ALERT_FILE" | "$BIN"
else
  # 从 stdin 读
  cat - | "$BIN"
fi

echo
echo "== 结果预览 =="
echo "-- logs --"
ls -l "$QLOG_DIR" || true
echo
echo "-- ctx  --"
find "$QCTX_DIR" -maxdepth 3 -type f -print || true
