#!/bin/bash
# 检查认证配置

echo "🔍 检查认证配置..."

echo "📋 环境变量："
echo "QPROXY_WS_URL: ${QPROXY_WS_URL:-未设置}"
echo "QPROXY_WS_USER: ${QPROXY_WS_USER:-未设置}"
echo "QPROXY_WS_PASS: ${QPROXY_WS_PASS:-未设置}"

echo ""
echo "📋 ttyd 进程："
ps aux | grep "ttyd.*q chat" | grep -v grep

echo ""
echo "📋 端口状态："
ss -tlnp | grep ":7682"

echo ""
echo "📋 测试 WebSocket 认证："
echo "尝试手动连接 WebSocket..."

# 测试 WebSocket 连接
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
     -H "Authorization: Basic $(echo -n "demo:password123" | base64)" \
     http://127.0.0.1:7682/ws
