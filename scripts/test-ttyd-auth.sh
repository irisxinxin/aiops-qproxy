#!/bin/bash
# 测试 ttyd 认证机制

echo "🧪 测试 ttyd 认证机制..."

# 停止现有的 ttyd
if pgrep -f "ttyd.*q chat" > /dev/null; then
    echo "🛑 停止现有的 ttyd..."
    pkill -f "ttyd.*q chat"
    sleep 2
fi

# 重新启动 ttyd
echo "▶️  重新启动 ttyd..."
nohup ttyd -p 7682 -c demo:password123 q chat > ./logs/ttyd-test.log 2>&1 &
TTYD_PID=$!
echo "ttyd PID: $TTYD_PID"

# 等待启动
sleep 3

# 测试不同的认证方式
echo ""
echo "🧪 测试不同的认证方式..."

echo "测试 1: 无认证"
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test1" | base64)" \
     http://127.0.0.1:7682/ws &
CURL_PID=$!
sleep 2
kill $CURL_PID 2>/dev/null

echo ""
echo "测试 2: URL 认证"
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test2" | base64)" \
     http://demo:password123@127.0.0.1:7682/ws &
CURL_PID=$!
sleep 2
kill $CURL_PID 2>/dev/null

echo ""
echo "测试 3: Authorization Header"
AUTH_HEADER=$(echo -n "demo:password123" | base64)
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test3" | base64)" \
     -H "Authorization: Basic $AUTH_HEADER" \
     http://127.0.0.1:7682/ws &
CURL_PID=$!
sleep 2
kill $CURL_PID 2>/dev/null

# 检查 ttyd 日志
echo ""
echo "📝 检查 ttyd 日志..."
tail -20 ./logs/ttyd-test.log | grep -E "auth|Auth|AUTH|credential|not authenticated|WS" || echo "没有发现相关日志"

# 清理
echo ""
echo "🛑 停止测试进程..."
kill $TTYD_PID
