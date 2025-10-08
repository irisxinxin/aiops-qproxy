#!/bin/bash

echo "🔍 调试 incident-worker 启动后立即崩溃问题..."

cd "$(dirname "$0")/.."

echo "📝 查看 incident-worker 日志："
if [ -f "./logs/incident-worker-real.log" ]; then
    echo "=== incident-worker 日志 ==="
    cat ./logs/incident-worker-real.log
    echo ""
    echo "日志文件大小："
    ls -la ./logs/incident-worker-real.log
else
    echo "❌ 日志文件不存在"
fi

echo ""
echo "🔍 检查进程状态："
ps aux | grep incident-worker | grep -v grep || echo "  没有 incident-worker 进程"

echo ""
echo "🔍 检查端口状态："
ss -tlnp | grep -E ":7682|:8080" || echo "  没有相关端口在监听"

echo ""
echo "🔍 手动测试 incident-worker："
echo "设置环境变量并手动启动..."

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=1
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

echo "环境变量："
echo "  QPROXY_WS_URL=$QPROXY_WS_URL"
echo "  QPROXY_WS_USER=$QPROXY_WS_USER"
echo "  QPROXY_CONV_ROOT=$QPROXY_CONV_ROOT"
echo "  QPROXY_WS_POOL=$QPROXY_WS_POOL"
echo ""

echo "▶️  手动启动 incident-worker（会显示实时输出）："
echo "   如果程序立即退出，说明有编译或运行时错误"
echo "   如果程序卡住，说明连接池初始化中"
echo ""

# 直接运行，不重定向到日志文件
./bin/incident-worker
