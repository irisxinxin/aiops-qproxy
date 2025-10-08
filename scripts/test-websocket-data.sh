#!/bin/bash

echo "🔍 详细测试 Q CLI 数据流..."

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
echo "🧪 使用 curl 测试 WebSocket 连接并保存输出..."
echo "建立连接并等待 30 秒，观察是否有数据..."

# 使用 curl 测试并保存输出到文件
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
     -H "Authorization: Basic $(echo -n "demo:password123" | base64)" \
     http://127.0.0.1:7682/ws > ./logs/websocket-test.log 2>&1 &
CURL_PID=$!

echo "等待 30 秒..."
sleep 30

# 检查 curl 进程是否还在运行
if ps -p $CURL_PID > /dev/null; then
    echo "✅ WebSocket 连接保持正常"
    kill $CURL_PID 2>/dev/null
else
    echo "❌ WebSocket 连接断开"
fi

echo ""
echo "📝 查看 WebSocket 测试日志："
if [ -f "./logs/websocket-test.log" ]; then
    echo "=== WebSocket 测试日志 ==="
    cat ./logs/websocket-test.log
    echo ""
    echo "日志文件大小："
    ls -la ./logs/websocket-test.log
else
    echo "❌ 测试日志文件不存在"
fi

echo ""
echo "🔍 检查 ttyd 日志中的 Q CLI 状态："
if [ -f "./logs/ttyd-q.log" ]; then
    echo "=== ttyd 最新日志 ==="
    tail -20 ./logs/ttyd-q.log
    echo ""
    echo "=== 检查是否有 Q CLI 相关输出 ==="
    if grep -i "q\|chat\|prompt\|>" ./logs/ttyd-q.log; then
        echo "✅ 发现 Q CLI 相关输出"
    else
        echo "❌ 没有发现 Q CLI 相关输出"
    fi
else
    echo "❌ ttyd 日志文件不存在"
fi

echo ""
echo "💡 分析："
echo "  - 如果 WebSocket 连接成功但没有数据，说明 Q CLI 没有准备好"
echo "  - 如果 ttyd 日志中没有 Q CLI 输出，说明 Q CLI 启动失败"
echo "  - 可能需要检查 Q CLI 配置或重启 ttyd"
