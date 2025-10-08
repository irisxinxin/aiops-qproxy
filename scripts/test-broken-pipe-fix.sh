#!/bin/bash
# 测试 broken pipe 修复效果

echo "🧪 测试 broken pipe 修复效果..."

# 检查服务状态
if ! pgrep -f "incident-worker" > /dev/null; then
    echo "❌ incident-worker 未运行，请先启动服务"
    exit 1
fi

echo "✅ incident-worker 运行中"

# 测试多个请求
echo "📊 发送 10 个测试请求..."
SUCCESS_COUNT=0
FAILED_COUNT=0

for i in {1..10}; do
    echo -n "测试 $i/10... "
    
    RESPONSE=$(curl -s -X POST http://127.0.0.1:8080/incident \
        -H 'content-type: application/json' \
        -d "{\"incident_key\":\"test-broken-pipe-$i\",\"prompt\":\"Hello test $i\"}")
    
    if echo "$RESPONSE" | grep -q "broken pipe"; then
        echo "❌ broken pipe"
        FAILED_COUNT=$((FAILED_COUNT + 1))
    elif echo "$RESPONSE" | grep -q "error\|failed"; then
        echo "⚠️  其他错误"
        FAILED_COUNT=$((FAILED_COUNT + 1))
    else
        echo "✅ 成功"
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
    fi
    
    # 短暂等待
    sleep 1
done

echo ""
echo "📊 测试结果："
echo "  成功: $SUCCESS_COUNT"
echo "  失败: $FAILED_COUNT"
echo "  成功率: $(( SUCCESS_COUNT * 100 / 10 ))%"

if [ $FAILED_COUNT -eq 0 ]; then
    echo "🎉 所有测试通过！broken pipe 问题已修复"
elif [ $FAILED_COUNT -lt 3 ]; then
    echo "✅ 大部分测试通过，broken pipe 问题有所改善"
else
    echo "❌ 仍有较多 broken pipe 错误，需要进一步优化"
    echo "💡 建议："
    echo "  1. 检查 Q CLI 连接稳定性"
    echo "  2. 增加连接池大小"
    echo "  3. 减少连接超时时间"
fi