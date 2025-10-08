#!/bin/bash

echo "🔍 测试 Q CLI 提示符发送..."

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
echo "🧪 使用 wscat 测试 WebSocket 连接和提示符..."
echo "建立连接并等待 Q CLI 发送提示符..."

# 检查是否有 wscat
if ! command -v wscat &> /dev/null; then
    echo "❌ wscat 未安装，使用 curl 测试..."
    echo "使用 curl 测试 WebSocket 连接..."
    curl -i -N -H "Connection: Upgrade" \
         -H "Upgrade: websocket" \
         -H "Sec-WebSocket-Version: 13" \
         -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
         -H "Authorization: Basic $(echo -n "demo:password123" | base64)" \
         http://127.0.0.1:7682/ws &
    CURL_PID=$!
    
    echo "等待 60 秒，观察是否有数据..."
    sleep 60
    
    if ps -p $CURL_PID > /dev/null; then
        echo "✅ WebSocket 连接保持正常"
        kill $CURL_PID 2>/dev/null
    else
        echo "❌ WebSocket 连接断开"
    fi
else
    echo "使用 wscat 测试..."
    echo "连接 WebSocket 并等待提示符..."
    timeout 60s wscat -c "ws://127.0.0.1:7682/ws" -H "Authorization: Basic $(echo -n "demo:password123" | base64)" || echo "连接超时或断开"
fi

echo ""
echo "💡 如果看到提示符（如 '> ' 或 'q> '），说明 Q CLI 正常工作"
echo "如果没有看到提示符，可能是 Q CLI 没有准备好或配置有问题"
