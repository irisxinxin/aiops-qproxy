#!/bin/bash
# 一次性设置 Q CLI 信任，避免每次 HTTP 请求都要 trust

echo "🔧 设置 Amazon Q CLI 自动信任..."
echo

# 方法1: 设置环境变量
echo "方法1: 环境变量 (推荐)"
export Q_MCP_AUTO_TRUST=true
export Q_MCP_SKIP_TRUST_PROMPTS=true
export Q_TOOLS_AUTO_TRUST=true

echo "✅ 环境变量已设置:"
echo "   Q_MCP_AUTO_TRUST=true"
echo "   Q_MCP_SKIP_TRUST_PROMPTS=true" 
echo "   Q_TOOLS_AUTO_TRUST=true"

# 方法2: 创建 Q CLI 配置文件
echo
echo "方法2: 配置文件"
Q_CONFIG_DIR="$HOME/.q"
mkdir -p "$Q_CONFIG_DIR"

cat > "$Q_CONFIG_DIR/config.json" << 'EOF'
{
  "mcp": {
    "auto_trust": true,
    "skip_prompts": true,
    "session_settings": {
      "auto_trust_all": true,
      "skip_trust_prompts": true
    }
  },
  "tools": {
    "auto_trust": true,
    "skip_permission_prompts": true
  }
}
EOF

echo "✅ 配置文件已创建: $Q_CONFIG_DIR/config.json"

# 方法3: 一次性手动信任
echo
echo "方法3: 一次性手动信任 (备选)"
if command -v q >/dev/null 2>&1; then
    echo "发现 Q CLI，执行一次性信任..."
    echo "/tools trust-all" | q 2>/dev/null
    echo "✅ 手动信任完成"
else
    echo "⚠️  未找到 Q CLI 命令"
fi

echo
echo "🎯 推荐方案："
echo "1. 在启动 HTTP 服务前运行: source ./scripts/setup-q-trust.sh"
echo "2. 或在 systemd 服务文件中设置环境变量"
echo "3. 或在启动脚本中设置 export Q_MCP_AUTO_TRUST=true"
