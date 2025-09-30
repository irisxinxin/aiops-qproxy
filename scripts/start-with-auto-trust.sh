#!/bin/bash
# 启动 HTTP 服务，带自动信任设置

echo "🚀 启动 AIOps Q Proxy (带自动信任)"
echo

# 设置自动信任环境变量
echo "📝 设置环境变量..."
export Q_MCP_AUTO_TRUST=true
export Q_MCP_SKIP_TRUST_PROMPTS=true
export Q_TOOLS_AUTO_TRUST=true

echo "✅ 自动信任环境变量已设置"

# 构建程序（如果需要）
if [ ! -f "bin/qproxy-runner" ]; then
    echo "🔧 构建程序..."
    go build -o bin/qproxy-runner ./cmd/runner
    echo "✅ 构建完成"
fi

# 启动 HTTP 服务
echo
echo "🌐 启动 HTTP 服务..."
echo "   监听端口: 8080"
echo "   自动信任: 已启用"
echo

# 前台运行以便看到输出
./bin/qproxy-runner --listen=:8080
