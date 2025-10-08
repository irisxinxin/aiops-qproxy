#!/bin/bash

echo "🔍 测试 Q CLI 完整初始化过程..."

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
echo "🧪 测试 Q CLI 完整初始化..."
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

echo "▶️  测试 Q CLI 完整初始化（发送 q chat 命令并等待初始化）..."
echo "   预计需要 60-90 秒完成初始化"
echo "   如果成功，应该能看到连接池初始化成功"
echo ""

# 使用调试版本测试
timeout 90s ./bin/incident-worker-debug 2>&1 || echo "程序退出或超时"

echo ""
echo "💡 Q CLI 初始化过程："
echo "  1. ttyd 启动 q chat 命令"
echo "  2. Q CLI 连接 Amazon Q 服务（需要时间）"
echo "  3. 初始化完成后发送提示符"
echo "  4. 可以开始交互"
echo ""
echo "💡 如果超时，可能需要："
echo "  - 检查 AWS 配置和网络连接"
echo "  - 检查 Q CLI 权限"
echo "  - 增加超时时间"
