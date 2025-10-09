#!/bin/bash
# 测试最简单的 prompt，排查 Q CLI 崩溃问题

echo "🧪 测试简单 prompt..."
echo ""

# 测试 1: 超级简单的 prompt
echo "测试 1: 'hello'"
echo "======================================"
RESPONSE1=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"test-simple-1","prompt":"hello"}')

echo "响应: $RESPONSE1"
echo ""
echo ""

# 测试 2: 稍微复杂一点
echo "测试 2: 简单的英文问题"
echo "======================================"
RESPONSE2=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"test-simple-2","prompt":"What is 1+1?"}')

echo "响应: $RESPONSE2"
echo ""
echo ""

# 测试 3: 中文 prompt
echo "测试 3: 简单的中文问题"
echo "======================================"
RESPONSE3=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"test-simple-3","prompt":"你好"}')

echo "响应: $RESPONSE3"
echo ""
echo ""

# 测试 4: 带换行符的 prompt
echo "测试 4: 带换行符的 prompt"
echo "======================================"
RESPONSE4=$(curl -sS -X POST http://127.0.0.1:8080/incident \
  -H "content-type: application/json" \
  -d '{"incident_key":"test-simple-4","prompt":"Line 1\nLine 2\nLine 3"}')

echo "响应: $RESPONSE4"
echo ""
echo ""

echo "✅ 测试完成"
echo ""
echo "💡 分析:"
echo "  - 如果所有测试都失败: Q CLI 本身有问题"
echo "  - 如果简单 prompt 成功，复杂 prompt 失败: prompt 长度或格式问题"
echo "  - 如果中文失败，英文成功: 编码问题"
echo "  - 如果带换行符失败: 换行符处理问题"

