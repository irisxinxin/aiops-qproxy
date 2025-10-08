#!/bin/bash

echo "🔍 检查 incident-worker 启动失败原因..."

cd "$(dirname "$0")/.."

echo "📝 查看 incident-worker 日志："
if [ -f "./logs/incident-worker-real.log" ]; then
    echo "=== incident-worker 最新日志 ==="
    tail -50 ./logs/incident-worker-real.log
    echo ""
    echo "=== 检查是否有错误 ==="
    if grep -i "error\|fail\|timeout\|broken\|pipe\|connection" ./logs/incident-worker-real.log; then
        echo "❌ 发现错误"
    else
        echo "✅ 没有发现明显错误"
    fi
else
    echo "❌ incident-worker 日志文件不存在"
fi

echo ""
echo "🔍 检查进程状态："
ps aux | grep incident-worker | grep -v grep || echo "  没有 incident-worker 进程"

echo ""
echo "🔍 检查端口状态："
ss -tlnp | grep -E ":7682|:8080" || echo "  没有相关端口在监听"

echo ""
echo "🔍 检查编译时间："
if [ -f "./bin/incident-worker" ]; then
    echo "incident-worker 编译时间："
    ls -la ./bin/incident-worker
    echo ""
    echo "源码修改时间："
    ls -la ./cmd/incident-worker/main.go
    ls -la ./internal/ttyd/wsclient.go
else
    echo "❌ incident-worker 二进制文件不存在"
fi

echo ""
echo "💡 建议："
echo "  1. 如果日志显示超时错误，可能需要重新编译"
echo "  2. 如果进程存在但没有监听端口，可能是初始化失败"
echo "  3. 尝试手动重新编译: go build -o ./bin/incident-worker ./cmd/incident-worker"
