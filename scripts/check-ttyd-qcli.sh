#!/bin/bash

echo "🔍 检查 ttyd 和 Q CLI 状态..."

cd "$(dirname "$0")/.."

echo "📝 查看 ttyd 日志："
if [ -f "./logs/ttyd-q.log" ]; then
    echo "=== ttyd 完整日志 ==="
    cat ./logs/ttyd-q.log
    echo ""
    echo "日志文件大小："
    ls -la ./logs/ttyd-q.log
else
    echo "❌ ttyd 日志文件不存在"
fi

echo ""
echo "🔍 检查 ttyd 进程："
ps aux | grep ttyd | grep -v grep || echo "  没有 ttyd 进程"

echo ""
echo "🔍 检查 Q CLI 进程："
ps aux | grep "q chat" | grep -v grep || echo "  没有 q chat 进程"

echo ""
echo "🔍 检查端口状态："
ss -tlnp | grep 7682 || echo "  端口 7682 没有监听"

echo ""
echo "🧪 手动测试 Q CLI："
echo "尝试直接运行 q chat..."
timeout 10s q chat --help 2>&1 || echo "Q CLI 命令超时或失败"

echo ""
echo "🔍 检查 Q CLI 安装："
if command -v q &> /dev/null; then
    echo "✅ Q CLI 已安装"
    echo "版本信息："
    q --version 2>/dev/null || echo "无法获取版本信息"
else
    echo "❌ Q CLI 未安装"
fi

echo ""
echo "🔍 检查 AWS 配置："
if aws sts get-caller-identity &> /dev/null; then
    echo "✅ AWS 配置正常"
    aws sts get-caller-identity
else
    echo "❌ AWS 配置有问题"
    echo "请运行: aws configure"
fi

echo ""
echo "💡 分析："
echo "  - 如果 ttyd 日志中没有 Q CLI 输出，说明 Q CLI 没有启动"
echo "  - 如果 Q CLI 进程不存在，说明启动失败"
echo "  - 可能需要检查 Q CLI 安装和 AWS 配置"
