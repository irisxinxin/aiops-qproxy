#!/bin/bash

echo "🔍 测试 WebSocket 连接和 Q CLI 状态..."

cd "$(dirname "$0")/.."

echo "📋 测试参数："
echo "  WS_URL: http://127.0.0.1:7682/ws"
echo "  WS_USER: demo"
echo "  WS_PASS: password123"
echo ""

echo "🔍 检查 ttyd 状态："
if ss -tlnp | grep -q ":7682 "; then
    echo "✅ ttyd 正在监听端口 7682"
    echo "ttyd 进程："
    ps aux | grep ttyd | grep -v grep
else
    echo "❌ ttyd 没有监听端口 7682"
    exit 1
fi

echo ""
echo "🧪 测试 WebSocket 握手："
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
     -H "Authorization: Basic $(echo -n "demo:password123" | base64)" \
     http://127.0.0.1:7682/ws &
CURL_PID=$!

sleep 5
echo "等待 5 秒后检查 WebSocket 连接状态..."

# 检查 curl 进程是否还在运行
if ps -p $CURL_PID > /dev/null; then
    echo "✅ WebSocket 连接正常（curl 进程还在运行）"
    kill $CURL_PID 2>/dev/null
else
    echo "❌ WebSocket 连接失败（curl 进程已退出）"
fi

echo ""
echo "🔍 检查 ttyd 日志中的 Q CLI 状态："
if [ -f "./logs/ttyd-q.log" ]; then
    echo "=== ttyd 最新日志 ==="
    tail -20 ./logs/ttyd-q.log
    echo ""
    echo "=== 检查是否有 Q CLI 相关错误 ==="
    if grep -i "error\|fail\|timeout" ./logs/ttyd-q.log; then
        echo "❌ 发现错误信息"
    else
        echo "✅ 没有发现明显错误"
    fi
else
    echo "❌ ttyd 日志文件不存在"
fi

echo ""
echo "💡 建议："
echo "  1. 如果 WebSocket 握手失败，检查 ttyd 配置"
echo "  2. 如果握手成功但连接池初始化失败，可能是 Q CLI 没有准备好"
echo "  3. 尝试重启 ttyd: ./scripts/start-ttyd.sh"
