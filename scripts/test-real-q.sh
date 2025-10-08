#!/bin/bash
# 测试真实 Q CLI 环境的脚本

set -e

echo "🧪 测试真实 Q CLI 环境..."

# 检查服务状态
echo "📋 检查服务状态..."
if ! curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "❌ incident-worker 未运行，请先运行 deploy-real-q.sh"
    exit 1
fi

echo "✅ incident-worker 运行正常"

# 测试不同类型的告警
echo ""
echo "🚨 测试告警处理..."

# 1. CPU 告警
echo "1️⃣ 测试 CPU 告警..."
RESPONSE1=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"v2|prd|omada-central|cpu|thr=0.85|win=5m","prompt":"CPU usage is 89%, please analyze and provide solutions."}')
echo "响应: $RESPONSE1"

# 2. 内存告警
echo ""
echo "2️⃣ 测试内存告警..."
RESPONSE2=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"v2|prd|vms-ai-manager|memory|thr=0.8|win=10m","prompt":"Memory usage is 87%, check for memory leaks."}')
echo "响应: $RESPONSE2"

# 3. 延迟告警
echo ""
echo "3️⃣ 测试延迟告警..."
RESPONSE3=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"v2|prd|omada-api-gateway|latency|thr=500ms|win=3m","prompt":"API latency is 750ms, analyze performance issues."}')
echo "响应: $RESPONSE3"

# 检查会话文件
echo ""
echo "📁 检查会话文件..."
if [ ! -d "./conversations" ]; then
    echo "❌ conversations 目录不存在，请先运行 deploy-real-q.sh"
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
echo "🎉 测试完成！"
echo ""
echo "💡 提示："
echo "  - 查看 ttyd 日志: tail -f ./logs/ttyd-q.log"
echo "  - 查看 incident-worker 日志: tail -f ./logs/incident-worker-real.log"
echo "  - 停止服务: pkill -f 'ttyd.*q chat\|incident-worker'"
