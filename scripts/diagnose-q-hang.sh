#!/bin/bash
# 诊断 Q CLI 为什么卡住不响应

set -e

echo "🔍 诊断 Q CLI 响应问题..."
echo ""

# 1. 检查 Q CLI 版本和配置
echo "1️⃣  检查 Q CLI 版本:"
q --version || echo "  ❌ Q CLI 未安装或不在 PATH 中"
echo ""

# 2. 检查环境变量
echo "2️⃣  检查相关环境变量:"
echo "  Q_MCP_AUTO_TRUST=${Q_MCP_AUTO_TRUST:-未设置}"
echo "  Q_MCP_SKIP_TRUST_PROMPTS=${Q_MCP_SKIP_TRUST_PROMPTS:-未设置}"
echo "  Q_TOOLS_AUTO_TRUST=${Q_TOOLS_AUTO_TRUST:-未设置}"
echo "  NO_COLOR=${NO_COLOR:-未设置}"
echo "  TERM=${TERM:-未设置}"
echo ""

# 3. 测试 Q CLI 是否能响应简单命令
echo "3️⃣  测试 Q CLI 响应 (10秒超时):"
echo "  发送: 'hello'"
if timeout 10s bash -c 'echo "hello" | env NO_COLOR=1 TERM=dumb Q_MCP_AUTO_TRUST=true Q_MCP_SKIP_TRUST_PROMPTS=true Q_TOOLS_AUTO_TRUST=true q chat 2>&1' > /tmp/q_test_output.txt; then
    echo "  ✅ Q CLI 有响应"
    echo "  输出前 20 行:"
    head -20 /tmp/q_test_output.txt | sed 's/^/    /'
else
    EXIT_CODE=$?
    if [ $EXIT_CODE -eq 124 ]; then
        echo "  ❌ Q CLI 超时（10秒内无响应）"
    else
        echo "  ❌ Q CLI 退出异常 (exit code: $EXIT_CODE)"
    fi
    echo "  输出内容:"
    cat /tmp/q_test_output.txt | sed 's/^/    /'
fi
echo ""

# 4. 检查 ttyd 进程
echo "4️⃣  检查 ttyd 进程:"
if pgrep -f "ttyd.*q chat" > /dev/null; then
    echo "  ✅ ttyd 进程在运行"
    echo "  进程信息:"
    ps aux | grep -E "ttyd.*q chat" | grep -v grep | sed 's/^/    /'
else
    echo "  ❌ 未找到 ttyd 进程"
fi
echo ""

# 5. 检查端口监听
echo "5️⃣  检查端口 7682 监听:"
if ss -tlnp 2>/dev/null | grep -q ":7682 "; then
    echo "  ✅ 端口 7682 正在监听"
    ss -tlnp 2>/dev/null | grep ":7682 " | sed 's/^/    /'
elif netstat -tlnp 2>/dev/null | grep -q ":7682 "; then
    echo "  ✅ 端口 7682 正在监听"
    netstat -tlnp 2>/dev/null | grep ":7682 " | sed 's/^/    /'
else
    echo "  ❌ 端口 7682 未监听"
fi
echo ""

# 6. 查看 ttyd 日志尾部
echo "6️⃣  ttyd 日志最后 30 行:"
if [ -f "./logs/ttyd-q.log" ]; then
    tail -30 ./logs/ttyd-q.log | sed 's/^/    /'
else
    echo "  ❌ 日志文件不存在: ./logs/ttyd-q.log"
fi
echo ""

# 7. 建议
echo "💡 诊断建议:"
echo "  - 如果 Q CLI 超时: 可能是 MCP server 初始化慢或需要交互"
echo "  - 如果 ttyd 日志有错误: 查看具体错误信息"
echo "  - 如果环境变量未设置: 检查 deploy-real-q.sh 脚本"
echo ""

echo "✅ 诊断完成"

