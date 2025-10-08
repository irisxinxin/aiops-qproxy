#!/bin/bash

echo "🔍 调试 incident-worker 启动失败..."

cd "$(dirname "$0")/.."

echo "📝 查看 incident-worker 日志："
if [ -f "./logs/incident-worker-real.log" ]; then
    echo "=== 最新日志 ==="
    tail -50 ./logs/incident-worker-real.log
else
    echo "❌ 日志文件不存在"
fi

echo ""
echo "🔍 检查进程状态："
ps aux | grep incident-worker | grep -v grep

echo ""
echo "🔍 检查端口状态："
ss -tlnp | grep -E ":7682|:8080"

echo ""
echo "🔍 手动测试连接池初始化："
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=1
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

echo "▶️  手动启动调试版本（30秒后自动停止）："
echo "   如果30秒内没有看到 'HTTP 服务器启动成功'，说明连接池初始化失败"
timeout 30s ./bin/incident-worker-debug 2>&1 || echo "程序退出或超时"
