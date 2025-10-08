#!/bin/bash

echo "⏳ 测试 Q CLI 准备时间..."

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
echo "🧪 测试 Q CLI 准备时间..."
echo "建立 WebSocket 连接并等待 Q CLI 准备完成..."

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=1
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

echo "▶️  启动调试版本（等待 Q CLI 准备）..."
echo "   预计需要 30-60 秒..."
echo "   如果看到 'HTTP 服务器启动成功'，说明 Q CLI 准备完成"
echo ""

# 使用调试版本测试
timeout 90s ./bin/incident-worker-debug 2>&1 || echo "程序退出或超时"

echo ""
echo "💡 提示："
echo "  - Q CLI 首次启动需要 30-60 秒准备时间"
echo "  - 如果超时，可能需要检查 AWS 配置或网络连接"
echo "  - 可以尝试重启 ttyd 让 Q CLI 重新初始化"
