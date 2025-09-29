#!/bin/bash
# Q Auto Trust Preparation Script
# 预配置 Q CLI 自动信任 MCP 工具

echo "🔧 配置 Amazon Q 自动信任 MCP 工具"
echo

# 方法1: 环境变量配置
echo "方法1: 设置环境变量"
export Q_MCP_AUTO_TRUST=true
export Q_TRUST_VICTORIAMETRICS=true
export Q_TRUST_AWSLABSEKS_MCP_SERVER=true
export Q_TRUST_ELASTICSEARCH_MCP_SERVER=true
export Q_TRUST_AWSLABSCLOUDWATCH_MCP_SERVER=true
export Q_TRUST_ALERTMANAGER=true

echo "✅ 环境变量已设置"

# 方法2: 创建配置文件
echo
echo "方法2: 创建 Q CLI 配置文件"
Q_CONFIG_DIR="$HOME/.config/q"
mkdir -p "$Q_CONFIG_DIR"

cat > "$Q_CONFIG_DIR/config.json" << 'EOF'
{
  "mcp": {
    "auto_trust": true,
    "trusted_servers": [
      "victoriametrics",
      "awslabseks_mcp_server", 
      "elasticsearch_mcp_server",
      "awslabscloudwatch_mcp_server",
      "alertmanager"
    ],
    "session_settings": {
      "auto_trust_all": true,
      "skip_trust_prompts": true
    }
  }
}
EOF

echo "✅ 配置文件已创建: $Q_CONFIG_DIR/config.json"

# 方法3: 信任指令
echo
echo "方法3: 准备信任指令"
echo "如果您手动运行 Q CLI，可以使用以下指令："
echo "/tools trust-all"
echo "/tools trust victoriametrics"
echo "/tools trust awslabseks_mcp_server"
echo "/tools trust elasticsearch_mcp_server"
echo "/tools trust awslabscloudwatch_mcp_server"
echo "/tools trust alertmanager"

echo
echo "🎯 建议在运行 Amazon Q 前执行此脚本，或手动运行 /tools trust-all"
