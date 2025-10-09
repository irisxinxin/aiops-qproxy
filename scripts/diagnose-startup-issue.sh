#!/bin/bash

echo "🔧 详细诊断 incident-worker 启动问题..."
echo "📋 检查服务状态..."

# 检查 ttyd 是否运行
echo "🔍 检查 ttyd 进程："
if pgrep -f "ttyd.*q chat" > /dev/null; then
    echo "✅ ttyd 正在运行"
    ps aux | grep ttyd | grep -v grep
else
    echo "❌ ttyd 未运行"
fi

# 检查端口
echo ""
echo "🔍 检查端口状态："
ss -tlnp | grep -E ":(7682|8080)"

# 检查 ttyd 日志
echo ""
echo "📝 查看 ttyd 最新日志："
if [ -f "./logs/ttyd-q.log" ]; then
    echo "=== ttyd 最新日志 ==="
    tail -20 ./logs/ttyd-q.log
else
    echo "❌ ttyd 日志文件不存在"
fi

# 测试 WebSocket 连接
echo ""
echo "🧪 测试 WebSocket 连接..."
echo "使用 curl 测试 WebSocket 握手（无认证）..."

timeout 10 curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
     http://127.0.0.1:7682/ws 2>&1 | head -10

echo ""
echo "📝 如果看到 '101 Switching Protocols'，说明 WebSocket 连接正常"
echo "📝 如果连接失败，请检查 ttyd 配置"

# 检查 incident-worker 日志
echo ""
echo "📝 查看 incident-worker 最新日志："
if [ -f "./logs/incident-worker-real.log" ]; then
    echo "=== incident-worker 最新日志 ==="
    tail -20 ./logs/incident-worker-real.log
else
    echo "❌ incident-worker 日志文件不存在"
fi
