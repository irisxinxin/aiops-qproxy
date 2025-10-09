#!/bin/bash
# 直接测试 Q CLI 是否能响应（不通过 ttyd）

echo "🧪 直接测试 Q CLI 响应能力..."
echo ""

# 测试 1: 最简单的 hello
echo "测试 1: 发送简单 prompt 'hello'"
echo "======================================"
timeout 30s bash -c 'echo "hello" | q chat 2>&1' > /tmp/q_test1.txt &
PID1=$!
echo "进程 PID: $PID1"

# 等待并监控
for i in {1..30}; do
    if ! ps -p $PID1 > /dev/null 2>&1; then
        echo "进程在 $i 秒后退出"
        break
    fi
    echo -n "."
    sleep 1
done
echo ""

echo "输出:"
cat /tmp/q_test1.txt
echo ""
echo ""

# 测试 2: 使用环境变量
echo "测试 2: 使用完整环境变量"
echo "======================================"
timeout 30s bash -c 'echo "hello" | env TERM=dumb NO_COLOR=1 CLICOLOR=0 Q_MCP_AUTO_TRUST=true Q_MCP_SKIP_TRUST_PROMPTS=true Q_TOOLS_AUTO_TRUST=true q chat 2>&1' > /tmp/q_test2.txt &
PID2=$!
echo "进程 PID: $PID2"

# 等待并监控
for i in {1..30}; do
    if ! ps -p $PID2 > /dev/null 2>&1; then
        echo "进程在 $i 秒后退出"
        break
    fi
    echo -n "."
    sleep 1
done
echo ""

echo "输出:"
cat /tmp/q_test2.txt
echo ""
echo ""

# 测试 3: 检查 Q CLI 日志
echo "测试 3: 检查 Q CLI 日志"
echo "======================================"
Q_LOG_DIR="$HOME/.amazon-q/logs"
if [ -d "$Q_LOG_DIR" ]; then
    echo "Q CLI 日志目录: $Q_LOG_DIR"
    echo ""
    echo "最新日志文件:"
    ls -lht "$Q_LOG_DIR" | head -5
    echo ""
    echo "最新日志内容 (最后 30 行):"
    LATEST_LOG=$(ls -t "$Q_LOG_DIR"/*.log 2>/dev/null | head -1)
    if [ -n "$LATEST_LOG" ]; then
        echo "文件: $LATEST_LOG"
        tail -30 "$LATEST_LOG"
    else
        echo "没有找到日志文件"
    fi
else
    echo "Q CLI 日志目录不存在"
fi
echo ""

# 测试 4: 检查 AWS 配置
echo "测试 4: 检查 AWS 配置"
echo "======================================"
if aws sts get-caller-identity > /tmp/aws_identity.txt 2>&1; then
    echo "✅ AWS 凭证有效"
    cat /tmp/aws_identity.txt
else
    echo "❌ AWS 凭证无效或过期"
    cat /tmp/aws_identity.txt
fi
echo ""

echo "✅ 测试完成"
echo ""
echo "💡 分析:"
echo "  - 如果测试 1 和 2 都超时，说明 Q CLI 本身有问题"
echo "  - 如果 AWS 凭证无效，Q CLI 可能在等待认证"
echo "  - 查看 Q CLI 日志可以找到详细错误信息"

