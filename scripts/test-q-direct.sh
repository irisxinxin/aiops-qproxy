#!/bin/bash
# 直接测试 Q CLI 是否能正常响应

set -e

echo "🧪 测试 Q CLI 直接响应..."

# 创建测试 prompt
TEST_PROMPT="Hello, please respond with 'OK' if you can hear me."

echo "📝 测试 prompt: $TEST_PROMPT"
echo ""

# 方法1: 使用 echo 管道
echo "方法1: 使用 echo 管道"
timeout 10s bash -c "echo '$TEST_PROMPT' | env NO_COLOR=1 TERM=dumb Q_MCP_AUTO_TRUST=true Q_MCP_SKIP_TRUST_PROMPTS=true Q_TOOLS_AUTO_TRUST=true q chat" 2>&1 | head -50
echo ""

# 方法2: 使用 heredoc
echo "方法2: 使用 heredoc"
timeout 10s env NO_COLOR=1 TERM=dumb Q_MCP_AUTO_TRUST=true Q_MCP_SKIP_TRUST_PROMPTS=true Q_TOOLS_AUTO_TRUST=true q chat <<EOF 2>&1 | head -50
$TEST_PROMPT
EOF
echo ""

echo "✅ 测试完成"

