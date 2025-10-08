#!/bin/bash

echo "🔍 调试 incident-worker 初始化问题..."

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=1
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

cd "$(dirname "$0")/.."

echo "📋 环境变量："
echo "  QPROXY_WS_URL=$QPROXY_WS_URL"
echo "  QPROXY_WS_USER=$QPROXY_WS_USER"
echo "  QPROXY_CONV_ROOT=$QPROXY_CONV_ROOT"
echo "  QPROXY_WS_POOL=$QPROXY_WS_POOL"
echo ""

echo "🔍 检查 ttyd 状态："
if ss -tlnp | grep -q ":7682 "; then
    echo "✅ ttyd 正在监听端口 7682"
else
    echo "❌ ttyd 没有监听端口 7682"
    exit 1
fi

echo ""
echo "🔍 检查目录："
ls -la ./conversations/ 2>/dev/null || echo "  conversations 目录不存在"

echo ""
echo "🔨 编译并运行（减少连接池大小到1）："
if go build -o ./bin/incident-worker-test ./cmd/incident-worker; then
    echo "✅ 编译成功"
    echo ""
    echo "▶️  启动测试（按 Ctrl+C 停止）："
    echo "   如果程序立即退出，说明连接池初始化失败"
    echo "   如果程序卡住，说明连接池初始化成功"
    echo ""
    ./bin/incident-worker-test
else
    echo "❌ 编译失败"
    exit 1
fi
