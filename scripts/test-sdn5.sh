#!/bin/bash
# 测试 sdn5 告警的脚本

set -e

echo "🧪 测试 sdn5 告警..."

# 检查服务状态
echo "📋 检查服务状态..."
if ! curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "❌ incident-worker 未运行，请先运行 deploy-real-q.sh"
    exit 1
fi

echo "✅ incident-worker 运行正常"

# 测试 sdn5 CPU 告警
echo ""
echo "🚨 测试 sdn5 CPU 告警..."
RESPONSE=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"v2|prd|sdn5|cpu|thr=0.95|win=5m","prompt":"CPU usage is 95%, please analyze and provide solutions."}')

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
