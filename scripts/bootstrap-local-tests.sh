#!/usr/bin/env bash
# scripts/bootstrap-local-tests.sh
# 本地环境一键准备 & 快速测试（不依赖真实 q，可用内置 mock-q）
set -euo pipefail

# -------- config --------
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN="$ROOT/bin/qproxy-runner"
ALERTS_DIR="$ROOT/alerts/dev"
CTX_DIR="$ROOT/ctx"
DATA_DIR="$ROOT/data"
LOGS_DIR="$ROOT/logs"
MOCK_Q="$ROOT/scripts/mock-q"
FALLBACK_CTX="$ROOT/ctx/sop-fallback/fallback_ctx.jsonl"   # 若你已导入兜底 ctx 包，这里会自动启用

# -------- helpers --------
msg() { echo "[bootstrap] $*"; }
die() { echo "[bootstrap][ERR] $*" >&2; exit 1; }

ensure_dirs() {
  mkdir -p "$ALERTS_DIR" "$CTX_DIR" "$DATA_DIR" "$LOGS_DIR"
}

ensure_mock_q() {
  if [[ -x "$MOCK_Q" ]]; then return 0; fi
  msg "写入 mock-q（用于本地不安装 q 也能跑） -> $MOCK_Q"
  cat >"$MOCK_Q" <<'EOF'
#!/usr/bin/env bash
# 这个 mock 只输出一个简化的 JSON 归因结果到 stdout，模拟 q 的“可解析”结果
set -euo pipefail
# 读取 stdin（runner 会把 alert/上下文塞给 q）
cat >/dev/null
# 输出一个简洁 JSON，runner 的清洗器会吃到它
cat <<'JSON'
{
  "root_cause": "mocked-analysis: transient spike / warmup / rollout related",
  "signals": [
    {"metric":"latency_p95","window":"5m","pattern":"spike_then_recover"}
  ],
  "confidence": 0.85,
  "next_checks": [
    "check rollout window",
    "check HPA warmup events",
    "verify error-rate baseline"
  ],
  "sop_link": "ctx://sop-fallback"
}
JSON
EOF
  chmod +x "$MOCK_Q"
}

ensure_examples() {
  # 生成几份可直接 cat | runner 的告警样例（region 改为 dev-nbu-aps1）
  # 1) latency firing
  cat >"$ALERTS_DIR/latency_firing.json" <<'JSON'
{
  "status":"firing",
  "env":"prd",
  "region":"dev-nbu-aps1",
  "service":"omada-central",
  "category":"latency",
  "severity":"critical",
  "title":"omada central account related interfaces average latency is too high",
  "group_id":"omada-central_critical",
  "method":"POST",
  "path":"/api/v1/central/account/accept-batch-invite",
  "threshold":"3.0",
  "window":"5m",
  "duration":"120s",
  "metadata":{
    "alertgroup":"omada-central",
    "auto_create_group":false,
    "comparison":">",
    "datasource_cluster":"dev-nbu-aps1",
    "department":"[ERD|Networking Solutions|Network Services]",
    "expression":"( sum(rate(omada_rest_dispatcher_requests_seconds_sum{namespace=\"omada-central\",application=\"omada-central\",path=~\"/api/v1/central/account/.*\", path!=\"/api/v1/central/account/accept-invite\", err_code!=\"-1\"}[5m])) by(path,method) / (sum(rate(omada_rest_dispatcher_requests_seconds_count{namespace=\"omada-central\",application=\"omada-central\",path=~\"/api/v1/central/account/.*\", path!=\"/api/v1/central/account/accept-invite\", err_code!=\"-1\"}[5m])) by(path,method) > 0) )>3.0",
    "prometheus":"monitoring/kps-prometheus",
    "service_name":"omada-central",
    "tel_up":"30m",
    "threshold_value":3
  }
}
JSON

  # 2) cpu resolved
  cat >"$ALERTS_DIR/cpu_resolved.json" <<'JSON'
{
  "status":"resolved",
  "env":"prd",
  "region":"dev-nbu-aps1",
  "service":"omada-essential",
  "category":"cpu",
  "severity":"critical",
  "title":"omada essential cpu usage rate is too high",
  "group_id":"omada-essential_critical",
  "claimedBy":null,
  "metadata":{
    "alertname":"omada essential cpu usage rate is too high",
    "raw_title":"[resolved][dev-nbu-aps1]omada essential cpu usage rate is too high"
  }
}
JSON

  # 3) login rate limit（作为兜底场景）
  cat >"$ALERTS_DIR/login_rate_limit.json" <<'JSON'
{
  "status":"firing",
  "env":"prd",
  "region":"dev-nbu-aps1",
  "service":"omada-iam",
  "category":"login_rate_limit",
  "severity":"warning",
  "title":"omada iam login rate limit times surged",
  "metadata":{
    "metric":"omada_iam_login_rate_limit_times_total",
    "window":"5m",
    "comparison":">",
    "threshold_value":10
  }
}
JSON
}

