#!/bin/bash
# 直接测试 ttyd + Q CLI 的交互

echo "🧪 测试 ttyd + Q CLI 交互..."
echo ""

# 检查 ttyd 是否在运行
if ! pgrep -f "ttyd.*q chat" > /dev/null; then
    echo "❌ ttyd 没有运行，请先启动"
    exit 1
fi

echo "✅ ttyd 正在运行"
echo ""

# 使用 websocat 测试 WebSocket 连接（如果有的话）
if command -v websocat > /dev/null 2>&1; then
    echo "使用 websocat 测试..."
    echo '{"columns":120,"rows":30}' | websocat ws://127.0.0.1:7682/ws | head -50
else
    echo "⚠️  websocat 未安装，跳过直接测试"
    echo ""
    echo "建议安装 websocat 进行测试:"
    echo "  cargo install websocat"
    echo ""
fi

# 查看 Q CLI 进程状态
echo "Q CLI 进程状态:"
ps aux | grep "q chat" | grep -v grep

echo ""
echo "✅ 测试完成"

