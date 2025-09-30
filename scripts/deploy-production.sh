#!/bin/bash

# 生产环境部署脚本
# 目标目录: /home/ubuntu/huixin/aiops/aiops-qproxy-v2.4

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TARGET_DIR="/home/ubuntu/huixin/aiops/aiops-qproxy-v2.4"

echo "=== AIOps QProxy 生产环境部署 ==="
echo "源目录: $PROJECT_DIR"
echo "目标目录: $TARGET_DIR"
echo

# 检查是否在正确的环境中运行
if [[ "$(whoami)" != "ubuntu" ]]; then
    echo "⚠️  建议以 ubuntu 用户运行此脚本"
    echo "当前用户: $(whoami)"
    read -p "是否继续? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# 1. 创建目标目录
echo "--- 创建目标目录 ---"
sudo mkdir -p "$(dirname "$TARGET_DIR")"
sudo chown ubuntu:ubuntu "$(dirname "$TARGET_DIR")"

# 2. 复制项目文件
echo "--- 复制项目文件 ---"
if [[ -d "$TARGET_DIR" ]]; then
    echo "目标目录已存在，备份现有文件..."
    sudo mv "$TARGET_DIR" "${TARGET_DIR}.backup.$(date +%Y%m%d_%H%M%S)"
fi

sudo cp -r "$PROJECT_DIR" "$TARGET_DIR"
sudo chown -R ubuntu:ubuntu "$TARGET_DIR"

# 3. 设置权限
echo "--- 设置权限 ---"
chmod +x "$TARGET_DIR/bin/qproxy-runner"
chmod +x "$TARGET_DIR/scripts"/*.sh
chmod +x "$TARGET_DIR/run-qproxy.sh"

# 4. 创建必要目录
echo "--- 创建必要目录 ---"
mkdir -p "$TARGET_DIR/ctx/sop" "$TARGET_DIR/ctx/final" "$TARGET_DIR/logs" "$TARGET_DIR/data"

# 5. 检查关键文件
echo "--- 检查关键文件 ---"
required_files=(
    "$TARGET_DIR/bin/qproxy-runner"
    "$TARGET_DIR/ctx/sop/omada_sop_full.jsonl"
    "$TARGET_DIR/ctx/sop/vigi_sop_full.jsonl"
    "$TARGET_DIR/systemd/aiops-qproxy-runner.service" 
)

for file in "${required_files[@]}"; do
    if [[ -f "$file" ]]; then
        echo "✅ $(basename "$file")"
    else
        echo "❌ $(basename "$file") - 文件不存在"
        exit 1
    fi
done

# 6. 检查 q CLI
echo "--- 检查 q CLI ---"
if command -v q >/dev/null 2>&1; then
    echo "✅ q CLI 已安装: $(which q)"
    q --version 2>/dev/null || echo "⚠️  q CLI 版本信息获取失败"
else
    echo "❌ q CLI 未安装，请先安装 q CLI"
    exit 1
fi

# 7. 安装 systemd 服务
echo "--- 安装 systemd 服务 ---"
sudo cp "$TARGET_DIR/systemd/aiops-qproxy-runner.service" /etc/systemd/system/
sudo systemctl daemon-reload

# 8. 启用服务
echo "--- 启用服务 ---"
sudo systemctl enable aiops-qproxy-runner

# 9. 测试配置
echo "--- 测试配置 ---"
cd "$TARGET_DIR"

# 测试 SOP 匹配
if echo '{"service":"omada-central","region":"prd-nbu-aps1","category":"latency","severity":"critical"}' | \
   Q_SOP_DIR="$TARGET_DIR/ctx/sop" Q_SOP_PREPEND=1 Q_BIN=/bin/cat go run cmd/runner/main.go 2>&1 | \
   grep -q "SOP.*knowledge"; then
    echo "✅ SOP 匹配功能正常"
else
    echo "❌ SOP 匹配功能异常"
    exit 1
fi

# 9.5. 设置 Q CLI 自动信任环境变量
echo "--- 设置 Q CLI 自动信任环境变量 ---"
echo "设置环境变量以启用自动信任..."
export Q_MCP_AUTO_TRUST=true
export Q_MCP_SKIP_TRUST_PROMPTS=true
export Q_TOOLS_AUTO_TRUST=true
echo "✅ Q CLI 自动信任环境变量已设置"
echo "   Q_MCP_AUTO_TRUST=true"
echo "   Q_MCP_SKIP_TRUST_PROMPTS=true"
echo "   Q_TOOLS_AUTO_TRUST=true"

# 10. 启动服务
echo "--- 启动服务 ---"
sudo systemctl start aiops-qproxy-runner

# 11. 检查服务状态
echo "--- 检查服务状态 ---"
sleep 2
if sudo systemctl is-active --quiet aiops-qproxy-runner; then
    echo "✅ 服务启动成功"
else
    echo "❌ 服务启动失败"
    echo "服务状态:"
    sudo systemctl status aiops-qproxy-runner --no-pager
    echo "服务日志:"
    sudo journalctl -u aiops-qproxy-runner --since "1 minute ago" --no-pager
    exit 1
fi

echo
echo "=== 部署完成 ==="
echo "服务状态: $(sudo systemctl is-active aiops-qproxy-runner)"
echo "服务日志: sudo journalctl -u aiops-qproxy-runner -f"
echo "重启服务: sudo systemctl restart aiops-qproxy-runner"
echo "停止服务: sudo systemctl stop aiops-qproxy-runner"
echo
echo "测试命令:"
echo "echo '{\"service\":\"omada-central\",\"region\":\"prd-nbu-aps1\",\"category\":\"latency\",\"severity\":\"critical\"}' | $TARGET_DIR/bin/qproxy-runner"
