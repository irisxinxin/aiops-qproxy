#!/bin/bash
# 测试 broken pipe 修复效果

set -e

echo "🔧 测试 broken pipe 修复效果..."

# 检查服务状态
echo "📋 检查服务状态..."
if ! curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "❌ incident-worker 未运行，请先运行 deploy-real-q.sh"
    exit 1
fi

echo "✅ incident-worker 运行正常"

# 测试多次请求，观察是否还有 broken pipe 错误
echo ""
echo "🧪 测试多次请求（观察 broken pipe 错误）..."

broken_pipe_count=0
total_requests=10

for i in $(seq 1 $total_requests); do
    echo "📤 第 $i 次请求..."
    
    RESPONSE=$(curl -sS -X POST http://127.0.0.1:8080/incident \
      -H "content-type: application/json" \
      -d "{\"incident_key\":\"test-broken-pipe-fix-$i\",\"prompt\":\"Test request $i: Please analyze this issue.\"}")
    
    echo "响应: $RESPONSE"
    
    # 检查是否包含 broken pipe 错误
    if echo "$RESPONSE" | grep -q "broken pipe"; then
        broken_pipe_count=$((broken_pipe_count + 1))
        echo "⚠️  发现 broken pipe 错误"
    fi
    
    # 等待一下，让连接有时间断开
    sleep 3
done

echo ""
echo "📊 测试结果统计："
echo "  总请求数: $total_requests"
echo "  broken pipe 错误数: $broken_pipe_count"
echo "  成功率: $(( (total_requests - broken_pipe_count) * 100 / total_requests ))%"

if [ $broken_pipe_count -eq 0 ]; then
    echo "🎉 完美！没有 broken pipe 错误"
elif [ $broken_pipe_count -lt $((total_requests / 2)) ]; then
    echo "✅ 良好！broken pipe 错误大幅减少"
else
    echo "❌ 仍需改进！broken pipe 错误仍然较多"
fi
echo ""
echo "💡 如果仍有 broken pipe 错误，可能的原因："
echo "  1. Q CLI 连接确实不稳定"
echo "  2. 连接池重新创建连接需要时间"
echo "  3. 需要进一步优化重试策略"

echo ""
echo "🔍 查看详细日志："
echo "  - incident-worker: tail -f ./logs/incident-worker-real.log"
echo "  - ttyd: tail -f ./logs/ttyd-q.log"
