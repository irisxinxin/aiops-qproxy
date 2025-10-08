#!/bin/bash
# 测试连接错误重试机制

set -e

echo "🔧 测试连接错误重试机制..."

# 检查服务状态
echo "📋 检查服务状态..."
if ! curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "❌ incident-worker 未运行，请先运行 deploy-real-q.sh"
    exit 1
fi

echo "✅ incident-worker 运行正常"

# 测试多次请求，观察连接错误重试机制
echo ""
echo "🧪 测试连接错误重试机制..."

broken_pipe_count=0
success_count=0
total_requests=10

for i in $(seq 1 $total_requests); do
    echo "📤 第 $i 次请求..."
    
    RESPONSE=$(curl -sS -X POST http://127.0.0.1:8080/incident \
      -H "content-type: application/json" \
      -d "{\"incident_key\":\"test-retry-mechanism-$i\",\"prompt\":\"Test request $i: Please analyze this issue.\"}")
    
    echo "响应: $RESPONSE"
    
    # 检查是否包含 broken pipe 错误
    if echo "$RESPONSE" | grep -q "broken pipe"; then
        broken_pipe_count=$((broken_pipe_count + 1))
        echo "⚠️  发现 broken pipe 错误"
    elif echo "$RESPONSE" | grep -q "answer"; then
        success_count=$((success_count + 1))
        echo "✅ 请求成功"
    else
        echo "❓ 未知响应"
    fi
    
    # 等待一下，让连接有时间断开
    sleep 2
done

echo ""
echo "📊 测试结果统计："
echo "  总请求数: $total_requests"
echo "  成功请求数: $success_count"
echo "  broken pipe 错误数: $broken_pipe_count"
echo "  成功率: $(( success_count * 100 / total_requests ))%"

if [ $broken_pipe_count -eq 0 ]; then
    echo "🎉 完美！没有 broken pipe 错误"
elif [ $success_count -gt $broken_pipe_count ]; then
    echo "✅ 良好！重试机制工作正常"
elif [ $success_count -eq $broken_pipe_count ]; then
    echo "⚠️  一般！重试机制部分有效"
else
    echo "❌ 仍需改进！重试机制效果不佳"
fi

echo ""
echo "💡 修复策略："
echo "  1. 连接错误立即失败，不进行无效重试"
echo "  2. 在 runner 层面重试，重新获取连接"
echo "  3. 连接池自动补充失效连接"

echo ""
echo "🔍 查看详细日志："
echo "  - incident-worker: tail -f ./logs/incident-worker-real.log"
echo "  - ttyd: tail -f ./logs/ttyd-q.log"
