#!/bin/bash

echo "🔍 使用调试版本测试 incident-worker..."

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

echo "🔍 检查 ttyd 状态："
if ss -tlnp | grep -q ":7682 "; then
    echo "✅ ttyd 正在监听端口 7682"
else
    echo "❌ ttyd 没有监听端口 7682"
    exit 1
fi

echo ""
echo "🔨 编译调试版本："
if go build -o ./bin/incident-worker-debug ./cmd/incident-worker-debug; then
    echo "✅ 编译成功"
    echo ""
    echo "▶️  启动调试版本（会显示详细日志）："
    echo "   在另一个终端运行: curl http://127.0.0.1:8080/healthz"
    echo ""
    ./bin/incident-worker-debug
else
    echo "❌ 编译失败"
    exit 1
fi
