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

# 测试多次请求，模拟连接断开和重连
echo ""
echo "🧪 测试多次请求（模拟连接断开和重连）..."

for i in {1..5}; do
    echo "📤 第 $i 次请求..."
    
    RESPONSE=$(curl -sS -X POST http://127.0.0.1:8080/incident \
      -H "content-type: application/json" \
      -d "{\"incident_key\":\"test-broken-pipe-$i\",\"prompt\":\"Test request $i: Please analyze this issue.\"}")
    
    echo "响应: $RESPONSE"
    
    # 等待一下，让连接有时间断开
    sleep 2
done

echo ""
echo "🎉 broken pipe 修复测试完成！"

echo ""
echo "💡 如果看到 'broken pipe' 错误，说明修复未生效"
echo "💡 如果所有请求都成功，说明修复生效"
