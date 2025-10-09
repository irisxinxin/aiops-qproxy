#!/bin/bash
# 诊断 Q CLI 为什么收到 prompt 后卡住不响应

echo "🔍 诊断 Q CLI 卡住问题..."
echo ""

# 1. 检查 ttyd 日志
echo "1️⃣  检查 ttyd 日志（最后 50 行）:"
echo "======================================"
tail -50 ./logs/ttyd-q.log
echo ""

# 2. 检查是否有 Q CLI 进程在运行
echo "2️⃣  检查 Q CLI 进程:"
echo "======================================"
Q_PID=$(pgrep -f "q chat" | head -1)
if [ -n "$Q_PID" ]; then
    echo "✅ Q CLI 进程在运行 (PID: $Q_PID)"
    echo ""
    echo "进程详情:"
    ps -p $Q_PID -o pid,ppid,user,%cpu,%mem,stat,start,time,command
    echo ""
    
    # 检查进程状态
    STAT=$(ps -p $Q_PID -o stat --no-headers | tr -d ' ')
    case $STAT in
        D*)
            echo "⚠️  进程状态: $STAT (不可中断睡眠 - 可能在等待 I/O)"
            ;;
        S*)
            echo "✅ 进程状态: $STAT (可中断睡眠 - 正常)"
            ;;
        R*)
            echo "✅ 进程状态: $STAT (运行中)"
            ;;
        Z*)
            echo "❌ 进程状态: $STAT (僵尸进程)"
            ;;
        T*)
            echo "⚠️  进程状态: $STAT (已停止)"
            ;;
        *)
            echo "进程状态: $STAT"
            ;;
    esac
    echo ""
    
    # 使用 strace 检查进程在做什么（如果有权限）
    echo "尝试使用 strace 查看进程当前系统调用:"
    timeout 3s sudo strace -p $Q_PID 2>&1 | head -20 || echo "  (需要 sudo 权限或进程已退出)"
    echo ""
else
    echo "❌ 没有找到 Q CLI 进程"
fi
echo ""

# 3. 手动测试 Q CLI 是否能响应
echo "3️⃣  手动测试 Q CLI (10秒超时):"
echo "======================================"
echo "发送测试 prompt: 'hello'"
timeout 10s bash -c 'echo "hello" | env NO_COLOR=1 TERM=dumb Q_MCP_AUTO_TRUST=true Q_MCP_SKIP_TRUST_PROMPTS=true Q_TOOLS_AUTO_TRUST=true q chat 2>&1' > /tmp/q_manual_test.txt &
TEST_PID=$!
echo "测试进程 PID: $TEST_PID"
sleep 1

# 监控测试进程
for i in {1..10}; do
    if ! ps -p $TEST_PID > /dev/null 2>&1; then
        echo "  进程已退出"
        break
    fi
    echo -n "."
    sleep 1
done
echo ""

if [ -f /tmp/q_manual_test.txt ]; then
    echo "输出 (前 30 行):"
    head -30 /tmp/q_manual_test.txt
else
    echo "❌ 没有输出文件"
fi
echo ""

# 4. 检查 Q CLI 配置
echo "4️⃣  检查 Q CLI 配置:"
echo "======================================"
if [ -f ~/.aws/credentials ]; then
    echo "✅ AWS credentials 存在"
else
    echo "⚠️  AWS credentials 不存在"
fi

if [ -f ~/.aws/config ]; then
    echo "✅ AWS config 存在"
else
    echo "⚠️  AWS config 不存在"
fi

echo ""
echo "Q CLI 版本:"
q --version || echo "  无法获取版本"
echo ""

# 5. 建议
echo "💡 诊断建议:"
echo "======================================"
echo "如果 Q CLI 进程状态是 'D' (不可中断睡眠):"
echo "  - 可能在等待网络 I/O (连接 AWS 服务)"
echo "  - 可能在等待 MCP server 响应"
echo "  - 建议检查网络连接和防火墙设置"
echo ""
echo "如果 Q CLI 进程状态是 'S' (可中断睡眠) 但没有输出:"
echo "  - 可能在等待用户输入（即使设置了 auto-trust）"
echo "  - 可能 MCP server 初始化卡住"
echo "  - 建议尝试禁用 MCP servers"
echo ""
echo "如果手动测试也超时:"
echo "  - Q CLI 本身有问题，与 WebSocket 无关"
echo "  - 建议检查 Q CLI 日志: ~/.amazon-q/logs/"
echo ""

echo "✅ 诊断完成"

