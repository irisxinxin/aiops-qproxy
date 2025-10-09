#!/bin/bash
# 测试 sdn5 告警的脚本

set -e

echo "🧪 测试 sdn5 告警..."

# 检查服务状态
echo "📋 检查服务状态..."
# 基于 JSON 的健康检查，重试 30 次
ok=false
for i in $(seq 1 30); do
  code=$(curl -sS -o /tmp/qproxy_hz.$$ -w '%{http_code}' http://127.0.0.1:8080/healthz || true)
  if [ "$code" = "200" ] && grep -q '"ready":[1-9]' /tmp/qproxy_hz.$$; then
    echo "✅ incident-worker 运行正常: $(cat /tmp/qproxy_hz.$$)"
    ok=true
    rm -f /tmp/qproxy_hz.$$
    break
  fi
  sleep 1
done
if [ "$ok" != true ]; then
  echo "❌ incident-worker 未就绪，最后响应: $(cat /tmp/qproxy_hz.$$ 2>/dev/null)"
  rm -f /tmp/qproxy_hz.$$ 2>/dev/null || true
  exit 1
fi

# 测试 sdn5 CPU 告警
echo ""
echo "🚨 测试 sdn5 CPU 告警..."

# 优先从 alerts/dev/sdn5_cpu.json 构造富上下文 Prompt
ALERT_JSON="aiops-qproxy/alerts/dev/sdn5_cpu.json"
[ -f "$ALERT_JSON" ] || ALERT_JSON="./alerts/dev/sdn5_cpu.json"
if command -v jq >/dev/null 2>&1 && [ -f "$ALERT_JSON" ]; then
  status=$(jq -r '.status // empty' "$ALERT_JSON")
  envv=$(jq -r '.env // empty' "$ALERT_JSON")
  region=$(jq -r '.region // empty' "$ALERT_JSON")
  service=$(jq -r '.service // empty' "$ALERT_JSON")
  severity=$(jq -r '.severity // empty' "$ALERT_JSON")
  title=$(jq -r '.title // empty' "$ALERT_JSON")
  window=$(jq -r '.window // empty' "$ALERT_JSON")
  duration=$(jq -r '.duration // empty' "$ALERT_JSON")
  threshold=$(jq -r '.threshold // empty' "$ALERT_JSON")
  current_value=$(jq -r '.metadata.current_value // empty' "$ALERT_JSON")
  group_id=$(jq -r '.metadata.group_id // empty' "$ALERT_JSON")
  expression=$(jq -r '.metadata.expression // empty' "$ALERT_JSON")
  container=$(jq -r '.metadata.container // empty' "$ALERT_JSON")
  pod=$(jq -r '.metadata.pod // empty' "$ALERT_JSON")
  datasource=$(jq -r '.metadata.prometheus // empty' "$ALERT_JSON")

  PROMPT_CN=$(cat <<EOF
你现在是资深 SRE，请对以下告警进行定位与处置，并输出结构化结论（原因、影响范围、SLA/风险、即时处置、根因验证、后续跟进）。\n\n告警上下文：\n- 标题: ${title}\n- 等级/状态: ${severity} / ${status}\n- 环境/区域/服务: ${envv} / ${region} / ${service}\n- 窗口/持续: ${window} / ${duration}\n- 阈值/当前值: ${threshold} / ${current_value}\n- 归组ID: ${group_id}\n- 指标表达式: ${expression}\n- 关键容器/Pod: ${container} / ${pod}\n- 数据源: ${datasource}\n\n请给出：\n1) 可能原因优先级清单（容器/节点/依赖/流量），\n2) 立即可执行的止血步骤（具体命令/系统操作），\n3) 验证与回滚策略，\n4) 监控/容量/告警改进建议。
EOF
  )
else
  PROMPT_CN="sdn5 生产集群 CPU 持续高于阈值，请结合容器/节点/依赖与流量特征进行定位，输出可执行处置与验证方案，并给出结构化结论（原因、影响范围、SLA/风险、处置、后续）。"
fi

RESPONSE=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d "{\"incident_key\":\"v2|prd|sdn5|cpu|thr=0.95|win=5m\",\"prompt\":\"${PROMPT_CN//\"/\\\"}\"}")

echo "响应: $RESPONSE"

# 检查会话文件
echo ""
echo "📁 检查会话文件..."
if [ ! -d "./conversations" ]; then
    echo "❌ conversations 目录不存在"
    exit 1
fi

if [ -f "./conversations/_sopmap.json" ]; then
    echo "✅ SOP 映射文件存在"
    echo "内容:"
    cat ./conversations/_sopmap.json | jq . 2>/dev/null || cat ./conversations/_sopmap.json
else
    echo "ℹ️ SOP 映射文件不存在（首次运行正常）"
fi

echo ""
echo "📊 会话文件列表:"
if ls ./conversations/*.json >/dev/null 2>&1; then
    ls -la ./conversations/*.json
else
    echo "无会话文件（首次运行正常）"
fi

echo ""
echo "🎉 sdn5 告警测试完成！"
echo ""
echo "💡 提示："
echo "  - 查看 ttyd 日志: tail -f ./logs/ttyd-q.log"
echo "  - 查看 incident-worker 日志: tail -f ./logs/incident-worker-real.log"
echo "  - 停止服务: pkill -f 'ttyd.*q chat\|incident-worker'"
