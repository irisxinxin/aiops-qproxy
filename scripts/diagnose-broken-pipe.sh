#!/bin/bash
# 诊断 broken pipe 问题

echo "🔍 诊断 broken pipe 问题..."

# 检查服务状态
echo "📋 服务状态："
if pgrep -f "ttyd.*q chat" > /dev/null; then
    echo "✅ ttyd 运行中"
else
    echo "❌ ttyd 未运行"
    exit 1
fi

if pgrep -f "incident-worker" > /dev/null; then
    echo "✅ incident-worker 运行中"
else
    echo "❌ incident-worker 未运行"
    exit 1
fi

# 检查端口
echo "📋 端口状态："
ss -tlnp | grep -E ":7682|:8080"

# 检查最近的错误日志
echo ""
echo "📝 最近的错误日志："
echo "=== ttyd 日志 ==="
tail -20 ./logs/ttyd-q.log | grep -E "error|Error|ERROR|WS|closed|broken" || echo "没有发现错误"

echo ""
echo "=== incident-worker 日志 ==="
tail -20 ./logs/incident-worker-real.log | grep -E "error|Error|ERROR|broken|pipe|connection" || echo "没有发现错误"

# 测试 WebSocket 连接稳定性
echo ""
echo "🧪 测试 WebSocket 连接稳定性..."
echo "发送多个测试请求..."

for i in {1..5}; do
    echo "测试 $i/5..."
    RESPONSE=$(curl -s -X POST http://127.0.0.1:8080/incident \
        -H 'content-type: application/json' \
        -d "{\"incident_key\":\"test-$i\",\"prompt\":\"Hello test $i\"}")
    
    if echo "$RESPONSE" | grep -q "broken pipe"; then
        echo "❌ 测试 $i 失败: broken pipe"
    elif echo "$RESPONSE" | grep -q "error\|failed"; then
        echo "⚠️  测试 $i 失败: $RESPONSE"
    else
        echo "✅ 测试 $i 成功"
    fi
    
    sleep 2
done

# 检查连接池状态
echo ""
echo "📊 连接池状态分析："
echo "检查 incident-worker 进程的连接..."

# 使用 netstat 检查连接
echo "WebSocket 连接数："
netstat -an | grep ":7682" | wc -l

echo "TCP 连接状态："
netstat -an | grep ":7682" | head -5

# 建议
echo ""
echo "💡 建议："
echo "1. 如果 broken pipe 频繁出现，可能是 Q CLI 连接不稳定"
echo "2. 尝试重启 ttyd: kill \$(pgrep -f 'ttyd.*q chat') && ./scripts/deploy-real-q.sh"
echo "3. 检查 AWS 网络连接和 Q CLI 配置"
echo "4. 考虑增加连接池大小或减少连接超时时间"