#!/bin/bash

echo "🔧 测试基本 WebSocket 连接..."
echo "📋 检查 ttyd 状态..."

# 检查 ttyd 是否运行
if ! pgrep -f "ttyd.*q chat" > /dev/null; then
    echo "❌ ttyd 未运行"
    echo "启动 ttyd..."
    nohup ttyd -p 7682 q chat > ./logs/ttyd-test.log 2>&1 &
    TTYD_PID=$!
    echo "ttyd PID: $TTYD_PID"
    sleep 3
else
    echo "✅ ttyd 正在运行"
fi

# 检查端口
echo "🔍 检查端口状态："
ss -tlnp | grep 7682

echo ""
echo "🧪 测试 WebSocket 连接..."
echo "使用 curl 测试 WebSocket 握手..."

# 测试 WebSocket 连接
timeout 10 curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
     http://127.0.0.1:7682/ws 2>&1

echo ""
echo "📝 如果看到 '101 Switching Protocols'，说明 WebSocket 连接正常"
echo "📝 如果连接失败，请检查 ttyd 配置"

# 检查 ttyd 日志
echo ""
echo "📝 查看 ttyd 日志："
if [ -f "./logs/ttyd-test.log" ]; then
    tail -10 ./logs/ttyd-test.log
fi
