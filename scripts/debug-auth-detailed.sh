#!/bin/bash
# 深入分析认证问题

echo "🔍 深入分析认证问题..."

# 检查 ttyd 的认证配置
echo "📋 检查 ttyd 认证配置..."
echo "ttyd 进程："
ps aux | grep "ttyd.*q chat" | grep -v grep

echo ""
echo "ttyd 启动命令中的认证配置："
echo "  -c demo:password123"

# 验证 Base64 编码
echo ""
echo "🔐 验证认证信息编码..."
echo "用户名: demo"
echo "密码: password123"
echo "组合: demo:password123"
AUTH_HEADER=$(echo -n "demo:password123" | base64)
echo "Base64 编码: $AUTH_HEADER"

# 检查 ttyd 日志中的 credential
echo ""
echo "📝 检查 ttyd 日志中的 credential..."
tail -50 ./logs/ttyd-q.log | grep "credential" || echo "没有找到 credential 信息"

# 手动测试 WebSocket 认证
echo ""
echo "🧪 手动测试 WebSocket 认证..."
echo "使用 curl 测试 WebSocket 连接..."

# 测试不同的认证方式
echo "测试 1: 使用 Authorization header"
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
     -H "Authorization: Basic $AUTH_HEADER" \
     http://127.0.0.1:7682/ws &
CURL_PID=$!

sleep 3
kill $CURL_PID 2>/dev/null

echo ""
echo "测试 2: 使用 URL 认证"
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test2" | base64)" \
     http://demo:password123@127.0.0.1:7682/ws &
CURL_PID=$!

sleep 3
kill $CURL_PID 2>/dev/null

# 检查 ttyd 日志
echo ""
echo "📝 检查测试后的 ttyd 日志..."
tail -10 ./logs/ttyd-q.log | grep -E "auth|Auth|AUTH|credential|not authenticated" || echo "没有发现认证相关日志"

# 检查 incident-worker 的环境变量
echo ""
echo "📋 检查 incident-worker 环境变量..."
if pgrep -f "incident-worker" > /dev/null; then
    WORKER_PID=$(pgrep -f "incident-worker")
    echo "incident-worker PID: $WORKER_PID"
    if [ -f "/proc/$WORKER_PID/environ" ]; then
        echo "QPROXY_WS_USER: $(tr '\0' '\n' < /proc/$WORKER_PID/environ 2>/dev/null | grep '^QPROXY_WS_USER=' | cut -d'=' -f2 || echo '未设置')"
        echo "QPROXY_WS_PASS: $(tr '\0' '\n' < /proc/$WORKER_PID/environ 2>/dev/null | grep '^QPROXY_WS_PASS=' | cut -d'=' -f2 || echo '未设置')"
    fi
fi

# 建议
echo ""
echo "💡 可能的问题和解决方案："
echo "1. ttyd 的认证机制可能与我们的实现不匹配"
echo "2. 可能需要使用不同的认证方式"
echo "3. 检查 ttyd 版本和认证配置"
echo ""
echo "🛠️  尝试修复："
echo "1. 重启 ttyd 并检查认证配置"
echo "2. 尝试使用 URL 认证而不是 Header 认证"
echo "3. 检查 ttyd 的认证文档"
