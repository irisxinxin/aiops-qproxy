#!/bin/bash

echo "🔍 测试发送 q chat 命令启动 Q CLI..."

cd "$(dirname "$0")/.."

echo "📋 测试参数："
echo "  WS_URL: http://127.0.0.1:7682/ws"
echo "  WS_USER: demo"
echo "  WS_PASS: password123"
echo ""

echo "🔍 检查 ttyd 状态："
if ss -tlnp | grep -q ":7682 "; then
    echo "✅ ttyd 正在监听端口 7682"
else
    echo "❌ ttyd 没有监听端口 7682"
    exit 1
fi

echo ""
echo "🧪 测试新的 q chat 策略..."
echo "设置环境变量并测试 incident-worker..."

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=1
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

echo "▶️  测试新的 q chat 策略（主动发送 q chat 命令）..."
echo "   如果成功，应该能看到连接池初始化成功"
echo ""

# 使用调试版本测试
timeout 60s ./bin/incident-worker-debug 2>&1 || echo "程序退出或超时"

echo ""
echo "💡 新策略说明："
echo "  - 连接 WebSocket 后主动发送 'q chat' 命令"
echo "  - 启动 Q CLI 的交互模式"
echo "  - 等待 Q CLI 发送提示符或响应"
echo "  - 如果失败，继续使用连接（不关闭）"
