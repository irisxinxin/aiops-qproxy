#!/bin/bash
# 详细诊断 broken pipe 问题

echo "🔍 详细诊断 broken pipe 问题..."

# 检查服务状态
echo "📋 服务状态："
echo "ttyd PID: $(pgrep -f 'ttyd.*q chat')"
echo "incident-worker PID: $(pgrep -f 'incident-worker')"

# 检查端口和连接
echo ""
echo "📋 网络连接状态："
echo "端口 7682 监听状态："
ss -tlnp | grep ":7682"

echo ""
echo "端口 8080 监听状态："
ss -tlnp | grep ":8080"

echo ""
echo "WebSocket 连接数："
netstat -an | grep ":7682" | wc -l

# 检查最近的日志
echo ""
echo "📝 最近的错误日志："
echo "=== ttyd 最新日志 ==="
tail -30 ./logs/ttyd-q.log | grep -E "error|Error|ERROR|WS|closed|broken|connection" || echo "没有发现明显错误"

echo ""
echo "=== incident-worker 最新日志 ==="
tail -30 ./logs/incident-worker-real.log | grep -E "error|Error|ERROR|broken|pipe|connection|failed" || echo "没有发现明显错误"

# 测试连接稳定性
echo ""
echo "🧪 测试连接稳定性..."
echo "发送 3 个快速请求测试连接池..."

for i in {1..3}; do
    echo -n "测试 $i/3... "
    RESPONSE=$(curl -s -X POST http://127.0.0.1:8080/incident \
        -H 'content-type: application/json' \
        -d "{\"incident_key\":\"diagnose-$i\",\"prompt\":\"Test $i\"}")
    
    if echo "$RESPONSE" | grep -q "broken pipe"; then
        echo "❌ broken pipe"
    elif echo "$RESPONSE" | grep -q "error\|failed"; then
        echo "⚠️  其他错误: $RESPONSE"
    else
        echo "✅ 成功"
    fi
    
    sleep 1
done

# 检查 Q CLI 状态
echo ""
echo "🔍 检查 Q CLI 状态..."
echo "检查 ttyd 日志中的 Q CLI 相关输出："
tail -50 ./logs/ttyd-q.log | grep -E "q chat|Q CLI|amazon|aws" || echo "没有发现 Q CLI 相关输出"

# 建议
echo ""
echo "💡 分析结果和建议："
echo "1. 如果 broken pipe 频繁出现，可能是 Q CLI 连接不稳定"
echo "2. 如果 ttyd 日志中没有 Q CLI 输出，可能是 Q CLI 没有正确启动"
echo "3. 建议检查："
echo "   - AWS 配置: aws configure list"
echo "   - Q CLI 状态: q --version"
echo "   - 网络连接: ping amazon.com"
echo ""
echo "🛠️  尝试修复："
echo "1. 重启 ttyd: kill \$(pgrep -f 'ttyd.*q chat') && ./scripts/deploy-real-q.sh"
echo "2. 检查 Q CLI 配置: q configure"
echo "3. 手动测试 Q CLI: q chat"
