#!/bin/bash

# 测试 q CLI 的脚本
# 用于验证 q CLI 的安装和基本功能

set -e

echo "=== q CLI 测试 ==="
echo

# 1. 检查 q CLI 是否安装
echo "--- 检查 q CLI 安装 ---"
if command -v q >/dev/null 2>&1; then
    echo "✅ q CLI 已安装"
    echo "路径: $(which q)"
    
    # 检查版本
    echo "版本信息:"
    q --version 2>/dev/null || echo "无法获取版本信息"
else
    echo "❌ q CLI 未安装"
    echo
    echo "安装方法:"
    echo "1. 访问 Amazon Q 控制台"
    echo "2. 下载并安装 q CLI"
    echo "3. 配置认证信息"
    echo
    echo "或者使用 mock 进行测试:"
    echo "export Q_BIN=/bin/cat"
    exit 1
fi

echo

# 2. 测试基本功能
echo "--- 测试基本功能 ---"
echo "测试简单对话:"
echo "Hello, how are you?" | q 2>/dev/null || echo "基本功能测试失败"

echo
echo "测试 JSON 解析:"
echo '{"test": "value", "number": 123}' | q "Parse this JSON and explain the structure" 2>/dev/null || echo "JSON 解析测试失败"

echo

# 3. 测试我们的 prompt 格式
echo "--- 测试 prompt 格式 ---"
test_prompt="/tools trust-all

[USER]
你是我的AIOps只读归因助手。严格禁止任何写操作。
任务：分析以下告警并输出 JSON 格式的根因分析。

【Normalized Alert】
{
  \"service\": \"test-service\",
  \"region\": \"test-region\",
  \"category\": \"cpu\",
  \"severity\": \"critical\"
}
[/USER]

/quit"

echo "测试完整 prompt:"
echo "$test_prompt" | q 2>/dev/null || echo "完整 prompt 测试失败"

echo

# 4. 测试不同的 prompt 格式
echo "--- 测试不同 prompt 格式 ---"

echo "格式 1 - 简单指令:"
echo "Analyze this alert: {\"service\":\"test\",\"severity\":\"critical\"}" | q 2>/dev/null || echo "格式 1 测试失败"

echo
echo "格式 2 - 带上下文:"
echo "Context: This is a CPU alert. Analyze: {\"service\":\"test\",\"severity\":\"critical\"}" | q 2>/dev/null || echo "格式 2 测试失败"

echo
echo "格式 3 - 结构化:"
echo "TASK: Analyze alert
INPUT: {\"service\":\"test\",\"severity\":\"critical\"}
OUTPUT: JSON format" | q 2>/dev/null || echo "格式 3 测试失败"

echo

# 5. 检查 q CLI 配置
echo "--- 检查 q CLI 配置 ---"
if [ -f ~/.q/config ]; then
    echo "配置文件存在: ~/.q/config"
    echo "配置内容:"
    cat ~/.q/config | head -5
else
    echo "配置文件不存在: ~/.q/config"
fi

echo
echo "=== 测试完成 ==="
echo
echo "建议:"
echo "1. 如果 q CLI 工作正常，检查我们的 prompt 格式"
echo "2. 如果 q CLI 有问题，检查安装和配置"
echo "3. 可以尝试简化 prompt 格式"
