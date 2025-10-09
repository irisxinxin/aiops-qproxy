#!/bin/bash

echo "🔧 诊断连接池初始化问题..."
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
echo "🧪 测试 incident-worker 启动（详细日志）..."
echo "设置环境变量并启动服务..."

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=1
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

# 启动服务
echo "▶️  启动 incident-worker（显示详细日志）..."
./bin/incident-worker