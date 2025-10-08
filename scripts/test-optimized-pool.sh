#!/bin/bash
# 测试优化后的连接池和重试机制

set -e

echo "🚀 测试优化后的连接池和重试机制..."

# 检查服务状态
echo "📋 检查服务状态..."
if ! curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "❌ incident-worker 未运行，请先运行 deploy-real-q.sh"
    exit 1
fi

echo "✅ incident-worker 运行正常"

# 测试1: 正常请求
echo ""
echo "🧪 测试1: 正常请求..."
RESPONSE1=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"test-normal-1","prompt":"Hello Q CLI, please analyze this normal request."}')

echo "响应: $RESPONSE1"

# 测试2: 连续请求（测试连接池复用）
echo ""
echo "🧪 测试2: 连续请求（测试连接池复用）..."
for i in {1..3}; do
    echo "📤 连续请求 $i..."
    RESPONSE=$(curl -sS -X POST http://127.0.0.1:8080/incident \
      -H "content-type: application/json" \
      -d "{\"incident_key\":\"test-continuous-$i\",\"prompt\":\"Continuous request $i: Please analyze this issue.\"}")
    
    echo "响应: $RESPONSE"
    sleep 1
done

# 测试3: 模拟连接断开（等待较长时间后请求）
echo ""
echo "🧪 测试3: 模拟连接断开（等待30秒后请求）..."
echo "⏳ 等待30秒，让连接可能断开..."
sleep 30

RESPONSE3=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"test-after-wait","prompt":"After waiting 30 seconds, please analyze this issue."}')

echo "响应: $RESPONSE3"

# 测试4: 快速连续请求（测试重试机制）
echo ""
echo "🧪 测试4: 快速连续请求（测试重试机制）..."
for i in {1..5}; do
    echo "📤 快速请求 $i..."
    RESPONSE=$(curl -sS -X POST http://127.0.0.1:8080/incident \
      -H "content-type: application/json" \
      -d "{\"incident_key\":\"test-fast-$i\",\"prompt\":\"Fast request $i: Please analyze this issue.\"}")
    
    echo "响应: $RESPONSE"
    sleep 0.5
done

echo ""
echo "🎉 优化测试完成！"

echo ""
echo "📊 测试结果分析："
echo "  - 如果所有请求都成功，说明连接池和重试机制工作正常"
echo "  - 如果出现 'broken pipe' 错误，说明需要进一步优化"
echo "  - 如果响应时间过长，说明重试机制在工作"

echo ""
echo "💡 查看详细日志："
echo "  - incident-worker: tail -f ./logs/incident-worker-real.log"
echo "  - ttyd: tail -f ./logs/ttyd-q.log"
