#!/bin/bash

echo "🔍 诊断 Q CLI 连接问题..."

cd "$(dirname "$0")/.."

echo "📝 查看 ttyd 日志中的 Q CLI 状态："
if [ -f "./logs/ttyd-q.log" ]; then
    echo "=== ttyd 最新日志 ==="
    tail -30 ./logs/ttyd-q.log
    echo ""
    echo "=== 检查是否有 Q CLI 相关错误 ==="
    if grep -i "error\|fail\|timeout\|broken\|pipe" ./logs/ttyd-q.log; then
        echo "❌ 发现错误信息"
    else
        echo "✅ 没有发现明显错误"
    fi
else
    echo "❌ ttyd 日志文件不存在"
fi

echo ""
echo "🔍 检查 Q CLI 是否安装和配置："
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
echo "🧪 手动测试 Q CLI："
echo "尝试直接运行 q chat..."
timeout 10s q chat --help 2>&1 || echo "Q CLI 命令超时或失败"

echo ""
echo "💡 建议："
echo "  1. 如果 Q CLI 没有安装，请安装: pip install amazon-q-cli"
echo "  2. 如果 AWS 配置有问题，请运行: aws configure"
echo "  3. 如果 Q CLI 安装但有问题，尝试重启 ttyd"
echo "  4. 检查网络连接和 AWS 权限"
