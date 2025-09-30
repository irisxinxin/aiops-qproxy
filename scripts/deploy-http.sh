#!/bin/bash

# 生产环境 HTTP 服务部署脚本
# 用于部署 aiops-qproxy HTTP 服务

set -e

echo "=== AIOps QProxy HTTP 服务部署 ==="
echo "时间: $(date)"
echo

# 检查是否在正确的目录
if [ ! -f "cmd/runner/main.go" ]; then
    echo "错误: 请在项目根目录运行此脚本"
    exit 1
fi

# 1. 重新构建程序
echo "--- 步骤 1: 重新构建程序 ---"
echo "构建 qproxy-runner..."
go build -o bin/qproxy-runner ./cmd/runner

if [ -f "bin/qproxy-runner" ]; then
    echo "✅ 构建成功"
    ls -la bin/qproxy-runner
else
    echo "❌ 构建失败"
    exit 1
fi

echo

# 2. 更新 systemd 服务
echo "--- 步骤 2: 更新 systemd 服务 ---"
if [ -f "systemd/aiops-qproxy-runner.service" ]; then
    echo "复制 systemd 服务文件..."
    sudo cp systemd/aiops-qproxy-runner.service /etc/systemd/system/
    echo "✅ 服务文件已更新"
else
    echo "❌ 找不到 systemd 服务文件"
    exit 1
fi

echo "重新加载 systemd..."
sudo systemctl daemon-reload
echo "✅ systemd 已重新加载"

echo

# 3. 停止旧服务（如果存在）
echo "--- 步骤 3: 停止旧服务 ---"
if systemctl is-active --quiet aiops-qproxy-runner; then
    echo "停止现有服务..."
    sudo systemctl stop aiops-qproxy-runner
    echo "✅ 服务已停止"
else
    echo "服务未运行，跳过停止步骤"
fi

echo

# 4. 启动新服务
echo "--- 步骤 4: 启动 HTTP 服务 ---"
echo "启动 aiops-qproxy-runner 服务..."
sudo systemctl start aiops-qproxy-runner

# 等待服务启动
sleep 3

echo "检查服务状态..."
if systemctl is-active --quiet aiops-qproxy-runner; then
    echo "✅ 服务启动成功"
else
    echo "❌ 服务启动失败"
    echo "查看服务状态:"
    sudo systemctl status aiops-qproxy-runner --no-pager
    exit 1
fi

echo

# 4.5. 设置 Q CLI 信任
echo "--- 步骤 4.5: 设置 Q CLI 信任 ---"
if [ -f "/home/ubuntu/.local/bin/q" ]; then
    echo "设置 Q CLI 自动信任..."
    export Q_BIN=/home/ubuntu/.local/bin/q
    echo -e "y\nq" | $Q_BIN /tools trust-all
    echo "✅ Q CLI 信任设置完成"
else
    echo "⚠️  Q CLI 未找到，跳过信任设置"
fi

echo

# 5. 测试服务
echo "--- 步骤 5: 测试服务 ---"

# 测试健康检查
echo "测试健康检查端点..."
if curl -s http://localhost:8080/health | jq '.' >/dev/null 2>&1; then
    echo "✅ 健康检查通过"
    curl -s http://localhost:8080/health | jq '.'
else
    echo "❌ 健康检查失败"
    echo "查看服务日志:"
    sudo journalctl -u aiops-qproxy-runner --no-pager -n 20
    exit 1
fi

echo

# 跳过告警处理测试（避免运行缓慢）
echo "⚠️  跳过告警处理测试（避免运行缓慢）"
echo "如需测试告警处理，请手动运行："
echo "curl -X POST http://localhost:8080/alert \\"
echo "  -H \"Content-Type: application/json\" \\"
echo "  -d '{\"service\":\"omada-central\",\"region\":\"prd-nbu-aps1\",\"category\":\"latency\",\"severity\":\"critical\"}'"

echo

# 6. 显示服务信息
echo "--- 步骤 6: 服务信息 ---"
echo "服务状态:"
sudo systemctl status aiops-qproxy-runner --no-pager

echo
echo "服务配置:"
echo "  监听端口: 8080"
echo "  健康检查: http://localhost:8080/health"
echo "  告警处理: http://localhost:8080/alert"
echo "  服务用户: ubuntu"
echo "  工作目录: /home/ubuntu/huixin/aiops/aiops-qproxy-v2.4"

echo
echo "常用命令:"
echo "  查看状态: sudo systemctl status aiops-qproxy-runner"
echo "  查看日志: sudo journalctl -u aiops-qproxy-runner -f"
echo "  重启服务: sudo systemctl restart aiops-qproxy-runner"
echo "  停止服务: sudo systemctl stop aiops-qproxy-runner"

echo
echo "=== 部署完成 ==="
echo "HTTP 服务已成功部署并运行在端口 8080"
echo "可以通过 curl 调用告警处理功能"
