#!/bin/bash

echo "🔍 诊断 broken pipe 错误..."

cd "$(dirname "$0")/.."

echo "📝 查看 incident-worker 日志："
if [ -f "./logs/incident-worker-real.log" ]; then
    echo "=== incident-worker 最新日志 ==="
    tail -50 ./logs/incident-worker-real.log
    echo ""
    echo "=== 检查是否有连接错误 ==="
    if grep -i "error\|fail\|timeout\|broken\|pipe\|connection" ./logs/incident-worker-real.log; then
        echo "❌ 发现连接错误"
    else
        echo "✅ 没有发现明显错误"
    fi
else
    echo "❌ incident-worker 日志文件不存在"
fi

echo ""
echo "📝 查看 ttyd 日志："
if [ -f "./logs/ttyd-q.log" ]; then
    echo "=== ttyd 最新日志 ==="
    tail -30 ./logs/ttyd-q.log
    echo ""
    echo "=== 检查是否有连接断开 ==="
    if grep -i "closed\|disconnect\|error" ./logs/ttyd-q.log; then
        echo "❌ 发现连接断开"
    else
        echo "✅ 没有发现明显错误"
    fi
else
    echo "❌ ttyd 日志文件不存在"
fi

echo ""
echo "🔍 检查服务状态："
echo "ttyd 进程："
ps aux | grep ttyd | grep -v grep || echo "  没有 ttyd 进程"
echo ""
echo "incident-worker 进程："
ps aux | grep incident-worker | grep -v grep || echo "  没有 incident-worker 进程"
echo ""
echo "端口状态："
ss -tlnp | grep -E ":7682|:8080" || echo "  没有相关端口在监听"

echo ""
echo "🧪 测试 WebSocket 连接："
echo "尝试建立 WebSocket 连接..."
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
     -H "Authorization: Basic $(echo -n "demo:password123" | base64)" \
     http://127.0.0.1:7682/ws &
CURL_PID=$!

sleep 5
echo "等待 5 秒后检查连接状态..."

if ps -p $CURL_PID > /dev/null; then
    echo "✅ WebSocket 连接保持正常"
    kill $CURL_PID 2>/dev/null
else
    echo "❌ WebSocket 连接断开"
fi

echo ""
echo "💡 可能的原因："
echo "  1. Q CLI 连接超时或断开"
echo "  2. WebSocket 连接池中的连接失效"
echo "  3. ttyd 进程重启或崩溃"
echo "  4. 网络问题或资源不足"
echo ""
echo "💡 建议："
echo "  1. 重启服务: ./scripts/deploy-real-q.sh"
echo "  2. 检查 Q CLI 状态"
echo "  3. 检查系统资源"
