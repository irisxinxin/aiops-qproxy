#!/bin/bash

echo "🔍 调试 incident-worker..."

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=3
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

cd "$(dirname "$0")/.."

echo "📋 环境变量："
echo "  QPROXY_WS_URL=$QPROXY_WS_URL"
echo "  QPROXY_WS_USER=$QPROXY_WS_USER"
echo "  QPROXY_CONV_ROOT=$QPROXY_CONV_ROOT"
echo "  QPROXY_HTTP_ADDR=$QPROXY_HTTP_ADDR"
echo ""

echo "🔍 检查依赖："
echo "  Go: $(go version 2>/dev/null || echo '未安装')"
echo "  ttyd: $(ttyd --version 2>/dev/null || echo '未安装')"
echo "  q: $(q --version 2>/dev/null || echo '未安装')"
echo ""

echo "🔍 检查端口："
ss -tlnp | grep -E ":7682|:8080" || echo "  没有相关端口在监听"
echo ""

echo "🔍 检查目录："
ls -la ./conversations/ 2>/dev/null || echo "  conversations 目录不存在"
ls -la ./logs/ 2>/dev/null || echo "  logs 目录不存在"
echo ""

echo "🔨 尝试编译："
if go build -o ./bin/incident-worker ./cmd/incident-worker; then
    echo "✅ 编译成功"
    echo ""
echo "▶️  手动启动 incident-worker（按 Ctrl+C 停止）："
echo "   如果程序立即退出，说明连接失败"
echo "   如果程序卡住，说明连接成功但等待中"
echo ""
./bin/incident-worker
else
    echo "❌ 编译失败"
    exit 1
fi
