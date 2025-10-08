#!/bin/bash

echo "🚀 启动 ttyd + Q CLI..."

# 停止现有的 ttyd
echo "🛑 停止现有 ttyd..."
pkill -f 'ttyd.*q chat' || true
sleep 2

# 创建日志目录
mkdir -p ./logs

# 启动 ttyd
echo "▶️  启动 ttyd..."
echo "   端口: 7682"
echo "   认证: demo:password123"
echo "   命令: q chat"
echo ""

nohup ttyd -p 7682 -c demo:password123 q chat > ./logs/ttyd-q.log 2>&1 &
TTYD_PID=$!

echo "ttyd PID: $TTYD_PID"
echo "日志文件: ./logs/ttyd-q.log"
echo ""

# 等待启动
echo "⏳ 等待 ttyd 启动..."
sleep 3

# 检查状态
if ss -tlnp | grep -q ":7682 "; then
    echo "✅ ttyd 启动成功！"
    echo ""
    echo "🌐 访问方式："
    echo "   Web 界面: http://127.0.0.1:7682"
    echo "   WebSocket: ws://127.0.0.1:7682/ws"
    echo "   认证: demo / password123"
    echo ""
    echo "📝 查看日志: tail -f ./logs/ttyd-q.log"
    echo "🛑 停止服务: kill $TTYD_PID"
else
    echo "❌ ttyd 启动失败"
    echo "查看日志: cat ./logs/ttyd-q.log"
    exit 1
fi
