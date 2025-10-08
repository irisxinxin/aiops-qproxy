#!/bin/bash
# 测试 Q CLI 连接维持时间

set -e

echo "🧪 测试 Q CLI 连接维持时间..."

# 检查服务状态
echo "📋 检查服务状态..."
if ! curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "❌ incident-worker 未运行，请先运行 deploy-real-q.sh"
    exit 1
fi

echo "✅ incident-worker 运行正常"

# 测试连接维持时间
echo ""
echo "🧪 测试连接维持时间..."

# 第一次请求
echo "📤 第1次请求（建立连接）..."
RESPONSE1=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"test-connection-duration-1","prompt":"Hello Q CLI, please analyze this issue."}')

echo "响应: $RESPONSE1"

# 等待不同时间间隔后测试
intervals=(5 10 30 60 120 300) # 5秒, 10秒, 30秒, 1分钟, 2分钟, 5分钟

for interval in "${intervals[@]}"; do
    echo ""
    echo "⏳ 等待 $interval 秒..."
    sleep $interval
    
    echo "📤 第2次请求（间隔${interval}秒）..."
    RESPONSE2=$(curl -sS -X POST http://127.0.0.1:8080/incident \
      -H "content-type: application/json" \
      -d "{\"incident_key\":\"test-connection-duration-2\",\"prompt\":\"Test after ${interval}s: Please analyze this issue.\"}")
    
    echo "响应: $RESPONSE2"
    
    # 检查是否包含 broken pipe 错误
    if echo "$RESPONSE2" | grep -q "broken pipe"; then
        echo "⚠️  连接在 ${interval} 秒后断开"
        break
    elif echo "$RESPONSE2" | grep -q "answer"; then
        echo "✅ 连接在 ${interval} 秒后仍然有效"
    else
        echo "❓ 未知响应"
    fi
done

echo ""
echo "📊 测试结果分析："
echo "  - 如果连接在短时间内断开，说明 Q CLI 不适合长连接"
echo "  - 如果连接能维持较长时间，说明问题在其他地方"
echo "  - 建议：根据测试结果调整连接池策略"

echo ""
echo "💡 可能的解决方案："
echo "  1. 减少连接池大小（避免资源浪费）"
echo "  2. 缩短连接最大存活时间"
echo "  3. 实现更频繁的连接重建"
echo "  4. 使用短连接策略（每次请求都重新连接）"
