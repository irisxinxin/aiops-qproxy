#!/bin/bash
# 调试 Q CLI 问题

echo "🔍 调试 Q CLI..."
echo ""

# 1. 检查 Q CLI 版本
echo "1️⃣  Q CLI 版本:"
echo "======================================"
q --version
echo ""

# 2. 检查 AWS 配置
echo "2️⃣  AWS 配置:"
echo "======================================"
if aws sts get-caller-identity 2>&1; then
    echo "✅ AWS 凭证有效"
else
    echo "❌ AWS 凭证无效"
fi
echo ""

# 3. 直接测试 Q CLI (不使用环境变量)
echo "3️⃣  直接测试 Q CLI (无环境变量):"
echo "======================================"
timeout 15s bash -c 'echo "hello" | q chat 2>&1' > /tmp/q_debug_1.txt &
PID=$!
echo "进程 PID: $PID"

# 监控进程状态
for i in {1..15}; do
    if ! ps -p $PID > /dev/null 2>&1; then
        echo "进程在 $i 秒后退出"
        break
    fi
    
    # 每秒检查一次进程状态
    STAT=$(ps -p $PID -o stat --no-headers 2>/dev/null | tr -d ' ')
    echo "  [$i s] 进程状态: $STAT"
    sleep 1
done

echo ""
echo "输出:"
cat /tmp/q_debug_1.txt
echo ""
echo ""

# 4. 使用环境变量测试
echo "4️⃣  使用环境变量测试:"
echo "======================================"
timeout 15s bash -c 'echo "hello" | env TERM=dumb NO_COLOR=1 CLICOLOR=0 Q_MCP_AUTO_TRUST=true Q_MCP_SKIP_TRUST_PROMPTS=true Q_TOOLS_AUTO_TRUST=true q chat 2>&1' > /tmp/q_debug_2.txt &
PID=$!
echo "进程 PID: $PID"

for i in {1..15}; do
    if ! ps -p $PID > /dev/null 2>&1; then
        echo "进程在 $i 秒后退出"
        break
    fi
    
    STAT=$(ps -p $PID -o stat --no-headers 2>/dev/null | tr -d ' ')
    echo "  [$i s] 进程状态: $STAT"
    sleep 1
done

echo ""
echo "输出:"
cat /tmp/q_debug_2.txt
echo ""
echo ""

# 5. 检查 Q CLI 日志
echo "5️⃣  Q CLI 日志:"
echo "======================================"
Q_LOG_DIR="$HOME/.amazon-q/logs"
if [ -d "$Q_LOG_DIR" ]; then
    echo "最新日志文件:"
    LATEST_LOG=$(ls -t "$Q_LOG_DIR"/*.log 2>/dev/null | head -1)
    if [ -n "$LATEST_LOG" ]; then
        echo "文件: $LATEST_LOG"
        echo ""
        echo "最后 50 行:"
        tail -50 "$LATEST_LOG"
    else
        echo "没有日志文件"
    fi
else
    echo "Q CLI 日志目录不存在"
fi
echo ""

# 6. 检查 ttyd 中的 Q CLI 进程
echo "6️⃣  检查 ttyd 中的 Q CLI 进程:"
echo "======================================"
if pgrep -f "q chat" > /dev/null; then
    echo "✅ 有 Q CLI 进程在运行"
    ps aux | grep "q chat" | grep -v grep
else
    echo "❌ 没有 Q CLI 进程"
fi
echo ""

echo "✅ 调试完成"

