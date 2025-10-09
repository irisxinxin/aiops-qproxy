#!/bin/bash
# 测试 pprof 是否正常工作

set -e

echo "🔍 测试 pprof 服务..."

# 检查 pprof 端口是否开启
if ss -tlnp 2>/dev/null | grep -q ":6060 " || netstat -tlnp 2>/dev/null | grep -q ":6060 "; then
    echo "✅ pprof 端口 6060 已开启"
else
    echo "❌ pprof 端口 6060 未开启"
    echo "   请确保 QPROXY_PPROF=1 环境变量已设置"
    exit 1
fi

# 测试 pprof 主页
echo ""
echo "📊 测试 pprof 主页..."
if curl -s http://127.0.0.1:6060/debug/pprof/ | grep -q "Types of profiles available"; then
    echo "✅ pprof 主页访问成功"
else
    echo "❌ pprof 主页访问失败"
    exit 1
fi

# 显示可用的 profile 类型
echo ""
echo "📋 可用的 profile 类型："
curl -s http://127.0.0.1:6060/debug/pprof/ | grep -oP '/debug/pprof/\w+' | sort -u

echo ""
echo "🎯 常用命令："
echo "  查看所有 goroutines:"
echo "    curl http://127.0.0.1:6060/debug/pprof/goroutine?debug=1"
echo ""
echo "  查看堆内存:"
echo "    curl http://127.0.0.1:6060/debug/pprof/heap?debug=1"
echo ""
echo "  30秒 CPU profile:"
echo "    curl http://127.0.0.1:6060/debug/pprof/profile?seconds=30 -o cpu.prof"
echo ""
echo "  查看当前 goroutine 数量:"
echo "    curl -s http://127.0.0.1:6060/debug/pprof/goroutine?debug=1 | grep 'goroutine profile:' "
echo ""
echo "💡 如需从本地访问，使用 SSH 端口转发："
echo "   ssh -L 6060:127.0.0.1:6060 ubuntu@your-server-ip"
echo "   然后访问 http://localhost:6060/debug/pprof/"

