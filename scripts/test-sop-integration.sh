#!/bin/bash

echo "🧪 测试 SOP 集成..."
echo ""

# 测试 1: Omada CPU 告警（应该匹配 SOP）
echo "测试 1: Omada CPU 告警"
echo "======================================"
curl -X POST http://localhost:8080/incident \
  -H "Content-Type: application/json" \
  -d @alerts/dev/test_sop_integration.json \
  2>/dev/null | jq -r '.answer' | head -50
echo ""
echo ""

# 测试 2: 简单 prompt（不应该触发 SOP）
echo "测试 2: 简单 prompt (无 SOP)"
echo "======================================"
curl -X POST http://localhost:8080/incident \
  -H "Content-Type: application/json" \
  -d '{"incident_key":"test-no-sop","prompt":"What is 2+2?"}' \
  2>/dev/null | jq -r '.answer'
echo ""
echo ""

# 测试 3: SDN5 CPU 告警（应该匹配不同的 SOP）
echo "测试 3: SDN5 CPU 告警"
echo "======================================"
curl -X POST http://localhost:8080/incident \
  -H "Content-Type: application/json" \
  -d '{
    "service": "sdn5",
    "category": "cpu",
    "severity": "critical",
    "region": "ap-southeast-1",
    "metadata": {"expression": "avg(cpu_usage) > 85"}
  }' \
  2>/dev/null | jq -r '.answer' | head -50
echo ""

echo "✅ 测试完成"

