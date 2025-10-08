#!/bin/bash

echo "🔍 测试 WebSocket 连接..."

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123

cd "$(dirname "$0")/.."

echo "📋 测试参数："
echo "  WS_URL: $QPROXY_WS_URL"
echo "  WS_USER: $QPROXY_WS_USER"
echo "  WS_PASS: $QPROXY_WS_PASS"
echo ""

echo "🔍 检查 ttyd 状态："
if ss -tlnp | grep -q ":7682 "; then
    echo "✅ ttyd 正在监听端口 7682"
else
    echo "❌ ttyd 没有监听端口 7682"
    echo "请先启动 ttyd:"
    echo "  nohup ttyd -p 7682 -c demo:password123 q chat > ./logs/ttyd-q.log 2>&1 &"
    exit 1
fi

echo ""
echo "🧪 测试 WebSocket 连接："
echo "使用 curl 测试 WebSocket 握手..."

# 测试 WebSocket 握手
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
     -H "Authorization: Basic $(echo -n "demo:password123" | base64)" \
     http://127.0.0.1:7682/ws

echo ""
echo "如果看到 '101 Switching Protocols'，说明 WebSocket 连接正常"
echo "如果看到其他错误，说明连接有问题"
