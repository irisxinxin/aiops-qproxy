#!/bin/bash

echo "🔧 测试修复后的 incident-worker..."
echo "📋 检查服务状态..."

# 检查 ttyd 是否运行
if ! pgrep -f "ttyd.*q chat" > /dev/null; then
    echo "❌ ttyd 未运行，请先运行: ./scripts/deploy-real-q.sh"
    exit 1
fi

echo "✅ ttyd 正在运行"

# 检查端口
echo "🔍 检查端口状态："
ss -tlnp | grep -E ":(7682|8080)"

echo ""
echo "🧪 测试修复后的 incident-worker..."
echo "设置环境变量并启动服务..."

# 设置环境变量
export QPROXY_WS_URL=ws://127.0.0.1:7682/ws
export QPROXY_WS_POOL=1
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

echo "环境变量："
echo "  QPROXY_WS_URL=$QPROXY_WS_URL"
echo "  QPROXY_WS_POOL=$QPROXY_WS_POOL"

echo ""
echo "▶️  启动 incident-worker（显示详细日志）..."
echo "等待 60 秒观察连接过程..."
echo ""

# 启动服务并等待
timeout 60 ./bin/incident-worker 2>&1 | head -50

echo ""
echo "📝 如果看到 'ttyd: received data:' 和 'ttyd: prompt detected'，说明修复成功"
echo "📝 如果还是卡在 'ttyd: waiting for initial prompt...'，说明还有其他问题"
