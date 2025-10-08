#!/bin/bash

echo "🔍 检查编译和依赖问题..."

cd "$(dirname "$0")/.."

echo "🔍 检查 Go 环境："
go version
echo ""

echo "🔍 检查 Go 模块："
go mod tidy
echo ""

echo "🔍 检查编译错误："
echo "尝试编译 incident-worker..."
if go build -v -o ./bin/incident-worker-test ./cmd/incident-worker; then
    echo "✅ 编译成功"
    echo ""
    echo "🔍 检查二进制文件："
    ls -la ./bin/incident-worker-test
    echo ""
    echo "🔍 检查依赖："
    ldd ./bin/incident-worker-test 2>/dev/null || echo "不是动态链接"
    echo ""
    echo "🔍 测试运行："
    echo "尝试运行二进制文件..."
    timeout 5s ./bin/incident-worker-test 2>&1 || echo "程序退出或超时"
else
    echo "❌ 编译失败"
fi

echo ""
echo "🔍 检查目录权限："
ls -la ./conversations/
ls -la ./logs/

echo ""
echo "💡 如果编译成功但运行失败，可能是："
echo "  1. 运行时依赖缺失"
echo "  2. 权限问题"
echo "  3. 环境变量问题"
echo "  4. 端口被占用"
