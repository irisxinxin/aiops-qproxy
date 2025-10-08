#!/bin/bash
# 诊断真实 Q CLI 环境问题的脚本

set -e

echo "🔍 诊断真实 Q CLI 环境..."

# 检查 Q CLI
echo "1️⃣ 检查 Q CLI..."
if command -v q &> /dev/null; then
    echo "✅ Q CLI 已安装"
    echo "版本信息:"
    q --version 2>&1 || echo "无法获取版本信息"
    echo ""
    echo "用户信息:"
    q whoami 2>&1 || echo "无法获取用户信息"
else
    echo "❌ Q CLI 未安装"
    echo "请安装: pip install amazon-q-cli"
fi

echo ""

# 检查 ttyd
echo "2️⃣ 检查 ttyd..."
if command -v ttyd &> /dev/null; then
    echo "✅ ttyd 已安装"
    echo "版本信息:"
    ttyd --version 2>&1 || echo "无法获取版本信息"
else
    echo "❌ ttyd 未安装"
    echo "请安装: apt install ttyd"
fi

echo ""

# 检查端口占用
echo "3️⃣ 检查端口占用..."
echo "端口 7682 (ttyd):"
netstat -tlnp | grep 7682 || echo "端口 7682 未被占用"

echo "端口 8080 (incident-worker):"
netstat -tlnp | grep 8080 || echo "端口 8080 未被占用"

echo ""

# 检查进程
echo "4️⃣ 检查相关进程..."
echo "ttyd 进程:"
ps aux | grep ttyd | grep -v grep || echo "无 ttyd 进程"

echo "incident-worker 进程:"
ps aux | grep incident-worker | grep -v grep || echo "无 incident-worker 进程"

echo ""

# 检查日志文件
echo "5️⃣ 检查日志文件..."
if [ -f "./logs/ttyd-q.log" ]; then
    echo "✅ ttyd 日志存在"
    echo "最后 10 行:"
    tail -10 ./logs/ttyd-q.log
else
    echo "❌ ttyd 日志不存在"
fi

echo ""

if [ -f "./logs/incident-worker-real.log" ]; then
    echo "✅ incident-worker 日志存在"
    echo "最后 10 行:"
    tail -10 ./logs/incident-worker-real.log
else
    echo "❌ incident-worker 日志不存在"
fi

echo ""

# 测试 ttyd 连接
echo "6️⃣ 测试 ttyd 连接..."
if curl -s -k https://127.0.0.1:7682/ws >/dev/null 2>&1; then
    echo "✅ ttyd WebSocket 连接正常"
else
    echo "❌ ttyd WebSocket 连接失败"
    echo "尝试 HTTP 连接:"
    curl -s -k http://127.0.0.1:7682/ws 2>&1 || echo "HTTP 连接也失败"
fi

echo ""

# 测试 incident-worker 健康检查
echo "7️⃣ 测试 incident-worker 健康检查..."
if curl -s -k http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "✅ incident-worker 健康检查通过"
else
    echo "❌ incident-worker 健康检查失败"
    echo "尝试连接:"
    curl -s -k http://127.0.0.1:8080/healthz 2>&1 || echo "连接失败"
fi

echo ""

# 检查目录权限
echo "8️⃣ 检查目录权限..."
echo "conversations 目录:"
ls -ld ./conversations 2>/dev/null || echo "conversations 目录不存在"

echo "logs 目录:"
ls -ld ./logs 2>/dev/null || echo "logs 目录不存在"

echo ""

# 检查环境变量
echo "9️⃣ 检查环境变量..."
echo "QPROXY_WS_URL: $QPROXY_WS_URL"
echo "QPROXY_WS_USER: $QPROXY_WS_USER"
echo "QPROXY_WS_PASS: $QPROXY_WS_PASS"
echo "QPROXY_CONV_ROOT: $QPROXY_CONV_ROOT"
echo "QPROXY_WS_INSECURE_TLS: $QPROXY_WS_INSECURE_TLS"

echo ""

echo "🎯 诊断完成！"
echo ""
echo "💡 建议："
echo "1. 如果 Q CLI 未安装，请先安装: pip install amazon-q-cli"
echo "2. 如果 ttyd 未安装，请先安装: apt install ttyd"
echo "3. 如果端口被占用，请杀死相关进程"
echo "4. 如果连接失败，请检查防火墙和网络配置"
echo "5. 查看详细日志: tail -f ./logs/ttyd-q.log 和 tail -f ./logs/incident-worker-real.log"
