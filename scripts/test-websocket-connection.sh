#!/bin/bash

echo "🔧 诊断 WebSocket 连接问题..."
echo "📋 检查服务状态..."

# 检查 ttyd 是否运行
if ! pgrep -f "ttyd.*q chat" > /dev/null; then
    echo "❌ ttyd 未运行，请先运行: ./scripts/deploy-real-q.sh"
    exit 1
fi

echo "✅ ttyd 正在运行"

# 检查端口
echo "🔍 检查端口状态："
ss -tlnp | grep -E ":(7682|8080)"

echo ""
echo "🧪 测试 WebSocket 连接..."
echo "使用 curl 测试 WebSocket 握手..."

# 测试 WebSocket 连接
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
     -H "Authorization: Basic $(echo -n 'demo:password123' | base64)" \
     http://127.0.0.1:7682/ws

echo ""
echo "📝 如果看到 '101 Switching Protocols'，说明 WebSocket 连接正常"
echo "📝 如果连接失败，请检查 ttyd 配置和认证"