build_if_needed() {
  if [[ -x "$BIN" ]]; then
    msg "已存在二进制：$BIN"
    return 0
  fi
  command -v go >/dev/null 2>&1 || die "本机未安装 Go，且 bin 不存在，无法自动构建。"
  msg "构建 runner -> $BIN"
  mkdir -p "$ROOT/bin"
  (cd "$ROOT" && go build -o "$BIN" ./cmd/runner)
}

export_envs() {
  # 让 runner 最终调用哪个 Q：
  if [[ "${Q_BIN:-}" == "" ]]; then
    if command -v q >/dev/null 2>&1; then
      export Q_BIN="$(command -v q)"
      msg "检测到本机 q：Q_BIN=$Q_BIN"
    else
      ensure_mock_q
      export Q_BIN="$MOCK_Q"
      msg "未检测到 q，启用内置 mock：Q_BIN=$Q_BIN"
    fi
  else
    msg "使用用户指定 Q_BIN=$Q_BIN"
  fi

  # 控制台 ANSI 关闭，避免控制码污染
  export NO_COLOR=1
  export CLICOLOR=0
  export TERM=dumb

  # 让 runner 知道 ctx/data/logs 路径
  export QCTX_DIR="$CTX_DIR"
  export QDATA_DIR="$DATA_DIR"
  export QLOG_DIR="$LOGS_DIR"
  export QWORKDIR="$ROOT"

  # 兜底 ctx（如果你已把 fallback_ctx 放到 ctx/sop-fallback 下，会自动启用）
  if [[ -f "$FALLBACK_CTX" ]]; then
    export Q_FALLBACK_CTX="$FALLBACK_CTX"
    msg "启用兜底 ctx：Q_FALLBACK_CTX=$Q_FALLBACK_CTX"
  else
    msg "未发现 $FALLBACK_CTX（可选），跳过兜底 ctx"
  fi
}

run_one() {
  local name="$1"
  local file="$ALERTS_DIR/${name}.json"
  [[ -f "$file" ]] || die "找不到告警文件: $file"
  msg "运行用例: $name"
  # runner 从 stdin 读告警
  cat "$file" | "$BIN"
}

usage() {
  cat <<EOF
用法:
  $0 up             # 一键准备目录/样例/环境变量，构建 runner（若不存在）
  $0 run latency    # 运行 latency_firing.json
  $0 run cpu        # 运行 cpu_resolved.json
  $0 run login      # 运行 login_rate_limit.json
  $0 env            # 打印关键环境变量
  $0 clean-logs     # 清空 logs/ 下的调试日志
说明:
  - 默认会用内置 mock-q；若机器已安装 Amazon Q CLI，则自动使用真实 q。
  - 运行完成后，可查看:
      ctx/     下是否新增了可复用 context（命中去重策略后）
      logs/    下是否生成清洗后的 stdout/stderr JSON（仅用于 debug）
      data/    下是否记录了命中记录/历史归因（按你的 runner 实现）
EOF
}

# -------- main --------
cmd="${1:-}"
case "$cmd" in
  up)
    ensure_dirs
    ensure_examples
    build_if_needed
    export_envs
    msg "OK，本地准备就绪。可执行示例："
    echo "  $0 run latency"
    echo "  $0 run cpu"
    echo "  $0 run login"
    ;;
  run)
    ensure_dirs
    build_if_needed
    export_envs
    which_case="${2:-}"
    case "$which_case" in
      latency) run_one "latency_firing" ;;
      cpu)     run_one "cpu_resolved" ;;
      login)   run_one "login_rate_limit" ;;
      *) die "未知用例: $which_case（支持 latency|cpu|login）" ;;
    esac
    ;;
  env)
    export_envs
    env | grep -E '^(Q_BIN|QCTX_DIR|QDATA_DIR|QLOG_DIR|QWORKDIR|Q_FALLBACK_CTX|NO_COLOR|CLICOLOR|TERM)=' | sort
    ;;
  clean-logs)
    rm -f "$LOGS_DIR"/* || true
    msg "已清空 $LOGS_DIR"
    ;;
  ""|-h|--help|help)
    usage
    ;;
  *)
    usage; die "未知命令: $cmd"
    ;;
esac
