#!/bin/bash

echo "🔧 手动测试 incident-worker 启动..."
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
echo "🧪 手动启动 incident-worker（显示详细日志）..."
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
echo "  QPROXY_CONV_ROOT=$QPROXY_CONV_ROOT"
echo "  QPROXY_WS_INSECURE_TLS=$QPROXY_WS_INSECURE_TLS"

echo ""
echo "▶️  启动 incident-worker（会显示实时日志）..."
echo "按 Ctrl+C 停止"
echo ""

# 直接运行，显示实时日志
./bin/incident-worker
