#!/bin/bash

echo "🔍 测试连接池自动重连机制..."

cd "$(dirname "$0")/.."

echo "📋 测试参数："
echo "  WS_URL: http://127.0.0.1:7682/ws"
echo "  WS_USER: demo"
echo "  WS_PASS: password123"
echo ""

echo "🔍 检查服务状态："
if ss -tlnp | grep -q ":8080 "; then
    echo "✅ incident-worker 正在运行"
else
    echo "❌ incident-worker 没有运行"
    exit 1
fi

echo ""
echo "🧪 测试多次告警处理（验证自动重连）..."
echo "发送多个 sdn5 告警，测试连接池是否能自动重连..."

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=1
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

echo ""
echo "▶️  发送第一个告警..."
curl -s -X POST http://127.0.0.1:8080/incident \
  -H 'content-type: application/json' \
  -d '{"incident_key":"v2|prd|sdn5|cpu|thr=0.95|win=5m","prompt":"CPU usage is high"}' | jq -r '.answer // "No answer"'

echo ""
echo "⏳ 等待 10 秒..."
sleep 10

echo ""
echo "▶️  发送第二个告警..."
curl -s -X POST http://127.0.0.1:8080/incident \
  -H 'content-type: application/json' \
  -d '{"incident_key":"v2|prd|sdn5|cpu|thr=0.95|win=5m","prompt":"CPU usage is still high"}' | jq -r '.answer // "No answer"'

echo ""
echo "⏳ 等待 10 秒..."
sleep 10

echo ""
echo "▶️  发送第三个告警..."
curl -s -X POST http://127.0.0.1:8080/incident \
  -H 'content-type: application/json' \
  -d '{"incident_key":"v2|prd|sdn5|cpu|thr=0.95|win=5m","prompt":"What should I do about high CPU?"}' | jq -r '.answer // "No answer"'

echo ""
echo "💡 如果所有告警都能成功处理，说明自动重连机制工作正常"
echo "💡 如果某个告警失败，说明需要进一步优化重连机制"
