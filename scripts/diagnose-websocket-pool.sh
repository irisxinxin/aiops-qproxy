#!/bin/bash

echo "🔍 诊断 WebSocket 连接池问题..."

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
echo "🔍 检查连接池配置："
echo "当前配置："
echo "  QPROXY_WS_URL: $QPROXY_WS_URL"
echo "  QPROXY_WS_USER: $QPROXY_WS_USER"
echo "  QPROXY_WS_PASS: $QPROXY_WS_PASS"
echo "  QPROXY_WS_POOL: $QPROXY_WS_POOL"

echo ""
echo "🧪 测试单个 WebSocket 连接："
echo "尝试建立 WebSocket 连接并保持 10 秒..."

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123

# 使用 wscat 或 curl 测试长时间连接
echo "使用 curl 测试长时间 WebSocket 连接..."
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
     -H "Authorization: Basic $(echo -n "demo:password123" | base64)" \
     http://127.0.0.1:7682/ws &
CURL_PID=$!

sleep 10
echo "等待 10 秒后检查连接状态..."

if ps -p $CURL_PID > /dev/null; then
    echo "✅ WebSocket 连接保持正常"
    kill $CURL_PID 2>/dev/null
else
    echo "❌ WebSocket 连接断开"
fi

echo ""
echo "💡 建议："
echo "  1. 如果 WebSocket 连接正常，问题可能在连接池管理"
echo "  2. 尝试减少连接池大小: export QPROXY_WS_POOL=1"
echo "  3. 检查 ttyd 的连接限制"
echo "  4. 尝试重启 ttyd 和 incident-worker"
