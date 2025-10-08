#!/bin/bash
# 检查认证问题

echo "🔍 检查认证问题..."

# 检查 ttyd 日志中的认证错误
echo "📝 检查 ttyd 日志中的认证错误..."
echo "=== ttyd 最新日志 ==="
tail -50 ./logs/ttyd-q.log | grep -E "auth|Auth|AUTH|credential|login|Login|LOGIN|not authenticated|unauthorized|Unauthorized|UNAUTHORIZED" || echo "没有发现认证相关错误"

echo ""
echo "=== ttyd 完整最新日志 ==="
tail -20 ./logs/ttyd-q.log

# 检查 incident-worker 日志
echo ""
echo "📝 检查 incident-worker 日志..."
echo "=== incident-worker 最新日志 ==="
tail -20 ./logs/incident-worker-real.log

# 测试 WebSocket 认证
echo ""
echo "🧪 测试 WebSocket 认证..."
echo "使用正确的认证信息测试连接..."

# 生成正确的 Basic Auth header
AUTH_HEADER=$(echo -n "demo:password123" | base64)
echo "Basic Auth Header: $AUTH_HEADER"

# 测试 WebSocket 连接
echo "测试 WebSocket 连接..."
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
     -H "Authorization: Basic $AUTH_HEADER" \
     http://127.0.0.1:7682/ws &
CURL_PID=$!

sleep 3
kill $CURL_PID 2>/dev/null

# 检查 ttyd 进程的认证配置
echo ""
echo "🔍 检查 ttyd 进程配置..."
ps aux | grep "ttyd.*q chat" | grep -v grep

# 建议
echo ""
echo "💡 可能的问题和解决方案："
echo "1. 如果 ttyd 日志显示 'not authenticated'，说明认证信息不匹配"
echo "2. 检查 ttyd 启动命令中的认证配置"
echo "3. 检查 incident-worker 中的认证环境变量"
echo ""
echo "🛠️  尝试修复："
echo "1. 重启 ttyd 使用正确的认证配置"
echo "2. 检查环境变量是否正确传递"
echo "3. 手动测试 WebSocket 认证"
