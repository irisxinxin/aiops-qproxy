#!/bin/bash

echo "🔍 直接测试 Q CLI 和 ttyd 交互..."

cd "$(dirname "$0")/.."

echo "📝 查看 ttyd 日志中的 Q CLI 输出："
if [ -f "./logs/ttyd-q.log" ]; then
    echo "=== ttyd 最新日志 ==="
    tail -50 ./logs/ttyd-q.log
    echo ""
    echo "=== 搜索 Q CLI 相关输出 ==="
    if grep -i "q\|chat\|prompt\|>" ./logs/ttyd-q.log; then
        echo "✅ 发现 Q CLI 相关输出"
    else
        echo "❌ 没有发现 Q CLI 相关输出"
    fi
else
    echo "❌ ttyd 日志文件不存在"
fi

echo ""
echo "🧪 测试 ttyd 的 Web 界面："
echo "尝试访问 ttyd 的 Web 界面..."
curl -s -o /dev/null -w "HTTP状态码: %{http_code}\n" http://127.0.0.1:7682/ || echo "无法访问 ttyd Web 界面"

echo ""
echo "🧪 测试 ttyd 的 WebSocket 端点："
echo "尝试访问 WebSocket 端点..."
curl -s -o /dev/null -w "HTTP状态码: %{http_code}\n" http://127.0.0.1:7682/ws || echo "无法访问 WebSocket 端点"

echo ""
echo "🔍 检查 ttyd 进程详情："
ps aux | grep ttyd | grep -v grep

echo ""
echo "🔍 检查 Q CLI 进程详情："
ps aux | grep "q chat" | grep -v grep

echo ""
echo "🧪 手动测试 Q CLI 交互："
echo "尝试直接运行 q chat 并发送命令..."
echo "echo 'test' | q chat" 2>&1 || echo "Q CLI 交互测试失败"

echo ""
echo "💡 可能的问题："
echo "  1. Q CLI 启动了但没有发送提示符"
echo "  2. ttyd 的 WebSocket 模式有问题"
echo "  3. Q CLI 需要用户交互才能发送提示符"
echo "  4. 提示符格式不是我们期望的"
