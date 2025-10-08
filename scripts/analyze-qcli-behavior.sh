#!/bin/bash
# 分析 Q CLI 连接行为

echo "🔍 分析 Q CLI 连接行为..."

# 检查 ttyd 日志
echo "📋 检查 ttyd 日志..."
if [ -f "./logs/ttyd-q.log" ]; then
    echo "=== ttyd 最新日志 ==="
    tail -20 ./logs/ttyd-q.log
else
    echo "❌ ttyd 日志文件不存在"
fi

echo ""
echo "📋 检查 incident-worker 日志..."
if [ -f "./logs/incident-worker-real.log" ]; then
    echo "=== incident-worker 最新日志 ==="
    tail -20 ./logs/incident-worker-real.log
else
    echo "❌ incident-worker 日志文件不存在"
fi

echo ""
echo "💡 分析建议："
echo "  1. 查看 ttyd 日志中的连接建立和断开时间"
echo "  2. 查看 incident-worker 日志中的错误模式"
echo "  3. 分析连接断开的频率和时机"

echo ""
echo "🧪 运行连接维持时间测试："
echo "  ./scripts/test-connection-duration.sh"
